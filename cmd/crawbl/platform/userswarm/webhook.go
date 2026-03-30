package userswarm

import (
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/userswarm/webhook"
)

func newWebhookCommand() *cobra.Command {
	cfg := &webhook.ListenConfig{}

	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Start the Metacontroller sync webhook server",
		Long:  "Start the HTTP server that Metacontroller calls to reconcile UserSwarm resources.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return webhook.Run(cmd.Context(), cfg)
		},
	}

	cmd.Flags().StringVar(&cfg.Addr, "addr", ":8080", "Address to listen on")
	cmd.Flags().StringVar(&cfg.ZeroClawCfgPath, "zeroclaw-config", "config/zeroclaw.yaml", "Path to the ZeroClaw config YAML")

	return cmd
}
