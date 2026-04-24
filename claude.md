# Crawbl Backend

## Purpose

Go middleware/orchestrator for Crawbl. Contains both the orchestrator HTTP API and the UserSwarm lifecycle/runtime control-plane. Sits between the Flutter app and each user's agent runtime and owns routing, auth, integrations, billing controls, and auditability. Treat this service as the control plane, not a thin API wrapper.

## Rules

- **Never sleep more than 10 seconds** when running commands or waiting for input.
- Always use the `crawbl` CLI for building, pushing, and deploying images — prefer `crawbl app build` / `crawbl app deploy` over raw docker/kubectl/yq.
- Always source `.env` for credentials: `set -a && source .env && set +a && <command>`. Never hardcode tokens or API keys.
- If you dont know what should be the .env key value, Just create it yourself, we will change later. If required add it to AWS
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
- BEFORE declaring task complete: Run snyk_code_scan tool when a significant change has been made in first party code.
- This should only apply for Snyk-supported coding language
- If any security issues are found based on newly introduced or modified code or dependencies, attempt to fix the issues using the results context from Snyk.
- Rescan the code after fixing the issues to ensure that the issues were fixed and that there are no newly introduced issues.
- Repeat this process until no new issues are found.

## Issue Tracking

- Track backlog in GitHub Issues; update when closed.
- Labels: priority (`P1` critical, `P2` important, `P3` tech-debt) and topic (`streaming`, `memory`, `mobile-api`, `infrastructure`, `performance`, `security`).
- One issue per bug — no bundled summary issues. Plain descriptive titles (no `fix():` / `[topic]` prefixes — labels carry the category).

## Code Structure

- Binaries under `cmd/` (currently: `crawbl`, `crawbl-agent-runtime`, `envoy-auth-filter`).
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
- No nested for loops, try to simplify the algorithms as close as O(1) if possible
- Before creating Cron Jobs, Goroutines, schedualers or smth similar, think first, maybe we can utilize `River` for this purposes.

## Local Development

- Dev/test happens on the live dev cluster. The dev cluster is a Hetzner Cloud VM running k3s (not DOKS) — connect via the k3s kubeconfig provisioned by `crawbl infra update`.
- Use `crawbl setup` to verify your environment, then `crawbl app deploy platform` to build and deploy.
- Migrations at `migrations/orchestrator/` are embedded and run automatically on orchestrator startup via `golang-migrate` — no manual step.
- Install toolchain with `mise install` (pins Go, `protoc`, `yq`, k8s, and cloud tooling in `.mise.toml`).
- Regenerate gRPC bindings from `proto/agentruntime/v1/*.proto` with `crawbl generate`.

## Deploy Workflow

**Dev deploys** run locally via `crawbl app deploy <component>`. The CLI blocks deploys when the active kubectl context is `do-fra1-crawbl-prod` — prod is CI-only.

**Prod deploys** must go through GitHub CI (`deploy-prod.yml`). Never run `crawbl app deploy` targeting prod.

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

CI (`deploy-dev.yml`) is a validation gate only — it runs e2e + release tagging, not builds or pushes.

## E2E Testing

`crawbl test e2e` runs godog/Gherkin tests against a live orchestrator. Write steps at the **product level** — never raw HTTP assertions.

- **CI mode**: `https://api-dev.crawbl.com` with `--e2e-token`.
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

The monitoring stack (VictoriaMetrics, VictoriaLogs, Fluent Bit) is deployed in **both dev and prod** via ArgoCD.

- **Dev (Hetzner k3s)**: VictoriaMetrics, VictoriaLogs, and Fluent Bit are deployed by the `root-k3s-dev/` app-of-apps. Use `kubectl logs` for quick tail access, or query VictoriaLogs directly. For ad-hoc resource checks, `kubectl top pods -n <namespace>` works without the full stack.
- **Prod**: VictoriaMetrics, VictoriaLogs, and Fluent Bit are managed by `root-prod/` and exposed via HTTPRoutes at `metrics.crawbl.com` and `logs.crawbl.com`.

## Go Best Practices

> Distilled from [smallnest/go-best-practices](https://github.com/smallnest/go-best-practices) and its linked sources: Effective Go, Go Code Review Comments, Go Common Mistakes, Uber Go Style Guide, Practical Go (Dave Cheney), go-perfbook, go-advices, Idiomatic Go (Shuralyov), Clean Go. **Project rules in the sections above take precedence where they differ.** This section only adds rules not already covered above.

### Code Style & Formatting

- Run `gofmt`/`goimports` on every file; CI must reject unformatted code.
- Soft line limit ~99 chars; long lines usually mean long names — rename rather than wrap.
- Imports in groups separated by blank lines: stdlib, third-party, internal. Let `goimports` handle it.
- Use `var x Foo` for deliberate zero-value declarations; use `x := ...` for explicit assignments.
- Prefer `switch` over long `if-else` ladders.
- Guard-clause / early-return style: keep the happy path un-indented descending the page; no `else` after `return`/`panic`.
- Declare variables in the narrowest possible scope, close to first use.
- Durations are always `time.Duration` literals (`30 * time.Second`) — never bare `int`/`int64` for time.
- Use raw string literals (backticks) for multi-line or escape-heavy strings.
- Write `_ = f()` (not bare `f()`) when intentionally discarding a return value, so `errcheck`-style linters agree with intent.
- Prefer `s == ""` over `len(s) == 0`.
- Single space after `//` in normal comments. Reserve `//go:` (no space) for compiler directives only.
- Use the canonical spellings: `marshaling`, `canceling`, `canceled` (single `l`).

### Naming

- Identifier length scales with scope: single letters are fine for tight loops, verbose names for package-level symbols.
- Reserve `i`/`j`/`k` for loop indices, `n` for counts, `v` for generic values, `k` for map keys, `s` for strings.
- Use the same name for the same concept everywhere: always `ctx context.Context`, always `db *sql.DB`, etc.
- Receiver names: a 1–2-letter abbreviation of the type, consistent across all methods of that type. Never `self`/`this`/`me`.
- Initialisms keep uniform case: `URL`/`url`, `HTTP`/`http`, `ID`/`id` — never `Url`, `Http`, `Id`. Examples: `ServeHTTP`, `appID`, `userURL`.
- Unexported multi-word initialisms lowercase all letters: `githubToken`, not `gitHubToken`.
- Never shadow builtins (`error`, `len`, `cap`, `copy`, `new`, `make`, `string`, `true`, `false`, …) with local names.
- Drop the type from the variable name: `users`, not `usersMap`; `files`, not `fileList`.
- Exported sentinel errors: `ErrFoo`; unexported: `errFoo`; custom error types: `FooError`.
- Enum types get a distinct named type (`type Status int`), not a type alias. Implement `fmt.Stringer` (consider `go:generate stringer`) so they log as names.

### Package Design

- Name a package for what it **provides**, not what it contains. Avoid meaningless buckets: `util`, `helpers`, `common`, `misc`, `base`, `lib`, `types`, `interfaces`.
- Package names should be unique across the module; rename if two directories would collide.
- The package name is part of every identifier at the call site — do not repeat it inside the package (callers write `chubby.File`, not `chubby.ChubbyFile`).
- Keep the exported API surface small — each exported symbol is a maintenance commitment.
- Avoid package-level mutable state; move it into constructor-wired struct fields.
- `init()` restrictions: no I/O, no goroutine spawning, no network calls, no mutation of globals — prefer explicit construction in `main()`/constructors. Libraries should avoid `init()` entirely.
- `import .` is forbidden outside `_test.go` files. `import _` (side-effect-only) is limited to `main` and tests.

### Interface Design

- Define interfaces **at the consumer**, not the producer. (Already a project rule — restated here because it's the single most common Go design mistake.)
- Keep interfaces small and focused. "The bigger the interface, the weaker the abstraction."
- Return concrete structs from constructors; only return an interface when the caller genuinely needs polymorphism at that boundary. (Already a project rule — restated for emphasis.)
- Verify interface compliance at compile time: `var _ SomeInterface = (*Concrete)(nil)` near the type definition.
- Never use a pointer to an interface (`*io.Reader`) — interfaces already hold a pointer-sized header.
- Avoid embedding exported structs/interfaces in exported types — it leaks implementation details and locks the embedded type's surface into your API forever. Write explicit delegate methods instead.
- Choose pointer vs value receivers by mutation/cost; **never mix both on the same type**. Mixing breaks method set rules and confuses readers.
- Mutex Hat pattern: place `mu sync.Mutex` immediately above the field(s) it protects; separate unrelated fields with a blank line.

### Function & API Signatures

- Avoid adjacent same-type parameters (`func Copy(src, dst string)`) — callers can silently swap them. Introduce helper types or split the function.
- Use the **Functional Options** pattern (`...Option`) for constructors with 3+ optional parameters or foreseeable expansion, instead of overloading opts structs with nilable fields.
- Printf-style format strings should be `const` at package level so static analyzers can check them. Name such functions with an `f` suffix (`Logf`, `Wrapf`).
- Named return values only when the names add documentation value (e.g., multiple identical-type returns). Avoid naked `return` except in very short functions.
- For slices and maps received as arguments: copy before storing in internal state. For slices/maps returned from getters: copy before returning, so callers cannot mutate internal state through aliasing.
- Don't pass `*string`/`*bool`/`*int` "to save bytes" — only pass pointers when the function mutates the pointee or the struct is genuinely large.
- Pre-size collections when the capacity is known: `make([]T, 0, n)`, `make(map[K]V, n)`.
- Always use the comma-ok form on map reads: `v, ok := m[k]`.
- Prefer nil slices (`var xs []T`) over empty literals (`xs := []T{}`), except where JSON `null` vs `[]` distinction matters to the wire format.

### Documentation

- Every exported symbol gets a doc comment starting with the symbol name and ending with a period. (Already project rule — reiterating for emphasis.)
- One idea per comment. Document **what** (observable behavior), **how** (when the mechanism isn't obvious from the code), and **why** (the external constraint driving the design).
- `// TODO(username): ...` — always attribute TODOs so future readers know who to ask.
- Don't comment bad code — rewrite it. If a block needs a comment to explain itself, extract it into a well-named function instead.
- Package doc comment sits directly above the `package` clause with no blank line between them.
- No "implements X" boilerplate on interface methods; only comment methods where behavior diverges from or adds to the interface contract.

### Error Handling

- Error strings are **lowercase and not terminated by punctuation** — they compose into larger log lines (`"reading workspace: %w"`, not `"Reading workspace: %w."`).
- Prefer short noun phrases over "failed to …" stacks: `"new store: %w"` beats `"failed to create new store: %w"`.
- Package-level `var ErrFoo = errors.New("…")` sentinels are preferred over creating errors at call sites — call-site `errors.New`/`fmt.Errorf` escapes to the heap on every invocation.
- Wrap with `%w` to preserve the chain for `errors.Is`/`errors.As`; use `%v` only when intentionally hiding the underlying cause.
- Use custom error types (with the `Error` suffix) only when callers need to extract structured fields via `errors.As`.
- **Handle an error exactly once**: log OR return, never both. Double-logging is the most common cause of log-spam incidents.
- Never use sentinel return values (`-1`, `""`, `nil`) to signal errors — return an explicit `(T, error)` or `(T, bool)`.
- Always check errors on deferred calls that can fail (`defer func() { err = f.Close() }()`). Silent `Close` failures on writable files are a classic data-loss bug.
- Do not `defer` inside a loop — defers accumulate until function return. Refactor into a helper function that defers and is called per iteration.
- `panic` only in genuinely unrecoverable startup failures in `main` or impossible-invariant-violation paths. Tests use `t.Fatal`, not `panic`. `os.Exit`/`log.Fatal*` are restricted to `main()` because `defer` does not run after them.
- Goroutines running independent/untrusted work must `recover()` in a deferred function — an un-recovered panic in any goroutine crashes the whole process.

### Concurrency

- **"Before you launch a goroutine, know when it will stop."** Every goroutine needs an explicit stop signal (ctx cancel or done channel) AND a join point (`sync.WaitGroup` or channel drain). The GC will never collect a goroutine blocked on an unreachable channel — it's a permanent leak.
- Prefer synchronous functions that return their results directly. Let callers add concurrency by invoking them from their own goroutine. Easier to test, no hidden leaks.
- Do not spawn goroutines in `init()` or package-level var initializers. Expose a constructor that returns a struct with an explicit `Close()`/`Stop()` method.
- Loop-variable capture in closures: pre-Go 1.22, either pass as a parameter or shadow with `v := v` before capturing. Go 1.22+ scopes loop vars per iteration automatically — but repo code may still run on older modules, so stay alert.
- Channel sizing rule of thumb: **unbuffered or size 1**. Any larger buffer needs a documented reason; arbitrary `make(chan T, 64)` usually papers over a lifecycle bug.
- Use `chan struct{}` (not `chan bool`) for pure signaling — zero allocation and clearer intent.
- Channel ownership: the creator closes. Never close a channel from the receiver side. Writing to a closed channel panics.
- Use `select` with `default` for non-blocking channel operations.
- Use `sync.Once` for thread-safe one-time init; don't hand-roll with flags/mutexes.
- Avoid `sync.Map` unless you have measured the specific access pattern it's designed for (mostly-read, disjoint keys). A plain `map` + `sync.RWMutex` is almost always simpler and faster.
- Do not embed `sync.Mutex`/`sync.RWMutex` in structs — embedding exposes `Lock`/`Unlock` on the outer type's public API. Always use a named field.
- "Channels orchestrate; mutexes serialize." Use channels to coordinate workflow, mutexes to protect shared state.
- Prefer `go.uber.org/atomic` over raw `sync/atomic` for the typed wrappers (`atomic.Bool`, `atomic.Int64`), which prevent the wrong-type-passed-to-the-wrong-operation class of bugs.
- Always `defer ticker.Stop()` for every `time.NewTicker`. Never use `time.After` inside long-lived loops — it leaks until it fires. Use `time.NewTimer` + `Stop`/`Reset` instead.

### Context

- `ctx` is always the first parameter, never stored in a struct field. (Already a project rule — reiterated because it's the most common context anti-pattern.)
- Pass `ctx` through the full call chain; every function that blocks, does I/O, or fans out must accept one.
- **Always `defer cancel()` immediately** after `context.WithCancel`/`WithTimeout`/`WithDeadline`. Forgetting leaks the context's internal resources.
- Check `ctx.Err()` / `<-ctx.Done()` at natural checkpoints inside long-running loops or batch iterations.
- Do not use context values for business-logic parameters — only for truly request-scoped metadata (trace IDs, auth tokens, deadlines). Business data goes in explicit parameters.

### Performance & Memory

- **Profile before optimizing.** Use `pprof` with the `-base` flag to isolate allocation hotspots against a baseline.
- Reduce heap escapes: every heap alloc costs twice (creation + GC scan). Keep hot paths allocation-free where possible.
- `strconv` is ~2× faster than `fmt.Sprintf` for primitive-to-string conversion.
- `strings.EqualFold(a, b)` beats `strings.ToLower(a) == strings.ToLower(b)` for case-insensitive compare — no allocation.
- `sync.Pool` for short-lived, frequently allocated buffers. **Only pool pointer types** — pooling a non-pointer allocates on every `Get()`. Measure wall-clock impact; a pool that's too large evicts CPU caches.
- Interface method calls use indirect dispatch. Avoid interfaces inside tight inner loops; use concrete types and lift the interface boundary out of the loop.
- Place cheap checks before expensive ones (length check before parse, string compare before regex).
- Hoist loop-invariant conditionals **out** of the loop body rather than branching inside it.
- Delete map keys rather than replacing the whole map — lets the runtime reuse the bucket memory.
- Slice/map argument safety: copy on the way in **and** on the way out to prevent external aliasing from mutating internal state.
- `time.Time` contains a pointer to `time.Location`; for high-volume timestamp storage use `int64` Unix seconds/nanos to dodge the GC scan cost.
- Avoid `interface{}`/`any` in the domain layer **and** in hot paths — boxing via `runtime.convT2E` forces heap allocation. (Already a project rule — restated with the perf rationale.)

### Logging

- Log at boundaries (request entry/exit, external call edges). Domain logic returns errors; boundaries decide to log them.
- **Log OR return — never both.** Double-logging of the same wrapped error is the #1 source of log spam.
- Log-line error fragments are lowercase and unpunctuated so they compose with surrounding context.
- Use `%+v` for structs with field names in diagnostic logs; `%q` or `%#v` in test output to reveal whitespace.
- Implement `fmt.Stringer` on integer enums so they log as names, not raw integers.
- For HTTP client observability, attach a `net/http/httptrace.ClientTrace` to the request context to capture DNS, connection reuse (`GotConnInfo.Reused`), request write, and response read lifecycle.

### Security

- **Never** use `math/rand` or `math/rand/v2` for keys, tokens, session IDs, or any security material. Always use `crypto/rand.Reader`. Encode with `encoding/hex` or `encoding/base64`; in Go 1.22+ prefer `rand.Text()` for text tokens.
- All SQL uses parameterized placeholders. Never build SQL with `fmt.Sprintf`/string concat from any value that could be user-influenced, even if "it's just an internal admin tool".
- Validate all inbound input at the transport boundary. Compile URL/path regex patterns at package init (`regexp.MustCompile`) and reject non-matching requests with a 400/404 early.
- Copy slices/maps received from callers before storing or returning — prevents caller-side mutation from silently corrupting internal state.
- Secrets live in environment variables or the secrets manager — never hardcoded. Named `os.FileMode` constants over raw octal literals for clarity.
- Libraries never `panic` on user-facing error paths; return errors and let the caller decide.
- `govulncheck` blocks CI on high-severity findings. Keep the Go toolchain on the latest minor release — minor releases carry security fixes.

### HTTP

- `main` is thin: parse flags, build the server struct, hand off. No business logic in `main`.
- Handlers take `io.Writer` (not `*os.File`/`http.ResponseWriter`) in the inner layers so they're testable without spinning up an HTTP server.
- `html/template`, never `text/template`, for anything emitted as HTML — XSS-safe by default.
- Templates parsed once at startup with `template.Must(template.ParseFiles(...))`. Never parse per request.
- `http.Error(w, msg, status)` for error responses; `http.Redirect(w, r, url, http.StatusFound)` with explicit status codes.
- `defer resp.Body.Close()` immediately after checking the `http.Get`/`http.Do` error, and fully drain the body (`io.Copy(io.Discard, resp.Body)`) if you're not reading it — partial reads block Keep-Alive reuse and exhaust the connection pool.
- Long-lived loops use `time.NewTimer` + `Stop`/`Reset`, not `time.After` (which leaks until it fires).
- Measure HTTP performance as latency distributions (p50/p95/p99 at given RPS), not averages. `vegeta`/`k6`/`fortio` for load generation.
- Compile-time `var _ http.Handler = (*MyHandler)(nil)` assertion catches silent interface drift.

### Database / SQL

- All DB operations take `ctx` (`QueryContext`, `ExecContext`, `BeginTx`). Set query timeouts via `context.WithTimeout`.
- `defer rows.Close()` immediately after `Query` returns without error, and **always** `rows.Err()` after the iteration loop — iteration errors only surface there.
- Use `sql.NullString` / `sql.NullInt64` / `sql.NullTime` for nullable columns; check `.Valid` before reading.
- Use `sql.Tx` for atomic multi-statement work. Don't mix `database/sql` transaction methods with raw `BEGIN`/`COMMIT` in the same connection.
- Tune `SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime` explicitly for the workload — defaults are rarely optimal. Export `db.Stats()` to metrics so pool exhaustion is visible.
- `sql.Conn` (dedicated connection) for schema DDL, advisory locks, or any work that depends on session state.
- Prepared statements (`db.Prepare`) for statements executed repeatedly in a loop.
- Use parameterized queries — never `fmt.Sprintf` user-derived values into SQL. (Already a project rule — restated for severity.)

### Modules

- Commit both `go.mod` and `go.sum`. Run `go mod tidy` in CI and gate on `git diff --exit-code` — drift should fail the build.
- Semver strictly: `major` for breaking changes, `minor` for additive features, `patch` for fixes. v2+ modules must include `/v2` in the import path.
- Mark broken releases with `retract` in `go.mod` so `go get` skips them.
- Never commit `go.work` — it's a developer-local workspace override, not a shared artifact.
- `replace` directives are only honored in the main module; libraries cannot use them to pin consumer behavior.
- Use `internal/` packages for APIs that are private to the module but shared across its sub-packages.

### Anti-Patterns (Quick Hit List)

- Goroutine with no stop signal or join point → **leak**.
- Loop variable captured in a goroutine closure (pre-Go 1.22) without shadow or parameter pass → **all goroutines observe the last value**.
- `sync.Map` used without the specific access pattern it's designed for → **slower than a plain map + mutex**.
- `chan bool` for signaling → **use `chan struct{}`**.
- `defer` inside a loop → **resources pile up until function return**.
- Logging and returning the same error → **duplicate log noise**.
- Capitalized or period-terminated error strings → **break log composition**.
- `context.Context` stored in a struct field → **invisible cancellation, per-call deadlines impossible**.
- `context.Background()` passed deep into a call chain where a live ctx is available → **cancellation signal lost**.
- Forgetting to call the `cancel` from `context.WithCancel`/`WithTimeout` → **leaks the ctx's internal resources**.
- Business-logic parameters shoved into context values → **invisible, type-unsafe coupling**.
- `fmt.Sprintf` in a hot path → **use `strconv` or `strings.Builder`**.
- Creating errors at call sites (`errors.New`/`fmt.Errorf`) for static sentinels → **every call heap-allocates; make them package-level `var`s**.
- `interface{}`/`any` in the domain layer → **boxes every value through the heap**.
- `time.After` inside a long-lived loop → **timer leak until it fires**.
- Not stopping `time.Ticker` → **goroutine + channel leak**.
- Pooling non-pointer types in `sync.Pool` → **allocates on every `Get()`**.
- `math/rand` for security material → **trivially predictable**.
- `fmt.Sprintf`-built SQL → **SQL injection**.
