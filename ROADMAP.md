# GFire Roadmap — v1.0.0

**Start:** July 2026
**Target:** September 2026
**Cadence:** Weekly bands. Each band produces a working, testable increment.

---

## Philosophy

- **GFire is a headless service** — a standalone binary that runs REST API + worker pool.
- **CLI** for server management + quick inspection (`gfire job list --state failed`).
- **Prometheus metrics** for production monitoring (Grafana dashboards).
- **Full web dashboard** comes after v1 as a separate React project (GFireUI) that talks to the same REST API.
- Each band is **shippable independently**. Band 3 alone gives you a working job engine.
- **Product wedge (v1):** PostgreSQL (or Redis) + HTTP/curl + continuations — not Faktory/BullMQ throughput. Jobs are ~1KB instruction cards; heavy data stays in handlers (S3, DB).

---



## Scaling & HA (no Raft needed)



### Architecture

```
         ┌──────────┐   ┌──────────┐   ┌──────────┐
         │  GFire   │   │  GFire   │   │  GFire   │
         │  Pod A   │   │  Pod B   │   │  Pod C   │
         │ workers  │   │ workers  │   │ workers  │
         └────┬─────┘   └────┬─────┘   └────┬─────┘
              │              │              │
              └──────────────┼──────────────┘
                             │ all read/write same storage
                             ▼
              ┌──────────────────────────────┐
              │     Shared Storage           │
              │  Redis / ValKey / PostgreSQL │
              │  (managed, outside cluster)  │
              └──────────────────────────────┘
```



### How scaling works (no consensus needed)

GFire uses **shared-state coordination**, not leader election. All nodes are peers.


| Operation             | Mechanism                                                                        | Why Raft is NOT needed                                                      |
| --------------------- | -------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| **Dequeue**           | `SKIP LOCKED` (PG) or `BRPOPLPUSH` (Redis)                                       | Storage guarantees atomicity — two workers never get the same job           |
| **State transition**  | Conditional `UPDATE ... WHERE state = 'Processing'`                              | Optimistic locking. If the job moved, the transition fails, worker moves on |
| **Graceful shutdown** | `SIGTERM` → drain workers → re-queue in-flight jobs → unregister                 | The node cleans up after itself before exiting                              |
| **Crash recovery**    | Coordinator detects stale heartbeat → re-queues "Processing" jobs from dead node | At-least-once semantics. Jobs may run twice on crash, but never lost        |
| **Recurring cron**    | Distributed lock on `lock:recurring:<id>`                                        | Only one node fires each tick. Lock TTL handles node death                  |
| **Scheduler**         | Runs on every node (idempotent via atomic conditional update)                    | Multiple nodes can pull the same scheduled job — first write wins           |
| **Scaling up**        | New pod registers heartbeat → starts dequeuing                                   | Storage handles the balancing naturally — more consumers = more throughput  |
| **Scaling down**      | `SIGTERM` → pod drains → unregisters                                             | Other pods continue working unaffected                                      |




### What happens on crash (step by step)

```
1. Pod C dies (OOM, node failure, network partition)
2. Pod A's Coordinator goroutine checks heartbeat table
3. Sees Pod C's heartbeat is 35s stale (> HeartbeatTimeout of 30s)
4. Marks Pod C as "stale"
5. After OrphanTimeout (5m), finds all jobs with state=Processing AND server_id=PodC
6. For each: ApplyState(jobID, "Processing", {Name: "Enqueued", Reason: "orphaned from PodC"})
7. Those jobs are now back in the queue, available for Pod A or B
```



### What happens on deploy (rolling update)

```
1. Kubernetes sends SIGTERM to Pod A (old version)
2. GFire's signal handler catches it
3. Worker pool stops accepting new jobs (context canceled)
4. In-flight workers have ShutdownTimeout (30s) to finish
5. Workers that don't finish: their jobs are re-queued (ApplyState → Enqueued)
6. Pod A unregisters from server registry
7. Exit code 0
8. Meanwhile Pod D (new version) starts, registers, begins dequeuing
9. Zero job loss
```



### Multi-AZ / multi-region considerations

- **Redis/ValKey**: Cross-AZ replication. If the primary fails, jobs in-flight may be lost (not persisted).
- **PostgreSQL**: Synchronous replication + failover. Jobs are ACID. Recommended for HA.

---



## Legend


| Icon | Meaning                                  |
| ---- | ---------------------------------------- |
| ✅    | Done                                     |
| ▶️   | In progress                              |
| ⬜    | Not started                              |
| 🔑   | **Milestone** (tagged release candidate) |


---



## Band 0 — Foundation (Week 1) ✅

> Core types, Storage interface, In-memory backend.


| Deliverable                                                                 | Status |
| --------------------------------------------------------------------------- | ------ |
| `internal/job/` — Job, JobState, ServerInfo, Lock, JobTicket, continuations | ✅      |
| `internal/storage/storage.go` — Full interface (31 methods; 29 at v0.1.0)   | ✅      |
| `internal/storage/errors/` — Sentinel errors                                | ✅      |
| `internal/storage/memory/` — Thread-safe in-memory backend                  | ✅      |
| `internal/storage/memory/memory_test.go` — 13 tests, all passing            | ✅      |
| `cmd/gfire/main.go` — Binary entry point (stub)                             | ✅      |
| `go.mod` — Module `github.com/hrodrig/gfire`                                | ✅      |
| `SPECIFICATIONS.md` — Live architecture document                            | ✅      |
| `ROADMAP.md` — This document                                                | ✅      |


**🔑 v0.1.0** — "Storage interface is stable, memory backend works."

---



## Band 1 — PostgreSQL (Week 2–3) ✅

> Production-ready PostgreSQL backend with migrations.


| Deliverable                                         | Status     |
| --------------------------------------------------- | ---------- |
| Schema design (`gfire` namespace, jobs + locks + …) | ✅          |
| Migration files (`golang-migrate`)                  | ✅          |
| `internal/storage/postgres/` — full Storage impl    | ✅          |
| SKIP LOCKED dequeue                                 | ✅          |
| LISTEN/NOTIFY (optional)                            | ⬜ deferred |
| Integration tests (docker-compose PG)               | ✅ 5 tests  |
| `docker-compose.yml` — postgres (+ redis/valkey)    | ✅          |


**Key decisions:**

- Use `pgx/v5` with connection pooling
- `golang-migrate/migrate` for schema versioning
- LISTEN/NOTIFY deferred (poll-based Dequeue is enough for v0.2)

**🔑 v0.2.0** — "PostgreSQL backend works. Can run gfire against real PG."

---



## Band 2 — Redis / ValKey (Week 3–4) ✅

> In-memory queue backend for high-throughput deployments.


| Deliverable                                             | Status    |
| ------------------------------------------------------- | --------- |
| `internal/storage/redis/storage.go` — full Storage impl | ✅         |
| Lua scripts for atomic multi-key operations             | ✅         |
| Integration tests (requires Redis, docker-compose)      | ✅ 5 tests |
| `docker-compose.yml` — gfire + redis dev env            | ✅         |


**Key decisions:**

- Share implementation for Redis and ValKey (API-compatible; connect via address)
- **`go-redis/v9`** driver (rueidis deferred — can revisit for perf)
- BRPOP for blocking dequeue
- Sorted sets for scheduling
- Lua scripts for atomic dequeue + state transition

**🔑 v0.3.0** — "All three backends work. Can swap via config."

---



## Band 3 — Engine: Worker Pool + Middleware (Week 4–5) ✅

> The core job processing loop. Workers, dequeue, retry, panic recovery.
> **Per-job timeout and job-level heartbeat** (for 4-hour SAP extractions).


| Deliverable                                                                      | Status |
| -------------------------------------------------------------------------------- | ------ |
| `internal/engine/engine.go` — Engine struct, Start/Stop lifecycle                | ✅      |
| `internal/engine/worker.go` — Worker goroutine: fetch → middleware → execute     | ✅      |
| Per-job timeout: kill subprocess if job exceeds `job.Timeout`                    | ✅      |
| Per-job heartbeat ticker: worker updates `HeartbeatJob` every 60s while job runs | ✅      |
| Graceful shutdown: context cancel → drain workers                                | ✅      |
| `internal/middleware/` — MiddlewareFunc, Pipeline, PanicRecovery                 | ✅      |
| `internal/handler/` — subprocess runner + Func for tests                         | ✅      |
| Retry with exponential backoff + jitter → `ScheduleRetry`                        | ✅      |
| Integration test: in-memory engine, 100 jobs                                     | ✅      |
| **B3-009** In-flight cancel — cancel context → `Cancelled`                       | ✅      |
| **B3-010** Per-queue concurrency cap — `server.queue_limits`                     | ✅      |
| **B3-011** Job result capture — stdout → `SetJobResult` (cap 64KB)               | ✅      |
| **B3-012** Dead (DLQ) state — after `retry_max` exhausted                        | ✅      |
| `ScheduleRetry` + `SetJobResult` on Storage interface (31 methods total)         | ✅      |
| SIGINT/SIGTERM signal wiring in `cmd/gfire server`                               | ⬜ Band 6 |


**Handler model note:** one subprocess per job adds ~100–500ms startup. Fine for minute/hour jobs (SAP ETL); poor for sub-50ms micro-tasks. Long-lived handler pool is post-v1 (see deferred list below).

**Architecture:**

```
Engine.Start()
  ├── spawn N workers (goroutines)
  ├── spawn scheduler goroutine
  ├── spawn coordinator goroutine (heartbeat + orphan recovery)
  └── signal handler (SIGINT/SIGTERM → Stop)
**Worker.run() (with heartbeat + timeout):**
```

loop:
  ticket = Dequeue()
  if timeout: context, cancel = context.WithTimeout(ctx, job.Timeout || config.DefaultTimeout)

  go heartbeatTicker(ctx, jobID):   ← separate goroutine
    tick every 60s:
      if ctx is done → return
      HeartbeatJob(jobID)

  result = middleware.Execute(ctx, job)
  cancel()   // stops ticker + kills subprocess if still running

  if result.Error:
    ApplyState(Processing → Failed, reason: result.Error)
    if retry_remaining > 0:
      AddScheduled(now + backoff(attempt))
  else:
    ApplyState(Processing → Succeeded)

  fire_continuations_if_needed(jobID, finalState)

```
Engine.Stop():
  cancel ctx (no more dequeues)
  wait for in-flight workers (ShutdownTimeout)
  re-queue any Processing jobs → Enqueued
  unregister from server registry
  return
```

**🔑 v0.4.0** — "Engine processes jobs with retry, cancel, and DLQ." ✅

---



## Band 4 — Scheduler + Recurring + Continuations (Week 5–6) — partial ✅

> Time-based and conditional job execution. **v0.6.0 ships continuations + coordinator; recurring cron deferred to v0.5.0 tag.**


| Deliverable                                                                | Status |
| -------------------------------------------------------------------------- | ------ |
| Scheduled → Enqueued poller (engine `schedulerLoop`)                       | ✅      |
| `engine/coordinator.go` — server heartbeat + orphan job requeue              | ✅      |
| `engine/continuations.go` — fire child jobs + parent result merge            | ✅      |
| `engine/coordinator.go` — cleanup via `RemoveExpired`                        | ✅      |
| `recurring.go` — RecurringManager (robfig/cron + distributed lock) | ✅ |
| Stale server registry sweep (full orphan server path) | ✅ |


**🔑 v0.5.0** — "Recurring cron + full coordinator." ✅

---



## Band 5 — REST API (Week 6–7) — core ✅

> HTTP API so applications can talk to GFire. **v0.6.0 = curl-usable core; B5-009, B5-010, B5-013 deferred.**


| Deliverable                                                | Status |
| ---------------------------------------------------------- | ------ |
| `internal/api/` — routes, JSON errors, max body, Bearer auth | ✅   |
| Jobs: enqueue, schedule, get, list, requeue, cancel, continue | ✅  |
| **B5-011** cancel in-flight                                | ✅      |
| **B5-012** Bearer auth (`auth.enabled`)                    | ✅      |
| Queues + servers + healthz/readyz                          | ✅      |
| **B5-014** job delete (`POST /v1/jobs/{id}/delete`) | ✅ |
| Recurring CRUD handlers | ✅ |
| **B5-009** bulk enqueue                                    | ⬜      |
| **B5-010** Idempotency-Key                                 | ⬜      |
| **B5-013** OpenAPI                                         | ⬜      |


**🔑 v0.6.0** — "Enqueue and inspect jobs via curl." ✅

---



## Band 6 — CLI + Monitoring (Week 7) — core ✅

> CLI for server management. **v0.6.0 ships server + job inspect; metrics/migrate deferred.**


| Deliverable                                                     | Status |
| --------------------------------------------------------------- | ------ |
| Cobra CLI (`internal/cli`)                                      | ✅      |
| `gfire server` — engine + API + SIGTERM shutdown                  | ✅      |
| `gfire job list/get/requeue`                                    | ✅      |
| `gfire migrate`, `gfire queue list`, `gfire server status`      | ⬜      |
| Prometheus `GET /metrics`                                       | ⬜      |
| **B6-009–011** dead filter, CLI cancel, dead metric             | ⬜      |


**🔑 v0.6.0 CLI milestone** — `gfire server` + job inspect ✅

**Route table (implemented in v0.6.0 unless noted):**

```
POST   /v1/jobs/enqueue, /schedule, GET /v1/jobs, GET /v1/jobs/{id}
POST   /v1/jobs/{id}/requeue, /cancel, /continue
GET    /v1/queues, /v1/queues/{name}, GET /v1/servers
GET    /healthz, /readyz
POST   /v1/jobs/enqueue/batch, GET /openapi.json, /v1/recurring/*  → planned
POST   /v1/jobs/{id}/delete                                         → planned (B5-014)
GET    /metrics                                                   → planned
```

**Band 4 notes (recurring — planned v0.5.0):** robfig/cron + lock `recurring:<id>`; scheduler poll already in engine.

**Band 6 config reference:** see `gfire.example.yaml` and SPEC §8.

---



## Band 7 — Release: Polish, Docker, Docs (Week 8)

> Production readiness. **MiniMax audit (July 2026): B7-001–006 use problem → fix wording.**


| Deliverable | Est. effort | Status |
| ----------- | ----------- | ------ |
| **B7-001** Plain `!=` on Bearer token → timing side-channel when `auth.enabled` → `crypto/subtle.ConstantTimeCompare` (Band 5) | — | ✅ |
| **B7-002** `promoteScheduled` loop looked like engine promoted jobs but did nothing → storage already promotes in `GetDueScheduled` → rename `tickScheduler` + document (Band 4) | — | ✅ |
| **B7-003** Post-dequeue `GetJob` used canceled engine ctx on shutdown → jobs stuck in `Processing` until orphan recovery → detached ctx (Band 3) | — | ✅ |
| **B7-004** All queues at concurrency limit → workers spin 100ms wakeups → exponential backoff or signal when a slot frees | 0.5 day | ✅ |
| **B7-005** Invalid config (e.g. unknown `storage.backend`) fails late at `OpenStorage` → validate in `Load()` and fail fast | 0.5 day | ✅ |
| **B7-006** `Requeue` ignores current state → can resurrect `Succeeded`/`Dead` jobs → reject or no-op on terminal states | 0.5 day | ✅ |
| `Dockerfile` — Multi-stage build (tiny distroless image) | 1 day | ✅ |
| `gfire.example.yaml` — Documented example config | 1 day | ✅ |
| `README.md` — Quick start, curl examples, config reference | 1 day | ✅ |
| Observability: structured logging (slog), request IDs | 1 day | ✅ (request IDs) |
| End-to-end test: docker-compose → curl jobs → verify via CLI | 1 day | ⬜ |
| Helm chart for AKS deployment (optional) | 2 days | ⬜ |
| `v1.0.0` tag + release notes | — | ⬜ |


**🔑 v1.0.0** — "Production-ready. Deploy on AKS. Monitoring via Prometheus + Grafana + CLI."

**Shipped v0.6.1** — B7-001–003: constant-time Bearer auth, scheduler tick clarity, shutdown GetJob fix (MiniMax audit).

---



## Band 8 — Pipelines (DAG orchestration) (post-v1)

> First-class DAG runs: parallel steps, `all_of` joins, fan-out, completion barriers. Headless YAML + HTTP — the wedge vs Airflow/Dagster is language-agnostic `cmd` handlers, not operator catalogs or embedded UI.


| Deliverable | Est. effort |
| ----------- | ----------- |
| `internal/pipeline/` — definition parser (YAML), DAG validation | 2 days |
| Storage: `pipeline_runs`, `pipeline_step_runs` (PG + Redis/ValKey) | 3 days |
| `engine/pipeline.go` — step readiness, fan-out, barrier on `on_all_success` | 3 days |
| `api/handlers_pipelines.go` — run, get, list, cancel | 2 days |
| CLI: `gfire pipeline run`, `gfire pipeline run get` | 1 day |
| E2E test: etl-daily fixture (2 extracts → pivot → 3 loads → run Succeeded) | 2 days |
| `docs/pipelines.md` — YAML reference + ETL example | 1 day |


**Design case #1:** multi-source extract → pivot table → fan-out to N destinations; run `Succeeded` only when all branches complete.

**Depends on:** v1.0.0 (engine, API, continuations, storage backends).

**🔑 v1.1.0** — "Headless DAG orchestration with join and fan-out barriers."

---



## Post-v1 enhancements (deferred — do not block v1.0.0)

> From product review (July 2026). Track here; implement after v1 API is stable unless demand forces earlier.


| ID | Feature | Why | Target |
| --- | --- | --- | --- |
| PV-001 | **GFireUI** (React dashboard) | Ops visibility, monetization | Post-v1 separate repo |
| PV-002 | **OpenTelemetry traces** | Multi-service prod debugging | v1.1+ / Band 7+ |
| PV-003 | **Continuations v2 — fan-out** (N child jobs) | Parallel ETL branches | v1.1+ (Band 8 pipelines overlap) |
| PV-004 | **Payload offload** (S3/etc.) for args >10MB | Large inline payloads | On customer demand |
| PV-005 | **Rate limit per queue** | Multi-tenant fairness | Enterprise / plugin |
| PV-006 | **Per-queue isolated worker pools** | critical vs low isolation | Engine v2 |
| PV-007 | **Webhooks on terminal state** | Push vs poll for ops | Post-v1 / enterprise |
| PV-008 | **Unique jobs** (dedupe by name+args hash) | Sidekiq-style | On demand |
| PV-009 | **Long-lived handler pool** (JSON-lines stdin) | Sub-50ms job latency | v1.x optional |

**Explicit non-goals for v1:** compete with BullMQ/Kafka throughput; embedded UI; inline GB payloads; Raft/leader election.

---



## Summary

```
Week 1  │ Band 0 ─ Foundation                      ✅ v0.1.0
Week 2-3│ Band 1 ─ PostgreSQL                       ✅ v0.2.0
Week 3-4│ Band 2 ─ Redis / ValKey                   ✅ v0.3.0
Week 4-5│ Band 3 ─ Engine: workers + middleware      ✅ v0.4.0
Week 5-6│ Band 4 ─ Scheduler + recurring + cont.    ▶ partial (v0.6.0)
Week 6-7│ Band 5 ─ REST API (core)                  ✅ v0.6.0
Week 7  │ Band 6 ─ CLI (core)                       ✅ v0.6.0
Week 8  │ Band 7 ─ Polish, Docker, release          ⬜ v1.0.0
Post    │ Band 8 ─ Pipelines (DAG)                  ⬜ v1.1.0
```

**Total:** ~8 weeks for one developer working consistently (v1.0.0).
**Post-v1:** Band 8 adds headless DAG orchestration (~2 weeks).
**Deliverable (v1):** Single binary (`gfire`), REST API, CLI, Prometheus metrics, three storage backends, n+1 scaling without consensus. No embedded UI (GFireUI separate project).
**Deliverable (v1.1):** Declarative pipelines with join, fan-out, and run-level completion barriers.