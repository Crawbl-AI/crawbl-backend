package app

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/argocd"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/gitutil"
)

func newDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy [component]",
		Short: "Build, push, and deploy a component",
		Long:  "Build a Docker image, push to DOCR, and update crawbl-argocd-apps with the new tag.",
		Example: `  crawbl app deploy platform --tag v1.0.0
  crawbl app deploy zeroclaw --tag v1.0.0
  crawbl app deploy all --tag v1.0.0`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: platform, auth-filter, zeroclaw, docs, website, all)", args[0])
		},
	}

	cmd.AddCommand(newDeployPlatformCommand())
	cmd.AddCommand(newDeployAuthFilterCommand())
	cmd.AddCommand(newDeployZeroClawCommand())
	cmd.AddCommand(newDeployDocsCommand())
	cmd.AddCommand(newDeployWebsiteCommand())
	cmd.AddCommand(newDeployAllCommand())

	return cmd
}

// addDeployFlags registers shared flags for deploy subcommands.
func addDeployFlags(cmd *cobra.Command, tag *string, platform *string, argocdRepo *string) {
	cmd.Flags().StringVarP(tag, "tag", "t", "", "Image tag (default: auto-calculated semver)")
	cmd.Flags().StringVar(platform, "platform", "linux/amd64", "Build platform")
	cmd.Flags().StringVar(argocdRepo, "argocd-repo", "", "Path to crawbl-argocd-apps (default: sibling dir)")
}

func newDeployPlatformCommand() *cobra.Command {
	var (
		tag        string
		platform   string
		argocdRepo string
	)

	cmd := &cobra.Command{
		Use:   "platform",
		Short: "Deploy the platform",
		Long:  "Build and push the crawbl-platform image, then update orchestrator, webhook, and reaper image tags in crawbl-argocd-apps.",
		Example: `  crawbl app deploy platform --tag v1.0.0
  crawbl app deploy platform --tag v1.0.0 --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(_ *cobra.Command, _ []string) error {
			resolvedTag, err := resolveDeployTag(tag)
			if err != nil {
				return err
			}
			tag = resolvedTag
			if err := argocd.CheckTools(); err != nil {
				return err
			}

			rootDir, err := gitutil.RootDir()
			if err != nil {
				return err
			}

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildPlatformImageRepo,
				dockerfile: fmt.Sprintf("%s/%s", rootDir, buildPlatformDockerfile),
				contextDir: rootDir,
				tag:        tag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return err
			}

			repoPath, err := resolveArgocdRepo(argocdRepo)
			if err != nil {
				return err
			}
			u := &argocd.Update{RepoPath: repoPath, Tag: tag}
			if err := u.PullLatest(); err != nil {
				return err
			}
			if err := u.UpdateOrchestrator(); err != nil {
				return err
			}
			if err := u.UpdatePlatform(); err != nil {
				return err
			}
			return u.CommitAndPush("platform")
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	return cmd
}

func newDeployAuthFilterCommand() *cobra.Command {
	var (
		tag        string
		platform   string
		argocdRepo string
	)

	cmd := &cobra.Command{
		Use:   "auth-filter",
		Short: "Deploy the Envoy auth filter",
		Long:  "Build and push the envoy-auth-filter image, then update the image tag in crawbl-argocd-apps.",
		Example: `  crawbl app deploy auth-filter --tag v1.0.0
  crawbl app deploy auth-filter --tag v1.0.0 --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(_ *cobra.Command, _ []string) error {
			resolvedTag, err := resolveDeployTag(tag)
			if err != nil {
				return err
			}
			tag = resolvedTag
			if err := argocd.CheckTools(); err != nil {
				return err
			}

			rootDir, err := gitutil.RootDir()
			if err != nil {
				return err
			}

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildAuthFilterImageRepo,
				dockerfile: fmt.Sprintf("%s/%s", rootDir, buildAuthFilterDockerfile),
				contextDir: fmt.Sprintf("%s/%s", rootDir, buildAuthFilterContext),
				tag:        tag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return err
			}

			repoPath, err := resolveArgocdRepo(argocdRepo)
			if err != nil {
				return err
			}
			u := &argocd.Update{RepoPath: repoPath, Tag: tag}
			if err := u.PullLatest(); err != nil {
				return err
			}
			if err := u.UpdateAuthFilter(); err != nil {
				return err
			}
			return u.CommitAndPush("auth-filter")
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	return cmd
}

func newDeployZeroClawCommand() *cobra.Command {
	var (
		tag        string
		platform   string
		argocdRepo string
		path       string
	)

	cmd := &cobra.Command{
		Use:   "zeroclaw",
		Short: "Deploy the ZeroClaw agent runtime",
		Long:  "Build and push the zeroclaw image, then update the image reference in crawbl-argocd-apps.",
		Example: `  crawbl app deploy zeroclaw --tag v1.0.0-crawbl1
  crawbl app deploy zeroclaw --path /custom/path/crawbl-zeroclaw`,
		RunE: func(_ *cobra.Command, _ []string) error {
			resolvedTag, err := resolveDeployTag(tag)
			if err != nil {
				return err
			}
			tag = resolvedTag
			if err := argocd.CheckTools(); err != nil {
				return err
			}

			zeroClawDir, err := gitutil.ResolveSiblingRepo(path, buildZeroClawRepoDir)
			if err != nil {
				return err
			}

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildZeroClawImageRepo,
				contextDir: zeroClawDir,
				tag:        tag,
				platform:   platform,
				push:       true,
				target:     "release",
			}); err != nil {
				return err
			}

			repoPath, err := resolveArgocdRepo(argocdRepo)
			if err != nil {
				return err
			}
			u := &argocd.Update{RepoPath: repoPath, Tag: tag}
			if err := u.PullLatest(); err != nil {
				return err
			}
			if err := u.UpdateZeroClaw(); err != nil {
				return err
			}
			return u.CommitAndPush("zeroclaw")
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	cmd.Flags().StringVar(&path, "path", "", "Path to crawbl-zeroclaw repo (default: ../crawbl-zeroclaw)")
	return cmd
}

func newDeployDocsCommand() *cobra.Command {
	var (
		tag        string
		platform   string
		argocdRepo string
		path       string
	)

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Deploy the documentation site",
		Long:  "Build and push the crawbl-docs image, then update the image tag in crawbl-argocd-apps.",
		Example: `  crawbl app deploy docs --tag v1.0.0
  crawbl app deploy docs --path /custom/path/crawbl-docs`,
		RunE: func(_ *cobra.Command, _ []string) error {
			resolvedTag, err := resolveDeployTag(tag)
			if err != nil {
				return err
			}
			tag = resolvedTag
			if err := argocd.CheckTools(); err != nil {
				return err
			}

			docsDir, err := gitutil.ResolveSiblingRepo(path, buildDocsRepoDir)
			if err != nil {
				return err
			}

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildDocsImageRepo,
				contextDir: docsDir,
				tag:        tag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return err
			}

			repoPath, err := resolveArgocdRepo(argocdRepo)
			if err != nil {
				return err
			}
			u := &argocd.Update{RepoPath: repoPath, Tag: tag}
			if err := u.PullLatest(); err != nil {
				return err
			}
			if err := u.UpdateDocs(); err != nil {
				return err
			}
			return u.CommitAndPush("docs")
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	cmd.Flags().StringVar(&path, "path", "", "Path to crawbl-docs repo (default: ../crawbl-docs)")
	return cmd
}

func newDeployWebsiteCommand() *cobra.Command {
	var (
		tag        string
		platform   string
		argocdRepo string
		path       string
	)

	cmd := &cobra.Command{
		Use:   "website",
		Short: "Deploy the marketing site",
		Long:  "Build and push the crawbl-website image, then update the image tag in crawbl-argocd-apps.",
		Example: `  crawbl app deploy website --tag v1.0.0
  crawbl app deploy website --path /custom/path/crawbl-website`,
		RunE: func(_ *cobra.Command, _ []string) error {
			resolvedTag, err := resolveDeployTag(tag)
			if err != nil {
				return err
			}
			tag = resolvedTag
			if err := argocd.CheckTools(); err != nil {
				return err
			}

			websiteDir, err := gitutil.ResolveSiblingRepo(path, buildWebsiteRepoDir)
			if err != nil {
				return err
			}

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildWebsiteImageRepo,
				contextDir: websiteDir,
				tag:        tag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return err
			}

			repoPath, err := resolveArgocdRepo(argocdRepo)
			if err != nil {
				return err
			}
			u := &argocd.Update{RepoPath: repoPath, Tag: tag}
			if err := u.PullLatest(); err != nil {
				return err
			}
			if err := u.UpdateWebsite(); err != nil {
				return err
			}
			return u.CommitAndPush("website")
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	return cmd
}

func newDeployAllCommand() *cobra.Command {
	var (
		tag        string
		platform   string
		argocdRepo string
	)

	cmd := &cobra.Command{
		Use:   "all",
		Short: "Deploy all backend components",
		Long:  "Build, push, and update argocd for platform, auth-filter, docs, and website. ZeroClaw is excluded (deploy separately with 'crawbl app deploy zeroclaw').",
		Example: `  crawbl app deploy all --tag v1.0.0
  crawbl app deploy all --tag v1.0.0 --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(_ *cobra.Command, _ []string) error {
			resolvedTag, err := resolveDeployTag(tag)
			if err != nil {
				return err
			}
			tag = resolvedTag
			if err := argocd.CheckTools(); err != nil {
				return err
			}

			rootDir, err := gitutil.RootDir()
			if err != nil {
				return err
			}

			// --- Build phase: build all images first ---

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildPlatformImageRepo,
				dockerfile: fmt.Sprintf("%s/%s", rootDir, buildPlatformDockerfile),
				contextDir: rootDir,
				tag:        tag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return fmt.Errorf("platform build: %w", err)
			}

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildAuthFilterImageRepo,
				dockerfile: fmt.Sprintf("%s/%s", rootDir, buildAuthFilterDockerfile),
				contextDir: fmt.Sprintf("%s/%s", rootDir, buildAuthFilterContext),
				tag:        tag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return fmt.Errorf("auth-filter build: %w", err)
			}

			docsDir, err := gitutil.ResolveSiblingRepo("", buildDocsRepoDir)
			if err != nil {
				return fmt.Errorf("docs: %w", err)
			}
			if err := runDockerBuild(buildOpts{
				imageRepo:  buildDocsImageRepo,
				contextDir: docsDir,
				tag:        tag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return fmt.Errorf("docs build: %w", err)
			}

			websiteDir, err := gitutil.ResolveSiblingRepo("", buildWebsiteRepoDir)
			if err != nil {
				return fmt.Errorf("website: %w", err)
			}
			if err := runDockerBuild(buildOpts{
				imageRepo:  buildWebsiteImageRepo,
				contextDir: websiteDir,
				tag:        tag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return fmt.Errorf("website build: %w", err)
			}

			// --- ArgoCD update phase: all updates then one commit ---

			repoPath, err := resolveArgocdRepo(argocdRepo)
			if err != nil {
				return err
			}
			u := &argocd.Update{RepoPath: repoPath, Tag: tag}

			if err := u.PullLatest(); err != nil {
				return err
			}
			if err := u.UpdateOrchestrator(); err != nil {
				return err
			}
			if err := u.UpdatePlatform(); err != nil {
				return err
			}
			if err := u.UpdateAuthFilter(); err != nil {
				return err
			}
			if err := u.UpdateDocs(); err != nil {
				return err
			}
			if err := u.UpdateWebsite(); err != nil {
				return err
			}

			return u.CommitAndPush("all")
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	return cmd
}
