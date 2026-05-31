-- 008_status_pages.up.sql
--
-- Public status pages. Each org can have N status pages, each page
-- groups monitors into "components", and each page may be reached via
-- a hosted slug (/s/:slug) or a custom domain (CNAME → reverse proxy).

CREATE TABLE IF NOT EXISTS status_pages (
  id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  slug                    text NOT NULL UNIQUE,
  name                    text NOT NULL,
  description             text NOT NULL DEFAULT '',
  custom_domain           text UNIQUE,
  custom_domain_verified  boolean NOT NULL DEFAULT false,
  theme                   jsonb NOT NULL DEFAULT '{}'::jsonb,
  published               boolean NOT NULL DEFAULT true,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_status_pages_org ON status_pages(organization_id);

CREATE TABLE IF NOT EXISTS status_page_components (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  status_page_id  uuid NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
  name            text NOT NULL,
  description     text NOT NULL DEFAULT '',
  position        integer NOT NULL DEFAULT 0,
  monitor_ids     uuid[] NOT NULL DEFAULT ARRAY[]::uuid[],
  group_name      text NOT NULL DEFAULT '',
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_status_page_components_page
  ON status_page_components(status_page_id, position);

-- Email subscribers (double opt-in). token_hash is the sha256 of the
-- one-shot unsubscribe / confirmation token.
CREATE TABLE IF NOT EXISTS status_page_subscriptions (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  status_page_id  uuid NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
  email           text NOT NULL,
  token_hash      text NOT NULL UNIQUE,
  confirmed       boolean NOT NULL DEFAULT false,
  unsubscribed_at timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_status_page_subscriptions_page
  ON status_page_subscriptions(status_page_id);

-- Operator-authored incident posts surfaced on the status page timeline.
CREATE TABLE IF NOT EXISTS status_page_incident_posts (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  status_page_id  uuid NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
  incident_id     uuid REFERENCES incidents(id) ON DELETE SET NULL,
  body            text NOT NULL,
  posted_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_status_page_incident_posts_page
  ON status_page_incident_posts(status_page_id, posted_at DESC);
