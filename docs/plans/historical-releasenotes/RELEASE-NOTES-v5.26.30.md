# datawatch v5.26.30 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.29 → v5.26.30
**Patch release** (no binaries — operator directive).
**Closed:** Unified "Profile" dropdown in New PRD modal — phase 1 of operator's PRD-flow rework.

## What's new

### New PRD modal: one Profile dropdown drives the whole form

Operator request: *"the project profile & cluster profile should be a dropdown 'profile' — 1st option project_dir, followed by a list of configured project profiles. if directory is selected a directory selector should be visible; if profile is selected the directory selector should be hidden. if profile is selected instead of showing directory a list of clusters in a dropdown should be visible."*

Old shape (v5.26.20 → v5.26.29):

```
Project profile  [select]    Cluster profile  [select]
Project directory [text input]
Backend [select]  Effort [select]  Model [select]
```

Three semi-independent fields with implicit "if profile, ignore dir" rules — operators had to know which combinations were valid.

New shape (v5.26.30):

```
Profile  [— project directory (local checkout) —  | profile-A | profile-B …]
   ↓ if "project directory":          ↓ if a profile:
   Project directory [path]           Cluster [select]
   Backend / Effort / Model           (backend/effort/model hidden — profile carries them)
```

The first option in the dropdown is the literal `__dir__` sentinel ("— project directory (local checkout) —"). Below that come every configured project profile by name. Selecting `__dir__` reveals the directory input + backend/effort/model row. Selecting a profile reveals a Cluster dropdown and hides the dir + backend rows entirely (the profile's `image_pair` carries the worker LLM, so backend selection would be redundant and confusing).

### Validation rules now match the UI

| Mode | Required fields | Hidden fields |
|------|-----------------|---------------|
| Directory | `project_dir` | cluster row |
| Profile | `project_profile` + `cluster_profile` | dir row, backend row |

Submit-time check fails fast with a toast (`"Pick a cluster for this profile"` or `"Enter a project directory"`) instead of bouncing through an HTTP 400.

### What didn't change

- The REST contract (`POST /api/autonomous/prds`) is identical — same fields. Older clients that POST `project_profile` + `project_dir` together still work; the daemon-side rule (one or the other) is unchanged.
- The PRD model carries both `ProjectProfile` and `ClusterProfile` exactly as before (added v5.26.19).
- Existing PRDs render the same.

## Configuration parity

No new config knob — pure UI consolidation.

## Tests

UI change only — Go test suite unaffected. Smoke run after install was clean (`/api/autonomous/prds` round-trip works the same).

## Known follow-ups (operator's full request, phased)

This is **phase 1 of 5** of the operator's PRD-flow rework. Subsequent phases tracked in `docs/plans/2026-04-27-v6-prep-backlog.md`:

- **Phase 2** — Story-level review / approve / edit UI. Currently approval gate is PRD-level only; operator needs to drill into a story, edit text, approve/reject per-story before run.
- **Phase 3** — Per-story execution profile. PRD gets a `decomposition_profile` (used to GENERATE the PRD) and a default `execution_profile` (used to run stories), with per-story override.
- **Phase 4** — File association. PRD/story/task records track which files in the workspace they reference (so the operator can see "story X touches `internal/foo.go` + `docs/howto/foo.md`").
- **Phase 5** — Persistent test cluster + test profile. Configure local testing cluster on the dev daemon, define a `datawatch-smoke` project profile (datawatch git URL + opencode for both decompose and exec), wire smoke to use it for full PRD round-trip.
- **Phase 6** — Howtos / screenshots / diagrams refresh.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA on next visit (the SW cache bump to
# datawatch-v5-26-30 forces a fresh app.js).
```
