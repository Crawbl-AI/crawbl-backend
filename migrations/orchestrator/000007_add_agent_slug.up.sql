-- Add slug column: the ZeroClaw routing identifier (matches [agents.<slug>] in config.toml).
ALTER TABLE agents ADD COLUMN IF NOT EXISTS slug TEXT NOT NULL DEFAULT '';

-- Backfill slug from existing role values (which were used as identifiers).
UPDATE agents SET slug = role WHERE slug = '';

-- Change role to semantic values: existing "researcher" and "writer" become "sub-agent".
UPDATE agents SET role = 'sub-agent' WHERE role IN ('researcher', 'writer', 'wally');

-- Drop old unique index on (workspace_id, role) and create new one on (workspace_id, slug).
DROP INDEX IF EXISTS idx_agents_workspace_role;
CREATE UNIQUE INDEX idx_agents_workspace_slug ON agents(workspace_id, slug);
