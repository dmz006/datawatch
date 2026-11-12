# [parity] Sessions grouping by node, group header titles and compact tiles (datawatch-app follow-up)

**Filed:** https://github.com/dmz006/datawatch-app/issues/465 (datawatch v9.154.5). This is the tracked draft; the issue lives on the `dmz006/datawatch-app` repository. Filing the issue does not change a line of this repo. Draft written 2026-11-12.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

Sessions grouping, in the web page and the daemon's grouped read:

- The `node` axis puts a federation peer's sessions under the peer's name and this daemon's under its real hostname; `local` is never a group name. In the grouped read (REST, MCP, CLI, chat) a session with no hostname is keyed under this daemon's hostname. The web page reads peers' sessions through `/api/sessions/aggregated` for the node and Automaton axes.
- A group header on the Automaton, project and node axes shows the record's human title with the id as secondary text; the ungrouped group always sorts last; a folder with a trailing slash is one group.
- Compact is the tile grid drawn at a smaller size, never rows.

There is no new setting, endpoint or field.

## What the mobile app needs

- If the app groups sessions by node, use the same keys (peer name, else the session's hostname) and never a group named `local`.
- Show a title with the id as secondary text on Automaton, project and node group headers, and sort the ungrouped group last.
- No new strings are required beyond the group header text the web page already uses.
