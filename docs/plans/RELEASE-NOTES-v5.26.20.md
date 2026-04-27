# datawatch v5.26.20 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.19 → v5.26.20
**Patch release** (no binaries — operator directive).
**Closed:** PWA New PRD modal profile dropdowns + `PUT /api/autonomous/prds/{id}/profiles`.

## What's new

v5.26.19 landed the `project_profile` + `cluster_profile` PRD fields and the F10 profile-resolver indirection but only the REST POST surface. v5.26.20 closes the two remaining v5.26.19 follow-ups: the PWA New PRD modal now exposes profile dropdowns, and a PUT endpoint exists for post-create profile changes.

### PWA New PRD modal — profile dropdowns

`openPRDCreateModal` pre-fetches `/api/profiles/projects` + `/api/profiles/clusters` (in parallel with the existing backends + models fetch) and renders two new dropdowns above the existing `Project directory` text input:

- **Project profile** — `(none — use project_dir below)` placeholder + all F10 project profile names.
- **Cluster profile** — `(local — tmux session on this host)` placeholder + all F10 cluster profile names.

The modal shows the helper text *"Pick either a project profile (worker clones the repo) or a project directory (worker uses an existing checkout). At least one is required."* — clarifies the OR-of-two semantics.

Submit-side validation: rejects with toast *"Pick a project profile OR enter a project directory"* if both are blank, matching the daemon-side rule. Avoids a round-trip to surface the 400.

### `PUT /api/autonomous/prds/{id}/profiles`

Post-create profile attach / detach. Body shape:

```json
{ "project_profile": "webapp", "cluster_profile": "prod-east" }
```

Empty strings clear the respective field. Reuses `Manager.SetPRDProfiles` so:

- Profile names validated against F10 stores (HTTP 400 on unknown).
- Refuses while the PRD is `running` (operator must Cancel first).
- Records a `set_profiles` Decision audit row on success.

Returns the updated PRD record.

### Smoke §7c

Extended with the PUT round-trip:

```
== 7c. PRD project_profile + cluster_profile attachment (v5.26.19) ==
  PASS  created project profile: smoke-prof-...
  PASS  create with unknown project_profile rejected (400)
  PASS  PRD record carries project_profile=...
  PASS  PUT /profiles cleared project_profile
```

## Configuration parity

No new config knob. The new PUT endpoint reuses existing `Manager.SetPRDProfiles`.

## Tests

- 1404 + 6 (from v5.26.19) Go unit tests passing.
- Functional smoke: **32 PASS / 0 FAIL / 3 SKIP**.

## Known follow-ups

- **Daemon-side clone for `project_profile` + local session** (v5.26.19 follow-up rolled to v5.26.21) — when `project_profile` is set without `cluster_profile`, the daemon should clone the profile's git URL into a worker workspace and pass that as `project_dir` to `/api/sessions/start`. Currently the field is passed through but the session handler doesn't act on it.
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-refresh PWA tab once for the new SW cache.
# Open Autonomous → New PRD: project + cluster profile dropdowns
# appear above the Project directory text input.
# Post-create profile changes via REST:
#   curl -X PUT -H 'Content-Type: application/json' \
#     -d '{"project_profile":"webapp","cluster_profile":"prod-east"}' \
#     https://localhost:8443/api/autonomous/prds/abc12345/profiles
```
