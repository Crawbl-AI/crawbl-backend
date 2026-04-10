-- No-op down for the no-op up. See 000008_ivfflat_index.up.sql for context:
-- vector index creation is deferred because pgvector's HNSW and IVFFlat paths
-- both SIGILL on Hetzner nodes that lack AVX2. Keep DROP IF EXISTS (no
-- CONCURRENTLY — golang-migrate wraps each migration in a transaction and
-- CONCURRENTLY is illegal inside a transaction block) so that if a future
-- session partially creates the index, rolling back still cleans it up.

DROP INDEX IF EXISTS orchestrator.idx_drawers_embedding_ivfflat;
