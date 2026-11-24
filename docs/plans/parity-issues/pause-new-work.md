# Pause new work (global and per project) is missing from the Android and iPhone apps

**Status:** draft, not filed. File it against datawatch-app when the datawatch release that carries it ships.

## Affected area

A datawatch-level "paused by operator" state, global or per project. New work is not admitted while it is on; running tasks finish.

- `GET /api/pause` returns `{paused, global?, projects[]}`; each entry is `{scope, project?, reason, by, at}`. `?peers=1` adds `peers`, a map of each federation peer's own result (`{ok, error?, state?}`).
- `POST /api/pause` body `{scope: global|project, project?, reason, actor?, all_peers?, peers?}` pauses; a reason is required (400 without).
- `POST /api/pause/resume` body `{scope, project?}` or `{all: true}`, plus `note?`, `actor?`, `all_peers?`, `peers?`. The reply carries `cleared`. A 403 with the text `only the operator can resume paused work` is the daemon refusing a non-operator caller; show it verbatim.
- Capabilities: `pause:read`, `pause:write`.
- Setting `pause.start_paused` (bool, default false, read at daemon start).
- PWA: a header button (`#headerPauseBtn`, in `internal/server/web/app-pause.js`) that is dim when nothing is paused and amber with the text *Paused* (and a count when more than one) when something is; it opens a dialog listing each pause with who, why and when, a project field, a required reason, **Pause**, a per-entry **Resume** and **Resume all**. State is polled every 15 s.
- Locale keys: `pause_modal_title`, `pause_badge_paused`, `pause_badge_title_paused`, `pause_badge_title_idle`, `pause_hint`, `pause_none`, `pause_scope_global`, `pause_scope_project`, `pause_by`, `pause_project_label`, `pause_reason_label`, `pause_reason_required`, `pause_action_pause`, `pause_action_resume`, `pause_action_resume_all`, `pause_saved`, `pause_resumed` in every file under `internal/server/web/locales`.

## Current behavior

The daemon and the PWA carry all of it. The apps have no pause indicator and no control, so an operator on a phone cannot see that work is paused, pause it, or resume it.

## Wanted

- A status indicator on the main screen that follows `GET /api/pause`.
- A pause and resume control with the same fields and the same refusal text.
- Send to peers is optional for the first version.
- The same five locales.

## Parity surface

REST, MCP, CLI, comm channel, YAML/config and PWA are done. Android and iPhone are this draft.
