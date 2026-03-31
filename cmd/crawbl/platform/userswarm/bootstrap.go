package userswarm

import (
	"fmt"
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
		zeroClawConfigPath  string
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
			if err := zeroclaw.EnsureManagedConfig(bootstrapConfigPath, liveConfigPath); err != nil {
				return err
			}

			// Write per-agent personality files to the PVC.
			zcConfig, err := zeroclaw.LoadConfig(zeroClawConfigPath)
			if err != nil {
				return fmt.Errorf("load zeroclaw config: %w", err)
			}
			agentFiles := zeroclaw.BuildAgentSkillFiles(zcConfig)
			if err := zeroclaw.EnsureAgentSkills(workspacePath, agentFiles); err != nil {
				return fmt.Errorf("ensure agent skills: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&bootstrapConfigPath, "bootstrap-config", "/bootstrap/config.toml", "Path to the rendered bootstrap config.toml")
	cmd.Flags().StringVar(&liveConfigPath, "live-config", "/zeroclaw-data/.zeroclaw/config.toml", "Path to the live PVC-backed ZeroClaw config.toml")
	cmd.Flags().StringVar(&workspacePath, "workspace", "/zeroclaw-data/workspace", "Path to the PVC-backed ZeroClaw workspace")
	cmd.Flags().StringVar(&zeroClawConfigPath, "zeroclaw-config", "/bootstrap/zeroclaw.yaml", "Path to operator ZeroClaw config for agent skill generation")

	return cmd
}
