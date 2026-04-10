package river

import (
	"context"
	"log/slog"
	"time"
)

const (
	// defaultSoftStopTimeout is the budget for in-flight jobs to drain
	// naturally before we escalate to cancellation.
	defaultSoftStopTimeout = 20 * time.Second
	// defaultHardStopTimeout is the budget for StopAndCancel to force
	// cancellation of stuck jobs before the process exits.
	defaultHardStopTimeout = 10 * time.Second
)

// Shutdown performs the River-recommended three-phase graceful shutdown:
//
//  1. Stop — stop fetching new jobs, wait for in-flight jobs to finish (20s budget).
//  2. StopAndCancel — if soft stop times out, cancel the context of any still-running
//     jobs and give them 10s to return.
//  3. Return — caller continues with the rest of its shutdown sequence.
//
// Safe to call with a nil client (no-op). Workers that do not honor
// ctx.Done() will leave jobs stuck in the running state; River rescues them
// after approximately one hour.
func Shutdown(client *Client, logger *slog.Logger) {
	if client == nil {
		return
	}

	softCtx, softCancel := context.WithTimeout(context.Background(), defaultSoftStopTimeout)
	defer softCancel()
	if err := client.Stop(softCtx); err != nil {
		logger.Warn("river soft stop exceeded deadline, escalating to cancel", "error", err)
		hardCtx, hardCancel := context.WithTimeout(context.Background(), defaultHardStopTimeout)
		defer hardCancel()
		if err := client.StopAndCancel(hardCtx); err != nil {
			logger.Error("river force stop failed", "error", err)
			return
		}
	}
	logger.Info("river client stopped")
}
