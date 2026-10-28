# feat(settings): add stuck_running_seconds setting parity

Parent release: datawatch 9.29.2 (the next patch above main at the time of writing).

## What changed in datawatch

A daemon watchdog now catches an Automaton that sits in `running` with every task finished and no active session. After `autonomous.stuck_running_seconds` the daemon re-runs the completion rollup once. If the Automaton is still stuck after a second full bound it moves to `blocked`, with a recorded decision, an audit entry and a status event. The bound is a new setting on the Automata card of the PWA Settings page.

## Setting to add to the mobile app

| Item | Value |
|---|---|
| Config key | `autonomous.stuck_running_seconds` |
| REST field | `stuck_running_seconds` under `autonomous` in `GET`/`PUT /api/config` |
| Label | Stuck Automaton timeout (seconds) |
| Type | whole number, seconds |
| Default | 900 (shown as the placeholder; 0 or empty means use 900) |
| Range | 0 or more; no upper bound (0 = default) |
| Placement | Automata settings card, next to the other watchdog bounds (worker idle nudge, orphan grace) |

## New locale keys

| Key | EN | DE | ES | FR | JA |
|---|---|---|---|---|---|
| `settings_stuck_running_seconds` | Stuck Automaton timeout (seconds) | Timeout für festhängenden Automaten (Sekunden) | Tiempo límite de autómata atascado (segundos) | Délai d'un automate bloqué (secondes) | 停止したオートマトンのタイムアウト（秒） |
| `settings_stuck_running_seconds_help` | How long an Automaton may stay running with every task finished and no live session before the watchdog re-checks it once, then blocks it. 0 uses the default (900). | Wie lange ein Automat im Status „läuft“ bleiben darf, obwohl alle Aufgaben beendet sind und keine aktive Sitzung existiert, bevor der Wächter ihn einmal erneut prüft und danach blockiert. 0 verwendet den Standardwert (900). | Cuánto tiempo puede un autómata permanecer en ejecución con todas las tareas terminadas y sin sesión activa antes de que el vigilante lo revise de nuevo una vez y luego lo bloquee. 0 usa el valor predeterminado (900). | Durée pendant laquelle un automate peut rester en cours avec toutes ses tâches terminées et sans session active avant que le chien de garde le revérifie une fois, puis le bloque. 0 utilise la valeur par défaut (900). | すべてのタスクが完了しアクティブなセッションがないままオートマトンが実行中の状態でいられる時間。この時間を超えると監視が一度再確認し、その後ブロックします。0 の場合は既定値（900）を使用します。 |

DE, ES, FR and JA values are the ones shipped in the datawatch PWA locale files; please review them against the app's own terminology for "Automaton".

## Acceptance

- The field appears in the app's Automata settings, reads the current value from the daemon config and writes it back through the config endpoint.
- Label and help text are localized in EN, DE, ES, FR and JA.
- An Automaton that the watchdog moves to `blocked` already shows as blocked through the existing status event; no new status value is added.

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/290 (2026-10-28, via `gh issue create --repo dmz006/datawatch-app`).
