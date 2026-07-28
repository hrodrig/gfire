# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-07-27

First **production-ready** release. All Band 0–7 delivered.

### Added

- **Band 4 (complete):** recurring cron (robfig/cron + distributed lock), stale server registry sweep with automatic unregister.
- **Band 5 (complete):** job delete (B5-014), recurring CRUD handlers (list/create/delete/trigger), bulk enqueue with partial acceptance (B5-009), idempotency-key client retry deduplication (B5-010), OpenAPI 3.0 spec at `GET /openapi.json` (B5-013).
- **Band 6 (complete):** `gfire migrate`, `gfire queue list`, `gfire server status` CLI commands; `gfire job cancel` via REST API; Prometheus `GET /metrics` endpoint; dead filter via `--state`.
- **Band 7 (complete):** B7-004 worker exponential backoff, B7-005 config validation on load, B7-006 requeue terminal-state guard, real HTTP server shutdown, request-ID middleware, `gfire.example.yaml` fully documented, README rewrite with curl cookbook and config reference, E2E test suite (20 steps, postgres backend).

### Changed

- Docs sync: SPEC/ROADMAP/README/example YAML mark B5–B6 surfaces as shipped (remove stale "planned" markers); README version badge and status → v1.0.0.

### Security

- B7-001: Bearer token constant-time compare (`crypto/subtle.ConstantTimeCompare`).
- Bump `golang.org/x/text` v0.29.0 → v0.39.0 (closes GO-2026-5970).

### Fixed

- B7-002: Scheduler tick renamed and documented.
- B7-003: Post-dequeue `GetJob` uses detached context on shutdown.

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

[Unreleased]: https://github.com/hrodrig/gfire/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/hrodrig/gfire/compare/v0.6.1...v1.0.0
[0.6.1]: https://github.com/hrodrig/gfire/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/hrodrig/gfire/compare/v0.4.0...v0.6.0
[0.3.0]: https://github.com/hrodrig/gfire/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/hrodrig/gfire/releases/tag/v0.2.0
