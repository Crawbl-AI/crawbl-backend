-- Restore the original partial index that only covers state = 'raw'.
DROP INDEX IF EXISTS idx_drawers_state;
CREATE INDEX IF NOT EXISTS idx_drawers_state
    ON memory_drawers (state)
    WHERE state = 'raw';
