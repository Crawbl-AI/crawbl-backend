package queue

import (
	"fmt"

	"github.com/riverqueue/river"
	"github.com/robfig/cron/v3"
)

// NewConfig builds the single river.Config for every background /
// periodic / cron job owned by the orchestrator. Callers pass the
// returned config into pkgriver.New and then riverClient.Start.
//
// Registered workers:
//
//   - memory_process (periodic, 1m) — cold LLM classification sweep
//   - memory_maintain (daily)       — importance decay + prune
//   - memory_enrich (periodic, 10m) — KG backfill for fast-path drawers
//   - memory_centroid_recompute     — weekly Sun 03:00 UTC
//   - usage_write (ad-hoc)          — per-LLM-call ClickHouse billing row
//   - pricing_refresh (daily)       — LiteLLM per-token price mirror
//   - message_cleanup (periodic,1m) — fail stale pending messages
//
// Auto-ingest is NOT a River worker — it runs in-process under
// internal/orchestrator/memory/autoingest so the chat-turn hot path
// pays zero river_job writes per message.
func NewConfig(deps Deps) (*river.Config, error) {
	workers := river.NewWorkers()

	// Memory-domain workers.
	river.AddWorker(workers, NewMemoryProcessWorker(deps))
	river.AddWorker(workers, NewMemoryMaintainWorker(deps))
	river.AddWorker(workers, NewMemoryEnrichWorker(deps))
	river.AddWorker(workers, NewMemoryCentroidRecomputeWorker(deps))

	// Orchestrator cross-cutting workers.
	river.AddWorker(workers, NewUsageWriter(deps))
	river.AddWorker(workers, NewPricingRefresh(deps))
	river.AddWorker(workers, NewMessageCleanup(deps))

	dailyAtMidnight, err := cron.ParseStandard("@midnight")
	if err != nil {
		return nil, fmt.Errorf("parse daily schedule: %w", err)
	}
	// Weekly centroid recompute: Sunday 03:00 UTC. Low-traffic window,
	// well away from daily maintenance so the two periodic jobs do not
	// contend for a worker slot.
	weeklyCentroid, err := cron.ParseStandard("0 3 * * 0")
	if err != nil {
		return nil, fmt.Errorf("parse centroid schedule: %w", err)
	}
	// Message cleanup runs every minute so the mobile UI never hangs on
	// an orphaned pending placeholder for longer than the dedup window.
	everyMinute, err := cron.ParseStandard("* * * * *")
	if err != nil {
		return nil, fmt.Errorf("parse message cleanup schedule: %w", err)
	}

	return &river.Config{
		Logger: deps.Logger,
		Queues: map[string]river.QueueConfig{
			QueueMemoryProcess:  {MaxWorkers: memoryProcessConcurrency},
			QueueMemoryMaintain: {MaxWorkers: 1},
			QueueMemoryEnrich:   {MaxWorkers: 1},
			QueueMemoryCentroid: {MaxWorkers: 1},
			UsageWriteQueue:     {MaxWorkers: usageWorkerConcurrency},
			PricingRefreshQueue: {MaxWorkers: 1},
			MessageCleanupQueue: {MaxWorkers: 1},
		},
		Workers: workers,
		PeriodicJobs: []*river.PeriodicJob{
			// Memory — 1-minute cold classification sweep.
			river.NewPeriodicJob(
				river.PeriodicInterval(memoryProcessSweepInterval),
				func() (river.JobArgs, *river.InsertOpts) {
					return MemoryProcessArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: true},
			),
			// Memory — daily decay + prune.
			river.NewPeriodicJob(
				dailyAtMidnight,
				func() (river.JobArgs, *river.InsertOpts) {
					return MemoryMaintainArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
			// Memory — 10-minute enrichment sweep.
			river.NewPeriodicJob(
				river.PeriodicInterval(memoryEnrichSweepInterval),
				func() (river.JobArgs, *river.InsertOpts) {
					return MemoryEnrichArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
			// Memory — weekly centroid recompute.
			river.NewPeriodicJob(
				weeklyCentroid,
				func() (river.JobArgs, *river.InsertOpts) {
					return MemoryCentroidRecomputeArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
			// Pricing — daily LiteLLM mirror.
			river.NewPeriodicJob(
				dailyAtMidnight,
				func() (river.JobArgs, *river.InsertOpts) {
					return PricingRefreshArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
			// Messaging — 1-minute stale cleanup sweep.
			river.NewPeriodicJob(
				everyMinute,
				func() (river.JobArgs, *river.InsertOpts) {
					return MessageCleanupArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
		},
	}, nil
}
