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
func NewConfig(deps Deps) (*river.Config, error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewProcessWorker(deps))
	river.AddWorker(workers, NewMaintainWorker(deps))
	river.AddWorker(workers, NewAutoIngestWorker(deps))

	dailyAtMidnight, err := cron.ParseStandard("@midnight")
	if err != nil {
		return nil, fmt.Errorf("parse maintain schedule: %w", err)
	}

	return &river.Config{
		Logger: deps.Logger,
		Queues: map[string]river.QueueConfig{
			QueueMemoryProcess:    {MaxWorkers: processConcurrency},
			QueueMemoryMaintain:   {MaxWorkers: 1},
			QueueMemoryAutoIngest: {MaxWorkers: autoIngestConcurrency},
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
