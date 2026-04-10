# Infrastructure (Pulumi Bootstrap)

## Purpose

Pulumi-based bootstrap for the DOKS cluster and ArgoCD installation only. This package does NOT deploy application workloads — all Helm charts and ongoing K8s resources are managed by ArgoCD after bootstrap.

## Scope Boundary (CRITICAL)

- This package: provision DOKS cluster, container registry, VPC, and install ArgoCD via Helm. Stop there.
- Everything else (app Helm charts, namespace configs, workload manifests) lives in `crawbl-argocd-apps/components/*/chart/` and is reconciled by ArgoCD. See the root `CLAUDE.md` for the authoritative rule.

## Layout

- `cluster/` — DOKS cluster provisioning: VPC, node pool, container registry, project attachment, kubeconfig export
- `platform/` — Kubernetes platform services: ArgoCD Helm install, AWS S3/Secrets Manager backup resources
- `pulumi.go` — top-level `Stack` type, `NewStack`, `buildProgram` (3-phase: cluster → k8s provider → platform), `Up`/`Preview`/`Destroy`/`Outputs`
- `types.go` — `Config`, `Stack`, `PreviewResult`, `UpResult`

## Conventions

- **Stack selection:** stack name equals `Config.Environment` (e.g. `"dev"`). `auto.UpsertStackInlineSource` creates or selects it. Never hardcode stack names.
- **Config source of truth:** cluster values live in `Pulumi.<env>.yaml` via `StackClusterConfig` (YAML tags). No magic defaults in Go — every value must be in the stack config file.
- **ESC for secrets:** provider tokens and environment variables are injected via Pulumi ESC (`Config.ESCEnvironment`, e.g. `"crawbl/dev"`). Never put DigitalOcean/AWS tokens in Go source or `.env` for the bootstrap path.
- **Resource naming:** cluster name is always `"crawbl-" + env`. Registry and project names come from stack config.
- **Three-phase program:** (1) cluster, (2) k8s provider from kubeconfig output, (3) platform. Keep this order — phases are sequentially dependent.
- **AWS backup resources** are optional: set `AWSRegion = ""` in `platform.Config` to skip them entirely.
- **SSH private key for ArgoCD repo** (`ArgoCDRepoSSHPrivateKey`) must come from ESC or a secret config value — never plain text.

## Gotchas

- Never add application Helm charts here. The only Helm chart permitted is ArgoCD itself (bootstrap boundary).
- Never replicate resources that ArgoCD will manage. Adding a K8s resource here means it will conflict with ArgoCD sync.
- `Destroy` tears down the DOKS cluster and VPC. If `DestroyAllAssociatedResources: true`, load balancers and volumes are also deleted. This is irreversible in production — double-check the stack before calling `Stack.Destroy`.
- Pulumi state is remote (Pulumi Cloud). Never run `pulumi stack rm` without confirming state backend and environment.
- The `crawbl app deploy` command is for application image deploys, not cluster bootstrap. Do not use it here.
- `StackClusterConfig` is the YAML-deserialized form; `cluster.Config` is the runtime form. Always populate via `ConfigFromStack` — do not construct `cluster.Config` directly.

## Key Files

- `pulumi.go` — `NewStack`, `buildProgram`, and `Stack` methods (`Up`, `Preview`, `Destroy`, `Outputs`)
- `types.go` — `Config` (top-level infra config), `Stack`, `PreviewResult`, `UpResult`
- `cluster/cluster.go` — `NewCluster` entry point; orchestrates VPC, version lookup, cluster, registry, project attachment
- `cluster/types.go` — `Config`, `StackClusterConfig` (YAML source of truth), `Outputs`, `NodeTaint`
- `cluster/cluster_resources.go` — low-level Pulumi resource constructors (VPC, DOKS cluster, registry)
- `platform/types.go` — `platform.Config` (ArgoCD + AWS backup options)
- `platform/platform.go` — `NewPlatform` entry point
- `platform/platform_resources.go` — ArgoCD Helm release and AWS resource construction

## Related

- `crawbl-argocd-apps/` — all application Helm charts live there, reconciled by ArgoCD after bootstrap completes
- `cmd/crawbl/` — CLI entry point that wires `infra.Config` and calls `Stack.Up` / `Stack.Preview`
- Root `CLAUDE.md` — authoritative rule on the bootstrap/ArgoCD split
