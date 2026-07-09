# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/hrodrig/gfire/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/hrodrig/gfire/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/hrodrig/gfire/releases/tag/v0.2.0
