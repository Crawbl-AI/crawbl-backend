// Package jobs provides standalone K8s CronJob entry points for MemPalace memory processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
)

const decaySkipRecentDays = 7

// MaintainDeps holds dependencies for the memory maintenance job. The
// DrawerRepo field uses the narrow consumer-side contract in ports.go.
type MaintainDeps struct {
	DB         *dbr.Connection
	DrawerRepo DrawerStore
}

// MaintainResult holds the outcome of a maintenance run.
type MaintainResult struct {
	Workspaces int
	Decayed    int
	Pruned     int
}

// RunMaintain performs importance decay and pruning on all active workspaces.
func RunMaintain(ctx context.Context, deps MaintainDeps) (*MaintainResult, error) {
	sess := deps.DB.NewSession(nil)

	activeIDs, err := deps.DrawerRepo.ActiveWorkspaces(ctx, sess, memory.DecayInterval)
	if err != nil {
		return nil, fmt.Errorf("list active workspaces: %w", err)
	}

	if len(activeIDs) == 0 {
		slog.Info("memory-maintain: no active workspaces")
		return &MaintainResult{}, nil
	}

	result := &MaintainResult{Workspaces: len(activeIDs)}

	for _, wsID := range activeIDs {
		decayed, err := deps.DrawerRepo.DecayImportance(ctx, sess, wsID,
			memory.DecayAgeDays,
			decaySkipRecentDays,
			memory.DecayFactor,
			memory.DecayFloor,
		)
		if err != nil {
			slog.Warn("memory-maintain: decay failed", "workspace_id", wsID, "error", err)
			continue
		}
		result.Decayed += decayed

		pruned, err := deps.DrawerRepo.PruneLowImportance(ctx, sess, wsID,
			memory.PruneThreshold,
			memory.PruneMinAccessCount,
			memory.PruneKeepMin,
		)
		if err != nil {
			slog.Warn("memory-maintain: prune failed", "workspace_id", wsID, "error", err)
			continue
		}
		result.Pruned += pruned
	}

	return result, nil
}
