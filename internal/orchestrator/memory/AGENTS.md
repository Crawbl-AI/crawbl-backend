# MemPalace Memory Subsystem

## Purpose

MemPalace is Crawbl's automatic long-term memory system. It captures every user-agent exchange on the chat-turn hot path, classifies it cheaply without an LLM when possible, stores the verbatim content into a workspace-scoped "palace" of wings/rooms, and serves it back through a 4-layer token-budgeted retrieval stack on the next conversation. The whole subsystem lives under the orchestrator because it is only consumed by `chatservice`, `mcpservice`, and `agentservice`; no other binary touches it.

## Layout

- `types.go` — All shared domain types, typed consts, and constants: `Drawer`, `Entity`, `Triple`, `Identity`, `HybridSearchResult`, `MemoryTypeCentroid`, the pipeline-tier enum (`PipelineTierHeuristic` / `Centroid` / `LLM`), workspace limits, auto-ingest tuning, decay constants, and the env-gated `HeuristicConfidenceHigh` / `HeuristicConfidenceLow` kill switches. The vocabulary of the entire subsystem.
- `autoingest/` — In-process chat-turn hot path. A `pond` v2 worker pool receives conversation exchanges via `Submit`, chunks them, runs the heuristic classifier, embeds each chunk, dedupes against existing drawers, routes via `pickTier` (heuristic → centroid → LLM), persists, and publishes a NATS `MemoryEvent`. Non-blocking: when the bounded queue is full, the Submit drops the work with a counter + warn log instead of backpressuring chatservice. **This replaces the former `memory_autoingest` River queue** — the plan at `.omc/plans/mempalace-heuristic-knn-phase1-2.md` explains why running it in-process removes per-chat-turn `river_job` writes.
- `extract/` — Classifiers. `classify.go` is the heuristic regex/marker classifier loaded from `config/classify_patterns.json`; `llm_classify.go` is the OpenAI-compatible LLM classifier used by the cold `memory_process` + `memory_enrich` workers.
- `config/` — Embedded tuning JSON (`classify_patterns.json`, `noise_patterns.json`) plus the Go loaders that compile them into regex + marker maps at startup.
- `jobs/` — Pure business logic for the River-backed cold pipeline. `process.go` runs LLM reclassification over raw drawers; `maintain.go` runs daily decay + prune; `enrich.go` backfills KG entities/triples for heuristic/centroid-tier drawers; `centroids.go` computes the weekly centroid recompute. The River `Work()` adapters live in `internal/orchestrator/queue/memory_workers.go` — this package is driver-agnostic business logic only.
- `layers/` — The 4-layer retrieval stack. `l0_identity.go` / `l1_essential.go` / `l2_ondemand.go` / `l3_search.go` render each layer; `stack.go` orchestrates them; `retrieval.go` holds `HybridRetrieve` (vector + KG hybrid search) + `rankHybridResults`. Hard cap 14k characters on total output.
- `repo/` — Persistence contracts. `types.go` declares the `DrawerRepo`, `KGRepo`, `PalaceGraphRepo`, `IdentityRepo`, and `CentroidRepo` interfaces; each sub-package (`drawerrepo/`, `kgrepo/`, `palacegraphrepo/`, `identityrepo/`, `centroidrepo/`) holds the Postgres implementation and any local row types. Follows the orchestrator's "interfaces at the consumer" rule: all interfaces live in the one shared `repo/types.go` because every memory consumer uses the same contract.

## Pipeline Tiers (CRITICAL — name collision callout)

MemPalace has TWO orthogonal concepts both informally called "tiers / layers":

1. **`pipeline_tier`** (column on `memory_drawers`) — which arm of the cold pipeline labelled a drawer: `heuristic` / `centroid` / `llm`. Controls whether the drawer needs enrichment and whether it feeds the centroid recompute.
2. **Retrieval layers L0/L1/L2/L3** (in-memory structure built by `layers/stack.go`) — which token budget the drawer lands in when building conversation context.

These two are **unrelated**. Pipeline tiers describe how a drawer was classified; retrieval layers describe how it is served back. The plan in `.omc/plans/mempalace-heuristic-knn-phase1-2.md` §0.3 documents the naming decision. Never conflate the two in logs, docs, or code comments.

## Conventions

**Hot path is in-process.** Auto-ingest runs inside the orchestrator binary under `autoingest/`, not as a River job. `chatservice.autoIngestConversation` calls `ingestPool.Submit(ctx, autoingest.Work{...})` after finalising each agent turn. Never reintroduce a River queue for the hot path — the plan explicitly rejected that because it wrote one `river_job` row per chat turn.

**Cold pipeline is on River.** Everything else (process, maintain, enrich, centroid recompute) is a River worker registered in `internal/orchestrator/queue/config.go`. The `jobs/` package holds the pure business logic; the `queue/memory_workers.go` file holds the thin River adapter. Do not collapse the two — the split lets tests exercise `jobs.RunProcess` etc. without spinning up River.

**Heuristic kill switch.** `memory.HeuristicConfidenceHigh` and `memory.HeuristicConfidenceLow` are package-level `var` (not `const`) so operations can flip them via env var at boot: `CRAWBL_MEM_HEURISTIC_HIGH` and `CRAWBL_MEM_HEURISTIC_LOW`. Defaults are `HeuristicKillSwitchValue` (999.0) which forces every chunk into the LLM branch — Phases 1 and 2 are disabled out of the box. The kill switch is read ONCE at package init, so rolling back requires a pod restart.

**Deps.Validate at construction.** `autoingest.NewService` panics if `Deps.DB`, `Deps.DrawerRepo`, or `Deps.Classifier` are nil — a wiring bug should fail boot, not produce silent drawer-less ingestion in production. Optional deps (`CentroidRepo`, `MemoryPublisher`, `Embedder`) degrade cleanly when nil.

**Centroid training filter.** The weekly centroid recompute (`jobs/centroids.go`) trains ONLY on drawers where `pipeline_tier = 'llm'` to prevent a feedback loop. Never include `heuristic` or `centroid` drawers in the training cohort; they are our own predictions.

**Sample-count gate.** `NearestType` in `centroidrepo` honours `MemoryCentroidMinSamples` (50) — centroids below that threshold are ignored entirely so cold-start workspaces cannot be dominated by a low-cohort type. On first deploy the table is empty and Phase 2 is dormant until the weekly recompute finds enough data.

**Startup seed bounded.** `orchestrator.go` runs `jobs.RunCentroidRecompute` once at boot under `centroidSeedTimeout` (30s) as a best-effort warm-up so Phase 2 is not dormant until Sunday. Failures log and move on — a broken pgvector install must never gate orchestrator boot.

**Partial index predicate is load-bearing.** `idx_drawers_enrich` (migration 000010) has the exact predicate `WHERE state='processed' AND pipeline_tier<>'llm' AND entity_count=0 AND importance>=3.0`. The `DrawerRepo.ListEnrichCandidates` SQL must match this predicate exactly or the index stops being sargable. Do not change one without the other.

**Repo layer owns SQL.** All pgvector / raw SQL lives under `repo/<sub>repo/postgres.go`. Higher layers (`jobs/`, `layers/`, `autoingest/`) talk through the `DrawerRepo` / `KGRepo` / `CentroidRepo` / `IdentityRepo` / `PalaceGraphRepo` interfaces in `repo/types.go`. Never import `pgvector-go` or build dbr queries outside the repo package — `jobs/centroids.go` used to do that and was moved into `drawerrepo.ListCentroidTrainingSamples` for exactly this reason.

**PipelineTier set at Drawer construction.** Every call site that constructs `*memory.Drawer` (autoingest worker, mcp `tools_memory.go`, `agentservice/agents.go`) must set `PipelineTier` explicitly. There is no Go-side fallback in `drawerrepo.Add` — the database column default is `'llm'`, but the INSERT statement always supplies the column explicitly, so an empty string would break the partial-index predicate.

## Gotchas

- **Workspace limit enforced at write time.** `drawerrepo.Add` checks `MaxDrawersPerWorkspace` (10,000) before every insert. Hitting it returns a plain error; higher layers surface it as a hard cap. This is why pgvector sequential scan stays fast — the workspace is bounded.
- **pgvector index is a no-op.** Migration 000008 intentionally does NOT create an IVFFlat or HNSW index on `memory_drawers.embedding` — both crashed the DigitalOcean Postgres backend with SIGILL on CPUs without AVX2. We rely on sequential scan, which is acceptable at the 10k-per-workspace ceiling. The file has a detailed comment explaining the three workarounds if we ever get AVX2-capable nodes.
- **Non-concurrent CREATE INDEX.** Migration 000010 uses plain `CREATE INDEX` for `idx_drawers_enrich`, not `CONCURRENTLY`, because golang-migrate v4 wraps every migration in a transaction and CONCURRENTLY is forbidden inside one. The partial-index predicate is narrow and memory_drawers is capped, so the ACCESS EXCLUSIVE lock window is short. PG11+ also fast-paths the `ADD COLUMN ... DEFAULT <constant> NOT NULL` pattern to O(1) metadata-only, so that ALTER does not rewrite the table.
- **Cold path is decoupled from user latency.** The ad-hoc `ProcessArgs` follow-up enqueue was removed from the autoingest hot path after Phase 0 — the 1-minute periodic sweep is the only trigger now. Raw drawers wait up to 60s before cold reclassification. High-confidence heuristic and centroid drawers bypass the cold pipeline entirely.
- **Enrichment is per-drawer, not batched.** `jobs/enrich.go` calls `LLMClassifier.ClassifyAndExtract` per drawer with a 15s timeout and soft-fails so one bad row never stops the sweep. Batching was considered but rejected because batch failure semantics do not compose with the "one bad row must not stop the sweep" requirement.
- **`hardSplit` slices by rune, not byte.** The auto-ingest chunker's hard-split fallback uses `[]rune` to avoid landing mid-rune on CJK / emoji input — otherwise the embedder rejects invalid UTF-8.
- **langchaingo/textsplitter swap was attempted and reverted.** The reuse audit recommended replacing `chunkText` with `textsplitter.RecursiveCharacter` but the swap force-upgraded `aws-sdk-go-v2/service/s3` by 25 minor versions as a transitive dep. The hand-rolled chunker stays until someone can isolate the swap without dragging in tiktoken-go + commonmark + AWS SDK churn. A comment on `chunkText` documents the rejection so future contributors do not re-try blindly.
- **`memory_drawers.id` is not composite.** The primary key is just `TEXT`, not `(workspace_id, id)`. Workspace isolation relies on code (every repo query filters by `workspace_id`), not the schema. Adding a new query path means auditing the WHERE clause.
- **jdkato/prose is rejected for classify.** Upstream archived May 2023. Phase 3 may revisit NER libraries but Phases 0-2 stay on the current keyword-marker approach. See `.omc/plans/mempalace-heuristic-knn-phase1-2.md` §10.1 for the rejection rationale.

## Key Files

- `types.go` — Domain types, tier constants, env-gated kill switches, `Drawer` struct (including `PipelineTier`, `EntityCount`, `TripleCount`), `MemoryTypeCentroid`, and the centroid tuning constants. Start here when touching any of the schema.
- `autoingest/service.go` — `NewService`, `Submit`, `Shutdown`, `Metrics`, and the pond wiring. Entry point for the hot path.
- `autoingest/worker.go` — `runChunkPipeline`, `ingestChunk`, and `pickTier` (the three-way heuristic → centroid → LLM decision). The whole Phase 0/1/2 state machine lives in `pickTier`.
- `autoingest/helpers.go` — `chunkText`, `splitSentences`, `assembleChunks`, `hardSplit`, `buildDrawer` (takes tier+state args), `autoIngestDrawerID`, `isNoise`.
- `extract/classify.go` — Heuristic classifier: marker-count scoring, sentiment lookup, resolution disambiguation, speaker-turn segmentation.
- `extract/llm_classify.go` — OpenAI-compatible LLM classifier used by `jobs.RunProcess` and `jobs.RunEnrich`. `ClassifyAndExtract` (per-drawer) and `ClassifyBatch` (cold path batching).
- `repo/types.go` — The 5 repo interfaces in one file: `DrawerRepo`, `KGRepo`, `PalaceGraphRepo`, `IdentityRepo`, `CentroidRepo`. Consumers in `service/`, `layers/`, `jobs/`, and `autoingest/` all hold them as interfaces.
- `repo/drawerrepo/postgres.go` — Every `memory_drawers` SQL path, including `SearchHybrid` (single-CTE pgvector ANN ∪ KG entity-name lookup), `ListEnrichCandidates` (matching the partial index predicate), and `ListCentroidTrainingSamples` (for the weekly recompute).
- `repo/centroidrepo/postgres.go` — `NearestType` (pgvector `<=>` with `sample_count >= 50` gate), `Upsert` with `source_hash IS DISTINCT FROM` guard so no-op runs don't churn rows, `GetAll` for the recompute.
- `repo/palacegraphrepo/postgres.go` — BFS traversal, tunnel detection, graph stats. Redis-cached per workspace via `cache.go`; nil-safe Redis client for local dev.
- `jobs/process.go` — Cold LLM reclassification loop; `jobs/enrich.go` — KG backfill sweep; `jobs/centroids.go` — weekly recompute with in-Go averaging + MD5 source hash.
- `layers/retrieval.go` — `HybridRetrieve` (calls `drawerRepo.SearchHybrid` once with extracted query terms) + pure `rankHybridResults` (importance × recency × relevance + agent affinity).
- `layers/stack.go` — `Stack.BuildContext`: assembles L0 + L1 + L2 + L3 under the 14k-char token budget, truncating outer layers first when space runs out.
- `.omc/plans/mempalace-heuristic-knn-phase1-2.md` — The plan document for Phases 0/1/2 cost reduction. Read it before changing the pipeline-tier state machine or the centroid recompute.
- `internal/orchestrator/queue/memory_workers.go` — River adapters for the four memory cold-path jobs. If you change `jobs.Run*` signatures, this is the file to update.
- `internal/orchestrator/queue/config.go` — Where all seven River workers (four memory, three cross-cutting) are registered into one `river.Config`.
