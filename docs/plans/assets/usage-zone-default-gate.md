# Usage zone default gate — final report

Scope: verify, without changing any source file, that the usage calculator's
`usage.calculator.reset_zone` behaves as specified — empty means "follow the
server zone", an explicit zone (including `UTC`) wins verbatim, an
unresolvable host zone degrades to `UTC` with an advice note, daylight saving
still works, the five locale files carry the reset-zone hint, and the shipped
config reference documents the empty default.

## Commands and exit statuses

| # | Command | Exit status |
|---|---------|-------------|
| 1 | `env -u TMUX -u TMUX_TMPDIR go build ./internal/... ./cmd/...` | 0 |
| 2 | `env -u TMUX -u TMUX_TMPDIR go vet ./internal/config ./internal/usagecalc ./internal/server ./cmd/datawatch` | 0 |
| 3 | `env -u TMUX -u TMUX_TMPDIR go test ./internal/config ./internal/usagecalc ./internal/server ./cmd/datawatch` | 0 (all four packages `ok`; `internal/config` and `internal/usagecalc` cached, `internal/server` 62.207s, `cmd/datawatch` 110.578s) |
| 4 | `bash scripts/check-no-internal-refs.sh` | 0 (PASS: no internal identifiers in user-facing surfaces) |
| 5 | `bash scripts/check-no-unscoped-tmux.sh` | 0 (OK: no unscoped session kills) |

## Confirmations

1. **Unset follows a faked TZ** — pass.
   `ResolveUsageServerZone` (internal/config/usage_calculator.go) tries the `TZ`
   environment value first via the injectable `usageServerZoneEnv` hook, and
   `TestEffectiveUsageResetZoneUnsetFollowsFakeTZ`
   (internal/config/usage_calculator_test.go) fakes `TZ=America/New_York` and
   asserts `EffectiveUsageResetZone` returns it with `serverZone=true,
   fellBack=false`; it ran green in command 3.

2. **An explicit value (including UTC) wins and is not rewritten** — pass.
   `EffectiveUsageResetZone` returns a non-empty configured zone unchanged with
   `serverZone=false, fellBack=false`, and `TestEffectiveUsageResetZoneExplicitWins`
   (internal/config/usage_calculator_test.go) covers both `UTC` and
   `Europe/London` against a faked `America/New_York` host, asserting no
   rewrite and no warning line when normalising an explicit `UTC`.

3. **An invalid TZ falls back to UTC with the advice note** — pass.
   A fixed offset is rejected by `usageFixedOffsetZone` and, when no host
   candidate resolves, `EffectiveUsageResetZone` returns `"UTC"` with
   `fellBack=true`; `TestEffectiveUsageResetZoneInvalidTZFallsBack`
   (internal/config/usage_calculator_test.go) covers the `UTC+5` host case, and
   `TestResetZoneFellBackNote` (internal/usagecalc/reset_test.go) pins the
   advice note ("…fell back to UTC… set usage.calculator.reset_zone
   explicitly to pin it") produced in internal/usagecalc/model.go when
   `Reset.FellBack` is set.

4. **Daylight saving still works** — pass.
   `TestResetDaylightSavingChange` (internal/usagecalc/reset_test.go) verifies
   a 169-hour fall-back week and a 167-hour spring-forward week around the
   America/New_York transition, that the reset stays Sunday 08:00 local on both
   sides but the offset moves from -4h to -5h;
   `TestDailyUseBoundariesFollowDaylightSaving`
   (internal/usagecalc/daily_test.go) covers the daily-boundary side. Both ran
   green in command 3.

5. **The five locale files are valid and the guard tests pass** — pass.
   internal/server/web/locales/ holds exactly five JSON files (de, en, es, fr,
   ja), each parses as JSON and each carries the
   `settings_usage_calculator_reset_zone_hint` key documenting the empty-zone
   and UTC-fallback semantics. The locale guard tests in
   internal/server/v5280_locales_test.go (`TestLocales_AllPresent`,
   `TestLocales_ParityWithEnglish`, `TestLocales_CommonNavKeysPresent`) ran
   green in command 3.

6. **Config reference ships an empty value** — pass.
   docs/config-reference.yaml line 2228 ships `reset_zone: ""` (with the
   explanatory comment at line 1247 that empty means the daemon's own zone),
   i.e. the shipped default is not a pinned `UTC`.

## Result

result: pass
