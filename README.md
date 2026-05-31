# UpTime

A self-hosted uptime and synthetic monitoring platform built with Go, Gin, PostgreSQL, Redis, worker pools, Prometheus metrics, and webhook notifications.

UpTime started as a small uptime-check API. This rebuild turns the same idea into a backend-first portfolio project with real persistence, scheduler/worker separation, incident handling, API-key auth, metrics, and Docker Compose.

## Features

- Gin REST API with `GET /health`, legacy `GET /health-check`, and legacy `POST /ping-endpoint`
- HTTP, API assertion, keyword, TCP, UDP, DNS, TLS, domain-expiry, ping, and heartbeat checks
- HTTP timing details through `httptrace`: DNS, TCP connect, TLS handshake, first byte, total duration
- PostgreSQL tables for monitors, check results, incidents, notification channels, API keys, and audit logs
- Worker process with goroutines, channels, context cancellation, duplicate-check avoidance, and graceful shutdown
- Incident lifecycle with acknowledgement, investigation, severity, impact, timeline evidence, comments, post-mortems, and action items
- Alert-quality controls for regional quorum, dependency suppression, grouping, flapping cooldown, and maintenance suppression
- Webhook, Slack, push, SMTP, chat, Twilio SMS/voice, and AWS SNS SMS notification channels for incident events
- Monitor tags, services, maintenance windows, public status pages, and uptime reports
- Status page subscribers, public announcements, and automatic incident updates
- Remote/private agents that poll assigned checks and submit regional results
- On-call schedules, overrides, escalation policies, runbooks, and browser synthetic artifacts
- API key authentication with hashed stored keys and a bootstrap admin key
- Prometheus metrics for API requests, checks, incidents, and worker jobs
- Docker Compose stack with API, worker, Postgres, Redis, Prometheus, and Grafana

## Screenshots

These screenshots were captured from the live Docker Compose stack.

![Health endpoint showing PostgreSQL and Redis status](docs/screenshots/api-health.png)

![Authenticated monitor API returning monitors updated by the worker](docs/screenshots/api-monitors.png)

![Legacy ping endpoint returning HTTP tracing timings](docs/screenshots/ping-endpoint.png)

![Prometheus scraping the API and worker metrics endpoints](docs/screenshots/prometheus-targets.png)

![Grafana dashboard provisioned for UpTime metrics](docs/screenshots/grafana-dashboard.png)

## Architecture

```mermaid
flowchart LR
  User[User / API Client] --> API[Go Gin API]
  API --> Postgres[(PostgreSQL)]
  API --> Redis[(Redis)]
  Worker[Go Worker Pool] --> Postgres
  Worker --> Redis
  Worker --> Targets[Websites / TCP / DNS / TLS Targets]
  Worker --> Notify[Webhook Notifications]
  Prometheus --> API
  Prometheus --> Worker
  Grafana --> Prometheus
```

## Tech Stack

- Go 1.22+
- Gin
- PostgreSQL via GORM
- Redis
- Prometheus client library
- Structured logging with `slog`
- Docker Compose

## Local Setup

Run the full stack:

```bash
make docker-up
```

API: `http://localhost:8008`

Prometheus: `http://localhost:9090`

Grafana: `http://localhost:3000` with `admin` / `admin`

Run without Docker for Go processes:

```bash
export DATABASE_URL='postgres://uptime:uptime@localhost:5432/uptime?sslmode=disable'
export REDIS_URL='redis://localhost:6379/0'
export UPTIME_BOOTSTRAP_API_KEY='dev_admin_key'

make migrate # runs GORM-managed schema migration
go run ./cmd/api
go run ./cmd/worker
```

## Environment

| Variable | Default | Description |
| --- | --- | --- |
| `APP_ENV` | `development` | Runtime environment (`production` enforces stricter defaults) |
| `APP_PORT` | `8008` | API port |
| `METRICS_PORT` | `8009` | Worker Prometheus metrics port |
| `DATABASE_URL` | local Postgres | PostgreSQL connection string (`postgres://` or `postgresql://`) |
| `REDIS_URL` | local Redis | Redis connection string (`redis://` or `rediss://`) |
| `UPTIME_BOOTSTRAP_API_KEY` | `dev_admin_key` (dev only) | Bootstrap bearer token. Required in production; must be ≥ 16 chars |
| `ALLOW_PRIVATE_TARGETS` | `false` | Allow localhost/private targets for checks/webhooks (forbidden in production) |
| `CHECK_WORKER_COUNT` | `10` | Worker goroutine count (1–1024) |
| `DEFAULT_CHECK_TIMEOUT_SECONDS` | `10` | Default check timeout (1–300) |
| `SCHEDULER_TICK_SECONDS` | `5` | How often the scheduler polls for due monitors (1–60) |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `TLS_EXPIRY_WARN_DAYS` | `14` | Days before expiry that TLS checks report `degraded` |
| `WEBHOOK_SIGNING_SECRET` | _empty_ | If set, webhook bodies are HMAC-SHA256 signed in `X-UpTime-Signature` |
| `WEBHOOK_TIMEOUT_SECONDS` | `10` | Per-attempt webhook timeout |
| `WEBHOOK_MAX_RETRIES` | `3` | Additional webhook attempts after the first failure (0–10) |
| `SHUTDOWN_TIMEOUT_SECONDS` | `15` | Graceful shutdown deadline |
| `API_READ_HEADER_TIMEOUT_SECONDS` | `5` | API `http.Server` read header timeout |
| `API_WRITE_TIMEOUT_SECONDS` | `30` | API `http.Server` write timeout |
| `MAX_REQUEST_BODY_BYTES` | `1048576` | Maximum accepted request body size in bytes |

## API Examples

Health:

```bash
curl http://localhost:8008/health
```

Manual legacy check:

```bash
curl -X POST http://localhost:8008/ping-endpoint \
  -H "Content-Type: application/json" \
  -d '{"endpoint":"https://example.com"}'
```

Create a monitor:

```bash
curl -X POST http://localhost:8008/api/v1/monitors \
  -H "Authorization: Bearer dev_admin_key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Example Website",
    "type": "http",
    "target": "https://example.com",
    "method": "GET",
    "expectedStatus": 200,
    "timeoutSeconds": 10,
    "intervalSeconds": 60,
    "failureThreshold": 3,
    "enabled": true
  }'
```

Run a monitor now:

```bash
curl -X POST http://localhost:8008/api/v1/monitors/00000000-0000-0000-0000-000000000101/check-now \
  -H "Authorization: Bearer dev_admin_key"
```

Create an API key:

```bash
curl -X POST http://localhost:8008/api/v1/api-keys \
  -H "Authorization: Bearer dev_admin_key" \
  -H "Content-Type: application/json" \
  -d '{"name":"local dev"}'
```

## Scheduler And Worker

`cmd/worker` periodically loads enabled monitors from PostgreSQL. It schedules checks by `intervalSeconds`, skips monitors already in flight, and fans jobs out to a fixed goroutine pool. Each job uses context timeouts, stores a check result, updates monitor status, and applies incident rules.

Redis is part of the local stack and health reporting. The current worker uses local in-process scheduling; Redis-backed distributed locks/queues are a natural next step for multiple worker replicas.

## Check Types

- `http`: validates URL, blocks private targets by default, supports `GET`/`HEAD`, expected status, redirects disabled, body snippets, and timing breakdowns.
- `api`: HTTP check with methods, headers, body, bearer/basic auth, and JSON assertion config.
- `keyword`: HTTP check plus expected keyword matching.
- `tcp`: checks `host:port` reachability with `net.Dialer`.
- `udp`: sends a datagram payload and can validate an expected response snippet.
- `dns`: resolves a hostname with Go's resolver.
- `tls`: connects to a TLS endpoint and marks certificates near expiry as degraded.
- `domain`: checks domain expiration through RDAP.
- `ping`: TCP reachability ping for environments where raw ICMP is not available.
- `heartbeat`: records inbound pings and opens incidents when check-ins are late or missing.
- `browser`: submits a Playwright transaction job to the optional browser worker sidecar and records screenshots, console errors, network failures, and artifact references.

## Incident Lifecycle

Checks are stored in `check_results`. A monitor opens an incident only after `failureThreshold` consecutive failures, any configured regional quorum is met, no parent dependency is already down, and the monitor is not flapping. A succeeding check resolves the active incident.

Incidents support `open`, `acknowledged`, `investigating`, `identified`, `monitoring`, and `resolved` states, plus severity (`info`, `warning`, `minor`, `major`, `critical`) and impact (`none`, `degraded`, `partial_outage`, `full_outage`). Timeline events capture state changes, check evidence, comments, escalation decisions, and recovery context with sensitive keys redacted.

Post-mortems can be attached to resolved incidents and exported as Markdown. Action items track owner, due date, and completion state.

## Communication And Response

Status pages can collect confirmed subscribers, publish announcements, and auto-publish incident updates for affected components. Subscriber confirmation and unsubscribe links use hashed one-time tokens.

Remote/private agents are provisioned with scoped tokens. Agents call `/api/v1/agent/jobs` for assigned checks, submit results to `/api/v1/agent/results`, and heartbeat through `/api/v1/agent/heartbeat`.

On-call schedules rotate participants from a timezone-aware handoff time, support temporary overrides, and expose current/upcoming shift APIs. Escalation policies can route by monitor, service, tag, severity, and impact, and incident timelines record the selected policy.

## Browser Transactions

Browser monitors use a Redis job contract so the Go worker remains lightweight. Run the optional Playwright sidecar with:

```bash
docker compose --profile browser up browser-worker
```

The sidecar executes saved scripts in an isolated Playwright context, captures failure screenshots, console errors, network failures, and emits artifact metadata. Artifact records include retention timestamps and authenticated download endpoints.

## Observability

`GET /metrics` exposes API metrics. The worker exposes metrics on `:8009/metrics`.

Prometheus scrapes both services, and Grafana is provisioned with a starter UpTime dashboard.

### Worker dashboard

A minimal job UI is served by the API at `GET /workers`. It polls
`GET /api/v1/workers/status` every 2 seconds and shows, per worker instance:
host, started/last-seen, active and queued jobs, in-flight monitor IDs, and
the most recent 50 check results. Workers write their state into
`worker_heartbeats` every 5 seconds, so the same view also reflects crashed
or restarting instances (rows older than ~20 seconds are flagged `stale`).

The HTML page is unauthenticated; it prompts for an API key client-side
and uses it as a Bearer token for the protected status XHR.

## Security

- `/api/v1/*` endpoints require `Authorization: Bearer <key>` or `X-API-Key`
- Raw generated API keys are shown once; only SHA-256 hashes are stored
- URLs and webhooks block localhost/private/link-local targets unless `ALLOW_PRIVATE_TARGETS=true`
- Checks use context timeouts and bounded response snippets
- Logs avoid raw API keys and webhook payload secrets

## Testing

```bash
make test
make check
```

The test suite covers HTTP checker success, timeout, expected-status mismatch, SSRF blocking, TCP success/failure, DNS success/failure, TLS expiry classification, API key hashing, incident open/resolve rules, regional quorum, dependency suppression, flapping suppression, SMS payload construction, and on-call rotation math.

## Roadmap

- API/UI polish for the growing response workflows
- Terraform, Helm, and CLI automation
- OIDC SSO, RBAC hardening, encrypted secrets, and audit UI
- OpenTelemetry export and long-term artifact storage
