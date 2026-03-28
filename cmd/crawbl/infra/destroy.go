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
		Short: "Destroy infrastructure",
		Long: `Destroy all infrastructure resources in the stack.

This command removes all resources managed by Pulumi for the specified environment.
Use with caution - this operation is irreversible.`,
		Example: `  crawbl infra destroy                    # Destroy with confirmation
  crawbl infra destroy --auto-approve     # Destroy without confirmation`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDestroy(cmd.Context(), env, region, autoApprove)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name (dev, staging, prod)")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region (fra1, nyc1, sfo2)")
	cmd.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "Skip confirmation prompts")

	return cmd
}

func runDestroy(ctx context.Context, env, region string, autoApprove bool) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	fmt.Printf("Destroying infrastructure for environment '%s' in region '%s'\n", env, region)
	fmt.Println("WARNING: This will permanently delete all resources!")

	if !autoApprove {
		if !confirmDestroy() {
			fmt.Println("Destroy cancelled")
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
	fmt.Println("Cleaning up LoadBalancer Services before cluster destroy...")
	if err := deleteLoadBalancerServices(ctx); err != nil {
		fmt.Printf("Warning: failed to clean up LoadBalancers: %v\n", err)
		fmt.Println("Proceeding with destroy anyway — check for orphaned LBs in DO console.")
	}

	if err := stack.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy failed: %w", err)
	}

	fmt.Println("\n✓ Destroy complete")
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

		fmt.Printf("  Deleting LoadBalancer service %s/%s...\n", svc.Namespace, svc.Name)
		if err := clientset.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("  Warning: failed to delete %s/%s: %v\n", svc.Namespace, svc.Name, err)
			continue
		}
		lbCount++
	}

	if lbCount == 0 {
		fmt.Println("  No LoadBalancer services found.")
		return nil
	}

	// Wait for DO cloud controller to deprovision LBs.
	fmt.Printf("  Waiting 30s for %d LoadBalancer(s) to deprovision...\n", lbCount)
	time.Sleep(30 * time.Second)
	return nil
}

func confirmDestroy() bool {
	fmt.Print("Do you want to destroy all resources? This cannot be undone. (y/N): ")
	var response string
	fmt.Scanln(&response)
	return response == "y" || response == "Y"
}
