# datawatch v5.26.63 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.62 → v5.26.63
**Patch release** (no binaries — operator directive).
**Closed:** New Session modal — unified Profile dropdown matching New PRD (operator-asked).

## What's new

Operator-asked: *"New session should have same directory or profile and local daemon or cluster profile to start."*

The New PRD modal got the unified Profile dropdown across v5.26.30/34/46. v5.26.63 mirrors it on the New Session modal. Behavior:

- **Profile dropdown** (new): first option `— project directory (local checkout) —` (the `__dir__` sentinel); subsequent options every configured F10 project profile by name.
- **Cluster dropdown** (new, hidden by default): appears when a project profile is selected. First option `— Local service instance (daemon-side) —` (empty value, daemon-side clone). Subsequent options every configured cluster profile.
- **Existing LLM backend + session-profile + dir picker** stay visible only in `__dir__` mode. When a project profile is selected they hide (the F10 profile's `image_pair` carries the worker LLM).

### Spawn routing

| Mode | Endpoint | Body |
|------|------|------|
| `__dir__` | `POST /api/sessions/start` | `{task, name, backend, project_dir, profile, ...}` (existing) |
| project profile selected | `POST /api/agents` | `{project_profile, cluster_profile, task, branch}` |

The cluster_profile field is optional in profile mode — empty means "Local service instance" (daemon-side clone + local tmux), matching the cluster dropdown's first option.

### Implementation parity with New PRD

The `_sessProfileChanged` helper toggles the visibility rows the same way `_prdNewProfileChanged` does. The `populateSessionProjectClusterDropdowns` function uses the same shared `state._prdProjectProfiles` / `state._prdClusterProfiles` caches the New PRD modal already populates, so opening either modal warms the lists for the other.

## Configuration parity

No new config knob — the existing F10 project + cluster profile stores back the dropdown population.

## Tests

UI-only change. Smoke unaffected (59/0/2). Go test suite unaffected (471 passing).

## Known follow-ups

- **#45 Phase 4 file association** — next implementation work.
- **#39 wake-up stack smoke** — needs F10 agent fixture wired in CI.
- **#41 testing.md ↔ smoke audit** — pending.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache datawatch-v5-26-63). New Session
# modal now shows a Profile dropdown above the LLM backend; pick
# a project profile to spawn through F10 instead of a local tmux.
```
