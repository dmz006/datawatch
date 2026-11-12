# [parity] Automata timeline: the all-projects interval chart (datawatch-app follow-up)

**Filed:** https://github.com/dmz006/datawatch-app/issues/466 (datawatch v9.155.0). This is the tracked draft; the issue lives on the `dmz006/datawatch-app` repository. Filing the issue does not change a line of this repo. Draft written 2026-11-12.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

A read-only interval chart of the Automata across all projects: one project at a time, each Automaton with its stages, every task as a bar with start and end, the stretches a task spent blocked or waiting with the reason, and a projected finish with its reason for each running Automaton. It reads existing Automata records and stores nothing.

- REST: `GET /api/autonomous/timeline` returns `{today, projects[], counts}` and takes `project`, `automaton`, `stage`, `kind` and `tag` filters.
- MCP: `autonomous_timeline` (read only). CLI: `datawatch timeline`. Chat: `timeline`.
- Web: a **Timeline** tab on the Automaton detail page and an all-projects Gantt card on the Progress tab.

There is no new setting.

## What the mobile app needs

- A timeline view (per project, per Automaton and all projects) drawn from `GET /api/autonomous/timeline`: bars for members and stages, blocked or waiting stretches with their reason, and the projected finish with its reason.
- The web page added `timeline_card_*` strings (and the Timeline tab label) in all five locales; reuse the wording where the app has the same controls.
