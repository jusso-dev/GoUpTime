-- Reverse of 004_multitenancy.up.sql. Drops organization_id columns and the
-- new tables; the seeded default organization disappears with the table.

ALTER TABLE api_keys
  DROP CONSTRAINT IF EXISTS api_keys_organization_id_fkey,
  DROP COLUMN IF EXISTS organization_id;

ALTER TABLE notification_channels
  DROP CONSTRAINT IF EXISTS notification_channels_organization_id_fkey,
  DROP COLUMN IF EXISTS organization_id;

ALTER TABLE check_results
  DROP CONSTRAINT IF EXISTS check_results_organization_id_fkey,
  DROP COLUMN IF EXISTS organization_id;

ALTER TABLE incidents
  DROP COLUMN IF EXISTS acknowledged_by_user_id,
  DROP COLUMN IF EXISTS acknowledged_at,
  DROP CONSTRAINT IF EXISTS incidents_organization_id_fkey,
  DROP COLUMN IF EXISTS organization_id;

ALTER TABLE monitors
  DROP CONSTRAINT IF EXISTS monitors_organization_id_fkey,
  DROP COLUMN IF EXISTS organization_id;

DROP TABLE IF EXISTS webhook_events;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
