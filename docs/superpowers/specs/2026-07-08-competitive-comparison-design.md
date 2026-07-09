# GFire competitive comparison — design

**Date:** 2026-07-08  
**Status:** Approved for implementation (pending user review of this file)  
**Snapshot baseline:** v0.2.0 / Band 1 (PostgreSQL storage)

## Goal

Ship a **serious, transparent** comparison of GFire vs common queue/job systems so README + landing can sell the real differentiator without hype: **headless service + HTTP/curl + external handlers + PG and/or Redis/ValKey + peer HA (no Raft)**.

Audience: Go developers **and** ops/platform (README + gfire.net).

## Non-goals

- Numeric scorecards or “winner” badges
- Claiming shipped features that are still roadmap
- Full Temporal/BullMQ/Machinery columns (footnote only)
- Building the gfire.net site in this repo (landing = later mirror; content source lives here)

## Approach: matrix-first

1. **Source of truth:** `docs/compare.md` (full matrix + narratives + disclaimer)
2. **README:** short Compare section (subset of axes) + link to `docs/compare.md`
3. **Infographic:** `docs/gfire-compare-infographic.png` — compact visual of the same matrix (charcoal + ember; no purple)
4. **Landing:** section or `/compare` page mirrors `docs/compare.md` (separate deploy; not blocking core docs)

## Competitors (columns)

| Product | Why included |
|---------|----------------|
| GFire | Subject |
| Asynq | Default Go + Redis library choice |
| River | Default Go + PostgreSQL library choice |
| Faktory | Closest “standalone service + multi-lang workers” peer |
| Sidekiq | Industry reference (Ruby); many mental models |
| Celery | Industry reference (Python) |

**Footnote only:** Temporal (orchestration, not a simple queue), BullMQ, Machinery, Huey, RQ.

## Axes (rows)

| Axis | Question answered |
|------|-------------------|
| Model | Library embedded in app vs standalone service |
| Enqueue | Language-coupled client vs HTTP/curl (any language) |
| Storage | Redis / PostgreSQL / multi-backend |
| Handlers | In-process functions vs external `cmd` processes |
| HA | How multi-node works (storage atomicity vs leader/Raft) |
| Continuations | First-class job chaining on terminal state |
| Cron / delayed | Scheduled and delayed enqueue |
| Ops surface | CLI, metrics, UI |
| License | OSS license / commercial tiers |

## Cell rules (transparency)

- Factual, short phrases — no marketing adjectives.
- **GFire cells must** use one of: `shipped`, `WIP Band N`, `planned` when the capability is not production-ready today.
- Header disclaimer on every surface:

  > Snapshot **v0.2.0 / Band 1**. GFire is early development. Cells marked WIP/planned are not available for production use. Competitor claims are best-effort summaries; verify upstream docs.

- Re-check competitor facts against upstream docs at implementation time; prefer conservative wording if unsure.
- No numeric scores.

## Draft matrix content (implementation seed)

Status tags apply to **GFire only**. Competitor cells = current public positioning (verify before publish).

| Axis | GFire | Asynq | River | Faktory | Sidekiq | Celery |
|------|-------|-------|-------|---------|---------|--------|
| Model | Standalone service (`planned` engine/API; storage `shipped`) | Go library | Go library | Standalone service | Ruby gem (+ Pro) | Python library + workers |
| Enqueue | HTTP / curl (`WIP` Band 5) | Go client API | Go client API | Faktory protocol / clients | Ruby API | Python API |
| Storage | PG `shipped` Band 1; Redis/ValKey `WIP` Band 2 | Redis | PostgreSQL | Redis | Redis | Redis, RabbitMQ, others |
| Handlers | External `cmd` (`planned` Band 3) | In-process Go | In-process Go | Multi-lang workers | In-process Ruby | In-process Python |
| HA | N peers, storage dequeue (`planned` with engine); no Raft | Multi-process + Redis | Multi-process + PG `SKIP LOCKED` | Multi-process + Redis | Multi-process + Redis | Multi-worker + broker |
| Continuations | First-class (`planned`) | Workflows / chains (library patterns) | Insert/hooks in app | Limited / app-level | Batches / callbacks (edition-dependent) | Canvas / chains |
| Cron / delayed | `planned` Band 4 | Yes | Yes | Yes | Yes (edition-dependent) | Beat / eta |
| Ops surface | CLI + Prometheus (`planned`); no embedded UI v1 | asynqmon | River UI (ecosystem) | Web UI | Web UI (Pro) | Flower / events |
| License | MIT (core) | MIT | MPL-2.0 | BSD-3-Clause (verify) | LGPL / commercial Pro | BSD-3-Clause (verify) |

> Implementation must verify Sidekiq/Celery/Faktory/River license strings and “continuations” wording against upstream before merge.

## Narratives — When to pick

| Product | Pick when… |
|---------|------------|
| **GFire** | Need a **headless job service**; apps enqueue over **HTTP** from any language; handlers are **external binaries**; want **PostgreSQL and/or Redis/ValKey**; horizontal peers without Raft. Accept early-stage software until v1. |
| **Asynq** | Pure **Go** app, **Redis** already in stack, want a mature **in-process** library. |
| **River** | Pure **Go** app, want jobs in the **same PostgreSQL** database, library embedding is fine. |
| **Faktory** | Want a **standalone** queue server with **multi-language** workers and are OK with Faktory’s protocol/ecosystem. |
| **Sidekiq** | **Ruby/Rails** shop; Sidekiq Pro/Enterprise features and support matter. |
| **Celery** | **Python** shop; existing Celery/broker operational knowledge. |

## When not GFire

- Go-only + Redis + library preferred → **Asynq**
- Go-only + PostgreSQL + library preferred → **River**
- Long-running durable **workflows** / sagas → **Temporal** (different category)
- Need a **dashboard UI today** → not GFire v1 (GFireUI post-v1); use CLI/Prometheus when shipped, or another product
- **Production-critical** workload now → wait for GFire v1; Band 1 is storage foundation only

## Deliverables checklist

| Path | Action |
|------|--------|
| `docs/compare.md` | Create — full matrix, narratives, disclaimer, footnotes |
| `README.md` | Add Compare section (5–6 key axes) + ToC link + link to `docs/compare.md` |
| `docs/gfire-compare-infographic.png` | Create — visual compact matrix; link from README/compare |
| Landing gfire.net | Out of repo scope for first PR; track as follow-up using same markdown |

## Visual / brand

- Match hero: dark charcoal, ember orange accents
- Infographic title: “GFire vs common job queues”
- Footer: `gfire.net` · MIT · snapshot version
- Prefer SVG→PNG or GenerateImage; if image gen fails, ship markdown table first and add PNG in follow-up

## Success criteria

- Reader can answer in under 2 minutes: “Is GFire a library or a service?” and “When should I not use it?”
- No GFire cell overclaims shipped status vs ROADMAP/README Band table
- README stays short; depth lives in `docs/compare.md`

## Implementation notes

- English for public docs (match README)
- Do not invent competitor weaknesses; transparency includes admitting GFire gaps
- After content lands, optional: regenerate features infographic separately (out of scope unless bundled)
