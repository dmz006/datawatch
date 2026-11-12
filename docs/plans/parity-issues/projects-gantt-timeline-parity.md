# [parity] Projects Gantt as an expandable timeline with a Focus view (datawatch-app follow-up)

**Not filed.** Draft written 2026-11-12; the issue belongs on the `dmz006/datawatch-app` repository and is for the operator to file. Writing this file changes no mobile code.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

The Projects page Gantt in the web app is now one expandable timeline (arcs, then Automata, then tasks, opened on demand) with a **Focus** view per Automation. It reads `GET /api/projects/{name}/timeline` and stores nothing in the daemon.

- Web: arc rows with bars from first to last task, expand and collapse, **All time** / 90 / 30 / 7 day presets, wheel zoom and drag pan, an **Automata with no arc** group, a 40-row cap with **Load more**, expansion remembered per project in the browser, and a **Focus** button on an arc row, an arc card and the Automation page header.
- The old 7/30/90-day window selector and the history Gantt rows in the page are gone.
- No new setting, REST route, MCP tool, CLI command or chat verb in this change (those shipped earlier with the timeline read).

## What the mobile app needs

- A project timeline view drawn from the timeline read, one level per call, with the Focus view using `focus=<id>`.
- The new `projects_gantt_*`, `automation_focus_button` and `projects_gantt_focus_*` strings exist in all five locales; reuse the wording.

## Surfaces

| Surface | Touched |
|---|---|
| Web app | Yes |
| REST, MCP, CLI, comm, YAML | No (timeline read already shipped) |
| Android, iPhone | Not changed here; tracked in datawatch-app |
