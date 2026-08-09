# GFire vs common job queues

> **Snapshot v1.0.2+ / Band 11 contract hardening.** GFire engine is production-ready (v1.0.x). Competitor claims are best-effort summaries — verify upstream docs before betting your architecture on them. For runnable adoption proofs see ROADMAP Band 9 (ADOPT-*).

## At a glance

| Axis | GFire | Asynq | River | Faktory | Sidekiq | Celery |
|------|-------|-------|-------|---------|---------|--------|
| **Model** | Standalone service | Go library | Go library | Standalone service | Ruby gem (+ Pro) | Python library + workers |
| **Enqueue** | HTTP / curl (any language) | Go client API | Go client API | Faktory protocol / clients | Ruby API | Python API |
| **Storage** | PostgreSQL, Redis/ValKey, memory | Redis | PostgreSQL | Redis | Redis | Redis, RabbitMQ, others |
| **Handlers** | External `cmd` (any language) | In-process Go | In-process Go | Multi-lang workers | In-process Ruby | In-process Python |
| **HA** | N peers, storage dequeue; no Raft | Multi-process + Redis | Multi-process + PG `SKIP LOCKED` | Multi-process + Redis | Multi-process + Redis | Multi-worker + broker |
| **Continuations** | First-class (`on_succeeded` / `on_failed` / `always`) | Workflows / chains (library patterns) | Insert/hooks in app | Limited / app-level | Batches / callbacks (edition-dependent) | Canvas / chains |
| **DAG / pipelines** | Planned Band 8 (headless YAML + join/fan-out) | — | — | — | — | Airflow / Dagster |
| **Cron / delayed** | Recurring (6-field cron + lock) + scheduled | Yes | Yes | Yes | Yes (edition-dependent) | Beat / eta |
| **Ops surface** | CLI + Prometheus; console out-of-tree ([GFireUI](https://github.com/hrodrig/gfireui) + BFF, v0.1.x) | asynqmon | River UI (ecosystem) | Web UI | Web UI (Pro) | Flower / events |
| **Cancel cross-node** | Local to executing node (PV-011 deferred) | Via Redis | Via PG | Yes | Yes | Broker-dependent |
| **License** | MIT (core) | MIT | MPL-2.0 | GPLv3 / commercial (verify) | LGPL / commercial Pro | BSD-3-Clause (verify) |

## When to pick

| Product | Pick when… |
|---------|------------|
| **GFire** | Need a **headless job service**; apps enqueue over **HTTP** from any language; handlers are **external binaries**; want **PostgreSQL and/or Redis/ValKey**; horizontal peers without Raft. Ops console available separately (GFireUI). Post-v1: **declarative DAG pipelines** (Band 8). |
| **Asynq** | Pure **Go** app, **Redis** already in stack, want a mature **in-process** library. |
| **River** | Pure **Go** app, want jobs in the **same PostgreSQL** database, library embedding is fine. |
| **Faktory** | Want a **standalone** queue server with **multi-language** workers and are OK with Faktory's protocol/ecosystem / license. |
| **Sidekiq** | **Ruby/Rails** shop; Sidekiq Pro/Enterprise features and support matter. |
| **Celery** | **Python** shop; existing Celery/broker operational knowledge. |

## When not GFire

- Go-only + Redis + library preferred → **Asynq**
- Go-only + PostgreSQL + library preferred → **River**
- Long-running durable **workflows** / sagas with compensation → **Temporal** (different category)
- Visual DAG editor + large **operator catalog** today → **Airflow / Dagster**; GFire Pipelines (Band 8) targets headless YAML + `cmd` handlers instead
- Need a **mature** dashboard UI today → GFireUI exists (v0.1.x) but is early vs asynqmon / Sidekiq Web / Temporal UI
- Need **cross-node cancel** today → not yet (local cancel only); see ROADMAP PV-011

## Footnotes

- **Temporal**, BullMQ, Machinery, Huey, RQ — useful references but out of scope for this matrix (orchestration vs simple queue).
- Re-check Sidekiq/Celery/Faktory/River license strings and feature wording against upstream before each release.
- This page is a narrative matrix, not a trial. Band 9 ADOPT harnesses are the reproducible proofs.

## Related

- [README Compare section](../README.md#compare)
- [ROADMAP](../ROADMAP.md) — band status and milestones
- [SPECIFICATIONS](../SPECIFICATIONS.md) — behavior contract
- Landing mirror (when deployed): [gfire.net/compare](https://gfire.net/compare)
