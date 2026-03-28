package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	buildPlatformImageRepo  = "registry.digitalocean.com/crawbl/crawbl-platform"
	buildPlatformDockerfile = "dockerfiles/platform-full.dockerfile"
)

func newBuildPlatformCommand() *cobra.Command {
	var (
		tag      string
		platform string
		push     bool
	)

	cmd := &cobra.Command{
		Use:   "platform",
		Short: "Build the unified platform image",
		Long:  "Build the crawbl-platform Docker image containing the orchestrator, webhook, and all supporting subcommands.",
		Example: `  crawbl app build platform --tag v1.0.0
  crawbl app build platform --tag latest --push`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}
			rootDir, err := getRootDir()
			if err != nil {
				return err
			}
			return runDockerBuild(buildOpts{
				imageRepo:  buildPlatformImageRepo,
				dockerfile: fmt.Sprintf("%s/%s", rootDir, buildPlatformDockerfile),
				contextDir: rootDir,
				tag:        tag,
				platform:   platform,
				push:       push,
			})
		},
	}

	addBuildFlags(cmd, &tag, &platform, &push)
	return cmd
}
