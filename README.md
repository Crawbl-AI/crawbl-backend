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
  infra/                Pulumi infrastructure-as-code
  pkg/                  Shared packages (database, errors, HTTP)
api/                    Kubernetes CRD types (v1alpha1)
helm/
  orchestrator/         Helm chart for the orchestrator
  operator/             Helm chart for the userswarm operator
  values/               Default values for upstream charts
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

Runs Helm upgrade against the live cluster.

```sh
crawbl app deploy orchestrator --tag <tag>
crawbl app deploy operator --tag <tag>
```

## Infrastructure

Pulumi manages three layers:

- **Cluster** - DigitalOcean Kubernetes (DOKS), VPC, container registry
- **Platform** - Namespaces, Vault, PostgreSQL, Redis, cert-manager, Envoy Gateway, operators
- **Edge** - Cloudflare DNS, Gateway API, TLS certificates

Current environment: `dev` in DigitalOcean `fra1`.

Code lives in `internal/infra/`. Configuration in `Pulumi.yaml` and `Pulumi.dev.yaml`.
