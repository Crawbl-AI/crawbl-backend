// Package autoingest owns the MemPalace hot-path auto-ingestion pipeline:
// chunk a chat exchange, classify each chunk with the heuristic classifier,
// embed it, dedup against existing drawers, persist, and publish.
//
// The Service is a thin wrapper around an alitto/pond worker pool. It runs
// inside the orchestrator process so the chat-turn critical path pays zero
// river_job inserts per message — the previous memory_autoingest River
// worker created too much write amplification at scale.
package autoingest

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sync/atomic"

	"github.com/alitto/pond/v2"
	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

// Service is the memory auto-ingest entry point used by chatservice.
// Submit is non-blocking: when the pool's bounded queue is full the work
// is dropped with a metric + warn log so the request goroutine never
// waits on a slow embedder.
type Service interface {
	// Submit enqueues one conversation exchange for background ingestion.
	// Returns immediately even when the queue is full.
	Submit(ctx context.Context, work Work)

	// Shutdown stops accepting new work and waits for in-flight tasks
	// to finish, bounded by the supplied context. Called from orchestrator
	// graceful-shutdown before the River client stops.
	Shutdown(ctx context.Context) error

	// Metrics returns pool observability counters (running, waiting,
	// completed, dropped). Cheap to call.
	Metrics() Metrics
}

// Work is one conversation exchange ready to be ingested.
type Work struct {
	WorkspaceID string
	AgentSlug   string
	Exchange    string
}

// Deps bundles every collaborator the pool needs to drive a chunk
// pipeline. CentroidRepo is optional — Phase 2 wires it in; Phase 0/1
// leave it nil and the worker skips the centroid branch cleanly. So is
// MemoryPublisher (disabled cleanly when NATS is down). Repo fields are
// consumer-side interfaces declared in ports.go.
type Deps struct {
	DB              *dbr.Connection
	DrawerRepo      drawerStore
	CentroidRepo    nearestTyper
	Classifier      extract.Classifier
	Embedder        embed.Embedder
	MemoryPublisher *queue.MemoryPublisher
	Logger          *slog.Logger
}

// ErrInvalidDeps is returned by Deps.Validate when a required
// collaborator is nil. NewService panics on this error because missing
// non-optional deps is a wiring bug, not a runtime condition.
var ErrInvalidDeps = errors.New("autoingest: invalid deps")

// Validate reports whether the required collaborators are non-nil.
// DB, DrawerRepo, and Classifier are mandatory; everything else either
// degrades cleanly (CentroidRepo, MemoryPublisher, Embedder) or is
// logger-substituted (Logger).
func (d Deps) Validate() error {
	switch {
	case d.DB == nil:
		return errors.Join(ErrInvalidDeps, errors.New("db is nil"))
	case d.DrawerRepo == nil:
		return errors.Join(ErrInvalidDeps, errors.New("drawer repo is nil"))
	case d.Classifier == nil:
		return errors.Join(ErrInvalidDeps, errors.New("classifier is nil"))
	}
	return nil
}

// Config controls pool sizing. Zero/negative values fall back to the
// package defaults below; both knobs are overridable via env vars in
// the orchestrator wiring.
type Config struct {
	// Workers is the pond pool's max concurrency. Default: 16.
	Workers int
	// QueueSize bounds how many Work items can wait before drops kick
	// in. Default: 1024.
	QueueSize int
}

// Metrics is a cheap snapshot of pool counters used for observability.
type Metrics struct {
	Running        int64
	Waiting        uint64
	Completed      uint64
	Dropped        uint64
	CentroidErrors uint64
}

// Default pool sizing. Keep these aligned with CLAUDE.md operational
// guidance: 16 workers saturate a single embedder provider rate-limit
// bucket; 1024 capacity gives ~1 second of head-room at 1K msg/sec per
// pod before the pool starts dropping.
const (
	defaultWorkers   = 16
	defaultQueueSize = 1024
)

// importanceScale turns the classifier's 0..1 confidence into the
// memory_drawers.importance scale used by the ranking pipeline.
const importanceScale = 5.0

// sentenceBoundary splits text on sentence-ending punctuation followed by
// whitespace. Compiled once at package init; this pattern is always valid.
var sentenceBoundary = regexp.MustCompile(`([.!?])\s+`)

// drawerStore is the drawer subset the auto-ingest worker uses:
// idempotent add for the hot path plus a duplicate-check probe before
// inserting.
type drawerStore interface {
	AddIdempotent(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error
	CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error)
}

// nearestTyper is the centroid subset used by the Phase-2 nearest-type
// classifier. Optional at runtime — the worker no-ops when nil.
type nearestTyper interface {
	NearestType(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32) (memType string, similarity float64, ok bool, err error)
}

// service is the concrete Service backed by a pond.Pool with a bounded
// non-blocking queue. It owns no goroutines of its own beyond the pool
// workers; all lifecycle concerns live in pond.
type service struct {
	pool           pond.Pool
	deps           Deps
	logger         *slog.Logger
	dropped        atomic.Uint64
	centroidErrors atomic.Uint64
	noiseMinLength int
	noisePattern   *regexp.Regexp
}
