# Security policy

## Scope

This policy covers the GFire service binary, its configuration handling, storage backends (in-memory, PostgreSQL, Redis/ValKey), the HTTP API, and published container images.

GFire is a **headless job orchestrator**. Job handlers are external processes configured by operators; treat handler args, logs, and storage contents as potentially sensitive.

## Supported versions

We support the **latest release** tagged on `main` and meaningful security fixes on the current development branch (`develop`). Versions follow [semantic versioning](https://semver.org/) (MAJOR.MINOR.PATCH).

| Version | Supported |
| ------- | --------- |
| Latest release (see [releases](https://github.com/hrodrig/gfire/releases)) | Yes |
| Older releases | No — upgrade to latest |
| Unreleased / `develop` | Best-effort; report issues early |

## Threat model notes (operators)

### Handler `cmd` execution (CTR-006)

Handlers are external binaries (or argv) declared in job definitions / config. GFire **spawns** those processes with job args. Anyone who can write `gfire.yaml`, enqueue arbitrary `cmd` jobs, or compromise the API Bearer token can achieve **remote code execution** as the GFire process user. Mitigations:

- Run GFire under a least-privilege OS user / container (distroless nonroot image).
- Protect config files and the API token; prefer network policy so only trusted clients reach the API.
- Do not expose the engine API directly to untrusted browsers (use [gfireui-backend](https://github.com/hrodrig/gfireui-backend) or another BFF for human auth).

### Unauthenticated observability endpoints

When `auth.enabled` is true, Bearer auth applies to `/v1/*`. The following remain **open by design** for probes and scrapers: `/healthz`, `/readyz`, `/metrics`, `/openapi.json`. `/metrics` may expose business counters (enqueue/success/fail volumes). Restrict scrape access at the network layer if that is sensitive.

### Cancel locality

`POST /v1/jobs/{id}/cancel` only cancels jobs **processing on the node that receives the request**. Do not assume cluster-wide cancel until PV-011.

## Reporting a vulnerability

**Do not open a public issue** for undisclosed security vulnerabilities.

- **Preferred:** [Report a vulnerability](https://github.com/hrodrig/gfire/security/advisories/new) via GitHub Security Advisories (private to maintainers).
- **Alternatively:** Contact the maintainer through options on [github.com/hrodrig](https://github.com/hrodrig). Include description, steps to reproduce, affected versions (if known), and impact.

## What to expect

- Acknowledgment as soon as practical.
- Investigation, fix timeline, and updates on critical issues.
- Credit in the advisory or release notes if you want it; anonymous disclosure respected if you ask.
- Brief explanation if we decline or defer (e.g. out of scope).

Thank you for helping keep GFire and its users safe.
