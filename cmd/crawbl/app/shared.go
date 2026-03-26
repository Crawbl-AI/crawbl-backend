// Package app provides shared utilities for app subcommands.
package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

const rolloutTimeout = 120

// deployOptions holds common deployment options.
type deployOptions struct {
	tag         string
	namespace   string
	helmRelease string
	infraDir    string
}

// getRootDir returns the git repository root directory.
func getRootDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// checkPrerequisites checks that required tools are installed and authenticated.
func checkPrerequisites() error {
	var missing []string

	tools := []string{"kubectl", "helm", "doctl"}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required tools: %s", strings.Join(missing, ", "))
	}

	cmd := exec.Command("doctl", "account", "get")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("not authenticated with DigitalOcean (run: doctl auth init)")
	}

	return nil
}

// verifyImageTagExists checks if a specific tag exists in a DOCR repository.
func verifyImageTagExists(repoName, tag string) error {
	cmd := exec.Command("doctl", "registry", "repository", "list-tags", repoName, "--format", "Tag")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list registry tags: %w", err)
	}

	// Check each line for the tag (skip header line)
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == tag {
			return nil
		}
	}

	return fmt.Errorf("image tag %s not found in registry", tag)
}

// helmUpgradeOptions holds options for Helm upgrade.
type helmUpgradeOptions struct {
	Release     string
	Namespace   string
	ImageTag    string
	ChartPath   string
	ExtraValues map[string]string
}

// runHelmUpgrade runs helm upgrade with the given options.
func runHelmUpgrade(ctx context.Context, opts helmUpgradeOptions) error {
	args := []string{
		"upgrade", opts.Release, opts.ChartPath,
		"--namespace", opts.Namespace,
		"--set", fmt.Sprintf("image.tag=%s", opts.ImageTag),
		"--install",
		"--wait",
		"--timeout", "5m",
	}

	// Add extra values
	for key, value := range opts.ExtraValues {
		args = append(args, "--set", fmt.Sprintf("%s=%s", key, value))
	}

	cmd := exec.CommandContext(ctx, "helm", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm upgrade failed: %w", err)
	}

	return nil
}

// waitForRollout waits for a deployment rollout to complete.
func waitForRollout(ctx context.Context, deployment, namespace string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "rollout", "status", fmt.Sprintf("deployment/%s", deployment),
		"--namespace", namespace,
		"--timeout", fmt.Sprintf("%ds", rolloutTimeout),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("deployment rollout failed: %w", err)
	}

	return nil
}

// checkDeploymentHealth checks if pods for a deployment are running.
func checkDeploymentHealth(ctx context.Context, namespace, helmRelease string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "get", "pods",
		"--namespace", namespace,
		"-l", fmt.Sprintf("app.kubernetes.io/instance=%s", helmRelease),
		"-o", "jsonpath={.items[*].status.phase}",
	)

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get pod status: %w", err)
	}

	if strings.Contains(string(output), "Running") {
		return nil
	}

	return fmt.Errorf("no running pods found for %s", helmRelease)
}

// addDeployFlags adds common deployment flags to a command.
func addDeployFlags(cmd *cobra.Command, opts *deployOptions, defaultNamespace, defaultRelease string) {
	cmd.Flags().StringVarP(&opts.tag, "tag", "t", "", "Image tag (required)")
	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", defaultNamespace, "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.helmRelease, "helm-release", defaultRelease, "Helm release name")
	cmd.Flags().StringVar(&opts.infraDir, "infra-dir", "", "Path to crawbl-infra directory (default: ./crawbl-infra from repo root)")

	_ = cmd.MarkFlagRequired("tag")
}

// getInfraDir returns the infra directory, defaulting to <repo-root>/crawbl-infra if not set.
func getInfraDir(rootDir, infraDir string) string {
	if infraDir == "" {
		return fmt.Sprintf("%s/crawbl-infra", rootDir)
	}
	return infraDir
}
