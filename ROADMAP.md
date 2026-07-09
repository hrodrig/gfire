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
| `internal/storage/storage.go` — Full interface (20 methods)                 | ✅      |
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



## Band 3 — Engine: Worker Pool + Middleware (Week 4–5)

> The core job processing loop. Workers, dequeue, retry, panic recovery.
> **Per-job timeout and job-level heartbeat** (for 4-hour SAP extractions).


| Deliverable                                                                      | Est. effort |
| -------------------------------------------------------------------------------- | ----------- |
| `engine/engine.go` — Engine struct, Start/Stop lifecycle                         | 2 days      |
| `engine/worker.go` — Worker goroutine: fetch → middleware → execute → finalize   | 2 days      |
| Per-job timeout: kill subprocess if job exceeds `job.Timeout`                    | 1 day       |
| Per-job heartbeat ticker: worker updates `HeartbeatJob` every 60s while job runs | 1 day       |
| Graceful shutdown: SIGINT/SIGTERM → drain → exit                                 | 1 day       |
| `middleware.go` — MiddlewareFunc, Pipeline (closure chain)                       | 1 day       |
| `recovery.go` — Panic recovery middleware                                        | 1 day       |
| `retry.go` — Automatic retry with exponential backoff + jitter                   | 1 day       |
| Integration test: in-memory engine, 100 jobs, verify all complete                | 1 day       |


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

**🔑 v0.4.0** — "Engine processes jobs with retry and recovery."

---



## Band 4 — Scheduler + Recurring + Continuations (Week 5–6)

> Time-based and conditional job execution.


| Deliverable                                                                | Est. effort |
| -------------------------------------------------------------------------- | ----------- |
| `engine/scheduler.go` — Scheduled → Enqueued poller                        | 1 day       |
| `recurring.go` — RecurringManager (robfig/cron + distributed lock)         | 2 days      |
| `engine/coordinator.go` — Heartbeat, orphan recovery, stale server cleanup | 2 days      |
| `continuation.go` — Fire child jobs on parent terminal state               | 1 day       |
| `cleanup.go` — RemoveExpired goroutine                                     | 1 day       |


**Scheduler design:**

- Polls every `scheduler_interval` (default 1s)
- Fetches due scheduled jobs in batches (default 100)
- Moves each to Enqueued state atomically
- Multiple nodes can run the scheduler safely — first write wins (atomic conditional)

**Recurring design:**

- On engine start: load recurring definitions from storage → register with `robfig/cron/v3`
- Cron tick: acquire distributed lock `lock:recurring:<id>` → only winning node fires
- Fire = enqueue a new Job, update last_run/next_run in storage
- Survives restarts (persisted in storage)

**🔑 v0.5.0** — "Scheduled, recurring, and chained jobs work."

---



## Band 5 — REST API (Week 6–7)

> HTTP API so applications can talk to GFire.


| Deliverable                                                | Est. effort |
| ---------------------------------------------------------- | ----------- |
| `api/api.go` — HTTP server setup, route registration, CORS | 1 day       |
| `api/handlers_jobs.go` — All job endpoints                 | 2 days      |
| `api/handlers_queues.go` — Queue inspection                | 1 day       |
| `api/handlers_recurring.go` — CRUD for recurring           | 1 day       |
| `api/handlers_servers.go` — Server listing                 | 1 day       |
| `api/handlers_continuations.go` — Continuation creation    | 1 day       |
| JSON error handling, validation, standard envelope         | 1 day       |


**Route table:**

```
# Jobs
POST   /v1/jobs/enqueue              → 201 {job_id, status}
POST   /v1/jobs/schedule             → 201 {job_id, enqueue_at, status}
GET    /v1/jobs/{id}                  → 200 {job, states}
GET    /v1/jobs                      → 200 [{jobs}] (?state=&queue=&offset=&limit=)
POST   /v1/jobs/{id}/requeue         → 200 {status}
POST   /v1/jobs/{id}/delete          → 204
POST   /v1/jobs/{id}/continue        → 201 {status}

# Queues
GET    /v1/queues                    → 200 [{name, depth}]
GET    /v1/queues/{name}             → 200 {name, depth, stats}

# Recurring
GET    /v1/recurring                 → 200 [{entries}]
POST   /v1/recurring                 → 201 {id}
DELETE /v1/recurring/{id}            → 204
POST   /v1/recurring/{id}/trigger    → 200 {job_id}

# Servers
GET    /v1/servers                   → 200 [{servers}]

# Health
GET    /healthz                      → 200 {status: "ok"}
GET    /readyz                       → 200 {status: "ok"} (waits for storage)

# Metrics (Prometheus)
GET    /metrics                      → 200 text/plain
```

**🔑 v0.6.0** — "Full REST API. Any language can submit jobs via curl."

---



## Band 6 — CLI + Monitoring (Week 7)

> CLI for server management + Prometheus metrics for production monitoring.


| Deliverable                                                     | Est. effort |
| --------------------------------------------------------------- | ----------- |
| `cmd/gfire/main.go` — Cobra CLI entry point                     | 1 day       |
| `gfire server` — Start daemon (load config, start engine + API) | 1 day       |
| `gfire job list --state failed --limit 20`                      | 1 day       |
| `gfire job get <id>`                                            | 0.5 day     |
| `gfire job requeue <id>`                                        | 0.5 day     |
| `gfire queue list`                                              | 0.5 day     |
| `gfire server status` — active nodes, uptime, version           | 0.5 day     |
| `gfire migrate` — run storage migrations                        | 0.5 day     |
| Prometheus endpoint `GET /metrics` (always on)                  | 1 day       |


**Prometheus metrics exposed:**

```
gfire_jobs_enqueued_total{queue="default"}
gfire_jobs_succeeded_total{queue="default"}
gfire_jobs_failed_total{queue="default"}
gfire_jobs_duration_seconds{queue="default",name="sap_extract"}
gfire_workers_active{server_id="node-a"}
gfire_queue_depth{queue="default"}
gfire_servers_active
gfire_servers_stale
```

**Config (headless — no dashboard):**

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  workers: 8
  shutdown_timeout: 45s
  default_timeout: 30s      # applied when job.Timeout = 0
  max_body_size: 10485760   # 10MB max request body (413 if exceeded)
```

**No embedded UI.** GFire v1 is a headless service. Monitoring options:

- `gfire job list --state failed` for quick CLI checks
- `GET /metrics` for Prometheus + Grafana
- `GET /v1/*` for programmatic access
- External GFireUI (React, separate project) post-v1

**🔑 v0.7.0** — "CLI works. Prometheus metrics on. Ready for production monitoring."

---



## Band 7 — Release: Polish, Docker, Docs (Week 8)

> Production readiness.


| Deliverable                                                  | Est. effort |
| ------------------------------------------------------------ | ----------- |
| `Dockerfile` — Multi-stage build (tiny distroless image)     | 1 day       |
| `gfire.example.yaml` — Documented example config             | 1 day       |
| `README.md` — Quick start, curl examples, config reference   | 1 day       |
| Observability: structured logging (slog), request IDs        | 1 day       |
| End-to-end test: docker-compose → curl jobs → verify via CLI | 1 day       |
| Helm chart for AKS deployment (optional)                     | 2 days      |
| `v1.0.0` tag + release notes                                 | —           |


**🔑 v1.0.0** — "Production-ready. Deploy on AKS. Monitoring via Prometheus + Grafana + CLI."

---



## Summary

```
Week 1  │ Band 0 ─ Foundation                      ✅ v0.1.0
Week 2-3│ Band 1 ─ PostgreSQL                       ✅ v0.2.0
Week 3-4│ Band 2 ─ Redis / ValKey                   ✅ v0.3.0
Week 4-5│ Band 3 ─ Engine: workers + middleware      ⬜ v0.4.0
Week 5-6│ Band 4 ─ Scheduler + recurring + cont.    ⬜ v0.5.0
Week 6-7│ Band 5 ─ REST API                         ⬜ v0.6.0
Week 7  │ Band 6 ─ CLI + Prometheus metrics         ⬜ v0.7.0
Week 8  │ Band 7 ─ Polish, Docker, release          ⬜ v1.0.0
```

**Total:** ~8 weeks for one developer working consistently.
**Deliverable:** Single binary (`gfire`), REST API, CLI, Prometheus metrics, three storage backends, n+1 scaling without consensus. No embedded UI (GFireUI post-v1).