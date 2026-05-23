DROP INDEX IF EXISTS idx_worker_heartbeats_region;
DROP INDEX IF EXISTS idx_check_results_monitor_region_checked;

ALTER TABLE check_results DROP COLUMN IF EXISTS region;
ALTER TABLE monitors
  DROP COLUMN IF EXISTS region_confirmation_threshold,
  DROP COLUMN IF EXISTS regions;
ALTER TABLE worker_heartbeats DROP COLUMN IF EXISTS region;
