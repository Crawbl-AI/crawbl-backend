# Crawbl Backend

## Purpose

Build the Go middleware/orchestrator for Crawbl.

This repo contains both the Crawbl orchestrator HTTP API and the Kubernetes operator/runtime control-plane code for `UserSwarm`.

The backend sits between the Flutter app and each user's ZeroClaw swarm. It owns routing, auth, orchestration, integrations, billing controls, and auditability.

## Core Responsibilities

- Authenticate users and issue/validate platform sessions
- Serve the mobile-facing HTTP API for auth, workspaces, and future realtime flows
- Provision `UserSwarm` resources and ZeroClaw deployments in shared runtime namespaces
- Proxy chat/task requests to the correct user swarm
- Integrate with hosted LLM providers and enforce cost and access policy at the backend layer
- Expose integration adapters for Gmail, Calendar, Asana, and future apps
- Store and manage user OAuth tokens server-side
- Enforce rate limits, plans, and usage attribution
- Record audit logs for tool usage and write actions
- Later: broker A2A communication between user swarms

## Rules

- Treat this service as the control plane, not a thin API wrapper
- Long-term, keep LLM provider credentials in the backend, not in ZeroClaw pods
- Runtime secrets are injected via ESO-managed Kubernetes Secrets (envSecretRef); provider key brokering will move fully behind the orchestrator later
- Default model access is platform-managed; add BYOK later for power users
- Connected app credentials are per-user and must be revocable
- Read actions may auto-execute after consent; write actions require approval by default
- Adapters expose narrow capabilities, not raw unrestricted API passthrough
- Cross-user A2A must go through backend mediation, never direct cross-namespace pod access
- Always check and synchronize contract crawbl-docs/ops/api-contract.md file when making changes on orchestrator frontend/backend API.
- Shared runtime namespaces are the current model; do not reintroduce namespace-per-user assumptions.
- The orchestrator uses Postgres only for persistence; do not add in-memory repository fallbacks back into the main API path.
- Mobile auth follows the Soulheim-style transport: `X-Token` plus device/security headers. `Authorization: Bearer` is only a compatibility path for tooling/dev, not the primary mobile contract.

## Design Priorities

- Clear typed contracts between mobile, backend, and ZeroClaw
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
- Add new API surface in small vertical slices, starting from `crawbl-docs/ops/api-contract.md`
- Local backend development should use the Postgres-backed path with `docker-compose.yaml`, `dockerfiles/`, SQL migrations, and `make setup/run`
- Cluster deployment is managed by ArgoCD via `crawbl-argocd-apps` — Helm charts are vendored there, not in this repo; do not use `crawbl app deploy` for cluster rollouts
- The current dev cluster uses a temporary single-node Bitnami PostgreSQL release in the `backend` namespace; later environments should move to a stronger database posture
- Reuse patterns from `Skatts/monobackend` where they fit: `internal/pkg/database`, `cmd/migrate`, service Dockerfile, and compose-driven local setup
- Use the local Docker stack for Venom-based minimal workflow verification; avoid reintroducing manual curl-only verification as the primary path
- `UserSwarm.status` is the source of truth for runtime readiness. Do not duplicate swarm phase/readiness into Postgres unless there is a strong product reason.
- The backend should expose workspace runtime readiness through the normal workspace endpoints before adding a dedicated provisioning endpoint.
- Runtime pods are private because Kubernetes limits reachability, not because ZeroClaw binds localhost. Keep that distinction in mind when changing `tomlOverrides` or gateway env defaults.

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
4. Move provider access behind the orchestrator instead of persisting provider keys in ZeroClaw runtime config

## MVP Focus

1. Auth and user provisioning
2. ZeroClaw request proxy
3. Gmail and Google Calendar adapters
4. Hosted LLM provider integration
5. Read-first integrations, then ask-before-write flows

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
  - `GET /v1/workspaces/{workspaceId}/conversations`
  - `GET /v1/workspaces/{workspaceId}/conversations/{id}`
  - `GET /v1/workspaces/{workspaceId}/conversations/{id}/messages`
  - `POST /v1/workspaces/{workspaceId}/conversations/{id}/messages`
- The minimal local verification path is `make test-e2e-one FILE=01_orchestrator_smoke.yml`
- `POST /v1/auth/sign-up` and `POST /v1/auth/sign-in` should best-effort seed the default workspace `UserSwarm` without waiting for `Verified=True`
- Mobile should poll `GET /v1/workspaces` or `GET /v1/workspaces/{id}` for `runtime.status` / `runtime.verified` while the first swarm is provisioning
