-- 007_notifications.up.sql
--
-- Widens the notification system from webhook-only to a dispatcher with
-- typed providers (webhook, slack, email, pagerduty, push), backed by a
-- durable outbox so a Redis/Slack hiccup doesn't drop incident alerts.

-- notification_channels gains a config jsonb so each provider can carry
-- its own shape (webhook URL, Slack mention list, PagerDuty routing key,
-- etc.) without growing the table schema for every new integration.
ALTER TABLE notification_channels
  ADD COLUMN IF NOT EXISTS config jsonb NOT NULL DEFAULT '{}'::jsonb;

-- Backfill the existing url column into config.webhook_url so existing
-- channels remain functional via the new Provider interface.
UPDATE notification_channels
  SET config = jsonb_build_object('webhook_url', url)
  WHERE config = '{}'::jsonb AND url <> '';

-- Allow the URL column to be empty for non-webhook channels; the
-- application layer is now the source of truth for required config.
ALTER TABLE notification_channels
  ALTER COLUMN url DROP NOT NULL;

ALTER TABLE notification_channels
  DROP CONSTRAINT IF EXISTS notification_channels_type_check;
ALTER TABLE notification_channels
  ADD CONSTRAINT notification_channels_type_check
    CHECK (type IN ('webhook','slack','email','pagerduty','push'));

-- Durable outbox: one row per (channel × incident event). Written inside
-- the same transaction as the incident state change. A background
-- poller drains rows with FOR UPDATE SKIP LOCKED and retries with
-- exponential backoff.
CREATE TABLE IF NOT EXISTS notification_outbox (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  channel_id      uuid REFERENCES notification_channels(id) ON DELETE SET NULL,
  incident_id     uuid REFERENCES incidents(id) ON DELETE SET NULL,
  event_type      text NOT NULL,
  payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
  attempts        integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  status          text NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','delivered','failed')),
  last_error      text NOT NULL DEFAULT '',
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notification_outbox_pending
  ON notification_outbox(status, next_attempt_at)
  WHERE status = 'pending';

-- Mobile push device registry. expo_token is unique so the same physical
-- device re-registering doesn't generate duplicate rows.
CREATE TABLE IF NOT EXISTS push_devices (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform        text NOT NULL CHECK (platform IN ('ios','android')),
  expo_token      text NOT NULL UNIQUE,
  app_version     text NOT NULL DEFAULT '',
  last_seen_at    timestamptz NOT NULL DEFAULT now(),
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_push_devices_organization_user
  ON push_devices(organization_id, user_id);
