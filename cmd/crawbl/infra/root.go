// Package infra provides the infra subcommand for Crawbl CLI.
// It manages Pulumi-based infrastructure deployment.
package infra

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cluster"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/edge"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/platform"
)

// buildConfig creates the full infra.Config from environment and CLI flags.
func buildConfig(env, region string) infra.Config {
	// Resolve HelmChartsDir: env var or ./helm relative to working directory
	helmChartsDir := os.Getenv("CRAWBL_HELM_CHARTS_DIR")
	if helmChartsDir == "" {
		if wd, err := os.Getwd(); err == nil {
			helmChartsDir = filepath.Join(wd, "helm")
		}
	}

	clusterConfig := cluster.DefaultClusterConfig(env, region)
	edgeConfig := edge.DefaultEdgeConfig()
	platformConfig := platform.DefaultPlatformConfig(helmChartsDir)

	// Cluster overrides
	if vpcID := os.Getenv("DIGITALOCEAN_VPC_ID"); vpcID != "" {
		clusterConfig.ManageVPC = false
		clusterConfig.ExistingVPCID = vpcID
	}
	if projectName := os.Getenv("DIGITALOCEAN_PROJECT_NAME"); projectName != "" {
		clusterConfig.ProjectName = projectName
	}

	// Edge overrides
	if zoneName := os.Getenv("CLOUDFLARE_ZONE_NAME"); zoneName != "" {
		edgeConfig.CloudflareZoneName = zoneName
	}
	if dnsRecord := os.Getenv("DNS_RECORD_NAME"); dnsRecord != "" {
		edgeConfig.DNSRecordName = dnsRecord
	}
	if acmeEmail := os.Getenv("ACME_EMAIL"); acmeEmail != "" {
		edgeConfig.ACMEMail = acmeEmail
	}

	// ESC environment name defaults to "crawbl/<env>"
	escEnv := os.Getenv("CRAWBL_ESC_ENV")
	if escEnv == "" {
		escEnv = "crawbl/" + env
	}

	return infra.Config{
		Environment:        env,
		Region:             region,
		ESCEnvironment:     escEnv,
		DigitalOceanToken:  os.Getenv("DIGITALOCEAN_TOKEN"),
		CloudflareAPIToken: os.Getenv("CLOUDFLARE_API_TOKEN"),
		OpenAIAPIKey:       os.Getenv("OPENAI_API_KEY"),
		ExistingVPCID:      os.Getenv("DIGITALOCEAN_VPC_ID"),
		HelmChartsDir:      helmChartsDir,
		ClusterConfig:      clusterConfig,
		PlatformConfig:     platformConfig,
		EdgeConfig:         edgeConfig,
	}
}

// NewInfraCommand creates the infra subcommand.
func NewInfraCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infra",
		Short: "Manage infrastructure with Pulumi",
		Long:  "Manage Crawbl infrastructure (Kubernetes cluster, platform services, edge routing) using Pulumi.",
		Example: `  crawbl infra init        # Initialize Pulumi stack
  crawbl infra plan        # Preview infrastructure changes
  crawbl infra apply       # Apply infrastructure changes
  crawbl infra destroy     # Destroy all infrastructure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(newPlanCommand())
	cmd.AddCommand(newApplyCommand())
	cmd.AddCommand(newDestroyCommand())

	return cmd
}