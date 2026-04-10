-- Phase 1 of the MemPalace cold-pipeline cost reduction (Crawbl-AI/crawbl-backend#155).
--
-- Adds three columns + one partial index that let the in-process
-- autoingest worker (internal/memory/autoingest) skip the LLM path for
-- high-confidence drawers while still scheduling asynchronous entity
-- linking via the memory_enrich River worker.
--
--   * pipeline_tier : which arm of the cold pipeline labelled this row
--                     — 'heuristic' | 'centroid' | 'llm'. Default 'llm'
--                     so every existing row stays in the LLM path.
--   * entity_count  : number of KG entities the enrich worker wired up
--                     for this drawer. Starts at 0.
--   * triple_count  : number of KG triples the enrich worker wired up
--                     for this drawer. Starts at 0.
--
-- The partial index idx_drawers_enrich matches the memory_enrich sweep
-- query in internal/memory/repo/drawerrepo/postgres.go.ListEnrichCandidates
-- exactly, so lookups stay sargable even as memory_drawers grows.
--
-- NOTE: This file intentionally uses plain CREATE INDEX rather than
-- CREATE INDEX CONCURRENTLY, matching the codebase convention set in
-- 000008_ivfflat_index.up.sql — golang-migrate v4 wraps each migration
-- in a transaction and CONCURRENTLY is forbidden inside one. The
-- partial-index predicate is narrow (processed-tier non-llm drawers
-- with zero entities and importance >= 3) and memory_drawers is bounded
-- at 10K rows per workspace, so the table lock acquired by a
-- non-concurrent CREATE INDEX is short-lived in practice.
--
-- The ALTER TABLE ... ADD COLUMN ... DEFAULT <constant> NOT NULL below
-- is also safe on a large table: Postgres 11+ stores the default in
-- pg_attribute metadata and materialises it on read, so the ALTER is
-- an O(1) metadata-only operation that holds ACCESS EXCLUSIVE for
-- milliseconds. No full-table rewrite, no row-by-row backfill. See
-- https://www.postgresql.org/docs/11/release-11.html "fast ALTER TABLE
-- ADD COLUMN with non-null default" for the fast-path semantics.

ALTER TABLE memory_drawers
    ADD COLUMN IF NOT EXISTS pipeline_tier TEXT NOT NULL DEFAULT 'llm',
    ADD COLUMN IF NOT EXISTS entity_count  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS triple_count  INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_drawers_enrich
    ON memory_drawers (workspace_id, created_at)
    WHERE state = 'processed'
      AND pipeline_tier <> 'llm'
      AND entity_count = 0
      AND importance >= 3.0;
