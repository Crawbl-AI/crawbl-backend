package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// defaultGCKeep is the number of latest tags to retain per repository when
// GC runs automatically after a deploy.
const defaultGCKeep = 5

func newGCCommand() *cobra.Command {
	var (
		keep   int
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Garbage-collect old container images from the registry",
		Long: `Remove old image tags from the DigitalOcean container registry, keeping
only the latest N tags per repository (sorted by updated_at).

Requires doctl to be installed and authenticated.`,
		Example: `  crawbl app gc                    # Keep latest 5 per repo (default)
  crawbl app gc --keep 3           # Keep latest 3 per repo
  crawbl app gc --dry-run          # Show what would be deleted`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGC(cmd.Context(), keep, dryRun)
		},
	}

	cmd.Flags().IntVar(&keep, "keep", 5, "Number of latest tags to keep per repository")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log what would be deleted without making changes")

	return cmd
}

// gcRepo represents a repository returned by doctl registry repository list-v2.
type gcRepo struct {
	Name string `json:"name"`
}

// gcTag represents a single tag returned by doctl registry repository list-tags.
type gcTag struct {
	Tag            string    `json:"tag"`
	ManifestDigest string    `json:"manifest_digest"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func runGC(ctx context.Context, keep int, dryRun bool) error {
	doctlPath, err := exec.LookPath("doctl")
	if err != nil {
		return fmt.Errorf("doctl is required but not found in PATH")
	}

	repos, err := gcListRepos(ctx, doctlPath)
	if err != nil {
		return fmt.Errorf("list repositories: %w", err)
	}

	out.Step(style.Docker, "Found %d repositories in registry", len(repos))

	var totalDeleted int

	for _, repo := range repos {
		totalDeleted += gcProcessRepo(ctx, doctlPath, repo.Name, keep, dryRun)
	}

	out.Ln()
	if dryRun {
		out.Step(style.Tip, "Dry run: %d tags would be deleted", totalDeleted)
	} else {
		out.Step(style.Reaper, "Deleted %d tags total", totalDeleted)
	}

	return nil
}

// gcProcessRepo deletes old tags from a single repository, keeping the latest N.
// Returns the number of tags deleted (or that would be deleted in dry-run mode).
func gcProcessRepo(ctx context.Context, doctlPath, repoName string, keep int, dryRun bool) int {
	tags, err := gcListTags(ctx, doctlPath, repoName)
	if err != nil {
		out.Warning("Failed to list tags for %s: %v", repoName, err)
		return 0
	}
	if len(tags) <= keep {
		out.Infof("%s: %d tags (within limit of %d, skipping)", repoName, len(tags), keep)
		return 0
	}

	// Sort by updated_at descending (newest first).
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].UpdatedAt.After(tags[j].UpdatedAt)
	})

	toDelete := tags[keep:]
	out.Step(style.Delete, "%s: %d total, keeping %d, deleting %d",
		repoName, len(tags), keep, len(toDelete))

	deleted := 0
	for _, tag := range toDelete {
		age := time.Since(tag.UpdatedAt).Truncate(time.Hour)
		if dryRun {
			out.Infof("[dry-run] would delete %s:%s (age: %s)", repoName, tag.Tag, age)
			deleted++
			continue
		}
		if err := gcDeleteManifest(ctx, doctlPath, repoName, tag.ManifestDigest); err != nil {
			out.Warning("Failed to delete %s:%s: %v", repoName, tag.Tag, err)
			continue
		}
		out.Infof("Deleted %s:%s (age: %s)", repoName, tag.Tag, age)
		deleted++
	}
	return deleted
}

// gcListRepos returns all repositories in the authenticated DOCR registry.
func gcListRepos(ctx context.Context, doctlPath string) ([]gcRepo, error) {
	data, err := exec.CommandContext(ctx, doctlPath, "registry", "repository", "list-v2", "-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("doctl registry repository list-v2: %w", err)
	}

	var repos []gcRepo
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, fmt.Errorf("parse repository list: %w", err)
	}
	return repos, nil
}

// gcListTags returns all tags for a given repository.
func gcListTags(ctx context.Context, doctlPath, repo string) ([]gcTag, error) {
	data, err := exec.CommandContext(ctx, doctlPath, "registry", "repository", "list-tags", repo, "-o", "json").Output() // #nosec G204 -- CLI tool, input from developer
	if err != nil {
		return nil, fmt.Errorf("doctl registry repository list-tags %s: %w", repo, err)
	}

	var tags []gcTag
	if err := json.Unmarshal(data, &tags); err != nil {
		return nil, fmt.Errorf("parse tag list for %s: %w", repo, err)
	}
	return tags, nil
}

// gcDeleteManifest deletes a manifest (and its associated tags) by digest.
func gcDeleteManifest(ctx context.Context, doctlPath, repo, digest string) error {
	output, err := exec.CommandContext(ctx, doctlPath, "registry", "repository", "delete-manifest", repo, digest, "--force").CombinedOutput() // #nosec G204 -- CLI tool, input from developer
	if err != nil {
		return fmt.Errorf("%s: %w", string(output), err)
	}
	return nil
}
