package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build [component]",
		Short: "Build Crawbl component images",
		Long:  "Build Docker images for Crawbl platform components.",
		Example: `  crawbl app build platform     # Build unified platform image (orchestrator + webhook)
  crawbl app build auth-filter  # Build Envoy auth WASM filter image
  crawbl app build docs         # Build docs site image`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: platform, auth-filter, docs)", args[0])
		},
	}

	cmd.AddCommand(newBuildPlatformCommand())
	cmd.AddCommand(newBuildAuthFilterCommand())
	cmd.AddCommand(newBuildDocsCommand())

	return cmd
}
