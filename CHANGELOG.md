# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned

- **Pipelines (DAG)** — Band 8 / v1.1.0
- Recurring cron API (v0.5.0 band), bulk enqueue, OpenAPI, job delete (B5-014), Prometheus `/metrics`

### Fixed

- SPEC/ROADMAP sync post-Hermes audit: Storage 31 methods, Result B3-011 implemented, cancel/auth marked shipped, delete tracked as B5-014

## [0.6.1] - 2026-07-11

Band 7 audit hardening — security + shutdown fixes (MiniMax review).

### Security

- **B7-001:** Bearer token check used plain string equality, enabling timing side-channels when `auth.enabled` is on. Compare tokens with `crypto/subtle.ConstantTimeCompare` instead.

### Fixed

- **B7-002:** Engine `promoteScheduled` loop looked like it promoted jobs but only iterated tickets; storage already moves due scheduled jobs to Enqueued. Renamed to `tickScheduler` and documented that promotion lives in `Storage.GetDueScheduled`.
- **B7-003:** During shutdown, workers called `GetJob` with the engine's canceled context after dequeue, so jobs could stay in `Processing` until orphan recovery (~5 min). Load dequeued jobs with a detached context so in-flight work can finish or fail cleanly.

## [0.6.0] - 2026-07-11

First **usable preview**: run the daemon, enqueue with curl, inspect with CLI.

### Added

- **Band 4 (partial):** continuations on terminal state, coordinator orphan recovery, job cleanup loop, scheduled retry promotion (engine).
- **Band 5 (core):** `internal/api` — enqueue, schedule, get/list jobs, requeue, cancel, continue, queues, servers, healthz/readyz, optional Bearer auth.
- **Band 6 (core):** Cobra CLI — `gfire server`, `gfire job list|get|requeue`; `internal/config` + `gfire.example.yaml`.
- **`internal/app`:** SIGINT/SIGTERM graceful shutdown for engine + HTTP.

### Notes

- Default in-memory backend; PostgreSQL/Redis via config. Run `make migrate-up` for PG (migration `002_job_result`).
- Not tagged as production-ready; v1.0.0 remains Band 7 polish.
- **v0.5.0 skipped** — recurring cron (Band 4 remainder) deferred; shipped v0.4.0 (engine) then v0.6.0 (usable preview) directly.

## [0.4.0] - 2026-07-11

Band 3 milestone — engine processes jobs with retry, cancel, and DLQ.

### Added

- **Engine (Band 3):** worker pool (goroutine-per-worker), exponential backoff retry with jitter, in-flight job cancel (B3-009), per-queue concurrency caps (B3-010), job result capture — handler stdout cap 64KB (B3-011), Dead/poison queue after retry exhaustion (B3-012).
- **Middleware pipeline:** `PanicRecovery`, context propagation, attempt counting.
- **Handler model:** external subprocess via YAML `cmd`; in-process `Func` runner for tests.
- Per-job timeout + heartbeat ticker (60s) for long-running handlers.
- Graceful shutdown: SIGTERM → drain workers → re-queue in-flight jobs → unregister.
- Integration test: in-memory engine processes 100 jobs.

## [0.3.0] - 2026-07-09

Band 2 milestone — Redis and ValKey storage backends work.

### Added

- **Redis / ValKey storage** (Band 2): `internal/storage/redis/` — full `Storage` implementation shared by Redis and ValKey (`go-redis/v9`, BRPOP dequeue, Lua scripts for atomic state transitions, sorted sets for scheduling).
- Integration tests for Redis backend (`make db-up` + `go test ./internal/storage/redis/`); ValKey via `GFIRE_REDIS_ADDR=localhost:6380`.
- **Compare docs**: `docs/compare.md` + README Compare section (matrix vs Asynq, River, Faktory, Sidekiq, Celery).

### Notes

- All three storage backends (memory, PostgreSQL, Redis/ValKey) implement the same `Storage` interface. No engine, REST API, or CLI beyond `gfire version` yet.

## [0.2.0] - 2026-07-08

Band 1 milestone — PostgreSQL backend works.

### Added

- **Band 0 — Foundation**: `internal/job/` domain types, `Storage` interface, sentinel errors, in-memory backend with tests, `cmd/gfire` stub (`gfire version`), `SPECIFICATIONS.md`, `ROADMAP.md`, `AGENTS.md`.
- **Band 1 — PostgreSQL**: `internal/storage/postgres/` with `SKIP LOCKED` dequeue, `golang-migrate` schema (`gfire` namespace), integration tests, `docker-compose.yml` (PostgreSQL + Redis + ValKey images for later bands).
- **CI / release**: GitHub Actions (`ci.yml`, `release.yml`), GoReleaser config, Makefile targets (`make ci`, `make release-check`, `make security`).
- **Docs**: README, CONTRIBUTING, SECURITY, CODE_OF_CONDUCT, planning triad (SPEC / ROADMAP / AGENTS).

### Notes

- No production-ready engine, REST API, or CLI beyond `gfire version`. Storage foundation only.

[Unreleased]: https://github.com/hrodrig/gfire/compare/v0.6.1...HEAD
[0.6.1]: https://github.com/hrodrig/gfire/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/hrodrig/gfire/compare/v0.4.0...v0.6.0
[0.3.0]: https://github.com/hrodrig/gfire/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/hrodrig/gfire/releases/tag/v0.2.0
