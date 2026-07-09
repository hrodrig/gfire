# Contributing to GFire

Thanks for helping improve GFire.

## Ground rules

- Follow the [Code of Conduct](CODE_OF_CONDUCT.md).
- For **security issues**, use [SECURITY.md](SECURITY.md) — do not file public issues for undisclosed vulnerabilities.

## How to contribute

- **Bugs and ideas:** Open an [issue](https://github.com/hrodrig/gfire/issues). Describe what you expected, what happened, and how to reproduce.
- **Code:** Open a pull request **against `develop`**. `main` is release-only; day-to-day work merges into `develop` first.

Use focused branches when needed, for example `fix/short-topic` or `feat/short-topic`. (Maintainers may commit directly on `develop` during early development.)

## Planning docs

- Behavior contract: **[SPECIFICATIONS.md](SPECIFICATIONS.md)**
- Planned work: **[ROADMAP.md](ROADMAP.md)**
- Agent conventions: **[AGENTS.md](AGENTS.md)**

When you ship user-facing behavior:

1. Update **SPECIFICATIONS** if the observable contract changed.
2. Mark the roadmap item done in **ROADMAP**.
3. Keep commits scoped; English commit subjects (imperative, ≤72 chars).

## Before you open a PR

1. **Format:** `gofmt -w .` (or `make lint` to check).
2. **Verify:** `make all` — build, vet, test, gofmt check, gocyclo ≤15.
3. **Coverage:** `make cover` — memory backend ≥80%.

Keep commits scoped and messages understandable.

## Project language

Repository content (code, comments, docs, UI strings) should be **English**.

## Early development note

GFire is **v0.3.0** (Band 2 — Redis/ValKey). APIs and layout will still change. Prefer small PRs and discuss large design changes in an issue first.

## Questions

If something is unclear, open an issue and we can narrow the design or scope there.

## Resources

New to open source? [Open Source Guide](https://opensource.guide/how-to-contribute/) has general contribution practices.
