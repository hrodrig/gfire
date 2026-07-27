# GFire — Agent Guide

This file tells AI agents (Cline, Claude Code, Copilot, etc.) how to work with this project.

## Project

- **Go module:** `github.com/hrodrig/gfire`
- **Language:** Go 1.26.5
- **Status:** Early development (pre-v1.0.0)
- **License:** MIT (core)

## Coding Standards

### Go
- No unused imports or variables (enforced by `go vet`)
- `gocyclo` cyclomatic complexity ≤ 15 per function
- Test coverage >= 80% on the memory storage backend (enforced by `make cover`)
- `gofmt` / `gofumpt` formatting
- No `init()` functions outside of migration or config registration
- Use `context.Context` as first parameter for all public functions
- Return sentinel errors from `internal/storage/errors/`, not strings

### Layout
- **Service, not library** — all Go code under `internal/`; entry point `cmd/gfire/main.go`
- No domain packages at repo root

### Concurrency
- All Storage methods must be safe for concurrent access
- Use `sync.RWMutex` for read-heavy, write-light operations
- Worker goroutines must respond to context cancellation
- Channels for signaling (dequeue), mutexes for data

### Testing
- Table-driven tests where possible
- Integration tests for storage backends require docker-compose
- Unit tests for in-memory backend must be self-contained (no deps)
- Use `-count=1` to disable test caching on CI

### Git
- Work on `develop` branch (no feature branches)
- Commits: imperative mood, English, 72-char subject line
- No commits to `.no-va-al-repo/` (it's gitignored)

## Architecture

```
API (net/http) → Engine (workers) → Storage (interface)
                   ↑
              Handlers (subprocess cmd)
```

- GFire is a **headless service** — no embedded UI in v1
- All state lives in the storage backend (PostgreSQL, Redis, or ValKey)
- All GFire nodes are peers — no leader, no Raft
- Horizontal scaling: add N+1 pods, they coordinate via storage
- Handlers are external binaries (YAML `cmd`). GFire spawns them per job.

## Build & Test

```sh
make help
make all            # fmt, vet, test, gocyclo, cover, build
make ci             # fmt-check + vet + gocyclo + test
make security       # govulncheck + gocyclo + grype
make release-check  # semver + goreleaser check + quality gates
make snapshot       # goreleaser snapshot to dist/ (no tag)
```

## Roadmap (abbreviated)

| Week | Band | What |
|------|------|------|
| 1 | 0 | Foundation ✅ |
| 2-3 | 1 | PostgreSQL ✅ |
| 3-4 | 2 | Redis / ValKey |
| 4-5 | 3 | Engine |
| 5-6 | 4 | Scheduler |
| 6-7 | 5 | REST API |
| 7 | 6 | CLI |
| 8 | 7 | Release |

See `ROADMAP.md` for full detail.
