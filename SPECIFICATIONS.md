# GFire — Standalone Background Job Service

**Status:** Live design (aligned with ROADMAP — headless v1)
**Language:** Go
**Storage backends:** Redis, ValKey, PostgreSQL
**Home:** `github.com/hrodrig/gfire`

---

## Table of Contents

1. [Why GFire?](#1-why-gfire)
2. [Core Architecture](#2-core-architecture)
3. [REST API (HTTP)](#3-rest-api-http)
4. [Job Model & State Machine](#4-job-model--state-machine)
5. [Storage Interface](#5-storage-interface)
6. [Redis / ValKey Storage](#6-redis--valkey-storage)
7. [PostgreSQL Storage](#7-postgresql-storage)
8. [Server & Worker Pool](#8-server--worker-pool)
9. [Recurring Jobs (Cron)](#9-recurring-jobs-cron)
10. [Delayed & Scheduled Jobs](#10-delayed--scheduled-jobs)
11. [Job Continuations (Chaining)](#11-job-continuations-chaining)
12. [Pipelines (DAG orchestration)](#12-pipelines-dag-orchestration)
13. [Filters / Middleware Pipeline](#13-filters--middleware-pipeline)
14. [Retry & Failure Handling](#14-retry--failure-handling)
15. [Distributed Coordination](#15-distributed-coordination)
16. [Observability & Ops Surface](#16-observability--ops-surface)
17. [Package Layout](#17-package-layout)
18. [Open Questions](#18-open-questions)

---



## 1. Why GFire?

Most background job processors run embedded in your application process. Your code imports a library, configures it at startup, and workers share the same memory and lifecycle as your app. That works well — but it's not the only way.

**GFire is a standalone service.** A separate binary running in its own process (or container). Your application sends jobs via HTTP and never imports GFire as a dependency.

> **GFire is an independent service.** A separate binary running in its own process (or container). Your application sends jobs via HTTP and never imports GFire as a dependency.

This means:

- **Your app doesn't import GFire** — no Go module dependency, no init cost
- **Language-agnostic** — any language (Go, Python, JS, Rust, C#) can send jobs via HTTP
- **Independent lifecycle** — deploy, scale, and restart GFire independently of your app
- **Dedicated resources** — CPU/memory for job processing doesn't compete with your app
- **Operate separately** — monitor, alert, and debug jobs without touching app logs



### Goals


| Goal                      | Detail                                                    |
| ------------------------- | --------------------------------------------------------- |
| **Standalone service**    | Single binary, runs as a daemon / container               |
| **HTTP API**              | Submit, inspect, and manage jobs via REST                 |
| **Pluggable storage**     | Redis, ValKey, PostgreSQL — common `Storage` interface    |
| **Goroutine worker pool** | No OS threads; channels for dispatch                      |
| **Graceful shutdown**     | Drain in-flight jobs, finish before exit                  |
| **Observability**         | Prometheus metrics + structured logging (`slog`)          |
| **Multi-node**            | Multiple GFire nodes coordinate via shared storage        |
| **Headless v1**           | No embedded UI — CLI + REST + Prometheus; GFireUI post-v1 |
| **Zero app dependency**   | App only needs `curl` or an HTTP client                   |




### Non-goals

- Replace Kafka / NATS / message brokers
- Be a Go library that apps import directly
- Support .NET-style reflection-based job registration
- Be embedded in another process
- Ship a full web dashboard in v1 (that is GFireUI, separate project)
- Replicate Airflow/Dagster operator ecosystems or visual DAG editors in the core binary (see [§12 Pipelines](#12-pipelines-dag-orchestration) for headless DAG orchestration post-v1)

---



## 2. Core Architecture

GFire is a **standalone headless service**. Apps talk HTTP; workers dequeue jobs and spawn external handler processes (`cmd`). All nodes are peers sharing one storage backend.

### System overview

```mermaid
flowchart TB
  subgraph Apps["Your applications"]
    A1["HTTP client / curl"]
    A2["Any language"]
  end

  subgraph GFire["GFire node (peer)"]
    API["REST API :8080"]
    CLI["CLI ops"]
    MET["GET /metrics"]
    ENG["Engine"]
    W1["Worker"]
    W2["Worker"]
    WN["Worker N"]
    SCH["Scheduler"]
    CRD["Coordinator"]
    ENG --> W1 & W2 & WN
    ENG --> SCH & CRD
    API --> ENG
  end

  subgraph Storage["Shared storage"]
    ST[("PostgreSQL / Redis / ValKey")]
  end

  subgraph Handlers["External handlers"]
    H1["cmd: zip_csv_ingest"]
    H2["cmd: price_sync"]
    H3["cmd: …"]
  end

  A1 & A2 -->|enqueue / schedule / inspect| API
  CLI --> API
  W1 & W2 & WN -->|Dequeue / ApplyState| ST
  SCH & CRD --> ST
  API --> ST
  W1 -.->|spawn subprocess| H1
  W2 -.->|spawn subprocess| H2
  WN -.->|spawn subprocess| H3
  MET -.->|Prometheus scrape| OBS["Grafana / Prometheus"]
```





### Multi-node (no Raft)

```mermaid
flowchart LR
  PA["GFire Pod A"] --> ST[("Shared storage")]
  PB["GFire Pod B"] --> ST
  PC["GFire Pod C"] --> ST
  ST -->|atomic dequeue| PA & PB & PC
```



All pods are peers. Dequeue atomicity comes from storage (`SKIP LOCKED` / `BRPOP`), not leader election.

### Job execution path

```mermaid
sequenceDiagram
  participant App
  participant API as GFire API
  participant Store as Storage
  participant Worker
  participant Cmd as Handler cmd

  App->>API: POST /v1/jobs/enqueue {name, args}
  API->>Store: Enqueue(job)
  API-->>App: 201 {job_id}

  Worker->>Store: Dequeue(queues)
  Store-->>Worker: JobTicket
  Worker->>Cmd: spawn + args via stdin/JSON
  Cmd-->>Worker: exit 0 / error
  Worker->>Store: ApplyState Succeeded|Failed
  Note over Worker,Store: On failure: retry / schedule backoff
```





### Logical entities


| Entity           | Role                                                          |
| ---------------- | ------------------------------------------------------------- |
| **HTTP API**     | REST interface — your app talks to this, never imports GFire  |
| **Core Engine**  | Internal: worker pool, scheduler, coordinator (no public API) |
| **CLI**          | `gfire` binary — server start, job/queue inspection, migrate  |
| **Storage**      | Interface + backend implementations (Redis, ValKey, PG)       |
| **Job**          | Serializable invocation data (instruction card, not payload)  |
| **State**        | Job lifecycle node (immutable, append-only)                   |
| **Worker**       | Goroutine inside the engine that dequeues and runs            |
| **Queue**        | Named FIFO (default "default"), optional per-queue concurrency cap |
| **Handler**      | External binary from YAML `cmd` — GFire spawns one per job    |
| **Result**       | Optional handler stdout JSON (cap 64KB), stored for continuations |
| **Middleware**   | Pluggable chain around job execution                          |
| **RecurringJob** | Cron schedule → enqueues on tick                              |
| **Continuation** | Conditional child job (on success / failure / any)            |


---



## 3. REST API (HTTP)

Your application never imports GFire. It sends HTTP requests to the GFire server.

> **Implementation status:** Core routes ship in v0.6.0. Items marked **(planned)** are specified for contract tests; see ROADMAP IDs `B5-009`, `B5-010`, `B5-013`, `B5-014`.

### Base URL

```
http://gfire:8080/v1
```



### Endpoints



#### Jobs

```
POST   /v1/jobs/enqueue              → { job_id, status }
POST   /v1/jobs/enqueue/batch        → { job_ids[], accepted, rejected }   # planned B5-009
POST   /v1/jobs/schedule             → { job_id, enqueue_at, status }
GET    /v1/jobs/{id}                  → job detail + state history + result?
GET    /v1/jobs                      → paginated, filterable (?state=&queue=&limit=&offset=)
POST   /v1/jobs/{id}/requeue         → { status }
POST   /v1/jobs/{id}/cancel          → { status }                            # v0.6.0 (B5-011)
POST   /v1/jobs/{id}/delete          → { status }                            # planned
```

**Headers (enqueue / batch):**

| Header            | Required | Purpose |
| ----------------- | -------- | ------- |
| `Idempotency-Key` | No       | Client retry dedupe — same key returns same `job_id` **(planned B5-010)** |
| `Authorization`   | When `auth.enabled` | `Bearer <token>` **(v0.6.0, B5-012)** |

**Enqueue request:**

```json
{
  "name": "send_email",
  "args": {
    "to": "user@example.com",
    "template": "welcome"
  },
  "queue": "default",
  "retry_max": 5
}
```

**Enqueue response:**

```json
{
  "job_id": "01934567-89ab-4cde-f012-3456789abcde",
  "status": "enqueued",
  "queue": "default"
}
```

**Bulk enqueue request (planned B5-009):**

```json
{
  "jobs": [
    { "name": "sap_extract", "args": { "source": "s3://bucket/a.csv" }, "queue": "critical" },
    { "name": "sap_extract", "args": { "source": "s3://bucket/b.csv" }, "queue": "critical" }
  ]
}
```

Each job in the batch is an instruction card (~1 KB). Partial acceptance is allowed: valid jobs enqueue, invalid entries appear in `rejected` with reasons.

**Schedule request:**

```json
{
  "name": "send_reminder",
  "args": { "user_id": 42 },
  "queue": "default",
  "enqueue_at": "2026-07-10T15:00:00Z"
}
```



#### Queues

```
GET    /v1/queues                    → list of queues + depth
GET    /v1/queues/{name}             → queue detail + stats
```



#### Recurring Jobs

```
GET    /v1/recurring                 → all recurring definitions
POST   /v1/recurring                 → create recurring job
DELETE /v1/recurring/{id}            → remove recurring job
POST   /v1/recurring/{id}/trigger    → fire immediately, outside schedule
```

#### Discovery

```
GET    /openapi.json                 → OpenAPI 3 document for all /v1/* routes   # planned B5-013
GET    /healthz                      → { status: "ok" }
GET    /readyz                       → { status: "ok" } (storage reachable)
GET    /metrics                      → Prometheus text (when metrics.enabled)
```

**Recurring create request:**

```json
{
  "id": "nightly-cleanup",
  "cron": "0 2 * * *",
  "queue": "default",
  "name": "cleanup_expired",
  "args": { "days": 30 }
}
```



#### Servers

```
GET    /v1/servers                   → active + stale servers
```



#### Continuations

```
POST   /v1/jobs/{id}/continue       → create continuation
```

**Continuation request:**

```json
{
  "child_name": "send_confirmation",
  "child_args": { "order_id": 123 },
  "child_queue": "default",
  "condition": "OnSucceeded"
}
```



### Ops surface (v1 — headless)

No embedded web UI in v1. Operators use:


| Surface           | Purpose                                      |
| ----------------- | -------------------------------------------- |
| REST `/v1/*`      | Programmatic job/queue/recurring management  |
| CLI `gfire job    | queue                                        |
| `GET /metrics`    | Prometheus scrape                            |
| GFireUI (post-v1) | Separate React app against the same REST API |




### Error format

```json
{
  "error": {
    "code": "JOB_NOT_FOUND",
    "message": "job with ID 01934567-... not found"
  }
}
```



### Payload size and large data

GFire enforces a max request body size (default 10MB, configurable via `server.max_body_size`). Requests exceeding the limit return HTTP 413.

**This is by design: job args are parameters, not data.** For large payloads (CSV exports, SAP extractions, file processing), store the data in object storage (S3, MinIO, GCS) or a shared filesystem first, then pass the reference:

```json
{
  "name": "etl_sap_extract",
  "args": {
    "source_url": "s3://bucket/extract-2026-07-08.csv",
    "target_table": "facturas",
    "format": "csv"
  }
}
```

The handler (`cmd`) is responsible for fetching and processing the referenced data.

**Future option:** If inline large payloads become necessary, GFire could store them in object storage transparently (the API accepts the large body, GFire streams it to S3, stores the reference in the DB, and provides the file to the subprocess handler via a temp file or pipe). This is a post-v1 enhancement.

### Authentication

Optional Bearer token when `auth.enabled: true`. Off by default (local/dev, private network).

```
Authorization: Bearer <auth.token>
```

Unauthenticated requests to protected routes return `401`. When disabled, all `/v1/*` routes are open — do not expose `:8080` on the public internet without TLS + auth.

**Implemented:** v0.6.0 (`B5-012`).

---



## 4. Job Model & State Machine



### Job

```go
type Job struct {
    ID        string        // UUIDv7 — time-sortable
    Name      string        // logical name for the handler
    Args      []byte        // serialized (JSON by default)
    Queue     string        // queue name
    CreatedAt time.Time
    RetryMax  int           // per-job override of default retry count
    Timeout   time.Duration // max handler runtime; 0 = server.default_timeout
    Result    []byte        // handler stdout JSON (optional, cap 64KB) — B3-011 ✅
}
```



### States

```
                     ┌──────────┐
                     │ Enqueued │ ◄── Client.Enqueue()
                     └─────┬────┘
                           │ fetched by worker (atomic)
                           ▼
                     ┌───────────┐
          cancel ──► │ Processing│ ───► success ──► ┌───────────┐
          (B5-011)   └─────┬─────┘                  │ Succeeded │
                           │ error / panic          └─────┬─────┘
                           ▼                              │
                     ┌────────┐                           │
                     │ Failed │  ◄── error, retries left   │
                     └───┬────┘                           │
                         │ retry N < Max                  │
                         ▼                                │
                     ┌──────────┐                         │
                     │ Scheduled│ ──► Enqueued (re-fetch) │
                     └───┬──────┘                         │
                         │ retries exhausted (B3-012)     │
                         ▼                                │
                     ┌────────┐                           │
                     │  Dead  │  poison / DLQ — manual     │
                     └────────┘  requeue only              │
                                                          │
                     ┌───────────┐                         │
                     │ Cancelled │ ◄── cancel API (B5-011) │
                     └───────────┘                         │
                                                          │
                     ┌──────────┐                         │
                     │ Awaiting │ ◄── continuation parent │
                     └──────────┘       enqueues child    │
                          │                               │
                          └── parent terminates ──────────┘
                                       (condition match)

    ┌──────────┐
    │ Deleted  │ — manual or automatic TTL-based cleanup
    └──────────┘
```

**State semantics:**

| State       | Retryable | Meaning |
| ----------- | --------- | ------- |
| `Failed`    | Yes       | Handler error; retry middleware may schedule another attempt |
| `Dead`      | No        | `retry_max` exhausted — poison / DLQ; inspect via `--state dead` |
| `Cancelled` | No        | Operator cancelled in-flight job; handler received SIGTERM |



### State transition rules


| From       | To         | Trigger                                |
| ---------- | ---------- | -------------------------------------- |
| Enqueued   | Processing | Worker dequeues (atomic fetch)         |
| Processing | Succeeded  | Handler returns nil                    |
| Processing | Failed     | Handler returns error                  |
| Processing | Cancelled  | Cancel API / engine cancel signal (B3-009) |
| Failed     | Enqueued   | Manual re-queue via API/CLI            |
| Failed     | Scheduled  | Retry middleware enqueues with delay   |
| Failed     | Dead       | `retry_max` exhausted (B3-012)         |
| Dead       | Enqueued   | Manual re-queue via API/CLI            |
| Scheduled  | Enqueued   | Scheduler goroutine fires              |
| Succeeded  | Deleted    | Cleanup TTL expired                    |
| Cancelled  | Deleted    | Cleanup TTL expired                    |
| Dead       | Deleted    | Cleanup TTL expired                    |
| Awaiting   | Enqueued   | Parent reaches matching terminal state |
| *          | Deleted    | Manual delete                          |




### JobState (immutable audit log)

```go
type JobState struct {
    Name      string            // "Enqueued" | "Processing" | "Succeeded" | ...
    Reason    string            // human-readable reason
    Data      map[string]string // metadata (retry_count, error_type, server_id, duration_ms)
    CreatedAt time.Time
}

type JobWithStates struct {
    Job    *Job
    States []*JobState
}
```

---



## 5. Storage Interface

The `Storage` interface is **the** abstraction. Every backend implements it. **31 methods** (including `Close`; `ScheduleRetry` and `SetJobResult` added in Band 3).

```go
type Storage interface {
    // ──────────────────────────────────────────────
    // Queues & Job Dispatch
    // ──────────────────────────────────────────────

    // Enqueue adds a job to the tail of the named queue.
    Enqueue(ctx context.Context, queue string, job *Job) (id string, err error)

    // Dequeue blocks until a job is available on any of the given queues,
    // or until the context is cancelled / timeout expires.
    // Returns a JobTicket: jobID + a fetch token for optimistic concurrency.
    Dequeue(ctx context.Context, queues []string, timeout time.Duration) (*JobTicket, error)

    // Requeue moves a job back to Enqueued state (orphan recovery, manual retry).
    Requeue(ctx context.Context, jobID string, reason string) error

    // ──────────────────────────────────────────────
    // State Machine
    // ──────────────────────────────────────────────

    // ApplyState transitions the job from expectedCurrent to newState.
    // Returns ErrStateConflict if the job is not in expectedCurrent.
    ApplyState(ctx context.Context, jobID string, expectedCurrent string, newState *JobState) error

    // HeartbeatJob updates last_progress for a Processing job (long-running work).
    // Workers call this every ~60s so orphan recovery does not re-queue active jobs.
    HeartbeatJob(ctx context.Context, jobID string) error

    // GetOrphanedJobs returns Processing jobs whose progress heartbeat is older than staleAge.
    // Coordinator uses this when a worker dies mid-job.
    GetOrphanedJobs(ctx context.Context, staleAge time.Duration) ([]*JobTicket, error)

    // GetJob retrieves a job with its full state history.
    GetJob(ctx context.Context, jobID string) (*JobWithStates, error)

    // GetJobsByState retrieves jobs in a given state (pagination, for recovery).
    GetJobsByState(ctx context.Context, state string, offset, limit int) ([]*JobWithStates, error)

    // ──────────────────────────────────────────────
    // Queue Metadata
    // ──────────────────────────────────────────────

    GetQueueLength(ctx context.Context, queue string) (int64, error)
    GetQueues(ctx context.Context) ([]string, error)

    // ──────────────────────────────────────────────
    // Server Registry
    // ──────────────────────────────────────────────

    RegisterServer(ctx context.Context, server *ServerInfo, ttl time.Duration) error
    UnregisterServer(ctx context.Context, serverID string) error
    Heartbeat(ctx context.Context, serverID string, ttl time.Duration) error
    GetServers(ctx context.Context) ([]*ServerInfo, error)

    // ──────────────────────────────────────────────
    // Scheduled Jobs (delayed / retry)
    // ──────────────────────────────────────────────

    AddScheduled(ctx context.Context, enqueueAt time.Time, job *Job) (id string, err error)
    GetDueScheduled(ctx context.Context, now time.Time, batchSize int) ([]*JobTicket, error)
    RemoveScheduled(ctx context.Context, jobID string) error

    // ──────────────────────────────────────────────
    // Recurring Jobs
    // ──────────────────────────────────────────────

    UpsertRecurring(ctx context.Context, entry *RecurringJobEntry) error
    RemoveRecurring(ctx context.Context, id string) error
    GetRecurringJobs(ctx context.Context) ([]*RecurringJobEntry, error)

    // ──────────────────────────────────────────────
    // Continuations
    // ──────────────────────────────────────────────

    AddContinuation(ctx context.Context, parentID string, child *ContinuationEntry) error
    GetContinuations(ctx context.Context, parentID string) ([]*ContinuationEntry, error)
    RemoveContinuations(ctx context.Context, parentID string) error

    // ──────────────────────────────────────────────
    // Counters (ops / metrics stats)
    // ──────────────────────────────────────────────

    IncrementCounter(ctx context.Context, key string, delta int64) error
    GetCounter(ctx context.Context, key string) (int64, error)
    GetAllCounters(ctx context.Context, skip, limit int) (map[string]int64, error)

    // ──────────────────────────────────────────────
    // Distributed Lock
    // ──────────────────────────────────────────────

    AcquireLock(ctx context.Context, resource string, ttl time.Duration) (Lock, error)

    // ──────────────────────────────────────────────
    // Maintenance
    // ──────────────────────────────────────────────

    // RemoveExpired deletes jobs with a terminal state older than the cutoff.
    RemoveExpired(ctx context.Context, cutoff time.Time) (int64, error)

    // ScheduleRetry moves a Failed job to Scheduled with enqueue_at (retry backoff).
    ScheduleRetry(ctx context.Context, jobID string, enqueueAt time.Time) error

    // SetJobResult stores handler stdout (JSON) on the job before Succeeded (cap 64KB).
    SetJobResult(ctx context.Context, jobID string, result []byte) error

    Close() error
}

// Lock must be Released to unlock the resource.
type Lock interface {
    Release(ctx context.Context) error
}

// JobTicket wraps a dequeued job with its concurrency token.
type JobTicket struct {
    JobID string
    Token string   // used for optimistic ApplyState
}
```



### Why a single interface for both Redis and PostgreSQL?

Both serve the same logical operations. Redis/ValKey use list/sorted-set/set primitives; PostgreSQL uses tables with indexes and SKIP LOCKED. The interface abstracts the difference.

---



## 6. Redis / ValKey Storage

Redis and ValKey are **API-compatible** (as of 2025). They share the same driver (`go-redis/v9` in v1) and implementation module. The only difference is the connection address.

### Data model


| Logical concept       | Redis data structure | Key pattern                                            |
| --------------------- | -------------------- | ------------------------------------------------------ |
| Job payload           | Hash                 | `gfire:job:<id>`                                       |
| Queue (FIFO)          | List                 | `gfire:queue:<name>`                                   |
| Processing jobs       | Hash                 | `gfire:processing` (field=jobID, value=serverID+token) |
| Job state history     | List pushed to       | `gfire:job:<id>:states`                                |
| Scheduled jobs        | Sorted Set           | `gfire:scheduled` (score = unix timestamp)             |
| Recurring definitions | Hash                 | `gfire:recurring`                                      |
| Recurring by schedule | Sorted Set           | `gfire:recurring:<schedule>`                           |
| Continuations         | Set                  | `gfire:continuations:<parentID>`                       |
| Continuation entries  | Hashes               | `gfire:continuation:<parentID>:<childID>`              |
| Server registry       | Hash                 | `gfire:servers` (field=serverID, value=JSON)           |
| Server heartbeat      | Sorted Set           | `gfire:server:heartbeats` (score = unix timestamp)     |
| Counters              | String (INCR)        | `gfire:counter:<key>`                                  |
| Distributed lock      | String (SET NX EX)   | `gfire:lock:<resource>`                                |




### Dequeue flow

1. `BRPOP` from `gfire:queue:<name>` with timeout
2. Fetch job hash, move to `gfire:processing` set
3. Apply "Processing" state via `RPUSH` to `gfire:job:<id>:states`
4. Return `JobTicket`



### Performance notes

- **Blocking pop** eliminates polling — workers sleep until work arrives
- Lua scripts for atomic multi-key operations (dequeue + state transition)
- Pipeline/script for batch scheduled job moves
- Sorted sets for scheduling: `ZRANGEBYSCORE ... WITHSCORES` then `ZREM` — atomic with Lua



### Driver choice — **DECIDED: `go-redis/v9` (v1)**

Band 2 ships `internal/storage/redis/` on **`github.com/redis/go-redis/v9`**. ValKey uses the same client (wire-compatible).

|                         | go-redis (v1)        | rueidis (deferred)   |
| ----------------------- | -------------------- | -------------------- |
| **Status**              | Implemented (v0.3.0) | Post-v1 perf revisit |
| **Lua scripting**       | Yes                  | Yes                  |
| **Sentinel / Cluster**  | Yes                  | Yes                  |
| **Client-side caching** | Manual               | Built-in (RESP3)     |

**Decision:** go-redis/v9 for v1 — simpler, well-known, sufficient for job-queue throughput. Revisit rueidis if Redis becomes a bottleneck (unlikely before high fan-out).

---



## 7. PostgreSQL Storage



### Schema

```sql
-- Core job table (normalized — not just a JSON blob)
CREATE TABLE gfire.jobs (
    id          UUID PRIMARY KEY,           -- UUIDv7
            -- unique queue-ordered identifier for fetch fairness
    queue_token BIGSERIAL NOT NULL,
    name        TEXT NOT NULL,
    args        JSONB NOT NULL DEFAULT '[]',
    queue       TEXT NOT NULL DEFAULT 'default',
    state       TEXT NOT NULL DEFAULT 'Enqueued',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    retry_max   INT NOT NULL DEFAULT 10
);

-- State history (immutable append-only)
CREATE TABLE gfire.job_states (
    id         BIGSERIAL PRIMARY KEY,
    job_id     UUID NOT NULL REFERENCES gfire.jobs(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    reason     TEXT,
    data       JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Scheduled jobs (for delayed + retry)
CREATE TABLE gfire.scheduled_jobs (
    id         UUID PRIMARY KEY,
    job_id     UUID NOT NULL REFERENCES gfire.jobs(id) ON DELETE CASCADE,
    enqueue_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Recurring job definitions
CREATE TABLE gfire.recurring_jobs (
    id         TEXT PRIMARY KEY,
    job_name   TEXT NOT NULL,
    args       JSONB NOT NULL DEFAULT '[]',
    queue      TEXT NOT NULL DEFAULT 'default',
    cron_expr  TEXT NOT NULL,
    last_run   TIMESTAMPTZ,
    next_run   TIMESTAMPTZ,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Continuations
CREATE TABLE gfire.continuations (
    id          BIGSERIAL PRIMARY KEY,
    parent_id   UUID NOT NULL REFERENCES gfire.jobs(id) ON DELETE CASCADE,
    child_name  TEXT NOT NULL,
    child_args  JSONB NOT NULL DEFAULT '[]',
    child_queue TEXT NOT NULL DEFAULT 'default',
    condition   TEXT NOT NULL CHECK (condition IN ('OnSucceeded', 'OnFailed', 'OnAny')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Server registry
CREATE TABLE gfire.servers (
    id            TEXT PRIMARY KEY,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_heartbeat TIMESTAMPTZ NOT NULL DEFAULT now(),
    worker_count  INT NOT NULL,
    queues        TEXT[] NOT NULL DEFAULT '{default}',
    status        TEXT NOT NULL DEFAULT 'active'
);

-- Counters (aggregated stats)
CREATE TABLE gfire.counters (
    key        TEXT PRIMARY KEY,
    value      BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Distributed locks (AcquireLock / Release)
CREATE TABLE gfire.locks (
    resource   TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- Indexes
CREATE INDEX idx_jobs_state_queue        ON gfire.jobs (state, queue) WHERE state = 'Enqueued';
CREATE INDEX idx_jobs_updated_at         ON gfire.jobs (updated_at);
CREATE INDEX idx_job_states_job_id       ON gfire.job_states (job_id, created_at);
CREATE INDEX idx_scheduled_jobs_due      ON gfire.scheduled_jobs (enqueue_at) WHERE enqueue_at <= NOW();
CREATE INDEX idx_continuations_parent    ON gfire.continuations (parent_id);
CREATE INDEX idx_servers_heartbeat       ON gfire.servers (last_heartbeat);
CREATE INDEX idx_locks_expires           ON gfire.locks (expires_at);

-- Schema in 'gfire' namespace to avoid colliding with app tables.
```



### Dequeue flow (SKIP LOCKED)

```sql
-- Atomic fetch: worker grabs next job
WITH fetched AS (
    SELECT id
    FROM gfire.jobs
    WHERE state = 'Enqueued' AND queue IN ('critical', 'default')
    ORDER BY queue_token ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE gfire.jobs j
SET state = 'Processing', updated_at = now()
FROM fetched
WHERE j.id = fetched.id
RETURNING j.id, j.name, j.args, j.queue;
```

On dequeue, the backend also appends a `job_states` row with `name = 'Processing'` and `data = {"server_id": "<node>"}` (default `"local"`; configurable via `NewWithServerID` / `SetServerID`) so orphan recovery can attribute in-flight work to a node.



### Why SKIP LOCKED?

- Skips rows locked by other workers — no blocking, no deadlocks
- Multiple workers can call it concurrently
- `ORDER BY queue_token ASC` ensures fairness (FIFO per queue)
- `queue_token` is a `BIGSERIAL` set on insert — natural ordering



### Notifications (optional, for low-latency dispatch)

```sql
-- Notify workers when a new job is enqueued:
CREATE OR REPLACE FUNCTION gfire.notify_enqueued()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('gfire:jobs', NEW.queue);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_jobs_enqueued
    AFTER INSERT ON gfire.jobs
    FOR EACH ROW
    WHEN (NEW.state = 'Enqueued')
    EXECUTE FUNCTION gfire.notify_enqueued();
```

Workers listen on `gfire:jobs` and wake up their fetch loop instead of polling.

### Connection pooling

- Use `pgx/v5` with `pgxpool`
- One pool per server, configurable min/max connections
- LISTEN/NOTIFY uses an additional dedicated connection per server (or skip if not needed)

---



## 8. Server & Worker Pool



### Configuration

GFire is configured via a YAML file and CLI flags (no Go functional options — it's a standalone binary).

```yaml
# gfire.yaml
server:
  host: "0.0.0.0"
  port: 8080
  workers: 8                     # goroutine pool size (default: 2x CPUs)
  queues:                        # priority-ordered dequeue list
    - "critical"
    - "default"
    - "low"
  queue_limits:                  # optional per-queue concurrency caps (B3-010)
    critical: 2                  # max concurrent Processing jobs on this queue; 0 = no cap
    default: 0
    low: 0
  dequeue_timeout: "5s"          # max block per fetch
  shutdown_timeout: "30s"        # max wait for in-flight jobs on stop
  max_body_size: 10485760        # 10MB max request body (413 if exceeded)
  default_timeout: 30s           # per-job timeout when job.Timeout = 0

heartbeat:
  interval: "5s"
  timeout: "30s"                 # server marked stale after this
  orphan_timeout: "5m"           # re-queue processing jobs after this

scheduler:
  interval: "1s"                 # poll scheduled→enqueued every N
  batch_size: 100

cleanup:
  interval: "1h"
  job_retention: "24h"           # keep terminal jobs before deleting

storage:
  backend: "redis"               # "redis" | "valkey" | "postgres"
  # Redis / ValKey options:
  redis:
    addr: "localhost:6379"
    password: ""
    db: 0
  # PostgreSQL options:
  postgres:
    dsn: "postgres://user:pass@localhost:5432/gfire?sslmode=disable"
    max_conns: 20
    min_conns: 2

logging:
  level: "info"                  # debug | info | warn | error
  format: "json"                 # json | text

metrics:
  enabled: true                  # serve /metrics (Prometheus)

auth:
  enabled: false
  token: ""                      # Bearer token for API endpoints (optional v1)

handlers:
  - name: "send_email"
    cmd: "/usr/local/bin/send-email"   # or use embedded Go handlers
  - name: "cleanup_expired"
    cmd: "/usr/local/bin/cleanup"
```

CLI flags override config file values:

```
gfire server --config gfire.yaml --port 8081 --workers 16
gfire server --backend postgres --pg-dsn "postgres://..."
gfire server --backend redis --redis-addr "10.0.0.5:6379"
```



### Worker goroutine loop

Workers respect `queue_limits` before dequeue: if a queue is at its cap, skip it and try the next queue in priority order.

On success, if handler stdout is valid JSON and ≤64KB, persist it as `Job.Result` (B3-011).

Cancel: when cancel is requested for a `Processing` job, the engine cancels the job context, sends **SIGTERM** to the handler subprocess, then transitions to `Cancelled` (B3-009).

```go
func (w *worker) run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return  // graceful shutdown
        default:
            ticket, err := w.storage.Dequeue(ctx, w.config.Queues, w.config.DequeueTimeout)
            if err != nil {
                continue  // timeout or no job
            }

            job, _ := w.storage.GetJob(ctx, ticket.JobID)

            // Execute middleware chain
            result := w.pipeline.Execute(w.newJobContext(job))

            finalState := "Succeeded"
            if result.Error != nil {
                finalState = "Failed"
            }
            w.storage.ApplyState(ctx, ticket.JobID, "Processing", &JobState{
                Name: finalState,
                Data: map[string]string{"duration_ms": fmt.Sprintf("%d", result.Duration.Milliseconds())},
            })

            // Fire continuations if terminal state matches
            if terminalStates[finalState] {
                w.fireContinuations(ctx, job.ID, finalState)
            }
        }
    }
}
```



### Graceful shutdown sequence

1. `SIGINT` / `SIGTERM` received → `Server.Stop(ctx)` called
2. Stop accepting new jobs (cancel background goroutines)
3. Drain in-flight workers (wait up to `ShutdownTimeout`)
4. For each in-flight job: complete if possible, otherwise re-queue
5. Unregister server from registry
6. Return from `Stop()`

---



## 9. Recurring Jobs (Cron)



### API (REST)

Recurring jobs are managed via the HTTP API, not Go code.

```
POST   /v1/recurring
       {"id": "nightly-cleanup", "cron": "0 2 * * *", "queue": "default", "name": "cleanup_expired", "args": {...}}

DELETE /v1/recurring/{id}

POST   /v1/recurring/{id}/trigger    → fire immediately, outside schedule

GET    /v1/recurring                 → list all recurring definitions
```



### Internal implementation

- Uses `robfig/cron/v3` internally
- On server start: load all recurring entries from storage → register with cron
- Cron tick: acquire distributed lock `gfire:lock:recurring:<id>` — only the winning node fires
- On fire: enqueue a new `Job`, update `last_run` and `next_run` in storage
- Entries survive server restarts (persisted in storage)



### Why distributed lock on fire?

Prevents duplicate execution in multi-node deployments. Only the node holding the lock executes the tick. If that node dies, the lock expires and another node takes over.

---



## 10. Delayed & Scheduled Jobs



### API (REST)

```
POST /v1/jobs/schedule
     {"name": "send_reminder", "args": {"user_id": 42}, "queue": "default", "enqueue_at": "2026-07-10T15:00:00Z"}
```

The `enqueue_at` field supports ISO 8601 (absolute time). For relative delays, the client calculates the absolute time before sending. GFire does not expose a "delay seconds" field — the client is responsible for computing `now + delay`.

### How it works


| Backend          | Mechanism                                                                                                                                                       |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Redis/ValKey** | `ZADD gfire:scheduled <unix_ts> <jobID>` — scheduler goroutine calls `ZRANGEBYSCORE ... 0 <now> WITHSCORES`, then `ZREM` each, then `LPUSH` to queue            |
| **PostgreSQL**   | `INSERT INTO gfire.scheduled_jobs` — scheduler goroutine calls `SELECT ... FOR UPDATE SKIP LOCKED WHERE enqueue_at <= NOW()`, then UPDATE job state to Enqueued |




### Scheduler goroutine

- Runs every `SchedulerInterval` (default 1 second)
- Fetches up to `batchSize` (default 100) due jobs
- For each: apply "Enqueued" state, insert into target queue
- Atomic: storage-level conditional to prevent double-enqueue (storage handles this)

---



## 11. Job Continuations (Chaining)



### API

```go
parentID := client.Enqueue(ctx, "order_process", args)

// Run after parent succeeds (any terminal state matching)
childID := client.ContinueWith(ctx, parentID, "send_confirmation", args, ConditionOnSucceeded)

// Run on failure
client.ContinueWith(ctx, parentID, "rollback_inventory", args, ConditionOnFailed)

// Run unconditionally
client.ContinueWith(ctx, parentID, "audit_log", args, ConditionOnAny)
```



### Data model

```go
type ContinuationEntry struct {
    ChildName string // job handler name
    ChildArgs []byte
    ChildQueue string
    Condition string // "OnSucceeded" | "OnFailed" | "OnAny"
    CreatedAt time.Time
}

type Condition string

const (
    ConditionOnSucceeded Condition = "OnSucceeded"
    ConditionOnFailed    Condition = "OnFailed"
    ConditionOnAny       Condition = "OnAny"
)
```



### Flow

1. `client.ContinueWith(...)` stores a `ContinuationEntry` in storage (Set or table)
2. When a worker finishes a job with a terminal state, it checks for continuations
3. Filters by condition: `OnSucceeded` → match only Succeeded; `OnFailed` → match only Failed; `OnAny` → always
4. For each matching continuation: enqueue child job (normal `Enqueue` call)
5. Child runs like any other job — it's a regular job, just enqueued by continuation logic

**Parent result (planned Band 4):** when the parent has a stored `Result` (B3-011), continuation dispatch may merge it into the child args (e.g. under `_parent_result`) so chains pass data, not just triggers.



### Important: continuation dispatch is **at-least-once**

If the worker crashes between enqueuing the child and recording the fact, the continuation may fire again on orphan recovery. Child jobs should be idempotent (same as any job in an at-least-once system).

**Scope:** Continuations cover simple parent → child chains. Multi-step DAGs with parallel joins, fan-out, and completion barriers are [Pipelines (§12)](#12-pipelines-dag-orchestration) — post-v1.0.0.

---



## 12. Pipelines (DAG orchestration)

> **Status:** Planned — Band 8 (post-v1.0.0). Not part of the v1.0.0 contract.

GFire v1 ships a job queue with continuations. **Pipelines** add first-class DAG orchestration: parallel steps, `all_of` joins, fan-out to N branches, and run-level completion only when configured barriers are satisfied.

**Wedge vs Airflow/Dagster:** headless service, HTTP/curl triggers, external `cmd` handlers in any language, declarative YAML checked into git, join/barrier logic in the engine (not DIY counters in app databases). GFire does not ship a visual editor or a Python operator catalog in the core binary.

### Design case: ETL with join and fan-out

```
extract_pg_a ──┐
               ├──► pivot ──► load_dest_1 ──┐
extract_pg_b ──┘              load_dest_2 ──├──► pipeline run Succeeded
                              load_dest_N ──┘
```

Heavy data stays in staging tables or object storage; pipeline args carry `pipeline_run_id`, step id, and references (~1 KB instruction cards).

### Pipeline definition (YAML)

Definitions live on disk (operator-managed) or are registered via API. Versioned in git alongside application code.

```yaml
name: etl-daily
version: 1
steps:
  - id: extract_a
    handler: extract_pg
    args: { source: db_a, table: orders }
  - id: extract_b
    handler: extract_pg
    args: { source: db_b, table: customers }
  - id: pivot
    handler: etl_pivot
    needs: [extract_a, extract_b]
  - id: load
    handler: load_dest
    needs: [pivot]
    fan_out:
      - { dest: warehouse_1 }
      - { dest: warehouse_2 }
      - { dest: warehouse_n }
    on_all_success: true   # run completes only when every fan_out branch Succeeded
```

| Field | Meaning |
| ----- | ------- |
| `needs` | Step runs when **all** listed steps reached `Succeeded` |
| `fan_out` | Engine enqueues one job per branch; args merged with branch payload |
| `on_all_success` | Pipeline run → `Succeeded` only when every fan-out branch succeeds |
| `handler` | Maps to configured `cmd` handler name (same as single jobs) |

Failure policy (default): first terminal `Failed` step marks the pipeline run `Failed`; optional `continue_on_failure` per step is a post-Band-8 enhancement.

### Pipeline run lifecycle

```
Pending → Running → Succeeded | Failed | Cancelled
```

Each **step** within a run tracks: `Pending` | `Running` | `Succeeded` | `Failed` | `Skipped`.

```mermaid
stateDiagram-v2
  [*] --> Pending
  Pending --> Running: start run
  Running --> Succeeded: all steps + barriers OK
  Running --> Failed: step failed (default policy)
  Running --> Cancelled: cancel API
  Succeeded --> [*]
  Failed --> [*]
  Cancelled --> [*]
```

### Engine integration

1. `POST /v1/pipelines/{name}/runs` creates a `PipelineRun` and enqueues all steps with no `needs` (roots).
2. When a step's backing **job** reaches a terminal state, the pipeline engine:
   - Records step outcome on the run.
   - Evaluates which steps have `needs` ⊆ `Succeeded` → enqueues their jobs.
   - For `fan_out`, enqueues N jobs and tracks branch completion.
   - When barrier conditions pass → marks run `Succeeded`.
3. Step jobs carry metadata linking `pipeline_run_id` and `step_id` (env or args) for observability and idempotency.

Continuations (§11) remain for ad-hoc chains; pipelines use the dedicated engine path to avoid double-firing.

### REST API (planned)

```
POST   /v1/pipelines/{name}/runs     → 201 {run_id, status}
GET    /v1/pipeline-runs/{id}        → run + per-step status
GET    /v1/pipeline-runs             → list (filter by status, pipeline name)
POST   /v1/pipeline-runs/{id}/cancel → cancel in-flight steps
```

CLI mirrors: `gfire pipeline run etl-daily`, `gfire pipeline run get <id>`.

### Storage additions (all backends)

| Entity | Purpose |
| ------ | ------- |
| `pipeline_definitions` | Name, version, parsed DAG (or hash + path) |
| `pipeline_runs` | Run id, pipeline name, status, timestamps |
| `pipeline_step_runs` | Step id, linked job id(s), status, fan_out index |

PostgreSQL: normalized tables under `gfire` schema. Redis/ValKey: keyed structures with the same semantics.

### Delivery guarantees

Same as jobs: **at-least-once**. Handlers must be idempotent. Join/barrier state is updated atomically in storage; duplicate step completion events must not double-enqueue downstream steps (conditional updates).

### Relationship to other tools

| Tool | GFire Pipelines |
| ---- | --------------- |
| **Airflow / Dagster** | Rich UI, Python operators, lineage — GFire targets headless + language-agnostic handlers |
| **Temporal** | Durable long-running workflows / sagas — different category; GFire stays instruction-card + subprocess |
| **Continuations (§11)** | Single parent → N children on terminal state; no join or run-level barrier |

---



## 13. Filters / Middleware Pipeline

Go closure chain, analogous to `net/http` or `gin` middleware.

### Interface

```go
// MiddlewareFunc wraps a job execution. Call next(ctx) to proceed.
type MiddlewareFunc func(ctx *JobContext, next func(*JobContext) error) error
```



### Built-in order

```
PanicRecovery (outermost)
    └── Retry
        └── StateMachine (apply Processing, final state)
            └── Observability (Prometheus metrics + slog)
                └── Logging
                    └── JobHandler (innermost, user's function)
```



### JobContext

```go
type JobContext struct {
    context.Context
    Job        *Job
    Attempt    int
    StartedAt  time.Time
    Logger     *slog.Logger
    Storage    Storage
    Items      map[string]any   // middleware-to-middleware data sharing
    setFinal   func(state string, reason string)
}
```



### Custom middleware example

```go
// Auth middleware — ensure job has a valid tenant
func AuthMiddleware(validTenants []string) gfire.MiddlewareFunc {
    return func(ctx *gfire.JobContext, next func(*gfire.JobContext) error) error {
        tenant := ctx.Job.Metadata("tenant")
        if !slices.Contains(validTenants, tenant) {
            return fmt.Errorf("unauthorized tenant: %s", tenant)
        }
        return next(ctx)
    }
}
```

---



## 14. Retry & Failure Handling



### Default retry

- Max attempts: 10
- Delay: exponential backoff with jitter
  - Formula: `min(1 minute * 2^attempt + rand(0, 1000ms), 1 hour)`
- After final failure: job transitions to **`Dead`** state (poison / DLQ, B3-012) — not retryable until manual requeue
- While retries remain: `Failed` → `Scheduled` → `Enqueued`



### Configuration

```go
type RetryConfig struct {
    MaxAttempts int
    DelayFunc   func(attempt int) time.Duration
    Retryable   func(err error) bool  // predicate: should this error trigger retry?
}

// Built-in delay strategies:
var ExponentialBackoff RetryConfig = ...
var FixedDelay(time.Duration) RetryConfig = ...
```



### Manual retry (API)

```
POST /v1/jobs/{id}/requeue
     {"reason": "manual retry after fixing data"}
```

Works for `Failed` and **`Dead`** jobs. Resets the retry counter and moves the job back to `Enqueued` state.

### Cancel in-flight (B5-011 / B3-009) ✅

```
POST /v1/jobs/{id}/cancel
     {"reason": "operator abort — bad deploy"}
```

Only valid when current state is `Processing`. Engine sends SIGTERM to handler subprocess; terminal state is `Cancelled` (not `Failed`, no retry).

CLI equivalent: `gfire job cancel <id>` (Band 6, B6-010).

### Job-level override

Jobs can specify `RetryMax` at enqueue time. 0 = no retries, -1 = infinite, N = max N attempts.

---



## 15. Distributed Coordination



### Server heartbeat protocol

1. On start: `RegisterServer(storage)` — insert server info with TTL
2. Every `HeartbeatInterval`: `Heartbeat(storage)` — update `last_heartbeat`
3. On stop: `UnregisterServer(storage)` — remove entry



### Orphan recovery (`Coordinator` goroutine)

Two mechanisms (server-level and job-level):

1. **Server stale:** every `HeartbeatTimeout / 2`, query `GetServers()`. If `now - last_heartbeat > HeartbeatTimeout` → mark server stale. After `OrphanTimeout`, re-queue all `Processing` jobs for that `server_id` via `Requeue`.
2. **Job progress stale (long jobs):** workers call `HeartbeatJob(jobID)` every ~60s while a job runs. Coordinator calls `GetOrphanedJobs(staleAge)` to find `Processing` jobs with no recent progress and re-queue them (covers worker crash without waiting for full server orphan timeout).

Steps for server orphan path:

1. Every `HeartbeatTimeout / 2`: query `GetServers()`
2. If `now - last_heartbeat > HeartbeatTimeout` → mark server as "stale"
3. If server is stale for > `OrphanTimeout`:
  - Find all jobs with `state = "Processing"` and `server_id = stale_server`
  - For each: `Requeue(jobID, "orphaned from server <ID>")`
  - Remove stale server from registry



### Distributed locks


| Backend          | Mechanism                                                                                                                                                                                              |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Redis/ValKey** | `SET resource UUID NX EX ttl` — lock is the key; UUID ensures only the locker can release via Lua: `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end` |
| **PostgreSQL**   | Table `gfire.locks` (`resource` PK, `owner_id`, `expires_at`). `AcquireLock` deletes expired rows for the resource, then `INSERT ... ON CONFLICT DO NOTHING`. `Release` deletes by `resource` + `owner_id`. Stale locks expire via `expires_at` (not session-scoped advisory locks). |




### What gets locked


| Resource          | Why                                                                                    |
| ----------------- | -------------------------------------------------------------------------------------- |
| `recurring:<id>`  | Only one node fires each recurring job tick                                            |
| `scheduler:run`   | Only one node runs the scheduled→enqueued poller (optional — safe to run concurrently) |
| `cleanup:run`     | Only one node runs expired-job cleanup                                                 |
| `stats:aggregate` | Only one node rolls up counters                                                        |


Note: Queue dequeue does **not** use a distributed lock — queue atomicity (SKIP LOCKED or BRPOPLPUSH) provides it at the storage level.

---



## 16. Observability & Ops Surface



### v1: headless

GFire v1 ships **no embedded UI**. Monitoring and ops:


| Tool       | How                                                                        |
| ---------- | -------------------------------------------------------------------------- |
| CLI        | `gfire job list --state failed`, `gfire job list --state dead`, `gfire job cancel <id>`, `gfire queue list`, `gfire server status` |
| REST       | `GET /v1/jobs`, `/v1/queues`, `/v1/servers`, `/v1/recurring`, `GET /openapi.json` |
| Prometheus | `GET /metrics` (always on when `metrics.enabled`)                          |
| Grafana    | External dashboards scrape `/metrics`                                      |




### Post-v1: GFireUI

Separate React project. Talks only to the public REST API. Not part of the `gfire` binary.

### Prometheus metrics (v1)

```
gfire_jobs_enqueued_total{queue}
gfire_jobs_succeeded_total{queue}
gfire_jobs_failed_total{queue}
gfire_jobs_dead_total{queue}              # poison / DLQ — planned B6-011
gfire_jobs_duration_seconds{queue,name}   # histogram
gfire_workers_active{server_id}
gfire_queue_depth{queue}
gfire_servers_active
gfire_servers_stale
```



### Logging (v1)

Structured `slog` (JSON or text). Request IDs on API. Job ID on every worker log line.

### OpenTelemetry (post-v1)

Per-job traces and OTel metrics are a post-v1 enhancement. v1 stays on Prometheus + slog only.

---



## 17. Package Layout

Standalone service — **not** a Go import library. All application code lives under `internal/` (compiler-enforced; external modules cannot import it).

```
gfire/
├── cmd/
│   └── gfire/
│       └── main.go                # Binary entry point (thin: flags + wire + Run)
│
├── internal/
│   ├── job/                       # Job, JobState, ServerInfo, Lock, JobTicket, continuations
│   │   ├── job.go
│   │   ├── types.go
│   │   └── recurring.go
│   │
│   ├── storage/
│   │   ├── storage.go             # Storage interface
│   │   ├── errors/                # Sentinel errors (package errors)
│   │   ├── memory/                # In-memory backend (tests/dev)
│   │   ├── postgres/              # PostgreSQL backend + migrations/
│   │   └── redis/                 # Redis/ValKey backend (Band 2)
│   │
│   ├── engine/                    # Worker pool, scheduler, coordinator (Band 3–4); pipeline runner (Band 8)
│   ├── pipeline/                  # YAML DAG definitions, run state machine (Band 8)
│   ├── api/                       # HTTP handlers (Band 5)
│   ├── config/                    # YAML + defaults (Band 6)
│   ├── handler/                   # Subprocess handler registry
│   └── middleware/                # Job execution middleware pipeline
│
├── go.mod
├── Makefile
├── docker-compose.yml
└── README.md

# Post-v1 (separate repo): GFireUI — React dashboard against REST API
# Post-v1 optional: pkg/client/ — Go SDK for enqueue (only if needed)
```

> **Convention:** no `.go` domain packages at repo root. `cmd/` is the only public entry point.



### Key differences from an embedded library


| Old (embedded library)                             | New (standalone service)                   |
| -------------------------------------------------- | ------------------------------------------ |
| `gfire.go` — exported client API                   | `cmd/gfire/main.go` — binary entry point   |
| `client.go` — Enqueue, Schedule, etc.              | `api/handlers_jobs.go` — HTTP handlers     |
| Functional options (`WithWorkers(n)`)              | YAML config + CLI flags                    |
| Imported as `go get github.com/hrodrig/gfire`      | Deployed as binary / Docker image          |
| `Server.Start()` called from user code             | `gfire server` CLI command                 |
| Handlers registered via `gfire.Register(name, fn)` | Handlers configured in YAML or compiled in |




### Dependencies (targeted)


| Package                                     | Purpose                                                     |
| ------------------------------------------- | ----------------------------------------------------------- |
| `spf13/cobra`                               | CLI framework (subcommands: `server`, `migrate`, `version`) |
| `spf13/viper`                               | Config loading (YAML + env vars + CLI flags)                |
| `google/uuid`                               | UUIDv7 job IDs                                              |
| `robfig/cron/v3`                            | Cron scheduling for recurring jobs                          |
| `go-redis/v9`                               | Redis/ValKey driver (v1 — rueidis deferred)                 |
| `jackc/pgx/v5`                              | PostgreSQL driver                                           |
| `golang-migrate/migrate` or `pressly/goose` | PG schema migrations                                        |
| `prometheus/client_golang`                  | Prometheus `/metrics`                                       |
| stdlib `net/http`                           | HTTP router (Go 1.22+)                                      |


Post-v1 optional: `go.opentelemetry.io/otel` for traces.

---



## 18. Open Questions



### Design decisions to resolve

1. **Job handler model** — **DECIDED:** `cmd` **(subprocess).** GFire executes a command per job, passing args via stdin/JSON. Language-agnostic. Compiled Go handlers (custom `main.go`) can be added later as a power-user feature.
2. **Go module name** — **DECIDED:** `github.com/hrodrig/gfire`
3. **HTTP router** — **DECIDED:** `net/http` **(stdlib).** Go 1.22+ routing is sufficient for the API surface.
4. **Config format** — **DECIDED: YAML**
5. **Delivery guarantees** — **DECIDED: at-least-once.** Jobs may re-run on crash. Handlers must be idempotent.
6. **Serialization format** — **DECIDED: JSON** for HTTP API and storage payloads (human-readable, language-agnostic).
7. **Job identity** — **DECIDED: UUIDv7** (time-sortable, index-friendly).
8. **Redis vs ValKey** — **DECIDED: share** one impl under `internal/storage/redis/` (`backend: redis|valkey` in config).
9. **UI** — **DECIDED: headless v1.** No embedded dashboard. GFireUI (React, separate repo) post-v1 against REST API.
10. **Authentication** — **DECIDED: optional Bearer token** in config (`auth.enabled` / `auth.token`). Off by default for local/dev.
11. **gRPC** — **DECIDED: REST only for v1.** Revisit if a concrete use case demands it.
12. **Pipelines / DAG** — **DECIDED: first-class Pipelines post-v1.0.0 (Band 8).** v1 ships job queue + continuations only. Pipelines add `needs`, `fan_out`, and `on_all_success` barriers in the engine. Headless YAML + HTTP; no visual DAG editor in core. Design case #1: multi-source ETL → pivot → N destinations.
13. **Bulk enqueue** — **DECIDED: `POST /v1/jobs/enqueue/batch` (B5-009).** Thousands of ~1 KB instruction cards per request; partial accept/reject. SAP/ETL card flood without N HTTP round-trips.
14. **Idempotency-Key** — **DECIDED: optional header on enqueue/batch (B5-010).** Same key → same `job_id`; safe client retries under at-least-once API.
15. **Cancel in-flight** — **DECIDED: `POST /v1/jobs/{id}/cancel` + engine SIGTERM (B3-009, B5-011).** Terminal state `Cancelled`; no automatic retry.
16. **Dead / DLQ state** — **DECIDED: `Dead` after `retry_max` exhausted (B3-012).** Distinct from retryable `Failed`; filter via `--state dead` and `gfire_jobs_dead_total`.
17. **Job result capture** — **DECIDED: handler stdout JSON → `Job.Result`, cap 64KB (B3-011).** Continuations may inject parent result into child args (Band 4).
18. **Per-queue concurrency cap** — **DECIDED: `server.queue_limits` map (B3-010).** e.g. `critical: 2` limits parallel SAP extracts; `0` = no cap beyond `server.workers`.
19. **OpenAPI** — **DECIDED: `GET /openapi.json` OpenAPI 3 spec (B5-013).** Curl-first API; generic clients without hand-written SDKs.



### Implementation sequencing

See `ROADMAP.md` (8 bands → v1.0.0). This table is obsolete; bands there are source of truth.

---

*Living document — update as decisions are made.*