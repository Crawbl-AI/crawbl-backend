package layers

import (
	"context"
	"fmt"
	"strings"

	drawerpkg "github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/kg"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

type stack struct {
	drawerRepo drawerpkg.Repo
	embedder   embed.Embedder
	kgGraph    kg.Graph
}

// NewStack creates a new memory stack. Pass nil for kgGraph to disable hybrid retrieval.
func NewStack(drawerRepo drawerpkg.Repo, embedder embed.Embedder, kgGraph kg.Graph) Stack {
	return &stack{
		drawerRepo: drawerRepo,
		embedder:   embedder,
		kgGraph:    kgGraph,
	}
}

func (s *stack) WakeUp(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) (string, error) {
	parts := make([]string, 0, 2)

	// L0: Identity.
	l0 := renderL0(ctx, sess, workspaceID)
	parts = append(parts, l0)

	// L1: Essential Story.
	l1 := renderL1(ctx, sess, s.drawerRepo, workspaceID, wing)
	parts = append(parts, l1)

	return strings.Join(parts, "\n\n"), nil
}

func (s *stack) Recall(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) (string, error) {
	return renderL2(ctx, sess, s.drawerRepo, workspaceID, wing, room, limit)
}

func (s *stack) Search(ctx context.Context, sess database.SessionRunner, workspaceID, query, wing, room string, limit int) (string, error) {
	if s.kgGraph == nil {
		// Fallback to pure vector search.
		return renderL3(ctx, sess, s.drawerRepo, s.embedder, workspaceID, query, wing, room, limit)
	}

	results, err := HybridRetrieve(ctx, sess, s.drawerRepo, s.kgGraph, s.embedder, workspaceID, query, "", limit)
	if err != nil {
		return renderL3(ctx, sess, s.drawerRepo, s.embedder, workspaceID, query, wing, room, limit)
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
