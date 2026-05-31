-- 009_maintenance.up.sql
--
-- Maintenance windows. The scheduler consults these before enqueueing
-- a check; an active window for the monitor's tag set skips the check
-- entirely so we don't pollute check_results with expected failures.

CREATE TABLE IF NOT EXISTS maintenance_windows (
  id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name                  text NOT NULL,
  description           text NOT NULL DEFAULT '',
  starts_at             timestamptz NOT NULL,
  ends_at               timestamptz NOT NULL,
  recurrence_rrule      text NOT NULL DEFAULT '',
  status_page_id        uuid REFERENCES status_pages(id) ON DELETE SET NULL,
  created_by_user_id    uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CHECK (ends_at > starts_at)
);
CREATE INDEX IF NOT EXISTS idx_maintenance_windows_org_window
  ON maintenance_windows(organization_id, starts_at, ends_at);

CREATE TABLE IF NOT EXISTS maintenance_window_monitors (
  maintenance_window_id uuid NOT NULL REFERENCES maintenance_windows(id) ON DELETE CASCADE,
  monitor_id            uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  PRIMARY KEY (maintenance_window_id, monitor_id)
);
CREATE INDEX IF NOT EXISTS idx_maintenance_window_monitors_monitor
  ON maintenance_window_monitors(monitor_id);
