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
		if !confirmPrompt("Do you want to destroy all resources? This cannot be undone. (y/N): ") {
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

	// Before destroying the cluster, delete all LoadBalancer Services and PersistentVolumeClaims.
	// DigitalOcean's cloud controller creates LBs and volumes outside Pulumi's state
	// when K8s LoadBalancer Services or PVCs are provisioned.
	// If we destroy the cluster with these resources still attached, they become orphaned
	// in the DO account and must be cleaned up manually.
	out.Step(style.Delete, "Cleaning up LoadBalancer Services before cluster destroy...")
	if err := deleteLoadBalancerServices(ctx); err != nil {
		out.Warning("Failed to clean up LoadBalancers: %v", err)
		out.Warning("Proceeding anyway — check for orphaned load balancers in the DO console")
	}

	out.Step(style.Delete, "Cleaning up PersistentVolumeClaims before cluster destroy...")
	if err := deletePersistentVolumeClaims(ctx); err != nil {
		out.Warning("Failed to clean up PersistentVolumeClaims: %v", err)
		out.Warning("Proceeding anyway — check for orphaned volumes in the DO console")
	}

	if err := stack.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy failed: %w", err)
	}

	out.Ln()
	out.Success("Destroy complete")
	return nil
}

// buildK8sClient builds a Kubernetes clientset from the default kubeconfig
// (~/.kube/config or KUBECONFIG env var).
func buildK8sClient() (*kubernetes.Clientset, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create k8s client: %w", err)
	}
	return clientset, nil
}

// waitForResourceDeletion polls countFn until it returns 0 or the timeout expires.
// label is used only for log messages (e.g. "LoadBalancer(s)", "PVC(s)").
func waitForResourceDeletion(ctx context.Context, label string, countFn func(context.Context) (int, error), timeout, interval time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		n, err := countFn(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		out.Infof("waiting for %d %s to be removed", n, label)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s", label)
		case <-ticker.C:
		}
	}
}

// deleteLoadBalancerServices finds and deletes all LoadBalancer-type Services
// across all namespaces, then waits for the DO cloud controller to deprovision
// the associated load balancers before returning.
func deleteLoadBalancerServices(ctx context.Context) error {
	clientset, err := buildK8sClient()
	if err != nil {
		return err
	}
	svcs, err := clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}

	lbCount := deleteLoadBalancerServiceRows(ctx, clientset, svcs.Items)
	if lbCount == 0 {
		out.Infof("No LoadBalancer services found")
		return nil
	}

	out.Step(style.Waiting, "Waiting for %d LoadBalancer(s) to terminate...", lbCount)
	if err := waitForResourceDeletion(ctx, "LoadBalancer(s)", countLoadBalancerServices(clientset), 5*time.Minute, 5*time.Second); err != nil {
		return err
	}
	out.Success("All LoadBalancers terminated")
	return nil
}

// deleteLoadBalancerServiceRows iterates a Service list, deletes each
// LoadBalancer-typed entry, and returns the number of successful deletes.
func deleteLoadBalancerServiceRows(ctx context.Context, clientset *kubernetes.Clientset, items []corev1.Service) int {
	count := 0
	for i := range items {
		svc := &items[i]
		if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}
		out.Infof("Deleting LoadBalancer service %s/%s...", svc.Namespace, svc.Name)
		if err := clientset.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
			out.Warning("Failed to delete %s/%s: %v", svc.Namespace, svc.Name, err)
			continue
		}
		count++
	}
	return count
}

// countLoadBalancerServices returns a closure that counts the remaining
// LoadBalancer-type Services in the cluster — used as the termination
// predicate for waitForResourceDeletion.
func countLoadBalancerServices(clientset *kubernetes.Clientset) func(context.Context) (int, error) {
	return func(c context.Context) (int, error) {
		list, listErr := clientset.CoreV1().Services("").List(c, metav1.ListOptions{})
		if listErr != nil {
			return 0, fmt.Errorf("list services: %w", listErr)
		}
		count := 0
		for i := range list.Items {
			if list.Items[i].Spec.Type == corev1.ServiceTypeLoadBalancer {
				count++
			}
		}
		return count, nil
	}
}

// deletePersistentVolumeClaims finds and deletes all PVCs across all namespaces,
// then waits for the DO cloud controller to deprovision the associated volumes.
func deletePersistentVolumeClaims(ctx context.Context) error {
	clientset, err := buildK8sClient()
	if err != nil {
		return err
	}

	// List all PVCs across all namespaces.
	pvcs, err := clientset.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list PVCs: %w", err)
	}

	var pvcCount int
	for i := range pvcs.Items {
		pvc := &pvcs.Items[i]
		out.Infof("Deleting PersistentVolumeClaim %s/%s...", pvc.Namespace, pvc.Name)
		if err := clientset.CoreV1().PersistentVolumeClaims(pvc.Namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err != nil {
			out.Warning("Failed to delete PVC %s/%s: %v", pvc.Namespace, pvc.Name, err)
			continue
		}
		pvcCount++
	}

	if pvcCount == 0 {
		out.Infof("No PersistentVolumeClaims found")
		return nil
	}

	out.Step(style.Waiting, "Waiting for %d PVC(s) to terminate...", pvcCount)
	err = waitForResourceDeletion(ctx, "PVC(s)", func(c context.Context) (int, error) {
		list, listErr := clientset.CoreV1().PersistentVolumeClaims("").List(c, metav1.ListOptions{})
		if listErr != nil {
			return 0, fmt.Errorf("list PVCs: %w", listErr)
		}
		return len(list.Items), nil
	}, 5*time.Minute, 5*time.Second)
	if err != nil {
		return err
	}
	out.Success("All PVCs terminated")
	return nil
}
