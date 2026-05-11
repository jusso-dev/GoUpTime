# UpTime

A self-hosted uptime and synthetic monitoring platform built with Go, Gin, PostgreSQL, Redis, worker pools, Prometheus metrics, and webhook notifications.

UpTime started as a small uptime-check API. This rebuild turns the same idea into a backend-first portfolio project with real persistence, scheduler/worker separation, incident handling, API-key auth, metrics, and Docker Compose.

## Features

- Gin REST API with `GET /health`, legacy `GET /health-check`, and legacy `POST /ping-endpoint`
- HTTP, keyword, TCP, DNS, and TLS checks
- HTTP timing details through `httptrace`: DNS, TCP connect, TLS handshake, first byte, total duration
- PostgreSQL tables for monitors, check results, incidents, notification channels, API keys, and audit logs
- Worker process with goroutines, channels, context cancellation, duplicate-check avoidance, and graceful shutdown
- Incident lifecycle: opens after `failureThreshold` consecutive failures and resolves on recovery
- Webhook notification channels for incident open/resolve events
- API key authentication with hashed stored keys and a bootstrap admin key
- Prometheus metrics for API requests, checks, incidents, and worker jobs
- Docker Compose stack with API, worker, Postgres, Redis, Prometheus, and Grafana

## Screenshots

These screenshots were captured from the live Docker Compose stack.

![Health endpoint showing PostgreSQL and Redis status](docs/screenshots/api-health.png)

![Authenticated monitor API returning seeded monitors updated by the worker](docs/screenshots/api-monitors.png)

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
- PostgreSQL via `pgx`
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

make migrate
go run ./cmd/api
go run ./cmd/worker
```

## Environment

| Variable | Default | Description |
| --- | --- | --- |
| `APP_ENV` | `development` | Runtime environment |
| `APP_PORT` | `8008` | API port |
| `DATABASE_URL` | local Postgres | PostgreSQL connection string |
| `REDIS_URL` | local Redis | Redis connection string |
| `UPTIME_BOOTSTRAP_API_KEY` | `dev_admin_key` | Bootstrap bearer token |
| `ALLOW_PRIVATE_TARGETS` | `false` | Allow localhost/private targets for checks/webhooks |
| `CHECK_WORKER_COUNT` | `10` | Worker goroutine count |
| `DEFAULT_CHECK_TIMEOUT_SECONDS` | `10` | Default check timeout |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |

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
- `keyword`: HTTP check plus expected keyword matching.
- `tcp`: checks `host:port` reachability with `net.Dialer`.
- `dns`: resolves a hostname with Go's resolver.
- `tls`: connects to a TLS endpoint and marks certificates near expiry as degraded.

## Incident Lifecycle

Checks are stored in `check_results`. A monitor opens an incident only after `failureThreshold` consecutive failures. A succeeding check resolves the open incident. Webhook notifications are sent on both transitions and attempts are recorded in `notification_events`.

## Observability

`GET /metrics` exposes API metrics. The worker exposes metrics on `:8009/metrics`.

Prometheus scrapes both services, and Grafana is provisioned with a starter UpTime dashboard.

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

The test suite covers HTTP checker success, timeout, expected-status mismatch, SSRF blocking, TCP success/failure, DNS success/failure, TLS expiry classification, API key hashing, and incident open/resolve rules.

## Roadmap

- Redis-backed distributed queue and locks
- Slack, Discord, and SMTP notification channels
- Public status pages
- Multi-tenant organisations
- Remote monitoring agents
- Optional React/Next.js dashboard
- Elasticsearch/Kibana analytics as a future optional integration
