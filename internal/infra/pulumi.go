package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cloudflare"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cluster"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/databases"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/platform"
)

// NewStack creates or selects a Pulumi stack.
func NewStack(ctx context.Context, config Config) (*Stack, error) {
	stack, err := auto.UpsertStackInlineSource(ctx, config.Environment, "crawbl", buildProgram(config))
	if err != nil {
		return nil, fmt.Errorf("create stack: %w", err)
	}

	// ESC injects pulumiConfig (provider tokens) and environmentVariables automatically.
	if config.ESCEnvironment != "" {
		if err := stack.AddEnvironments(ctx, config.ESCEnvironment); err != nil {
			return nil, fmt.Errorf("add ESC environment %q: %w", config.ESCEnvironment, err)
		}
	}

	return &Stack{stack: stack, config: config}, nil
}

// buildProgram creates the Pulumi program.
func buildProgram(infraCfg Config) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		// Phase 1: Create cluster
		clusterResult, err := createCluster(ctx, infraCfg)
		if err != nil {
			return err
		}

		// Phase 2: Create Kubernetes provider
		k8sProvider, err := createKubernetesProvider(ctx, clusterResult)
		if err != nil {
			return err
		}

		// Phase 3: Create managed databases (prod only — nil config skips this phase)
		if infraCfg.DatabasesConfig != nil {
			if err := createDatabases(ctx, infraCfg, clusterResult); err != nil {
				return err
			}
		}

		// Phase 4: Create platform services (ArgoCD + repo bootstrap)
		if err := createPlatform(ctx, infraCfg, k8sProvider); err != nil {
			return err
		}

		// Phase 5: Create Cloudflare tunnel and DNS records (dev only — no-op when ManageTunnel=false)
		if err := createCloudflare(ctx, infraCfg); err != nil {
			return err
		}

		// Export outputs
		exportOutputs(ctx, clusterResult)
		return nil
	}
}

// createCluster provisions the DOKS cluster.
func createCluster(ctx *pulumi.Context, cfg Config) (*cluster.Cluster, error) {
	result, err := cluster.NewCluster(ctx, cfg.ClusterConfig.Name, cfg.ClusterConfig)
	if err != nil {
		return nil, fmt.Errorf("create cluster: %w", err)
	}
	return result, nil
}

// createKubernetesProvider creates a Kubernetes provider from cluster kubeconfig.
func createKubernetesProvider(ctx *pulumi.Context, clusterResult *cluster.Cluster) (*kubernetes.Provider, error) {
	provider, err := kubernetes.NewProvider(ctx, "k8s", &kubernetes.ProviderArgs{
		Kubeconfig: clusterResult.Outputs.Kubeconfig,
	})
	if err != nil {
		return nil, fmt.Errorf("create kubernetes provider: %w", err)
	}
	return provider, nil
}

// createDatabases provisions managed PostgreSQL, Valkey, and PgBouncer for prod.
func createDatabases(ctx *pulumi.Context, config Config, clusterResult *cluster.Cluster) error {
	_, err := databases.NewDatabases(ctx, "databases", *config.DatabasesConfig, clusterResult.Cluster.ID())
	if err != nil {
		return fmt.Errorf("create databases: %w", err)
	}
	return nil
}

// createPlatform provisions platform services.
func createPlatform(ctx *pulumi.Context, config Config, k8sProvider *kubernetes.Provider) error {
	platformConfig := config.PlatformConfig
	platformConfig.Provider = k8sProvider

	_, err := platform.NewPlatform(ctx, "platform", platformConfig, pulumi.Provider(k8sProvider))
	if err != nil {
		return fmt.Errorf("create platform: %w", err)
	}
	return nil
}

// createCloudflare provisions the Cloudflare tunnel, ingress config, and DNS records.
// It is a no-op when cfg.CloudflareConfig.ManageTunnel is false.
func createCloudflare(ctx *pulumi.Context, cfg Config) error {
	_, err := cloudflare.NewCloudflare(ctx, "cloudflare", cfg.CloudflareConfig)
	if err != nil {
		return fmt.Errorf("create cloudflare: %w", err)
	}
	return nil
}

// exportOutputs exports stack outputs.
func exportOutputs(ctx *pulumi.Context, clusterResult *cluster.Cluster) {
	ctx.Export("environment", clusterResult.Outputs.ClusterName)
	ctx.Export("clusterName", clusterResult.Outputs.ClusterName)
	ctx.Export("clusterEndpoint", clusterResult.Outputs.ClusterEndpoint)
	ctx.Export("kubeconfig", clusterResult.Outputs.Kubeconfig)

	ctx.Export("readme", clusterResult.Outputs.ClusterName.ApplyT(func(name string) string {
		return "# Crawbl Infrastructure\n\n" +
			"DOKS cluster **" + name + "** bootstrapped with ArgoCD.\n\n" +
			"All application workloads are managed by ArgoCD via the " +
			"[crawbl-argocd-apps](https://github.com/Crawbl-AI/crawbl-argocd-apps) repo.\n\n" +
			"## Components\n\n" +
			"| Layer | Managed By |\n" +
			"|-------|------------|\n" +
			"| DOKS cluster, VPC, Cloudflare tunnel | Pulumi (this stack) |\n" +
			"| ArgoCD Helm release + root Application | Pulumi (this stack) |\n" +
			"| All other K8s workloads | ArgoCD |\n"
	}).(pulumi.StringOutput))
}

// Preview runs a Pulumi preview.
func (s *Stack) Preview(ctx context.Context) (*PreviewResult, error) {
	result, err := s.stack.Preview(ctx, optpreview.ProgressStreams(os.Stdout))
	if err != nil {
		return nil, fmt.Errorf("preview failed: %w", err)
	}

	// Extract summary from preview - ChangeSummary is map[string]int
	summary := &PreviewResult{}
	for opType, count := range result.ChangeSummary {
		switch opType {
		case "create":
			summary.Adds = count
		case "update":
			summary.Updates = count
		case "delete":
			summary.Deletes = count
		case "same":
			summary.Same = count
		}
	}
	return summary, nil
}

// Up runs a Pulumi up to deploy infrastructure.
func (s *Stack) Up(ctx context.Context) (*UpResult, error) {
	result, err := s.stack.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("up failed: %w", err)
	}

	outputs := make(map[string]any)
	for k, v := range result.Outputs {
		outputs[k] = v.Value
	}
	return &UpResult{Outputs: outputs}, nil
}

// Destroy destroys all infrastructure in the stack.
func (s *Stack) Destroy(ctx context.Context) error {
	// PULUMI_K8S_DELETE_UNREACHABLE covers native K8s resources (Namespaces,
	// Secrets, CRDs) but NOT Helm releases — the Helm provider has its own
	// unreachable check. RemoveUnreachableK8sState handles the Helm case by
	// stripping those resources from state before destroy runs.
	os.Setenv("PULUMI_K8S_DELETE_UNREACHABLE", "true")
	_, err := s.stack.Destroy(ctx)
	return err
}

// RemoveUnreachableK8sState exports the Pulumi state, strips all kubernetes:*
// resources (including Helm releases that PULUMI_K8S_DELETE_UNREACHABLE cannot
// handle) and clears pending operations from interrupted deployments, then
// re-imports the cleaned state. Call this before Destroy when the K8s cluster
// is known to be unreachable.
func (s *Stack) RemoveUnreachableK8sState(ctx context.Context) error {
	state, err := s.stack.Export(ctx)
	if err != nil {
		return fmt.Errorf("export state: %w", err)
	}

	var deployment map[string]json.RawMessage
	if err := json.Unmarshal(state.Deployment, &deployment); err != nil {
		return fmt.Errorf("unmarshal state: %w", err)
	}

	// Remove all K8s-managed resources (Helm releases, CRDs, namespaces, secrets, etc.).
	if raw, ok := deployment["resources"]; ok {
		var resources []map[string]any
		if err := json.Unmarshal(raw, &resources); err != nil {
			return fmt.Errorf("unmarshal resources: %w", err)
		}
		filtered := make([]map[string]any, 0, len(resources))
		for _, r := range resources {
			t, _ := r["type"].(string)
			if strings.HasPrefix(t, "kubernetes:") {
				continue
			}
			filtered = append(filtered, r)
		}
		b, err := json.Marshal(filtered)
		if err != nil {
			return fmt.Errorf("marshal filtered resources: %w", err)
		}
		deployment["resources"] = b
	}

	// Clear pending operations from interrupted deployments.
	deployment["pending_operations"] = json.RawMessage("[]")

	cleaned, err := json.Marshal(deployment)
	if err != nil {
		return fmt.Errorf("marshal cleaned state: %w", err)
	}
	state.Deployment = cleaned

	return s.stack.Import(ctx, state)
}

// Outputs returns the stack outputs.
func (s *Stack) Outputs(ctx context.Context) (map[string]any, error) {
	outputs, err := s.stack.Outputs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get outputs: %w", err)
	}

	// Convert to map[string]interface{}
	result := make(map[string]any)
	for k, v := range outputs {
		result[k] = v.Value
	}
	return result, nil
}
