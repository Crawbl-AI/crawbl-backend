package app

import (
	"fmt"
	"os"
	"os/exec"

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
  crawbl app deploy zeroclaw --tag v1.0.0
  crawbl app deploy docs
  crawbl app deploy website
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
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkAllTools(); err != nil {
				return err
			}
			resolved, err := resolveDeployTag(tag, true, "")
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
			if err := u.PullLatest(); err != nil {
				return err
			}
			if err := u.UpdateOrchestrator(); err != nil {
				return err
			}
			if err := u.UpdatePlatform(); err != nil {
				return err
			}
			if err := u.CommitAndPush("platform"); err != nil {
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
		Long:  "Build and push the envoy-auth-filter image, then update the image tag in crawbl-argocd-apps.",
		Example: `  crawbl app deploy auth-filter --tag v1.0.0
  crawbl app deploy auth-filter --tag v1.0.0 --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkAllTools(); err != nil {
				return err
			}
			resolved, err := resolveDeployTag(tag, true, "")
			if err != nil {
				return err
			}
			tag = resolved.Tag

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
			if err := u.CommitAndPush("auth-filter"); err != nil {
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
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkAllTools(); err != nil {
				return err
			}
			resolved, err := resolveDeployTag(tag, true, "agent-runtime/")
			if err != nil {
				return err
			}
			tag = resolved.Tag

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
			if err := u.PullLatest(); err != nil {
				return err
			}
			if err := u.UpdateAgentRuntime(); err != nil {
				return err
			}
			if err := u.CommitAndPush("agent-runtime"); err != nil {
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
		Long:  "Build and push the zeroclaw image, then update the image reference in crawbl-argocd-apps. Auto-increments the crawbl fork suffix (e.g. v0.6.8-crawbl.1 → v0.6.8-crawbl.2).",
		Example: `  crawbl app deploy zeroclaw
  crawbl app deploy zeroclaw --tag v0.6.8-crawbl.2
  crawbl app deploy zeroclaw --path /custom/path/crawbl-zeroclaw`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkAllTools(); err != nil {
				return err
			}

			zeroClawDir, err := gitutil.ResolveSiblingRepo(path, buildZeroClawRepoDir)
			if err != nil {
				return err
			}

			resolved, err := resolveZeroClawTag(tag, zeroClawDir)
			if err != nil {
				return err
			}
			tag = resolved.Tag

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
			if err := u.CommitAndPush("zeroclaw"); err != nil {
				return err
			}

			return release.TagAndRelease(release.Config{
				RepoPath: zeroClawDir,
				RepoSlug: "Crawbl-AI/crawbl-zeroclaw",
				Tag:      tag,
				PrevTag:  resolved.PrevTag,
			})
		},
	}

	addDeployFlags(cmd, &tag, &platform, &argocdRepo)
	cmd.Flags().StringVar(&path, "path", "", "Path to crawbl-zeroclaw repo (default: ../crawbl-zeroclaw)")
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

			resolved, err := resolveDeployTag(tag, false, docsDir)
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

			resolved, err := resolveDeployTag(tag, false, websiteDir)
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
		Long:  "Build, push, and update argocd for platform and auth-filter. External components (docs, website, zeroclaw) must be deployed individually.",
		Example: `  crawbl app deploy all --tag v1.0.0
  crawbl app deploy all --tag v1.0.0 --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkAllTools(); err != nil {
				return err
			}
			resolved, err := resolveDeployTag(tag, true, "")
			if err != nil {
				return err
			}
			tag = resolved.Tag

			rootDir, err := gitutil.RootDir()
			if err != nil {
				return err
			}

			// --- Build phase ---

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

			// --- ArgoCD update phase ---

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
			if err := u.CommitAndPush("all"); err != nil {
				return err
			}

			// --- Tag + release ---

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
	cmd := exec.Command("npm", "run", "build")
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
	cmd := exec.Command("wrangler", "pages", "deploy", outputDir, "--project-name", projectName)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wrangler pages deploy failed: %w", err)
	}
	out.Success("Deployed %s to Cloudflare Pages", projectName)
	return nil
}
