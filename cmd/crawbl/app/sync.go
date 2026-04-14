package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

func newSyncCommand() *cobra.Command {
	var (
		force bool
		all   bool
	)

	cmd := &cobra.Command{
		Use:   "sync [app-name]",
		Short: "Sync ArgoCD applications",
		Long:  "Trigger a sync or hard refresh for one or all ArgoCD applications. Use --force to reset a stuck operation before syncing.",
		Example: `  crawbl app sync platform                 # Hard-refresh a specific app
  crawbl app sync platform --force         # Reset stuck operation + hard refresh
  crawbl app sync --all                    # Hard-refresh all apps in argocd namespace
  crawbl app sync --all --force            # Reset stuck operations on all apps`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if all && len(args) > 0 {
				return fmt.Errorf("cannot specify both --all and an app name")
			}
			if !all && len(args) == 0 {
				return cmd.Help()
			}

			if all {
				apps, err := listArgoCDApps(ctx)
				if err != nil {
					return err
				}
				if len(apps) == 0 {
					out.Warning("no ArgoCD apps found in argocd namespace")
					return nil
				}
				for _, app := range apps {
					if err := syncApp(ctx, app, force); err != nil {
						out.Fail("failed to sync %s: %v", app, err)
					}
				}
				return nil
			}

			return syncApp(ctx, args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Reset stuck operation and hard-refresh before syncing")
	cmd.Flags().BoolVar(&all, "all", false, "Sync all apps in the argocd namespace")
	return cmd
}

// syncApp performs a hard refresh (and optional stuck-operation reset) for one ArgoCD app.
func syncApp(ctx context.Context, appName string, force bool) error {
	if force {
		out.Step(style.Running, "Resetting stuck operation on %s", appName)
		patch := `[{"op":"remove","path":"/operation"}]`
		cmd := exec.CommandContext(ctx, "kubectl", "patch", "app", appName, // #nosec G204 -- CLI tool, input from developer
			"-n", "argocd", "--type", "json", "-p", patch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// Ignore errors here — the operation field may not exist, which is fine.
		_ = cmd.Run()
	}

	out.Step(style.Deploy, "Triggering hard refresh on %s", appName)
	annotation := `{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"hard"}}}`
	cmd := exec.CommandContext(ctx, "kubectl", "patch", "app", appName, // #nosec G204 -- CLI tool, input from developer
		"-n", "argocd", "--type", "merge", "-p", annotation)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hard refresh patch failed for %s: %w", appName, err)
	}

	out.Success("Synced %s", appName)
	return nil
}

// listArgoCDApps returns the names of all ArgoCD Application resources in the argocd namespace.
func listArgoCDApps(ctx context.Context) ([]string, error) {
	out.Step(style.Running, "Listing ArgoCD apps in argocd namespace")
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", "get", "app",
		"-n", "argocd", "-o", "jsonpath={.items[*].metadata.name}")
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("kubectl get app failed: %w", err)
	}

	raw := strings.TrimSpace(buf.String())
	if raw == "" {
		return nil, nil
	}

	// jsonpath {.items[*].metadata.name} returns space-separated names.
	return strings.Fields(raw), nil
}
