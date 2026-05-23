-- 005_new_check_types.up.sql
--
-- Widens the supported monitor types and adds tables for the per-type
-- configuration: heartbeat tokens, multi-step request scripts, browser
-- (Playwright) scripts. check_results gains a domain_expires_at column so
-- domain-expiry checks can surface "X days remaining" without a separate
-- detail table.

ALTER TABLE monitors
  DROP CONSTRAINT IF EXISTS monitors_type_check;

ALTER TABLE monitors
  ADD CONSTRAINT monitors_type_check
    CHECK (type IN ('http','tcp','dns','tls','keyword','heartbeat','icmp','browser','domain','multistep'));

-- Heartbeat / push monitors. Each monitor has exactly one heartbeat row.
-- The plaintext token is shown once at creation; only the hash is stored.
CREATE TABLE IF NOT EXISTS heartbeats (
  monitor_id                uuid PRIMARY KEY REFERENCES monitors(id) ON DELETE CASCADE,
  token_hash                text NOT NULL UNIQUE,
  expected_interval_seconds integer NOT NULL CHECK (expected_interval_seconds > 0),
  grace_seconds             integer NOT NULL DEFAULT 30 CHECK (grace_seconds >= 0),
  last_ping_at              timestamptz,
  last_ping_source_ip       text NOT NULL DEFAULT '',
  last_ping_user_agent      text NOT NULL DEFAULT '',
  created_at                timestamptz NOT NULL DEFAULT now(),
  updated_at                timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_heartbeats_last_ping_at ON heartbeats(last_ping_at);

CREATE TABLE IF NOT EXISTS multistep_scripts (
  monitor_id uuid PRIMARY KEY REFERENCES monitors(id) ON DELETE CASCADE,
  steps      jsonb NOT NULL DEFAULT '{"steps":[]}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS browser_scripts (
  monitor_id uuid PRIMARY KEY REFERENCES monitors(id) ON DELETE CASCADE,
  source     text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE check_results
  ADD COLUMN IF NOT EXISTS domain_expires_at timestamptz;
