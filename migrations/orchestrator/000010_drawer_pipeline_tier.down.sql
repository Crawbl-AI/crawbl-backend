-- Roll back the Phase 1 columns + enrichment index added in 000010.

DROP INDEX IF EXISTS idx_drawers_enrich;

ALTER TABLE memory_drawers
    DROP COLUMN IF EXISTS triple_count,
    DROP COLUMN IF EXISTS entity_count,
    DROP COLUMN IF EXISTS pipeline_tier;
