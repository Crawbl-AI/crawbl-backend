package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	buildAuthFilterImageRepo  = "registry.digitalocean.com/crawbl/envoy-auth-filter"
	buildAuthFilterDockerfile = "dockerfiles/envoy-auth-filter.dockerfile"
	buildAuthFilterContext    = "cmd/envoy-auth-filter"
)

func newBuildAuthFilterCommand() *cobra.Command {
	var (
		tag      string
		platform string
		push     bool
	)

	cmd := &cobra.Command{
		Use:   "auth-filter",
		Short: "Build Envoy auth filter WASM image",
		Long:  "Build the Envoy edge authentication WASM filter as an OCI image using docker buildx.",
		Example: `  crawbl app build auth-filter --tag v1.0.0
  crawbl app build auth-filter --tag latest --push`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}
			rootDir, err := getRootDir()
			if err != nil {
				return err
			}
			return runDockerBuild(buildOpts{
				imageRepo:  buildAuthFilterImageRepo,
				dockerfile: fmt.Sprintf("%s/%s", rootDir, buildAuthFilterDockerfile),
				contextDir: fmt.Sprintf("%s/%s", rootDir, buildAuthFilterContext),
				tag:        tag,
				platform:   platform,
				push:       push,
			})
		},
	}

	addBuildFlags(cmd, &tag, &platform, &push)
	return cmd
}
