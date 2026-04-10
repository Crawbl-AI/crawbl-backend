package background

import (
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"github.com/robfig/cron/v3"
)

const (
	// processConcurrency bounds concurrent LLM classify calls inside the
	// memory_process queue. Tunable via orchestrator env in a later phase;
	// kept as a const here until we see real contention.
	processConcurrency = 3

	// processSweepInterval is the safety-net periodic sweep cadence. The
	// primary trigger for memory_process is an ad-hoc Insert from the
	// auto-ingest worker — this interval only catches drawers whose insert
	// slipped through (e.g. crash between AddIdempotent and Insert).
	processSweepInterval = time.Minute
)

// NewConfig builds the memory-domain-specific River Config: queues,
// workers, and periodic jobs. Takes no *sql.DB — infrastructure concerns
// live in internal/pkg/river. Callers pass the returned config into
// pkgriver.New to construct a live client.
func NewConfig(deps Deps) (*river.Config, error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewProcessWorker(deps))
	river.AddWorker(workers, NewMaintainWorker(deps))

	dailyAtMidnight, err := cron.ParseStandard("@midnight")
	if err != nil {
		return nil, fmt.Errorf("parse maintain schedule: %w", err)
	}

	return &river.Config{
		Logger: deps.Logger,
		Queues: map[string]river.QueueConfig{
			QueueMemoryProcess:  {MaxWorkers: processConcurrency},
			QueueMemoryMaintain: {MaxWorkers: 1},
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
		},
	}, nil
}
