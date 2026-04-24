// Package workflowservice provides the workflow execution engine for the Crawbl
// multi-agent system. It manages workflow definitions, creates execution records,
// and runs steps sequentially by calling agent runtimes.
package workflowservice

import (
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/config"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// service implements the workflow execution engine.
type service struct {
	db            *dbr.Connection
	workflowRepo  workflowrepo.Repo
	runtimeClient userswarmclient.Client
	broadcaster   realtime.Broadcaster
}

// MaxWorkflowDuration caps the total wall-clock time for a single workflow
// execution. 30 minutes is well above the longest expected agent chain
// (real workflows complete in seconds to a few minutes), while still ensuring
// a stuck or runaway workflow is cancelled before leaking resources past the
// pod's SIGTERM grace window. Tune this if workflows grow beyond ~100 steps.
const MaxWorkflowDuration = 30 * time.Minute

var (
	// WorkflowCleanupTimeout is the time budget for post-failure DB writes when
	// the workflow context has already been cancelled.
	WorkflowCleanupTimeout = config.ShortTimeout
)

// workflowEmitter wraps the realtime broadcaster for a single workflow
// execution. It captures workspaceID + definition + executionID once so
// per-call sites only need to pass the event name and optional extra
// payload fields.
type workflowEmitter struct {
	broadcaster    realtime.Broadcaster
	workspaceID    string
	workflowID     string
	workflowName   string
	executionID    string
	conversationID string
}

// executeWorkflowStepOpts groups the inputs for executeWorkflowStep.
type executeWorkflowStepOpts struct {
	sess        *dbr.Session
	executionID string
	i           int
	step        workflowrepo.WorkflowStep
	workflowCtx map[string]string
	execution   *workflowrepo.WorkflowExecutionRow
	emitter     *workflowEmitter
	runtime     *orchestrator.RuntimeStatus
}

// handleStepFailureOpts groups the inputs for handleStepFailure.
type handleStepFailureOpts struct {
	sess        *dbr.Session
	executionID string
	i           int
	step        workflowrepo.WorkflowStep
	stepExec    *workflowrepo.WorkflowStepExecutionRow
	execution   *workflowrepo.WorkflowExecutionRow
	emitter     *workflowEmitter
	callErr     error
	durationMs  int
	completedAt time.Time
}
