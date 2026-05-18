# datawatch Test Suite

> **Tests now run from outside the repo.** The test runner and all artifacts live in a sibling folder named `../datawatch-<id>/` where `<id>` is a short 6-char hex identifier unique to each test environment.
> This `docs/testing/` folder holds the **canonical docs** (master cookbook, test plan) that are synced back here after each run.

Using a unique ID lets multiple test environments (different machines, parallel CI runs, local experiments) coexist on the same shared filesystem without conflicting.

---

## Setup

```bash
# Generate a unique ID for this test environment (do once per machine/environment)
id=$(openssl rand -hex 3)          # e.g. "a3f9c1"
mkdir -p "../datawatch-${id}"
# copy or symlink run-tests.sh into that folder
export DATAWATCH_TEST_ID="$id"     # add to ~/.bashrc or shell profile
echo "DATAWATCH_TEST_ID=$id" >> ~/.bashrc
```

## How to Run Tests

```bash
# Full run (DATAWATCH_TEST_ID must be set, or exactly one ../datawatch-*/ exists)
bash scripts/run-tests.sh

# Filter by surface
bash scripts/run-tests.sh --surface=api

# Single story
bash scripts/run-tests.sh --story=TS-042

# Resume after fixing a blocker
bash scripts/run-tests.sh --resume-from=TS-042

# Explicit ID (override env)
DATAWATCH_TEST_ID=a3f9c1 bash scripts/run-tests.sh --story=TS-042
```

`scripts/run-tests.sh` resolves the test folder automatically:
1. `DATAWATCH_TEST_ID` env var → `../datawatch-${DATAWATCH_TEST_ID}/`
2. Auto-discover: glob `../datawatch-*/run-tests.sh`; use if exactly one match
3. Error (multiple matches → set `DATAWATCH_TEST_ID` to choose)

After each run the script automatically:
1. Updates `docs/testing/master-cookbook.md` in this repo with the latest pass/fail counts
2. Copies `runs/YYYY-MM-DD-NNN/summary.md` and `failures.jsonl` here (gitignored)
3. Prints a `git commit` command to record the run

## Key Files

| File | Where it lives | Purpose |
|------|---------------|---------|
| `run-tests.sh` | `../datawatch-<id>/` | Test runner (outside repo) |
| `master-cookbook.md` | `docs/testing/` ← **this folder** | Story status (canonical copy, auto-synced from testing folder) |
| `v7.0.0/plan.md` | `docs/testing/v7.0.0/` ← **this folder** | Test plan + story list (canonical, auto-synced) |
| `.datawatch-test-<pid>/` | `../datawatch-<id>/` | Isolated test daemon data dir (never in repo) |
| `runs/` | `../datawatch-<id>/runs/` | Run evidence (outside repo) |

## Folder structure (outside repo)

```
datawatch-a3f9c1/           ← sibling of this repo; "a3f9c1" is your env ID
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
└── .datawatch-test-<pid>/  ← isolated test daemon per run (hash = shell PID)
```

Multiple environments on the same machine/NFS share:
```
../datawatch-a3f9c1/   ← machine A / CI job 1
../datawatch-b8e204/   ← machine B / CI job 2
../datawatch-3d71fa/   ← local experiment
```

## Adding new test stories

1. Add rows to `docs/testing/master-cookbook.md` (canonical location in this repo)
2. Copy updated cookbook to `../datawatch-<id>/master-cookbook.md` (working copy)
3. Add corresponding story implementation to `../datawatch-<id>/run-tests.sh`
4. Run the tests — the sync-back updates the repo copy automatically

## Release smoke test

`scripts/release-smoke.sh` tests the **production** daemon at `https://localhost:8443` as a pre-release health check. It is separate from the isolated E2E suite above. Run it before tagging a release:

```bash
bash scripts/release-smoke.sh
```
