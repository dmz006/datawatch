# [sync] Verifier run cap in verifier stats: run_cap_seconds, run_cap_ends and the run_cap category

## Affected area

Verifier statistics for the autonomous (Automata) verifier. This work adds the run cap to the rolling verifier aggregates in `internal/autonomous/verifier_stats.go`. The aggregates are carried in the capacity response under `verifier`. The new fields are `run_cap_seconds` (the cap in effect, 0 meaning no cap) and `run_cap_ends` (runs ended by the cap). A new outcome category, `run_cap`, is added to `categories`, and the one-line status text gains a `run_cap N` segment when the count is non-zero. The cap value is wired from config at daemon start in `cmd/datawatch/main.go`. Covered by `internal/autonomous/verifier_stats_run_cap_test.go`, `internal/autonomous/verifier_stats_cap_count_test.go` and `cmd/datawatch/verifier_run_cap_stats_test.go`.

Clients affected: the PWA capacity and verifier views, the Android and iOS apps (datawatch-app), the CLI status line, MCP capacity output and the comm channel status text.

## Current behavior

The daemon keeps a count of verifier runs by outcome. A run that the cap ended is counted once, under `run_cap`, and that category wins over every other category, escalated included. The verifier view reports `run_cap_seconds` as the cap in effect, so an operator can see the limit next to the counts. The one-line status text shows `run_cap N` only when N is above zero, so a daemon without cap ends prints the same line as before.

The cap value is set at daemon start from the `autonomous.verifier_max_run_seconds` config value. The unit tests check the zero state, the set and clear behavior, negative values being treated as zero, the JSON key names, the category mapping and the line format. The cmd test checks the default of 2700 seconds and the zero case, but it calls a local copy of the wiring rather than the code in `cmd/datawatch/main.go`, so it does not prove the daemon wiring itself.

No client renders these fields yet. The apps and the PWA do not know `run_cap_seconds`, `run_cap_ends` or the `run_cap` category, so an operator sees a run ended by the cap only as an unlabelled count, or not at all, depending on the view.

## Expected behavior

Every surface that shows verifier stats renders the same three facts with the same meaning:

- The cap in effect, shown in seconds or as a readable duration, with 0 shown as "no cap" and never as a zero-second limit.
- The count of runs ended by the cap, labelled as a run-cap end and shown only when it is above zero, so the view stays quiet on a normal daemon.
- The `run_cap` category as its own labelled bucket in the category breakdown, next to pass, content, infra, watchdog, timeout, capacity and escalated.

Clients read the values from the capacity response and do not compute them. A run ended by the cap must read differently from a watchdog end or a timeout, because the operator's next step is different: a capped run is a sizing question, not a stuck session.

The cap value shown by the stats should match the value the daemon enforces at the time of the view. A change made through the config API should show in the stats without a daemon restart, or the view should say that a restart is needed. Today the value is set once at start, so a live change may not show until restart. This needs to be confirmed in the daemon code before the clients rely on it.

## Suggested next step

1. Confirm in `cmd/datawatch/main.go` and the config update path whether `SetRunCapSeconds` runs again after a live change to `autonomous.verifier_max_run_seconds`. If it does not, add the call to the config apply path so the stats track the setting. Replace the local copy in `verifier_run_cap_stats_test.go` with a test that calls the real wiring.
2. Once that is settled, file the datawatch-app parity issue for the verifier stats view. It should cover the three fields above, the `run_cap` category label and the zero-means-no-cap rule, with label text in all five locales.
3. Update the PWA capacity verifier view in the same change, since the PWA is the reference render.
4. Update the CLI status line and the MCP and comm channel output only if they already print the verifier line. The `run_cap N` segment comes from the shared formatter, so it should appear there without extra work, and the tests should confirm it.
5. Leave the enforcement path (the session end hook that sets the cap category) out of this parity work. It belongs to the verifier run cap enforcement draft, not to the stats view.

Open questions for the operator: whether the apps should show a warning colour for a non-zero `run_cap_ends`, and whether the cap value should appear on the capacity card or only in the verifier detail view. Both are design choices and are not decided here.
