# Orchestrator

## Purpose

The orchestrator is the mobile-facing control plane that sits between the Flutter app and each user's agent runtime swarm. It owns auth, workspace provisioning, chat routing, integrations, real-time messaging, usage tracking, and the internal MCP server that agents call back into.

## Layout

- `types.go` — All shared domain types, typed consts (AgentStatus, MessageRole, MessageStatus, etc.), and helper funcs (StatusForRuntime, EnrichAgentStatus). The single source of truth for domain vocabulary.
- `repo/` — Concrete Postgres repository implementations, one sub-package per aggregate (userrepo, workspacerepo, conversationrepo, messagerepo, agentrepo, etc.). Each sub-package **exports a struct** (e.g., `type Repo struct{…}`) and its own `types.go` for row types and query filter/option types. `repo/rows.go` holds DB row structs shared across sub-packages. **Repository interfaces do NOT live here — they are declared at the consumer (service) site.**
- `service/` — Business logic split by domain: authservice, chatservice, workspaceservice, agentservice, integrationservice, mcpservice, auditservice, workflowservice. Each service sub-package declares its **own minimal repo interfaces** (Interface Segregation) in its own `types.go`, listing only the repo methods it actually calls.
- `server/` — HTTP transport layer: `routes.go` wires chi routes, `handler/` contains one file per resource group, `dto/` holds request/response types and domain-to-DTO mappers, `socketio/` is the Socket.IO server for real-time chat, `mcp/` is the internal MCP server for agent callbacks.
- `queue/` — Every River-backed background job, periodic schedule, and outbound event publisher the orchestrator owns. Exactly 5 files: `types.go` (all static — constants, Args types, Worker struct declarations, unified `Deps`, event payloads, helpers), `config.go` (single `NewConfig(Deps)` that registers all 7 workers + queues + cron schedules), `memory_workers.go` (4 memory-domain `Work` methods: process / maintain / enrich / centroid recompute), `orchestrator_workers.go` (3 cross-cutting `Work` methods: usage_write / pricing_refresh / message_cleanup + LiteLLM fetch helpers), `publishers.go` (MemoryPublisher → NATS, UsagePublisher → River insert). See `queue/` alone for every queue / worker / cron in the subsystem.
- `memory/` — MemPalace long-term memory subsystem. Auto-ingest pool (in-process), heuristic + LLM classifiers, 4-layer retrieval stack, Postgres repos with pgvector. See `memory/AGENTS.md` for the full layout + the Phase 0/1/2 pipeline-tier state machine.
- `integration/` — OAuth adapter types for third-party integrations.

## SOLID Rules (CRITICAL — Claude's two most common mistakes in this subsystem)

1. **All persistence calls live in the repo layer.** Any `sess.Select/InsertInto/Update/DeleteFrom` — or any function that takes a `SessionRunner` / `*dbr.Session` / `*dbr.Tx` — must be a method on a repo sub-package struct under `repo/<aggregate>repo/`. Never inline SQL or dbr builder calls in `service/`, `server/handler/`, or `types.go`. If a service needs a query that does not exist, add a method to the matching repo — do not reach for the session from the service.
2. **Interfaces belong at the consumer, not the producer.** Repo sub-packages **export concrete structs**, not interfaces. The service package that consumes a repo declares its own minimal interface listing only the methods it uses, e.g.:
   ```go
   // internal/orchestrator/service/chatservice/types.go
   type workspaceRepo interface {
       ListByUserID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) ([]*orchestrator.Workspace, *merrors.Error)
   }
   ```
   Do NOT put repo interfaces in `repo/<aggregate>repo/types.go`. That file is for row types, filter types, and query options only. Multi-consumer shared interfaces may be promoted to `internal/orchestrator/types.go`, but the default is consumer-side.

## Conventions

**Route registration.** All routes live in `server/routes.go` — one file, no route helpers scattered elsewhere. Public routes go above the `AuthMiddleware` group; authenticated routes go inside it. The `/internal/*` routes use inline HMAC bearer validation (no middleware), not the Firebase auth stack.

**Handler shape.** Handlers are plain functions taking `*handler.Context` and returning `http.HandlerFunc`, not receiver methods. `handler.Context` is created once in `registerRoutes` and shared across all handlers.

**Auth check in handlers.** Every authenticated handler calls `c.CurrentUser(r)` at the top. That helper extracts the principal from context, loads the user from the DB, and immediately returns 403 for banned or soft-deleted users — no per-handler guard needed.

**DTOs stay in `server/dto/`.** Request and response structs (and domain→DTO mapper functions) live in `server/dto/`, one file per resource group. Domain types from `internal/orchestrator` never carry `json:` tags meant only for the wire; domain types that do carry tags (e.g. `Agent`, `Message`) are used directly in responses only because mobile contracts require them — do not add new wire-only tags to domain types.

**Error → HTTP mapping.** Use `handler.HTTPStatusForError(mErr)` — all error-code-to-status-code mapping is centralised there. Do not switch on error codes in individual handlers.

**Response envelopes.** Use `handler.WriteSuccess` for single-object responses (`{"data": ...}`), `handler.WriteMessagesListResponse` for the messages list (`{"data": [...], "pagination": {...}}` flat, no outer envelope), and `handler.WriteError` for errors. Do not call `httpserver` helpers directly from handlers.

**Socket.IO.** Mounted at `/socket.io/` via a `http.ServeMux` in `server.go`, separate from the chi router. Socket event names are typed constants in `server/socketio/types.go`. Per-socket state (auth principal, cancel func) is stored in `socket.Data()` as `socketData`.

**MCP server.** Mounted at `/mcp/v1`. Agent runtime pods call it to push streaming events back; it uses the same HMAC signing key (`CRAWBL_MCP_SIGNING_KEY`) as the `/internal/agents` endpoint.

**Async work.** Background jobs use River (see `queue/`). Do not add goroutines or tickers — enqueue a River job instead.

## Gotchas

- **Seed data via embedded JSON.** Default agents, available models, and integration categories are loaded from `migrations/orchestrator/seed/` at compile time via `GetDefaultAgents()`, `GetAvailableModels()`, and `IntegrationCategories()`. Do not hardcode these inline.
- **Messages list has a non-standard envelope.** `GET /workspaces/{id}/conversations/{id}/messages` returns `{"data": [...], "pagination": {...}}` at the top level — no `{"data": {...}}` wrapper. This is intentional; use `WriteMessagesListResponse`, not `WriteSuccess`.
- **`/internal/agents` is not on the public Envoy HTTPRoute.** Adding a new internal endpoint requires updating the ArgoCD HTTPRoute in `crawbl-argocd-apps`, not just adding it to `routes.go`.
- **`usagerepo.New()` is constructed directly in `registerRoutes`.** It is the only repo instantiated in the server layer; all others are injected via `NewServerOpts`. This is intentional because `UsageRepo` is read-only and has no service wrapper.
- **Socket.IO and MCP handlers are optional.** `NewServer` accepts nil for both; a `http.ServeMux` is only created when at least one is non-nil. Tests that only exercise REST can omit them.
- **`repo/rows.go` holds shared DB row structs.** Sub-packages (e.g. `agentsettingsrepo`) define their own row types locally; only rows shared between multiple sub-packages belong in the top-level `repo/rows.go`.

## Key Files

- `internal/orchestrator/types.go` — Domain types, typed consts, and helper funcs; the vocabulary of the entire subsystem.
- `internal/orchestrator/repo/rows.go` — Shared DB row structs. Sub-packages own their own row types locally; only truly shared rows live here.
- `internal/orchestrator/server/routes.go` — The complete route table; every new REST endpoint is registered here.
- `internal/orchestrator/server/handler/context.go` — `handler.Context`, shared response helpers (`WriteSuccess`, `WriteError`, `HTTPStatusForError`), and `CurrentUser`.
- `internal/orchestrator/server/dto/` — Request/response DTOs and domain-to-wire mappers, one file per resource group.
- `internal/orchestrator/server/socketio/types.go` — Socket.IO event name constants, payload structs, and the `Broadcaster` type.
- `internal/orchestrator/queue/events.go` — `UsageEvent` and `MemoryEvent` — the shared payload types for async pipelines.
- `cmd/crawbl/platform/orchestrator/orchestrator.go` — Wiring: constructs all concrete repos, services, and the server; the only place that assembles the dependency graph.
