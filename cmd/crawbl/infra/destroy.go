package infra

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/runtime"
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

	if env == "dev" {
		return destroyRuntime(ctx, env, region)
	}
	return destroyDOKS(ctx, env, region)
}

// destroyRuntime destroys the Hetzner k3s runtime stack for the dev environment.
// No DO-specific post-destroy cleanup is needed.
func destroyRuntime(ctx context.Context, env, region string) error {
	cfg, err := buildRuntimeConfig(env, region)
	if err != nil {
		return fmt.Errorf("build runtime config: %w", err)
	}

	stack, err := runtime.NewStack(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create runtime stack: %w", err)
	}

	if err := stack.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy failed: %w", err)
	}

	out.Ln()
	out.Success("Destroy complete")
	return nil
}

// destroyDOKS destroys the DOKS platform stack for non-dev environments.
func destroyDOKS(ctx context.Context, env, region string) error {
	config, err := buildConfig(env, region)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	// Best-effort in-cluster cleanup: prevent orphaned DO LBs and Volumes.
	runInClusterCleanup(ctx)

	// Always strip K8s resources from Pulumi state before destroy.
	// Pulumi's K8s provider uses its own stored kubeconfig which may be stale,
	// and the Helm provider cannot uninstall releases on unreachable clusters
	// regardless of PULUMI_K8S_DELETE_UNREACHABLE.
	out.Step(style.Delete, "Cleaning K8s resources from Pulumi state...")
	if err := stack.RemoveUnreachableK8sState(ctx); err != nil {
		return fmt.Errorf("clean pulumi state: %w", err)
	}

	if err := stack.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy failed: %w", err)
	}

	// Post-destroy cleanup of cloud resources that live outside Pulumi state.
	// Skipped for prod — those resources are managed separately.
	if env != "prod" {
		runPostDestroyCloudCleanup(ctx, region)
	}

	out.Ln()
	out.Success("Destroy complete")
	return nil
}

// runInClusterCleanup performs best-effort cleanup of in-cluster resources
// (ArgoCD, LoadBalancers, PVCs) before Pulumi destroy. Failures are logged
// as warnings rather than returned so that destroy proceeds regardless.
func runInClusterCleanup(ctx context.Context) {
	if !isClusterReachable(ctx) {
		out.Warning("Cluster is unreachable — skipping in-cluster cleanup")
		out.Warning("Check the DO console for orphaned load balancers and volumes")
		return
	}

	out.Step(style.Delete, "Suspending ArgoCD to prevent resource reconciliation...")
	if err := suspendArgoCD(ctx); err != nil {
		out.Warning("Failed to suspend ArgoCD: %v", err)
	}

	out.Step(style.Delete, "Cleaning up LoadBalancer Services...")
	if err := deleteLoadBalancerServices(ctx); err != nil {
		out.Warning("Failed to clean up LoadBalancers: %v", err)
		out.Warning("Check for orphaned load balancers in the DO console")
	}

	out.Step(style.Delete, "Cleaning up PersistentVolumeClaims...")
	if err := deletePersistentVolumeClaims(ctx); err != nil {
		out.Warning("Failed to clean up PVCs: %v", err)
		out.Warning("Check for orphaned volumes in the DO console")
	}
}

// runPostDestroyCloudCleanup removes orphaned DigitalOcean cloud resources
// (block volumes, snapshots, Spaces buckets) that live outside Pulumi state.
// Failures are logged as warnings so that all cleanup steps run.
func runPostDestroyCloudCleanup(ctx context.Context, region string) {
	out.Step(style.Delete, "Cleaning up orphaned DigitalOcean volumes...")
	if err := deleteOrphanedDOVolumes(ctx, region); err != nil {
		out.Warning("Failed to clean up volumes: %v", err)
	}

	out.Step(style.Delete, "Cleaning up orphaned volume snapshots...")
	if err := deleteOrphanedDOSnapshots(ctx); err != nil {
		out.Warning("Failed to clean up snapshots: %v", err)
	}

	out.Step(style.Delete, "Cleaning up Spaces object storage...")
	if err := deleteOrphanedDOSpaces(ctx, region); err != nil {
		out.Warning("Failed to clean up Spaces: %v", err)
	}
}

// isClusterReachable returns true if the K8s API server responds to a lightweight request.
func isClusterReachable(ctx context.Context) bool {
	clientset, err := buildK8sClient()
	if err != nil {
		return false
	}
	_, err = clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	return err == nil
}

// suspendArgoCD scales the ArgoCD application controller to zero replicas
// and waits for the pod to terminate so it cannot reconcile deleted resources.
func suspendArgoCD(ctx context.Context) error {
	clientset, err := buildK8sClient()
	if err != nil {
		return err
	}

	zero := int32(0)
	scale, err := clientset.AppsV1().StatefulSets("argocd").GetScale(ctx, "argocd-application-controller", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get application-controller scale: %w", err)
	}
	scale.Spec.Replicas = zero
	_, err = clientset.AppsV1().StatefulSets("argocd").UpdateScale(ctx, "argocd-application-controller", scale, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("scale application-controller to zero: %w", err)
	}

	return waitForResourceDeletion(ctx, "ArgoCD controller pod(s)", func(c context.Context) (int, error) {
		pods, listErr := clientset.CoreV1().Pods("argocd").List(c, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=argocd-application-controller",
		})
		if listErr != nil {
			return 0, fmt.Errorf("list controller pods: %w", listErr)
		}
		return len(pods.Items), nil
	}, 1*time.Minute, 3*time.Second)
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

	lbCount := deleteLBServices(ctx, clientset, svcs.Items)
	if lbCount == 0 {
		out.Infof("No LoadBalancer services found")
	} else {
		out.Infof("Requested deletion of %d LoadBalancer(s)", lbCount)
	}
	return nil
}

// deleteLBServices deletes each LoadBalancer-type Service and returns the count deleted.
func deleteLBServices(ctx context.Context, clientset *kubernetes.Clientset, items []corev1.Service) int {
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

// deletePersistentVolumeClaims finds and deletes all PVCs across all namespaces.
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
	} else {
		out.Infof("Requested deletion of %d PVC(s)", pvcCount)
	}
	return nil
}

// deleteOrphanedDOVolumes deletes all DigitalOcean block storage volumes in the
// given region via doctl. These are created by the K8s CSI driver for PVCs and
// live outside Pulumi state.
func deleteOrphanedDOVolumes(ctx context.Context, region string) error {
	doctlPath, err := exec.LookPath("doctl")
	if err != nil {
		return fmt.Errorf("doctl not found in PATH: %w", err)
	}

	output, err := exec.CommandContext(ctx, doctlPath, "compute", "volume", "list",
		"--region", region, "--format", "ID,Name", "--no-header").Output()
	if err != nil {
		return fmt.Errorf("list volumes: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		out.Infof("No volumes found in region %s", region)
		return nil
	}

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		id, name := fields[0], fields[1]
		out.Infof("Deleting volume %s (%s)...", name, id)
		if delErr := exec.CommandContext(ctx, doctlPath, "compute", "volume", "delete", id, doctlForceFlag).Run(); delErr != nil {
			out.Warning("Failed to delete volume %s: %v", name, delErr)
		}
	}
	return nil
}

// deleteOrphanedDOSnapshots deletes all volume snapshots via doctl.
func deleteOrphanedDOSnapshots(ctx context.Context) error {
	doctlPath, err := exec.LookPath("doctl")
	if err != nil {
		return fmt.Errorf("doctl not found in PATH: %w", err)
	}

	output, err := exec.CommandContext(ctx, doctlPath, "compute", "snapshot", "list",
		"--resource", "volume", "--format", "ID,Name", "--no-header").Output()
	if err != nil {
		return fmt.Errorf("list snapshots: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		out.Infof("No volume snapshots found")
		return nil
	}

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		id, name := fields[0], fields[1]
		out.Infof("Deleting snapshot %s (%s)...", name, id)
		if delErr := exec.CommandContext(ctx, doctlPath, "compute", "snapshot", "delete", id, doctlForceFlag).Run(); delErr != nil {
			out.Warning("Failed to delete snapshot %s: %v", name, delErr)
		}
	}
	return nil
}

// deleteOrphanedDOSpaces deletes all Spaces buckets in the given region using
// the aws CLI with the DO S3-compatible endpoint. Requires DO_SPACES_ACCESS_KEY_ID
// and DO_SPACES_SECRET_ACCESS_KEY environment variables.
func deleteOrphanedDOSpaces(ctx context.Context, region string) error {
	keyID := os.Getenv("DO_SPACES_ACCESS_KEY_ID")
	secretKey := os.Getenv("DO_SPACES_SECRET_ACCESS_KEY")
	if keyID == "" || secretKey == "" {
		out.Warning("DO_SPACES_ACCESS_KEY_ID or DO_SPACES_SECRET_ACCESS_KEY not set — skipping Spaces cleanup")
		return nil
	}

	awsPath, err := exec.LookPath("aws")
	if err != nil {
		return fmt.Errorf("aws CLI not found in PATH: %w", err)
	}

	endpoint := fmt.Sprintf("https://%s.digitaloceanspaces.com", region)
	spacesEnv := append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+keyID,
		"AWS_SECRET_ACCESS_KEY="+secretKey,
	)

	cmd := exec.CommandContext(ctx, awsPath, "s3", "ls", "--endpoint-url", endpoint)
	cmd.Env = spacesEnv
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("list spaces: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		out.Infof("No Spaces found in region %s", region)
		return nil
	}

	// Output format: "2024-01-01 00:00:00 bucket-name"
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		bucket := fields[2]
		out.Infof("Deleting Space %s...", bucket)
		delCmd := exec.CommandContext(ctx, awsPath, "s3", "rb",
			fmt.Sprintf("s3://%s", bucket), doctlForceFlag, "--endpoint-url", endpoint)
		delCmd.Env = spacesEnv
		if delErr := delCmd.Run(); delErr != nil {
			out.Warning("Failed to delete Space %s: %v", bucket, delErr)
		}
	}
	return nil
}
