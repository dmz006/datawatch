# datawatch v5.26.19 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.18 → v5.26.19
**Patch release** (no binaries — operator directive).
**Closed:** PRD project_profile + cluster_profile attachment.

## What's new

### PRDs can be based on F10 project + cluster profiles

Operator-asked: *"Prd should be based on directory or profile, should be able to check out repo and do work"* + *"Prd should also support using cluster profiles"* + *"and smoke tests tests include those"*.

Pre-v5.26.19, autonomous PRDs only accepted a local `project_dir` path. Operators wanting the executor to clone-then-work, OR dispatch the worker to a cluster, had no clean way in.

v5.26.19 adds:

| Field | What it does |
|---|---|
| `project_profile` | Names an F10 project profile (`charts/datawatch/values.yaml` → project store). The profile carries the git URL + branch + image-pair hints; the worker can clone before running the task. |
| `cluster_profile` | Names an F10 cluster profile. When set, the autonomous spawn callback POSTs to `/api/agents` (cluster spawn via the F10 driver) instead of `/api/sessions/start` (local tmux). |

Either or both can be set. `project_dir` continues to work for local-checkout PRDs.

### Validation

- At create time: **at least one of `project_dir` or `project_profile` is required** — naked PRDs (no work source) get rejected with HTTP 400.
- Profile names are validated against the F10 stores. Unknown names get `400 project profile %q not found` (or `cluster profile`). Once validated, the names persist on the PRD record alongside an audit `set_profiles` Decision row.
- Refuses to attach profiles while the PRD is `running` — operator must Cancel first.

### Wiring

- New `autonomouspkg.ProfileResolver` interface + `Manager.SetProfileResolver` indirection. Implemented in `cmd/datawatch/main.go` as `autonomousProfileResolver{projects, clusters}` adapting the existing F10 stores. Keeps `internal/autonomous` free of `internal/profile` dependencies.
- `SpawnRequest.ProjectProfile` + `ClusterProfile` threaded from the executor through `cmd/datawatch/main.go`'s `autonomousSpawn`.
- When `ClusterProfile` is set, spawn POSTs to `/api/agents` (F10 cluster spawn) instead of `/api/sessions/start`. Returned agent ID is prefixed with `agent:` in the SpawnResult so downstream readers can distinguish.
- Server-side `/api/sessions/start` now accepts `project_profile` as an optional clone hint (passed through from autonomous spawn). Daemon-side clone-handling for the local-session path lands as v5.26.20 follow-up; for now operators using `project_profile` alone should pre-clone OR pair with a cluster profile so the agent worker handles the clone.

### REST surface

```
POST /api/autonomous/prds
  body: { spec, project_dir, project_profile?, cluster_profile?, backend?, effort? }
```

```
PATCH (planned for follow-up):
  PUT /api/autonomous/prds/{id}/profiles  { project_profile, cluster_profile }
```

For now profile changes are at-create only via the REST POST body. The Manager already supports `SetPRDProfiles` so adding the PATCH endpoint is a one-handler follow-up.

### Tests

- 6 new unit tests in `internal/autonomous/profiles_test.go` cover:
  - Project name validation (known vs unknown)
  - Cluster name validation (known vs unknown)
  - Refuse-while-running guard
  - No-resolver fallback (validation skipped, names persist as-is)
  - `set_profiles` Decision audit row append
  - Missing-PRD error
- Smoke `§7c` exercises:
  - Profile pre-creation via `POST /api/profiles/projects`
  - PRD-create with unknown profile name → expect 400
  - PRD-create with known profile name → expect 200 + record carries profile name
  - Cleanup of both PRD and project profile via the EXIT trap

### Smoke result

29 PASS / 0 FAIL / 3 SKIP. New §7c lands inside the Pass count.

## Configuration parity

No new config knob (profiles use the existing F10 store).

## Known follow-ups

- **Daemon-side clone handling for `project_profile` + local session** — currently the project_profile is passed through but the `/api/sessions/start` handler doesn't consume it for cloning. Pair with cluster_profile (where the agent worker handles clone) OR pre-clone manually until v5.26.20.
- **`PUT /api/autonomous/prds/{id}/profiles`** REST endpoint for post-create profile changes.
- **PWA New PRD modal** dropdown for profile pickers (fetched from `/api/profiles/projects` + `/api/profiles/clusters`). Currently the modal accepts `project_dir` only; operators using profiles must POST directly to `/api/autonomous/prds` until the PWA catches up.
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Try a profile-based PRD via REST:
#   curl -X POST -H 'Content-Type: application/json' \
#     -d '{"spec":"...","project_profile":"webapp","cluster_profile":"prod-east"}' \
#     https://localhost:8443/api/autonomous/prds
# Operators with non-existent profile names get 400; valid names land
# in the PRD record + Decision audit log.
```
