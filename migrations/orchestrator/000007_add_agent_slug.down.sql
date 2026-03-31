DROP INDEX IF EXISTS idx_agents_workspace_slug;
CREATE UNIQUE INDEX idx_agents_workspace_role ON agents(workspace_id, role);
UPDATE agents SET role = slug WHERE role = 'sub-agent';
ALTER TABLE agents DROP COLUMN IF EXISTS slug;
