package layers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// renderL2 retrieves on-demand drawers filtered by wing/room.
func renderL2(ctx context.Context, sess database.SessionRunner, drawerRepo drawerStore, workspaceID, wing, room string, limit int) (string, error) {
	if limit <= 0 {
		limit = 10
	}
	drawers, err := drawerRepo.GetByWingRoom(ctx, sess, workspaceID, wing, room, limit)
	if err != nil || len(drawers) == 0 {
		label := ""
		if wing != "" {
			label = "wing=" + wing
		}
		if room != "" {
			if label != "" {
				label += " "
			}
			label += "room=" + room
		}
		return fmt.Sprintf("No drawers found for %s.", label), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## L2 — ON-DEMAND (%d drawers)", len(drawers))

	for i := range drawers {
		d := &drawers[i]
		snippet := strings.ReplaceAll(strings.TrimSpace(d.Content), "\n", " ")
		if len(snippet) > l2MaxSnippetLen {
			snippet = snippet[:l2MaxSnippetLen-3] + "..."
		}
		entry := fmt.Sprintf("\n  [%s] %s", d.Room, snippet)
		if d.SourceFile != "" {
			entry += fmt.Sprintf("  (%s)", filepath.Base(d.SourceFile))
		}
		sb.WriteString(entry)
	}

	return sb.String(), nil
}
