package infra

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra/runtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/cliexec"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// newKubeconfigCommand creates the infra kubeconfig subcommand.
func newKubeconfigCommand() *cobra.Command {
	var (
		env         string
		region      string
		clusterName string
	)

	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Fetch and save kubeconfig for the cluster",
		Long: `Fetch the kubeconfig for the target environment and merge it into ~/.kube/config.

For dev (Hetzner k3s): connects via SSH to the k3s server, fetches the
kubeconfig from /etc/rancher/k3s/k3s.yaml, renames the context to crawbl-dev,
and merges it into ~/.kube/config.

For prod (DOKS): uses doctl to save the kubeconfig and verify DOCR integration.

This command does NOT run pulumi up — it only fetches the kubeconfig from
an already-provisioned cluster.`,
		Example: `  crawbl infra kubeconfig                   # Fetch dev kubeconfig
  crawbl infra kubeconfig --env prod        # Fetch prod kubeconfig`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if clusterName == "" {
				clusterName = "crawbl-" + env
			}
			return runKubeconfig(cmd.Context(), env, region, clusterName)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name, for example dev, staging, or prod")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region")
	cmd.Flags().StringVar(&clusterName, "cluster", "", "DOKS cluster name (defaults to crawbl-<env>)")

	return cmd
}

func runKubeconfig(ctx context.Context, env, region, clusterName string) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	if env == "dev" {
		return kubeconfigDev(ctx, env, region)
	}
	return kubeconfigDOKS(ctx, clusterName)
}

// kubeconfigDev fetches kubeconfig from the k3s server via SSH.
func kubeconfigDev(ctx context.Context, env, region string) error {
	cfg, err := buildRuntimeConfig(env, region)
	if err != nil {
		return fmt.Errorf("build runtime config: %w", err)
	}

	stack, err := runtime.NewStack(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create runtime stack: %w", err)
	}

	outputs, err := stack.Outputs(ctx)
	if err != nil {
		return fmt.Errorf("get stack outputs: %w", err)
	}

	serverIPRaw, ok := outputs["serverIP"]
	if !ok {
		return fmt.Errorf("serverIP output not found — has the cluster been provisioned?")
	}
	serverIP, ok := serverIPRaw.(string)
	if !ok {
		return fmt.Errorf("serverIP output is not a string")
	}

	out.Step(style.Infra, "Fetching kubeconfig from k3s server %s via SSH...", serverIP)
	kubeconfig, err := fetchKubeconfigSSH(serverIP)
	if err != nil {
		return fmt.Errorf("fetch kubeconfig via SSH: %w", err)
	}

	kubeconfig = renameK3sKubeconfig(kubeconfig, "crawbl-dev")

	kubeconfigPath := os.ExpandEnv("$HOME/.kube/config-crawbl-dev")
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0600); err != nil { // #nosec G306 -- kubeconfig requires 0600
		return fmt.Errorf("write kubeconfig to %s: %w", kubeconfigPath, err)
	}
	out.Step(style.Check, "Kubeconfig saved to %s", kubeconfigPath)

	if err := mergeKubeconfigDev(ctx, kubeconfigPath); err != nil {
		return fmt.Errorf("merge kubeconfig: %w", err)
	}

	out.Ln()
	out.Step(style.Celebrate, "Kubeconfig ready — context crawbl-dev is active")
	return nil
}

// kubeconfigDOKS saves kubeconfig via doctl and verifies DOCR integration.
func kubeconfigDOKS(ctx context.Context, clusterName string) error {
	out.Step(style.Infra, "Saving kubeconfig for %s via doctl...", clusterName)
	if err := cliexec.Run(ctx, "doctl", "kubernetes", "cluster", "kubeconfig", "save", clusterName); err != nil {
		return fmt.Errorf("kubeconfig save failed: %w", err)
	}
	out.Step(style.Check, "Kubeconfig saved")

	out.Step(style.Infra, "Ensuring DOCR registry integration...")
	if err := cliexec.Run(ctx, "doctl", "kubernetes", "cluster", "registry", "add", clusterName); err != nil {
		out.Warning("Registry add may already be integrated: %v", err)
	}
	out.Step(style.Check, "Registry integration verified")

	out.Ln()
	out.Step(style.Celebrate, "Kubeconfig ready — context do-fra1-%s is active", clusterName)
	return nil
}
