// Package infra provides the infra subcommand for Crawbl CLI.
// It manages Pulumi-based infrastructure deployment.
package infra

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cluster"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/platform"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/configenv"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/yamlvalues"
)

// loadStackSection reads a section from Pulumi.<env>.yaml into target.
func loadStackSection(env, key string, target any) error {
	if err := yamlvalues.LoadStackConfig(env, key, target); err != nil {
		return fmt.Errorf("load %s from Pulumi.%s.yaml: %w", key, env, err)
	}
	return nil
}

// buildConfig creates the full infra.Config from Pulumi stack config and environment variables.
func buildConfig(env, region string) (infra.Config, error) {
	// Load config sections from Pulumi.<env>.yaml
	var clusterCfg cluster.StackClusterConfig
	if err := loadStackSection(env, "crawbl:cluster", &clusterCfg); err != nil {
		return infra.Config{}, err
	}

	clusterConfig := cluster.ConfigFromStack(env, region, clusterCfg)
	cwd, err := os.Getwd()
	if err != nil {
		return infra.Config{}, fmt.Errorf("get working directory: %w", err)
	}
	helmValuesDir := filepath.Join(cwd, "config", "helm")
	platformConfig := platform.DefaultPlatformConfig(helmValuesDir)

	// Environment variable overrides (secrets and runtime values not stored in YAML)
	if vpcID := os.Getenv("DIGITALOCEAN_VPC_ID"); vpcID != "" {
		clusterConfig.ManageVPC = false
		clusterConfig.ExistingVPCID = vpcID
	}
	if projectName := os.Getenv("DIGITALOCEAN_PROJECT_NAME"); projectName != "" {
		clusterConfig.ProjectName = projectName
	}

	// ArgoCD deploy key — read from env var or file path
	if key := os.Getenv("ARGOCD_SSH_PRIVATE_KEY"); key != "" {
		platformConfig.ArgoCDRepoSSHPrivateKey = key
	} else if keyPath := os.Getenv("ARGOCD_SSH_KEY_PATH"); keyPath != "" {
		if data, err := os.ReadFile(keyPath); err == nil {
			platformConfig.ArgoCDRepoSSHPrivateKey = string(data)
		}
	}
	if platformConfig.ArgoCDRepoSSHPrivateKey == "" {
		out.Warning("ARGOCD_SSH_PRIVATE_KEY and ARGOCD_SSH_KEY_PATH are both unset — repo secret will not be managed by Pulumi")
	}

	return infra.Config{
		Environment:    env,
		Region:         region,
		ESCEnvironment: configenv.StringOr("CRAWBL_ESC_ENV", "crawbl/"+env),
		ExistingVPCID:  os.Getenv("DIGITALOCEAN_VPC_ID"),
		ClusterConfig:  clusterConfig,
		PlatformConfig: platformConfig,
	}, nil
}

// confirmPrompt prints prompt and returns true only when the user types "y" or "Y".
func confirmPrompt(prompt string) bool {
	out.Prompt(style.Warning, "%s", prompt)
	var response string
	_, _ = fmt.Scanln(&response)
	return response == "y" || response == "Y"
}

// NewInfraCommand creates the infra subcommand.
func NewInfraCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infra",
		Short: "Manage cloud infrastructure with Pulumi",
		Long:  "Manage Crawbl infrastructure (Kubernetes cluster, platform services, ArgoCD bootstrap) using Pulumi.",
		Example: `  crawbl infra init        # Initialize Pulumi stack
  crawbl infra plan        # Preview infrastructure changes
  crawbl infra update      # Apply infrastructure changes
  crawbl infra destroy     # Destroy all infrastructure
  crawbl infra bootstrap   # Bootstrap cluster end-to-end`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(newPlanCommand())
	cmd.AddCommand(newUpdateCommand())
	cmd.AddCommand(newDestroyCommand())
	cmd.AddCommand(newBootstrapCommand())

	return cmd
}
