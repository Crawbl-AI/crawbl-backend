package userswarm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

const workspaceDirPerm = 0o700

func newBootstrapCommand() *cobra.Command {
	var (
		bootstrapConfigPath string
		liveConfigPath      string
		workspacePath       string
		bootstrapDir        string
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

			// Extract agent skill files from the ConfigMap mount.
			// Files are stored as "agent-skill--<agent>--<filename>" keys in the ConfigMap,
			// which Kubernetes mounts as files with those names under the bootstrap dir.
			agentFiles, err := readAgentSkillFiles(bootstrapDir)
			if err != nil {
				return fmt.Errorf("read agent skill files: %w", err)
			}
			if len(agentFiles) > 0 {
				if err := zeroclaw.EnsureAgentSkills(workspacePath, agentFiles); err != nil {
					return fmt.Errorf("ensure agent skills: %w", err)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&bootstrapConfigPath, "bootstrap-config", "/bootstrap/config.toml", "Path to the rendered bootstrap config.toml")
	cmd.Flags().StringVar(&liveConfigPath, "live-config", "/zeroclaw-data/.zeroclaw/config.toml", "Path to the live PVC-backed ZeroClaw config.toml")
	cmd.Flags().StringVar(&workspacePath, "workspace", "/zeroclaw-data/workspace", "Path to the PVC-backed ZeroClaw workspace")
	cmd.Flags().StringVar(&bootstrapDir, "bootstrap-dir", "/bootstrap", "Path to the mounted bootstrap ConfigMap directory")

	return cmd
}

// readAgentSkillFiles scans the bootstrap ConfigMap mount for agent skill files.
// Files are stored with keys like "agent-skill--wally--personality.md" in the ConfigMap,
// which Kubernetes mounts as files with those names under the volume mount path.
// Returns a map of agent name -> filename -> content.
func readAgentSkillFiles(bootstrapDir string) (map[string]map[string]string, error) {
	entries, err := os.ReadDir(bootstrapDir)
	if err != nil {
		return nil, err
	}

	result := make(map[string]map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "agent-skill--") {
			continue
		}
		// Parse "agent-skill--wally--personality.md" -> agent="wally", file="personality.md"
		trimmed := strings.TrimPrefix(entry.Name(), "agent-skill--")
		parts := strings.SplitN(trimmed, "--", 2)
		if len(parts) != 2 {
			continue
		}
		agentName, filename := parts[0], parts[1]
		content, err := os.ReadFile(filepath.Join(bootstrapDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read agent skill %s: %w", entry.Name(), err)
		}
		if result[agentName] == nil {
			result[agentName] = make(map[string]string)
		}
		result[agentName][filename] = string(content)
	}
	return result, nil
}
