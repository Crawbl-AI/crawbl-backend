package infra

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/cliexec"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// newBootstrapCommand creates the infra bootstrap subcommand.
func newBootstrapCommand() *cobra.Command {
	var (
		env         string
		region      string
		autoApprove bool
		clusterName string
		timeout     time.Duration
	)

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap a new cluster from scratch",
		Long: `Bootstrap the full cluster from scratch.

This command automates the entire cluster bootstrap process:
  1. Run infra update (creates DOKS cluster + ArgoCD via Pulumi)
  2. Save kubeconfig via doctl
  3. Ensure DOCR registry integration (safety net)
  4. Wait for ArgoCD application-controller to be ready
  5. Wait for all ArgoCD applications to sync (with timeout)`,
		Example: `  crawbl infra bootstrap                        # Bootstrap with confirmation
  crawbl infra bootstrap --auto-approve        # Bootstrap without confirmation
  crawbl infra bootstrap --cluster crawbl-prod # Use a different cluster name
  crawbl infra bootstrap --timeout 15m         # Custom sync timeout`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBootstrap(cmd.Context(), env, region, autoApprove, clusterName, timeout)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name, for example dev, staging, or prod")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region, for example fra1, nyc1, or sfo2")
	cmd.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "Skip confirmation prompts")
	cmd.Flags().StringVar(&clusterName, "cluster", "crawbl-dev", "DOKS cluster name for kubeconfig")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "Timeout while waiting for applications to sync")

	return cmd
}

func runBootstrap(ctx context.Context, env, region string, autoApprove bool, clusterName string, timeout time.Duration) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	out.Step(style.Infra, "Crawbl Cluster Bootstrap")
	out.Step(style.Config, "Environment: %s | Region: %s | Cluster: %s", env, region, clusterName)
	out.Ln()

	if !autoApprove {
		if !confirmUpdate() {
			out.Warning("Bootstrap cancelled")
			return nil
		}
	}

	// Step 1: Run Pulumi up
	out.Step(style.Infra, "[1/5] Applying infrastructure (Pulumi)...")
	if err := pulumiUp(ctx, env, region); err != nil {
		return fmt.Errorf("pulumi up failed: %w", err)
	}
	out.Step(style.Check, "Infrastructure applied")

	// Step 2: Save kubeconfig
	out.Step(style.Infra, "[2/5] Saving kubeconfig...")
	if err := cliexec.Run(ctx, "doctl", "kubernetes", "cluster", "kubeconfig", "save", clusterName); err != nil {
		return fmt.Errorf("kubeconfig save failed: %w", err)
	}
	out.Step(style.Check, "Kubeconfig saved")

	// Step 3: Ensure DOCR registry integration (Pulumi sets registryIntegration=true
	// on the cluster, but doctl registry add is a safety net in case of state drift).
	out.Step(style.Infra, "[3/5] Ensuring DOCR registry integration...")
	if err := cliexec.Run(ctx, "doctl", "kubernetes", "cluster", "registry", "add", clusterName); err != nil {
		out.Warning("Registry add failed and may already be integrated: %v", err)
	}
	out.Step(style.Check, "Registry integration verified")

	// Step 4: Wait for controller to be ready
	out.Step(style.Infra, "[4/5] Waiting for ArgoCD application-controller...")
	if err := waitForController(ctx, timeout); err != nil {
		return fmt.Errorf("controller readiness failed: %w", err)
	}
	out.Step(style.Ready, "ArgoCD application-controller is ready")

	// Step 5: Wait for all apps to sync
	out.Step(style.Infra, "[5/5] Waiting for all applications to sync...")
	if err := waitForAppsSync(timeout); err != nil {
		return fmt.Errorf("app sync wait failed: %w", err)
	}
	out.Step(style.Ready, "Applications are synced")

	// Print final status
	out.Ln()
	out.Step(style.Celebrate, "Bootstrap complete")
	printAppStatus()
	return nil
}

// waitForController waits for the ArgoCD application-controller StatefulSet to be ready.
func waitForController(ctx context.Context, timeout time.Duration) error {
	return cliexec.Run(ctx, "kubectl", "rollout", "status",
		"statefulset/argocd-application-controller",
		"-n", "argocd",
		"--timeout", timeout.String(),
	)
}

// waitForAppsSync polls ArgoCD applications until all are Synced/Healthy or timeout.
func waitForAppsSync(timeout time.Duration) error {
	const appSyncPollInterval = 15 * time.Second
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for apps to sync", timeout)
		}

		synced, total, err := checkAppSyncStatus()
		if err != nil {
			out.Infof("Waiting for applications to appear... (%v)", err)
			time.Sleep(10 * time.Second)
			continue
		}

		out.Infof("%d/%d applications synced", synced, total)

		if total > 0 && synced == total {
			return nil
		}

		time.Sleep(appSyncPollInterval)
	}
}

// checkAppSyncStatus returns (synced count, total count, error).
func checkAppSyncStatus() (int, int, error) {
	cmd := exec.CommandContext(context.Background(), "kubectl", "get", "applications", "-n", "argocd",
		"-o", "jsonpath={range .items[*]}{.status.sync.status},{.status.health.status}{\"\\n\"}{end}")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return 0, 0, fmt.Errorf("no applications found")
	}

	total := 0
	synced := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		total++
		parts := strings.SplitN(line, ",", 2)
		syncStatus := parts[0]
		healthStatus := ""
		if len(parts) > 1 {
			healthStatus = parts[1]
		}
		if syncStatus == "Synced" && (healthStatus == "Healthy" || healthStatus == "Progressing") {
			synced++
		}
	}
	return synced, total, nil
}

// printAppStatus prints the current status of all ArgoCD applications.
func printAppStatus() {
	out.Ln()
	out.Step(style.Config, "Application status:")
	cmd := exec.CommandContext(context.Background(), "kubectl", "get", "applications", "-n", "argocd",
		"-o", "custom-columns=NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status,MESSAGE:.status.conditions[0].message")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}
