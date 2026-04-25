// Package infra provides the infra subcommand for Crawbl CLI.
// It manages Pulumi-based infrastructure deployment.
package infra

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cloudflare"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cluster"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/databases"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/platform"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/runtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/config"
)

// loadStackSection reads a section from Pulumi.<env>.yaml into target.
func loadStackSection(env, key string, target any) error {
	if err := platform.LoadStackConfig(env, key, target); err != nil {
		return fmt.Errorf("load %s from Pulumi.%s.yaml: %w", key, env, err)
	}
	return nil
}

// buildConfig creates the full infra.Config from Pulumi stack config and environment variables.
// For env=dev, it delegates to buildRuntimeConfig. For all other environments, it uses the
// existing DOKS platform program.
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
		if data, err := os.ReadFile(keyPath); err == nil { // #nosec G304,G703 -- CLI tool, paths from developer config
			platformConfig.ArgoCDRepoSSHPrivateKey = string(data)
		}
	}
	if platformConfig.ArgoCDRepoSSHPrivateKey == "" {
		out.Warning("ARGOCD_SSH_PRIVATE_KEY and ARGOCD_SSH_KEY_PATH are both unset — repo secret will not be managed by Pulumi")
	}

	// Managed databases are prod-only. Attempt to load the config section;
	// when absent (dev stack), leave DatabasesConfig nil to skip provisioning.
	var dbsCfg *databases.Config
	var stackDBCfg databases.StackDatabasesConfig
	if err := loadStackSection(env, "crawbl:databases", &stackDBCfg); err == nil {
		cfg := databases.Config(stackDBCfg)
		dbsCfg = &cfg
	}

	// Cloudflare tunnel config — optional; absent key means ManageTunnel stays false.
	var cfConfig cloudflare.Config
	var stackCFCfg cloudflare.StackCloudflareConfig
	if err := loadStackSection(env, "crawbl:cloudflare", &stackCFCfg); err == nil {
		cfConfig = cloudflare.Config{
			ManageTunnel: stackCFCfg.ManageTunnel,
			AccountID:    stackCFCfg.AccountID,
			ZoneID:       stackCFCfg.ZoneID,
			TunnelName:   stackCFCfg.TunnelName,
			TunnelID:     stackCFCfg.TunnelID,
			EnvoyService: stackCFCfg.EnvoyService,
			Subdomains:   stackCFCfg.Subdomains,
			ZoneName:     stackCFCfg.ZoneName,
			// Tunnel secret is a runtime credential — never stored in YAML.
			TunnelSecret: os.Getenv("CLOUDFLARE_TUNNEL_SECRET"),
		}
	}

	return infra.Config{
		Environment:      env,
		Region:           region,
		ESCEnvironment:   config.StringOr("CRAWBL_ESC_ENV", "crawbl/"+env),
		ExistingVPCID:    os.Getenv("DIGITALOCEAN_VPC_ID"),
		ClusterConfig:    clusterConfig,
		PlatformConfig:   platformConfig,
		DatabasesConfig:  dbsCfg,
		CloudflareConfig: cfConfig,
	}, nil
}

// buildRuntimeConfig creates a runtime.RuntimeConfig from Pulumi stack config
// for the Hetzner k3s dev environment.
func buildRuntimeConfig(env, region string) (runtime.RuntimeConfig, error) {
	var runtimeCfg runtime.StackRuntimeConfig
	if err := loadStackSection(env, "crawbl:runtime", &runtimeCfg); err != nil {
		return runtime.RuntimeConfig{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return runtime.RuntimeConfig{}, fmt.Errorf("get working directory: %w", err)
	}
	helmValuesDir := filepath.Join(cwd, "config", "helm")
	platformCfg := platform.DefaultPlatformConfig(helmValuesDir)

	// ArgoCD deploy key — read from env var or file path
	if key := os.Getenv("ARGOCD_SSH_PRIVATE_KEY"); key != "" {
		platformCfg.ArgoCDRepoSSHPrivateKey = key
	} else if keyPath := os.Getenv("ARGOCD_SSH_KEY_PATH"); keyPath != "" {
		if data, err := os.ReadFile(keyPath); err == nil { // #nosec G304,G703 -- CLI tool, paths from developer config
			platformCfg.ArgoCDRepoSSHPrivateKey = string(data)
		}
	}
	if platformCfg.ArgoCDRepoSSHPrivateKey == "" {
		out.Warning("ARGOCD_SSH_PRIVATE_KEY and ARGOCD_SSH_KEY_PATH are both unset — repo secret will not be managed by Pulumi")
	}

	cfg := runtime.ConfigFromStack(env, region, runtimeCfg, platformCfg)

	// Resolve "auto" in SSHAllowedCIDRs to the current public IP.
	// This allows developers with dynamic IPs to simply set "auto" in
	// Pulumi.dev.yaml instead of hardcoding an IP that changes daily.
	cfg.Hetzner.SSHAllowedCIDRs = resolveAutoCIDRs(cfg.Hetzner.SSHAllowedCIDRs)
	cfg.Hetzner.K8sAPIAllowedCIDRs = resolveAutoCIDRs(cfg.Hetzner.K8sAPIAllowedCIDRs)

	// Cloudflare tunnel secret is a runtime credential — never stored in YAML.
	cfg.Cloudflare.TunnelSecret = os.Getenv("CLOUDFLARE_TUNNEL_SECRET")

	cfg.ESCEnvironment = config.StringOr("CRAWBL_ESC_ENV", "crawbl/"+env)

	return cfg, nil
}

// resolveAutoCIDRs replaces the literal string "auto" in a CIDR list with
// the current machine's public IP as a /32. This lets developers with
// dynamic IPs use sshAllowedCIDRs: ["auto"] in Pulumi.dev.yaml.
func resolveAutoCIDRs(cidrs []string) []string {
	resolved := make([]string, 0, len(cidrs))
	for _, c := range cidrs {
		if c == "auto" {
			ip := detectPublicIP()
			if ip != "" {
				out.Infof("Resolved 'auto' CIDR to %s/32", ip)
				resolved = append(resolved, ip+"/32")
			} else {
				out.Warning("Could not detect public IP for 'auto' CIDR — skipping")
			}
		} else {
			resolved = append(resolved, c)
		}
	}
	return resolved
}

// detectPublicIP returns the current machine's public IP via ifconfig.me.
func detectPublicIP() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ifconfig.me", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "curl/8.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
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
		Example: `  crawbl infra init                          # Initialize Pulumi stack
  crawbl infra plan                          # Preview infrastructure changes
  crawbl infra update                        # Apply infrastructure changes
  crawbl infra update --save-kubeconfig      # Apply + configure kubectl + wait for ArgoCD
  crawbl infra kubeconfig                    # Fetch kubeconfig without applying changes
  crawbl infra destroy                       # Destroy all infrastructure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(newPlanCommand())
	cmd.AddCommand(newUpdateCommand())
	cmd.AddCommand(newDestroyCommand())
	cmd.AddCommand(newKubeconfigCommand())

	return cmd
}
