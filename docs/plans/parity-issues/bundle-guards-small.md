# [sync] Worker tmux kill guard setting, restart-needed keys on config saves, planning refusals on LLM setters

Draft only. No issue has been filed on any tracker.

## Affected area

The Android and iPhone apps: the autonomous settings page, any config save, and the Automata LLM override actions. The PWA already has all of it.

## What changed in the daemon

1. **Worker tmux kill guard.** New setting `autonomous.tmux_kill_guard`, a boolean, default on (unset reads on). On, every worker, verifier and judge pane the daemon starts has a guard `tmux` first on its PATH that refuses a server-wide kill unless the call names a throwaway socket. Read and written with `GET` and `PUT /api/config` (key `autonomous.tmux_kill_guard`). PWA: Settings, autonomous card, switch "Worker tmux kill guard", locale key `settings_tmux_kill_guard` (help text) in en, de, es, fr and ja. The apps need the same switch with the same help text.
2. **Restart-needed keys.** `PUT /api/config` now answers with `applied` (saved keys that took effect now) and `requires_restart` (saved keys that wait for a daemon restart, for example `server.port`); both are omitted when empty and a key listed under `skipped` is in neither. The apps should show a "restart needed" notice when `requires_restart` is not empty and offer the daemon restart action. PWA: a toast and a "Restart needed (n)" badge that restarts the daemon when pressed; locale keys `settings_restart_pending_badge` and `settings_saved_restart_needed` in en, de, es, fr and ja.
3. **Planning refusals on LLM setters.** `POST /api/automations/{id}/set_llm` (backend, model or effort), `POST /api/autonomous/prds/{id}/set_story_llm` and `set_task_llm` now answer HTTP 409 while an Automaton is planning (for an Automation: while any member Automaton is planning), the same as the Automaton `set_llm` already did. Nothing is changed; retry after the plan ends. A planning-only change (`planning_backend`, `planning_model`, `decomposition_*`) is still accepted. The apps should show the refusal text and not treat a 409 here as a generic failure.
4. **Safeguard alert for a forced run of shipped work.** `POST /api/autonomous/prds/{id}/run` with `{force: true, reason}` now raises the same safeguard alert as a pause resume or an attempt-count force reset when the caller is not the web app and not a session credential. The apps that send this call should send the header `X-Datawatch-Client: pwa` only if they are the operator's own app; an app that does not send it raises the alert, which is the intended visibility.

## Gap on the chat channel

The comm channel `configure` reply still ends "Restart may be required." for every key. It does not use the new lists, because five existing tests pin that wording.

## Ask for the mobile apps

Add the kill guard switch, show the restart-needed notice from `requires_restart`, and surface the 409 text on the three LLM setters.
