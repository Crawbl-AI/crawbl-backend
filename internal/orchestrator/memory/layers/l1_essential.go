package layers

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	memrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

const (
	l1MaxDrawers  = 15
	l1MaxChars    = memory.TokenBudgetL1
	maxSnippetLen = 200
)

const l1TruncationNote = "\n  ... (more in L3 search)"

// renderL1 generates the essential story from top drawers.
func renderL1(ctx context.Context, sess database.SessionRunner, drawerRepo memrepo.DrawerRepo, workspaceID, wing string) string {
	drawers, err := drawerRepo.GetTopByImportance(ctx, sess, workspaceID, wing, l1MaxDrawers)
	if err != nil || len(drawers) == 0 {
		return "## L1 — No memories yet."
	}

	byRoom := groupDrawersByRoom(drawers)
	rooms := sortedRoomNames(byRoom)

	var sb strings.Builder
	header := "## L1 — ESSENTIAL STORY"
	sb.WriteString(header)
	totalLen := len(header)

	for _, room := range rooms {
		written, truncated := appendRoomSection(&sb, room, byRoom[room], totalLen)
		totalLen += written
		if truncated {
			return sb.String()
		}
	}
	return sb.String()
}

// groupDrawersByRoom buckets drawers by their room name.
func groupDrawersByRoom(drawers []memory.Drawer) map[string][]memory.Drawer {
	byRoom := make(map[string][]memory.Drawer)
	for i := range drawers {
		byRoom[drawers[i].Room] = append(byRoom[drawers[i].Room], drawers[i])
	}
	return byRoom
}

// sortedRoomNames returns the room keys in a deterministic order so L1
// wake-up output is stable across calls.
func sortedRoomNames(byRoom map[string][]memory.Drawer) []string {
	rooms := make([]string, 0, len(byRoom))
	for room := range byRoom {
		rooms = append(rooms, room)
	}
	sort.Strings(rooms)
	return rooms
}

// appendRoomSection writes one room's heading plus its drawer snippets to
// sb, stopping as soon as the running L1 character budget would overflow.
// Returns the number of characters written and whether a truncation note
// was emitted (so the caller can stop iterating rooms).
func appendRoomSection(sb *strings.Builder, room string, drawers []memory.Drawer, runningLen int) (int, bool) {
	roomLine := fmt.Sprintf("\n[%s]", room)
	sb.WriteString(roomLine)
	written := len(roomLine)
	runningLen += written

	for i := range drawers {
		entry := drawerLine(&drawers[i])
		if runningLen+len(entry) > l1MaxChars {
			sb.WriteString(l1TruncationNote)
			return written + len(l1TruncationNote), true
		}
		sb.WriteString(entry)
		written += len(entry)
		runningLen += len(entry)
	}
	return written, false
}

// drawerLine formats a single drawer for the L1 essential story.
func drawerLine(d *memory.Drawer) string {
	snippet := strings.ReplaceAll(strings.TrimSpace(d.Content), "\n", " ")
	if len(snippet) > maxSnippetLen {
		snippet = snippet[:maxSnippetLen-3] + "..."
	}
	entry := fmt.Sprintf("\n  - %s", snippet)
	if d.SourceFile != "" {
		entry += fmt.Sprintf("  (%s)", filepath.Base(d.SourceFile))
	}
	return entry
}
