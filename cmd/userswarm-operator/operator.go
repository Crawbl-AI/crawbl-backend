package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/operator/controller"
	"github.com/Crawbl-AI/crawbl-backend/internal/operator/indexes"
)

func newOperatorCommand() *cobra.Command {
	var (
		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
	)

	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Run the UserSwarm operator manager",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runOperatorWithOptions(metricsAddr, probeAddr, enableLeaderElection)
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")

	return cmd
}

func runOperator(cmd *cobra.Command) error {
	metricsAddr, _ := cmd.Flags().GetString("metrics-bind-address")
	probeAddr, _ := cmd.Flags().GetString("health-probe-bind-address")
	enableLeaderElection, _ := cmd.Flags().GetBool("leader-elect")
	return runOperatorWithOptions(metricsAddr, probeAddr, enableLeaderElection)
}

func runOperatorWithOptions(metricsAddr, probeAddr string, enableLeaderElection bool) error {
	bootstrapImage := os.Getenv("USERSWARM_BOOTSTRAP_IMAGE")
	if bootstrapImage == "" {
		bootstrapImage = "registry.digitalocean.com/crawbl/crawbl-userswarm-operator:dev"
	}
	runtimeVault := controller.RuntimeVaultConfig{
		Enabled:            envBool("USERSWARM_RUNTIME_VAULT_ENABLED", false),
		AuthPath:           envString("USERSWARM_RUNTIME_VAULT_AUTH_PATH", "auth/kubernetes"),
		Role:               envString("USERSWARM_RUNTIME_VAULT_ROLE", ""),
		PrePopulateOnly:    envBool("USERSWARM_RUNTIME_VAULT_PRE_POPULATE_ONLY", true),
		SecretPath:         envString("USERSWARM_RUNTIME_VAULT_SECRET_PATH", ""),
		SecretKey:          envString("USERSWARM_RUNTIME_VAULT_SECRET_KEY", "OPENAI_API_KEY"),
		FileName:           envString("USERSWARM_RUNTIME_VAULT_FILE_NAME", "openai-api-key"),
		AgentCPURequest:    envString("USERSWARM_RUNTIME_VAULT_AGENT_REQUESTS_CPU", "25m"),
		AgentMemoryRequest: envString("USERSWARM_RUNTIME_VAULT_AGENT_REQUESTS_MEM", "32Mi"),
		AgentCPULimit:      envString("USERSWARM_RUNTIME_VAULT_AGENT_LIMITS_CPU", "100m"),
		AgentMemoryLimit:   envString("USERSWARM_RUNTIME_VAULT_AGENT_LIMITS_MEM", "64Mi"),
	}

	ctrl.SetLogger(zap.New())

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(crawblv1alpha1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "userswarm-operator.crawbl.ai",
	})
	if err != nil {
		return err
	}

	if err := indexes.Setup(mgr); err != nil {
		return err
	}

	if err := (&controller.UserSwarmReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		APIReader:      mgr.GetAPIReader(),
		BootstrapImage: bootstrapImage,
		RuntimeVault:   runtimeVault,
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	return mgr.Start(ctrl.SetupSignalHandler())
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	case "":
		return fallback
	default:
		return fallback
	}
}
