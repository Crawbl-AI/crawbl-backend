# crawbl-backend

The control plane between the Crawbl mobile app and per-user ZeroClaw runtimes on Kubernetes. Handles auth, user provisioning, swarm lifecycle, LLM routing, integration adapters, and the Kubernetes operator that runs isolated swarm workloads.

## Repository structure

```
cmd/
  orchestrator/         HTTP API server (mobile-facing)
  userswarm-operator/   Kubernetes operator for per-user swarm runtimes
  crawbl/               CLI for infra, build, and deploy tasks
internal/
  orchestrator/         API server domain logic, services, handlers
  operator/             UserSwarm reconciler and controller
  infra/                Pulumi infrastructure-as-code (cluster + ArgoCD bootstrap only)
  pkg/                  Shared packages (database, errors, HTTP)
api/                    Kubernetes CRD types (v1alpha1)
config/
  helm/                 ArgoCD Helm values for Pulumi
  zeroclaw.yaml         ZeroClaw operator bootstrap defaults
migrations/             PostgreSQL migration files
dockerfiles/            Dockerfiles for each binary
e2e/                    Venom end-to-end test suite
```

## Prerequisites

- Go 1.23+
- Docker with buildx
- kubectl
- helm
- doctl (authenticated with DigitalOcean)
- Pulumi CLI

## Local development

Start Postgres and apply migrations:

```sh
make setup
```

Start the full local stack (orchestrator + Postgres + migrations):

```sh
make run
```

Rebuild from a clean database:

```sh
make run-clean
```

Stop all local services:

```sh
make stop
```

Run end-to-end tests (requires [venom](https://github.com/ovh/venom)):

```sh
make test-e2e
```

Run a single e2e test file:

```sh
make test-e2e-one FILE=01_orchestrator_smoke.yml
```

## CLI

The `crawbl` CLI is the primary tool for managing infrastructure and deployments.

### Infrastructure

Backed by Pulumi. Manages the full DigitalOcean stack.

```sh
crawbl infra init
crawbl infra plan
crawbl infra apply
crawbl infra destroy
```

### Image builds

Uses Docker buildx, pushes to DigitalOcean Container Registry.

```sh
crawbl app build orchestrator --tag <tag> --push
crawbl app build operator --tag <tag> --push
crawbl app build zeroclaw --tag <tag> --push
```

ZeroClaw builds clone the upstream repository at a pinned ref. Override with `--source`, `--ref`, `--target`, `--repository`.

### Deployments

Deployments are managed by ArgoCD via the `crawbl-argocd-apps` repo. Push a new image with `crawbl app build`, then update the image tag in `crawbl-argocd-apps` values and commit — ArgoCD syncs the rollout automatically.

## Infrastructure

Pulumi manages two layers:

- **Cluster** - DigitalOcean Kubernetes (DOKS) with `registryIntegration=true`, VPC, container registry
- **Platform** - ArgoCD Helm release only

After `crawbl infra apply`, ArgoCD takes over and deploys all application resources (namespaces, External Secrets Operator, PostgreSQL, Redis, cert-manager, Envoy Gateway, operators, external-dns) from `crawbl-argocd-apps`.

Current environment: `dev` in DigitalOcean `fra1`.

Code lives in `internal/infra/`. Configuration in `Pulumi.yaml` and `Pulumi.dev.yaml`. ArgoCD values in `config/helm/`.
