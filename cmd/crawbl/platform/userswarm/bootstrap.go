package userswarm

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

const workspaceDirPerm = 0o700

func newBootstrapCommand() *cobra.Command {
	var (
		bootstrapConfigPath string
		liveConfigPath      string
		workspacePath       string
	)

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap ZeroClaw config on the runtime PVC",
		Long:  "Ensure the live ZeroClaw config exists on the runtime PVC and is derived from the managed bootstrap config.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := os.MkdirAll(filepath.Dir(liveConfigPath), workspaceDirPerm); err != nil {
				return err
			}
			if err := os.MkdirAll(workspacePath, workspaceDirPerm); err != nil {
				return err
			}
			return zeroclaw.EnsureManagedConfig(bootstrapConfigPath, liveConfigPath)
		},
	}

	cmd.Flags().StringVar(&bootstrapConfigPath, "bootstrap-config", "/bootstrap/config.toml", "Path to the rendered bootstrap config.toml")
	cmd.Flags().StringVar(&liveConfigPath, "live-config", "/zeroclaw-data/.zeroclaw/config.toml", "Path to the live PVC-backed ZeroClaw config.toml")
	cmd.Flags().StringVar(&workspacePath, "workspace", "/zeroclaw-data/workspace", "Path to the PVC-backed ZeroClaw workspace")

	return cmd
}
