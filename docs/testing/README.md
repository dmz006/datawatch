# datawatch Test Suite

## Structure

| Path | Purpose |
|---|---|
| `docs/testing/plan.md` | Master test plan — infrastructure, tags, design decisions |
| `docs/testing/master-cookbook.md` | Canonical story catalog (all TS-NNN entries, all versions) |
| `docs/testing/v7.0.0/` | v7.0.0 test stories, results, session summaries |
| `docs/testing/v8.0.0/` | v8.0.0 test plan and results (T40: TS-609–TS-626) |
| `docs/testing/docker-compose.test.yml` | Docker compose stack for routing E2E tests |

## Quick Start

```bash
# Full suite (auto-starts isolated daemon)
bash scripts/run-tests.sh

# v8.0 routing tests only
bash scripts/run-tests.sh --feature=routing

# Filter options
bash scripts/run-tests.sh --surface=api
bash scripts/run-tests.sh --surface=mcp
bash scripts/run-tests.sh --feature=memory
bash scripts/run-tests.sh --story=TS-609

# Resume after failure
DATAWATCH_TEST_ID=abc123 bash scripts/run-tests.sh --resume-from=TS-NNN

# Force serial execution
bash scripts/run-tests.sh --serial

# Skip daemon auto-start (use already-running daemon)
bash scripts/run-tests.sh --no-daemon
```

## Adding a New Version's Tests

1. Create `docs/testing/vX.Y.Z/plan.md` — what's new, story index
2. Create `docs/testing/vX.Y.Z/cookbook.md` — results tracker starting at 📋
3. Add new TS-NNN stories continuing from the last number in `master-cookbook.md`
4. Add entries to `master-cookbook.md` with the new sprint label
5. Implement in `scripts/test-stories/TS-NNN.sh`
