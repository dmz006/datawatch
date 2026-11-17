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

## PWA E2E — the schedules-flood story (TS-797)

Runs the PWA **schedules-flood** story (`scripts/test-stories/pwa/schedule-flood.mjs`,
the TS-797 spec) in real headless Chrome against an isolated daemon —
**no Docker required**. The local runner is `scripts/e2e-pwa.sh`; with no
story argument it runs `schedule-flood`:

```bash
bash scripts/e2e-pwa.sh                    # default story: schedule-flood
bash scripts/e2e-pwa.sh schedule-flood     # the same, named explicitly
TEST_TRUSTED_TLS=https://127.0.0.1:2443 bash scripts/e2e-pwa.sh schedule-flood  # fixed trusted origin
```

**Load group** — `scripts/e2e-pwa.sh` also accepts a story *group*: `load`
(or `group:load`) runs the slow-network load scenarios TS-802..TS-812 via
`node run-all.mjs --group load` (see `scripts/test-stories/pwa/run-all.mjs`;
Makefile target `make test-e2e-pwa-load`). Stories that exit 2 (skip — e.g. no
seeded fixture or no Chrome) are counted as skipped in the runner's summary
and do not fail the run.

**Wire group** — the `wire` group (`group:wire` for `scripts/e2e-pwa.sh`, or
`node run-all.mjs --group wire`) runs TS-816: websocket bytes on a 390x844 phone
viewport against the seeded fixture (cold connect, reconnect after a 2.5 s
outage, one idle session open for 60 s, and Show history). It fails when a frame
exceeds 300 KB, when the cold connect or reconnect exceeds 512 KB, when the idle
view exceeds 256 KB per minute, or when a window-form client gets more than one
`sessions` frame per socket. Budgets are `PERF_BUDGET_WIRE_*` in
`scripts/test-stories/pwa/perf-lib.mjs`. It skips (exit 2) without the seeded
fixture.

What the runner does in local mode (`scripts/e2e-pwa.sh`):

1. Picks free 127.0.0.1 ports and creates a per-run dir
   (`dw-e2e-pwa-XXXXXX` under `${TMPDIR:-/tmp}`), generates an isolated
   config from `testdata/datawatch.yaml`, starts the daemon (`bin/datawatch`,
   or `TEST_BINARY`, or `datawatch` on the `PATH`, else `go build`) and
   waits for `/api/health`. The config is written by `e2e_pwa_write_config`
   in `scripts/e2e-pwa-config.sh`, which also sets
   `server.allowed_project_roots` to the parent of that run dir
   (`${TMPDIR:-/tmp}`): stories such as `schedule-flood` seed sessions under
   `/tmp` (TS-823 under `$TMPDIR` when set), and without this project root
   the daemon's project-root guard refuses them. `tests/test-e2e-pwa-config.sh`
   (run by `make test`) checks that the generated config carries the temp
   root.
2. `npm ci` in `scripts/test-stories/pwa` (falls back to `npm install`
   without a lockfile).
3. Runs `node <story>.mjs` (or `run-all.mjs --group <group>` for a group)
   with `TEST_HTTP`, `TEST_TLS`, `TEST_TOKEN` (default `dw-e2e-pwa-token`),
   `EVIDENCE_DIR`, `TEST_TLS_CA` (locally: the daemon's own generated cert)
   and, when set, `TEST_TRUSTED_TLS` exported. The exit code is the story's:
   **0 pass, 1 fail, 2 skip** — a failing story makes the runner (and any CI
   step calling it) exit non-zero. TS-797 itself fails (exit 1) when Google
   Chrome is not at `/usr/bin/google-chrome`.
4. On exit — pass, fail, skip or signal — ends the daemon's sessions through
   the API first, then kills the daemon by its recorded pid.
5. Keeps the per-run dir (evidence + `daemon.log`) on any non-zero exit,
   including skip, and removes it on pass.

If `TEST_HTTP` + `TEST_TLS` + `TEST_TOKEN` are all set, the already-running
daemon at those origins is used instead (external-daemon mode): nothing is
started, and `EVIDENCE_DIR` defaults to `/tmp/dw-e2e-pwa-evidence` in that
mode.

**Two origins** (the story runs the same flood scenario against both):

- **(A) Untrusted self-signed TLS origin** — the daemon's `TEST_TLS` origin;
  the Playwright context uses `ignoreHTTPSErrors`. The service worker cannot
  claim control here.
- **(B) Trusted TLS origin** — `TEST_TRUSTED_TLS` if provided, otherwise
  generated on the fly (throwaway openssl CA + daemon-TLS-terminating Node
  proxy + Chrome `--ignore-certificate-errors-spki-list`).
  `navigator.serviceWorker.controller` is asserted to be set **before**
  request counting begins. If (B) is infeasible on the host *and* scenario
  (A) passed, the story exits 2 (SKIP, with a reason in `result.txt` and
  `SKIP-REASON.txt`) — never a silent pass. A failure in (A) always exits 1.

Pass criteria per origin: `GET /api/schedules` requests stay ≤ 5 (constant,
vs. the 20+ seeded sessions — `TS797_SESSION_COUNT` raises the seed count,
not the cap) across a WS disconnect/reconnect burst, zero
`net::ERR_INSUFFICIENT_RESOURCES` request failures, and `/api/sessions` +
`/api/stats` still succeed afterwards. Seeded sessions are always deleted and
killed in the story's finally block.

**Evidence** — under `$EVIDENCE_DIR/schedule-flood/` (in local mode the
default `EVIDENCE_DIR` is the per-run dir's `evidence/`, kept on a non-passing
run; set `EVIDENCE_DIR` to keep it after a pass too; in external-daemon mode
the default is `/tmp/dw-e2e-pwa-evidence/`): `ts797-A-untrusted-tls.png` /
`ts797-B-trusted-tls.png` screenshots; `verdict.txt.txt` with the verdict and
per-scenario request counts; `failure-A.txt.txt` / `failure-B.txt.txt` on
failure; `SKIP-REASON.txt.txt` and `result.txt` on a (B) skip; and
`trusted-origin.txt.txt` plus `trusted-tls/` (the generated CA, cert, key and
SPKI) when (B) was generated. (The doubled `.txt.txt`: the story hands
lib.mjs's `saveLog(name)` names that already end in `.txt`.)

**`make test-e2e-pwa` is the docker-compose route, not the local one:** it
starts the `datawatch` + `playwright` services
(`docs/testing/docker-compose.test.yml`) and runs the **full** story suite
(`node run-all.mjs`) inside the `playwright` container, with a fixed
environment (`TEST_HTTP=http://datawatch:8080`, `TEST_TOKEN`,
`EVIDENCE_DIR`) and no `TEST_TLS`/`TEST_TRUSTED_TLS`. The schedules-flood
story needs two *loopback* TLS origins (the local `TEST_TLS` default is
`https://127.0.0.1:18443`, plus its generated 127.0.0.1 proxy) and
`/usr/bin/google-chrome`, none of which are reachable from inside that
container, so it cannot pass through this route — use the local runner
above. The compose route is aimed at the group suites (`load`, `wire`)
against the daemon's compose-network port.

## Mobile-fit — `make test-e2e-mobile-fit`

Story IDs:
- TS-817 (`scripts/test-stories/pwa/TS-817.mjs`) — Observer mobile-fit regression guard
- TS-818 (`scripts/test-stories/pwa/TS-818.mjs`) — desktop/tablet layout baseline guard

Runs the datawatch mobile-fit suite against an isolated daemon — **no Docker required**:

```bash
make test-e2e-mobile-fit              # isolated local daemon, fresh 127.0.0.1 ports
bash scripts/e2e-pwa.sh TS-817        # one story only
TEST_HTTP=https://host:8443 TEST_TLS=1 TEST_TOKEN=... bash scripts/e2e-pwa.sh TS-817   # external daemon
```

What the target does (`scripts/e2e-pwa.sh`):

1. Same daemon lifecycle as `make test-e2e-pwa` (isolated 127.0.0.1 daemon,
   or the external daemon named by `TEST_HTTP`/`TEST_TLS`/`TEST_TOKEN`).
2. `npm ci` in `scripts/test-stories/pwa` and runs `node TS-817.mjs` then
   `node TS-818.mjs`. Exit code: **0 pass, 1 fail, 2 skip** — a finding
   makes the target exit non-zero.

What the stories check (full suite mechanics in docs/howto/testing-against-a-running-daemon.md,
section **Mobile-fit suite**):

- TS-817 — re-runs the recorded pre-fix Observer mobile-fit run (reproduction
  report: `docs/testing/runs/observer-mobile-fit/repro-report.txt` — 29,968
  findings in 68 groups over 199 scenario runs, 2026-10-23) with the API
  state fully seeded. Passes when **none** of the 68 recorded groups produces
  a finding (e.g. `G-20442ed3`, clipped text); fails listing the group IDs
  that still reproduce. Official Observer replay: `MOBILE_FIT_SCOPE=#view
  bash scripts/e2e-pwa.sh TS-817` — the shared header/navigation chrome is
  owned by a separate pass, and without the scope those groups count too.
  `make test-e2e-mobile-fit` runs the unscoped story (it currently fails the
  still-pending shared-chrome groups) and then TS-818.
- TS-818 — re-captures the Observer view at 1280×800 / 1920×1080 /
  768×1024 (dark, 100% zoom, `three-peers` seed) and passes when every
  tracked selector is within `max(2px, 1%)` of
  `scripts/test-stories/pwa/mobile-fit-desktop-baseline.json`; a tracked
  selector that is gone is always a failure.

Evidence: per story `$EVIDENCE_DIR/TS-817/mobile-fit-report.json` (+ `.txt`,
`screenshots/*.png`); archived post-fix run
  `docs/testing/runs/observer-mobile-fit/post-fix-report.txt` (2026-10-24:
  0 findings in 284 scenario runs, observer/full). Detector evidence lives
  next to the suite: `node mobile-fit-selftest.mjs` (known-good/known-bad
  fixture pages) and `node --test mobile-fit-report.test.mjs
  mobile-fit-seeds.test.mjs` from `scripts/test-stories/pwa/`; layout ratchets
  for the shipped Observer markup in `internal/server/web/` (`node --test
  app-observer-compact-units.test.js app-observer-layout.test.js`).

## Adding a New Version's Tests

1. Create `docs/testing/vX.Y.Z/plan.md` — what's new, story index
2. Create `docs/testing/vX.Y.Z/cookbook.md` — results tracker starting at 📋
3. Add new TS-NNN stories continuing from the last number in `master-cookbook.md`
4. Add entries to `master-cookbook.md` with the new sprint label
5. Implement in `scripts/test-stories/TS-NNN.sh`
