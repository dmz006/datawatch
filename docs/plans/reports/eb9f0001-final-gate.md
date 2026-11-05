# eb9f0001 final gate report

Counts only. Overall result: FAIL

The gate script could not run as written because its helper and the two check scripts it names do not exist in the worktree. The checks were run directly instead, with no files changed.

| Check | Result | Detail |
|---|---|---|
| go vet | pass | no findings |
| go test (whole module) | pass | 88 packages ok, 0 failed, 12 without test files |
| PWA node suite (full run) | fail | 2035 tests, 2034 passed, 1 failed |
| PWA node suite (failing file alone) | pass | 17 tests, 17 passed, 0 failed |
| locale check | not run | no dedicated check exists; locale tests are inside the node suite above |
| docs check | pass | internal-reference and howto-coverage scripts exit 0; the docs-sync script was skipped because it would write to an ignored directory |

The single node failure does not reproduce when its file runs alone, so it looks order or load dependent. It is reported, not fixed.
