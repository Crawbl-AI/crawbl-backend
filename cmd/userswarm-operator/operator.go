package main

import (
	"fmt"
	"os"

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
	"github.com/Crawbl-AI/crawbl-backend/internal/operator/zeroclaw"
)

func newOperatorCommand() *cobra.Command {
	var (
		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
		zeroClawConfigPath   string
	)

	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Run the UserSwarm operator manager",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runOperatorWithOptions(metricsAddr, probeAddr, enableLeaderElection, zeroClawConfigPath)
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	cmd.Flags().StringVar(&zeroClawConfigPath, "zeroclaw-config", "config/zeroclaw.yaml", "Path to the ZeroClaw operator config YAML file.")

	return cmd
}

func runOperatorWithOptions(metricsAddr, probeAddr string, enableLeaderElection bool, zeroClawConfigPath string) error {
	zcConfig, err := zeroclaw.LoadConfig(zeroClawConfigPath)
	if err != nil {
		return fmt.Errorf("load zeroclaw config: %w", err)
	}

	bootstrapImage := os.Getenv("USERSWARM_BOOTSTRAP_IMAGE")
	if bootstrapImage == "" {
		bootstrapImage = "registry.digitalocean.com/crawbl/crawbl-userswarm-operator:dev"
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
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		APIReader:        mgr.GetAPIReader(),
		BootstrapImage:   bootstrapImage,
		ZeroClawConfig:   zcConfig,
		BackupImage:      os.Getenv("USERSWARM_BACKUP_IMAGE"),
		BackupBucket:     os.Getenv("USERSWARM_BACKUP_BUCKET"),
		BackupRegion:     os.Getenv("USERSWARM_BACKUP_REGION"),
		BackupSecretName: os.Getenv("USERSWARM_BACKUP_SECRET_NAME"),
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

