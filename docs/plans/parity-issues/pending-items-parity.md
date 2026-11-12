# [parity] Pending items: Automation members for work with no Automaton yet (datawatch-app follow-up)

**Filed:** https://github.com/dmz006/datawatch-app/issues/461 (datawatch v9.154.0). This is the tracked draft; the issue lives on the `dmz006/datawatch-app` repository. Filing the issue does not change a line of this repo. Draft written 2026-11-12.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

An Automation can hold a place for work that has no Automaton yet. A pending item is a named piece of work with an optional outside label. It is inserted as an Automation member, the Automation holds there and says which item it waits for, and attaching an Automaton converts the member in place and the Automation carries on by itself (switchable). The item is marked done when that Automaton completes. The daemon can also copy items from a table in a file the operator keeps (read only).

- REST: `GET` and `POST /api/autonomous/pending-items`, `POST /api/autonomous/pending-items/{id}/attach`, `POST /api/autonomous/pending-items/refresh`; `POST /api/automations/{id}/insert_member` takes `member.pending_item_id` in place of `prd_id`.
- MCP: `autonomous_pending_items`, `autonomous_pending_item_add`, `autonomous_pending_item_attach`, `autonomous_pending_items_refresh`; `automation_insert_member` takes `pending_item_id`.
- CLI and chat: `autonomous pending-items`, `pending-item-add`, `pending-item-attach`, `pending-items-refresh`; `automation insert --pending-item` and `pending_item=<id>`.
- Web: a **Pending items** card on the Automation view's Housekeeping sub-tab (list, add, attach, refresh), a **Waiting for an Automaton** badge with an attach control on a held member, an insert picker, and a Settings group.

## New config keys

Nine keys under `autonomous.automation`, returned and accepted by `GET` and `PUT /api/config` and shown in Settings > Automata:

| Key | Default | Meaning |
|---|---|---|
| `pending_item_policy` | `hold` | `hold` blocks the Automation at a waiting item, `skip` passes it by |
| `pending_item_auto_resume` | `true` | A held Automation continues when an Automaton is attached |
| `pending_item_source.path` | empty | File registry to read; empty means none |
| `pending_item_source.id_pattern` | `^\S+$` | Regular expression for an item id |
| `pending_item_source.id_column`, `.title_column`, `.state_column` | `1`, `2`, none | Header name or 1-based column number |
| `pending_item_source.done_values` | done, closed, shipped | Words that mean done |
| `pending_item_source.on_missing_row` | `keep` | `keep` or `drop` when a row disappears |

## What the mobile app needs

- A Pending items view (list, add with an optional label, attach an Automaton, refresh), the held-member badge with the attach control, and the insert picker for a pending item.
- The nine Settings fields above, in all five locales (the web page added the `pending_item_*` and `settings_*pending_item*` strings).
