# datawatch Test Suite

> **Test artifacts live outside the repo.** `scripts/run-tests.sh` automatically creates and cleans up a sibling working directory (`../datawatch-<id>/`) for each run so artifacts never touch the source tree.
> This `docs/testing/` folder holds the **canonical docs** (master cookbook, test plan) that are synced back here after each run.

No manual setup required. Just run the script.

---

## How to Run Tests

```bash
# Full run — working dir created automatically, deleted on success
bash scripts/run-tests.sh

# Filter by surface or feature
bash scripts/run-tests.sh --surface=api
bash scripts/run-tests.sh --feature=sessions

# Single story
bash scripts/run-tests.sh --story=TS-042

# Resume a failed run (reuses the working dir the failure left behind)
DATAWATCH_TEST_ID=abc123 bash scripts/run-tests.sh --resume-from=TS-042

# Keep working dir even on success (for debugging)
KEEP_TEST_DIR=1 bash scripts/run-tests.sh
```

Each run gets a unique 6-char hex `RUN_ID` (printed at startup). If the run fails the working dir is kept and the resume command is printed. Parallel runs on the same filesystem get different IDs and never collide.

After each run the script automatically:
1. Updates `docs/testing/master-cookbook.md` in this repo with the latest pass/fail counts
2. Writes `evidence/TS-NNN/` into the working dir (kept on failure)
3. Prints a `git commit` command to record the run

## Key Files

| File | Where it lives | Purpose |
|------|---------------|---------|
| `scripts/run-tests.sh` | `datawatch/scripts/` ← **this repo** | Test runner + working dir lifecycle |
| `scripts/test-stories/TS-NNN.sh` | `datawatch/scripts/test-stories/` | Per-story implementations |
| `master-cookbook.md` | `docs/testing/` ← **this folder** | Story status (canonical, auto-synced after runs) |
| `v7.0.0/plan.md` | `docs/testing/v7.0.0/` ← **this folder** | Test plan + story list (canonical, auto-synced) |
| `.datawatch-test-<pid>/` | `../datawatch-<id>/` | Isolated daemon data dir (auto-created, auto-deleted) |
| `evidence/` | `../datawatch-<id>/evidence/` | Run evidence (kept on failure) |

## Working directory structure (auto-managed, outside repo)

```
../datawatch-a3f9c1/         ← auto-created per run; "a3f9c1" = run ID
├── .datawatch-test-<pid>/   ← isolated daemon data dir (deleted on success)
└── evidence/
    └── TS-NNN/              ← per-story evidence (kept on failure)
```

## Adding new test stories

1. Add rows to `docs/testing/master-cookbook.md` (story definition, canonical)
2. Add `scripts/test-stories/TS-NNN.sh` (story implementation)
3. Run `bash scripts/run-tests.sh --story=TS-NNN` to test it

## Release smoke test

`scripts/release-smoke.sh` tests the **production** daemon at `https://localhost:8443` as a pre-release health check. It is separate from the isolated E2E suite above. Run it before tagging a release:

```bash
bash scripts/release-smoke.sh
```
