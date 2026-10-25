# BL461 tag browsing and tile redesign: operator decisions

Date started: 2026-10-25. The ten open questions of `docs/plans/2026-10-20-tag-browsing-tile-design.md` are answered one at a time, with the operator's words quoted. The implementation PRD reads this file and does not ask again.

## Decisions

1. **Session tags: inherited read-only from the Automaton that spawned the session.** (Operator, 2026-10-25: "1, inherit read-only".) A session started by an Automaton appears under that Automaton's tags; a session started by hand has no tags and shows as untagged. No new session data and no session tag editing.

## Still open (asked in order)

2. Subtree counts or direct counts.
3. Whether `needs_review` is the only attention status, or `paused` joins it.
4. Whether the Automata list returns task counts and tags on each item.
5. Where the tag browser lives: the Automata tab or the Automation tab.
6. Whether a tag shown under several branches is acceptable.
7. Whether counts refresh on the event stream or on poll.
8. Whether tags are local to one node or federated.
9. The default match mode of the tag facet.
10. The registry source for the MCP tag tools.
