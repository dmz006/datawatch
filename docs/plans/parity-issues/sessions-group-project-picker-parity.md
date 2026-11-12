# [parity] Sessions grouping: Automaton-aware project source and a picker that keeps its value (datawatch-app follow-up)

**Filed:** https://github.com/dmz006/datawatch-app/issues/463 (datawatch v9.154.2). This is the tracked draft; the issue lives on the `dmz006/datawatch-app` repository. Filing the issue does not change a line of this repo. Draft written 2026-11-12.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

Two fixes to Sessions list grouping, in the web page and the daemon read:

- The `project` grouping reads, in order, the project of the session's Automaton (its worktree is mapped back to the repository and matched against the registered projects), then a `project:` tag on the session, then falls back to the working folder. `GET /api/sessions/groups` and the list page use the same rule, so Automaton sessions no longer land in folder-path groups.
- The group picker keeps its value: the choice is remembered on this device, restored after a reload and kept through list refreshes. The toolbar placement control and the picker are one choice.

There is no new setting and no new endpoint or field.

## What the mobile app needs

- If the app groups sessions by project, use the same source order (the session's Automaton project, then a `project:` tag, then the folder).
- If the app has a group picker, remember its value per device and keep it through refreshes.
- No new strings.
