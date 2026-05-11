INSERT INTO monitors (id, name, type, target, method, expected_status, timeout_seconds, interval_seconds, failure_threshold, enabled)
VALUES
  ('00000000-0000-0000-0000-000000000101', 'Example Website', 'http', 'https://example.com', 'GET', 200, 10, 60, 3, true),
  ('00000000-0000-0000-0000-000000000102', 'Cloudflare DNS', 'dns', 'cloudflare.com', 'GET', 200, 10, 120, 3, true)
ON CONFLICT (id) DO NOTHING;

