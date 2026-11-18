# [parity] Grouped, searchable Sessions tag filter (datawatch-app follow-up)

**Status:** draft, not filed. The issue would live on the `dmz006/datawatch-app` repository; this file changes no behavior in this repo. Draft written 2026-11-12.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

The PWA Sessions page's tag filter (inside the Filters sheet) no longer draws one chip per tag. It has a tag search box; chips grouped under collapsible Kind, Role, Lane, Compute node, Project, Your tags and Other headers (six chips per group, then **Show all (N)**); chip counts taken from the sessions in the current list; no chips for `automaton:`, `stage:`, `task:` or `story:` tags, with a **Find an Automaton** picker (title first, id secondary) instead; and a row of removable chips for the selected tags above the session list with a **Clear all** control. 18 new strings in all five locales (`sessions_filters_tag_*`).

There is no new setting, endpoint, MCP tool, CLI command or chat verb.

## What the mobile app needs

- Decide whether the app's session filter should group and search tags the same way, and show the selected tags as removable chips.
- Reuse the 18 `sessions_filters_tag_*` strings where the app has the same controls.

## Operator review

- Decide where the equivalent control sits in the app (design question, not decided here).
