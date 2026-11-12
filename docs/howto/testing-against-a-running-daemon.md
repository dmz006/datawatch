---
docs:
  index: true
  topics: [testing, ci, scripts, autonomous]
---
# How-to: Testing Against a Running Daemon

Datawatch's two test scripts — `scripts/release-smoke.sh` (functional
smoke) and `scripts/run-tests.sh` (E2E stories) — each default to
spinning up their own fresh, isolated daemon with a throwaway data
directory. Both also support pointing at a daemon you already have
running instead: a CI-deployed kind/k8s instance, a persistent dev
daemon, a remote sandbox. This is a reusable capability, not a
one-off CI hack — any script or app driving datawatch's test surface
can use the same variables.

## Why this exists

`scripts/release-smoke.sh`'s own `kind-smoke` CI job builds a Docker
image, loads it into a `kind` cluster, and waits for it to answer
`/api/health` through a port-forward — the whole point is to catch
chart/image regressions in that *real deployed instance*. But the
script always launched its own separate local daemon anyway, so the
in-kind daemon was never actually exercised; the job silently tested
nothing new. Fixed 2026-10-13 by teaching `release-smoke.sh` to run
its functional checks against the daemon that's actually already
running, when asked to.

## `release-smoke.sh`: `DW_BASE` / `DW_BASE_TOKEN` / `DW_BASE_INSECURE`

```sh
# Point at an already-running daemon instead of starting a new one.
DW_BASE=http://localhost:18080 bash scripts/release-smoke.sh

# With auth and a self-signed/untrusted cert (the common case for a
# test/sandbox/kind-cluster daemon):
DW_BASE=https://sandbox.example:8443 \
  DW_BASE_TOKEN=your-bearer-token \
  bash scripts/release-smoke.sh
```

| Variable | Required | Meaning |
|---|---|---|
| `DW_BASE` | to enable this mode | Full base URL of the already-running daemon (`scheme://host:port`, no trailing slash). Unset = default behavior, spin up a fresh isolated daemon (unchanged). |
| `DW_BASE_TOKEN` | no | Bearer token for that daemon's REST API. Empty = no `Authorization` header (matches a daemon with `server.token: ""`). |
| `DW_BASE_INSECURE` | no (default **on** in this mode) | Whether the `datawatch` CLI's own `-u`/`--url` calls skip TLS certificate verification. Every REST/MCP check in this script already uses `curl -sk` (skip-verify) unconditionally regardless of mode, so this defaults to `1` to match that same posture — the common case (a self-signed test/sandbox/kind-cluster cert) just works. Set `DW_BASE_INSECURE=0` yourself if your external daemon has a real, CA-trusted cert and you want the CLI checks to actually verify it. |

**What's skipped in this mode, and why:** a few checks need to drop a
fixture file where the *daemon* will read it from its own local disk
(the managed-plugin discovery/lifecycle sections, and the evals-suite
smoke check) — that only works when the script and the daemon share
one filesystem, which isn't guaranteed for an external/remote/
in-cluster target. These report `SKIP` with an explicit reason in
this mode, not a false `FAIL`. Everything else — every REST/MCP
check, every CLI round-trip — runs identically against any
already-running daemon, local or external.

## `run-tests.sh`: `DW_BASE` / `DW_BASE_TOKEN` / `DW_BASE_INSECURE`

```sh
# Run E2E stories against a daemon you already have running, instead
# of starting a fresh one.
DW_BASE=https://127.0.0.1:18443 DW_BASE_TOKEN=your-bearer-token \
  bash scripts/run-tests.sh --story=TS-042
```

Same three variables and the same meaning as `release-smoke.sh`'s
above — this is one mechanism, not two. `--no-daemon` on its own still
works too (it means "skip the script's own daemon-start step," full
stop) but on its own it does **not** give you a way to point at an
arbitrary external daemon — it only reuses one on the exact ports
`TEST_PORT`/`TEST_TLS_PORT` would otherwise have picked, which is only
useful for resuming a daemon this *same* harness already started
earlier in the run (see `DATAWATCH_TEST_ID=... --resume-from=...` in
the script's own header). Pointing at a daemon on arbitrary ports —
anything you didn't start with `run-tests.sh` itself — needs `DW_BASE`.

**Corrected 2026-10-13, same day this doc was first written:** an
earlier version of this section documented `--no-daemon` + `TEST_BASE`
as a general external-daemon override. That was wrong, and the only
reason it's not still wrong in this doc is that it got tested, not
just read from the script: `TEST_PORT`/`TEST_TLS_PORT` are only
honored by the script's own port-picker when the given port is
currently *free* — which an already-running daemon's port never is —
so a pre-set `TEST_BASE` got silently recomputed and ignored, and the
run failed against nothing. `DW_BASE` was added to `run-tests.sh`
afterward, mirroring `release-smoke.sh`'s real, working mechanism
instead, and re-verified against a real external daemon before this
section was rewritten.

## Why one mechanism now, not two

Both scripts' daemon-lifecycle code predates this capability and each
had its own (in `run-tests.sh`'s case, non-functional) attempt at it.
`DW_BASE`/`DW_BASE_TOKEN`/`DW_BASE_INSECURE` is now the one shared
mechanism both use, so a new script or app that needs the same
"point me at a daemon I already have" capability should use the same
three names rather than inventing a fourth variant.

## Mobile-fit suite

The mobile-fit suite is a Playwright layout-fitness check for the datawatch PWA. For one view it installs seeded API states, sweeps viewport/theme/zoom matrices, drives the view in a live browser, runs the same five-check layout pass over the settled DOM, and reports grouped findings. It is what the datawatch Automaton runs for the Observer (System Statistics) mobile layout, and what the release-smoke and the guard stories below wrap.

### What the suite checks

Per scenario the harness runs (in `scripts/test-stories/pwa/mobile-fit-lib.mjs`):

- **`page-scroll`** — the document is wider than the CSS viewport, i.e. the page scrolls horizontally at a fixed, non-scrollable viewport.
- **`edge-cross`** — an element whose bounding box crosses the left/right viewport edge and does not sit inside its own scroll container.
- **`clipped-text`** — an element owns a direct text node that clips (overflows), yet does not expose the full value: it lacks `text-overflow: ellipsis` and/or a tooltip (non-empty `title` or `aria-label` within three levels). Ellipsis *and* tooltip together make clipped text acceptable.
- **`tap-target`** — an interactive element (`a[href]`, `button`, `select`, `textarea`, labeled `input`, `[role]`, non-negative `tabindex`) with width **or** height under **40 CSS px** (`TAP_MIN = 40`). Checkbox/radio inputs are measured over their label.
- **`overlap`** — two elements that are siblings (in layout) and overlap.

On top of the static layout pass, the runner performs the view's **interaction checks** at the first matrix cell. For the Observer view: section collapse/expand (re-fit after each toggle), refresh, and re-render without duplicated timers. `--scope`/`MOBILE_FIT_SCOPE` (a comma-separated CSS selector list, e.g. `'#view'` or `'#settings-sec-stats'`) narrows the layout checks to the matching subtree for that run.

### How to run it

The suite lives in `scripts/test-stories/pwa/` and follows the PWA story conventions: `npm ci` in that directory, and `TEST_HTTP` / `TEST_TLS` / `TEST_TOKEN` telling it which daemon under test to drive.

**Guard stories via Make** — `e2e-pwa.sh` boots an isolated daemon on fresh 127.0.0.1 ports (or runs against an external daemon when `TEST_HTTP`/`TEST_TLS`/`TEST_TOKEN` are set) and executes the story:

```bash
make test-e2e-mobile-fit                     # TS-817 then TS-818
bash scripts/e2e-pwa.sh TS-817               # Observer regression guard only
TEST_HTTP=https://host:8443 TEST_TLS=1 TEST_TOKEN=... bash scripts/e2e-pwa.sh TS-817
```

**Direct harness** (from `scripts/test-stories/pwa/`, any running daemon):

```bash
node mobile-fit.mjs                                  # observer view, quick profile (default)
node mobile-fit.mjs --view observer --profile full
node mobile-fit.mjs --scope '#settings-sec-stats' --profile full
node mobile-fit.mjs --exhaustive                     # cross product (requires --profile full)
node mobile-fit.mjs --list                           # print the run plan, no browser needed
```

Every flag has an env-var twin:

| Flag | Env | Default | Meaning |
|------|-----|---------|---------|
| `--view` | `MOBILE_FIT_VIEW` | `observer` | PWA view to drive (only `observer` is wired today) |
| `--profile` | `MOBILE_FIT_PROFILE` | `quick` | `smoke` \| `quick` \| `full` |
| `--exhaustive` | `MOBILE_FIT_EXHAUSTIVE=1` | off | full viewport×theme×zoom×state cross product (requires `--profile full`) |
| `--out` | `MOBILE_FIT_OUT` | `$EVIDENCE_DIR/$CURRENT_STORY/mobile-fit` | report/screenshot output directory |
| `--report-only` | `MOBILE_FIT_REPORT_ONLY=1` | off | rebuild the report from a saved run; no browser |
| `--scope` | `MOBILE_FIT_SCOPE` | whole page | comma-separated CSS selectors scoping the layout checks |
| `--list` | — | — | print the run plan (states × profiles, named scenarios, interactions) and exit |

Exit codes: `0` pass (no findings), `1` findings or a harness failure, `2` browser unavailable or malformed flags.

`node mobile-fit-selftest.mjs` proves the detectors themselves on known-good/known-bad fixture pages at a 360×740 viewport (no daemon, no PWA): the `clean` fixture must yield zero findings and each known-bad fixture must yield exactly its expected checks.

### Profiles and what each covers

The matrices come from `mobile-fit-matrix.mjs` — 12 viewports: portrait 320/360/375/390/412/600/768 and landscape 320/360/375/390/412 (e.g. 390×844, 768×1024); themes light and dark; zooms 100, 130, 200.

- **smoke** — one cell: 390×844 portrait, dark, 100% zoom, on the first seeded state. Fastest possible check that the harness can drive the view at all.
- **quick** (default) — per quick state (all states except `stats-empty` and `stats-slow`): portrait 320/360/390/768, dark, 100% zoom; plus 360-landscape dark, one 360-portrait light cell per state; and one 360-portrait cell at 200% zoom on the first quick state. (Seven cells per quick state.)
- **full** — three sweeps over every state: **A** all 12 viewports, dark, 100% zoom; **B** four reference cells (360 portrait, 390 portrait, 768 portrait, 360 landscape), light, 100% zoom; **C** 130% and 200% zoom over portrait 320/360/390/412, on the `three-peers` zoom state. The 2026-10-24 Observer post-fix full run was 284 scenario runs.
- **exhaustive** (with `--profile full`) — the full cross product: all 12 viewports × 2 themes × 3 zooms × every state. Use only when debugging a specific geometry; it is an order of magnitude larger than full.

Every profile also runs the view's **named scenarios** (Observer: `system-stats-phone` — `three-peers` at 360×740, dark, 100% zoom, scoped to `#settings-sec-stats`) and the three **interaction checks** at the first matrix cell.

### Reports, screenshots, and reading group ids

`writeReport` lands under the out directory (default `$EVIDENCE_DIR/$CURRENT_STORY/mobile-fit`):

- `mobile-fit-report.json` — machine-readable: runs, checks, and findings grouped by group id;
- `mobile-fit-report.txt` — the same as a text listing: each finding's check, CSS selector, viewport, and the first screenshot that shows it;
- `screenshots/<sanitised-scenario>.png` — one screenshot per scenario run; a finding's own screenshot (when one exists) wins, otherwise the run's screenshot is cited.

A **group id** is `G-` + the first 8 hex characters of the SHA-1 of `check + CSS selector`, computed identically in every run on any tree. Consequences for reading reports:

- the same group id in two different runs is the *same check on the same element* — i.e. the same root cause, even if the affected viewport or state changed;
- one group can contain many findings (one per affected scenario), so **group counts and finding counts never agree by design** — compare by group id, not by totals;
- the regression guard (TS-817) keys on group ids: it fails while any of the recorded pre-fix groups (e.g. `G-20442ed3`, clipped text) still produces a finding, and passes when none of them reproduce. The official Observer replay runs it with `MOBILE_FIT_SCOPE=#view`: the shared header / navigation bar is owned by a separate shared-navigation pass and the guard's verdict then covers every recorded group that lives inside the Observer view; without a scope the whole page is checked and those shared-chrome groups count too (the unscoped replay currently still fails on them).

### Seeded data states

States live in `scripts/test-stories/pwa/mobile-fit-seeds.mjs`. The module is pure data plus a Playwright route-handler factory: `buildSeed(state)` returns a fixed set of API responses keyed by route, and `installSeed(page, state)` intercepts `**/api/**` at the context level so **no live peers, no real stats, no timing** are involved. Bodies are deterministic (fixed reference clock, no randomness or timestamps) — the unit tests assert byte-identical rebuilds.

Observer states today:

| State | What the view sees |
|-------|--------------------|
| `no-peers` | no peers at all; every peer table empty |
| `one-peer` / `three-peers` | one / three peers; basic card population (three-peers doubles as the zoom-sweep state) |
| `ten-peers` | ten peers inside fixed-height cards (scrolling stress) |
| `long-hostname` | 70-char local hostname, 80- and 40-char peer names (ellipsis/layout) |
| `peer-errored` | one 502 peer, one never-pushed peer, one stale peer |
| `peer-no-gpu` / `peer-multi-gpu` | local without GPUs / local with a multi-GPU set |
| `ebpf-unavailable` / `ebpf-many-processes` / `ebpf-long-names` | eBPF off, large per-process tables, long process names |
| `sessions-empty` / `sessions-large` | no sessions / large session lists |
| `stats-slow` | stats endpoints delayed (the view is checked in **loading** and settled phases) |
| `stats-failed` | stats endpoints return long 5xx errors |
| `numbers-large` | petabyte/exabyte-scale counters (compact unit formatting) |

**Adding a new data state:**

1. Pick a descriptive kebab-case name — states are referenced by name in stories, reports, and this table.
2. Add it to `STATE_NAMES` in `mobile-fit-seeds.mjs` (the seed inventory test `mobile-fit-seeds.test.mjs` pins the exact list — update its expectation in the same change).
3. Add a builder under `STATE_BUILDERS`: start from `baseSeed({ peers, local })` and override individual `responses` entries where the state needs different payloads (see `stats-slow`/`stats-failed` for per-endpoint delay/error patterns). Reuse the `make*` builders; keep it deterministic — no `Math.random()`, no `Date.now()`.
4. Decide its matrix tier in the view provider (`mobile-fit-observer.mjs`): `states` (full sweeps, always), `quickStates` (quick profile), `zoomStates` (zoom sweep). Slow or empty-endpoint states are typically excluded from `quick`.
5. Cover the new state in `mobile-fit-seeds.test.mjs` (shape + determinism) and re-run `node mobile-fit-selftest.mjs` to keep the detector evidence current.

### Desktop and tablet baseline (TS-818)

`mobile-fit-desktop.mjs` captures the bounding rectangles of the tracked Observer selectors at 1280×800, 1920×1080 and 768×1024 (dark, 100% zoom, `three-peers`) and records them in `scripts/test-stories/pwa/mobile-fit-desktop-baseline.json`. TS-818 re-captures the same document against a live daemon and passes when every tracked value is within `max(2px, 1% of the baseline value)`; a tracked selector that no longer exists on the page is always a failure.

**Re-recording rule.** The baseline is a lock, not a snapshot of the moment. Re-record it (`node mobile-fit-desktop.mjs --record`, which needs `TEST_HTTP`/`TEST_TOKEN` of a live daemon; `--compare` is the default mode and `--self-test` is the browser-free comparator check) only for a **deliberate, reviewed layout change** whose reason is recorded with the change that re-recorded it. The capture harness is read-only — it must not modify `app.js`/`style.css` or the tracked layout — so an unexplained drift against the baseline is exactly what the guard exists to fail.

### Release-smoke integration

`scripts/release-smoke-mobile-fit.sh` is the release-smoke entry point (invoked from `scripts/release-smoke.sh` as the mobile-fit view runner check). It derives `TEST_HTTP` / `TEST_TLS` / `TEST_TOKEN` from `DATAWATCH_BASE` + `DATAWATCH_TOKEN` and runs the observer view at the quick profile. The report lands in a fresh temp dir whose path the script prints — `$REPORT_DIR/mobile-fit/mobile-fit-report.json` and `-report.txt`, plus `screenshots/`. It skips (exit 2) when the daemon is unreachable at `DATAWATCH_BASE`, `node` is missing from PATH, or Playwright is not installed in `scripts/test-stories/pwa` (run `cd scripts/test-stories/pwa && npm ci` there first).

| Exit | Meaning |
|------|---------|
| `0` | suite passed (no findings) |
| `1` | findings present — release failure |
| `2` | browser or environment unavailable — `SKIP:` line on stderr; the release smoke records this as a skip, not a failure |

## For other agents and integrations

Both mechanisms are safe to drive from outside a human terminal — an
orchestration script, an automation, or another datawatch Automaton
that needs to functionally verify a daemon it (or something else)
already stood up. Set the env vars documented above and invoke the
script exactly as shown; nothing about either mechanism assumes an
interactive shell.

## See also

- [daemon-operations.md](daemon-operations.md) — general daemon lifecycle (start/stop/restart)
- [docs/testing-tracker.md](../testing-tracker.md) — the TS-### E2E story ledger `run-tests.sh` drives
- `scripts/release-smoke.sh` and `scripts/run-tests.sh` — both scripts' own header comments are the authoritative source; this doc summarizes them
