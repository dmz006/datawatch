# datawatch v5.26.33 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.32 → v5.26.33
**Patch release** (no binaries — operator directive).
**Closed:** Persistent test cluster + project profile fixtures for smoke (phase 5 of operator's PRD-flow rework).

## What's new

### Persistent smoke fixtures: `smoke-testing` + `datawatch-smoke`

Operator directive: *"the testing cluster can be configured on the local server and left there for future tests and a test profile can be used with datawatch git and opencode as llm for prd and opencode as llm for coding for smoke tests."*

Up through v5.26.32 every smoke run created an ephemeral name-tagged project profile (`smoke-prof-<unix-timestamp>`) and tore it down on exit. Operator-tunable smoke wanted **persistent** fixtures the daemon keeps between runs. v5.26.33 adds a new section §7d:

```
== 7d. Persistent test profiles (datawatch-smoke + smoke-testing) ==
  PASS  cluster profile smoke-testing created (persistent — not cleaned up)
  PASS  project profile datawatch-smoke created (persistent — not cleaned up)
  PASS  PRD round-trip carries persistent fixtures (project=datawatch-smoke cluster=smoke-testing)
```

Second run on the same daemon:

```
  PASS  cluster profile smoke-testing already present (reused)
  PASS  project profile datawatch-smoke already present (reused)
  PASS  PRD round-trip carries persistent fixtures (project=datawatch-smoke cluster=smoke-testing)
```

The fixtures:

| Profile | Kind | Purpose |
|------|------|------|
| `smoke-testing` | Cluster (kind: docker, namespace: default) | Persistent local-docker cluster slot for any future smoke that needs a cluster reference |
| `datawatch-smoke` | Project (git: `https://github.com/dmz006/datawatch`, branch: main; image_pair: agent-opencode + lang-go; memory: sync-back) | Pinned to the datawatch repo with `agent-opencode` as the worker image (operator's "opencode for both PRD + coding") |

### Why these specifically

- **`agent-opencode` for the worker image** matches the operator's stated preference for opencode as the coding LLM. The PRD decompose backend (`autonomous.decomposition_backend`) is a separate config knob set in `config.yaml` — the smoke fixture covers the worker-side half of the operator's request (the half that lives in profile data); operators set `autonomous.decomposition_backend: opencode` themselves to complete the loop.
- **`kind: docker` cluster** is the simplest valid cluster shape that exercises the profile schema without requiring kubectl plumbing. Operators with k8s testing clusters wired can edit `smoke-testing` in place via `PUT /api/profiles/clusters/smoke-testing` — the smoke reuses-by-name, so the next run picks up whatever shape is there.
- **Persistent in cleanup** — `add_cleanup` is NOT called for these fixture creates, so the trap-on-EXIT cleanup walker leaves them alone. The PRD created against them still gets reaped (it's an ephemeral PRD, just one that *uses* the persistent fixtures).

### What this enables

Phase 5 unblocks phase 3 (per-story execution profile) and phase 4 (file association) by giving the smoke a stable, named profile pair to test against without polluting the operator's profile list with one-off `smoke-prof-<timestamp>` entries every run.

Future smoke phases will likely:

- Run a full decompose → approve → run → spawn cycle through `datawatch-smoke` + `smoke-testing` once `agents.enabled` + a docker socket are detected.
- Validate per-story execution profile attachment when phase 3 lands.
- Validate file-association tracking when phase 4 lands.

## Configuration parity

No new config knob. Smoke creates the fixtures via `POST /api/profiles/clusters` and `POST /api/profiles/projects` — same endpoints any operator interface uses.

## Tests

Smoke now reports `37 pass / 0 fail / 1 skip` against a fresh daemon (was `34 / 0 / 1` in v5.26.32; the new section adds 3 PASS entries). Idempotent across runs — second invocation reports "already present (reused)".

Go unit tests unaffected (script-only change).

## Known follow-ups

Phase 6 of the PRD-flow rework (howtos + screenshots + diagrams refresh) and the docs-howto coverage audit (operator directive: smoke-required subsystems should also have a howto) tracked in `docs/plans/2026-04-27-v6-prep-backlog.md`.

Phase 3 (per-story execution profile + per-story approval gate) — design first, then implementation. Persistent fixtures from v5.26.33 are the test substrate.

Phase 4 (file association) — design needed; what files does each PRD/story/task touch, where do we store the references, how do we surface them in the PWA.

## Upgrade path

```bash
git pull
# No daemon restart needed — the change is purely in scripts/.
# Run scripts/release-smoke.sh once to seed the fixtures; they
# persist for every subsequent run.
```

The fixtures will appear at:

```bash
curl -sk https://localhost:8443/api/profiles/clusters/smoke-testing
curl -sk https://localhost:8443/api/profiles/projects/datawatch-smoke
```

To clean them up manually if no longer wanted:

```bash
curl -sk -X DELETE https://localhost:8443/api/profiles/clusters/smoke-testing
curl -sk -X DELETE https://localhost:8443/api/profiles/projects/datawatch-smoke
# Next smoke run recreates them.
```
