# BL461 tag browsing and tile redesign: operator decisions

Date started: 2026-10-25. The ten open questions of `docs/plans/2026-10-20-tag-browsing-tile-design.md` are answered one at a time, with the operator's words quoted. The implementation PRD reads this file and does not ask again.

## Decisions

1. **Session tags: both.** (Operator, 2026-10-25: first "1, inherit read-only", then "sessions may run outside of automation and have their own tag", then "1, both, set from the session detail and all surfaces".) A session spawned by an Automaton inherits that Automaton's tags. Any session, including one that runs outside any Automation, can also carry tags of its own. The session gets a new tags field. A session's own tags are set from the session detail page and from every surface that sets tags on Automata today: PWA, mobile apps, CLI, REST, MCP, the comm channel and the config surfaces that apply. Inherited tags are shown as inherited and are not edited on the session; they change with the Automaton.

2. **Counts: subtree counts as the main figure.** (Operator, 2026-10-25: "Q33 1 subtree counts".) Each tag shows the number of distinct items under it including its children, an item under two children counted once; the API may still return direct counts for views that want them.
3. **Attention statuses: `blocked`, `needs_review` and `paused`.** (Operator, 2026-10-25: "2".) A paused Automaton joins the attention group so every paused item is visible in one place; the `attention_statuses` setting still lets the operator change the list. **Sub-question answered (operator, 2026-10-25: "Go with your recommendations"):** an Automaton with a task held for operator review (`review_needed`) also appears in the attention group.
4. **List payload: add `tags` to each Automata list item and keep the existing task counts.** (Operator, 2026-10-25: "Go with your recommendations", for the supervisor's recommendation 1.) The tile shows progress from the counts the list already returns and tag chips from the new `tags` field, with no second request per tile; the three count fields stay as they are.

## Still open (asked in order)

5. Where the tag browser lives: the Automata tab or the Automation tab.
6. Whether a tag shown under several branches is acceptable.
7. Whether counts refresh on the event stream or on poll.
8. Whether tags are local to one node or federated.
9. The default match mode of the tag facet.
10. The registry source for the MCP tag tools.
