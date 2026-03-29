# 🔬 Internals Reference

> Every hardcoded constant, error code, and default compiled into the binary.
> You probably want [`README.md`](README.md) instead — this is for deep dives.

---

## Tool Catalog

Single source of truth: `internal/zeroclaw/tools.go`

Adding a tool here auto-enables it in the mobile app AND ZeroClaw's auto-approve list.

| Tool | Display Name | Category |
|------|-------------|----------|
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

## Integration Providers

Source: `internal/orchestrator/service/integrationservice/integrationservice.go`

| Provider | Enabled | Description |
|----------|---------|-------------|
| `google_calendar` | Yes | View and create calendar events |
| `gmail` | Yes | Search and read emails |
| `slack` | No | Send messages, search channels |
| `jira` | No | Search and manage issues |
| `notion` | No | Search pages, manage databases |
| `asana` | No | Manage tasks, track projects |
| `github` | No | Browse repos, manage PRs |
| `zoom` | No | Schedule meetings |

---

## Error Codes

Source: `internal/pkg/errors/types.go`

| Code | What |
|------|------|
| `AUTH0001` | Not authenticated |
| `AUTH0002` | Invalid/expired token |
| `USR0001` | Account deleted |
| `USR0002` | User not found |
| `WSP0001` | Workspace not found |
| `AGT0001` | Agent not found |
| `CHT0001` | Conversation not found |
| `MSG0001` | Message not found |
| `MSG0002` | Unsupported message type |
| `RTM0001` | Agent pod still starting |
| `INT0001` | Integration not configured |

---

## ZeroClaw Defaults

Source: `internal/zeroclaw/types.go`

| Constant | Value | What |
|----------|-------|------|
| Temperature | `0.7` | LLM sampling temperature |
| Timeout | `30s` | Provider call timeout |
| Short timeout | `15s` | Web search timeout |
| Max response (HTTP) | `1 MB` | `http_request` tool body limit |
| Max response (fetch) | `500 KB` | `web_fetch` tool body limit |
| Max search results | `5` | `web_search` result count |
| Provider retries | `2` | Auto-retry on LLM failure |
| Retry backoff | `500ms` | Wait between retries |

---

## Pod / K8s Defaults

Source: `internal/userswarm/webhook/types.go`, `internal/userswarm/client/types.go`

| What | Value |
|------|-------|
| Container UID/GID | `65532` (distroless nonroot) |
| ConfigMap file permissions | `0444` (read-only) |
| Runtime namespace | `userswarms` |
| PVC size | `2Gi` |
| Gateway port | `42617` |
| Poll timeout (readiness) | `60s` |
| Poll interval | `2s` |
| HTTP client timeout | `90s` |
| Resync interval | `30s` |

---

## Orchestrator Defaults

Source: `internal/orchestrator/types.go`, `internal/orchestrator/server/types.go`

| What | Value |
|------|-------|
| Default workspace name | `My Swarm` |
| Server port | `7171` |
| Read header timeout | `5s` |
| Shutdown timeout | `10s` |

---

## MCP Server

Source: `internal/orchestrator/mcp/server.go`

| What | Value |
|------|-------|
| Server name | `crawbl-orchestrator` |
| Server version | `1.0.0` |
| Audit log max output | `2048 bytes` |
| Audit write timeout | `5s` |

---

## LLM API Key Priority

Inside ZeroClaw pods, scanned in order (first non-empty wins):

1. `OPENAI_API_KEY`
2. `USERSWARM_API_KEY`
3. `ZEROCLAW_API_KEY`
4. `API_KEY`
5. `OPENROUTER_API_KEY`
6. `ANTHROPIC_API_KEY`
7. `GEMINI_API_KEY`
