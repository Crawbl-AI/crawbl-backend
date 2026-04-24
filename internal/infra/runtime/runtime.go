package runtime

import (
	"context"
	"fmt"
	"os"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cloudflare"
)

// BuildRuntimeProgram creates the Pulumi RunFunc that provisions the full
// Hetzner k3s runtime: server, firewall, k3s install, ArgoCD bootstrap,
// and Cloudflare tunnel/DNS.
func BuildRuntimeProgram(cfg RuntimeConfig) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		name := "crawbl-" + cfg.Environment

		// Phase 1: Build cloud-init from config.
		cloudInit := buildCloudInit(cfg)

		// Phase 2: Create firewall.
		fw, err := createFirewall(ctx, name, cfg)
		if err != nil {
			return err
		}

		// Phase 3: Create Hetzner server with cloud-init and firewall.
		server, err := createServer(ctx, name, cfg, fw, cloudInit)
		if err != nil {
			return err
		}

		// Phase 4: Build kubeconfig from server IP.
		kubeconfig := extractKubeconfig(ctx, name, server)

		// Phase 5: Create Kubernetes provider from kubeconfig.
		k8sProvider, err := kubernetes.NewProvider(ctx, "k8s", &kubernetes.ProviderArgs{
			Kubeconfig: kubeconfig,
		})
		if err != nil {
			return fmt.Errorf("create kubernetes provider: %w", err)
		}

		// Phase 6: Bootstrap ArgoCD using the k8s provider.
		if err := bootstrapArgoCD(ctx, name, cfg, k8sProvider); err != nil {
			return err
		}

		// Phase 7: Create Cloudflare tunnel and DNS records.
		if _, err := cloudflare.NewCloudflare(ctx, "cloudflare", cfg.Cloudflare); err != nil {
			return fmt.Errorf("create cloudflare: %w", err)
		}

		// Phase 8: Export outputs.
		ctx.Export("serverIP", server.Ipv4Address)
		ctx.Export("kubeconfig", kubeconfig)
		ctx.Export("clusterName", pulumi.String(name))

		return nil
	}
}

// NewStack creates or selects a Pulumi stack for the Hetzner k3s runtime.
func NewStack(ctx context.Context, cfg RuntimeConfig) (*Stack, error) {
	stack, err := auto.UpsertStackInlineSource(ctx, cfg.Environment, "crawbl-runtime", BuildRuntimeProgram(cfg))
	if err != nil {
		return nil, fmt.Errorf("create stack: %w", err)
	}

	if cfg.ESCEnvironment != "" {
		if err := stack.AddEnvironments(ctx, cfg.ESCEnvironment); err != nil {
			return nil, fmt.Errorf("add ESC environment %q: %w", cfg.ESCEnvironment, err)
		}
	}

	return &Stack{stack: stack, config: cfg}, nil
}

// Preview runs a Pulumi preview on the runtime stack.
func (s *Stack) Preview(ctx context.Context) (*PreviewResult, error) {
	result, err := s.stack.Preview(ctx, optpreview.ProgressStreams(os.Stdout))
	if err != nil {
		return nil, fmt.Errorf("preview failed: %w", err)
	}

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

// Up runs a Pulumi up to deploy the runtime stack.
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

// Destroy destroys all infrastructure in the runtime stack.
func (s *Stack) Destroy(ctx context.Context) error {
	_ = os.Setenv("PULUMI_K8S_DELETE_UNREACHABLE", "true")
	_, err := s.stack.Destroy(ctx)
	return err
}

// Outputs returns the runtime stack outputs.
func (s *Stack) Outputs(ctx context.Context) (map[string]any, error) {
	outputs, err := s.stack.Outputs(ctx)
	if err != nil {
		return nil, fmt.Errorf("get outputs: %w", err)
	}

	result := make(map[string]any)
	for k, v := range outputs {
		result[k] = v.Value
	}
	return result, nil
}

// PreviewResult contains preview summary information.
type PreviewResult struct {
	Adds    int
	Updates int
	Deletes int
	Same    int
}

// UpResult contains apply result information.
type UpResult struct {
	Outputs map[string]any
}
