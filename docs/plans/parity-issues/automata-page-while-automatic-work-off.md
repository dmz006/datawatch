# Parity: show the Automata pages while automatic work is off (datawatch-app)

Draft only; no issue filed.

## Web behaviour

The web app reads `GET /api/autonomous/config` once at start.

- The request answers, with `enabled` true or false: the Automata and Dashboard
  buttons, the Automation tab, remembered views and Automation links all show.
- The request fails (daemon down, refused token, no engine): they stay hidden.

No REST, MCP, CLI or chat change. The buttons on those pages behave as before,
so some still start agent work while automatic work is off (see the how-to
`automation-tab.md`).

## Mobile client

Check whether the app hides these screens when `enabled` is false. If it does,
apply the same rule. The app was not checked for this draft.
