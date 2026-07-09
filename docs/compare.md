# GFire vs common job queues

> **Snapshot v0.3.0 / Band 2.** GFire is early development. Cells marked **WIP** or **planned** are not available for production use. Competitor claims are best-effort summaries — verify upstream docs before betting your architecture on them.

## At a glance

| Axis | GFire | Asynq | River | Faktory | Sidekiq | Celery |
|------|-------|-------|-------|---------|---------|--------|
| **Model** | Standalone service (`planned` engine/API; storage `shipped`) | Go library | Go library | Standalone service | Ruby gem (+ Pro) | Python library + workers |
| **Enqueue** | HTTP / curl (`WIP` Band 5) | Go client API | Go client API | Faktory protocol / clients | Ruby API | Python API |
| **Storage** | PG `shipped` Band 1; Redis/ValKey `shipped` Band 2 | Redis | PostgreSQL | Redis | Redis | Redis, RabbitMQ, others |
| **Handlers** | External `cmd` (`planned` Band 3) | In-process Go | In-process Go | Multi-lang workers | In-process Ruby | In-process Python |
| **HA** | N peers, storage dequeue (`planned` with engine); no Raft | Multi-process + Redis | Multi-process + PG `SKIP LOCKED` | Multi-process + Redis | Multi-process + Redis | Multi-worker + broker |
| **Continuations** | First-class (`planned`) | Workflows / chains (library patterns) | Insert/hooks in app | Limited / app-level | Batches / callbacks (edition-dependent) | Canvas / chains |
| **Cron / delayed** | `planned` Band 4 | Yes | Yes | Yes | Yes (edition-dependent) | Beat / eta |
| **Ops surface** | CLI + Prometheus (`planned`); no embedded UI v1 | asynqmon | River UI (ecosystem) | Web UI | Web UI (Pro) | Flower / events |
| **License** | MIT (core) | MIT | MPL-2.0 | BSD-3-Clause (verify) | LGPL / commercial Pro | BSD-3-Clause (verify) |

## When to pick

| Product | Pick when… |
|---------|------------|
| **GFire** | Need a **headless job service**; apps enqueue over **HTTP** from any language; handlers are **external binaries**; want **PostgreSQL and/or Redis/ValKey**; horizontal peers without Raft. Accept early-stage software until v1. |
| **Asynq** | Pure **Go** app, **Redis** already in stack, want a mature **in-process** library. |
| **River** | Pure **Go** app, want jobs in the **same PostgreSQL** database, library embedding is fine. |
| **Faktory** | Want a **standalone** queue server with **multi-language** workers and are OK with Faktory's protocol/ecosystem. |
| **Sidekiq** | **Ruby/Rails** shop; Sidekiq Pro/Enterprise features and support matter. |
| **Celery** | **Python** shop; existing Celery/broker operational knowledge. |

## When not GFire

- Go-only + Redis + library preferred → **Asynq**
- Go-only + PostgreSQL + library preferred → **River**
- Long-running durable **workflows** / sagas → **Temporal** (different category)
- Need a **dashboard UI today** → not GFire v1 (GFireUI post-v1); use CLI/Prometheus when shipped, or another product
- **Production-critical** workload now → wait for GFire v1; Band 2 is storage foundation only (no engine/API yet)

## Footnotes

- **Temporal**, BullMQ, Machinery, Huey, RQ — useful references but out of scope for this matrix (orchestration vs simple queue).
- Re-check Sidekiq/Celery/Faktory/River license strings and feature wording against upstream before each release.

## Related

- [README Compare section](../README.md#compare)
- [ROADMAP](../ROADMAP.md) — band status and milestones
- [SPECIFICATIONS](../SPECIFICATIONS.md) — behavior contract
- Landing mirror (when deployed): [gfire.net/compare](https://gfire.net/compare)
