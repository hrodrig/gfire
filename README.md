# GFire — standalone background job service

<a id="readme-top"></a>

**🔥** _Language-agnostic job orchestration over HTTP — PostgreSQL, Redis, or ValKey_

[![Version](https://img.shields.io/badge/version-0.6.0-blue)](./VERSION)
[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)
[![Status](https://img.shields.io/badge/status-early%20development-orange)](#early-development--not-for-production)
[![gghstats clones](https://gghstats.hermesrodriguez.com/api/v1/badge/hrodrig/gfire?metric=clones)](https://gghstats.hermesrodriguez.com/hrodrig/gfire)

**Repo:** [github.com/hrodrig/gfire](https://github.com/hrodrig/gfire) · **Site:** [gfire.net](https://gfire.net) · **Spec:** [SPECIFICATIONS.md](SPECIFICATIONS.md) · **Roadmap:** [ROADMAP.md](ROADMAP.md) · **Security:** [SECURITY.md](SECURITY.md)

<p align="center">
  <img src="docs/gfire-hero.png" alt="GFire — standalone background job service" width="100%" />
</p>

GFire is a **headless background job service**: a standalone Go binary that runs a worker pool and (soon) a REST API. Applications enqueue work over HTTP — they never import GFire as a library. Workers spawn external handler processes (`cmd`) against shared storage (**PostgreSQL**, **Redis**, or **ValKey**).

> **Early development (v0.6.0 — usable preview).** Run `gfire server`, enqueue via **curl**, inspect with CLI. Not production-hardened — see [ROADMAP.md](ROADMAP.md).

## Table of contents

- [Early development — not for production](#early-development--not-for-production)
- [Current status](#current-status-band-1--v020)
- [What GFire will be](#what-gfire-will-be)
- [Architecture (sketch)](#architecture-sketch)
- [Requirements](#requirements)
- [Development](#development)
- [PostgreSQL (Band 1)](#postgresql-band-1)
- [Redis / ValKey (Band 2)](#redis--valkey-band-2)
- [Compare](#compare)
- [Project docs](#project-docs)
- [Get involved](#get-involved)
- [License](#license)

[↑ Back to top](#readme-top)

## Early development — not for production

**GFire is in pre-release (v0.6.0 — first usable preview).**

- **`gfire server`** runs engine + REST API (memory/PostgreSQL/Redis backends).
- Enqueue and inspect jobs with **curl** or **`gfire job`** — no Go client required.
- Recurring cron, bulk enqueue, OpenAPI, and Prometheus metrics are **not** in this release yet.
- **Do not use in production** without your own hardening.

Check [ROADMAP.md](ROADMAP.md) for what is planned and what is done.

[↑ Back to top](#readme-top)

## Current status (v0.6.0 — usable preview)

| Component | Status |
|-----------|--------|
| Storage (memory, PG, Redis/ValKey) | ✅ |
| Engine (workers, retry, cancel, DLQ) | ✅ |
| Continuations + orphan recovery | ✅ |
| REST API (enqueue, schedule, list, cancel, continue) | ✅ core |
| CLI (`gfire server`, `gfire job list/get/requeue`) | ✅ core |
| Recurring cron, bulk enqueue, OpenAPI, `/metrics` | ⬜ next bands |

[↑ Back to top](#readme-top)

## What GFire will be

- **Headless service** — single binary, no embedded UI in v1
- **HTTP + curl** — apps never import GFire as a Go library
- **Multi-backend** — PostgreSQL (`SKIP LOCKED`), Redis, ValKey
- **Horizontal scale** — N peer nodes, shared storage, no Raft
- **Continuations** — chain jobs on success/failure
- **Handlers** — external binaries from YAML `cmd` (any language)

See [SPECIFICATIONS.md](SPECIFICATIONS.md) for the full design.

[↑ Back to top](#readme-top)

## Architecture (sketch)

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

- **Go 1.26.5** (pinned in [`go.mod`](go.mod); includes fix for `crypto/tls` GO-2026-5856)
- Docker (optional) for PostgreSQL / Redis / ValKey via `docker compose`
- [`golang-migrate`](https://github.com/golang-migrate/migrate) CLI for schema migrations (Band 1)

[↑ Back to top](#readme-top)

## Development

```sh
make help     # list targets
make all      # fmt, vet, test, gocyclo, cover, build
make ci       # fmt-check + vet + gocyclo + test
make security # govulncheck + gocyclo + grype
make cover    # memory backend ≥80% gate
make version  # build and print version metadata
make install  # install bin/gfire to $(go env GOPATH)/bin
make server   # build + run daemon (creates gfire.yaml from example if missing)
```

Binary output: `bin/gfire`.

### Quick start (in-memory)

```sh
make server   # or: make build && ./bin/gfire server --config gfire.yaml
```

Enqueue a job:

```sh
curl -sS -X POST http://127.0.0.1:8080/v1/jobs/enqueue \
  -H 'Content-Type: application/json' \
  -d '{"name":"echo","args":{"hello":"world"},"queue":"default"}'
```

List jobs:

```sh
./bin/gfire job list --config gfire.yaml
curl -sS 'http://127.0.0.1:8080/v1/jobs?limit=10'
```

PostgreSQL: set `storage.backend: postgres` in `gfire.yaml`, run `make db-up && make migrate-up` (includes migration `002` for job results).

```sh
./bin/gfire version
```

[↑ Back to top](#readme-top)

## PostgreSQL (Band 1)

```sh
make db-up
make migrate-up
go test ./internal/storage/postgres/ -count=1
```

Default DSN: `postgres://gfire:gfire@localhost:5432/gfire?sslmode=disable` (see `Makefile` / `docker-compose.yml`).

[↑ Back to top](#readme-top)

## Redis / ValKey (Band 2)

```sh
make db-up
go test ./internal/storage/redis/ -count=1
```

Default Redis: `localhost:6379`. ValKey (same API): `GFIRE_REDIS_ADDR=localhost:6380 go test ./internal/storage/redis/ -count=1`.

[↑ Back to top](#readme-top)

## Compare

> Snapshot **v0.3.0 / Band 2**. GFire cells marked WIP/planned are not production-ready.

| Axis | GFire | Asynq | River |
|------|-------|-------|-------|
| Model | Standalone service | Go library | Go library |
| Enqueue | HTTP / curl (`WIP`) | Go API | Go API |
| Storage | PG + Redis/ValKey `shipped` | Redis | PostgreSQL |
| Handlers | External `cmd` (`planned`) | In-process | In-process |
| HA | N peers, no Raft (`planned`) | Redis | PG `SKIP LOCKED` |

Full matrix (Sidekiq, Celery, Faktory, narratives): **[docs/compare.md](docs/compare.md)** · [gfire.net/compare](https://gfire.net/compare)

[↑ Back to top](#readme-top)

## Project docs

| Doc | Purpose |
|-----|---------|
| [SPECIFICATIONS.md](SPECIFICATIONS.md) | Behavior / architecture contract |
| [ROADMAP.md](ROADMAP.md) | Weekly bands → v1.0.0 |
| [CHANGELOG.md](CHANGELOG.md) | Shipped changes per release |
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
