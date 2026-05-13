# datawatch Test Suite

**Test plan version**: v7.0.0  
**Last run**: (never)  
**Coverage**: 0 / 170+ stories

---

## Coverage by Sprint

| Sprint | Stories | Planned | Pass | Fail | Skip | Status |
|--------|---------|---------|------|------|------|--------|
| T1 — Daemon Bootstrap + Auth | 8 | 8 | — | — | — | 📋 planned |
| T2 — Sessions | 10 | 10 | — | — | — | 📋 planned |
| T3 — Automata | 10 | 10 | — | — | — | 📋 planned |
| T4 — Council | 8 | 8 | — | — | — | 📋 planned |
| T5 — Memory + KG | 10 | 10 | — | — | — | 📋 planned |
| T6 — Secrets + Config | 10 | 10 | — | — | — | 📋 planned |
| T7 — Plugins + Skills | 8 | 8 | — | — | — | 📋 planned |
| T8 — MCP Surface | 12 | 12 | — | — | — | 📋 planned |
| T9 — Comms | 18 | 18 | — | — | — | 📋 planned |
| T10 — CLI Surface | 12 | 12 | — | — | — | 📋 planned |
| T11 — PWA / Chrome Plugin | 14 | 14 | — | — | — | 📋 planned |
| T12 — Advanced Features | 14 | 14 | — | — | — | 📋 planned |
| T13 — Docker Simulation | 8 | 8 | — | — | — | 📋 planned |
| T14 — Kubernetes Deployment | 8 | 8 | — | — | — | 📋 planned |
| T15 — Parity Audit | 11 | 11 | — | — | — | 📋 planned |
| T16 — Howto Coverage | 32 | 32 | — | — | — | 📋 planned |
| T17 — Major Feature Journeys | 10 | 10 | — | — | — | 📋 planned |
| **Total** | **173** | **173** | — | — | — | — |

---

## How to Run Tests

```bash
# Full run (starts isolated test daemon, runs all 17 T-Sprints)
bash scripts/run-tests.sh

# Filter by surface
bash scripts/run-tests.sh --surface=api

# Filter by feature
bash scripts/run-tests.sh --feature=sessions

# Skip conflict-tagged stories (e.g. those needing real Signal or k8s)
bash scripts/run-tests.sh --skip-conflict=signal
bash scripts/run-tests.sh --skip-conflict=k8s

# Combine filters
bash scripts/run-tests.sh --surface=api --feature=memory --skip-conflict=llm
```

The runner:
1. Starts an isolated daemon on ports 18080/18443/18081/18433
2. Uses `.datawatch-test/` as data dir (cleaned up on exit)
3. Saves evidence to `testing/runs/YYYY-MM-DD-NNN/evidence/TS-NNN/`
4. Updates `testing/master-cookbook.md` with pass/fail/skip per story
5. Updates this README with the latest run summary

---

## How to Read the Cookbook

`testing/master-cookbook.md` is the live status record. Each row is one story. Columns:

| Column | Meaning |
|--------|---------|
| Sprint | T-sprint number and name |
| TS-ID | Story identifier (TS-001 through TS-249) |
| Title | Short description |
| Tags | Surface + feature + conflict tags |
| Status | 📋 planned / ✅ pass / ❌ fail / ⏭ skip |
| Last Run | Date of most recent run that touched this story |
| Notes | Any failure message or skip reason |

After each run the cookbook is overwritten with updated status rows. It is the permanent status record — never deleted.

---

## How Runs Are Stored

```
testing/
  runs/               ← gitignored (local only)
    .gitkeep          ← tracked so directory exists in git
    2026-05-13-001/   ← one dir per run
      plan-snapshot.md    ← copy of master-plan.md at run time
      run-results.md      ← per-story results for this run
      evidence/
        TS-001/
          health.json
        TS-002/
          health.json
        TS-013/
          hook_start.json
          hook_activity.json
          hook_stop.json
        ...
```

Evidence directories are **never deleted**. Each run keeps its own dated directory. Only `.datawatch-test/` (the live daemon data dir) is cleaned up after each run.

To view evidence from the latest run:

```bash
ls testing/runs/
ls testing/runs/$(ls testing/runs/ | grep -v .gitkeep | sort | tail -1)/evidence/
```

---

## How to Update the Plan for New Releases

The master plan (`testing/master-plan.md`) is the single source of truth. Update it when:
- A new API endpoint is added
- A feature changes shape (request/response fields)
- A new T-Sprint is needed for a new subsystem
- Version-gated stories (tagged `[v7.1.0]`) become active

Procedure:
1. Edit `testing/master-plan.md` — add/update stories
2. Add corresponding rows to `testing/master-cookbook.md` with `📋 planned`
3. Update the coverage table in this README
4. Run `bash scripts/run-tests.sh` to populate initial status

Do **not** change the plan between runs mid-sprint. The plan is version-controlled; a git diff shows exactly what changed and when.

---

## Key Files

| File | Purpose |
|------|---------|
| `testing/master-plan.md` | Canonical story definitions — updated when features change |
| `testing/master-cookbook.md` | Live status per story — updated after every run |
| `testing/runs/YYYY-MM-DD-NNN/` | Dated run evidence (local only, gitignored) |
| `scripts/run-tests.sh` | Test runner — reads plan, writes cookbook + evidence |
