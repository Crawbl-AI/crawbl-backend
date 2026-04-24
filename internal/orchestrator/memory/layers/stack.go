package layers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

type stack struct {
	drawerRepo   drawerStore
	identityRepo identityGetter
	embedder     embed.Embedder
}

// NewStack creates a new memory stack. The hybrid retrieval path uses the
// drawer repo's SearchHybrid method — there is no longer a separate KG
// graph handle passed in, since the CTE in drawerrepo owns the KG join.
// Both repo arguments are typed against the consumer-side interfaces in
// ports.go; the concrete postgres repos in memory/repo/... satisfy them
// implicitly.
func NewStack(drawerRepo drawerStore, identityRepo identityGetter, embedder embed.Embedder) Stack {
	return &stack{
		drawerRepo:   drawerRepo,
		identityRepo: identityRepo,
		embedder:     embedder,
	}
}

func (s *stack) WakeUp(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) (string, error) {
	l0 := renderL0(ctx, sess, s.identityRepo, workspaceID)
	l1 := renderL1(ctx, sess, s.drawerRepo, workspaceID, wing)
	return l0 + "\n\n" + l1, nil
}

func (s *stack) Recall(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) (string, error) {
	return renderL2(ctx, sess, s.drawerRepo, workspaceID, wing, room, limit)
}

func (s *stack) Search(ctx context.Context, sess database.SessionRunner, workspaceID, query, wing, room string, limit int) (string, error) {
	if s.embedder == nil {
		return renderL3(ctx, sess, renderL3Opts{
			DrawerRepo:  s.drawerRepo,
			Embedder:    s.embedder,
			WorkspaceID: workspaceID,
			Query:       query,
			Wing:        wing,
			Room:        room,
			Limit:       limit,
		})
	}

	results, err := HybridRetrieve(ctx, sess, HybridRetrieveOpts{
		DrawerRepo:  s.drawerRepo,
		Embedder:    s.embedder,
		WorkspaceID: workspaceID,
		Query:       query,
		AgentSlug:   "",
		Limit:       limit,
	})
	if err != nil {
		slog.WarnContext(ctx, "memory-search: hybrid retrieval failed, falling back to pure vector search",
			slog.String("workspace_id", workspaceID),
			slog.String("query", query),
			slog.String("error", err.Error()),
		)
		return renderL3(ctx, sess, renderL3Opts{
			DrawerRepo:  s.drawerRepo,
			Embedder:    s.embedder,
			WorkspaceID: workspaceID,
			Query:       query,
			Wing:        wing,
			Room:        room,
			Limit:       limit,
		})
	}
	if len(results) == 0 {
		return "No results found.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## L3 — HYBRID SEARCH for \"%s\"", query)
	for i := range results {
		r := &results[i]
		snippet := strings.ReplaceAll(strings.TrimSpace(r.Content), "\n", " ")
		if len(snippet) > l3MaxSnippetLen {
			snippet = snippet[:l3MaxSnippetLen-3] + "..."
		}
		fmt.Fprintf(&sb, "\n  [%d] %s/%s (score=%.3f)", i+1, r.Wing, r.Room, r.FinalScore)
		fmt.Fprintf(&sb, "\n      %s", snippet)
	}
	return sb.String(), nil
}
