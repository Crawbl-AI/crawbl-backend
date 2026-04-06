package app

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/gitutil"
)

const (
	buildPlatformImageRepo  = "registry.digitalocean.com/crawbl/crawbl-platform"
	buildPlatformDockerfile = "dockerfiles/platform-full.dockerfile"

	buildAuthFilterImageRepo  = "registry.digitalocean.com/crawbl/envoy-auth-filter"
	buildAuthFilterDockerfile = "dockerfiles/envoy-auth-filter.dockerfile"
	buildAuthFilterContext    = "cmd/envoy-auth-filter"

	buildDocsRepoDir = "crawbl-docs"

	buildWebsiteRepoDir = "crawbl-website"

	// crawbl-agent-runtime — the Phase 2 in-tree Go replacement for the
	// Rust agent runtime. Built from the same repo as the platform
	// image but uses a dedicated, minimal Dockerfile (distroless nonroot,
	// ~26 MB) so per-workspace pods pull a small image instead of the
	// full ~200 MB platform binary.
	buildAgentRuntimeImageRepo  = "registry.digitalocean.com/crawbl/crawbl-agent-runtime"
	buildAgentRuntimeDockerfile = "dockerfiles/agent-runtime.dockerfile"
)

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build [component]",
		Short: "Build a component Docker image",
		Long:  "Build a Docker image for one Crawbl component such as the platform or auth filter.",
		Example: `  crawbl app build platform     # Build unified platform image (orchestrator + webhook)
  crawbl app build auth-filter  # Build Envoy auth WASM filter image`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: platform, auth-filter)", args[0])
		},
	}

	cmd.AddCommand(newBuildPlatformCommand())
	cmd.AddCommand(newBuildAuthFilterCommand())

	return cmd
}

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
			rootDir, err := gitutil.RootDir()
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

func newBuildAuthFilterCommand() *cobra.Command {
	var (
		tag      string
		platform string
		push     bool
	)

	cmd := &cobra.Command{
		Use:   "auth-filter",
		Short: "Build the Envoy auth filter image",
		Long:  "Build the Envoy edge authentication WASM filter as an OCI image using docker buildx.",
		Example: `  crawbl app build auth-filter --tag v1.0.0
  crawbl app build auth-filter --tag latest --push`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}
			rootDir, err := gitutil.RootDir()
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
