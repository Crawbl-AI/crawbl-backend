package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		Long:  "Build and deploy a component. Platform and agent-runtime use ko + ArgoCD. Auth-filter uses Docker + ArgoCD. Docs and website deploy to Cloudflare Pages.",
		Example: `  crawbl app deploy platform --tag v1.0.0
  crawbl app deploy auth-filter --tag v1.0.0
  crawbl app deploy agent-runtime --tag v1.0.0
  crawbl app deploy docs
  crawbl app deploy website`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: platform, auth-filter, agent-runtime, docs, website)", args[0])
		},
	}

	cmd.AddCommand(newDeployPlatformCommand())
	cmd.AddCommand(newDeployAuthFilterCommand())
	cmd.AddCommand(newDeployAgentRuntimeCommand())
	cmd.AddCommand(newDeployDocsCommand())
	cmd.AddCommand(newDeployWebsiteCommand())

	return cmd
}

// addKoDeployFlags registers shared flags for ko-based deploy subcommands (platform, agent-runtime).
func addKoDeployFlags(cmd *cobra.Command, tag *string, argocdRepo *string) {
	cmd.Flags().StringVarP(tag, "tag", "t", "", "Image tag (default: auto-calculated semver)")
	cmd.Flags().StringVar(argocdRepo, "argocd-repo", "", "Path to crawbl-argocd-apps (default: sibling dir)")
}

// addDockerDeployFlags registers shared flags for Docker-based deploy subcommands (auth-filter).
func addDockerDeployFlags(cmd *cobra.Command, tag *string, platform *string, argocdRepo *string) {
	cmd.Flags().StringVarP(tag, "tag", "t", "", "Image tag (default: auto-calculated semver)")
	cmd.Flags().StringVar(platform, "platform", "linux/amd64", "Build platform")
	cmd.Flags().StringVar(argocdRepo, "argocd-repo", "", "Path to crawbl-argocd-apps (default: sibling dir)")
}

// checkNotProdContext returns an error when the current kubectl context targets
// the production cluster. Production deploys must go through GitHub CI only.
func checkNotProdContext() error {
	kubectlPath, err := exec.LookPath("kubectl")
	if err != nil {
		// If kubectl is unavailable, allow the deploy to proceed — the context
		// check is a safety net, not a hard dependency.
		return nil
	}
	var buf bytes.Buffer
	cmd := exec.CommandContext(context.Background(), kubectlPath, "config", "current-context")
	cmd.Stdout = &buf
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		// If kubectl is unavailable or returns an error, allow the deploy to
		// proceed — the context check is a safety net, not a hard dependency.
		return nil
	}
	if strings.TrimSpace(buf.String()) == prodKubeContext {
		return fmt.Errorf("production deploys must go through GitHub CI (deploy-prod.yml). Direct CLI deploys to prod are not allowed")
	}
	return nil
}

// checkAllTools verifies all required tools (argocd + release) are present.
func checkAllTools() error {
	if err := release.CheckTools(); err != nil {
		return err
	}
	return argocd.CheckTools()
}

// tagAndRelease tags the backend repo and creates a GitHub release.
// Uses gitutil.RootDir to locate the repo root automatically.
func tagAndRelease(repoSlug, tag, prevTag string) error {
	rootDir, err := gitutil.RootDir()
	if err != nil {
		return err
	}
	return release.TagAndRelease(release.Config{
		RepoPath: rootDir,
		RepoSlug: repoSlug,
		Tag:      tag,
		PrevTag:  prevTag,
	})
}

func newDeployPlatformCommand() *cobra.Command {
	var (
		tag        string
		argocdRepo string
		gc         bool
		scan       bool
	)

	cmd := &cobra.Command{
		Use:   "platform",
		Short: "Deploy the platform",
		Long:  "Build and push the crawbl-platform image via ko, then update orchestrator, webhook, and reaper image tags in crawbl-argocd-apps.",
		Example: `  crawbl app deploy platform --tag v1.0.0
  crawbl app deploy platform --tag v1.0.0 --argocd-repo ../crawbl-argocd-apps
  crawbl app deploy platform --gc`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeployPlatform(cmd.Context(), tag, argocdRepo, gc, scan)
		},
	}

	addKoDeployFlags(cmd, &tag, &argocdRepo)
	cmd.Flags().BoolVar(&gc, "gc", true, gcDescription)
	cmd.Flags().BoolVar(&scan, "scan", true, "Run SonarQube analysis after deploy")
	return cmd
}

// runDeployPlatform implements the platform deploy: build, push, argocd update, release, optional scan+GC.
func runDeployPlatform(ctx context.Context, tag, argocdRepo string, gc, scan bool) error {
	if err := checkNotProdContext(); err != nil {
		return err
	}
	if err := checkAllTools(); err != nil {
		return err
	}
	resolved, err := resolveDeployTag(tag, "")
	if err != nil {
		return err
	}
	tag = resolved.Tag

	if err := runKoBuild(ctx, koBuildOpts{
		importPath:   "./cmd/crawbl",
		imageRepo:    buildPlatformImageRepo,
		tag:          tag,
		push:         true,
		buildVersion: tag,
	}); err != nil {
		return err
	}
	if err := updateArgocdAndCommit(ctx, tag, argocdRepo, func(u *argocd.Update) error {
		if err := u.UpdateOrchestrator(ctx); err != nil {
			return err
		}
		return u.UpdatePlatform(ctx)
	}, "platform"); err != nil {
		return err
	}
	if err := tagAndRelease(RepoSlugBackend, tag, resolved.PrevTag); err != nil {
		return err
	}
	if scan {
		if err := runScan(ctx); err != nil {
			out.Warning("SonarQube scan failed: %v", err)
		}
	}
	if gc {
		return runGC(ctx, defaultGCKeep, false)
	}
	return nil
}

// updateArgocdAndCommit pulls the argocd repo, applies component-specific updates via
// updateFn, then commits and pushes under the given component label.
func updateArgocdAndCommit(ctx context.Context, tag, argocdRepo string, updateFn func(*argocd.Update) error, component string) error {
	repoPath, err := resolveArgocdRepo(argocdRepo)
	if err != nil {
		return err
	}
	u := &argocd.Update{RepoPath: repoPath, Tag: tag}
	if err := u.PullLatest(ctx); err != nil {
		return err
	}
	if err := updateFn(u); err != nil {
		return err
	}
	return u.CommitAndPush(ctx, component)
}

func newDeployAuthFilterCommand() *cobra.Command {
	var (
		tag        string
		platform   string
		argocdRepo string
		gc         bool
	)

	cmd := &cobra.Command{
		Use:   "auth-filter",
		Short: "Deploy the Envoy auth filter",
		Long:  "Build and push the envoy-auth-filter image, then update the image tag in crawbl-argocd-apps. Tags releases under auth-filter/vX.Y.Z.",
		Example: `  crawbl app deploy auth-filter
  crawbl app deploy auth-filter --tag v1.0.0
  crawbl app deploy auth-filter --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeployAuthFilter(cmd.Context(), tag, platform, argocdRepo, gc)
		},
	}

	addDockerDeployFlags(cmd, &tag, &platform, &argocdRepo)
	cmd.Flags().BoolVar(&gc, "gc", true, gcDescription)
	return cmd
}

// runDeployAuthFilter implements the auth-filter deploy: docker build+push, argocd update, release, optional GC.
func runDeployAuthFilter(ctx context.Context, tag, platform, argocdRepo string, gc bool) error {
	if err := checkNotProdContext(); err != nil {
		return err
	}
	if err := checkAllTools(); err != nil {
		return err
	}
	resolved, err := resolveDeployTag(tag, "auth-filter/")
	if err != nil {
		return err
	}
	// Git tag is prefixed (auth-filter/v0.1.0), Docker tag is bare (v0.1.0).
	gitTag := resolved.Tag
	dockerTag := strings.TrimPrefix(gitTag, "auth-filter/")

	rootDir, err := gitutil.RootDir()
	if err != nil {
		return err
	}
	if err := runDockerBuild(ctx, dockerBuildOpts{
		imageRepo:  buildAuthFilterImageRepo,
		dockerfile: filepath.Join(rootDir, buildAuthFilterDockerfile),
		contextDir: filepath.Join(rootDir, buildAuthFilterContext),
		tag:        dockerTag,
		platform:   platform,
		push:       true,
	}); err != nil {
		return err
	}
	if err := updateArgocdAndCommit(ctx, dockerTag, argocdRepo, func(u *argocd.Update) error {
		return u.UpdateAuthFilter(ctx)
	}, "auth-filter"); err != nil {
		return err
	}
	if err := tagAndRelease(RepoSlugBackend, gitTag, resolved.PrevTag); err != nil {
		return err
	}
	if gc {
		return runGC(ctx, defaultGCKeep, false)
	}
	return nil
}

// newDeployAgentRuntimeCommand builds the crawbl-agent-runtime image via ko,
// pushes to DOCR, bumps the tag in crawbl-argocd-apps, and tags the
// crawbl-backend repo with an agent-runtime/vX.Y.Z namespaced tag so it
// doesn't collide with the main platform tag sequence.
//
// This is the deploy counterpart to `crawbl app build agent-runtime`
// (which just builds locally without pushing). Use this once per
// merged PR that touches cmd/crawbl-agent-runtime/ or
// internal/agentruntime/.
func newDeployAgentRuntimeCommand() *cobra.Command {
	var (
		tag        string
		argocdRepo string
		gc         bool
	)

	cmd := &cobra.Command{
		Use:   "agent-runtime",
		Short: "Deploy the crawbl-agent-runtime image",
		Long:  "Build and push the crawbl-agent-runtime image via ko, then update the agent-runtime image tag in crawbl-argocd-apps. Tags releases under agent-runtime/vX.Y.Z.",
		Example: `  crawbl app deploy agent-runtime
  crawbl app deploy agent-runtime --tag v0.1.0
  crawbl app deploy agent-runtime --argocd-repo ../crawbl-argocd-apps`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeployAgentRuntime(cmd.Context(), tag, argocdRepo, gc)
		},
	}

	addKoDeployFlags(cmd, &tag, &argocdRepo)
	cmd.Flags().BoolVar(&gc, "gc", true, gcDescription)
	return cmd
}

// runDeployAgentRuntime implements the agent-runtime deploy: ko build+push, argocd update, release, optional GC.
func runDeployAgentRuntime(ctx context.Context, tag, argocdRepo string, gc bool) error {
	if err := checkNotProdContext(); err != nil {
		return err
	}
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

	if err := runKoBuild(ctx, koBuildOpts{
		importPath: "./cmd/crawbl-agent-runtime",
		imageRepo:  buildAgentRuntimeImageRepo,
		tag:        imageTag,
		push:       true,
	}); err != nil {
		return err
	}
	if err := updateArgocdAndCommit(ctx, imageTag, argocdRepo, func(u *argocd.Update) error {
		return u.UpdateAgentRuntime(ctx)
	}, "agent-runtime"); err != nil {
		return err
	}
	if err := tagAndRelease(RepoSlugBackend, gitTag, resolved.PrevTag); err != nil {
		return err
	}
	if gc {
		return runGC(ctx, defaultGCKeep, false)
	}
	return nil
}

func newDeployDocsCommand() *cobra.Command {
	return newStaticDeployCommand(staticDeployOpts{
		Use:   "docs",
		Short: "Deploy the documentation site to Cloudflare Pages",
		Long:  "Build the Docusaurus site and deploy static output to Cloudflare Pages.",
		Example: `  crawbl app deploy docs
  crawbl app deploy docs --tag v1.0.0
  crawbl app deploy docs --path /custom/path/crawbl-docs`,
		RepoDir:     buildDocsRepoDir,
		RepoSlug:    RepoSlugDocs,
		OutputDir:   "build",
		PagesName:   "crawbl-docs",
		PathDefault: "crawbl-docs",
	})
}

func newDeployWebsiteCommand() *cobra.Command {
	return newStaticDeployCommand(staticDeployOpts{
		Use:   "website",
		Short: "Deploy the marketing site to Cloudflare Pages",
		Long:  "Build the Next.js static site and deploy output to Cloudflare Pages.",
		Example: `  crawbl app deploy website
  crawbl app deploy website --tag v1.0.0
  crawbl app deploy website --path /custom/path/crawbl-website`,
		RepoDir:     buildWebsiteRepoDir,
		RepoSlug:    RepoSlugWebsite,
		OutputDir:   "out",
		PagesName:   "crawbl-website",
		PathDefault: "crawbl-website",
	})
}

// newStaticDeployCommand builds a Cloudflare Pages deploy subcommand for a
// static site (docs, website). Behaviour is identical across sites; only the
// repo directory, build output dir, and Pages project name vary.
func newStaticDeployCommand(opts staticDeployOpts) *cobra.Command {
	var (
		tag  string
		path string
	)

	cmd := &cobra.Command{
		Use:     opts.Use,
		Short:   opts.Short,
		Long:    opts.Long,
		Example: opts.Example,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkStaticDeployTools(); err != nil {
				return err
			}

			repoDir, err := gitutil.ResolveSiblingRepo(path, opts.RepoDir)
			if err != nil {
				return err
			}

			resolved, err := resolveDeployTagForRepo(tag, false, repoDir)
			if err != nil {
				return err
			}
			tag = resolved.Tag

			if err := runNpmBuild(repoDir); err != nil {
				return err
			}

			if err := runWranglerDeploy(repoDir, opts.OutputDir, opts.PagesName); err != nil {
				return err
			}

			return release.TagAndRelease(release.Config{
				RepoPath: repoDir,
				RepoSlug: opts.RepoSlug,
				Tag:      tag,
				PrevTag:  resolved.PrevTag,
			})
		},
	}

	addStaticDeployFlags(cmd, &tag, &path, opts.PathDefault)
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
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		return fmt.Errorf("npm not found in PATH: %w", err)
	}
	out.Step(style.Docker, "Building static site in %s", dir)
	cmd := exec.CommandContext(context.Background(), npmPath, "run", "build")
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
	wranglerPath, err := exec.LookPath("wrangler")
	if err != nil {
		return fmt.Errorf("wrangler not found in PATH: %w", err)
	}
	out.Step(style.Deploy, "Deploying %s to Cloudflare Pages", projectName)
	cmd := exec.CommandContext(context.Background(), wranglerPath, "pages", "deploy", outputDir, "--project-name", projectName) // #nosec G204 -- CLI tool, input from developer
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wrangler pages deploy failed: %w", err)
	}
	out.Success("Deployed %s to Cloudflare Pages", projectName)
	return nil
}
