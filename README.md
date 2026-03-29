<div align="center">

# 🧠 Crawbl

**Control plane for Crawbl AI infrastructure**

[![CI](https://github.com/Crawbl-AI/crawbl-backend/actions/workflows/deploy-dev.yml/badge.svg)](https://github.com/Crawbl-AI/crawbl-backend/actions/workflows/deploy-dev.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![K8s](https://img.shields.io/badge/K8s-DOKS-326CE5?logo=kubernetes&logoColor=white)]()
[![MCP](https://img.shields.io/badge/MCP-v1-8B5CF6)]()

</div>

---

Sits between the mobile app and per-user [ZeroClaw](https://github.com/Crawbl-AI/crawbl-zeroclaw) agent pods. Handles auth, workspaces, chat proxying, MCP tools, push notifications, integrations, and audit logging.

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
| 🧹 | **Reaper** | Cleans up stale e2e test resources |
| 🏗️ | **Infra** | Pulumi IaC (cluster bootstrap only) |

## 🗂️ Structure

```
cmd/crawbl/                     # Single binary: CLI + servers
internal/
├── orchestrator/               # 🌐 API domain
│   ├── mcp/                    #    MCP server (agent ↔ orchestrator)
│   ├── integration/            #    OAuth connections
│   ├── server/                 #    HTTP handlers
│   ├── service/                #    Business logic
│   └── repo/                   #    Postgres repos
├── userswarm/                  # 🔄 K8s lifecycle
│   ├── client/                 #    CR management
│   ├── webhook/                #    Metacontroller sync
│   └── reaper/                 #    E2E cleanup
├── zeroclaw/                   # 🧠 Agent runtime config
├── pkg/                        # 📦 Shared packages
│   ├── firebase/               #    FCM push
│   ├── hmac/                   #    Token signing
│   ├── database/               #    Postgres helpers
│   └── ...
└── infra/                      # 🏗️ Pulumi bootstrap
config/                         # 📋 Config reference + samples
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
