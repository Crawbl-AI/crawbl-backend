-- Phase 2 of the MemPalace cold-pipeline cost reduction (Crawbl-AI/crawbl-backend#155).
--
-- memory_type_centroids stores one 1536-dim prototype vector per
-- memory type. The autoingest worker uses them to classify chunks in
-- the medium-confidence band ([HeuristicConfidenceLow, High)) without
-- an LLM call: pick the nearest centroid and, if cosine similarity
-- exceeds MemoryCentroidThreshold, persist the drawer with
-- pipeline_tier='centroid', state='processed'.
--
-- Centroids are rebuilt weekly by the memory_centroid_recompute River
-- worker (internal/memory/background/centroid_recompute.go) from
-- pipeline_tier='llm' drawers only — training never pulls its own
-- predictions, preventing a feedback loop.
--
-- Rows with sample_count < memory.MemoryCentroidMinSamples (50) are
-- treated as unreliable and ignored by NearestType so cold-start
-- workspaces do not get dominated by a low-cohort centroid.

CREATE TABLE IF NOT EXISTS memory_type_centroids (
    memory_type  TEXT         NOT NULL PRIMARY KEY,
    centroid     vector(1536) NOT NULL,
    sample_count INTEGER      NOT NULL,
    computed_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    source_hash  TEXT         NOT NULL
);
