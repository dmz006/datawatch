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

## Live-daemon run record (2026-10-29)

Run on a freshly built binary (`go build -o datawatch ./cmd/datawatch/`), a
scratch daemon with its own empty data directory and its own `HOME`, ports
that are not the production ones, `env -u TMUX -u TMUX_TMPDIR`. No production
daemon was touched; the scratch daemon was ended by its exact PID and exited.

| # | Check | Result |
|---|-------|--------|
| 1 | `GET /api/config` on the fresh daemon: `reset_zone` is the daemon's local zone (`America/New_York`), `reset_zone_source: server`, `reset_zone_fell_back: false` | pass |
| 2 | `SMOKE_ONLY=85 DW_BASE=<scratch daemon> bash scripts/release-smoke.sh` (external-daemon mode; the self-started daemon mode was ended by a SIGTERM from outside the daemon before section 85 on two tries, so the section was pointed at a daemon the script does not manage) | Pass 18, Fail 0, Skip 2 (the two CLI checks, which external mode skips) |
| 3 | The smoke reset-zone checks inside that run: config contract fields, advice `zone` + `server_zone` and the text markers, explicit zone pin (source `configured`, marker gone), restore with `""` | pass x3 |
| 4 | CLI `datawatch config get usage.calculator.reset_zone` on the unset zone | `America/New_York (server zone)` |
| 5 | After `PUT usage.calculator.reset_zone = Europe/Lisbon`, the same CLI call | `"Europe/Lisbon"` (no marker); `PUT ""` restores `America/New_York (server zone)` |
| 6 | Daemon restarted with `TZ=Europe/Lisbon` and the zone unset: `GET /api/config` | `reset_zone: Europe/Lisbon`, source `server` |
| 7 | Served PWA bundle (`/app.js`) carries the `settings_usage_calculator_reset_zone_server_note` key | pass |

Additional re-run in this fix pass: `go vet` on the four touched packages,
`go test` of `internal/config`, `internal/usagecalc`, the zone-related server
tests (`UsageCalc`, `EffectiveUsage`, `Locales`) and the CLI `ConfigGet` test,
`node --check internal/server/web/app.js`, and
`node internal/server/web/app-usage-calculator-settings.test.js` — all pass;
`gofmt -l` lists none of the changed Go files.

## Rules-audit tokens for this unit

This PRD unit is treated as one sprint for the per-sprint rules audit. The same
tokens are carried in the final commit message.

| Row | Token |
|-----|-------|
| A1 | rules: Localization, Testing Requirements, Testing Tracker, Release testing (B12), Mobile-Parity, Config-Parity, Documentation checklist |
| A2 | tests: ok (full `go test ./...` on 2026-10-29: 82 packages ok, exit 0, 160 s) |
| A3 | version: N/A — the release manager bumps both version files at merge |
| A4 | changelog: added |
| A5 | readme: N/A — no release line change before merge |
| A6 | backlog: N/A — no plan backlog item closed by this unit |
| A7 | id-check: clean |
| A8 | leak-check: clean |
| A9 | node-check: ok |
| B1 | tracker: GET /api/config contract changed; GET /api/usage/advice contract changed |
| B2 | locales: settings_usage_calculator_reset_zone, settings_usage_calculator_reset_zone_hint, settings_usage_calculator_reset_zone_server_note |
| B3 | locale-guard: added settings_usage_calculator_reset_zone_server_note |
| B4 | mobile-parity: datawatch-app#297 |
| B6 | config-parity: verified |
| B12 | smoke-extended: 85 Usage calculator (reset-zone checks) |

## Result

result: pass
