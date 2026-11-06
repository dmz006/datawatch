# Final gate — observer pull mode (PRD ec310001), rules-review findings fix

Date: 2026-11-06
Scope: the observer pull protocol (federated daemon pulls a peer's v2 stats
instead of the peer pushing) and the five rules-review findings against it:
the unread `observer.pull.enabled` gate, the missing startup reconcile, the
missing PWA Settings section (three keys) and Monitor card, the missing
locale keys (three, in five bundles), and the missing PWA story (TS-844).
No version change, no gh, no daemon started in this worktree.

## Checks

| Check | Result |
|---|---|
| `go build ./...` | PASS — no output |
| `gofmt -l .` | PASS — 0 files (the 2026-11-05 gofmt FAIL — 3 unformatted files — is fixed; alignment to the Go 1.27.2 gofmt) |
| `go test ./...` | PASS — 11087 passed, 0 failed in 100 packages (includes `TestObserverPullGlobalGate_DefaultsOff`, `TestObserverPullFetch_DisabledGateBlocksRequest`, `TestObserverPullReconcile_Lifecycle`, `TestObserverPullReconcile_NoStoreSafe`, `TestObserverPullConfig_DefaultsDisabled`, and the locale guard `TestLocales_CommonNavKeysPresent` with the three new keys) |
| `node --test internal/server/web/*.test.js` | PASS — 2026 of 2026, 0 failures |
| `golangci-lint run ./...` | PASS — no issues (the four unchecked `fmt.Fprintf` writes in `cmd/datawatch/cli_observer.go` now assign to `_`) |
| Missing locale keys (en, de, es, fr, ja) | PASS — 0 missing (the twelve pull-mode keys and the three new `stats_observer_pull*` Monitor card keys are all present in all five bundles) |
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
