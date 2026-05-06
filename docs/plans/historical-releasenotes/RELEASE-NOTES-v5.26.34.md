# datawatch v5.26.34 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.33 → v5.26.34
**Patch release** (no binaries — operator directive).
**Closed:** Cluster dropdown now lists "Local service instance" first; v5.26.30's required-field validation on cluster reverted.

## What's new

### Cluster dropdown leads with "Local service instance"

Operator clarification: *"The prd automation, since it supports profile on local disk the cluster profile selection should support local service instance in addition to a cluster. cluster should be only if non local disk profile is selected."*

The v5.26.30 unified Profile dropdown made the New PRD modal cleaner, but the cluster validation rule was wrong: when an operator picked a project profile, the modal *required* a Cluster Profile. That ignored the daemon's own ability to host the worker — `handleStartSession` clones the project profile's repo into `<data_dir>/workspaces/` and runs the session in a local tmux as a first-class supported mode (the v5.26.21 + v5.26.22 + v5.26.24 + v5.26.26 + v5.26.27 work all converged on making that path solid). Forcing the operator to invent or pre-create a cluster profile when they just wanted to run on the local daemon was friction with no payoff.

v5.26.34:

- **Cluster dropdown's first option is now `— Local service instance (daemon-side) —`** with empty value. Selecting it (or just leaving the dropdown alone) submits with `cluster_profile=""`, which the daemon already interprets as "clone + run on me."
- **The required-field check on cluster is gone** when a project profile is selected. The operator can pick a real cluster from the dropdown if they want to dispatch remotely, but it's optional.
- Project-directory mode is unchanged — `project_dir` is still required when the dir option is selected.

### Decision matrix

| Profile selection | Cluster selection | Required? | What runs |
|------|------|------|------|
| `__dir__` (local checkout) | (cluster row hidden) | `project_dir` required | Local tmux against the operator's existing checkout |
| project profile picked | `— Local service instance —` (empty) | optional — defaults to local | Daemon clones repo into `<data_dir>/workspaces/`, runs in local tmux |
| project profile picked | named cluster profile | optional — explicit choice | Cluster spawn via `/api/agents`, worker container/Pod runs in the named cluster |

### Backward compatibility

REST contract is unchanged — `POST /api/autonomous/prds` and `POST /api/sessions/start` still accept `cluster_profile=""` (always have). The change is purely PWA-side: dropdown wording + dropping the client-side required-field check.

## Configuration parity

No new config knob.

## Tests

UI change only — Go test suite unaffected (still 465 passing).

Smoke unaffected: §7d still creates and references the persistent `smoke-testing` cluster + `datawatch-smoke` project profiles. The cluster-required-field rule lived only in the PWA submit handler, never in the smoke script.

## Known follow-ups

Phase 6 (howtos / screenshots / diagrams refresh) needs the PWA shape to settle before screenshots are recaptured — v5.26.34 is hopefully the last UI-shape change in the New PRD flow before that refresh.

Phase 3 (per-story execution profile) and phase 4 (file association) still upcoming — both benefit from the persistent fixtures from v5.26.33 as their test substrate.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA on next visit (SW cache bumped to
# datawatch-v5-26-34). Open New PRD, pick a project profile —
# the Cluster dropdown's first item is now "Local service
# instance" and submitting with that value works without an
# error toast.
```
