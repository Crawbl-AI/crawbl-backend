package background

import (
	"fmt"

	"github.com/riverqueue/river"
	"github.com/robfig/cron/v3"
)

// NewConfig builds the memory-domain-specific River Config: queues,
// workers, and periodic jobs. Takes no *sql.DB — infrastructure concerns
// live in internal/pkg/river. Callers pass the returned config into
// pkgriver.New to construct a live client.
//
// After Phase 0 the auto-ingest hot path runs in-process under
// internal/memory/autoingest, so this config only covers the cold
// classification, maintenance, enrichment, and centroid-recompute
// workers that benefit from durability and cross-pod coordination.
func NewConfig(deps Deps) (*river.Config, error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewProcessWorker(deps))
	river.AddWorker(workers, NewMaintainWorker(deps))
	river.AddWorker(workers, NewEnrichWorker(deps))
	river.AddWorker(workers, NewCentroidRecomputeWorker(deps))

	dailyAtMidnight, err := cron.ParseStandard("@midnight")
	if err != nil {
		return nil, fmt.Errorf("parse maintain schedule: %w", err)
	}

	// Weekly centroid recompute: Sunday 03:00 UTC. Chosen for the same
	// reason as @midnight: low-traffic window, well away from daily
	// maintenance so the two periodic jobs do not contend.
	weeklyCentroid, err := cron.ParseStandard("0 3 * * 0")
	if err != nil {
		return nil, fmt.Errorf("parse centroid schedule: %w", err)
	}

	return &river.Config{
		Logger: deps.Logger,
		Queues: map[string]river.QueueConfig{
			QueueMemoryProcess:  {MaxWorkers: processConcurrency},
			QueueMemoryMaintain: {MaxWorkers: 1},
			QueueMemoryEnrich:   {MaxWorkers: 1},
			QueueMemoryCentroid: {MaxWorkers: 1},
		},
		Workers: workers,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(processSweepInterval),
				func() (river.JobArgs, *river.InsertOpts) {
					return ProcessArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: true},
			),
			river.NewPeriodicJob(
				dailyAtMidnight,
				func() (river.JobArgs, *river.InsertOpts) {
					return MaintainArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(enrichSweepInterval),
				func() (river.JobArgs, *river.InsertOpts) {
					return EnrichArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
			river.NewPeriodicJob(
				weeklyCentroid,
				func() (river.JobArgs, *river.InsertOpts) {
					return CentroidRecomputeArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
		},
	}, nil
}
