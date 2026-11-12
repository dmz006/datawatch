# [parity] Compact Sessions page controls: one toolbar row, Filters sheet and search (datawatch-app follow-up)

**Filed:** https://github.com/dmz006/datawatch-app/issues/460 (datawatch v9.153.0). This is the tracked draft; the issue lives on the `dmz006/datawatch-app` repository. Filing the issue does not change a line of this repo. Draft written 2026-11-12.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

The Sessions page keeps its controls in one toolbar row so it fits a phone: a search box, a **Filters** button with a badge counting the active filters, a chip per server and the List, Tiles and Compact toggle. Grouping, saved views, the full set of state chips, the watched-only toggle, the backend chips and the tag filter moved behind the Filters button into a sheet (closes on an outside tap, its close button or Escape). The state counts take one line and still filter. On a narrow screen a session's tags collapse to one `+N` chip. The search box matches name, id, backend, tag text and state, shows an `N of M` count while active and remembers its text per device. Fold all and Unfold all buttons act on every group.

There is no new setting, endpoint, MCP tool, CLI command or chat verb: the daemon behaves exactly as before.

## What the mobile app needs

- Decide whether the app's session list should offer the same one-row layout: a search over name, id, backend, tags and state with an `N of M` count, and one Filters entry that holds grouping, saved views, state, backend and tag filters.
- The web page added 21 strings in all five locales (`sessions_search_*`, `sessions_filters_*`, `sessions_summary_clear_tip`, `sessions_summary_filter_tip`, `sessions_fold_all`, `sessions_unfold_all`, `sessions_fold_all_tip`, `sessions_unfold_all_tip`, `sessions_tags_less`, `sessions_tags_collapse_tip`, `sessions_tags_expand_tip`); reuse the wording where the app has the same controls.
