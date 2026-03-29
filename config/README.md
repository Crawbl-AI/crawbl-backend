# ⚙️ Configuration Reference

> Where to find and change things in crawbl-backend.

## 🔍 Quick Reference

| I want to... | Where |
|--------------|-------|
| Add a new agent tool | `internal/zeroclaw/tools.go` → add to `defaultToolCatalog` |
| Change LLM defaults (temperature, model) | `internal/zeroclaw/types.go` (constants) |
| Add an integration provider | `internal/orchestrator/service/integrationservice/integrationservice.go` |
| Change default workspace name | `internal/orchestrator/types.go` → `DefaultWorkspaceName` |
| Add a new error code | `internal/pkg/errors/types.go` |
| Change pod security settings | `internal/userswarm/webhook/types.go` |

## 🔐 Environment Variables

Source `.env` before running: `set -a && source .env && set +a`

### Server

| Variable | Default | What |
|----------|---------|------|
| `CRAWBL_SERVER_PORT` | `7171` | API port |
| `CRAWBL_ENVIRONMENT` | `local` | `local` / `dev` / `prod` |
| `CRAWBL_E2E_TOKEN` | — | E2E test auth bypass (empty = disabled) |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |

### Agent Runtime

| Variable | Default | What |
|----------|---------|------|
| `CRAWBL_RUNTIME_DRIVER` | `fake` | `fake` (local echo) or `userswarm` (real K8s) |
| `CRAWBL_RUNTIME_NAMESPACE` | `userswarms` | K8s namespace for agent pods |
| `CRAWBL_RUNTIME_IMAGE` | — | ZeroClaw container image (required in prod) |
| `CRAWBL_RUNTIME_DEFAULT_PROVIDER` | `openai` | LLM provider for new agents |
| `CRAWBL_RUNTIME_DEFAULT_MODEL` | `gpt-5-mini` | LLM model for new agents |
| `CRAWBL_RUNTIME_STORAGE_SIZE` | `2Gi` | PVC size per user |
| `CRAWBL_RUNTIME_PORT` | `42617` | ZeroClaw gateway port inside pod |
| `CRAWBL_RUNTIME_ENV_SECRET_NAME` | — | K8s Secret with LLM API keys |
| `CRAWBL_RUNTIME_POLL_TIMEOUT` | `60s` | Max wait for agent pod readiness |

### MCP Server

| Variable | Default | What |
|----------|---------|------|
| `CRAWBL_MCP_SIGNING_KEY` | — | HMAC secret (shared with webhook). Empty = MCP disabled |
| `CRAWBL_MCP_ENDPOINT` | — | URL for agents to reach MCP (webhook only) |
| `CRAWBL_FCM_PROJECT_ID` | — | Firebase project for push notifications |
| `CRAWBL_FCM_SERVICE_ACCOUNT_PATH` | — | Path to Firebase SA JSON file |

### Database

| Variable | Default | What |
|----------|---------|------|
| `CRAWBL_DATABASE_HOST` | `127.0.0.1` | Postgres host |
| `CRAWBL_DATABASE_PORT` | `5432` | Postgres port |
| `CRAWBL_DATABASE_USER` | `postgres` | DB user |
| `CRAWBL_DATABASE_PASSWORD` | `postgres` | DB password |
| `CRAWBL_DATABASE_NAME` | `crawbl` | DB name |
| `CRAWBL_DATABASE_SCHEMA` | `orchestrator` | Postgres schema |

### Redis

| Variable | Default | What |
|----------|---------|------|
| `CRAWBL_REDIS_ADDR` | — | Redis address. Empty = realtime disabled |
| `CRAWBL_REDIS_PASSWORD` | — | Redis password |

## 📋 Samples

- [`config/samples/zeroclaw.yaml.example`](samples/zeroclaw.yaml.example) — Full ZeroClaw config with all options commented

## 📖 Deep Reference

For the complete list of every hardcoded constant, error code, integration provider, tool catalog entry, and internal default, see [`INTERNALS.md`](INTERNALS.md).
