# Configuration Reference

> Where to find and change every configurable value in crawbl-backend.

## Quick Reference

| What you want to change | File | Format |
|--------------------------|------|--------|
| Tool catalog (agent capabilities) | `internal/zeroclaw/tools.go` | Go |
| ZeroClaw provider defaults (temperature, timeouts, retries) | `internal/zeroclaw/types.go` | Go constants |
| ZeroClaw operator config (cluster-wide YAML override) | `config/zeroclaw.yaml` | YAML |
| Per-user TOML bootstrap defaults | `internal/zeroclaw/toml.go` | Go |
| Orchestrator HTTP server port | `CRAWBL_SERVER_PORT` env var | Env |
| Runtime driver (fake vs real K8s) | `CRAWBL_RUNTIME_DRIVER` env var | Env |
| Runtime namespace | `CRAWBL_RUNTIME_NAMESPACE` env var | Env |
| Default LLM provider / model | `CRAWBL_RUNTIME_DEFAULT_PROVIDER` / `CRAWBL_RUNTIME_DEFAULT_MODEL` env vars | Env |
| MCP server signing key | `CRAWBL_MCP_SIGNING_KEY` env var | Env |
| Database connection | `CRAWBL_DATABASE_*` env vars | Env |
| Redis connection | `CRAWBL_REDIS_ADDR` env var | Env |
| Integration provider catalog | `internal/orchestrator/service/integrationservice/integrationservice.go` | Go |
| Error codes surfaced to clients | `internal/pkg/errors/types.go` | Go constants |
| Default workspace / swarm names | `internal/orchestrator/types.go` | Go constants |
| Default agents created on sign-up | `internal/orchestrator/types.go` (`DefaultAgents`) | Go |
| Webhook server listen address | `--addr` CLI flag (default `:8080`) | CLI flag |
| ZeroClaw config file path | `--zeroclaw-config` CLI flag (default `config/zeroclaw.yaml`) | CLI flag |
| Container security UID/GID | `internal/userswarm/webhook/types.go` | Go constants |

---

## Environment Variables

All environment variables consumed by the orchestrator binary. Read at startup inside
`cmd/crawbl/platform/orchestrator/orchestrator.go` via `envOrDefault()`.

### Orchestrator Server

| Variable | Default | Description |
|----------|---------|-------------|
| `CRAWBL_SERVER_PORT` | `7171` | TCP port the orchestrator HTTP server binds to |
| `CRAWBL_ENVIRONMENT` | `local` | Runtime environment label. `local` disables auth enforcement for dev/test. Other values: `dev`, `prod` |
| `CRAWBL_E2E_TOKEN` | _(empty — disabled)_ | Shared secret that enables the dev-only `POST /v1/e2e/query` endpoint. Leave empty in production |
| `LOG_LEVEL` | `info` | Logging verbosity. Accepted values: `debug`, `info`, `warn`, `error` |

### Runtime / UserSwarm Client

| Variable | Default | Description |
|----------|---------|-------------|
| `CRAWBL_RUNTIME_DRIVER` | `fake` | Selects the runtime client implementation. `fake` echoes messages locally; `userswarm` uses real Kubernetes |
| `CRAWBL_RUNTIME_FAKE_REPLY_PREFIX` | `Fake runtime reply` | Prefix prepended to echoed messages when using the fake driver |
| `CRAWBL_RUNTIME_NAMESPACE` | `userswarms` | Kubernetes namespace where UserSwarm pods are scheduled |
| `CRAWBL_RUNTIME_IMAGE` | _(required in prod)_ | Fully-qualified ZeroClaw container image, e.g. `registry.digitalocean.com/crawbl/zeroclaw:v1.2.3-crawbl1` |
| `CRAWBL_RUNTIME_IMAGE_PULL_SECRET` | _(empty)_ | Name of the Kubernetes `dockerconfigjson` Secret for pulling the ZeroClaw image |
| `CRAWBL_RUNTIME_STORAGE_SIZE` | `2Gi` | PVC capacity requested for each UserSwarm's persistent volume |
| `CRAWBL_RUNTIME_STORAGE_CLASS_NAME` | _(empty — cluster default)_ | Kubernetes StorageClass to pin PVCs to. Empty uses the cluster default (DO Block Storage in dev) |
| `CRAWBL_RUNTIME_DEFAULT_PROVIDER` | `openai` | LLM provider injected into new ZeroClaw runtimes (e.g. `openai`, `anthropic`) |
| `CRAWBL_RUNTIME_DEFAULT_MODEL` | `gpt-5-mini` | LLM model injected into new ZeroClaw runtimes |
| `CRAWBL_RUNTIME_ENV_SECRET_NAME` | _(empty)_ | Name of the Kubernetes Secret (managed by ESO) injected as env vars into ZeroClaw pods — typically contains LLM API keys |
| `CRAWBL_RUNTIME_TOML_OVERRIDES` | _(empty)_ | Raw TOML string merged into every ZeroClaw runtime config before the pod starts. The orchestrator uses this to inject the gateway bind address |
| `CRAWBL_RUNTIME_POLL_TIMEOUT` | `60s` | Maximum time `EnsureRuntime` waits for a UserSwarm to reach `Verified=true`. Go duration format (e.g. `90s`, `2m`) |
| `CRAWBL_RUNTIME_POLL_INTERVAL` | `2s` | How often `EnsureRuntime` re-checks the UserSwarm CR status while waiting |
| `CRAWBL_RUNTIME_PORT` | `42617` | TCP port the ZeroClaw gateway listens on inside the pod |

### MCP Server

| Variable | Default | Description |
|----------|---------|-------------|
| `CRAWBL_MCP_SIGNING_KEY` | _(empty — MCP disabled)_ | HMAC secret for signing and validating per-swarm MCP bearer tokens. Must be set identically on the orchestrator and the UserSwarm webhook. If empty, the MCP server at `/mcp/v1` is not mounted |
| `CRAWBL_FCM_PROJECT_ID` | _(empty — FCM disabled)_ | Google Firebase project ID for push notifications. Both this and `CRAWBL_FCM_SERVICE_ACCOUNT_PATH` must be set to enable FCM |
| `CRAWBL_FCM_SERVICE_ACCOUNT_PATH` | _(empty)_ | Filesystem path to the Firebase service account JSON file |

### UserSwarm Webhook

These variables are read by `ConfigFromEnv()` in `internal/userswarm/webhook/handler.go`.
The webhook binary is a separate process from the orchestrator.

| Variable | Default | Description |
|----------|---------|-------------|
| `USERSWARM_BOOTSTRAP_IMAGE` | `registry.digitalocean.com/crawbl/crawbl-platform:dev` | Image used for the init container that seeds ZeroClaw config on the PVC at first boot |
| `CRAWBL_MCP_ENDPOINT` | _(empty)_ | Orchestrator MCP URL reachable from swarm pods, e.g. `http://orchestrator.backend.svc.cluster.local:7171/mcp/v1`. If empty, MCP is not injected into ZeroClaw bootstrap config |
| `CRAWBL_MCP_SIGNING_KEY` | _(empty)_ | Same HMAC key as the orchestrator side. Used to generate per-swarm bearer tokens |
| `USERSWARM_BACKUP_BUCKET` | _(empty — backups disabled)_ | S3 bucket name for workspace PVC backups. If empty, no backup Job is created |
| `USERSWARM_BACKUP_REGION` | _(empty)_ | AWS/S3 region for the backup bucket |
| `USERSWARM_BACKUP_SECRET_NAME` | _(empty)_ | Kubernetes Secret name containing S3 credentials for backup Jobs |

The webhook's listen address and ZeroClaw config path are set via CLI flags, not environment variables:

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8080` | Host:port the webhook HTTP server listens on |
| `--zeroclaw-config` | `config/zeroclaw.yaml` | Path to the ZeroClaw operator config YAML (usually mounted from a ConfigMap) |

### Database (PostgreSQL)

All variables use the `CRAWBL_` prefix, resolved in `internal/pkg/database/database.go` via `ConfigFromEnv("CRAWBL_")`.

| Variable | Default | Description |
|----------|---------|-------------|
| `CRAWBL_DATABASE_HOST` | `127.0.0.1` | PostgreSQL server hostname |
| `CRAWBL_DATABASE_PORT` | `5432` | PostgreSQL server port |
| `CRAWBL_DATABASE_USER` | `postgres` | Database user name. Supports `_FILE` suffix for Docker secrets |
| `CRAWBL_DATABASE_PASSWORD` | `postgres` | Database password. Supports `_FILE` suffix for Docker secrets |
| `CRAWBL_DATABASE_NAME` | `crawbl` | Database name |
| `CRAWBL_DATABASE_SCHEMA` | `orchestrator` | PostgreSQL schema (sets `search_path`) |
| `CRAWBL_DATABASE_SSLMODE` | `disable` | PostgreSQL SSL mode (`disable`, `require`, `verify-full`, etc.) |
| `CRAWBL_DATABASE_MAX_OPEN_CONNECTIONS` | `20` | Maximum number of open database connections |
| `CRAWBL_DATABASE_MAX_IDLE_CONNECTIONS` | `10` | Maximum number of idle database connections |
| `CRAWBL_DATABASE_CONN_MAX_LIFETIME` | `5m` | Maximum lifetime of a connection. Go duration format |

### Redis (Realtime / Socket.IO)

All variables use the `CRAWBL_` prefix, resolved in `internal/pkg/redisclient/redisclient.go` via `ConfigFromEnv("CRAWBL_")`.
Redis is **optional** — if `CRAWBL_REDIS_ADDR` is not set, realtime is disabled and a no-op broadcaster is used.

| Variable | Default | Description |
|----------|---------|-------------|
| `CRAWBL_REDIS_ADDR` | _(empty — realtime disabled)_ | Redis server address, e.g. `localhost:6379`. Must be set to enable Socket.IO realtime |
| `CRAWBL_REDIS_PASSWORD` | _(empty)_ | Redis password. Supports `_FILE` suffix for Docker secrets |
| `CRAWBL_REDIS_DB` | `0` | Redis logical database number |

### LLM Provider API Keys (injected into ZeroClaw pods)

These are **not read by the orchestrator binary directly**. They are injected into ZeroClaw pods
via `CRAWBL_RUNTIME_ENV_SECRET_NAME` (a Kubernetes Secret managed by ESO). Inside each pod,
ZeroClaw scans for these variables in priority order — first non-empty value wins:

| Variable | Priority | Provider |
|----------|----------|----------|
| `OPENAI_API_KEY` | 1 (highest) | OpenAI |
| `USERSWARM_API_KEY` | 2 | Generic / platform-managed |
| `ZEROCLAW_API_KEY` | 3 | ZeroClaw native |
| `API_KEY` | 4 | Generic fallback |
| `OPENROUTER_API_KEY` | 5 | OpenRouter |
| `ANTHROPIC_API_KEY` | 6 | Anthropic |
| `GEMINI_API_KEY` | 7 (lowest) | Google Gemini |

### Legal Documents

| Variable | Default | Description |
|----------|---------|-------------|
| `CRAWBL_LEGAL_TERMS_OF_SERVICE` | `https://crawbl.com/terms` | URL or text content for the terms of service |
| `CRAWBL_LEGAL_TERMS_OF_SERVICE_VERSION` | `v1` | Version string for the current terms of service |
| `CRAWBL_LEGAL_PRIVACY_POLICY` | `https://crawbl.com/privacy` | URL or text content for the privacy policy |
| `CRAWBL_LEGAL_PRIVACY_POLICY_VERSION` | `v1` | Version string for the current privacy policy |

---

## Hardcoded Defaults

Values compiled into the binary that require a code change and redeploy to modify.

### ZeroClaw Agent Defaults

Defined as constants in `internal/zeroclaw/types.go`.

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultTemperature` | `0.7` | LLM sampling temperature applied to all provider calls |
| `DefaultTimeoutSecs` | `30` | Default timeout in seconds for provider and HTTP tool calls |
| `DefaultTimeoutSecsShort` | `15` | Short timeout used for web search calls |
| `DefaultMaxResponseSize` | `1,000,000` (1 MB) | Maximum HTTP response body size for the `http_request` tool |
| `DefaultMaxResponseSizeSmall` | `500,000` (500 KB) | Maximum response body size for the `web_fetch` tool |
| `DefaultMaxResults` | `5` | Maximum number of results returned by the `web_search` tool |
| `DefaultProviderRetries` | `2` | Number of automatic retries on LLM provider failure |
| `DefaultProviderBackoffMs` | `500` | Milliseconds to wait between provider retry attempts |

### ZeroClaw Bootstrap TOML Defaults

Hardcoded in `BuildConfigTOML()` in `internal/zeroclaw/toml.go`. These are the per-user
config values baked into each ZeroClaw runtime at provisioning time before any per-user overrides.

| Field | Hardcoded Value | Description |
|-------|-----------------|-------------|
| `default_provider` | `openai` | LLM provider used unless overridden by `CRAWBL_RUNTIME_DEFAULT_PROVIDER` or the UserSwarm CR |
| `default_model` | `gpt-5-mini` | LLM model used unless overridden by `CRAWBL_RUNTIME_DEFAULT_MODEL` or the UserSwarm CR |
| `autonomy.level` | `supervised` | Autonomy level; agent asks before most actions |
| `autonomy.workspace_only` | `true` | Agent file access is restricted to the workspace directory |
| `http_request.enabled` | `true` | HTTP request tool is enabled by default |
| `http_request.allow_private_hosts` | `false` | Agent cannot call internal network addresses |
| `web_fetch.enabled` | `true` | Web fetch tool is enabled by default |
| `web_fetch.allowed_domains` | `["*"]` | All domains allowed for web fetch |
| `web_search.enabled` | `true` | Web search tool is enabled by default |
| `gateway.host` | `[::]` | ZeroClaw gateway binds to all interfaces (IPv6 dual-stack) |
| `gateway.allow_public_bind` | `true` | Binding to all interfaces is permitted; access controlled by K8s NetworkPolicy |
| `gateway.require_pairing` | `true` | ZeroClaw requires pairing before accepting traffic |
| `mcp.deferred_loading` | `false` | MCP servers are loaded at startup, not lazily |
| MCP tool timeout | `30s` | Timeout for individual MCP tool calls from ZeroClaw to the orchestrator |

### ZeroClaw Operator Config Defaults

Applied in `DefaultConfig()` in `internal/zeroclaw/config.go`. These are cluster-wide defaults
used when `config/zeroclaw.yaml` does not exist or does not override a field.

| Field (YAML path) | Default Value | Description |
|-------------------|---------------|-------------|
| `defaults.temperature` | `0.7` | LLM temperature |
| `defaults.timeout` | `30` | Request timeout (seconds) |
| `defaults.shortTimeout` | `15` | Short timeout (seconds) |
| `defaults.providerRetries` | `2` | Provider retry count |
| `defaults.providerBackoffMs` | `500` | Provider backoff (ms) |
| `httpRequest.maxResponseSize` | `1,000,000` | Max HTTP response body (bytes) |
| `httpRequest.allowedDomains` | `["*"]` | All domains allowed |
| `webFetch.maxResponseSize` | `500,000` | Max web fetch body (bytes) |
| `webSearch.provider` | `duckduckgo` | Web search backend |
| `webSearch.maxResults` | `5` | Maximum search results returned |
| `autonomy.allowedCommands` | `git, ls, cat, grep, find, pwd, wc, head, tail, date, sed` | Shell commands the agent may run without approval |
| `autonomy.forbiddenPaths` | `/etc, /root, /usr, /bin, /sbin, /lib, /opt, /boot, /dev, /proc, /sys, /var, /tmp, ~/.ssh, ~/.gnupg, ~/.aws, ~/.config` | Filesystem paths the agent cannot access |
| `autonomy.autoApprove` | _(all tools in the tool catalog)_ | Tool calls that do not require user approval |

### Orchestrator Defaults

Defined in `internal/orchestrator/types.go` and `internal/orchestrator/server/types.go`.

| Constant | Value | File | Description |
|----------|-------|------|-------------|
| `DefaultWorkspaceName` | `My Swarm` | `orchestrator/types.go` | Name given to workspaces created at sign-up |
| `DefaultSwarmTitle` | `My Swarm` | `orchestrator/types.go` | Title given to new swarm instances |
| `DefaultAgentAvatarURL` | `""` | `orchestrator/types.go` | Default avatar URL for agents (none) |
| `DefaultServerPort` | `7171` | `orchestrator/server/types.go` | Orchestrator HTTP listen port |
| `DefaultReadHeaderTimeout` | `5s` | `orchestrator/server/types.go` | Maximum duration to read request headers (slowloris protection) |
| `shutdownTimeout` | `10s` | `cmd/.../orchestrator.go` | Graceful shutdown window before forced exit |

### Kubernetes / Pod Defaults

Defined as constants in `internal/userswarm/webhook/types.go` and `internal/userswarm/client/types.go`.

| Constant | Value | File | Description |
|----------|-------|------|-------------|
| `runtimeUID` | `65532` | `webhook/types.go` | UID for ZeroClaw runtime containers (distroless `nonroot`) |
| `runtimeGID` | `65532` | `webhook/types.go` | GID for ZeroClaw runtime containers (distroless `nonroot`) |
| `bootstrapConfigMode` | `0444` | `webhook/types.go` | File permission for bootstrap ConfigMap mounts (world-readable, immutable) |
| `DefaultRuntimeNamespace` | `userswarms` | `client/types.go` | Kubernetes namespace for all UserSwarm pods |
| `DefaultRuntimeStorageSize` | `2Gi` | `client/types.go` | PVC capacity for each user's workspace volume |
| `DefaultRuntimePort` | `42617` | `client/types.go` | ZeroClaw gateway TCP port inside the pod |
| `DefaultGatewayPort` | `42617` | `api/v1alpha1/userswarm_types.go` | CRD-level gateway port default (shared source with `DefaultRuntimePort`) |
| `DefaultPollTimeout` | `60s` | `client/types.go` | Max wait time for `EnsureRuntime` to reach `Verified=true` |
| `DefaultPollInterval` | `2s` | `client/types.go` | Poll frequency while waiting for `Verified=true` |
| `defaultHTTPTimeout` | `90s` | `client/types.go` | HTTP client timeout for orchestrator-to-pod webhook calls |
| `readyConditionType` | `"Ready"` | `client/types.go` | Kubernetes condition type that signals a healthy runtime |
| `ResyncAfterSeconds` | `30` | `webhook/handler.go` | Metacontroller resync interval for each UserSwarm |

### Runtime Client Drivers

| Constant | Value | File | Description |
|----------|-------|------|-------------|
| `DriverFake` | `fake` | `client/types.go` | Selects the local echo client (no Kubernetes required) |
| `DriverUserSwarm` | `userswarm` | `client/types.go` | Selects the real Kubernetes-backed client |
| `DefaultFakeReplyPrefix` | `Fake runtime reply` | `client/types.go` | Echo prefix used by the fake client |

### Database Connection Defaults

Defined in `internal/pkg/database/types.go`.

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultHost` | `127.0.0.1` | PostgreSQL host |
| `DefaultPort` | `5432` | PostgreSQL port |
| `DefaultUser` | `postgres` | Database user |
| `DefaultPassword` | `postgres` | Database password |
| `DefaultName` | `crawbl` | Database name |
| `DefaultSchema` | `orchestrator` | PostgreSQL schema |
| `DefaultSSLMode` | `disable` | SSL mode |
| `DefaultMaxOpenConnections` | `20` | Max open connections in the pool |
| `DefaultMaxIdleConnections` | `10` | Max idle connections in the pool |
| `DefaultConnMaxLifetime` | `5m` | Max connection lifetime |
| `DefaultPingAttempts` | `5` | Startup ping retry count |
| `DefaultPingDelay` | `2s` | Delay between startup ping attempts |

### Redis Connection Defaults

Defined in `internal/pkg/redisclient/types.go`.

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultAddr` | `localhost:6379` | Redis server address |
| `DefaultDB` | `0` | Redis database number |
| `DefaultPingAttempts` | `5` | Startup ping retry count |
| `DefaultPingDelay` | `2s` | Delay between startup ping attempts |

### MCP Server Identity

Hardcoded in `internal/orchestrator/mcp/server.go`.

| Field | Value | Description |
|-------|-------|-------------|
| Server name | `crawbl-orchestrator` | MCP implementation name advertised to ZeroClaw clients |
| Server version | `1.0.0` | MCP implementation version |
| Audit log truncation | `2048` bytes | Maximum output size written per tool call to `mcp_audit_logs` |
| Audit log timeout | `5s` | Async write timeout for audit log entries |

---

## Integration Providers

Static catalog defined in `internal/orchestrator/service/integrationservice/integrationservice.go`.
`IsEnabled=false` means the integration is shown in the mobile app as "coming soon" but is not connectable.

| Provider | Name | Enabled | Icon URL |
|----------|------|---------|----------|
| `google_calendar` | Google Calendar | Yes | `https://cdn.crawbl.com/integrations/google-calendar.png` |
| `gmail` | Gmail | Yes | `https://cdn.crawbl.com/integrations/gmail.png` |
| `slack` | Slack | No | `https://cdn.crawbl.com/integrations/slack.png` |
| `jira` | Jira | No | `https://cdn.crawbl.com/integrations/jira.png` |
| `notion` | Notion | No | `https://cdn.crawbl.com/integrations/notion.png` |
| `asana` | Asana | No | `https://cdn.crawbl.com/integrations/asana.png` |
| `github` | GitHub | No | `https://cdn.crawbl.com/integrations/github.png` |
| `zoom` | Zoom | No | `https://cdn.crawbl.com/integrations/zoom.png` |

To add a new provider: add an entry to `providerCatalog` in `integrationservice.go` with `IsEnabled: true`, then implement `GetOAuthConfig` and `HandleOAuthCallback` for that provider.

---

## Tool Catalog

Canonical tool list defined in `internal/zeroclaw/tools.go` (`defaultToolCatalog`).
This is the **single source of truth** — adding an entry here automatically:
1. Makes the tool visible in the mobile app's tools screen via `GET /v1/workspaces/{id}/tools`
2. Adds the tool to ZeroClaw's `auto_approve` list so it runs without user confirmation

| Tool Name | Display Name | Category |
|-----------|-------------|----------|
| `web_search_tool` | Web Search | search |
| `web_fetch` | Web Fetch | search |
| `http_request` | HTTP Request | search |
| `file_read` | Read Files | files |
| `file_write` | Write Files | files |
| `file_edit` | Edit Files | files |
| `glob_search` | File Search | files |
| `content_search` | Content Search | files |
| `memory_store` | Remember | memory |
| `memory_recall` | Recall | memory |
| `memory_forget` | Forget | memory |
| `cron_add` | Schedule Task | scheduling |
| `cron_list` | List Schedules | scheduling |
| `cron_remove` | Remove Schedule | scheduling |
| `cron_update` | Update Schedule | scheduling |
| `cron_run` | Run Now | scheduling |
| `cron_runs` | Run History | scheduling |
| `orchestrator__send_push_notification` | Push Notification | notification |
| `orchestrator__get_user_profile` | User Profile | context |
| `orchestrator__get_workspace_info` | Workspace Info | context |
| `orchestrator__list_conversations` | Conversations | context |
| `orchestrator__search_past_messages` | Search Messages | context |
| `calculator` | Calculator | utility |
| `weather` | Weather | utility |
| `image_info` | Image Info | utility |
| `shell` | Shell Commands | shell |

---

## Error Codes

Client-facing error codes defined in `internal/pkg/errors/types.go`.
These are returned in API error responses as the `code` field.

| Code | Constant | Description |
|------|----------|-------------|
| `AUTH0001` | `ErrCodeUnauthorized` | User is not authenticated |
| `AUTH0002` | `ErrCodeInvalidToken` | Token is invalid or expired |
| `USR0001` | `ErrCodeUserDeleted` | User account has been deleted |
| `USR0002` | `ErrCodeUserNotFound` | User does not exist |
| `USR0003` | `ErrCodeUserWrongFirebaseUID` | Firebase UID mismatch during auth (server-side only) |
| `USR0004` | `ErrCodeUserFirebaseUIDMismatch` | Firebase UID does not match expected |
| `WSP0001` | `ErrCodeWorkspaceNotFound` | Workspace does not exist |
| `AGT0001` | `ErrCodeAgentNotFound` | Agent does not exist |
| `CHT0001` | `ErrCodeConversationNotFound` | Conversation does not exist |
| `MSG0001` | `ErrCodeMessageNotFound` | Message does not exist |
| `MSG0002` | `ErrCodeUnsupportedMessage` | Message type is not supported |
| `RTM0001` | `ErrCodeRuntimeNotReady` | User swarm runtime is still starting |
| `INT0001` | `ErrCodeIntegrationNotConfigured` | Integration provider is not yet configured |

---

## Operator Config YAML Keys

The `config/zeroclaw.yaml` file is loaded by both the orchestrator webhook and the orchestrator
server. All fields are optional — any missing key falls back to the hardcoded defaults above.

```yaml
defaults:
  temperature: 0.7          # float: LLM sampling temperature
  timeout: 30               # int: request timeout (seconds)
  shortTimeout: 15          # int: short timeout (seconds)
  providerRetries: 2        # uint32: retry count on provider failure
  providerBackoffMs: 500    # uint64: backoff between retries (ms)

httpRequest:
  maxResponseSize: 1000000  # int: max response body for http_request tool (bytes)
  allowedDomains:           # list of allowed domains; ["*"] means all
    - "*"

webFetch:
  maxResponseSize: 500000   # int: max response body for web_fetch tool (bytes)

webSearch:
  provider: duckduckgo      # string: search backend ("duckduckgo", "brave", "searxng")
  maxResults: 5             # int: max results returned

autonomy:
  allowedCommands:          # list of shell commands the agent may run without approval
    - git
    - ls
    - cat
    - grep
    - find
    - pwd
    - wc
    - head
    - tail
    - date
    - sed
  forbiddenPaths:           # filesystem paths the agent cannot access
    - /etc
    - /root
    - /usr
    - /bin
    - /sbin
    - /lib
    - /opt
    - /boot
    - /dev
    - /proc
    - /sys
    - /var
    - /tmp
    - ~/.ssh
    - ~/.gnupg
    - ~/.aws
    - ~/.config
  autoApprove:              # tool names that run without user confirmation
    - web_search_tool
    - web_fetch
    # ... (all tools in the catalog by default)
```

---

## Samples

- `config/samples/` — Sample Kubernetes resource manifests (CRD instances, Helm values, etc.)
- `config/crd/` — UserSwarm CRD definitions
- `config/helm/` — Helm chart templates
