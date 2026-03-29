<div align="center">

# 🧠 Crawbl

**Control plane for Crawbl AI infrastructure**

[![CI](https://github.com/Crawbl-AI/crawbl-backend/actions/workflows/deploy-dev.yml/badge.svg)](https://github.com/Crawbl-AI/crawbl-backend/actions/workflows/deploy-dev.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![K8s](https://img.shields.io/badge/K8s-DOKS-326CE5?logo=kubernetes&logoColor=white)]()
[![MCP](https://img.shields.io/badge/MCP-v1-8B5CF6)]()

</div>

---

The backend that powers Crawbl. When you open the app and talk to your AI agent,
this is the server that authenticates you, routes your messages to the right agent,
and connects your apps (Gmail, Slack, Calendar) so the agent can act on your behalf.

> 📚 **Full docs:** [crawbl-docs](https://github.com/Crawbl-AI/crawbl-docs) · API reference, architecture, runbooks

## 🏗️ Architecture

```
  📱 Mobile App
       │
       ▼
  🔒 Envoy Gateway (JWT auth)
       │
       ▼
  ⚙️  Orchestrator ◄──── 🗄️ Postgres + Redis
       │       │
       │       └──── 🔌 MCP Server (/mcp/v1)
       │                    ▲
       ▼                    │
  🔄 Metacontroller         │
       │                    │
       ▼                    │
  🧠 ZeroClaw Pods ─────────┘
     (per-user agents)
```

> ⚠️ Simplified view. For detailed architecture, data flows, and system diagrams see [crawbl-docs](https://github.com/Crawbl-AI/crawbl-docs).

## 🚀 Quick Start

```bash
make setup      # Postgres + Redis via docker-compose
make run        # Orchestrator on :7171
make test       # Run tests
```

All secrets in `.env` — copy from `.env.example` and `source .env` before running.

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
