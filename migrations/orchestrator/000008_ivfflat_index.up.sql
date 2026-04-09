-- IVFFlat vector index for memory_drawers.
-- Using IVFFlat instead of HNSW because Hetzner CPUs lack AVX2 support
-- which causes SIGILL crashes with HNSW. IVFFlat may work without AVX2.
-- If this also crashes, fall back to sequential scan (acceptable at <10K drawers).
CREATE INDEX CONCURRENTLY idx_drawers_embedding_ivfflat
ON orchestrator.memory_drawers
USING ivfflat (embedding public.vector_cosine_ops)
WITH (lists = 100);
