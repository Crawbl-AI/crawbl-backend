# Crawbl Backend

## Purpose

Go middleware/orchestrator for Crawbl. Contains both the orchestrator HTTP API and the UserSwarm lifecycle/runtime control-plane. Sits between the Flutter app and each user's agent runtime and owns routing, auth, integrations, billing controls, and auditability. Treat this service as the control plane, not a thin API wrapper.

## Rules

- **Never sleep more than 10 seconds** when running commands or waiting for input.
- Always use the `crawbl` CLI for building, pushing, and deploying images — prefer `crawbl app build` / `crawbl app deploy` over raw docker/kubectl/yq.
- Always source `.env` for credentials: `set -a && source .env && set +a && <command>`. Never hardcode tokens or API keys.
- Keep LLM provider credentials in the backend, not in agent runtime pods. Runtime secrets are injected via ESO-managed Kubernetes Secrets (`envSecretRef`).
- Default model access is platform-managed; BYOK comes later.
- Connected app credentials are per-user and must be revocable.
- Read actions may auto-execute after consent; write actions require approval by default.
- Adapters expose narrow capabilities, never raw API passthrough.
- Cross-user A2A must go through backend mediation, never direct cross-namespace pod access.
- Shared runtime namespaces are the current model — do not reintroduce namespace-per-user assumptions.
- Orchestrator uses Postgres only for persistence — no in-memory repository fallbacks in the main API path.
- Mobile auth uses transport: `X-Token` + device/security headers. `Authorization: Bearer` is only a compatibility path for tooling/dev.
- `UserSwarm.status` is the source of truth for runtime readiness — don't duplicate swarm phase/readiness into Postgres.
- Always read `../crawbl-docs/internal-docs/reference/api/endpoints.md` when changing or adding API endpoints.

## Issue Tracking

- Track backlog in GitHub Issues; update when closed.
- Labels: priority (`P1` critical, `P2` important, `P3` tech-debt) and topic (`streaming`, `memory`, `mobile-api`, `infrastructure`, `performance`, `security`).
- One issue per bug — no bundled summary issues. Plain descriptive titles (no `fix():` / `[topic]` prefixes — labels carry the category).

## Code Structure

- Binaries under `cmd/` (currently: `crawbl`, `crawbl-agent-runtime`, `envoy-auth-filter`, `usage-writer`).
- Domain/application code under `internal/` (`orchestrator`, `agentruntime`, `userswarm`, `memory`, `infra`, `pkg`, `testsuite`).
- Orchestrator API split:
  - `internal/orchestrator/types.go` — shared domain types and constants
  - `internal/orchestrator/repo/` — repository contracts, row types, persistence
  - `internal/orchestrator/service/` — typed service opts, contracts, business logic
  - `internal/orchestrator/server/` — HTTP handlers, request/response DTOs
  - `internal/pkg/` — shared database, error, runtime, HTTP helpers
- **Session / opts / repo pattern.** The HTTP handler creates one `*dbr.Session` per request and passes it down inside a typed opts struct. Service methods take `*XxxOpts`; repo methods take a `SessionRunner` (interface over `*dbr.Session` or `*dbr.Tx`) as first arg. Example:

  ```go
  // internal/orchestrator/service/types.go
  type EnsureDefaultWorkspaceOpts struct {
      Sess   orchestratorrepo.SessionRunner
      UserID string
  }

  // service
  func (s *service) EnsureDefaultWorkspace(ctx context.Context, opts *EnsureDefaultWorkspaceOpts) *merrors.Error {
      ws, err := s.workspaceRepo.ListByUserID(ctx, opts.Sess, opts.UserID)
      // ...
  }

  // repo
  func (r *workspaceRepo) ListByUserID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) ([]*orchestrator.Workspace, *merrors.Error)
  ```

  `SessionRunner` lives in `internal/pkg/database/types.go` and exposes `Select` / `InsertInto` / `Update` / `DeleteFrom` so repos work with either a raw session or a transaction.

- Prefer the dbr query builder (`From` / `Where` / `Set`) over raw SQL in the repo layer.
- Keep `types.go` files for request/response types, constants, and interfaces — don't scatter them across handler files.
- Max 4-5 params per function; group into opts/deps structs. Use typed consts/enums — no magic strings or numbers.
- No `// ----` separator comments; use proper Go doc comments.
- Add new API surface in small vertical slices.
- `internal/infra/` (Pulumi) only bootstraps the DOKS cluster and installs ArgoCD. All Helm charts live in `crawbl-argocd-apps/components/*/chart/` — ArgoCD manages K8s resources after bootstrap. Do not use `crawbl app deploy` for cluster rollouts.

## Architecture & Maintainability

**Layering (Clean Architecture).** Dependency direction is strictly one-way: `server → service → repo → database`. Never import upward.

- `server/` — transport only. Parse, validate, call the service, marshal the response. No business rules, no SQL.
- `service/` — business logic and orchestration. Operates on domain types + repo interfaces. Must not import `server/`, `net/http`, or any DB driver.
- `repo/` — persistence only. Takes typed inputs, returns domain types, talks to `SessionRunner`. No business rules.
- `types.go` — shared domain types with no transport or persistence imports.
- Transport DTOs (`*Request` / `*Response`) never leak past `server/`. Domain types never carry `json:` tags meant only for the wire.

**SOLID, Go-flavoured.**

- **Single Responsibility.** One service per bounded concept, one repo per aggregate. Handlers are transport-only. Split files when a type starts owning multiple reasons to change.
- **Open/Closed.** Extend by adding a new interface implementation or a new method — not by piling boolean flags onto an existing function. If you're about to add a fourth `if opts.LegacyX` branch, reach for a strategy interface instead.
- **Liskov.** Any `SomethingRepo` implementation (Postgres, fake, in-memory test double) must satisfy every caller without type assertions or behavioural carve-outs.
- **Interface Segregation.** Prefer many small interfaces over one wide one. Define interfaces **at the consumer**, not the producer — e.g., `service` declares the minimal repo surface it actually uses; the repo package exports structs, not interfaces.
- **Dependency Inversion.** Services depend on interfaces; `cmd/crawbl/platform/orchestrator/orchestrator.go` wires the concrete implementations. No package should `import` a concrete DB struct across layer boundaries.

**Go practices.**

- Accept interfaces, return concrete structs.
- `context.Context` is always the first parameter on anything that can block, do I/O, or fan out.
- Errors bubble up via `*merrors.Error`; wrap with context (`merrors.Wrap(err, "loading workspace")`). Never `panic` except in unrecoverable startup in `main`.
- No hidden global state. Dependencies go through constructors / opts structs; only `main` assembles them.
- Zero-value-useful structs where practical; explicit constructors (`NewXxx`) when invariants matter.
- Functions stay short and flat — if you're four `if`s deep or past ~60 lines, split it.
- Table-driven tests for anything branchy. Fakes over mocks; test through the public interface.
- No `interface{}` / `any` in the domain layer — use concrete or generic types.
- Exported identifiers get doc comments starting with the identifier name (`// Service orchestrates ...`).

## Local Development

- Postgres-backed via `docker-compose.yaml` and `dockerfiles/`. Use `make setup` then `make run`.
- Migrations at `migrations/orchestrator/` are embedded and run automatically on orchestrator startup via `golang-migrate` — no manual step.
- Install toolchain with `mise install` (pins Go, `protoc`, `yq`, k8s, and cloud tooling in `.mise.toml`).
- Regenerate gRPC bindings from `proto/agentruntime/v1/*.proto` with `make generate`.

## Deploy Workflow

Deploys run **locally** via `crawbl app deploy <component>`. CI (`deploy-dev.yml`) is a validation gate only — it runs e2e + release tagging, not builds or pushes.

```bash
crawbl app deploy platform
crawbl app deploy auth-filter
crawbl app deploy agent-runtime
crawbl app deploy docs
crawbl app deploy website
crawbl app deploy all          # platform + auth-filter
crawbl app deploy platform --tag v1.2.3   # override auto tag
```

Tag is auto-calculated from conventional commits since the last `v*` tag: `feat:` → minor, `!:` → major, otherwise patch. Working tree must be clean and pushed.

Backend components (platform, auth-filter) build the image, push to DOCR (`registry.digitalocean.com/crawbl/`), bump the tag in `crawbl-argocd-apps`, and create a GitHub release — ArgoCD auto-syncs. Docs / website skip the Docker path and run `npm run build` + `wrangler pages deploy` instead.

Makefile shortcuts wrap the same logic: `make deploy-dev`, `make deploy-platform`, etc.

## E2E Testing

`crawbl test e2e` runs godog/Gherkin tests against a live orchestrator. Write steps at the **product level** — never raw HTTP assertions.

- **CI mode**: `https://dev.api.crawbl.com` with `--e2e-token`.
- **Local mode**: port-forward the orchestrator (and optionally postgres), no token needed.

```bash
kubectl port-forward svc/orchestrator 7171:7171 -n backend &
kubectl port-forward svc/backend-postgresql 5432:5432 -n backend &
crawbl test e2e \
  --base-url http://localhost:7171 \
  --database-dsn "postgres://postgres:<PG_PASSWORD>@localhost:5432/crawbl?sslmode=disable&search_path=orchestrator" \
  --verbose --runtime-ready-timeout 4m
```

Get the postgres password:

```bash
kubectl get secret backend-postgresql-auth -n backend -o jsonpath='{.data.postgres-password}' | base64 -d
```

To delete a dev user: remove the user row from the port-forwarded Postgres, then delete the matching `userswarm` CR (find it by annotations/labels in the `userswarms` namespace).

## Observability

- VictoriaMetrics at `dev.metrics.crawbl.com` — metrics storage + Prometheus-compatible query API.
- VictoriaLogs at `dev.logs.crawbl.com` — log storage + query UI. Fluent Bit ships container logs from every namespace.
