<div align="center">

# 🧠 Crawbl

"**Control plane for Crawbl AI**"

[![CI](https://github.com/Crawbl-AI/crawbl-backend/actions/workflows/deploy-dev.yml/badge.svg)](https://github.com/Crawbl-AI/crawbl-backend/actions/workflows/deploy-dev.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![K8s](https://img.shields.io/badge/K8s-DOKS-326CE5?logo=kubernetes&logoColor=white)]()
[![MCP](https://img.shields.io/badge/MCP-v1-8B5CF6)]()

</div>

---

- 🔐 **Auth & API** — authenticates users and serves the mobile app
- 💬 **Chat routing** — delivers messages between you and your AI agent
- 🔌 **Integrations** — connects Gmail, Slack, Calendar so the agent can act on your behalf
- 🧠 **Agent management** — spins up a private AI agent for each user, configures its tools and personality
- ☸️ **Infrastructure** — provisions and manages everything on Kubernetes via Pulumi + ArgoCD

> 📚 **Full docs:** [crawbl-docs](https://github.com/Crawbl-AI/crawbl-docs) · API reference, architecture, runbooks

## 🏗️ Architecture

```mermaid
flowchart LR
    client["Mobile App / API Client"]
    envoy["Envoy Gateway"]
    orch["Orchestrator"]
    db["Postgres"]
    redis["Redis"]
    cr["UserSwarm CR"]
    mc["Metacontroller"]
    runtime["ZeroClaw Runtime"]
    mcp["Embedded MCP Server"]
    llm["LLM / External APIs"]

    client --> envoy
    envoy --> orch
    orch --> db
    orch --> redis
    orch --> cr
    cr --> mc
    mc --> runtime
    orch --> runtime
    runtime --> mcp
    mcp --> orch
    runtime --> llm
```

> ⚠️ Simplified view. For detailed architecture, data flows, and system diagrams see [crawbl-docs](https://dev.docs.crawbl.com/core-concepts/architecture/system-overview).

## 🚀 Quick Start

```bash
# 1. Build the repo-local CLI, install hooks, and check your machine:
make setup

# 2. Source environment and start the stack:
set -a && source .env && set +a
./crawbl dev start

# 3. Verify:
curl http://localhost:7171/v1/health
```

🚀 The repo root ships a small `./crawbl` launcher. 

It builds `bin/crawbl` on first run and rebuilds it when CLI source changes, so you do not need a global install.

- Docs site: https://dev.docs.crawbl.com
- Getting started: https://dev.docs.crawbl.com/getting-started
- System overview: https://dev.docs.crawbl.com/core-concepts/architecture/system-overview
- CLI reference: https://dev.docs.crawbl.com/reference/cli/crawbl-cli

## 🛠️ CLI

Everything is managed through the `./crawbl` launcher or the thin root `Makefile`.

```
./crawbl setup                  # Check tools + create .env
./crawbl dev start              # Start the full local stack
./crawbl dev start --database-only
./crawbl dev stop
./crawbl dev reset
./crawbl dev migrate
./crawbl dev fmt
./crawbl dev lint [--fix]
./crawbl dev verify
./crawbl test unit
./crawbl test e2e
./crawbl app build <component>
./crawbl infra plan
./crawbl infra update
```

## ✅ Local Checks

This repo ships a versioned `pre-push` hook in `.githooks/pre-push`.

- `make setup` installs the hook automatically
- `make hooks` re-installs it if your Git config was reset
- the hook runs `make ci-check`
- `make ci-check` runs unit tests plus local and linux/amd64 `crawbl` builds to catch the same local-safe failures CI would catch later

The hook does not run the live E2E suite because that depends on the shared dev cluster and takes longer than a normal push gate should. Lint stays available as an explicit manual check with `./crawbl dev lint`.

## 📦 Components

| | Component | What it does |
|---|-----------|-------------|
| 🌐 | **Orchestrator** | Mobile-facing HTTP API + MCP server |
| 🔄 | **Webhook** | Builds and manages per-user AI agent pods |
| 🔐 | **Auth Filter** | Verifies user identity before requests reach the API |
| 🧹 | **Reaper** | Cleans up stale test users + orphaned agent pods |
| 🏗️ | **Infra** | Pulumi IaC for DOKS cluster + ArgoCD |

## 🗂️ Structure

```
cmd/crawbl/                     # Main binary: CLI + servers
cmd/envoy-auth-filter/          # Auth filter for Envoy Gateway
internal/
├── orchestrator/               # 🌐 API domain
│   ├── mcp/                    #    MCP server (agent ↔ orchestrator tools)
│   ├── integration/            #    OAuth connections (Gmail, Slack, etc.)
│   ├── server/                 #    HTTP handlers + Socket.IO realtime
│   ├── service/                #    Business logic layer
│   └── repo/                   #    Data access (Postgres)
├── userswarm/                  # 🔄 Agent pod lifecycle
│   ├── client/                 #    Creates and manages agent pods on K8s
│   ├── webhook/                #    Builds pod specs when agents are provisioned
│   └── reaper/                 #    Cleans up stale users + orphaned pods
├── zeroclaw/                   # 🧠 Agent runtime config + tool catalog
├── pkg/                        # 📦 Shared packages
│   ├── configenv/              #    Environment variable loading
│   ├── database/               #    Postgres connection + migrations
│   ├── errors/                 #    Typed error codes
│   ├── fileutil/               #    File + TOML helpers
│   ├── firebase/               #    FCM push notifications
│   ├── hmac/                   #    HMAC token signing + validation
│   ├── httpserver/             #    HTTP middleware + auth
│   ├── kube/                   #    Kubernetes helpers
│   ├── realtime/               #    Socket.IO + Redis pub/sub
│   ├── redisclient/            #    Redis connection
│   ├── runtime/                #    Graceful shutdown helpers
│   └── yamlvalues/             #    YAML file patching
└── infra/                      # 🏗️ Pulumi IaC
config/                         # 📋 Config reference + samples
migrations/                     # 📊 Postgres schema migrations
api/                            # 📐 Kubernetes CRD types
```

## ⚙️ Configuration

See [`config/README.md`](config/README.md) for the complete reference of every env var and hardcoded default.

## 🔗 Related

| | Repo | |
|---|------|---|
| 📚 | [crawbl-docs](https://github.com/Crawbl-AI/crawbl-docs) | Docs, API reference, architecture |
| 🤖 | [crawbl-zeroclaw](https://github.com/Crawbl-AI/crawbl-zeroclaw) | ZeroClaw agent runtime |
| 📱 | [crawbl-mobile](https://github.com/Crawbl-AI/crawbl-mobile) | Flutter mobile app |
| ☸️ | [crawbl-argocd-apps](https://github.com/Crawbl-AI/crawbl-argocd-apps) | K8s manifests + Helm values |
