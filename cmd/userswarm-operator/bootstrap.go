package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/operator/zeroclaw"
)

func newBootstrapCommand() *cobra.Command {
	var (
		bootstrapConfigPath string
		liveConfigPath      string
		workspacePath       string
	)

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap the live UserSwarm ZeroClaw config on the PVC",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := os.MkdirAll(filepath.Dir(liveConfigPath), 0o700); err != nil {
				return err
			}
			if err := os.MkdirAll(workspacePath, 0o700); err != nil {
				return err
			}
			return zeroclaw.EnsureManagedConfig(bootstrapConfigPath, liveConfigPath)
		},
	}

	cmd.Flags().StringVar(&bootstrapConfigPath, "bootstrap-config", "/bootstrap/config.toml", "Path to the rendered bootstrap config.toml.")
	cmd.Flags().StringVar(&liveConfigPath, "live-config", "/zeroclaw-data/.zeroclaw/config.toml", "Path to the live PVC-backed ZeroClaw config.toml.")
	cmd.Flags().StringVar(&workspacePath, "workspace", "/zeroclaw-data/workspace", "Path to the PVC-backed ZeroClaw workspace.")

	return cmd
}
