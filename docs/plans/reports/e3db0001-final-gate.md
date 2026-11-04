# e3db0001 final gate — counts only

Gate: build of the main binary plus `go test` of the six touched packages, run with the TMUX environment variables unset. No daemon started, no `gh` calls, no version variable changed, no files modified.

| Check | Result |
|---|---|
| Build of the main binary | PASS |
| Touched packages passed | 6 / 6 (0 failed) |
| Top-level tests run | 7827 (0 failed; 12 skipped) |
| Subtests run | 3698 (0 failed) |
| New tests added | 55 (39 Go test functions + 16 browser-bundle tests) |
| Locale keys added per locale — locale 1 | 9 |
| Locale keys added per locale — locale 2 | 9 |
| Locale keys added per locale — locale 3 | 9 |
| Locale keys added per locale — locale 4 | 9 |
| Locale keys added per locale — locale 5 | 10 (of which 4 lines duplicate key names already added in the same locale file) |
| Locale keys equal across the five locales | FAIL (10 vs 9; the fifth locale also backfills one previously missing key) |
| Tracker-ID hits in the branch diff | 0 (6 IDs checked) |
| No daemon started / no gh / no version bump | PASS |

**Overall: FAIL** — all build/test counts are green and tracker-ID hits are 0, but the per-locale added-key counts are not equal across the five locales (one locale carries 10 added keys, including 4 duplicated key names and 1 backfill, versus 9 in each of the other four).
