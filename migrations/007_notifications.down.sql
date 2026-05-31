DROP INDEX IF EXISTS idx_push_devices_organization_user;
DROP TABLE IF EXISTS push_devices;

DROP INDEX IF EXISTS idx_notification_outbox_pending;
DROP TABLE IF EXISTS notification_outbox;

ALTER TABLE notification_channels
  DROP CONSTRAINT IF EXISTS notification_channels_type_check;
ALTER TABLE notification_channels
  ADD CONSTRAINT notification_channels_type_check CHECK (type IN ('webhook'));

ALTER TABLE notification_channels
  ALTER COLUMN url SET NOT NULL,
  DROP COLUMN IF EXISTS config;
