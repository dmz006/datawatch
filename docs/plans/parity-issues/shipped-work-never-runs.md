# [sync] Shipped work never runs: one setting, one lane fill reason, a force on run (datawatch next release)

Parent release: datawatch next release

Draft only, for the datawatch-app maintainers. No issue is filed.

## What changed in datawatch

The daemon now refuses to start, approve or decompose an Automaton whose own record says its work already shipped (a release and a merge commit are both set), and any Automaton that is archived, cancelled, completed or rejected. Queued work with a shipped record is dropped from every queue on each lane fill poll: it ends **delivered** (status `completed`, never `cancelled`) with the reason kept on the Automaton. A "Land X onto main" Automaton is the first case. Only the Automaton's own record counts: a shipped record on another Automaton (a dependency, a sibling) never blocks it, and an Automaton on hold or already running is never touched. This replaces the standing rule that only the doer session followed.

## Settings to add to the mobile app

- `autonomous.shipped_refusal`: string, `on` (default), `shadow` (log only) or `off`. Select control. Read and written with `GET` and `PUT /api/config`, and listed in `GET /api/autonomous/config` as `shipped_refusal` (the effective value). Applies live.

## Changed REST contract

- `POST /api/autonomous/prds/{id}/run` accepts an optional body `{"force": true, "reason": "..."}`. Without `force` the call is unchanged, except it can now answer 400 with a reason in the body: `automaton <id> already shipped in <release> (commit <short>); it is marked delivered and does not run (to run it anyway, force with a reason)` or `automaton <id> is <status> (closed work): it never runs`. With `force` a non-empty `reason` is required (400 otherwise); the force is audited with who, why and the merge commit it covers, and it covers only that commit. Closed work (archived, cancelled, rejected) cannot be forced. The app should show the 400 body text as the error.
- `GET /api/autonomous/lane-fill` decisions: `eligibility_reason` has a new value `already_shipped`.
- The Automaton read (`GET /api/autonomous/prds/{id}`) gains two optional objects: `shipped_refusal` (`reason`, `release`, `commit`, `from`, `at`: why the daemon dropped it as delivered and the status it had) and `closed_work_override` (`commit`, `actor`, `reason`, `at`: the operator's force). Decision kinds `shipped_refusal`, `closed_work_refused` and `closed_work_override` can appear in the decision list.

## New locale keys with English values

| Key | English |
|---|---|
| `settings_shipped_refusal` | Shipped-work rule (on refuses and drops work that already shipped or is closed, shadow only logs, off disables; default on) |
| `lane_fill_eligibility_already_shipped` | Already shipped |

The PWA ships real translations of `lane_fill_eligibility_already_shipped` in de, es, fr and ja; the settings label stays English in those four, as the neighbouring settings rows do.

## Screens

- Settings, Automata: the select row for `autonomous.shipped_refusal`.
- Lane fill card and decision rows: the `already_shipped` reason label.
- Automaton detail: show `shipped_refusal.reason` when present. A forced run is a deliberate operator action and has no PWA control; use the CLI, chat, MCP or REST. The mobile app needs none either.

## Not in this change

No PWA force button: forcing shipped work is rare and needs a reason, so it stays on the CLI, chat, MCP and REST.
