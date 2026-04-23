// Package queue — ports.go declares the narrow repository contracts
// this package depends on. Per project convention, interfaces are defined
// at the consumer, not the producer.
//
// Memory repo interfaces (DrawerStore, KGStore, CentroidStore) are
// defined in internal/orchestrator/memory/jobs and re-used here via
// the jobs import. Only the messageStore interface is unique to this
// package.
//
// Keeping this file package-private means callers cannot accidentally
// widen what the queue layer depends on without also adding a matching
// call site.
package queue

import (
	"context"
	"time"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// messageStore is the message subset used by the stale-pending cleanup
// worker: a single UPDATE call that flips orphaned placeholders to
// failed status so the mobile UI never hangs.
type messageStore interface {
	FailStalePending(ctx context.Context, sess orchestratorrepo.SessionRunner, cutoff time.Time) (int, *merrors.Error)
}
