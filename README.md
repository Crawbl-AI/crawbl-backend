<div align="center">

# crawbl-backend

**The AI infrastructure control plane for Crawbl**

[![CI](https://github.com/Crawbl-AI/crawbl-backend/actions/workflows/deploy-dev.yml/badge.svg)](https://github.com/Crawbl-AI/crawbl-backend/actions/workflows/deploy-dev.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Proprietary-red)]()
[![K8s](https://img.shields.io/badge/Kubernetes-DOKS-326CE5?logo=kubernetes&logoColor=white)]()

</div>

---

`crawbl-backend` is the control plane between the Crawbl mobile app and per-user ZeroClaw agent runtimes on Kubernetes. It handles authentication, user provisioning, workspace lifecycle, LLM routing, integration adapters, and the MCP server that connects orchestrators to agent swarms.

## Architecture

```
┌──────────────────┐        ┌───────────────────────┐
│   Mobile App     │──────▶ │   Envoy Gateway        │
│   (Flutter)      │  HTTPS │   (JWT auth filter)    │
└──────────────────┘        └───────────┬───────────┘
                                         │
                             ┌───────────▼───────────┐
                             │     Orchestrator        │
                             │  (HTTP API + MCP server)│
                             └──┬──────────────────┬──┘
                                │                  │
               ┌────────────────▼──┐    ┌──────────▼────────────┐
               │  Metacontroller   │    │   Postgres + Redis      │
               │  (UserSwarm sync) │    │   (state + sessions)    │
               └────────┬──────────┘    └────────────────────────┘
                        │
          ┌─────────────▼──────────────┐
          │   ZeroClaw Runtime Pods     │
          │   (per-user, shared ns)     │◀── MCP (tool calls)
          └─────────────────────────────┘
```

The orchestrator provisions `UserSwarm` Kubernetes custom resources via Metacontroller. Each user gets an isolated ZeroClaw pod in a shared runtime namespace. The orchestrator exposes an MCP server at `/mcp/v1` that ZeroClaw connects to as a client, enabling structured tool-call mediation with audit logging.

## Features

- **Firebase auth** — sign-in, sign-up, session validation via `X-Token` + device headers
- **Workspace management** — per-user workspace provisioning backed by `UserSwarm` CRs
- **UserSwarm lifecycle** — Metacontroller webhook drives creation, sync, and cleanup of ZeroClaw pods
- **MCP server** — Model Context Protocol endpoint at `/mcp/v1`; ZeroClaw connects as client
- **Integration adapters** — OAuth-based connections to Gmail, Calendar, and future apps; tokens stored server-side
- **FCM push notifications** — Firebase Cloud Messaging for mobile-side async events
- **Realtime channel** — Socket.IO adapter over Redis for live agent ↔ app communication
- **Audit logging** — structured records of tool usage and write-action approvals
- **Rate limiting and billing controls** — per-user LLM access policy enforced at the backend layer
- **Envoy auth filter** — sidecar ExtProc filter that validates tokens before requests reach the orchestrator
- **Pulumi IaC** — DOKS cluster bootstrap and ArgoCD install only; all app resources live in `crawbl-argocd-apps`

## Components

| Component | Path | Description |
|-----------|------|-------------|
| Orchestrator | `cmd/crawbl/platform/orchestrator/` | HTTP API server (mobile-facing) |
| UserSwarm webhook | `cmd/crawbl/platform/userswarm/` | Metacontroller sync webhook + reaper cronjob |
| Envoy auth filter | `cmd/envoy-auth-filter/` | Token validation ExtProc sidecar |
| CLI | `cmd/crawbl/` | `crawbl` binary — infra, builds, and e2e tests |
| Infra | `internal/infra/` | Pulumi IaC (DOKS cluster + ArgoCD bootstrap only) |

## Quick Start

**Prerequisites:** Go 1.25+, Docker with buildx, `kubectl`, `doctl` (authenticated)

```bash
# Clone the repo
git clone https://github.com/Crawbl-AI/crawbl-backend.git
cd crawbl-backend

# Start Postgres and apply migrations
make setup

# Run the orchestrator locally (connects to local Postgres)
make run

# Rebuild from a clean database
make run-clean

# Stop all local services
make stop
```

Local development uses `docker-compose.yaml` to run Postgres. The orchestrator reads config from environment variables — copy `.env.example` to `.env` and fill in the required values, then `source .env`.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/health` | Health check |
| `GET` | `/v1/legal` | Legal documents |
| `POST` | `/v1/fcm-token` | Register FCM push token |
| `POST` | `/v1/auth/sign-in` | Authenticate and issue session |
| `POST` | `/v1/auth/sign-up` | Create account and provision workspace |
| `DELETE` | `/v1/auth/delete` | Delete account |
| `GET` | `/v1/users/profile` | Fetch user profile |
| `PATCH` | `/v1/users` | Update user profile |
| `GET` | `/v1/users/legal` | Fetch user legal acceptance status |
| `POST` | `/v1/users/legal/accept` | Accept legal terms |
| `GET` | `/v1/workspaces` | List workspaces (includes runtime status) |
| `GET` | `/v1/workspaces/{id}` | Get workspace + runtime readiness |
| `GET` | `/v1/workspaces/{workspaceId}/agents` | List agents in workspace |
| `GET` | `/v1/workspaces/{workspaceId}/tools` | List tools available to workspace |
| `GET` | `/v1/workspaces/{workspaceId}/conversations` | List conversations |
| `GET` | `/v1/workspaces/{workspaceId}/conversations/{id}` | Get conversation |
| `GET` | `/v1/workspaces/{workspaceId}/conversations/{id}/messages` | Get messages |
| `POST` | `/v1/workspaces/{workspaceId}/conversations/{id}/messages` | Send message |

Mobile clients poll `GET /v1/workspaces/{id}` for `runtime.status` / `runtime.verified` while the first swarm is provisioning.

## Project Structure

```
cmd/
├── crawbl/                    # Single binary — CLI + platform server
│   ├── main.go
│   ├── platform/
│   │   ├── orchestrator/      # HTTP API server entrypoint
│   │   └── userswarm/         # Metacontroller webhook + reaper entrypoint
│   ├── app/                   # CLI: image build commands
│   ├── infra/                 # CLI: Pulumi infra commands
│   └── test/                  # CLI: e2e test runner
└── envoy-auth-filter/         # Standalone ExtProc auth sidecar

internal/
├── orchestrator/              # API domain logic
│   ├── types.go               # Shared types, constants, interfaces
│   ├── mcp/                   # MCP server (agent ↔ orchestrator tools)
│   ├── integration/           # OAuth connections and token management
│   ├── server/                # HTTP handlers and request/response DTOs
│   ├── service/               # Business logic (typed opts pattern)
│   └── repo/                  # Repository contracts and Postgres implementations
├── userswarm/                 # UserSwarm lifecycle
│   ├── client/                # Kubernetes CR management
│   ├── webhook/               # Metacontroller sync webhook
│   └── reaper/                # E2E cleanup cronjob
├── zeroclaw/                  # ZeroClaw runtime configuration
├── infra/                     # Pulumi IaC (bootstrap only)
└── pkg/                       # Shared packages
    ├── database/              # Postgres helpers (dbr sessions)
    ├── firebase/              # FCM push client
    ├── hmac/                  # Token generation and validation
    ├── httpserver/            # HTTP server setup
    ├── kube/                  # Kubernetes client helpers
    ├── realtime/              # Socket.IO + Redis adapter
    ├── redisclient/           # Redis connection
    ├── runtime/               # Runtime config helpers
    ├── configenv/             # Environment config loading
    └── errors/                # Typed error helpers

api/                           # Kubernetes CRD types (v1alpha1)
migrations/                    # PostgreSQL migration files
dockerfiles/                   # Dockerfiles for each component
config/                        # ArgoCD Helm values and ZeroClaw defaults
```

## Configuration

The orchestrator is configured via environment variables. Source `.env` before running any local or infra command:

```bash
set -a && source .env && set +a
```

Key variables:

| Variable | Description |
|----------|-------------|
| `DATABASE_URL` | Postgres connection string |
| `REDIS_URL` | Redis connection string |
| `FIREBASE_CREDENTIALS` | Firebase service account JSON (base64) |
| `DIGITALOCEAN_TOKEN` | DigitalOcean API token (infra + registry) |
| `PULUMI_ACCESS_TOKEN` | Pulumi state backend token |
| `ARGOCD_SSH_KEY_PATH` | Path to ArgoCD deploy key |

See `CLAUDE.md` for the full environment variable reference and infra conventions.

## CI/CD

GitHub Actions (`deploy-dev.yml`) is the source of truth for all backend deployments. The pipeline runs on every push to `main`:

```
build (16 vCPU Blacksmith)
  └── Compile Go binary + build Docker images → push to DOCR
       └── infra-drift-check
             └── Run `crawbl infra plan` — fail on detected drift
                  └── update-argocd
                        └── Patch image tags in crawbl-argocd-apps → ArgoCD auto-syncs
                             └── e2e-test
                                   └── Wait for rollout → run E2E suite against dev cluster
                                        └── rollback-argocd (on e2e failure only)
                                              └── Restore previous image tags
```

Key properties:
- Pipeline time: ~6 min (down from 22 min via prebuilt binary + Blacksmith NVMe caching)
- Concurrency group `deploy-dev` queues runs — no silent cancellations on `main`
- Automatic rollback patches `crawbl-argocd-apps` back to previous tags if E2E fails
- ZeroClaw images are built separately from `crawbl-zeroclaw` on tag push (`v*-crawbl*`)

## Development

```bash
make setup       # Start Postgres via docker-compose and apply migrations
make run         # Run orchestrator locally
make run-clean   # Wipe database and restart from scratch
make stop        # Stop all local services
make build       # Build the crawbl binary

# Run the minimal smoke E2E suite against a local or dev cluster
crawbl test e2e --base-url https://dev.api.crawbl.com -v

# Infrastructure (local testing only — CI is the default deploy path)
crawbl infra plan
crawbl infra update

# Build and push images manually (local testing only)
crawbl app build orchestrator --tag <tag> --push
crawbl app build auth-filter --tag <tag> --push
```

## Related Repositories

| Repository | Description |
|------------|-------------|
| [crawbl-argocd-apps](https://github.com/Crawbl-AI/crawbl-argocd-apps) | ArgoCD app-of-apps — all K8s manifests and Helm values |
| [crawbl-zeroclaw](https://github.com/Crawbl-AI/crawbl-zeroclaw) | Forked ZeroClaw agent runtime with Crawbl webhook extensions |
| [crawbl-mobile](https://github.com/Crawbl-AI/crawbl-mobile) | Flutter mobile app |
| [crawbl-docs](https://github.com/Crawbl-AI/crawbl-docs) | Docusaurus documentation site |
