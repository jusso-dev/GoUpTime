CREATE TABLE IF NOT EXISTS worker_heartbeats (
  instance_id     text PRIMARY KEY,
  hostname        text NOT NULL DEFAULT '',
  version         text NOT NULL DEFAULT '',
  started_at      timestamptz NOT NULL,
  last_seen_at    timestamptz NOT NULL DEFAULT now(),
  worker_count    integer NOT NULL DEFAULT 0,
  active_jobs     integer NOT NULL DEFAULT 0,
  queue_depth     integer NOT NULL DEFAULT 0,
  queue_capacity  integer NOT NULL DEFAULT 0,
  jobs_completed  bigint NOT NULL DEFAULT 0,
  jobs_failed     bigint NOT NULL DEFAULT 0,
  in_flight       jsonb NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS idx_worker_heartbeats_last_seen
  ON worker_heartbeats(last_seen_at DESC);
