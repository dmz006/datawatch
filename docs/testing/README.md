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

## PWA E2E — `make test-e2e-pwa`

Runs the PWA **schedules-flood** story (`scripts/test-stories/pwa/schedule-flood.mjs`,
the TS-797 spec) against an isolated daemon — **no Docker required**:

```bash
make test-e2e-pwa                    # isolated local daemon, fresh 127.0.0.1 ports
TEST_TRUSTED_TLS=https://127.0.0.1:2443 make test-e2e-pwa   # fixed trusted origin
```

**Load group** — `scripts/e2e-pwa.sh` also accepts a story *group*: `load`
(or `group:load`) runs the slow-network load scenarios TS-802..TS-810 via
`node run-all.mjs --group load` (see `scripts/test-stories/pwa/run-all.mjs`;
Makefile target `make test-e2e-pwa-load`). Stories that exit 2 (skip — e.g. no
seeded fixture or no Chrome) are counted as skipped in the runner's summary
and do not fail the run.

**Wire group** — the `wire` group (`group:wire` for `scripts/e2e-pwa.sh`, or
`node run-all.mjs --group wire`) runs TS-811: websocket bytes on a 390x844 phone
viewport against the seeded fixture (cold connect, reconnect after a 2.5 s
outage, one idle session open for 60 s, and Show history). It fails when a frame
exceeds 300 KB, when the cold connect or reconnect exceeds 512 KB, when the idle
view exceeds 256 KB per minute, or when a window-form client gets more than one
`sessions` frame per socket. Budgets are `PERF_BUDGET_WIRE_*` in
`scripts/test-stories/pwa/perf-lib.mjs`. It skips (exit 2) without the seeded
fixture.

What the target does (`scripts/e2e-pwa.sh`):

1. Picks free 127.0.0.1 ports, generates an isolated config from
   `testdata/datawatch.yaml`, starts the daemon (same mechanism as
   `scripts/run-tests.sh`) and waits for `/api/health`.
2. `npm ci` in `scripts/test-stories/pwa` (falls back to `npm install` without
   a lockfile).
3. Runs `node schedule-flood.mjs` with `TEST_HTTP`, `TEST_TLS`, `TEST_TOKEN`
   (plus `TEST_TRUSTED_TLS` when set). Exit code is the story's: **0 pass,
   1 fail, 2 skip** — so a failing story makes the target (and any CI step
   calling it) exit non-zero.
4. Stops the daemon; the per-run tmp dir (evidence + `daemon.log`) is kept on
   non-zero exit and removed on success.

If `TEST_HTTP` + `TEST_TLS` + `TEST_TOKEN` are all set, the already-running
daemon at those origins is used instead (external-daemon mode).

**Two origins** (the story runs the same flood scenario against both):

- **(A) Untrusted self-signed TLS origin** — the daemon's `TEST_TLS` origin;
  the Playwright context uses `ignoreHTTPSErrors`. The service worker cannot
  claim control here.
- **(B) Trusted TLS origin** — `TEST_TRUSTED_TLS` if provided, otherwise
  generated on the fly (throwaway openssl CA + daemon-TLS-terminating Node
  proxy + Chrome `--ignore-certificate-errors-spki-list`).
  `navigator.serviceWorker.controller` is asserted to be set **before**
  request counting begins. If (B) is infeasible on the host, the story exits
  2 (SKIP, with a reason in `result.txt`) — never a silent pass.

Pass criteria per origin: `GET /api/schedules` requests stay ≤ 5 (constant,
vs. 20+ seeded sessions) across a WS disconnect/reconnect burst, zero
`net::ERR_INSUFFICIENT_RESOURCES` request failures, and `/api/sessions` +
`/api/stats` still succeed afterwards. Seeded sessions are always cleaned up.

Evidence: screenshots + `logs.jsonl` + `verdict.txt` under
`$EVIDENCE_DIR/schedule-flood/` (inside the per-run dir, or
`/tmp/dw-e2e-pwa-evidence/schedule-flood/` in external mode).

A docker-compose convenience service (`playwright` in
`docker-compose.test.yml`) points at the same story dir, but the stories
expect native Chrome (`/usr/bin/google-chrome`) and a Node host, so prefer
the local route above.

## Adding a New Version's Tests

1. Create `docs/testing/vX.Y.Z/plan.md` — what's new, story index
2. Create `docs/testing/vX.Y.Z/cookbook.md` — results tracker starting at 📋
3. Add new TS-NNN stories continuing from the last number in `master-cookbook.md`
4. Add entries to `master-cookbook.md` with the new sprint label
5. Implement in `scripts/test-stories/TS-NNN.sh`
