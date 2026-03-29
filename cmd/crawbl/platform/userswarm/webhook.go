package userswarm

import (
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/userswarm/webhook"
)

func newWebhookCommand() *cobra.Command {
	sc := &webhook.ServerConfig{}

	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Start the Metacontroller sync webhook server",
		Long:  "Start the HTTP server that Metacontroller calls to reconcile UserSwarm resources.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return webhook.ListenAndServe(sc)
		},
	}

	cmd.Flags().StringVar(&sc.Addr, "addr", ":8080", "Address to listen on")
	cmd.Flags().StringVar(&sc.ZeroClawCfgPath, "zeroclaw-config", "config/zeroclaw.yaml", "Path to the ZeroClaw config YAML")

	return cmd
}
