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

	buildDocsImageRepo = "registry.digitalocean.com/crawbl/crawbl-docs"
	buildDocsRepoDir   = "crawbl-docs"

	buildWebsiteImageRepo = "registry.digitalocean.com/crawbl/crawbl-website"
	buildWebsiteRepoDir   = "crawbl-website"

	buildZeroClawImageRepo = "registry.digitalocean.com/crawbl/zeroclaw"
	buildZeroClawRepoDir   = "crawbl-zeroclaw"
)

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build [component]",
		Short: "Build a component Docker image",
		Long:  "Build a Docker image for one Crawbl component such as the platform, auth filter, or docs site.",
		Example: `  crawbl app build platform     # Build unified platform image (orchestrator + webhook)
  crawbl app build auth-filter  # Build Envoy auth WASM filter image
  crawbl app build docs         # Build docs site image`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: platform, auth-filter, docs, website, zeroclaw)", args[0])
		},
	}

	cmd.AddCommand(newBuildPlatformCommand())
	cmd.AddCommand(newBuildAuthFilterCommand())
	cmd.AddCommand(newBuildDocsCommand())
	cmd.AddCommand(newBuildWebsiteCommand())
	cmd.AddCommand(newBuildZeroClawCommand())

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

func newBuildDocsCommand() *cobra.Command {
	var (
		tag      string
		platform string
		push     bool
		path     string
	)

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Build the documentation site image",
		Long:  "Build the Crawbl documentation site Docker image using docker buildx.",
		Example: `  crawbl app build docs --tag v1.0.0
  crawbl app build docs --tag latest --push
  crawbl app build docs --tag v1.0.0 --path /custom/path/crawbl-docs`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}
			docsDir, err := gitutil.ResolveSiblingRepo(path, buildDocsRepoDir)
			if err != nil {
				return err
			}
			return runDockerBuild(buildOpts{
				imageRepo:  buildDocsImageRepo,
				contextDir: docsDir,
				tag:        tag,
				platform:   platform,
				push:       push,
			})
		},
	}

	addBuildFlags(cmd, &tag, &platform, &push)
	cmd.Flags().StringVar(&path, "path", "", "Path to crawbl-docs repo (default: ../crawbl-docs)")
	return cmd
}

func newBuildWebsiteCommand() *cobra.Command {
	var (
		tag      string
		platform string
		push     bool
		path     string
	)

	cmd := &cobra.Command{
		Use:   "website",
		Short: "Build the marketing site image",
		Long:  "Build the Crawbl marketing site Docker image using docker buildx.",
		Example: `  crawbl app build website --tag v1.0.0
  crawbl app build website --tag latest --push
  crawbl app build website --tag v1.0.0 --path /custom/path/crawbl-website`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}
			websiteDir, err := gitutil.ResolveSiblingRepo(path, buildWebsiteRepoDir)
			if err != nil {
				return err
			}
			return runDockerBuild(buildOpts{
				imageRepo:  buildWebsiteImageRepo,
				contextDir: websiteDir,
				tag:        tag,
				platform:   platform,
				push:       push,
			})
		},
	}

	addBuildFlags(cmd, &tag, &platform, &push)
	cmd.Flags().StringVar(&path, "path", "", "Path to crawbl-website repo (default: ../crawbl-website)")
	return cmd
}

func newBuildZeroClawCommand() *cobra.Command {
	var (
		tag      string
		platform string
		push     bool
		path     string
	)

	cmd := &cobra.Command{
		Use:   "zeroclaw",
		Short: "Build the ZeroClaw agent runtime image",
		Long:  "Build the ZeroClaw agent runtime Docker image using docker buildx.",
		Example: `  crawbl app build zeroclaw --tag v1.0.0
  crawbl app build zeroclaw --tag latest --push
  crawbl app build zeroclaw --tag v1.0.0 --path /custom/path/crawbl-zeroclaw`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}
			zeroClawDir, err := gitutil.ResolveSiblingRepo(path, buildZeroClawRepoDir)
			if err != nil {
				return err
			}
			return runDockerBuild(buildOpts{
				imageRepo:  buildZeroClawImageRepo,
				contextDir: zeroClawDir,
				tag:        tag,
				platform:   platform,
				push:       push,
				target:     "release",
			})
		},
	}

	addBuildFlags(cmd, &tag, &platform, &push)
	cmd.Flags().StringVar(&path, "path", "", "Path to crawbl-zeroclaw repo (default: ../crawbl-zeroclaw)")
	return cmd
}
