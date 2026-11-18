# [parity] Sessions view mode and the lineage-tree toggle (datawatch-app follow-up)

**Status:** draft, not filed. The issue would live on the `dmz006/datawatch-app` repository; this file changes no behavior in this repo. Draft written 2026-11-12.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

With no grouping, the PWA Sessions page's List, Tiles and Compact buttons did nothing when a lineage-tree flag (`cs_session_tree_view`) was saved: the tree overrode every mode, and its toggle is hidden while the toolbar is collapsed. Now the tree shows only in List mode, and clicking any mode button clears the flag. No setting, endpoint, MCP tool, CLI command or chat verb changed.

## What the mobile app needs

- Check that the app's session list does not let a saved tree or lineage choice override the chosen layout.

## Operator review

- Confirm the app has an equivalent tree toggle at all before filing.
