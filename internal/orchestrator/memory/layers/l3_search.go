package layers

import (
	"context"
	"fmt"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

const l3MaxSnippetLen = 300

// renderL3Opts groups the parameters for renderL3. ctx and sess remain
// positional per the project session/opts/repo pattern.
type renderL3Opts struct {
	DrawerRepo  drawerStore
	Embedder    embed.Embedder
	WorkspaceID string
	Query       string
	Wing        string
	Room        string
	Limit       int
}

// renderL3 performs semantic search and formats results.
func renderL3(ctx context.Context, sess database.SessionRunner, opts renderL3Opts) (string, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 5
	}

	// Generate query embedding.
	queryEmbed, err := opts.Embedder.Embed(ctx, opts.Query)
	if err != nil {
		return fmt.Sprintf("Search error: %v", err), nil
	}

	results, err := opts.DrawerRepo.Search(ctx, sess, opts.WorkspaceID, queryEmbed, opts.Wing, opts.Room, limit)
	if err != nil {
		return fmt.Sprintf("Search error: %v", err), nil
	}
	if len(results) == 0 {
		return "No results found.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## L3 — SEARCH RESULTS for \"%s\"", opts.Query)

	for i := range results {
		r := &results[i]
		snippet := strings.ReplaceAll(strings.TrimSpace(r.Content), "\n", " ")
		if len(snippet) > l3MaxSnippetLen {
			snippet = snippet[:l3MaxSnippetLen-3] + "..."
		}
		fmt.Fprintf(&sb, "\n  [%d] %s/%s (sim=%.3f)", i+1, r.Wing, r.Room, r.Similarity)
		fmt.Fprintf(&sb, "\n      %s", snippet)
		if r.SourceFile != "" {
			fmt.Fprintf(&sb, "\n      src: %s", r.SourceFile)
		}
	}

	return sb.String(), nil
}
