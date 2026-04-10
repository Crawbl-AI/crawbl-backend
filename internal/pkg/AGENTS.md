# Internal Shared Packages

## Purpose

Shared infrastructure utilities and thin wrappers for the orchestrator and runtime binaries. NOT a dumping ground — each sub-package owns a single, narrow responsibility and must not encode business rules.

## Layout

**Data & Persistence**
- `database/` — dbr connection pool, `SessionRunner` interface, connection config and defaults
- `clickhouse/` — ClickHouse client wiring for analytics writes
- `redisclient/` — Redis client construction and config
- `embed/` — embedded file helpers (migrations, static assets)

**Infrastructure & Runtime**
- `httpserver/` — HTTP server bootstrap, auth middleware, request/response envelope helpers, typed header constants
- `grpc/` — gRPC server/client construction helpers
- `kube/` — Kubernetes client construction (used by runtime and operator paths)
- `argocd/` — ArgoCD API client wrapper
- `river/` — River job queue client wiring and migration helpers; worker implementations live in `internal/memory/background/`
- `crawblnats/` — NATS connection and subscription helpers
- `realtime/` — real-time event broadcast abstractions (Socket.IO / pub-sub)
- `runtime/` — shared runtime lifecycle utilities
- `configenv/` — typed environment variable loading

**Observability**
- `telemetry/` — OpenTelemetry tracer and metrics provider setup
- `release/` — release/version metadata surface for observability and healthchecks
- `versioning/` — version string parsing and comparison

**Crypto & Security**
- `hmac/` — HMAC signing and verification helpers
- `firebase/` — Firebase Admin SDK client construction (JWT verification)

**Errors**
- `errors/` — `*merrors.Error` type, `ServerError`/`BusinessError` discrimination, typed error codes by domain (AUTH, USR, WSP, …)

**Filesystem & Tooling**
- `fileutil/` — filesystem helpers
- `gitutil/` — Git metadata helpers (commit SHA, branch)
- `yamlvalues/` — YAML value extraction helpers (used by deploy/config tooling)
- `cli/` — shared CLI flag/command helpers
- `pricing/` — LLM pricing tables and token-cost calculations

## Conventions

- Every sub-package has exactly one responsibility. If you find yourself adding a second unrelated concern to an existing package, create a new package instead.
- Sub-packages **must not import** `internal/orchestrator`, `internal/agentruntime`, or any other domain package. Dependency direction is always domain → pkg, never the reverse.
- Expose construction via `New(…)` constructors or typed `Config`/`Opts` structs. No naked global `init()` side-effects.
- `database/types.go` defines `SessionRunner` — the interface that decouples repo layer code from raw `*dbr.Session` vs `*dbr.Tx`. All repo methods accept `SessionRunner` as their first argument; services never import `database/` directly.
- Prefer framework-agnostic implementations. Avoid leaking `net/http`, `grpc`, or Socket.IO types into packages where they are not the core concern.
- Use typed constants (see `errors/types.go`, `httpserver/types.go`). No magic strings or bare integers.

## Gotchas

- **No generic packages.** Never add a package named `util`, `helper`, `common`, or `misc`. Pick a specific noun that names the responsibility.
- **No upward imports.** `internal/pkg/` must never import `internal/orchestrator/` or `internal/agentruntime/`. Violating this breaks clean architecture and creates import cycles.
- **River is infrastructure here, not behaviour.** `river/` wires the queue client and runs migrations; job args, workers, and scheduling logic belong in `internal/memory/background/`. See the River-first rule in root `CLAUDE.md` before reaching for goroutines or cron jobs.
- **`errors/` is the only error type.** Use `*merrors.Error` everywhere. Do not introduce `fmt.Errorf`-only paths in service or repo layers — wrap with `merrors.Wrap` so callers can discriminate `ServerError` vs `BusinessError`.

## Key Files

- `database/types.go` — `SessionRunner` interface and `Config` struct; the repo-layer abstraction every repository depends on
- `errors/types.go` — `Error` struct, `ErrorType` enum, all domain error codes (`AUTH*`, `USR*`, `WSP*`, …) and predefined sentinel errors
- `errors/errors.go` — `NewBusinessError`, `NewServerErrorText`, `Wrap`, `IsCode` constructors and helpers
- `httpserver/types.go` — typed HTTP header constants, `RequestMetadata`, auth token source enum, response envelope shapes
- `httpserver/middleware.go` — Firebase JWT + X-Token auth middleware; sets principal and metadata on request context
- `httpserver/response.go` — `WriteSuccessResponse` / `WriteErrorResponse` — enforces the `{"data":…}` envelope contract
- `river/client.go` — `New(db, cfg)` constructor that wires the `database/sql` River driver; exposes `Client` and `Config` type aliases
- `river/migrate.go` — River schema migration runner (called at orchestrator startup alongside golang-migrate)
