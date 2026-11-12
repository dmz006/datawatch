# [parity] Observed peers with no compute node and the unanswered auto-created report (datawatch-app follow-up)

**Filed:** https://github.com/dmz006/datawatch-app/issues/462 (datawatch v9.154.1). This is the tracked draft; the issue lives on the `dmz006/datawatch-app` repository. Filing the issue does not change a line of this repo. Draft written 2026-11-12.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

A monitoring peer's first push no longer creates a compute node from the connection's source address. A push only binds the peer to a node of the same name when one exists and never changes that node's address. A peer with no matching node is listed as **Observed peers with no node**: on the Compute page, as `observed_peers` in the node list REST response, in the CLI and chat `compute node list` output and in the `compute_node_list` MCP tool. Each has an **Add as a compute node** action that opens the node form with the name filled in and the address empty; nothing is created until the address is entered and saved. A review-only report, **Auto-created nodes that never answered** (`unanswered_auto_created`), lists nodes earlier versions created this way whose address never answered; nothing is deleted or disabled automatically.

There is no new setting.

## What the mobile app needs

- Show `observed_peers` from the node list response as a list with an Add action that opens the add-node form with the name filled and the address empty, and show `unanswered_auto_created` as a review-only list.
- The web page added 6 strings in all five locales: `compute_observed_peers_title`, `compute_observed_peers_hint`, `compute_observed_peer_add`, `compute_observed_peer_add_tip`, `compute_unanswered_title`, `compute_unanswered_hint`.
