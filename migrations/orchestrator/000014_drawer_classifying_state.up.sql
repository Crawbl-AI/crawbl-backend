-- Extend the partial index on memory_drawers.state to cover the new
-- 'classifying' transitional state so concurrent process workers can
-- efficiently skip rows already claimed by another pod.
DROP INDEX IF EXISTS idx_drawers_state;
CREATE INDEX IF NOT EXISTS idx_drawers_state
    ON memory_drawers (state)
    WHERE state IN ('raw', 'classifying');
