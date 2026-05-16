# datawatch Test Suite

> **Tests now run from outside the repo.** The test runner and all artifacts live in the sibling folder `../datawatch-testing/`.
> This `docs/testing/` folder holds the **canonical docs** (master cookbook, test plan) that are synced back here after each run.

---

## How to Run Tests

```bash
# Full run — run from anywhere, script is outside the repo
bash ../datawatch-testing/run-tests.sh

# Filter by surface
bash ../datawatch-testing/run-tests.sh --surface=api

# Single story
bash ../datawatch-testing/run-tests.sh --story=TS-042

# Resume after fixing a blocker
bash ../datawatch-testing/run-tests.sh --resume-from=TS-042
```

After each run the script automatically:
1. Updates `docs/testing/master-cookbook.md` in this repo with the latest pass/fail counts
2. Copies `runs/YYYY-MM-DD-NNN/summary.md` and `failures.jsonl` here (gitignored)
3. Prints a `git commit` command to record the run

## Key Files

| File | Where it lives | Purpose |
|------|---------------|---------|
| `run-tests.sh` | `../datawatch-testing/` | Test runner (outside repo) |
| `master-cookbook.md` | `docs/testing/` ← **this folder** | Story status (canonical copy, auto-synced from testing folder) |
| `v7.0.0/plan.md` | `docs/testing/v7.0.0/` ← **this folder** | Test plan + story list (canonical, auto-synced) |
| `.datawatch-test/` | `../datawatch-testing/` | Isolated test daemon data dir (never in repo) |
| `runs/` | `../datawatch-testing/runs/` | Run evidence (outside repo) |

## Folder structure (outside repo)

```
datawatch-testing/          ← sibling of this repo
├── run-tests.sh
├── master-cookbook.md      ← working copy (synced to docs/testing/ after runs)
├── v7.0.0/
│   ├── plan.md             ← working copy (synced to docs/testing/v7.0.0/ after runs)
│   ├── cookbook.md
│   └── test-isolation-guide.md
├── runs/                   ← evidence (local only, never committed)
│   └── YYYY-MM-DD-NNN/
│       ├── summary.md
│       ├── failures.jsonl
│       └── evidence/TS-NNN/
└── .datawatch-test/        ← isolated test daemon (local only)
```

## Adding new test stories

1. Add rows to `docs/testing/master-cookbook.md` (canonical location in this repo)
2. Copy updated cookbook to `../datawatch-testing/master-cookbook.md` (working copy)
3. Add corresponding story implementation to `../datawatch-testing/run-tests.sh`
4. Run the tests — the sync-back updates the repo copy automatically

## Release smoke test

`scripts/release-smoke.sh` tests the **production** daemon at `https://localhost:8443` as a pre-release health check. It is separate from the isolated E2E suite above. Run it before tagging a release:

```bash
bash scripts/release-smoke.sh
```
