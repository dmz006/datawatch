# Final gate — observer pull mode (PRD ec310001), rules-review findings fix

Date: 2026-11-06
Scope: the observer pull protocol (federated daemon pulls a peer's v2 stats
instead of the peer pushing) and the five rules-review findings against it:
the unread `observer.pull.enabled` gate, the missing startup reconcile, the
missing PWA Settings section (three keys) and Monitor card, the missing
locale keys (three, in five bundles), and the missing PWA story (TS-844).
No version change, no gh, no daemon started in this worktree.

Re-verified on attempt 3 (2026-11-06, fresh runs in this worktree): same outcome as the
lines below, freshly measured — `make build` PASS (v9.67.0, docs-index 4831
chunks across 241 unique paths), `go test ./...` 11018 passed / 0 failed in
100 packages, `node --test internal/server/web/*.test.js` 2026 of 2026,
`gofmt -l .` 0 files, `go vet ./...` clean, `make lint` 0 issues, gosec
(`-severity high -conf high`) 0 findings. This attempt also closed the two
remaining review residuals: the `### observer_pull_status` detail entry
(parameters + example response) in `docs/mcp.md`, and the generated
`docs/plans/parity-issues/ec310001.md` (DRAFT, not filed).

## Checks

| Check | Result |
|---|---|
| `make build` (A10: sync-docs + docs-index + `go build`) | PASS — binary built at `./bin/datawatch`, v9.67.0 |
| `gofmt -l .` | PASS — 0 files (the 2026-11-05 gofmt FAIL — 3 unformatted files — is fixed; alignment to the Go 1.27.2 gofmt) |
| `go test ./...` | PASS — 11018 passed, 0 failed in 100 packages (attempt-3 measurement; includes `TestObserverPullGlobalGate_DefaultsOff`, `TestObserverPullFetch_DisabledGateBlocksRequest`, `TestObserverPullReconcile_Lifecycle`, `TestObserverPullReconcile_NoStoreSafe`, `TestObserverPullConfig_DefaultsDisabled`, and the locale guard `TestLocales_CommonNavKeysPresent` with the new keys) |
| `node --test internal/server/web/*.test.js` | PASS — 2026 of 2026, 0 failures |
| `go vet ./...` | PASS — no output |
| `make lint` (A12: all check scripts + `golangci-lint run ./...`) | PASS — 0 issues (the four unchecked `fmt.Fprintf` writes in `cmd/datawatch/cli_observer.go` now assign to `_`); leak check 0/0/0/0 |
| gosec (C2, `-severity high -conf high`, `.gosec-exclude`) | PASS — 0 HIGH findings (553 dev-level informational, policy-suppressed) |
| Missing locale keys (en, de, es, fr, ja) | PASS — 0 missing (the twelve `federation_observer_pull*` keys and the three `stats_observer_pull*` Monitor card keys are all present in all five bundles) |
| Tracker-ID matches in the observer howto | PASS — 0 matches |

## Not run (recorded, not claimed)

- **TS-844** (`scripts/test-stories/pwa/observer-pull-card.mjs`): `node --check`
  and `bash -n` pass; the story itself needs a Playwright sandbox daemon built
  from this tree, which is not reachable from this worktree. This is the live
  verification the rules-review pass flagged as skipped; it is outstanding and
  listed in the "Still owed" line of `docs/testing.md` (2026-11-06 section) —
  not claimed as done here.
- No daemon was started against this gate; all rows above are repository-level.

## Overall

**PASS** on every repository-level check. The one owed item is the TS-844
live story run, disclosed above rather than counted as done.
