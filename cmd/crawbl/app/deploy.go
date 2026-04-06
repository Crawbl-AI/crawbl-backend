package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/argocd"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/gitutil"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/release"
)

func newDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy [component]",
		Short: "Build, push, and deploy a component",
		Long:  "Build and deploy a component. Backend components use Docker + ArgoCD. Docs and website deploy to Cloudflare Pages.",
		Example: `  crawbl app deploy platform --tag v1.0.0
  crawbl app deploy agent-runtime --tag v1.0.0
  crawbl app deploy docs
  crawbl app deploy website
  crawbl app deploy all --tag v1.0.0`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: platform, auth-filter, agent-runtime, docs, website, all)", args[0])
		},
	}

	cmd.AddCommand(newDeployPlatformCommand())
	cmd.AddCommand(newDeployAuthFilterCommand())
	cmd.AddCommand(newDeployAgentRuntimeCommand())
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

// checkAllTools verifies all required tools (argocd + release) are present.
func checkAllTools() error {
	if err := release.CheckTools(); err != nil {
		return err
	}
	return argocd.CheckTools()
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := checkAllTools(); err != nil {
				return err
			}
			resolved, err := resolveDeployTag(tag, "")
			if err != nil {
				return err
			}
			tag = resolved.Tag

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
			if err := u.PullLatest(ctx); err != nil {
				return err
			}
			if err := u.UpdateOrchestrator(ctx); err != nil {
				return err
			}
			if err := u.UpdatePlatform(ctx); err != nil {
				return err
			}
			if err := u.CommitAndPush(ctx, "platform"); err != nil {
				return err
			}

			return release.TagAndRelease(release.Config{
				RepoPath: rootDir,
				RepoSlug: "Crawbl-AI/crawbl-backend",
				Tag:      tag,
				PrevTag:  resolved.PrevTag,
			})
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
		Long:  "Build and push the envoy-auth-filter image, then update the image tag in crawbl-argocd-apps. Tags releases under auth-filter/vX.Y.Z.",
		Example: `  crawbl app deploy auth-filter
  crawbl app deploy auth-filter --tag v1.0.0
  crawbl app deploy auth-filter --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := checkAllTools(); err != nil {
				return err
			}
			resolved, err := resolveDeployTag(tag, "auth-filter/")
			if err != nil {
				return err
			}
			// Git tag is prefixed (auth-filter/v0.1.0), Docker tag is bare (v0.1.0).
			gitTag := resolved.Tag
			tag = strings.TrimPrefix(gitTag, "auth-filter/")

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
			if err := u.PullLatest(ctx); err != nil {
				return err
			}
			if err := u.UpdateAuthFilter(ctx); err != nil {
				return err
			}
			if err := u.CommitAndPush(ctx, "auth-filter"); err != nil {
				return err
			}

			return release.TagAndRelease(release.Config{
				RepoPath: rootDir,
				RepoSlug: "Crawbl-AI/crawbl-backend",
				Tag:      gitTag,
				PrevTag:  resolved.PrevTag,
			})
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	return cmd
}

// newDeployAgentRuntimeCommand ships the new Phase 2 crawbl-agent-runtime
// image: builds the small distroless binary from dockerfiles/agent-runtime
// .dockerfile, pushes to DOCR, bumps the tag in crawbl-argocd-apps, and
// tags the crawbl-backend repo with an agent-runtime/vX.Y.Z namespaced
// tag so it doesn't collide with the main platform tag sequence.
//
// This is the deploy counterpart to `crawbl app build agent-runtime`
// (which just builds locally without pushing). Use this once per
// merged PR that touches cmd/crawbl-agent-runtime/ or
// internal/agentruntime/.
func newDeployAgentRuntimeCommand() *cobra.Command {
	var (
		tag        string
		platform   string
		argocdRepo string
	)

	cmd := &cobra.Command{
		Use:   "agent-runtime",
		Short: "Deploy the crawbl-agent-runtime image",
		Long:  "Build and push the crawbl-agent-runtime image (distroless, ~26 MB), then update the agent-runtime image tag in crawbl-argocd-apps. Tags releases under agent-runtime/vX.Y.Z.",
		Example: `  crawbl app deploy agent-runtime
  crawbl app deploy agent-runtime --tag v0.1.0
  crawbl app deploy agent-runtime --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := checkAllTools(); err != nil {
				return err
			}
			resolved, err := resolveDeployTag(tag, "agent-runtime/")
			if err != nil {
				return err
			}
			// Git tag is prefixed (agent-runtime/v0.1.0), Docker tag is bare (v0.1.0).
			gitTag := resolved.Tag
			imageTag := strings.TrimPrefix(gitTag, "agent-runtime/")
			tag = imageTag

			rootDir, err := gitutil.RootDir()
			if err != nil {
				return err
			}

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildAgentRuntimeImageRepo,
				dockerfile: fmt.Sprintf("%s/%s", rootDir, buildAgentRuntimeDockerfile),
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
			if err := u.PullLatest(ctx); err != nil {
				return err
			}
			if err := u.UpdateAgentRuntime(ctx); err != nil {
				return err
			}
			if err := u.CommitAndPush(ctx, "agent-runtime"); err != nil {
				return err
			}

			return release.TagAndRelease(release.Config{
				RepoPath: rootDir,
				RepoSlug: "Crawbl-AI/crawbl-backend",
				Tag:      gitTag,
				PrevTag:  resolved.PrevTag,
			})
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	return cmd
}

func newDeployDocsCommand() *cobra.Command {
	var (
		tag  string
		path string
	)

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Deploy the documentation site to Cloudflare Pages",
		Long:  "Build the Docusaurus site and deploy static output to Cloudflare Pages.",
		Example: `  crawbl app deploy docs
  crawbl app deploy docs --tag v1.0.0
  crawbl app deploy docs --path /custom/path/crawbl-docs`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkStaticDeployTools(); err != nil {
				return err
			}

			docsDir, err := gitutil.ResolveSiblingRepo(path, buildDocsRepoDir)
			if err != nil {
				return err
			}

			resolved, err := resolveDeployTagForRepo(tag, false, docsDir)
			if err != nil {
				return err
			}
			tag = resolved.Tag

			if err := runNpmBuild(docsDir); err != nil {
				return err
			}

			if err := runWranglerDeploy(docsDir, "build", "crawbl-docs"); err != nil {
				return err
			}

			return release.TagAndRelease(release.Config{
				RepoPath: docsDir,
				RepoSlug: "Crawbl-AI/crawbl-docs",
				Tag:      tag,
				PrevTag:  resolved.PrevTag,
			})
		},
	}

	addStaticDeployFlags(cmd, &tag, &path, "crawbl-docs")
	return cmd
}

func newDeployWebsiteCommand() *cobra.Command {
	var (
		tag  string
		path string
	)

	cmd := &cobra.Command{
		Use:   "website",
		Short: "Deploy the marketing site to Cloudflare Pages",
		Long:  "Build the Next.js static site and deploy output to Cloudflare Pages.",
		Example: `  crawbl app deploy website
  crawbl app deploy website --tag v1.0.0
  crawbl app deploy website --path /custom/path/crawbl-website`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkStaticDeployTools(); err != nil {
				return err
			}

			websiteDir, err := gitutil.ResolveSiblingRepo(path, buildWebsiteRepoDir)
			if err != nil {
				return err
			}

			resolved, err := resolveDeployTagForRepo(tag, false, websiteDir)
			if err != nil {
				return err
			}
			tag = resolved.Tag

			if err := runNpmBuild(websiteDir); err != nil {
				return err
			}

			if err := runWranglerDeploy(websiteDir, "out", "crawbl-website"); err != nil {
				return err
			}

			return release.TagAndRelease(release.Config{
				RepoPath: websiteDir,
				RepoSlug: "Crawbl-AI/crawbl-website",
				Tag:      tag,
				PrevTag:  resolved.PrevTag,
			})
		},
	}

	addStaticDeployFlags(cmd, &tag, &path, "crawbl-website")
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
		Long:  "Build, push, and update argocd for platform and auth-filter. External components (docs, website, agent-runtime) must be deployed individually.",
		Example: `  crawbl app deploy all --tag v1.0.0
  crawbl app deploy all --tag v1.0.0 --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := checkAllTools(); err != nil {
				return err
			}

			// Platform uses the global v* tag sequence.
			platformResolved, err := resolveDeployTag(tag, "")
			if err != nil {
				return err
			}
			platformTag := platformResolved.Tag

			// Auth-filter uses its own auth-filter/v* tag sequence.
			afResolved, err := resolveDeployTag(tag, "auth-filter/")
			if err != nil {
				return err
			}
			afGitTag := afResolved.Tag
			afImageTag := strings.TrimPrefix(afGitTag, "auth-filter/")

			rootDir, err := gitutil.RootDir()
			if err != nil {
				return err
			}

			// --- Build phase ---

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildPlatformImageRepo,
				dockerfile: fmt.Sprintf("%s/%s", rootDir, buildPlatformDockerfile),
				contextDir: rootDir,
				tag:        platformTag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return fmt.Errorf("platform build: %w", err)
			}

			if err := runDockerBuild(buildOpts{
				imageRepo:  buildAuthFilterImageRepo,
				dockerfile: fmt.Sprintf("%s/%s", rootDir, buildAuthFilterDockerfile),
				contextDir: fmt.Sprintf("%s/%s", rootDir, buildAuthFilterContext),
				tag:        afImageTag,
				platform:   platform,
				push:       true,
			}); err != nil {
				return fmt.Errorf("auth-filter build: %w", err)
			}

			// --- ArgoCD update phase ---

			repoPath, err := resolveArgocdRepo(argocdRepo)
			if err != nil {
				return err
			}

			if err := (&argocd.Update{RepoPath: repoPath, Tag: platformTag}).PullLatest(ctx); err != nil {
				return err
			}

			platformUpdate := &argocd.Update{RepoPath: repoPath, Tag: platformTag}
			if err := platformUpdate.UpdateOrchestrator(ctx); err != nil {
				return err
			}
			if err := platformUpdate.UpdatePlatform(ctx); err != nil {
				return err
			}

			afUpdate := &argocd.Update{RepoPath: repoPath, Tag: afImageTag}
			if err := afUpdate.UpdateAuthFilter(ctx); err != nil {
				return err
			}

			if err := platformUpdate.CommitAndPush(ctx, "all"); err != nil {
				return err
			}

			// --- Tag + release (platform tag is the primary release) ---

			if err := release.TagAndRelease(release.Config{
				RepoPath: rootDir,
				RepoSlug: "Crawbl-AI/crawbl-backend",
				Tag:      platformTag,
				PrevTag:  platformResolved.PrevTag,
			}); err != nil {
				return err
			}

			// Tag auth-filter separately in its own namespace.
			return release.TagAndRelease(release.Config{
				RepoPath: rootDir,
				RepoSlug: "Crawbl-AI/crawbl-backend",
				Tag:      afGitTag,
				PrevTag:  afResolved.PrevTag,
			})
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	return cmd
}

// addStaticDeployFlags registers flags for static site deploy subcommands (docs, website).
func addStaticDeployFlags(cmd *cobra.Command, tag *string, path *string, pathDefault string) {
	cmd.Flags().StringVarP(tag, "tag", "t", "", "Release tag (default: auto-calculated semver)")
	cmd.Flags().StringVar(path, "path", "", fmt.Sprintf("Path to %s repo (default: ../%s)", pathDefault, pathDefault))
}

// checkStaticDeployTools verifies required tools for static site deploys.
func checkStaticDeployTools() error {
	if err := release.CheckTools(); err != nil {
		return err
	}
	for _, tool := range []string{"npm", "wrangler"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is required but not found in PATH", tool)
		}
	}
	return nil
}

// runNpmBuild runs npm run build in the given directory.
func runNpmBuild(dir string) error {
	out.Step(style.Docker, "Building static site in %s", dir)
	cmd := exec.CommandContext(context.Background(), "npm", "run", "build")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm run build failed: %w", err)
	}
	out.Success("Static site built successfully")
	return nil
}

// runWranglerDeploy deploys a static site to Cloudflare Pages using wrangler.
func runWranglerDeploy(dir, outputDir, projectName string) error {
	out.Step(style.Deploy, "Deploying %s to Cloudflare Pages", projectName)
	cmd := exec.CommandContext(context.Background(), "wrangler", "pages", "deploy", outputDir, "--project-name", projectName)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wrangler pages deploy failed: %w", err)
	}
	out.Success("Deployed %s to Cloudflare Pages", projectName)
	return nil
}
