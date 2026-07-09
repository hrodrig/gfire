# Security policy

## Scope

This policy covers the GFire service binary, its configuration handling, storage backends (in-memory, PostgreSQL, Redis/ValKey), the HTTP API (when shipped), and published container images.

GFire is a **headless job orchestrator**. Job handlers are external processes configured by operators; treat handler args, logs, and storage contents as potentially sensitive.

**Status:** GFire is early pre-release (**v0.2.0**, Band 1). Do not run in production. Security expectations still apply to the code that exists.

## Supported versions

We support the **latest release** tagged on `main` and meaningful security fixes on the current development branch (`develop`). Versions follow [semantic versioning](https://semver.org/) (MAJOR.MINOR.PATCH).

| Version | Supported |
| ------- | --------- |
| Latest release (see [releases](https://github.com/hrodrig/gfire/releases)) | Yes |
| Older releases | No — upgrade to latest |
| Unreleased / `develop` | Best-effort; report issues early |

Until the first tagged release, report issues against `develop`.

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
