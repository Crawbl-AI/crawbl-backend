package runtime

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
)

// BuildRuntimeProgram creates the Pulumi RunFunc that provisions the full
// Hetzner k3s runtime: server, firewall, k3s install, ArgoCD bootstrap,
// and Cloudflare tunnel/DNS.
//
// The SSH private key is loaded eagerly (before the Pulumi engine runs) so
// the program fails fast with a clear error instead of mid-deployment.
func BuildRuntimeProgram(cfg RuntimeConfig) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		name := "crawbl-" + cfg.Environment

		// Load SSH key eagerly — needed for remote kubeconfig extraction.
		sshKey, err := loadSSHPrivateKey()
		if err != nil {
			return fmt.Errorf("load ssh key: %w", err)
		}

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

		// Phase 4: SSH into the server, wait for k3s, extract real kubeconfig.
		kubeconfig, err := extractKubeconfig(ctx, name, server, sshKey)
		if err != nil {
			return err
		}

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
// It first strips K8s resources from Pulumi state (Helm releases and
// namespaces become unreachable when the server is deleted), then
// destroys the remaining cloud resources.
func (s *Stack) Destroy(ctx context.Context) error {
	_ = os.Setenv("PULUMI_K8S_DELETE_UNREACHABLE", "true")
	if err := s.RemoveUnreachableK8sState(ctx); err != nil {
		return fmt.Errorf("clean pulumi state: %w", err)
	}
	_, err := s.stack.Destroy(ctx)
	return err
}

// RemoveUnreachableK8sState exports the Pulumi state, strips all
// kubernetes:* and command:remote:* resources, and clears pending
// operations. This allows Destroy to proceed when the K8s cluster or
// SSH connection is unreachable.
func (s *Stack) RemoveUnreachableK8sState(ctx context.Context) error {
	state, err := s.stack.Export(ctx)
	if err != nil {
		return fmt.Errorf("export state: %w", err)
	}

	var deployment map[string]json.RawMessage
	if err := json.Unmarshal(state.Deployment, &deployment); err != nil {
		return fmt.Errorf("unmarshal state: %w", err)
	}

	if raw, ok := deployment["resources"]; ok {
		var resources []map[string]any
		if err := json.Unmarshal(raw, &resources); err != nil {
			return fmt.Errorf("unmarshal resources: %w", err)
		}
		// Collect URNs of resources we're removing so we can also remove
		// any providers that depend on them (e.g. k8s provider depends on
		// the remote command that extracts kubeconfig).
		removedURNs := make(map[string]bool)
		filtered := make([]map[string]any, 0, len(resources))
		for _, r := range resources {
			t, _ := r["type"].(string)
			if strings.HasPrefix(t, "kubernetes:") || strings.HasPrefix(t, "command:remote:") {
				if urn, ok := r["urn"].(string); ok {
					removedURNs[urn] = true
				}
				continue
			}
			filtered = append(filtered, r)
		}
		// Second pass: remove providers whose dependencies reference removed resources.
		final := make([]map[string]any, 0, len(filtered))
		for _, r := range filtered {
			if deps, ok := r["dependencies"].([]any); ok {
				orphaned := false
				for _, d := range deps {
					if ds, ok := d.(string); ok && removedURNs[ds] {
						orphaned = true
						break
					}
				}
				if orphaned {
					continue
				}
			}
			final = append(final, r)
		}
		b, err := json.Marshal(final)
		if err != nil {
			return fmt.Errorf("marshal filtered resources: %w", err)
		}
		deployment["resources"] = b
	}

	deployment["pending_operations"] = json.RawMessage("[]")

	cleaned, err := json.Marshal(deployment)
	if err != nil {
		return fmt.Errorf("marshal cleaned state: %w", err)
	}
	state.Deployment = cleaned

	return s.stack.Import(ctx, state)
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
