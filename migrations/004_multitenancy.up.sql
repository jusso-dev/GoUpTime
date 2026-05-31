-- 004_multitenancy.up.sql
--
-- Introduces Clerk-Organization multi-tenancy. After this migration runs,
-- every tenant-scoped row is owned by an organization, and the existing
-- bootstrap API key is reassigned to a default "personal" organization so
-- pre-migration deployments keep functioning.
--
-- Strategy: add organization_id columns as nullable, backfill to the default
-- org, then ALTER ... SET NOT NULL. The migrate runner wraps the whole file
-- in a single transaction, so partial failures roll back automatically.

CREATE TABLE IF NOT EXISTS organizations (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  clerk_org_id  text UNIQUE,
  name          text NOT NULL,
  slug          text NOT NULL UNIQUE,
  plan          text NOT NULL DEFAULT 'free',
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  clerk_user_id  text UNIQUE,
  email          text NOT NULL,
  name           text NOT NULL DEFAULT '',
  image_url      text NOT NULL DEFAULT '',
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS memberships (
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role            text NOT NULL CHECK (role IN ('owner','admin','member','viewer')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_memberships_user_id ON memberships(user_id);

-- Idempotent dedup of inbound webhook events from Clerk (delivered via Svix).
CREATE TABLE IF NOT EXISTS webhook_events (
  id          text PRIMARY KEY,
  source      text NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  payload     jsonb NOT NULL DEFAULT '{}'::jsonb
);

-- Seed the default organization with a deterministic UUID so the worker and
-- repository layer can reference it without first reading a row. This UUID
-- is also exposed via the BOOTSTRAP_ORG_ID env var; if operators set that
-- variable to a different value, this row remains in the table as a no-op.
INSERT INTO organizations (id, name, slug, plan)
VALUES (
  '00000000-0000-0000-0000-000000000001',
  'Default',
  'default',
  'free'
)
ON CONFLICT (id) DO NOTHING;

-- Add organization_id to every tenant-scoped table. Three steps per table:
-- 1. add column (nullable)
-- 2. backfill existing rows
-- 3. SET NOT NULL + FK + index

ALTER TABLE monitors
  ADD COLUMN IF NOT EXISTS organization_id uuid;
UPDATE monitors
  SET organization_id = '00000000-0000-0000-0000-000000000001'
  WHERE organization_id IS NULL;
ALTER TABLE monitors
  ALTER COLUMN organization_id SET NOT NULL,
  ADD CONSTRAINT monitors_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_monitors_organization_id ON monitors(organization_id);

ALTER TABLE incidents
  ADD COLUMN IF NOT EXISTS organization_id uuid;
UPDATE incidents
  SET organization_id = m.organization_id
  FROM monitors m
  WHERE incidents.monitor_id = m.id AND incidents.organization_id IS NULL;
ALTER TABLE incidents
  ALTER COLUMN organization_id SET NOT NULL,
  ADD CONSTRAINT incidents_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_incidents_organization_id ON incidents(organization_id);

-- Acknowledgement metadata. A soft-ack today (no escalation yet); when
-- on-call lands the same column gets reused.
ALTER TABLE incidents
  ADD COLUMN IF NOT EXISTS acknowledged_at timestamptz,
  ADD COLUMN IF NOT EXISTS acknowledged_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE check_results
  ADD COLUMN IF NOT EXISTS organization_id uuid;
UPDATE check_results
  SET organization_id = m.organization_id
  FROM monitors m
  WHERE check_results.monitor_id = m.id AND check_results.organization_id IS NULL;
ALTER TABLE check_results
  ALTER COLUMN organization_id SET NOT NULL,
  ADD CONSTRAINT check_results_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_check_results_organization_id
  ON check_results(organization_id, checked_at DESC);

ALTER TABLE notification_channels
  ADD COLUMN IF NOT EXISTS organization_id uuid;
UPDATE notification_channels
  SET organization_id = '00000000-0000-0000-0000-000000000001'
  WHERE organization_id IS NULL;
ALTER TABLE notification_channels
  ALTER COLUMN organization_id SET NOT NULL,
  ADD CONSTRAINT notification_channels_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_notification_channels_organization_id
  ON notification_channels(organization_id);

ALTER TABLE api_keys
  ADD COLUMN IF NOT EXISTS organization_id uuid;
UPDATE api_keys
  SET organization_id = '00000000-0000-0000-0000-000000000001'
  WHERE organization_id IS NULL;
ALTER TABLE api_keys
  ALTER COLUMN organization_id SET NOT NULL,
  ADD CONSTRAINT api_keys_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_api_keys_organization_id ON api_keys(organization_id);
