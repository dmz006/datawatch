# Final gate report

Counts only. One line per check.

| Check | Result |
|---|---|
| go vet, six listed packages | pass |
| go test, six listed packages | pass |
| go test ./... (whole repository) | fail: 1 failing test in 1 package, 99 other packages pass |
| make lint | fail: 4 errcheck findings, all other lint steps pass |
| check-no-internal-refs.sh | pass |

Skipped gates: none.
Nothing was fixed in this task.
