# UserSwarm Lifecycle

## Purpose

Manages per-workspace crawbl-agent-runtime pod lifecycle via a `UserSwarm` CRD. The `client` package creates/deletes CRs; Metacontroller calls the `webhook` to reconcile the desired resource graph; the `reaper` CronJob removes stale and orphaned CRs.

## Layout

- `client/` — Orchestrator-side K8s client: creates/updates/deletes `UserSwarm` CRs,
  polls for `Verified=true`, and forwards agent traffic to the runtime pod over gRPC
  (`SendText`, `SendTextStream`, Memory CRUD). `fake.go` provides a test double.
- `webhook/` — Metacontroller sync handler (`/sync`). Stateless HTTP server that receives
  every CR change event and returns the full desired child graph (ServiceAccount, Service,
  Deployment). Also drives finalization (teardown) when a CR is deleted.
- `reaper/` — One-shot CronJob: Phase 1 deletes stale e2e `UserSwarm` CRs and
  soft-deletes their database users; Phase 2 orphan-sweeps any CR whose `Spec.UserID`
  no longer has an active row in Postgres.

## Conventions

**CR naming.** `"workspace-" + strings.ToLower(workspaceID)`. The webhook reverses this
with `workspaceIDFromSwarmName`. Keep the two helpers inverse of each other.

**CRD watch.** Metacontroller owns the watch loop. The webhook is stateless
request/response; all state lives in the CR status or child resource annotations.

**Status update.** The webhook sets `status.phase`, `status.runtimeNamespace`,
`status.serviceName`, `status.readyReplicas`, and a single `Ready` condition.
Readiness is `True` only when `observedReadyReplicas > 0` on the child Deployment.
The client reads `Ready` via `isConditionTrue` and surfaces it as `RuntimeStatus.Verified`.

**Readiness gate.** `EnsureRuntime` polls `getRuntimeState` every 2 s (default) until
`Verified=true` or 60 s elapses. Pass `WaitForVerified: true` when a live pod is
required before sending a message.

**Shared namespace.** All pods land in `Spec.Placement.RuntimeNamespace` (default
`"userswarms"`). Never introduce per-user namespace logic.

**Labels.** Child resources carry `crawbl.ai/userswarm=<cr-name>` and
`crawbl.ai/user-id=<userID>`. E2e CRs also get `crawbl.ai/e2e=true` when the
principal subject starts with `"e2e-"`. The reaper identifies e2e swarms via the DB
subject prefix, not this label.

**Configuration injection.** Runtime pods receive config via CLI flags (injected by
`buildRuntimeDeployment`) and the `envSecretRef` ESO-managed Secret. No ConfigMap,
no TOML. Non-secret vars come from the webhook process env (`runtimeConfigFromEnv`).

**gRPC transport.** HMAC bearer tokens keyed on `(userID, workspaceID)`. Connections
pooled per target in `crawblgrpc.Pool`. Drop the pool entry in `DeleteRuntime`.

**Reaper phases.** Phase 1 targets e2e swarms older than `Config.MaxAge`, driven off
the CR's `CreationTimestamp` (not the DB row). Phase 2 removes any CR whose
`Spec.UserID` has no active Postgres row. Per-item errors are counted but never abort.

## Gotchas

- `UserSwarm.status` is the sole source of truth for runtime readiness. Never mirror
  `phase`, `Verified`, or readiness signals into Postgres.
- Shared namespace (`"userswarms"`) only — no namespace-per-user logic.
- The webhook serves `/sync` for Metacontroller only; no other caller should POST to it.
- `Spec.Suspend=true` drives replicas to 0 and phase to `"Suspended"`. Honour this in
  any new readiness or scheduling logic.
- Legacy HTTP wire (port 42617) is gone. All runtime communication is gRPC on port 42618.
- `Config.DryRun=true` logs candidates but makes no mutations; counters still populate.

## Key Files

- `client/types.go` — `Client` interface, all opts structs, driver constants, and
  `UserSwarmConfig`. Start here when adding a new runtime operation.
- `client/userswarm.go` — `NewUserSwarmClient`, `EnsureRuntime`, `DeleteRuntime`,
  `desiredUserSwarm`, `getRuntimeState`. Core CR lifecycle logic.
- `client/grpc_converse.go` — `SendText` / `SendTextStream` gRPC implementations; `client/fake.go` — test double (driver `"fake"`).
- `webhook/surface.go` — `Run()` entrypoint, HTTP route registration, `runtimeConfigFromEnv`.
- `webhook/flow.go` — `driveSync` / `reconcileGraph` / `finalizeGraph` decision tree and
  `readinessSnapshot`.
- `webhook/blueprint_runtime.go` — `buildRuntimeDeployment`: the authoritative shape of
  the runtime pod (container flags, env, secrets, security context).
- `reaper/reaper.go` — `Run()`, both cleanup phases, and all DB/K8s mutation helpers.
