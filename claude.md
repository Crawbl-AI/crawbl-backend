# Crawbl Backend

## Purpose

Build the Go middleware/orchestrator for Crawbl.

This repo contains both the Crawbl orchestrator HTTP API and the UserSwarm lifecycle/runtime control-plane code.

The backend sits between the Flutter app and each user's agent runtime. It owns routing, auth, orchestration, integrations, billing controls, and auditability.

## Core Responsibilities

- Authenticate users and issue/validate platform sessions
- Serve the mobile-facing HTTP API for auth, workspaces, and future realtime flows
- Provision `UserSwarm` resources and the agent runtime deployments in shared runtime namespaces
- Proxy chat/task requests to the correct user swarm
- Integrate with hosted LLM providers and enforce cost and access policy at the backend layer
- Expose integration adapters for Gmail, Calendar, Asana, and future apps
- Store and manage user OAuth tokens server-side
- Enforce rate limits, plans, and usage attribution
- Record audit logs for tool usage and write actions
- Later: broker A2A communication between user swarms

## Rules

- WHEN RUNNING COMMANDS OR WAITING FOR INPUT OR WAITING FOR SOMETHING, NEVER SLEEP FOR MORE THAN 10 SECONDS
- Always use the `crawbl` CLI for building, pushing, and deploying images. Prefer `crawbl app build` and `crawbl app deploy` over raw docker/kubectl/yq commands.
- Treat this service as the control plane, not a thin API wrapper
- Long-term, keep LLM provider credentials in the backend, not in agent runtime pods
- Runtime secrets are injected via ESO-managed Kubernetes Secrets (envSecretRef); provider key brokering will move fully behind the orchestrator later
- Default model access is platform-managed; add BYOK later for power users
- Connected app credentials are per-user and must be revocable
- Read actions may auto-execute after consent; write actions require approval by default
- Adapters expose narrow capabilities, not raw unrestricted API passthrough
- Cross-user A2A must go through backend mediation, never direct cross-namespace pod access
- Always check and synchronize contract crawbl-docs/ops/api/api-contract.md file when making changes on orchestrator frontend/backend API.
- Shared runtime namespaces are the current model; do not reintroduce namespace-per-user assumptions.
- The orchestrator uses Postgres only for persistence; do not add in-memory repository fallbacks back into the main API path.
- Mobile auth follows the Soulheim-style transport: `X-Token` plus device/security headers. `Authorization: Bearer` is only a compatibility path for tooling/dev, not the primary mobile contract.

## Design Priorities

- Clear typed contracts between mobile, backend, and agent runtime
- Idempotent provisioning and retries
- Secure secret storage and token refresh
- Structured logs, audit trails, and per-user usage accounting
- Provider abstraction, usage attribution, and cost control
- Small, composable services and packages over framework-heavy abstractions

## Code Structure

- Put binaries under `cmd/`
- Keep domain/application code in `internal/`
- Keep transport helpers internal unless they are truly reusable across repos
- For the orchestrator API, prefer the current split:
  - `internal/orchestrator/types.go` for shared domain types and constants
  - `internal/orchestrator/repo/` for repository contracts, row types, and persistence implementations
  - `internal/orchestrator/service/` for typed service opts/contracts and business logic
  - `internal/orchestrator/server/` for HTTP handlers and request/response DTOs
  - `internal/pkg/` for shared database, error, runtime, and HTTP helpers
- Pulumi (`internal/infra/`) bootstraps DOKS cluster and installs ArgoCD only — no edge package, no Helm chart management in this repo
- All Helm charts live in `crawbl-argocd-apps/components/*/chart/`; ArgoCD manages all K8s resources after bootstrap
- Keep `types.go` files for request/response types, constants, vars, and interfaces instead of scattering them through handler files
- Follow the Soulheim/Skatts style: one `dbr.Session` per request, pass typed opts through service methods, and let repos work with a `SessionRunner`
- Add new API surface in small vertical slices, starting from `crawbl-docs/ops/api/api-contract.md`
- Local backend development should use the Postgres-backed path with `docker-compose.yaml`, `dockerfiles/`, SQL migrations, and `make setup/run`
- Cluster deployment is managed by ArgoCD via `crawbl-argocd-apps` — Helm charts are vendored there, not in this repo; do not use `crawbl app deploy` for cluster rollouts
- Observability stack: VictoriaMetrics at `dev.metrics.crawbl.com` (metrics storage + Prometheus-compatible query API), VictoriaLogs at `dev.logs.crawbl.com` (log storage + query UI), Fluent Bit collects all container logs from every namespace and ships them to VictoriaLogs
- The current dev cluster uses a temporary single-node Bitnami PostgreSQL release in the `backend` namespace; later environments should move to a stronger database posture
- Reuse patterns from `Skatts/monobackend` where they fit: `internal/pkg/database`, `cmd/migrate`, service Dockerfile, and compose-driven local setup
- Use the local Docker stack for Venom-based minimal workflow verification; avoid reintroducing manual curl-only verification as the primary path
- `UserSwarm.status` is the source of truth for runtime readiness. Do not duplicate swarm phase/readiness into Postgres unless there is a strong product reason.
- The backend should expose workspace runtime readiness through the normal workspace endpoints before adding a dedicated provisioning endpoint.
- Runtime pods are private because Kubernetes limits reachability, not because the agent runtime binds localhost. Keep that distinction in mind when changing `tomlOverrides` or gateway env defaults.

## Environment Variables and Secrets

- Always source `.env` file for all environment variables, API keys, tokens, and secrets
- Run infrastructure commands with: `set -a && source .env && set +a && <command>`
- The `.env` file contains all temporary dev credentials (Pulumi, DigitalOcean, Cloudflare, etc.)
- Never hardcode tokens or API keys in code — always use `.env` or environment variables
- For ArgoCD deploy key: set `ARGOCD_SSH_KEY_PATH` pointing to the deploy key file

## Current Direction

1. Build the orchestrator HTTP foundation first
2. Add swarm-aware auth/session and workspace state
3. Add internal MCP endpoints and first-party skills
4. Move provider access behind the orchestrator instead of persisting provider keys in agent runtime config

## MVP Focus

1. Auth and user provisioning
2. Agent runtime request proxy
3. Gmail and Google Calendar adapters
4. Hosted LLM provider integration
5. Read-first integrations, then ask-before-write flows

## GitHub Actions CI/CD

### Role of CI

CI is a **validation gate**, not the build/deploy pipeline.

On push to `main`, `deploy-dev.yml` runs two parallel jobs:
- e2e tests against the live dev cluster
- release tagging

Builds and deployments happen **locally** via `crawbl app deploy`. CI does not build or push images.

### Local Deploy Workflow

Use `crawbl app deploy <component>` to build, push, and deploy a component. The tag is auto-calculated from conventional commits — `--tag` is optional and only needed to override. The working tree must be clean and fully pushed before deploying.

```bash
# Deploy a single component (tag auto-calculated via semver)
crawbl app deploy platform
crawbl app deploy auth-filter
crawbl app deploy docs
crawbl app deploy website
crawbl app deploy agent-runtime   # built/deployed separately

# Deploy platform + auth-filter only
crawbl app deploy all

# Override the auto-calculated tag explicitly
crawbl app deploy platform --tag v1.2.3
```

> **Migrations run automatically on startup.** The orchestrator runs `golang-migrate` against the Postgres database before starting the HTTP server. No manual migration step is needed after deploy. Migration files at `migrations/orchestrator/` are embedded in the container image.

Semver logic: finds the last `v*` tag, scans commits since then — `feat:` triggers a minor bump, `!:` (breaking) triggers a major bump, everything else is a patch bump.

Each `crawbl app deploy` call for backend components (platform, auth-filter):
1. Verifies working tree is clean and pushed
2. Builds the Docker image locally
3. Pushes to DOCR (`registry.digitalocean.com/crawbl/`)
4. Updates the image tag in `crawbl-argocd-apps` and pushes
5. Creates a Git tag (auto-calculated; bumps patch if tag already exists on remote)
6. Creates a GitHub release with Claude-enriched notes (sonnet model) and a full changelog link
7. ArgoCD auto-syncs the new image to the cluster

For **docs** and **website**, steps 2-4 and 7 are skipped. Instead, the deploy runs `npm run build` in the sibling repo and `wrangler pages deploy` to upload the static output to Cloudflare Pages. Tagging and GitHub release still happen.

`crawbl app deploy all` deploys platform + auth-filter only. Docs, website, and agent-runtime are deployed individually.

`crawbl setup` verifies required tools: `docker`, `yq`, `gh`, `claude`. `.mise.toml` includes `yq`, `protoc`, and the standard Go/k8s/cloud toolchain. Run `mise install` inside `crawbl-backend/` after a fresh clone to provision `protoc` and the rest.

**Protobuf / gRPC toolchain** (required by `internal/agentruntime/` and `cmd/crawbl-agent-runtime/`):
- `protoc` — installed via `mise install` (pinned in `.mise.toml`) or `brew install protobuf`.
- `protoc-gen-go` and `protoc-gen-go-grpc` — Go plugins, installed by the `make generate` target or manually with:
  ```bash
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
  ```
  These land in `$(go env GOPATH)/bin` which must be on `PATH` for `protoc` to find them.
- Regenerate gRPC bindings from `proto/agentruntime/v1/*.proto` with `make generate`.

Makefile shortcuts use auto-semver — no manual tag needed:

```bash
make deploy-dev        # deploy platform + auth-filter
make deploy-platform
# etc.
```

### Workflow Files

- `deploy-dev.yml` — Push to `main`: runs e2e + release tagging only
- `deploy-prod.yml` — Manual trigger only; still uses the full CI pipeline (unchanged)
- `reusable-build.yml`, `reusable-deploy.yml`, `reusable-update-argocd.yml` — Kept for reference; no longer called from `deploy-dev.yml`

### Workflow Validation

Use [`actionlint`](https://github.com/rhysd/actionlint) to validate workflow YAML before pushing:

```bash
brew install actionlint
actionlint .github/workflows/
```

## E2E Testing Against the Dev Cluster

The e2e suite (`crawbl test e2e`) runs godog/Gherkin tests against a live orchestrator. Two modes:

- **CI mode**: runs against `https://dev.api.crawbl.com` with `--e2e-token`
- **Local mode**: port-forward the orchestrator and optionally postgres, no token needed

### Running locally against the dev cluster

```bash
# 1. Port-forward orchestrator + postgres
kubectl port-forward svc/orchestrator 7171:7171 -n backend &
kubectl port-forward svc/backend-postgresql 5432:5432 -n backend &

# 2. Run e2e (with DB assertions)
crawbl test e2e \
  --base-url http://localhost:7171 \
  --database-dsn "postgres://postgres:<PG_PASSWORD>@localhost:5432/crawbl?sslmode=disable&search_path=orchestrator" \
  --verbose --runtime-ready-timeout 4m
```

## Deleting a user in dev cluster/db

If asked to delete a user from Cluster, App or Database

0. Delete a user from dev DB by port-forwarding db from `backend` namespace
1. Find a userswarm that is used by user in K8s `userswarms` namespace.
2. You can find it by checking annotations/labels on the userswarm CR.
3. Delete a userswarm.

Get the postgres password: `kubectl get secret backend-postgresql-auth -n backend -o jsonpath='{.data.postgres-password}' | base64 -d`

### CI pipeline (deploy-dev.yml)

- Build → Docker images → Update ArgoCD tags → Wait for sync → Run e2e
- If e2e fails in CI, fix the code and push again; the cluster stays on the latest deployed version

## Current API Slice

- The implemented orchestrator slice currently covers:
  - `GET /v1/health`
  - `GET /v1/legal`
  - `POST /v1/fcm-token`
  - `POST /v1/auth/sign-in`
  - `POST /v1/auth/sign-up`
  - `DELETE /v1/auth/delete`
  - `GET /v1/users/profile`
  - `PATCH /v1/users`
  - `GET /v1/users/legal`
  - `POST /v1/users/legal/accept`
  - `GET /v1/workspaces`
  - `GET /v1/workspaces/{id}`
  - `GET /v1/workspaces/{workspaceId}/agents`
  - `GET /v1/workspaces/{workspaceId}/tools`
  - `GET /v1/workspaces/{workspaceId}/conversations`
  - `GET /v1/workspaces/{workspaceId}/conversations/{id}`
  - `GET /v1/workspaces/{workspaceId}/conversations/{id}/messages`
  - `POST /v1/workspaces/{workspaceId}/conversations/{id}/messages` — returns `[]*Message` (multi-agent turns)
  - `GET /v1/models`
  - `GET /v1/agents/{id}`
  - `GET /v1/agents/{id}/details`
  - `GET /v1/agents/{id}/history`
  - `GET /v1/agents/{id}/settings`
  - `GET /v1/agents/{id}/tools`
- The minimal local verification path is `make test-e2e-one FILE=01_orchestrator_smoke.yml`
- `POST /v1/auth/sign-up` and `POST /v1/auth/sign-in` should best-effort seed the default workspace `UserSwarm` without waiting for `Verified=True`
- Mobile should poll `GET /v1/workspaces` or `GET /v1/workspaces/{id}` for `runtime.status` / `runtime.verified` while the first swarm is provisioning
