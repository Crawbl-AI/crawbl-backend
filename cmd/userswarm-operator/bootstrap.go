// Package controller provides Kubernetes controller logic.
package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/operator/zeroclaw"
)

// Directory permission for workspace directories (owner rwx only).
const workspaceDirPerm = 0o700

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
			if err := os.MkdirAll(filepath.Dir(liveConfigPath), workspaceDirPerm); err != nil {
				return err
			}
			if err := os.MkdirAll(workspacePath, workspaceDirPerm); err != nil {
				return err
			}

			vaultConfig := &zeroclaw.RuntimeVaultConfig{
				Enabled:  envBool("VAULT_ENABLED", false),
				FileName: envString("VAULT_FILE_NAME", "openai-api-key"),
			}

			return zeroclaw.EnsureManagedConfig(bootstrapConfigPath, liveConfigPath, vaultConfig)
		},
	}

	cmd.Flags().StringVar(&bootstrapConfigPath, "bootstrap-config", "/bootstrap/config.toml", "Path to the rendered bootstrap config.toml.")
	cmd.Flags().StringVar(&liveConfigPath, "live-config", "/zeroclaw-data/.zeroclaw/config.toml", "Path to the live PVC-backed ZeroClaw config.toml.")
	cmd.Flags().StringVar(&workspacePath, "workspace", "/zeroclaw-data/workspace", "Path to the PVC-backed ZeroClaw workspace.")

	return cmd
}
