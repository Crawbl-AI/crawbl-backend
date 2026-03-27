// Package infra provides Pulumi-based infrastructure management for Crawbl.
package infra

import (
	"context"
	"fmt"
	"os"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cluster"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/platform"
)

// Config holds all infrastructure configuration.
type Config struct {
	Environment        string
	Region             string
	ESCEnvironment     string // Pulumi ESC environment (e.g. "crawbl/dev")
	DigitalOceanToken  string
	CloudflareAPIToken string
	OpenAIAPIKey       string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSRegion          string
	ExistingVPCID  string
	ClusterConfig  cluster.Config
	PlatformConfig platform.Config
}

// Stack represents a Pulumi stack.
type Stack struct {
	stack  auto.Stack
	config Config
}

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

		// Phase 3: Create platform services
		if err := createPlatform(ctx, infraCfg, k8sProvider); err != nil {
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

// createPlatform provisions platform services.
func createPlatform(ctx *pulumi.Context, config Config, k8sProvider *kubernetes.Provider) error {
	platformConfig := config.PlatformConfig
	platformConfig.Provider = k8sProvider
	platformConfig.DigitalOceanToken = config.DigitalOceanToken
	platformConfig.CloudflareAPIToken = config.CloudflareAPIToken
	platformConfig.OpenAIAPIKey = config.OpenAIAPIKey
	platformConfig.AWSAccessKeyID = config.AWSAccessKeyID
	platformConfig.AWSSecretAccessKey = config.AWSSecretAccessKey
	platformConfig.AWSRegion = config.AWSRegion

	_, err := platform.NewPlatform(ctx, "platform", platformConfig, pulumi.Provider(k8sProvider))
	if err != nil {
		return fmt.Errorf("create platform: %w", err)
	}
	return nil
}

// exportOutputs exports stack outputs.
func exportOutputs(ctx *pulumi.Context, clusterResult *cluster.Cluster) {
	ctx.Export("environment", clusterResult.Outputs.ClusterName)
	ctx.Export("clusterName", clusterResult.Outputs.ClusterName)
	ctx.Export("clusterEndpoint", clusterResult.Outputs.ClusterEndpoint)
	ctx.Export("kubeconfig", clusterResult.Outputs.Kubeconfig)
}

// PreviewResult contains preview summary information.
type PreviewResult struct {
	Adds    int
	Updates int
	Deletes int
	Same    int
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

// UpResult contains apply result information.
type UpResult struct {
	Outputs map[string]interface{}
}

// Up runs a Pulumi up to deploy infrastructure.
func (s *Stack) Up(ctx context.Context) (*UpResult, error) {
	result, err := s.stack.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("up failed: %w", err)
	}

	outputs := make(map[string]interface{})
	for k, v := range result.Outputs {
		outputs[k] = v.Value
	}
	return &UpResult{Outputs: outputs}, nil
}

// Destroy destroys all infrastructure in the stack.
func (s *Stack) Destroy(ctx context.Context) error {
	_, err := s.stack.Destroy(ctx)
	return err
}

// Outputs returns the stack outputs.
func (s *Stack) Outputs(ctx context.Context) (map[string]interface{}, error) {
	outputs, err := s.stack.Outputs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get outputs: %w", err)
	}

	// Convert to map[string]interface{}
	result := make(map[string]interface{})
	for k, v := range outputs {
		result[k] = v.Value
	}
	return result, nil
}