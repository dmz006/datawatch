# Node placement final gate

Run from `<worktree>` on branch `automaton/69810001`, every command wrapped with `env -u TMUX -u TMUX_TMPDIR`. No daemon was started.

| # | Command | Result |
|---|---------|--------|
| 1 | `go build -o datawatch ./cmd/datawatch/` | PASS |
| 2 | `go vet ./internal/capacity/ ./internal/autonomous/ ./cmd/datawatch/` | PASS |
| 3 | `go test ./internal/capacity/ ./internal/autonomous/ ./internal/config/ -count=1` | PASS (capacity ok, autonomous ok, config ok) |
| 4 | `go test ./cmd/datawatch/ -run 'Capacity\|Node' -count=1` | PASS |
| 5 | `.datawatch/verify/node-placement-fix.sh` | PASS (gofmt clean, build, capacity/autonomous/cmd tests ok) |
| 6 | `.datawatch/verify/node-placement-docs-version.sh` | **FAIL** |
| 7 | `.datawatch/verify/node-repro-tests.sh` (extra) | **FAIL** |
| 8 | `scripts/check-no-internal-refs.sh` (extra) | PASS |

## Failures, with output

**6. node-placement-docs-version.sh**, exit 1:

```
CHANGELOG.md not changed
```

The version check passed (both Version vars match, see below). The failure comes from the script, not the branch. It runs `git diff --name-only HEAD`, which lists only uncommitted changes, and this branch's work is already committed. The branch does change `CHANGELOG.md`, `docs/config-reference.yaml` and `docs/implementation.md` against the base commit, and `CHANGELOG.md` contains a `v9.56.1` section. Not fixed here, per the gate rules.

**7. node-repro-tests.sh**, exit 1:

```
no changed Go files
```

It uses the same `git diff HEAD` pattern, so it sees no changed Go files on a committed branch. The checks it wraps (vet, the three test packages, the cmd tests) were run directly as items 2 to 4 and passed. Not fixed here.

## Repro tests (all in the passing runs above)

- Verifier on a full node: `TestNodeOverAdmit_VerifierWithChoicesTakesIdleNodeOrWaits`, `TestNodeOverAdmit_PinnedVerifierWaitsOnFullNode`, `TestRespawnAdmit_VerifierRespawnReplacesHeldLease`, `TestRespawnAdmit_VerifierRespawnPrefersFreePreviousNode`, `TestRelaunchPlacement_VerifierHeldLeaseIsReleased`
- Resumed worker: `TestNodeOverAdmit_AdoptWithAlternativeNodeNeverOverLimit`, `TestNodeOverAdmit_AdoptCountsSessionOncePerNode`
- Restart ordering: `TestNodeOverAdmit_RestartAdoptedFirstThenNewRequestUsesIdleNode`, `TestNodeOverAdmit_RestartRequestBeforeAdoptionDoesNotTakeResumedSlot`
- Pool by node: `TestNodeOverAdmit_PoolCapCountsByNodeNotAcrossNodes`, `TestNodeOverAdmit_PoolCapTwoSessionsOnTwoNodesThirdRefused`

## Version sync

- `cmd/datawatch/main.go`: `var Version = "9.56.1"`
- `internal/server/api.go`: `var Version = "9.56.1"`
- Match: yes.

## Summary

Build, vet, all test suites and the fix verify script pass. The two verify scripts that diff against `HEAD` fail only because the work is committed, not because of a code defect.
