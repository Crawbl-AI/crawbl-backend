# Agent Runtime

## Purpose

Go implementation of the per-workspace agent runtime pod. Exposes a gRPC service (Converse bidi stream) that the orchestrator calls to drive multi-agent conversations using Google's ADK-Go framework with a Redis-backed session layer. Durable long-term memory lives in the orchestrator's memory palace (`memory_drawers` and friends), reached from agents through the `memory_add_drawer` MCP tool — the runtime itself is stateless w.r.t. long-term memory.

## Layout

- `proto/v1/` — Generated gRPC bindings for the `AgentRuntime` (Converse) service. Do not edit `.pb.go` files directly; regenerate with `crawbl generate`.
- `server/` — gRPC server wiring: registers the `AgentRuntime` handler, installs the HMAC auth interceptor from `internal/pkg/grpc`, manages graceful shutdown lifecycle.
- `runner/` — ADK runner construction and turn dispatch. `blueprint.go` fetches the workspace agent graph from the orchestrator at startup; `runner.go` routes each Converse turn to the correct per-agent ADK runner; `workflow.go` builds the multi-agent graph.
- `agents/` — Concrete ADK `llmagent` constructors for the three default agents: Manager (root router with SubAgents), Wally (research), Eve (scheduling). All share a single `newLLMAgent` helper.
- `tools/` — Tool catalog and tool implementations. `catalog.go` holds typed name constants and wraps `migrations/orchestrator/seed`; `tools/local/` has in-process tools (`web_fetch`, `web_search_tool`, `files`); `tools/mcp/` bridges orchestrator MCP tools — including the memory palace tools — to the orchestrator's MCP endpoint via HMAC-signed HTTP.
- `model/` — LLM adapter wiring via `adk-utils-go`'s OpenAI client. `registry.go` constructs the adapter from `config.OpenAIConfig`; supports BaseURL override for Ollama/OpenRouter/Azure.
- `session/` — Redis-backed `adksession.Service` implementation. Stores session metadata, events (capped list), and scoped state (app/user/session) as separate Redis keys with configurable TTL.
- `config/` — Typed `Config` struct loaded once at startup from CLI flags and env vars. No globals; passed by value into every subpackage. Reuses `internal/pkg/redisclient` config types.
- `storage/` — DigitalOcean Spaces S3 client backing `file_read`/`file_write` tools. Optional; runtime boots without it when `SpacesConfig` fields are empty.
- `telemetry/` — OpenTelemetry exporter wiring to VictoriaMetrics/VictoriaLogs (follow-up; currently a stub).

## Conventions

**gRPC services** — The Converse handler is a thin struct (`converseHandler`) constructed via a private `newConverseHandler` helper and registered against the `*grpc.Server` in `server/grpc_server.go`. Auth uses the shared HMAC interceptor from `internal/pkg/grpc`; gRPC reflection is always on (reflection paths are auth-exempt).

**Proto regeneration** — Run `crawbl generate` from the repo root after editing any `.proto` file under `proto/agentruntime/v1/`. Never hand-edit `*.pb.go` or `*_grpc.pb.go` files.

**Agent registration** — Every agent slug that appears in Go code must be a typed constant in `agents/defaults.go` (`ManagerName`, `WallyName`, `EveName`). Adding a new agent means: add a constant, add a `NewXxx` constructor (delegate to `newLLMAgent`), register it in `runner.BuildGraph`, and add a per-agent `adkrunner.Runner` entry in `runner.New`.

**Tool registration** — Tool names must be constants in `tools/catalog.go`. Data (display name, description, icon, category, `implemented` flag) lives in `migrations/orchestrator/seed/tools.json`. To add a tool: append a seed entry with `"implemented": false`, land the implementation in `tools/local/` or `tools/mcp/`, then flip the flag to `true`. Never expose unimplemented tools to users — call `ImplementedCatalog()` not `DefaultCatalog()`.

**Blueprint fetch** — At startup, `runner.FetchBlueprint` calls `GET /v1/internal/agents?workspace_id=<id>` on the orchestrator (derived by stripping `/mcp/v1` from `MCPEndpoint`), signed with an HMAC bearer token. A fetch failure is fatal and causes the pod to exit so Kubernetes restarts it.

**Session state scoping** — ADK state keys are partitioned by prefix: `app:` (durable, no TTL), `user:` (durable, no TTL), `temp:` (discarded on `AppendEvent`), and bare keys (session-local, TTL-scoped). `splitStateDeltas` enforces this at every write; do not mix scopes within a single state map.

**Config threading** — `config.Config` is constructed once in `main.go` and passed by value into every subpackage. No global config vars.

## Gotchas

- **Proto edits require `crawbl generate`** — `*.pb.go` files are committed but generated; the workflow gate will catch drift. Always commit the generated files alongside the `.proto` change.
- **Separate binary** — This package tree is built as `cmd/crawbl-agent-runtime`, not as part of the orchestrator binary. Importing it from orchestrator code is a layering violation.
- **Redis session TTL** — Session keys (metadata + events) expire after `RedisSessionTTL` (default 24h). App- and user-scoped state hashes do NOT expire. A client resuming a session after TTL expiry gets a fresh session (ADK `AutoCreateSession=true`); prior events are gone.
- **Event list cap** — `maxSessionEvents = 60` events per session (≈15 turns). `AppendEvent` calls `LTrim` on every write; old events are silently dropped. Callers that need full history must use the orchestrator's memory palace (`memory_drawers`) via the `memory_add_drawer` tool.
- **Partial events skipped** — `AppendEvent` is a no-op for `event.Partial == true` to avoid bloating Redis with streaming SSE chunks. Only finalized events are persisted.
- **HMAC signing key shared** — `MCPSigningKey` secures both the runtime's own gRPC server (inbound HMAC auth) and outbound MCP/blueprint calls. Rotation requires coordinated restart of orchestrator and runtime pods.
- **Blueprint is required to boot** — There is no hardcoded fallback agent graph. A pod that cannot reach the orchestrator's `/v1/internal/agents` endpoint will not serve traffic.

## Key Files

- `internal/agentruntime/doc.go` — Package overview and subpackage index; read first.
- `internal/agentruntime/server/grpc_server.go` — Top-level gRPC server: HMAC auth wiring, service registration, shutdown ordering.
- `internal/agentruntime/runner/runner.go` — `Runner` struct: per-agent ADK runner map, `RunTurn` dispatch logic, session service lifecycle.
- `internal/agentruntime/runner/blueprint.go` — `WorkspaceBlueprint` wire type and `FetchBlueprint` startup call.
- `internal/agentruntime/agents/defaults.go` — Agent slug constants and `NewManager`/`NewWally`/`NewEve` constructors.
- `internal/agentruntime/tools/catalog.go` — All tool name constants and `ImplementedCatalog()` / `DefaultCatalog()` accessors.
- `internal/agentruntime/session/redis.go` — Full Redis session service: key layout, TTL rules, state scope partitioning.
- `internal/agentruntime/config/types.go` — `Config` struct with all fields and their env-var sources.
