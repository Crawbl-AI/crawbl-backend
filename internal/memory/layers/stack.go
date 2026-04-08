package layers

import (
	"context"
	"strings"

	drawerpkg "github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

type stack struct {
	drawerRepo drawerpkg.Repo
	embedder   embed.Embedder
}

// NewStack creates a new memory stack.
func NewStack(drawerRepo drawerpkg.Repo, embedder embed.Embedder) Stack {
	return &stack{
		drawerRepo: drawerRepo,
		embedder:   embedder,
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
	return renderL3(ctx, sess, s.drawerRepo, s.embedder, workspaceID, query, wing, room, limit)
}
