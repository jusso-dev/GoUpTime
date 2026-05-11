CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS monitors (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  type text NOT NULL CHECK (type IN ('http','tcp','dns','tls','keyword')),
  target text NOT NULL,
  method text NOT NULL DEFAULT 'GET',
  expected_status integer NOT NULL DEFAULT 200,
  expected_keyword text NOT NULL DEFAULT '',
  timeout_seconds integer NOT NULL DEFAULT 10 CHECK (timeout_seconds > 0),
  interval_seconds integer NOT NULL DEFAULT 60 CHECK (interval_seconds > 0),
  failure_threshold integer NOT NULL DEFAULT 3 CHECK (failure_threshold > 0),
  enabled boolean NOT NULL DEFAULT true,
  status text NOT NULL DEFAULT 'degraded' CHECK (status IN ('up','down','degraded')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS check_results (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  monitor_id uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  status text NOT NULL CHECK (status IN ('up','down','degraded')),
  success boolean NOT NULL,
  response_time_ms bigint NOT NULL DEFAULT 0,
  status_code integer NOT NULL DEFAULT 0,
  error text NOT NULL DEFAULT '',
  checked_at timestamptz NOT NULL DEFAULT now(),
  dns_ms bigint NOT NULL DEFAULT 0,
  tcp_connect_ms bigint NOT NULL DEFAULT 0,
  tls_handshake_ms bigint NOT NULL DEFAULT 0,
  time_to_first_byte_ms bigint NOT NULL DEFAULT 0,
  total_ms bigint NOT NULL DEFAULT 0,
  response_snippet text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS incidents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  monitor_id uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  status text NOT NULL CHECK (status IN ('open','resolved')),
  started_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz,
  reason text NOT NULL,
  last_error text NOT NULL DEFAULT '',
  consecutive_failures integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notification_channels (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  type text NOT NULL CHECK (type IN ('webhook')),
  url text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notification_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  channel_id uuid REFERENCES notification_channels(id) ON DELETE SET NULL,
  incident_id uuid REFERENCES incidents(id) ON DELETE SET NULL,
  event_type text NOT NULL,
  success boolean NOT NULL,
  status_code integer NOT NULL DEFAULT 0,
  error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS api_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  key_hash text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  revoked_at timestamptz
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor text NOT NULL DEFAULT '',
  action text NOT NULL,
  target_type text NOT NULL DEFAULT '',
  target_id text NOT NULL DEFAULT '',
  metadata jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_check_results_monitor_id ON check_results(monitor_id);
CREATE INDEX IF NOT EXISTS idx_check_results_checked_at ON check_results(checked_at DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_monitor_id ON incidents(monitor_id);
CREATE INDEX IF NOT EXISTS idx_incidents_status ON incidents(status);
CREATE INDEX IF NOT EXISTS idx_monitors_enabled ON monitors(enabled);
CREATE INDEX IF NOT EXISTS idx_monitors_status ON monitors(status);

