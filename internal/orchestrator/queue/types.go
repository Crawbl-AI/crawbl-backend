// Package queue owns every River-backed background job, periodic
// schedule, and outbound event publisher used by the orchestrator.
// All queue / worker / cron logic lives here so the service/ and
// repo/ layers stay free of infrastructure glue.
//
// The layout is intentionally small — five files, one purpose each:
//
//   - types.go             : every static symbol (constants, Args,
//     Kind/InsertOpts, Worker struct
//     declarations, tag + metadata vars,
//     event payloads, helpers)
//   - config.go            : NewConfig(Deps) — builds the single
//     river.Config covering every worker
//   - memory_workers.go    : 4 memory-domain Work() implementations
//     (process, maintain, enrich, centroid)
//   - orchestrator_workers.go : 3 cross-cutting Work() implementations
//     (usage_write, pricing_refresh,
//     message_cleanup) + LiteLLM fetch helpers
//   - publishers.go        : MemoryPublisher (NATS) + UsagePublisher
//     (River insert) + shared event stamper
//
// Auto-ingest is NOT on the River queue list — it runs in-process
// under internal/orchestrator/memory/autoingest so the chat-turn
// critical path pays zero river_job writes per message.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/jobs"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/llmusagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/modelpricingrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/pricing"
)

// Deps bundles every collaborator the River-backed workers need. It is
// the single dependency struct the orchestrator wires once at boot and
// passes to NewConfig; individual workers pick out the fields they use.
//
// Any field may be nil if the matching feature is intentionally
// disabled at this environment (e.g. ClickHouse down → LLMUsageRepo
// nil → the usage_write worker no-ops silently). Workers check their
// own fields; NewConfig does not refuse to boot when one repo is
// missing because partial startup is often the intended mode.
//
// Repo fields are typed against consumer-side interfaces declared in
// ports.go so the queue layer never holds a producer-owned interface.
type Deps struct {
	// Shared infrastructure.
	DB     *dbr.Connection
	Logger *slog.Logger

	// Memory domain.
	DrawerRepo    jobs.DrawerStore
	KGRepo        jobs.KGStore
	CentroidRepo  jobs.CentroidStore
	LLMClassifier extract.LLMClassifier
	Embedder      embed.Embedder

	// Messaging — stale pending message cleanup.
	MessageRepo stalePendingFailer

	// Pricing — daily LiteLLM mirror + in-memory cache refresh.
	ModelPricingRepo modelpricingrepo.Repo
	PricingCache     *pricing.Cache

	// Usage / billing — per-LLM-call ClickHouse writer.
	LLMUsageRepo llmusagerepo.Inserter
}

// River queue names. NewConfig registers a river.QueueConfig for each
// and every Args type's InsertOpts routes its job here.
const (
	QueueMemoryProcess       = "memory_process"
	QueueMemoryMaintain      = "memory_maintain"
	QueueMemoryEnrich        = "memory_enrich"
	QueueMemoryCentroid      = "memory_centroid"
	UsageWriteQueue          = "usage_write"
	PricingRefreshQueue      = "pricing_refresh"
	PricingCacheRefreshQueue = "pricing_cache_refresh"
	MessageCleanupQueue      = "message_cleanup"
)

// Worker concurrency caps. `1` is the default for single-stream periodic
// jobs; memory_process and usage_write fan out to multiple workers.
const (
	memoryProcessConcurrency = 3
	usageWorkerConcurrency   = 4
)

// Cadence + dedup windows. Every periodic job ships with one of these
// so operators can reason about the full schedule from one file.
const (
	memoryProcessSweepInterval = time.Minute
	memoryProcessDedupWindow   = 60 * time.Second

	memoryMaintainDedupWindow = time.Hour

	memoryEnrichSweepInterval = 10 * time.Minute

	memoryCentroidDedupWindow = 24 * time.Hour

	messageCleanupDedupeWindow = 30 * time.Second
	pendingMessageMaxAge       = 5 * time.Minute

	pricingRefreshDedupeWindow = 12 * time.Hour
	litellmFetchTimeout        = 5 * time.Minute
	litellmResponseCap         = 50 << 20 // 50 MB cap on upstream payload

	pricingCacheRefreshInterval     = 10 * time.Minute
	pricingCacheRefreshDedupeWindow = 5 * time.Minute
)

// litellmPricingURL is the upstream JSON mirror of provider/model costs
// the pricing_refresh worker polls daily.
const litellmPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// LiteLLM provider canonicalisation. We mirror only the providers the
// orchestrator actually supports so drift in the upstream list is
// harmless.
const (
	providerOpenAI    = "openai"
	providerAnthropic = "anthropic"
)

// Tag badge sets per job kind. riverui renders these next to the job
// row so operators can group at a glance without parsing the kind
// string.
var (
	tagsMemoryProcess       = []string{"memory", "classify", "periodic"}
	tagsMemoryMaintain      = []string{"memory", "maintain", "daily"}
	tagsMemoryEnrich        = []string{"memory", "enrich", "periodic"}
	tagsMemoryCentroid      = []string{"memory", "centroid", "weekly"}
	tagsUsageWrite          = []string{"orchestrator", "billing", "usage"}
	tagsPricingRefresh      = []string{"orchestrator", "pricing", "daily"}
	tagsPricingCacheRefresh = []string{"orchestrator", "pricing", "cache", "periodic"}
	tagsMessageCleanup      = []string{"orchestrator", "messages", "cleanup"}
)

// Per-job metadata JSON blobs. Attached to every InsertOpts so
// riverui renders a human-readable description column next to each
// job row. Marshalled once at package init so every Insert reuses the
// same byte slice.
var (
	memoryProcessMetadata       = mustMarshalJobMetadata("cold LLM reclassification of raw drawers across active workspaces", "memory", "every 1m")
	memoryMaintainMetadata      = mustMarshalJobMetadata("decay and prune low-importance drawers across active workspaces", "memory", "daily")
	memoryEnrichMetadata        = mustMarshalJobMetadata("backfill KG entities/triples for heuristic/centroid-tier drawers", "memory", "every 10m")
	memoryCentroidMetadata      = mustMarshalJobMetadata("rebuild per-type centroid vectors from the last 90 days of LLM-labelled drawers", "memory", "weekly Sun 03:00 UTC")
	usageWriteMetadata          = mustMarshalJobMetadata("persist one LLM usage event to ClickHouse for billing + analytics", "orchestrator", "ad-hoc per agent turn")
	pricingRefreshMetadata      = mustMarshalJobMetadata("fetch LiteLLM per-token prices and append model_pricing rows when upstream costs drift", "orchestrator", "daily")
	pricingCacheRefreshMetadata = mustMarshalJobMetadata("reload in-memory pricing cache from Postgres model_pricing table", "orchestrator", "every 10m")
	messageCleanupMetadata      = mustMarshalJobMetadata("mark pending messages older than 5m as failed so mobile UI never hangs on orphaned placeholders", "orchestrator", "every 1m")
)

// MemoryProcessArgs triggers a single batch run of jobs.RunProcess
// over all active workspaces. Empty struct: concurrent inserts dedupe
// via the memoryProcessDedupWindow uniqueness window.
type MemoryProcessArgs struct{}

// Kind implements river.JobArgs.
func (MemoryProcessArgs) Kind() string { return "memory_process" }

// InsertOpts carries the queue route, riverui metadata + tags, and the
// dedup window. Concurrent inserts within memoryProcessDedupWindow
// collapse into one real execution.
func (MemoryProcessArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:    QueueMemoryProcess,
		Metadata: memoryProcessMetadata,
		Tags:     tagsMemoryProcess,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: memoryProcessDedupWindow,
		},
	}
}

// MemoryProcessWorker is the River adapter over jobs.RunProcess.
type MemoryProcessWorker struct {
	river.WorkerDefaults[MemoryProcessArgs]
	deps Deps
}

// MemoryMaintainArgs triggers a single batch run of jobs.RunMaintain
// (decay + prune) over all active workspaces.
type MemoryMaintainArgs struct{}

// Kind implements river.JobArgs.
func (MemoryMaintainArgs) Kind() string { return "memory_maintain" }

// InsertOpts routes maintain jobs and dedupes concurrent runs within
// a 1-hour window.
func (MemoryMaintainArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:    QueueMemoryMaintain,
		Metadata: memoryMaintainMetadata,
		Tags:     tagsMemoryMaintain,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: memoryMaintainDedupWindow,
		},
	}
}

// MemoryMaintainWorker is the River adapter over jobs.RunMaintain.
type MemoryMaintainWorker struct {
	river.WorkerDefaults[MemoryMaintainArgs]
	deps Deps
}

// MemoryEnrichArgs triggers a sweep of the memory enrichment worker
// which backfills KG entity/triple links for drawers that took the
// heuristic or centroid fast path.
type MemoryEnrichArgs struct{}

// Kind implements river.JobArgs.
func (MemoryEnrichArgs) Kind() string { return "memory_enrich" }

// InsertOpts routes enrich jobs and dedupes overlapping runs within
// the 10-minute sweep window.
func (MemoryEnrichArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:    QueueMemoryEnrich,
		Metadata: memoryEnrichMetadata,
		Tags:     tagsMemoryEnrich,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: memoryEnrichSweepInterval,
		},
	}
}

// MemoryEnrichWorker is the River adapter over jobs.RunEnrich.
type MemoryEnrichWorker struct {
	river.WorkerDefaults[MemoryEnrichArgs]
	deps Deps
}

// MemoryCentroidRecomputeArgs triggers the weekly centroid rebuild
// over the last 90 days of LLM-labelled drawers. Empty struct: a
// single run processes every memory type in one pass.
type MemoryCentroidRecomputeArgs struct{}

// Kind implements river.JobArgs.
func (MemoryCentroidRecomputeArgs) Kind() string { return "memory_centroid_recompute" }

// InsertOpts routes centroid recompute jobs and dedupes concurrent
// runs within the daily window.
func (MemoryCentroidRecomputeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:    QueueMemoryCentroid,
		Metadata: memoryCentroidMetadata,
		Tags:     tagsMemoryCentroid,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: memoryCentroidDedupWindow,
		},
	}
}

// MemoryCentroidRecomputeWorker is the River adapter over
// jobs.RunCentroidRecompute.
type MemoryCentroidRecomputeWorker struct {
	river.WorkerDefaults[MemoryCentroidRecomputeArgs]
	deps Deps
}

// UsageEvent is the payload a caller fills in per LLM call. It is also
// the River job payload — the Kind() + InsertOpts() methods below make
// it satisfy river.JobArgsWithInsertOpts so the publisher can Insert
// directly without a wrapper type.
type UsageEvent struct {
	EventID             string  `json:"event_id"`
	EventTime           string  `json:"event_time"`
	UserID              string  `json:"user_id"`
	WorkspaceID         string  `json:"workspace_id"`
	ConversationID      string  `json:"conversation_id"`
	MessageID           string  `json:"message_id"`
	AgentID             string  `json:"agent_id"`
	AgentDBID           string  `json:"agent_db_id"`
	Model               string  `json:"model"`
	Provider            string  `json:"provider"`
	PromptTokens        int32   `json:"prompt_tokens"`
	CompletionTokens    int32   `json:"completion_tokens"`
	TotalTokens         int32   `json:"total_tokens"`
	ToolUsePromptTokens int32   `json:"tool_use_prompt_tokens"`
	ThoughtsTokens      int32   `json:"thoughts_tokens"`
	CachedTokens        int32   `json:"cached_tokens"`
	CostUSD             float64 `json:"cost_usd"`
	CallSequence        int32   `json:"call_sequence"`
	TurnID              string  `json:"turn_id"`
	SessionID           string  `json:"session_id"`
}

// Kind implements river.JobArgs.
func (UsageEvent) Kind() string { return "usage_write" }

// InsertOpts routes every usage_write job. Uniqueness is NOT enforced
// at the River layer: each event has its own deterministic event_id
// and ClickHouse deduplicates on primary key, so accidental
// double-enqueues collapse at the database layer.
func (UsageEvent) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:    UsageWriteQueue,
		Metadata: usageWriteMetadata,
		Tags:     tagsUsageWrite,
	}
}

// UsageWriter is the River worker that persists a single usage event
// via llmusagerepo. Thin adapter around LLMUsageRepo.Insert.
type UsageWriter struct {
	river.WorkerDefaults[UsageEvent]
	deps Deps
}

// PricingRefreshArgs is the empty River job payload for the daily
// pricing refresh. No per-job arguments — the worker always polls
// LiteLLM and diffs against the latest stored prices.
type PricingRefreshArgs struct{}

// Kind implements river.JobArgs.
func (PricingRefreshArgs) Kind() string { return "pricing_refresh" }

// InsertOpts routes pricing_refresh jobs and dedupes overlapping runs
// within a 12-hour window so concurrent ad-hoc enqueues collapse into
// one execution.
func (PricingRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:    PricingRefreshQueue,
		Metadata: pricingRefreshMetadata,
		Tags:     tagsPricingRefresh,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: pricingRefreshDedupeWindow,
		},
	}
}

// PricingRefresh is the River worker that pulls LiteLLM pricing and
// appends new rows to model_pricing when upstream values have drifted.
type PricingRefresh struct {
	river.WorkerDefaults[PricingRefreshArgs]
	deps Deps
}

// PricingCacheRefreshArgs is the empty River job payload for the
// short-interval in-memory cache refresh. No per-job arguments — the
// worker always reloads the latest model_pricing rows from Postgres.
type PricingCacheRefreshArgs struct{}

// Kind implements river.JobArgs.
func (PricingCacheRefreshArgs) Kind() string { return "pricing_cache_refresh" }

// InsertOpts routes pricing_cache_refresh jobs and dedupes overlapping
// enqueues inside a 5-minute window.
func (PricingCacheRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:    PricingCacheRefreshQueue,
		Metadata: pricingCacheRefreshMetadata,
		Tags:     tagsPricingCacheRefresh,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: pricingCacheRefreshDedupeWindow,
		},
	}
}

// PricingCacheRefreshWorker is the River worker that reloads the
// in-memory pricing cache from Postgres on a short interval, replacing
// the previous time.Ticker goroutine in pricing.Cache.
type PricingCacheRefreshWorker struct {
	river.WorkerDefaults[PricingCacheRefreshArgs]
	deps Deps
}

// litellmEntry mirrors the shape of a single model in the upstream
// LiteLLM JSON. Fields are pointers so zero values are distinguishable
// from missing fields.
type litellmEntry struct {
	InputCostPerToken  *float64 `json:"input_cost_per_token"`
	OutputCostPerToken *float64 `json:"output_cost_per_token"`
	CacheReadInput     *float64 `json:"cache_read_input_token_cost"`
	Provider           string   `json:"litellm_provider"`
	Mode               string   `json:"mode"`
}

// MessageCleanupArgs is the empty River job payload for the periodic
// stale-pending-message cleanup. No per-job arguments — the worker
// always applies the same time cutoff to the whole messages table.
type MessageCleanupArgs struct{}

// Kind implements river.JobArgs.
func (MessageCleanupArgs) Kind() string { return "message_cleanup" }

// InsertOpts routes cleanup jobs and dedupes overlapping enqueues
// inside a 30-second window.
func (MessageCleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:    MessageCleanupQueue,
		Metadata: messageCleanupMetadata,
		Tags:     tagsMessageCleanup,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: messageCleanupDedupeWindow,
		},
	}
}

// MessageCleanup is the River worker that marks stale pending messages
// as failed. It replaces the previous chatservice ticker goroutine.
type MessageCleanup struct {
	river.WorkerDefaults[MessageCleanupArgs]
	deps Deps
}

// MemoryEvent is the payload published on NATS each time a new raw
// drawer is inserted by the auto-ingest hot path. Consumers use it to
// kick off downstream distillation, analytics, or audit pipelines.
type MemoryEvent struct {
	EventID     string `json:"event_id"`
	EventTime   string `json:"event_time"`
	WorkspaceID string `json:"workspace_id"`
	DrawerID    string `json:"drawer_id"`
	Wing        string `json:"wing"`
	Room        string `json:"room"`
	MemoryType  string `json:"memory_type"`
	AgentID     string `json:"agent_id"`
	ContentLen  int    `json:"content_len"`
}

// mustMarshalJobMetadata encodes a fixed-shape job metadata object into
// a byte slice at package init. Panics on encode failure because the
// inputs are string literals — a failure here is a programmer error.
//
// Schema (kept small so the riverui dashboard column stays readable):
//
//	{
//	  "description": "one-sentence what this job does",
//	  "component":   "memory | orchestrator | ...",
//	  "cadence":     "periodic | daily | weekly"
//	}
func mustMarshalJobMetadata(description, component, cadence string) []byte {
	payload := map[string]string{
		"description": description,
		"component":   component,
		"cadence":     cadence,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("queue: marshal job metadata: %v", err))
	}
	return raw
}

// stampEventMetadata fills in a default EventID and EventTime when the
// caller left either blank. Used by every publisher so the uuid +
// RFC3339Nano boilerplate lives in exactly one place.
func stampEventMetadata(id, eventTime string) (string, string) {
	if id == "" {
		id = uuid.NewString()
	}
	if eventTime == "" {
		eventTime = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return id, eventTime
}

// stalePendingFailer is the message subset used by the stale-pending cleanup
// worker: a single UPDATE call that flips orphaned placeholders to
// failed status so the mobile UI never hangs.
type stalePendingFailer interface {
	FailStalePending(ctx context.Context, sess orchestratorrepo.SessionRunner, cutoff time.Time) (int, *merrors.Error)
}
