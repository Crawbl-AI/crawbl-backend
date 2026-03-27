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
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/yamlvalues"
)

// loadStackSection reads a section from Pulumi.<env>.yaml into target.
func loadStackSection(env, key string, target interface{}) {
	if err := yamlvalues.LoadStackConfig(env, key, target); err != nil {
		panic(fmt.Sprintf("load %s from Pulumi.%s.yaml: %v", key, env, err))
	}
}

// envOrDefault returns the environment variable value or a fallback.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// buildConfig creates the full infra.Config from Pulumi stack config and environment variables.
func buildConfig(env, region string) infra.Config {
	// Load config sections from Pulumi.<env>.yaml
	var clusterCfg cluster.StackClusterConfig
	loadStackSection(env, "crawbl:cluster", &clusterCfg)

	clusterConfig := cluster.ConfigFromStack(env, region, clusterCfg)
	helmValuesDir := filepath.Join(must(os.Getwd()), "config", "helm")
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

	return infra.Config{
		Environment:        env,
		Region:             region,
		ESCEnvironment: envOrDefault("CRAWBL_ESC_ENV", "crawbl/"+env),
		ExistingVPCID:  os.Getenv("DIGITALOCEAN_VPC_ID"),
		ClusterConfig:  clusterConfig,
		PlatformConfig: platformConfig,
	}
}

func must(s string, err error) string {
	if err != nil {
		return "."
	}
	return s
}

// NewInfraCommand creates the infra subcommand.
func NewInfraCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infra",
		Short: "Manage infrastructure with Pulumi",
		Long:  "Manage Crawbl infrastructure (Kubernetes cluster, platform services, ArgoCD bootstrap) using Pulumi.",
		Example: `  crawbl infra init        # Initialize Pulumi stack
  crawbl infra plan        # Preview infrastructure changes
  crawbl infra update       # Apply infrastructure changes
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
