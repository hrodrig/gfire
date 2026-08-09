# GFire — standalone background job service

<a id="readme-top"></a>

**🔥** _Language-agnostic job orchestration over HTTP — PostgreSQL, Redis, or ValKey_

[![Version](https://img.shields.io/badge/version-1.0.3-blue)](./VERSION)
[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)
[![Status](https://img.shields.io/badge/status-production-blue)](#)
[![gghstats clones](https://gghstats.hermesrodriguez.com/api/v1/badge/hrodrig/gfire?metric=clones)](https://gghstats.hermesrodriguez.com/hrodrig/gfire)

**Repo:** [github.com/hrodrig/gfire](https://github.com/hrodrig/gfire) · **Site:** [gfire.net](https://gfire.net) · **Spec:** [SPECIFICATIONS.md](SPECIFICATIONS.md) · **Roadmap:** [ROADMAP.md](ROADMAP.md) · **Security:** [SECURITY.md](SECURITY.md)

<p align="center">
  <img src="docs/gfire-hero.png" alt="GFire — standalone background job service" width="100%" />
</p>

GFire is a **headless background job service**: a standalone Go binary that runs a REST API and worker pool. Applications enqueue work over HTTP — they never import GFire as a library. Workers spawn external handler processes (`cmd`) against shared storage (**PostgreSQL**, **Redis**, or **ValKey**).

> **v1.0.3 — production-ready.** Run `gfire server`, enqueue via **curl**, inspect with CLI. Recurring cron validated (6-field); `last_run` tracked. Nested `GFIRE_*` env works for Compose. Ops console: [GFireUI](https://github.com/hrodrig/gfireui) + [BFF](https://github.com/hrodrig/gfireui-backend). See [ROADMAP.md](ROADMAP.md) for Band 12 / Pipelines / Adoption.

## Table of contents

- [Quick start](#quick-start)
- [Config reference](#config-reference)
- [Curl cookbook](#curl-cookbook)
- [Current status](#current-status)
- [What GFire is](#what-gfire-is)
- [Architecture](#architecture)
- [Requirements](#requirements)
- [Development](#development)
- [PostgreSQL setup](#postgresql-setup)
- [Redis / ValKey setup](#redis--valkey-setup)
- [Compare](#compare)
- [Project docs](#project-docs)
- [Get involved](#get-involved)
- [License](#license)

[↑ Back to top](#readme-top)

## Quick start

No dependencies. One binary, one command.

```sh
make build             # → bin/gfire
make server            # → build + run (creates gfire.yaml from example if missing)
```

Or manually:

```sh
go build -o bin/gfire ./cmd/gfire
cp gfire.example.yaml gfire.yaml
./bin/gfire server --config gfire.yaml
```

Default: in-memory backend, 4 workers, listening on `0.0.0.0:8080`. No external services needed.

**Health checks:**

```sh
curl -sS http://127.0.0.1:8080/healthz   # → {"status":"ok"}
curl -sS http://127.0.0.1:8080/readyz    # → storage reachable probe
```

[↑ Back to top](#readme-top)

## Config reference

All configuration lives in `gfire.yaml`. Copy `gfire.example.yaml` as a starting point — every value shown is the default.

```yaml
# Minimal config (in-memory, no auth):
storage:
  backend: memory

server:
  workers: 8          # goroutines pulling from queues
  queues:
    - critical
    - default
```

**Key sections:**

| Section | Purpose |
|---------|---------|
| `server` | Bind address, port, worker count, queue list, timeouts |
| `queue_limits` | Per-queue concurrency cap (`0` = unlimited) |
| `storage` | Backend selection: `memory`, `postgres`, or `redis` |
| `auth` | Optional Bearer token authentication |
| `handlers` | Name → executable path mapping for job subprocesses |
| `heartbeat` | Server heartbeat interval, stale timeout, orphan grace period |
| `scheduler` | Poll interval and batch size for delayed/scheduled jobs |
| `cleanup` | How often expired terminal-state jobs are purged |
| `logging` | Level (`debug`/`info`/`warn`/`error`) and format (`text`/`json`) |

**Environment overrides:** Every key can be set as `GFIRE_<PATH>` with dots replaced by underscores:

```sh
GFIRE_STORAGE_BACKEND=postgres GFIRE_AUTH_TOKEN=secret ./bin/gfire server
```

Full reference: [`gfire.example.yaml`](gfire.example.yaml) — every field documented with defaults.

[↑ Back to top](#readme-top)

## Curl cookbook

All examples assume the server is running on `localhost:8080`.

### Enqueue a job

```sh
curl -sS -X POST http://127.0.0.1:8080/v1/jobs/enqueue \
  -H 'Content-Type: application/json' \
  -d '{"name":"echo","args":{"hello":"world"},"queue":"default"}'
# → {"job_id":"...","status":"enqueued","queue":"default"}
```

With timeout and retry:

```sh
curl -sS -X POST http://127.0.0.1:8080/v1/jobs/enqueue \
  -H 'Content-Type: application/json' \
  -d '{"name":"echo","args":{"x":1},"timeout":"5m","retry_max":3}'
```

### Schedule a job (delayed execution)

```sh
curl -sS -X POST http://127.0.0.1:8080/v1/jobs/schedule \
  -H 'Content-Type: application/json' \
  -d '{"name":"echo","args":{"x":1},"enqueue_at":"2026-07-28T09:00:00Z"}'
# → {"job_id":"...","status":"scheduled","enqueue_at":"2026-07-28T09:00:00Z"}
```

### Get a job (state + history)

```sh
curl -sS http://127.0.0.1:8080/v1/jobs/JOB_ID
# → {"job":{...},"states":[...],"current_state":"Succeeded"}
```

### List jobs (filter by state)

```sh
curl -sS 'http://127.0.0.1:8080/v1/jobs?limit=20'
curl -sS 'http://127.0.0.1:8080/v1/jobs?state=Failed&limit=10'
```

### Cancel an in-flight job

```sh
curl -sS -X POST http://127.0.0.1:8080/v1/jobs/JOB_ID/cancel
# → {"status":"cancelling"}
```

### Requeue a failed job (manual retry)

```sh
curl -sS -X POST http://127.0.0.1:8080/v1/jobs/JOB_ID/requeue
# → {"status":"enqueued"}
```

### Chain a continuation (child job on terminal state)

```sh
curl -sS -X POST http://127.0.0.1:8080/v1/jobs/JOB_ID/continue \
  -H 'Content-Type: application/json' \
  -d '{"child_name":"echo","child_args":{"step":2},"condition":"on_succeeded"}'
# → {"status":"registered"}
```

Conditions: `on_succeeded` (default), `on_failed`, `on_any`.

### Queues and servers

```sh
curl -sS http://127.0.0.1:8080/v1/queues         # all queues + depth
curl -sS http://127.0.0.1:8080/v1/queues/default  # single queue detail
curl -sS http://127.0.0.1:8080/v1/servers         # active servers in cluster
```

### CLI equivalents

```sh
./bin/gfire job list --config gfire.yaml
./bin/gfire job list --state Failed --config gfire.yaml
./bin/gfire job get JOB_ID --config gfire.yaml
./bin/gfire job requeue JOB_ID --config gfire.yaml
```

[↑ Back to top](#readme-top)

## Current status

v1.0.3 — production-ready. Server, REST API, CLI, Prometheus metrics, all three storage backends. Recurring cron validation + `last_run`. Nested `GFIRE_*` env BindEnv. `/healthz` version/commit.

| Component | Status |
|-----------|--------|
| Storage (memory, PostgreSQL, Redis/ValKey) | ✅ |
| Engine (workers, retry, cancel, DLQ, result capture) | ✅ |
| Continuations + recurring cron + orphan recovery | ✅ |
| REST API (enqueue, batch, schedule, list, cancel, continue, requeue, delete, recurring) | ✅ |
| CLI (`gfire server`, job, migrate, queue, status) | ✅ |
| Bearer auth, OpenAPI (`/openapi.json`), Prometheus (`/metrics`) | ✅ |

[↑ Back to top](#readme-top)

## What GFire is

- **Headless service** — single binary, no embedded UI in v1
- **HTTP + curl** — apps never import GFire as a Go library
- **Multi-backend** — PostgreSQL (`SKIP LOCKED`), Redis, ValKey
- **Horizontal scale** — N peer nodes, shared storage, no Raft
- **Continuations** — chain jobs on success/failure
- **Handlers** — external binaries from YAML `cmd` (any language)

See [SPECIFICATIONS.md](SPECIFICATIONS.md) for the full design.

[↑ Back to top](#readme-top)

## Architecture

```
App (any language)  --HTTP-->  GFire API  -->  Engine / workers
                                                 |
                                                 v
                                           Shared storage
                                        (PG / Redis / ValKey)
                                                 |
                                                 v
                                        Handler subprocess (cmd)
```

Job args are **instruction cards** (~1KB), not large payloads. Heavy data lives in S3/DB; the handler fetches and processes it.

[↑ Back to top](#readme-top)

## Requirements

- **Go 1.26.5** (pinned in [`go.mod`](go.mod))
- Docker (optional) for PostgreSQL / Redis / ValKey via `docker compose`
- [`golang-migrate`](https://github.com/golang-migrate/migrate) CLI for PostgreSQL schema migrations

[↑ Back to top](#readme-top)

## Development

```sh
make help     # list all targets
make all      # fmt, vet, test, gocyclo, cover, build
make ci       # fmt-check + vet + gocyclo + test
make security # govulncheck + gocyclo + grype
make cover    # memory backend coverage (≥80% gate)
make version  # build and print version + commit info
make install  # install bin/gfire to $(go env GOPATH)/bin
make server   # build + run daemon
```

Binary output: `bin/gfire`.

### Release quality (fail-closed)

- Tag `v*` only from `main` after merging `develop`.
- Local bar before tagging: `make release-check` (fmt, vet, **test**, cover ≥80% memory, gocyclo, govulncheck, grype, `goreleaser check`).
- Tag workflow re-runs gates with `STRICT_RELEASE=1` (adds docker-scan) **before** GoReleaser publishes binaries/GHCR.
- Red gate = no image, no GitHub Release assets.

[↑ Back to top](#readme-top)

## PostgreSQL setup

Set `storage.backend: postgres` in `gfire.yaml`, then:

```sh
make db-up         # start postgres + redis + valkey via docker compose
make migrate-up    # apply gfire schema (includes migration 002 for job results)
go test ./internal/storage/postgres/ -count=1
./bin/gfire server --config gfire.yaml
```

Default DSN: `postgres://gfire:gfire@localhost:5432/gfire?sslmode=disable`
(see `Makefile` / `docker-compose.yml`).

[↑ Back to top](#readme-top)

## Redis / ValKey setup

Set `storage.backend: redis` in `gfire.yaml`, then:

```sh
make db-up         # starts redis on :6379, valkey on :6380
go test ./internal/storage/redis/ -count=1
./bin/gfire server --config gfire.yaml
```

ValKey is a drop-in Redis-compatible fork. Same config block, same `addr:port` field:

```sh
GFIRE_STORAGE_REDIS_ADDR=localhost:6380 ./bin/gfire server
```

[↑ Back to top](#readme-top)

## Compare

Snapshot v1.0.0. GFire is a standalone service (HTTP API); the rest are embedded Go libraries.

| Axis | GFire | Asynq | River |
|------|-------|-------|-------|
| Model | Standalone service | Go library | Go library |
| Enqueue | HTTP / curl ✅ | Go API | Go API |
| Storage | PG + Redis/ValKey ✅ | Redis | PostgreSQL |
| Handlers | External `cmd` ✅ | In-process | In-process |
| HA | N peers, no Raft (partial) | Redis | PG `SKIP LOCKED` |

Full matrix (Sidekiq, Celery, Faktory, narratives): **[docs/compare.md](docs/compare.md)** · [gfire.net/compare](https://gfire.net/compare)

[↑ Back to top](#readme-top)

## Project docs

| Doc | Purpose |
|-----|---------|
| [SPECIFICATIONS.md](SPECIFICATIONS.md) | Behavior / architecture contract |
| [ROADMAP.md](ROADMAP.md) | Weekly bands → v1.0.0 |
| [CHANGELOG.md](CHANGELOG.md) | Shipped changes per release |
| [gfire.example.yaml](gfire.example.yaml) | Configuration reference (every field documented) |
| [docs/compare.md](docs/compare.md) | GFire vs Asynq, River, Faktory, Sidekiq, Celery |
| [AGENTS.md](AGENTS.md) | Conventions for AI agents / contributors |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute |
| [SECURITY.md](SECURITY.md) | Vulnerability reporting |
| [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) | Community standards |

[↑ Back to top](#readme-top)

## Get involved

- Open an [issue](https://github.com/hrodrig/gfire/issues) for bugs or ideas
- PRs target **`develop`** (see [CONTRIBUTING.md](CONTRIBUTING.md))
- Security: report privately via [SECURITY.md](SECURITY.md) — do not open a public issue for undisclosed vulns

[↑ Back to top](#readme-top)

## License

[MIT](LICENSE) — Copyright (c) 2026 hrodrig.

[↑ Back to top](#readme-top)
