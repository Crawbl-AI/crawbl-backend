package infra

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/cliexec"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// newUpdateCommand creates the infra update subcommand.
func newUpdateCommand() *cobra.Command {
	var (
		env            string
		region         string
		autoApprove    bool
		saveKubeconfig bool
		clusterName    string
		syncTimeout    time.Duration
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Apply infrastructure changes",
		Long: `Create or update infrastructure using Pulumi.

Pulumi manages:
  - DOKS cluster + VPC + container registry
  - Managed databases (PostgreSQL, Valkey, PgBouncer) — prod only
  - Cloudflare Tunnel + DNS — dev only
  - ArgoCD namespace + Helm release + repo secret + root Application

Everything else is managed by ArgoCD via the crawbl-argocd-apps repo.

Use --save-kubeconfig on first-time setup to automatically configure
kubectl, verify DOCR integration, and wait for ArgoCD to sync.`,
		Example: `  crawbl infra update                              # Apply with confirmation
  crawbl infra update --auto-approve               # Apply without confirmation
  crawbl infra update --save-kubeconfig            # Apply + save kubeconfig + wait for ArgoCD
  crawbl infra update --env prod --auto-approve    # Apply prod changes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), env, region, autoApprove, saveKubeconfig, clusterName, syncTimeout)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name, for example dev, staging, or prod")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region, for example fra1, nyc1, or sfo2")
	cmd.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&saveKubeconfig, "save-kubeconfig", false, "Save kubeconfig, verify DOCR, and wait for ArgoCD sync after apply")
	cmd.Flags().StringVar(&clusterName, "cluster", "", "DOKS cluster name (defaults to crawbl-<env>)")
	cmd.Flags().DurationVar(&syncTimeout, "sync-timeout", 10*time.Minute, "Timeout for ArgoCD sync wait (used with --save-kubeconfig)")

	return cmd
}

func runUpdate(ctx context.Context, env, region string, autoApprove, saveKubeconfig bool, clusterName string, syncTimeout time.Duration) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	if clusterName == "" {
		clusterName = "crawbl-" + env
	}

	out.Step(style.Infra, "Applying infrastructure changes for environment %q in region %q", env, region)

	if !autoApprove {
		if !confirmPrompt("Do you want to perform this action? (y/N): ") {
			out.Warning("Update cancelled")
			return nil
		}
	}

	if err := pulumiUp(ctx, env, region); err != nil {
		return err
	}

	if !saveKubeconfig {
		return nil
	}

	// Post-provision: save kubeconfig, verify DOCR, wait for ArgoCD.
	out.Ln()
	out.Step(style.Infra, "Saving kubeconfig for %s...", clusterName)
	if err := cliexec.Run(ctx, "doctl", "kubernetes", "cluster", "kubeconfig", "save", clusterName); err != nil {
		return fmt.Errorf("kubeconfig save failed: %w", err)
	}
	out.Step(style.Check, "Kubeconfig saved")

	out.Step(style.Infra, "Ensuring DOCR registry integration...")
	if err := cliexec.Run(ctx, "doctl", "kubernetes", "cluster", "registry", "add", clusterName); err != nil {
		out.Warning("Registry add may already be integrated: %v", err)
	}
	out.Step(style.Check, "Registry integration verified")

	out.Step(style.Infra, "Waiting for ArgoCD application-controller...")
	if err := waitForController(ctx, syncTimeout); err != nil {
		return fmt.Errorf("controller readiness failed: %w", err)
	}
	out.Step(style.Ready, "ArgoCD application-controller is ready")

	out.Step(style.Infra, "Waiting for all applications to sync...")
	if err := waitForAppsSync(syncTimeout); err != nil {
		return fmt.Errorf("app sync wait failed: %w", err)
	}
	out.Step(style.Ready, "Applications are synced")

	out.Ln()
	out.Step(style.Celebrate, "Infrastructure update complete")
	printAppStatus()
	return nil
}

// pulumiUp is the shared Pulumi apply logic used by both 'update' and 'bootstrap'.
func pulumiUp(ctx context.Context, env, region string) error {
	config, err := buildConfig(env, region)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	result, err := stack.Up(ctx)
	if err != nil {
		return fmt.Errorf("pulumi up failed: %w", err)
	}

	printOutputs(result)
	return nil
}

func validateEnvVars() error {
	// Provider tokens are injected by Pulumi ESC; only PULUMI_ACCESS_TOKEN is required.
	if os.Getenv("PULUMI_ACCESS_TOKEN") == "" {
		return fmt.Errorf("missing PULUMI_ACCESS_TOKEN environment variable")
	}
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

// appSyncPollInterval is how often to check ArgoCD application sync status.
const appSyncPollInterval = 15 * time.Second

// waitForAppsSync polls ArgoCD applications until all are Synced/Healthy or timeout.
func waitForAppsSync(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(appSyncPollInterval)
	defer ticker.Stop()

	for {
		synced, total, err := checkAppSyncStatus()
		if err != nil {
			out.Infof("Waiting for applications to appear... (%v)", err)
		} else {
			out.Infof("%d/%d applications synced", synced, total)
			if total > 0 && synced == total {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out after %s waiting for apps to sync", timeout)
		case <-ticker.C:
		}
	}
}

// checkAppSyncStatus returns (synced count, total count, error).
func checkAppSyncStatus() (int, int, error) {
	kubectlPath, err := exec.LookPath("kubectl")
	if err != nil {
		return 0, 0, fmt.Errorf("kubectl not found in PATH: %w", err)
	}
	cmd := exec.CommandContext(context.Background(), kubectlPath, "get", "applications", "-n", "argocd",
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
	kubectlPath, err := exec.LookPath("kubectl")
	if err != nil {
		out.Warning("kubectl not found in PATH: %v", err)
		return
	}
	out.Ln()
	out.Step(style.Config, "Application status:")
	cmd := exec.CommandContext(context.Background(), kubectlPath, "get", "applications", "-n", "argocd",
		"-o", "custom-columns=NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func printOutputs(result *infra.UpResult) {
	out.Ln()
	out.Success("Apply complete")
	if len(result.Outputs) == 0 {
		return
	}
	out.Ln()
	out.Step(style.Config, "Outputs:")
	for name, output := range result.Outputs {
		out.Infof("%s: %v", name, output)
	}
}
