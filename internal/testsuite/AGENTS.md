# Test Suite (Godog E2E)

## Purpose

Product-level end-to-end tests for the Crawbl orchestrator, written in Gherkin and executed by godog against a live environment. Scenarios describe observable user behaviour, never internal HTTP mechanics.

## Layout

- `e2e/` — all Go source for the suite
  - `e2e.go` — suite entry point: `Config`, `testContext`, `suiteUsers`, `Run()`, scenario wiring
  - `helpers.go` — shared `testContext` methods (data fetchers, capture helpers, message senders, `normalizeKey`)
  - `poll.go` — runtime-readiness polling loop used by steps that wait for a swarm to become ready
  - `steps_auth.go` — sign-up, login, token, logout steps
  - `steps_agent.go` — agent catalog, agent details, agent conversation steps
  - `steps_assert.go` — generic status code and JSON body assertion steps
  - `steps_chat.go` — message send, conversation open, response wait steps
  - `steps_db.go` — direct Postgres assertion steps (skipped when `--database-dsn` is absent)
  - `steps_health.go` — health-check and runtime-ready steps
  - `steps_http.go` — low-level HTTP dispatch helpers (`userSendsGet`, `doRequest`, `interpolatePath`)
  - `steps_integrations.go` — OAuth integration CRUD steps
  - `steps_isolation.go` — cross-user IDOR and data-isolation assertion steps
  - `steps_redis.go` / `steps_spaces.go` — infrastructure assertion steps (no-op when not configured)
  - `steps_state.go` — `tc.saved` key capture and recall (`save response field … as …`)
  - `steps_user.go` / `steps_workspace.go` — profile, push-token, deletion, workspace CRUD

- `test-features/` — Gherkin feature files, one subfolder per domain
  - `auth/` — sign-up, logout, cleanup, auth error cases
  - `chat/` — messaging, conversations CRUD, agent interactions, session continuity, mentions
  - `core/` — health, legal, models, upload contract, edge cases
  - `integrations/` — OAuth integration lifecycle
  - `multi_user/` — cross-user isolation and IDOR scenarios
  - `profile/` — profile read/update, error cases
  - `tools/` — memory, web-search tool scenarios
  - `workspaces/` — workspace lifecycle, mobile first-launch, error cases

## Conventions

- All step definitions live in `e2e/steps_*.go`; each file covers one domain and calls a single
  `registerXxxSteps(sc, tc)` function that is wired in `initScenario` inside `e2e.go`.
- Feature files live under `test-features/<category>/`. Run a single category with
  `crawbl test e2e --category <category>`.
- Shared per-scenario state is held in `testContext` (defined in `e2e.go`). Per-user journey
  state (workspace ID, conversation IDs, agent slugs) lives in `userJourneyState`, accessed
  via `tc.userState(alias)`.
- Exactly 3 test users are created once per suite run (`primary`, `frank`, `grace`) and
  reused across all scenarios. Never create additional users — this caps swarm instances at 3.
- `{key}` placeholders in step paths and JSON bodies are interpolated from `tc.saved` via
  `interpolatePath`. Use the `steps_state.go` "save response field … as …" step to populate them.
- Infrastructure clients (Postgres `dbConn`, `redisClient`, `spacesClient`) are nil when the
  corresponding config flag is absent; every step that uses them must nil-check and skip
  gracefully so `crawbl test e2e --base-url ...` stays green in CI without extra port-forwards.
- The orchestrator `BaseURL` is always sourced from `cfg.BaseURL` (set by `--base-url`). Never
  hardcode a host in a step definition.

## Gotchas

- Never write raw HTTP status assertions in step definitions — always go through the typed
  assertion helpers in `steps_assert.go` (`tc.assertStatus`, `tc.assertBodyContains`, etc.).
- Scenario isolation is logical, not transactional: the 3 users persist across scenarios, so
  steps must be written to be idempotent or use lazy-init bootstrappers (`ensureDefaultWorkspace`,
  `ensureAgentCatalog`, `ensureConversationCatalog`) rather than assuming a clean slate.
- `--runtime-ready-timeout` (default 3 min) controls how long polling steps wait for a swarm to
  reach `ready`. Increase it in slow CI environments; don't lower it below 1 min.
- The CI `E2EToken` is injected as `X-E2E-Token` + `X-E2E-UID/Email/Name` headers; local runs
  use `X-Firebase-*` headers instead. The switch is automatic based on whether `cfg.E2EToken`
  is set — no step code needs to branch on this.
- Adding a new feature: create `test-features/<category>/<name>.feature`; register any new steps
  in a new or existing `steps_<domain>.go` and wire the register call in `initScenario`.

## Key Files

| File | Role |
|------|------|
| `e2e/e2e.go` | Suite entry point, `Config`, `testContext`, `Run()`, `initScenario` |
| `e2e/helpers.go` | Shared helpers: data fetchers, capture methods, `normalizeKey`, `sendMessage` |
| `e2e/poll.go` | Runtime-readiness polling (`waitForRuntimeReady`) |
| `e2e/steps_assert.go` | All generic assertion steps — the canonical place for status/body checks |
| `e2e/steps_http.go` | HTTP dispatch: `doRequest`, `interpolatePath`, `setAuthHeaders` |
| `test-features/` | Gherkin source of truth — one subfolder per domain |
