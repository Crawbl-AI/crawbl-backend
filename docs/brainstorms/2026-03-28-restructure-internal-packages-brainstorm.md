---
date: 2026-03-28
topic: restructure-internal-packages
---

# Restructure cmd/ and internal/ by Domain

## What We're Building

Reorganize the project structure so that `internal/` is grouped by domain (what the code is about), not by deployment artifact (operator vs orchestrator). Remove the dead `internal/operator/` name. Mirror the domain grouping in `cmd/`.

## Current Problems

1. **`internal/operator/` is a lie** — no operator exists after Metacontroller migration. Contains webhook + zeroclaw config.
2. **`internal/orchestrator/runtimeclient/`** — K8s client for UserSwarm CRDs lives under orchestrator but is about swarm lifecycle.
3. **`internal/testsuite/reaper/`** — e2e cleanup is swarm lifecycle, not a test utility.
4. **`cmd/crawbl/platform/`** — flat files (backup, bootstrap, reaper, webhook) mixed with nested orchestrator/. Inconsistent.

## Proposed Structure

### internal/

```
internal/
  zeroclaw/                     ← AI runtime config (from operator/zeroclaw/)
    config.go, toml.go, markdown.go, bootstrap.go

  userswarm/                    ← UserSwarm lifecycle (NEW domain package)
    client/                       (K8s CRUD for UserSwarm CRs — from orchestrator/runtimeclient/)
    webhook/                      (Metacontroller sync handler — from operator/webhook/)
    reaper/                       (e2e cleanup — from testsuite/reaper/)

  orchestrator/                 ← HTTP API (unchanged)
    server/, service/, repo/

  infra/                        ← Pulumi IaC (unchanged)
  testsuite/e2e/                ← E2E runner (reaper moves out)
  pkg/                          ← Shared utilities (unchanged)
```

### cmd/

```
cmd/crawbl/platform/
  root.go
  orchestrator/                 ← KEEP
    orchestrator.go               (crawbl platform orchestrator)
    migrate.go                    (crawbl platform orchestrator migrate)
  userswarm/                    ← NEW (groups all swarm subcommands)
    webhook.go                    (crawbl platform userswarm webhook)
    bootstrap.go                  (crawbl platform userswarm bootstrap)
    backup.go                     (crawbl platform userswarm backup)
    reaper.go                     (crawbl platform userswarm reaper)
```

## Key Decisions

- **Domain grouping**: `zeroclaw/` (AI runtime), `userswarm/` (swarm lifecycle), `orchestrator/` (HTTP API)
- **`userswarm` naming**: used consistently everywhere — package name, CLI command, CRD, namespace prefix
- **`internal/operator/` deleted**: replaced by `internal/userswarm/` + `internal/zeroclaw/`
- **`runtimeclient` → `userswarm/client`**: domain ownership over consumer coupling
- **`testsuite/reaper` → `userswarm/reaper`**: it's swarm cleanup, not a test tool
- **cmd/ mirrors internal/**: `cmd/.../userswarm/` maps to `internal/userswarm/`

## Open Questions

None — all resolved during brainstorm.

## Next Steps

→ `/workflows:plan` for implementation (file moves, import updates, ArgoCD manifest updates)
