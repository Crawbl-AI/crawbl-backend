package infra

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// newDestroyCommand creates the infra destroy subcommand.
func newDestroyCommand() *cobra.Command {
	var (
		env         string
		region      string
		autoApprove bool
	)

	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy all infrastructure resources",
		Long: `Destroy all infrastructure resources in the stack.

This command removes all resources managed by Pulumi for the specified environment.
Use with caution - this operation is irreversible.`,
		Example: `  crawbl infra destroy                    # Destroy with confirmation
  crawbl infra destroy --auto-approve     # Destroy without confirmation`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDestroy(cmd.Context(), env, region, autoApprove)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name, for example dev, staging, or prod")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region, for example fra1, nyc1, or sfo2")
	cmd.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "Skip confirmation prompts")

	return cmd
}

func runDestroy(ctx context.Context, env, region string, autoApprove bool) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	out.Step(style.Destroyed, "Destroying infrastructure for environment %q in region %q", env, region)
	out.Warning("This will permanently delete all resources")

	if !autoApprove {
		if !confirmDestroy() {
			out.Warning("Destroy cancelled")
			return nil
		}
	}

	config, err := buildConfig(env, region)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	// Before destroying the cluster, delete all LoadBalancer Services.
	// DigitalOcean's cloud controller creates LBs outside Pulumi's state
	// when K8s LoadBalancer Services are provisioned (e.g. by Envoy Gateway).
	// If we destroy the cluster with LBs still attached, they become orphaned
	// in the DO account and must be cleaned up manually.
	out.Step(style.Delete, "Cleaning up LoadBalancer Services before cluster destroy...")
	if err := deleteLoadBalancerServices(ctx); err != nil {
		out.Warning("Failed to clean up LoadBalancers: %v", err)
		out.Warning("Proceeding anyway — check for orphaned load balancers in the DO console")
	}

	if err := stack.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy failed: %w", err)
	}

	out.Ln()
	out.Success("Destroy complete")
	return nil
}

// deleteLoadBalancerServices finds and deletes all LoadBalancer-type Services
// across all namespaces, then waits for the DO cloud controller to deprovision
// the associated load balancers before returning.
func deleteLoadBalancerServices(ctx context.Context) error {
	// Build a K8s client from the default kubeconfig (~/.kube/config or KUBECONFIG).
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create k8s client: %w", err)
	}

	// List all Services across all namespaces.
	svcs, err := clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}

	var lbCount int
	for i := range svcs.Items {
		svc := &svcs.Items[i]
		if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}

		out.Infof("Deleting LoadBalancer service %s/%s...", svc.Namespace, svc.Name)
		if err := clientset.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
			out.Warning("Failed to delete %s/%s: %v", svc.Namespace, svc.Name, err)
			continue
		}
		lbCount++
	}

	if lbCount == 0 {
		out.Infof("No LoadBalancer services found")
		return nil
	}

	// Wait for DO cloud controller to deprovision LBs.
	out.Step(style.Waiting, "Waiting 30s for %d LoadBalancer(s) to deprovision...", lbCount)
	time.Sleep(30 * time.Second)
	return nil
}

func confirmDestroy() bool {
	out.Prompt(style.Warning, "Do you want to destroy all resources? This cannot be undone. (y/N): ")
	var response string
	fmt.Scanln(&response)
	return response == "y" || response == "Y"
}
