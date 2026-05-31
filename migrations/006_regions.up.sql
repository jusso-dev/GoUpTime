-- 006_regions.up.sql
--
-- Multi-region check execution. Workers self-identify with a region label
-- (WORKER_REGION env); each monitor declares which regions should run it
-- and the minimum number that must agree before an incident opens.

ALTER TABLE worker_heartbeats
  ADD COLUMN IF NOT EXISTS region text NOT NULL DEFAULT 'default';

ALTER TABLE monitors
  ADD COLUMN IF NOT EXISTS regions text[] NOT NULL DEFAULT ARRAY['default']::text[],
  ADD COLUMN IF NOT EXISTS region_confirmation_threshold integer NOT NULL DEFAULT 1
    CHECK (region_confirmation_threshold > 0);

ALTER TABLE check_results
  ADD COLUMN IF NOT EXISTS region text NOT NULL DEFAULT 'default';

CREATE INDEX IF NOT EXISTS idx_check_results_monitor_region_checked
  ON check_results(monitor_id, region, checked_at DESC);

CREATE INDEX IF NOT EXISTS idx_worker_heartbeats_region
  ON worker_heartbeats(region);
