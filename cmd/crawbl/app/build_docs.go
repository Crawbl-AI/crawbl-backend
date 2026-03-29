package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const (
	buildDocsImageRepo = "registry.digitalocean.com/crawbl/crawbl-docs"
	buildDocsRepoDir   = "crawbl-docs"
)

func newBuildDocsCommand() *cobra.Command {
	var (
		tag      string
		platform string
		push     bool
	)

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Build the documentation site image",
		Long:  "Build the Crawbl documentation site Docker image using docker buildx.",
		Example: `  crawbl app build docs --tag v1.0.0
  crawbl app build docs --tag latest --push`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}
			rootDir, err := getRootDir()
			if err != nil {
				return err
			}

			docsDir := filepath.Join(filepath.Dir(rootDir), buildDocsRepoDir)
			if _, err := os.Stat(filepath.Join(docsDir, "Dockerfile")); err != nil {
				return fmt.Errorf("crawbl-docs not found at %s: %w", docsDir, err)
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
	return cmd
}
