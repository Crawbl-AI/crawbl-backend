package app

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/configenv"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/gitutil"
)

var (
	registryBase = configenv.StringOr("CRAWBL_REGISTRY", "registry.digitalocean.com/crawbl")

	buildPlatformImageRepo     = registryBase + "/crawbl-platform"
	buildAgentRuntimeImageRepo = registryBase + "/crawbl-agent-runtime"
	buildAuthFilterImageRepo   = registryBase + "/envoy-auth-filter"
	buildAuthFilterDockerfile  = "dockerfiles/envoy-auth-filter.dockerfile"
	buildAuthFilterContext     = "cmd/envoy-auth-filter"

	buildDocsRepoDir    = "crawbl-docs"
	buildWebsiteRepoDir = "crawbl-website"
)

const errTagRequired = "--tag is required"

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build [component]",
		Short: "Build a component container image",
		Long:  "Build a container image for one Crawbl component. Platform and agent-runtime use ko; auth-filter uses Docker.",
		Example: `  crawbl app build platform        # Build unified platform image (ko)
  crawbl app build agent-runtime   # Build agent runtime image (ko)
  crawbl app build auth-filter     # Build Envoy auth WASM filter image (Docker)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: platform, agent-runtime, auth-filter)", args[0])
		},
	}

	cmd.AddCommand(newBuildPlatformCommand())
	cmd.AddCommand(newBuildAgentRuntimeCommand())
	cmd.AddCommand(newBuildAuthFilterCommand())

	return cmd
}

func newBuildPlatformCommand() *cobra.Command {
	var (
		tag  string
		push bool
	)

	cmd := &cobra.Command{
		Use:   "platform",
		Short: "Build the unified platform image",
		Long:  "Build the crawbl-platform container image containing the orchestrator, webhook, and all supporting subcommands.",
		Example: `  crawbl app build platform --tag v1.0.0
  crawbl app build platform --tag latest --push`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf(errTagRequired)
			}
			return runKoBuild(cmd.Context(), koBuildOpts{
				importPath:   "./cmd/crawbl",
				imageRepo:    buildPlatformImageRepo,
				tag:          tag,
				push:         push,
				buildVersion: tag,
			})
		},
	}

	addKoBuildFlags(cmd, &tag, &push)
	return cmd
}

func newBuildAgentRuntimeCommand() *cobra.Command {
	var (
		tag  string
		push bool
	)

	cmd := &cobra.Command{
		Use:   "agent-runtime",
		Short: "Build the agent runtime image",
		Long:  "Build the crawbl-agent-runtime container image (distroless nonroot, lightweight).",
		Example: `  crawbl app build agent-runtime --tag v1.0.0
  crawbl app build agent-runtime --tag latest --push`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf(errTagRequired)
			}
			return runKoBuild(cmd.Context(), koBuildOpts{
				importPath: "./cmd/crawbl-agent-runtime",
				imageRepo:  buildAgentRuntimeImageRepo,
				tag:        tag,
				push:       push,
			})
		},
	}

	addKoBuildFlags(cmd, &tag, &push)
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf(errTagRequired)
			}
			rootDir, err := gitutil.RootDir()
			if err != nil {
				return err
			}
			return runDockerBuild(cmd.Context(), dockerBuildOpts{
				imageRepo:  buildAuthFilterImageRepo,
				dockerfile: filepath.Join(rootDir, buildAuthFilterDockerfile),
				contextDir: filepath.Join(rootDir, buildAuthFilterContext),
				tag:        tag,
				platform:   platform,
				push:       push,
			})
		},
	}

	addDockerBuildFlags(cmd, &tag, &platform, &push)
	return cmd
}
