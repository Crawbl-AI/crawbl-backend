package infra

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
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
		Short: "Bootstrap cluster end-to-end",
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

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name (dev, staging, prod)")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region (fra1, nyc1, sfo2)")
	cmd.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "Skip confirmation prompts")
	cmd.Flags().StringVar(&clusterName, "cluster", "crawbl-dev", "DOKS cluster name for kubeconfig")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "Timeout waiting for apps to sync")

	return cmd
}

func runBootstrap(ctx context.Context, env, region string, autoApprove bool, clusterName string, timeout time.Duration) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	fmt.Println("=== Crawbl Cluster Bootstrap ===")
	fmt.Printf("Environment: %s | Region: %s | Cluster: %s\n\n", env, region, clusterName)

	if !autoApprove {
		if !confirmUpdate() {
			fmt.Println("Bootstrap cancelled")
			return nil
		}
	}

	// Step 1: Run Pulumi up
	fmt.Println("\n[1/5] Applying infrastructure (Pulumi)...")
	if err := pulumiUp(ctx, env, region); err != nil {
		return fmt.Errorf("pulumi up failed: %w", err)
	}
	fmt.Println("  ok")

	// Step 2: Save kubeconfig
	fmt.Println("\n[2/5] Saving kubeconfig...")
	if err := runCommand("doctl", "kubernetes", "cluster", "kubeconfig", "save", clusterName); err != nil {
		return fmt.Errorf("kubeconfig save failed: %w", err)
	}
	fmt.Println("  ok")

	// Step 3: Ensure DOCR registry integration (Pulumi sets registryIntegration=true
	// on the cluster, but doctl registry add is a safety net in case of state drift).
	fmt.Println("\n[3/5] Ensuring DOCR registry integration...")
	if err := runCommand("doctl", "kubernetes", "cluster", "registry", "add", clusterName); err != nil {
		fmt.Printf("  warning: registry add failed (may already be integrated): %v\n", err)
	}
	fmt.Println("  ok")

	// Step 4: Wait for controller to be ready
	fmt.Println("\n[4/5] Waiting for ArgoCD application-controller...")
	if err := waitForController(ctx, timeout); err != nil {
		return fmt.Errorf("controller readiness failed: %w", err)
	}
	fmt.Println("  ok")

	// Step 5: Wait for all apps to sync
	fmt.Println("\n[5/5] Waiting for all applications to sync...")
	if err := waitForAppsSync(ctx, timeout); err != nil {
		return fmt.Errorf("app sync wait failed: %w", err)
	}
	fmt.Println("  ok")

	// Print final status
	fmt.Println("\n=== Bootstrap Complete ===")
	printAppStatus()
	return nil
}

// waitForController waits for the ArgoCD application-controller StatefulSet to be ready.
func waitForController(ctx context.Context, timeout time.Duration) error {
	return runCommand("kubectl", "rollout", "status",
		"statefulset/argocd-application-controller",
		"-n", "argocd",
		"--timeout", timeout.String(),
	)
}

// waitForAppsSync polls ArgoCD applications until all are Synced/Healthy or timeout.
func waitForAppsSync(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for apps to sync", timeout)
		}

		synced, total, err := checkAppSyncStatus()
		if err != nil {
			fmt.Printf("  waiting for applications to appear... (%v)\n", err)
			time.Sleep(10 * time.Second)
			continue
		}

		fmt.Printf("  %d/%d applications synced\n", synced, total)

		if total > 0 && synced == total {
			return nil
		}

		time.Sleep(15 * time.Second)
	}
}

// checkAppSyncStatus returns (synced count, total count, error).
func checkAppSyncStatus() (int, int, error) {
	cmd := exec.Command("kubectl", "get", "applications", "-n", "argocd",
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
	fmt.Println("\nApplication Status:")
	cmd := exec.Command("kubectl", "get", "applications", "-n", "argocd",
		"-o", "custom-columns=NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status,MESSAGE:.status.conditions[0].message")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

// runCommand executes a command with stdout/stderr connected to the terminal.
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
