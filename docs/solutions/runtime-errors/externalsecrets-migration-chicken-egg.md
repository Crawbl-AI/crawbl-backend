---
title: ExternalSecrets + Migration Job Chicken-and-Egg
category: runtime-errors
component: argocd, external-secrets, orchestrator
severity: critical
date_solved: 2026-03-27
symptoms:
  - "orchestrator-orchestrator-migrate: CreateContainerConfigError"
  - 'Error: secret "orchestrator-vault-secrets" not found'
  - Migration job stuck, orchestrator sync blocked
tags: [argocd, eso, migration, sync-waves, helm-hooks]
---

# ExternalSecrets + Migration Job Chicken-and-Egg

## Problem

After ArgoCD restructure, the orchestrator app sync was stuck. The migration job couldn't start because the `orchestrator-vault-secrets` K8s Secret didn't exist. The Secret is created by ESO's ExternalSecret, but the ExternalSecret hadn't been synced yet because ArgoCD was waiting for the migration job to complete.

## Root Cause

The migration job used Helm hooks (`helm.sh/hook: pre-install,pre-upgrade`) which ArgoCD converts to **PreSync** phase. The ExternalSecrets were in a separate ArgoCD source (raw manifests in `components/orchestrator/resources/`) and synced during the **Sync** phase.

Execution order:
1. **PreSync**: Migration job starts → needs `orchestrator-vault-secrets` → **FAILS** (secret doesn't exist)
2. **Sync**: ExternalSecrets would be applied → creates the secret → **NEVER REACHED**

## Solution

Replaced Helm hooks with ArgoCD sync waves to control ordering within the Sync phase:

### ExternalSecrets (wave -5) — applied first

```yaml
# components/orchestrator/resources/es-orchestrator-secrets.yaml
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "-5"
```

### Migration Job (wave -1) — runs after secrets exist

```yaml
# components/orchestrator/chart/templates/migration-job.yaml
annotations:
    argocd.argoproj.io/hook: Sync
    argocd.argoproj.io/hook-delete-policy: BeforeHookCreation,HookSucceeded
    argocd.argoproj.io/sync-wave: "-1"
```

### Regular resources (wave 0) — deployment, services, etc.

Order: ExternalSecrets (wave -5) → ESO syncs → Secret created → Migration job (wave -1) → Deployment (wave 0)

## Investigation Steps

1. `kubectl describe pod orchestrator-migrate` showed `Error: secret "orchestrator-vault-secrets" not found`
2. Checked ArgoCD sync phases — Helm hooks run PreSync, resources run Sync
3. Multi-source app: chart (Helm) + resources (raw YAML) — different sync phases
4. Manually applied ExternalSecrets to unblock, then fixed ordering with sync waves

## Prevention

- Never use Helm hooks in ArgoCD multi-source apps where resources depend on each other across sources
- Use ArgoCD sync waves for ordering within the Sync phase
- ExternalSecrets that create secrets needed by other resources should have negative sync waves
- Document sync wave ordering in the app's README

## Related

- `crawbl-argocd-apps/components/orchestrator/resources/es-orchestrator-secrets.yaml`
- `crawbl-argocd-apps/components/orchestrator/chart/templates/migration-job.yaml`
- `crawbl-docs/ops/argocd/architecture/sync-waves.md`
