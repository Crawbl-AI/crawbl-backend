package layers

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	drawerpkg "github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

const (
	l1MaxDrawers  = 15
	l1MaxChars    = memory.TokenBudgetL1
	maxSnippetLen = 200
)

// renderL1 generates the essential story from top drawers.
func renderL1(ctx context.Context, sess database.SessionRunner, drawerRepo drawerpkg.Repo, workspaceID, wing string) string {
	drawers, err := drawerRepo.GetTopByImportance(ctx, sess, workspaceID, wing, l1MaxDrawers)
	if err != nil {
		return "## L1 — No memories yet."
	}
	if len(drawers) == 0 {
		return "## L1 — No memories yet."
	}

	// Group by room.
	byRoom := make(map[string][]memory.Drawer)
	for i := range drawers {
		byRoom[drawers[i].Room] = append(byRoom[drawers[i].Room], drawers[i])
	}

	// Sort room names for deterministic output.
	rooms := make([]string, 0, len(byRoom))
	for room := range byRoom {
		rooms = append(rooms, room)
	}
	sort.Strings(rooms)

	var sb strings.Builder
	header := "## L1 — ESSENTIAL STORY"
	sb.WriteString(header)
	totalLen := len(header)

	for _, room := range rooms {
		roomLine := fmt.Sprintf("\n[%s]", room)
		sb.WriteString(roomLine)
		totalLen += len(roomLine)

		roomDrawers := byRoom[room]
		for i := range roomDrawers {
			d := &roomDrawers[i]
			snippet := strings.ReplaceAll(strings.TrimSpace(d.Content), "\n", " ")
			if len(snippet) > maxSnippetLen {
				snippet = snippet[:maxSnippetLen-3] + "..."
			}

			entry := fmt.Sprintf("\n  - %s", snippet)
			if d.SourceFile != "" {
				entry += fmt.Sprintf("  (%s)", filepath.Base(d.SourceFile))
			}

			if totalLen+len(entry) > l1MaxChars {
				sb.WriteString("\n  ... (more in L3 search)")
				return sb.String()
			}

			sb.WriteString(entry)
			totalLen += len(entry)
		}
	}

	return sb.String()
}
