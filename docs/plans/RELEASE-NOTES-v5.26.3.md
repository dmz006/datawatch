# datawatch v5.26.3 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.2 → v5.26.3
**Patch release** (no binaries / containers — operator directive: every release until v6.0 is a patch).
**Closed:** Long-press server-status refresh + autonomous CRUD button revival + reload CLI test + security review pre-v6.0 + dependency-vuln cleanup

## What's new

### Long-press any server-status indicator → force-refresh the connection

Operator: *"If long hold on server status green or red indicator refresh server connection. On any page where the server status is shown."*

Status indicators are everywhere — header bar (`#statusDot`), Settings → Comms → Servers card (`.connection-indicator`). v5.26.3 wires a single delegated long-press handler to both:

- `pointerdown` on a status indicator arms a 600 ms timer.
- `pointermove` over 10 px tolerance cancels (lets clicks/taps stay clicks).
- On expiry, `forceRefreshConnection()` closes the live WS, resets the back-off, and calls `connect()` after a 100 ms flush window. Toast confirms.

Both indicators get a `title="Long-press to refresh server connection"` tooltip and `cursor: pointer; user-select: none; touch-action: manipulation;` styling.

### Autonomous tab buttons revived (Edit / Delete / Approve / Reject / Run / Cancel / Decompose / Revise / LLM)

Operator: *"The autonomous tab all had refresh and isn't crud, no edit, etc."*

Every PRD-row button silently no-op'd. Cause: `renderPRDActions` built `<button onclick="${fn}">` where `fn` interpolated `JSON.stringify` outputs. Those outputs contain literal `"` chars, which closed the outer `onclick="..."` attribute mid-string — the browser parsed the attribute as `onclick="callFn("` and discarded the rest. Click-handlers compiled to invalid JS and never fired.

v5.22.0 hit the same bug for the Save button inside the Edit modal and patched that one site (`escHtml(JSON.stringify(id))` so inner quotes become `&quot;`). v5.26.3 fixes the parent helper:

```js
const a = (label, fn, color) =>
  `<button class="btn-secondary" … onclick="${escHtml(fn)}">${label}</button>`;
//                                          ^^^^^^^^^ added
```

Two more inline-onclick sites had the same shape and got the same fix:

- `renderTask` Edit (✎) button → `escHtml(editFn)`
- `loadPRDChildren` Load button on each PRD row → `escHtml('loadPRDChildren(' + JSON.stringify(id) + ')')`
- Alerts-view session link → `escHtml(`navigate('session',${JSON.stringify(sessID)})`)`

Auto-refresh wiring (v5.24.0) was already correct — the `prd_update` WS message + 250ms-debounced `loadPRDPanel()` reload still works. Once buttons fire, the auto-refresh shows the post-mutation state.

### Reload CLI test (audit gap from 2026-04-26)

`newReloadCmd` shipped in v5.7.0 with no test; the 2026-04-26 audit flagged it. v5.26.3 adds `reload_cmd_test.go`:

- `TestNewReloadCmd_CobraShape` — `Use="reload"`, `Long` mentions "hot-reload", `RunE` wired.
- `TestNewReloadCmd_HitsReloadEndpoint` — stands up an httptest fake daemon, points the CLI at it via `$HOME/.datawatch/config.yaml`, asserts `POST /api/reload` was hit.
- `TestNewReloadCmd_PropagatesErrors` — 500 from daemon → `RunE` returns an error containing "500".

### Pre-v6.0 security review

New `docs/security-review.md` documents the gosec triage and govulncheck state. Replaces the rolling "gosec HIGH-severity review" follow-up that's been carried since the v5.0 era.

**govulncheck:** clean. Two transitive flags (GO-2026-4503 in `filippo.io/edwards25519`, GO-2026-4559 in `golang.org/x/net`) closed by `go get -u` + `go mod tidy`. Symbol-resolution had already shown datawatch wasn't calling either vuln, but bumping clears the audit.

**gosec HIGH/MEDIUM-conf:** 55 → 41 findings. Real fixes / mitigations:

- **G702 (7 → 0)** — `syscall.Exec(selfPath, …)` for daemon hot-restart and `exec.Command("git", "-C", dir, …)` for project-summary git calls. All argv-list invocations (no shell), so taint flowing into "tainted exec" is a tracker false-positive. Annotated with `// #nosec G702 -- argv-list invocation, not shell` at every site so future scans don't re-surface them.
- **G402 (7 → 0)** — every `InsecureSkipVerify=true` site annotated with rule + reason: parent↔worker pinning paths cite `TestPinnedTLSConfig_RoundTrip`; in-cluster k8s API + localhost HEALTHCHECK + bootstrap-fallback paths cite their trust-boundary justification. New annotation rule: `// #nosec G402` requires either pinning or an "accept (documented)" entry in `docs/security-review.md`.

Remaining 41 findings (G115 int-overflow false positives, G118 goroutine-ctx false positives, G122 TOCTOU in daemon's own data dir, G123 paired with already-fixed G402, G703/G704 taint-flow noise downstream of the G702 sites) are triaged in `docs/security-review.md` with one-line dispositions and an updated procedural rule for future patches.

### Dependency cascade

`go get -u` cascade swept through:

```
filippo.io/edwards25519 v1.1.0 → v1.2.0
golang.org/x/crypto    v0.48.0 → v0.50.0
golang.org/x/mod       v0.33.0 → v0.34.0
golang.org/x/net       v0.50.0 → v0.53.0
golang.org/x/sync      v0.19.0 → v0.20.0
golang.org/x/sys       v0.42.0 → v0.43.0
golang.org/x/term      v0.40.0 → v0.42.0
golang.org/x/text      v0.34.0 → v0.36.0
golang.org/x/tools     v0.42.0 → v0.43.0
```

All transitive — no API changes for datawatch. Test suite passes unchanged (1395 tests).

## Configuration parity

No new config knob.

## Tests

1395 passing — added 3 (`TestNewReloadCmd_*`). Long-press handler is browser-only and out of Go test scope; manually verified in Chromium.

## Known follow-ups

- Doc-alignment sweep (next patch — v5.26.4): `docs/mcp.md`, `docs/commands.md`, README interface table, `docs/api/*.md` for v5.x-added endpoints, `docs/testing-tracker.md`.
- Design refresh (v5.26.5): `docs/design.md` + `docs/architecture.md` + `docs/architecture-overview.md`.
- Container hygiene (v5.26.6): parent-full retag, GHCR cleanup script, datawatch-app#10 catch-up issue, container audit doc.
- v6.0 cumulative release notes.

## Upgrade path

```bash
git pull          # patch series — no binary update path
# Long-press the dot for ~600ms → toast "Refreshing server connection…"
# Autonomous tab → Edit / Delete / Run / Cancel / Approve / Reject all work again
```
