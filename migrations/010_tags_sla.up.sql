-- 010_tags_sla.up.sql
--
-- Tags + tag filtering on monitors. SLA reporting reads from existing
-- incidents/maintenance_windows tables so no schema change is needed
-- there — the migration only covers tag storage.

CREATE TABLE IF NOT EXISTS tags (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name            text NOT NULL,
  color           text NOT NULL DEFAULT '#888888',
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (organization_id, name)
);
CREATE INDEX IF NOT EXISTS idx_tags_organization_id ON tags(organization_id);

CREATE TABLE IF NOT EXISTS monitor_tags (
  monitor_id uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  tag_id     uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (monitor_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_monitor_tags_tag_id ON monitor_tags(tag_id);
