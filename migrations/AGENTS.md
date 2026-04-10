# Database Migrations

## Purpose

SQL migrations for the Postgres orchestrator schema and the ClickHouse analytics schema.
Postgres migrations are embedded in the binary and applied automatically at orchestrator startup via golang-migrate. ClickHouse migrations are applied manually or by the usage-writer on first startup.

## Layout

- `orchestrator/` — Postgres schema for the orchestrator. Embedded and applied on startup.
- `clickhouse/` — ClickHouse schema for analytics and LLM usage events.
- `orchestrator/seed/` — Seed data scripts run separately from schema migrations.

## Conventions

**Filename format**

- Orchestrator: `NNNNNN_description.up.sql` / `NNNNNN_description.down.sql` (zero-padded 6 digits, e.g. `000009_users_email_unique.up.sql`)
- ClickHouse: `NNN_description.sql` (zero-padded 3 digits, e.g. `001_llm_usage.sql`). Each sequence is independent — orchestrator is at `000009`, ClickHouse is at `001`.

**Next migration number**

The current highest orchestrator migration is `000009`. The next must be `000010`. Always check the directory listing before picking a number — never reuse or skip.

**One logical change per migration.** A migration that adds a column and creates an index is fine. A migration that restructures two unrelated tables is not.

**Both `.up` and `.down` files are required** for every orchestrator migration. ClickHouse DDL uses a single `.sql` file (no paired down).

**Dev-only pragmatism.** There is no production database yet. Do not pad migrations with defensive `IF EXISTS` dedupe or cleanup logic for states that cannot actually occur on a clean dev cluster.

**Embedding.** Orchestrator migrations are picked up automatically via `go:embed` in the orchestrator package. Adding a new `.sql` file to `orchestrator/` is sufficient — no registration step needed.

**Style.** Use `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`, and `CREATE UNIQUE INDEX IF NOT EXISTS`. Prefer `TIMESTAMPTZ` for timestamps. Foreign keys use `ON DELETE CASCADE` where the child row is meaningless without the parent.

## Gotchas

- We dont have production database, so no need to make complex migrations
- Always write both `.up` **and** `.down` for orchestrator migrations. A missing `.down` will block golang-migrate from rolling back.
- Test the `.down` migration manually on a local dev DB before committing — a broken rollback is worse than no rollback.
- Do not squash or consolidate early migrations unless the dev cluster is also wiped and re-provisioned at the same time.
- ClickHouse has its own numbering sequence, independent of the orchestrator sequence. `001` in ClickHouse does not correspond to `000001` in orchestrator.
- The running orchestrator binary applies Postgres migrations automatically on startup. ClickHouse migrations require manual application or the usage-writer service to run them.
- Seed scripts in `orchestrator/seed/` are not managed by golang-migrate — run them explicitly when needed.

## Key Files

- `orchestrator/000001_create_all.up.sql` — consolidated initial schema (all core tables)
- `orchestrator/000009_users_email_unique.up.sql` — current highest migration; use `000010` for the next one
- `clickhouse/001_llm_usage.sql` — ClickHouse schema for LLM usage events and daily rollup materialized view
