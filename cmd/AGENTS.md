# Command Binaries

## Purpose

The three Go binaries that make up the Crawbl backend and CLI toolchain: a unified
platform CLI (`crawbl`), a per-workspace agent runtime gRPC server
(`crawbl-agent-runtime`), and a proxy-wasm Envoy auth filter (`envoy-auth-filter`).

## Layout

- `crawbl/` — unified platform binary; one binary, multiple entrypoints selected by
  sub-command (`platform run`, container args) or local CLI tooling (`app`, `dev`,
  `infra`, `test`, `setup`)
- `crawbl-agent-runtime/` — standalone gRPC server that runs as a per-user swarm pod;
  hosts the agent graph (Manager + Wally + Eve) behind HMAC-authenticated gRPC
- `envoy-auth-filter/` — proxy-wasm plugin compiled with TinyGo; validates HMAC device
  signatures on `X-Token` mobile requests at the Envoy Gateway edge

## Conventions

**`main.go` stays thin.** Each binary follows the same pattern:
parse config / flags → wire concrete dependencies → delegate to internal packages →
serve + signal loop. Business logic lives entirely under `internal/`.

**`crawbl` sub-command groups** (defined in `main.go`):

| Group | Sub-commands | Purpose |
|-------|-------------|---------|
| Runtime | `platform` | Container entrypoints (orchestrator, userswarm, reaper) |
| Build | `app` | `build` and `deploy` for Docker images and static sites |
| Infrastructure | `infra` | Pulumi cluster bootstrap (`init`, `plan`, `bootstrap`, `destroy`, `update`) |
| Development | `dev`, `test`, `setup` | Local dev lifecycle, e2e/unit test runners, first-time setup |

**Config loading.** `crawbl-agent-runtime` uses `internal/agentruntime/config.Load`
(flags + env). `crawbl` sub-commands source env from `.env` via `set -a && source .env`.
`envoy-auth-filter` reads JSON plugin config injected by Envoy (`OnPluginStart`).

**Tag namespacing.** Each deployable component uses its own git tag prefix so sequences
don't collide: bare `vX.Y.Z` for platform, `auth-filter/vX.Y.Z`, `agent-runtime/vX.Y.Z`.
The Docker image tag is always the bare version; the git tag carries the prefix.

## Gotchas

- **Deploys run locally** via `crawbl app deploy <component>`. CI (`deploy-dev.yml`) is a
  validation gate only — it does not build or push images for backend components.
- **`crawbl app deploy` is app-level only.** It builds images, pushes to DOCR, bumps tags
  in `crawbl-argocd-apps`, and creates a GitHub release. Cluster provisioning uses
  `crawbl infra` (Pulumi) — see `internal/infra/`.
- **Working tree must be clean** and the branch must be pushed before `crawbl app deploy`
  runs; tag auto-calculation reads conventional commits since the last matching `v*` tag.
- **`envoy-auth-filter` requires TinyGo**, not the standard Go toolchain:
  `tinygo build -o filter.wasm -scheduler=none -target=wasi .`
- **`platform` sub-command = runtime entrypoint**, not a deploy step. K8s workloads run
  `crawbl platform orchestrator`, `crawbl platform userswarm`, etc. as their container args.

## Key Files

- `crawbl/main.go` — root Cobra command, group registration, signal context wiring
- `crawbl/app/root.go` — `app` sub-command: `build` + `deploy` entry point
- `crawbl/app/deploy.go` — deploy logic for platform, auth-filter, agent-runtime, docs, website
- `crawbl/app/semver.go` — tag auto-calculation from conventional commits
- `crawbl/platform/orchestrator/orchestrator.go` — orchestrator runtime wiring (deps, HTTP server start)
- `crawbl/platform/userswarm/` — userswarm reaper and webhook entrypoints
- `crawbl-agent-runtime/main.go` — agent runtime wiring: config → DB/Redis → blueprint → gRPC serve
- `envoy-auth-filter/main.go` — proxy-wasm plugin: HMAC validation, WebSocket bypass, timestamp freshness
