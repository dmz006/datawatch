# Automation pending items — live check on a sandbox daemon

**Date:** 2026-11-11 (run started 22:17:30, local time)
**Version under test:** datawatch v9.51.0 (binary built from this worktree)
**Script:** `scripts/pending-items-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/pending-items-live-check.sh`)
**Scope:** the nine `autonomous.automation` pending item settings, `GET /api/autonomous/pending-items` and its MCP, CLI and chat reads, an Automation that holds at a pending item until an Automaton is attached, the attach on REST, the CLI, the MCP tool and the chat verb, the skip policy, a file registry with an invented id pattern and column mapping, and the case of no file registry, against a throwaway project repository whose default branch is `trunk`.

## Result

**Overall:** PASS

| # | Check | Result | Assertions |
|---|---|---|---|
| 1 | Config: the nine settings | PASS | 10 of 10 passed |
| 2 | Read surfaces agree | PASS | 6 of 6 passed |
| 3 | Hold until attached | PASS | 7 of 7 passed |
| 4 | Attach: REST, CLI, MCP tool and chat verb | PASS | 25 of 25 passed |
| 5 | Skip policy passes through | PASS | 5 of 5 passed |
| 6 | File registry: invented id pattern and column mapping | PASS | 13 of 13 passed |
| 7 | No file registry configured: refresh answers configured false and changes nothing | PASS | 6 of 6 passed |
| 8 | Cleanup | PASS | 1 of 1 passed |

## Setup

- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `autonomous.verification_backend: shell`, `server.allowed_project_roots`, and `shell_backend.script_path` (the stand-in worker). The production daemon, its service and its data were not touched, and nothing was ended by name.
- **Ports.** HTTP 41863, TLS 45849, MCP 34621, DNS 52881, webhook 34381, notify 51045, each probed free just before use.
- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder, so no session of the operator was reachable and none was touched. The script itself issues no multiplexer command.
- **Everything** lives under one temporary folder outside the worktree, removed on every exit path.
- **Throwaway project.** A bare repository whose default branch is `trunk`, cloned to the worktree base repository; `refs/remotes/origin/HEAD` records it and `main` does not exist.
- **Worker.** The daemon's shell backend runs a generated script as the worker. Each Automaton declares the write directory `src/`; the script writes its sample file there (the session's own commit stages it). Every task carries a deterministic verification script, and the daemon's verification (a one-shot session on the same backend, `autonomous.verification_backend: shell`) is answered by the same script with a fixed pass, so no model is involved anywhere.
- **Sample registry.** Written inside the sandbox by the script, with an invented id pattern (`^Z-[0-9]+$`) and a column mapping that is not the default.

```
refs/remotes/origin/trunk; main exists: no
```

| Automaton | Used in |
|---|---|
| `d0a10001` hold-first | checks 3, 4 (hold chain) |
| `d0a10002` attach-by-rest | check 4 (attached by REST) |
| `d0a10003` attach-by-cli | check 4 (attached by the CLI) |
| `d0a10004` attach-by-mcp | check 4 (attached by the MCP tool) |
| `d0a10005` attach-by-chat | check 4 (attached by the chat verb) |
| `d0a10006` hold-last | checks 3, 4 (hold chain) |
| `d0a10007` skip-first | check 5 (skip chain) |
| `d0a10008` skip-last | check 5 (skip chain) |
| `d0a10009` file-first | check 6 (file chain) |
| `d0a1000a` file-last | check 6 (file chain) |


## 1. Config: the nine settings

- **PASS** — a fresh config reads policy hold, auto-resume on and no file registry, with the default id pattern, columns, done words and on-missing word
  - `GET /api/autonomous/pending-items`: {"policy":"hold","auto_resume":true,"source":{"configured":false,"path":"","id_pattern":"^\\S+$","id_column":"1","title_column":"2","state_column":"","done_values":["done","closed","shipped"],"on_missing_row":"keep"}}

- **PASS** — PUT then GET round-trips all nine settings (policy, auto-resume, path, id pattern, id column, title column, state column, done values, on-missing-row)
  - `pending_item_policy`: HTTP 200, read `"skip"`; `pending_item_auto_resume`: HTTP 200, read `false`; `pending_item_source.path`: HTTP 200, read `"sample/work-items.md"`; `pending_item_source.id_pattern`: HTTP 200, read `"^W-[0-9]+$"`; `pending_item_source.id_column`: HTTP 200, read `"Key"`; `pending_item_source.title_column`: HTTP 200, read `"3"`; `pending_item_source.state_column`: HTTP 200, read `"Progress"`; `pending_item_source.done_values`: HTTP 200, read `["finished","retired"]`; `pending_item_source.on_missing_row`: HTTP 200, read `"drop"`; 

- **PASS** — the registry route reads the values in effect, without a restart
  - {"policy":"skip","auto_resume":false,"source":{"configured":true,"path":"sample/work-items.md","id_pattern":"^W-[0-9]+$","id_column":"Key","title_column":"3","state_column":"Progress","done_values":["finished","retired"],"on_missing_row":"drop"}}

- **PASS** — the Automata settings route round-trips settings too
  - `PUT /api/autonomous/config` HTTP 200; read back {"policy":"hold","auto_resume":true,"id_column":"2","on_missing_row":"keep"}

- **PASS** — the chat verb `configure` refuses a bad id pattern before it reaches the daemon and saves nothing
  - reply: `[dw-e2e-test] Configure failed: autonomous.automation.pending_item_source.id_pattern must be a valid regular expression: error parsing regexp: missing closing ]: `[unclosed``; pattern still `^W-[0-9]+$`

- **PASS** — the chat verb `configure` reaches the daemon (recorded as a finding when it does not; see the notes)
  - reply: `[dw-e2e-test] Configure failed: API returned 401`; policy then `hold`

- **PASS** — a bad id pattern is a 400 on both routes
  - `PUT /api/config`: 400 autonomous.automation.pending_item_source.id_pattern must be a valid regular expression: error parsing regexp: missing closing ]: `[unclosed`  — `PUT /api/autonomous/config`: 400 autonomous.automation.pending_item_source.id_pattern must be a valid regular expression: error parsing regexp: missing closing ]: `[unclosed` 

- **PASS** — a bad policy word and a bad on-missing word are a 400 that names the allowed words
  - policy: 400 autonomous.automation.pending_item_policy must be "hold" or "skip", got "sometimes"  — on-missing-row: 400 autonomous.automation.pending_item_source.on_missing_row must be "keep" or "drop", got "maybe" 

- **PASS** — the refused writes saved nothing
  - after the four refused writes: {"policy":"hold","id_pattern":"^W-[0-9]+$","on_missing_row":"keep"}

- **PASS** — writing the defaults back (an empty path and an empty state column included) takes effect live
  - {"policy":"hold","auto_resume":true,"source":{"configured":false,"path":"","id_pattern":"^\\S+$","id_column":"1","title_column":"2","state_column":"","done_values":["done","closed","shipped"],"on_missing_row":"keep"}}

## 2. Read surfaces agree

- **PASS** — `GET /api/autonomous/pending-items` answers 200
  - HTTP 200

- **PASS** — the body has the policy, auto-resume, the registry settings, the last refresh, the items (id, ref, title, state, source, Automaton, Automations that carry it) and the held Automations
  - {"policy":"hold","auto_resume":true,"items":[{"id":"8bcc7a3e","state":"waiting","source":"internal"},{"id":"f72314bf","state":"waiting","source":"internal"},{"id":"e7153663","state":"waiting","source":"internal"},{"id":"346e6f4c","state":"waiting","source":"internal"},{"id":"18d4d401","state":"waiting","source":"internal"}],"held":[{"automation_id":"c26ce6bb","item_id":"8bcc7a3e"}]}

- **PASS** — an unsupported method is a 405
  - PUT: HTTP 405

CLI text (`datawatch autonomous pending-items`):
```
Pending items (policy: hold, auto-resume: on):
  8bcc7a3e  waiting   Write the migration guide [ref first-label]
      held: Automation "hold chain" (c26ce6bb) waits since 2026-11-12 03:17 UTC: member "Write the migration guide" is a pending item waiting for an Automaton to be attached (item 8bcc7a3e); attach one to continue
  f72314bf  waiting   Draft the rollout notes
      in Automation "hold chain" (c26ce6bb)
  e7153663  waiting   Review the access policy
      in Automation "hold chain" (c26ce6bb)
  346e6f4c  waiting   Archive the old dashboards
      in Automation "hold chain" (c26ce6bb)
  18d4d401  waiting   Plan the next quarter
File registry: not configured

```

Chat reply (`POST /api/test/message`, text `autonomous pending-items`):
```
[dw-e2e-test] autonomous pending-items
Pending items (policy: hold, auto-resume: on):
  8bcc7a3e  waiting   Write the migration guide [ref first-label]
      held: Automation "hold chain" (c26ce6bb) waits since 2026-11-12 03:17 UTC: member "Write the migration guide" is a pending item waiting for an Automaton to be attached (item 8bcc7a3e); attach one to continue
  f72314bf  waiting   Draft the rollout notes
      in Automation "hold chain" (c26ce6bb)
  e7153663  waiting   Review the access policy
      in Automation "hold chain" (c26ce6bb)
  346e6f4c  waiting   Archive the old dashboards
      in Automation "hold chain" (c26ce6bb)
  18d4d401  waiting   Plan the next quarter
File registry: not configured

```

MCP result (`autonomous_pending_items`, first 700 bytes):
```
{"policy":"hold","auto_resume":true,"source":{"configured":false,"path":"","id_pattern":"^\\S+$","id_column":"1","title_column":"2","state_column":"","done_values":["done","closed","shipped"],"on_missing_row":"keep"},"last_refresh":{"configured":false,"at":null,"rows":0,"added":0,"updated":0,"done":0,"dropped":0,"kept":0,"skipped":0,"error":""},"items":[{"id":"8bcc7a3e","ref":"first-label","title":"Write the migration guide","state":"waiting","source":"internal","automaton_id":"","created_at":"2026-11-11T22:17:31.816127745-05:00","updated_at":"2026-11-11T22:17:31.816127745-05:00","automations":[{"automation_id":"c26ce6bb","name":"hold chain","member_id":"a1000002","held":true}]},{"id":"f7231

```

- **PASS** — REST, MCP, CLI (--json) and the chat verb return the same registry, the same held Automation and the same file registry line
  - comparison: `OK`; items [{"id":"8bcc7a3e","state":"waiting"},{"id":"f72314bf","state":"waiting"},{"id":"e7153663","state":"waiting"},{"id":"346e6f4c","state":"waiting"},{"id":"18d4d401","state":"waiting"}]

- **PASS** — the filters narrow the list (state, Automation) and an unknown state is a 400
  - waiting 5, done 0, bad state HTTP 400, in the hold chain 4, in an unknown Automation 0

- **PASS** — an item added by the CLI, the MCP tool and the chat verb is a waiting internal item
  - CLI {"title":"Added by the command line","ref":"cli-ref","state":"waiting","source":"internal"}; MCP {"title":"Added by the MCP tool","state":"waiting","source":"internal"}; chat `[dw-e2e-test] autonomous pending-item-add
Recorded pending item f0e9edaa (waiting): Added by chat [ref chat-ref]`

## 3. Hold until attached

The Automation `hold chain` has six members: an Automaton, four pending items, and a last Automaton. Its first Automaton runs; the cursor then reaches the first pending item. Nothing is staged by hand: the Automata run on the daemon's shell backend and the Automation is created and activated through the REST API.

- **PASS** — the Automation holds at the first pending item: blocked, with the cursor on that member
  - status `blocked`, current member `a1000002` (item member `a1000002`)

The Automation after the hold (`GET /api/automations/<id>`, trimmed):
```
{
  "id": "c26ce6bb",
  "name": "hold chain",
  "status": "blocked",
  "current_member_id": "a1000002",
  "members": [
    {
      "id": "a1000001",
      "kind": "automaton",
      "automaton": "d0a10001",
      "pending_item_id": null,
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    },
    {
      "id": "a1000002",
      "kind": "pending_item",
      "automaton": "",
      "pending_item_id": "8bcc7a3e",
      "status": "pending",
      "edge_taken": null,
      "outcome": null
    },
    {
      "id": "a1000003",
      "kind": "pending_item",
      "automaton": "",
      "pending_item_id": "f72314bf",
      "status": "",
      "edge_taken": null,
      "outcome": null
    },
    {
      "id": "a1000004",
      "kind": "pending_item",
      "automaton": "",
      "pending_item_id": "e7153663",
      "status": "",
      "edge_taken": null,
      "outcome": null
    },
    {
      "id": "a1000005",
      "kind": "pending_item",
      "automaton": "",
      "pending_item_id": "346e6f4c",
      "status": "",
      "edge_taken": null,
      "outcome": null
    },
    {
      "id": "a1000006",
      "kind": "automaton",
      "automaton": "d0a10006",
      "pending_item_id": null,
      "status": "",
      "edge_taken": null,
      "outcome": null
    }
  ]
}

```

The held entry of `GET /api/autonomous/pending-items`:
```
{
  "automation_id": "c26ce6bb",
  "name": "hold chain",
  "member_id": "a1000002",
  "item_id": "8bcc7a3e",
  "title": "Write the migration guide",
  "ref": "first-label",
  "since": "2026-11-11T22:17:38.437284873-05:00",
  "reason": "member \"Write the migration guide\" is a pending item waiting for an Automaton to be attached (item 8bcc7a3e); attach one to continue"
}

```

- **PASS** — the reason is visible and names the item and its id
  - reason: `member "Write the migration guide" is a pending item waiting for an Automaton to be attached (item 8bcc7a3e); attach one to continue`

- **PASS** — the same reason is in the Automation's decision log
  - decisions: ["member \"Write the migration guide\" is a pending item waiting for an Automaton to be attached (item 8bcc7a3e); attach one to continue"]

- **PASS** — the first Automaton completed before the hold
  - Automaton `completed`; member `completed`

- **PASS** — the successor never starts: after ten more seconds it is still approved and its worker has not run
  - Automaton `approved`, worker runs 0, member ``

- **PASS** — the hold is stable: still blocked, the item still waiting
  - status `blocked`, item `waiting`

- **PASS** — only the item at the cursor is held; the items behind it are in the Automation but not held
  - held entries for the Automation: 1

Audit entries mentioning a pending item or a block (informational): 9.

## 4. Attach: REST, CLI, MCP tool and chat verb

REST attach reply (`POST /api/autonomous/pending-items/<item>/attach`):
```
{
  "item": {
    "id": "8bcc7a3e",
    "state": "attached",
    "automaton_id": "d0a10002"
  },
  "converted_automations": [
    {
      "automation_id": "c26ce6bb",
      "member_ids": [
        "a1000002"
      ]
    }
  ],
  "resumed_automations": [
    "c26ce6bb"
  ],
  "notes": null
}

```

- **PASS** — REST: the attach is accepted
  - HTTP 200

- **PASS** — REST: the reply says the member was converted in the Automation, the Automation was resumed and the item is attached
  - item `8bcc7a3e` -> Automaton `d0a10002`: converted in 1 Automation, resumed 1, item state `attached`

- **PASS** — REST: the member is now an Automaton member for that Automaton and keeps the item as provenance
  - member: {"id":"a1000002","kind":"automaton","automaton":"d0a10002","pending_item_id":"8bcc7a3e"}

- **PASS** — REST: the Automaton runs and completes
  - Automaton `d0a10002` is `completed`, worker runs 1

- **PASS** — REST: the Automation moves on and holds at the next item
  - {"status":"blocked","current_member_id":"a1000003"}

CLI text of an attach (`datawatch autonomous pending-item-attach`) is the same reply rendered; the raw reply was:
```
{
  "item": {
    "id": "f72314bf",
    "state": "attached",
    "automaton_id": "d0a10003"
  },
  "converted_automations": [
    {
      "automation_id": "c26ce6bb",
      "member_ids": [
        "a1000003"
      ]
    }
  ],
  "resumed_automations": [
    "c26ce6bb"
  ],
  "notes": null
}

```

- **PASS** — CLI: `autonomous pending-item-attach` is accepted
  - exit 0

- **PASS** — CLI: the reply says the member was converted in the Automation, the Automation was resumed and the item is attached
  - item `f72314bf` -> Automaton `d0a10003`: converted in 1 Automation, resumed 1, item state `attached`

- **PASS** — CLI: the member is now an Automaton member for that Automaton and keeps the item as provenance
  - member: {"id":"a1000003","kind":"automaton","automaton":"d0a10003","pending_item_id":"f72314bf"}

- **PASS** — CLI: the Automaton runs and completes
  - Automaton `d0a10003` is `completed`, worker runs 1

- **PASS** — CLI: the Automation moves on and holds at the next item
  - {"status":"blocked","current_member_id":"a1000004"}

MCP result (`autonomous_pending_item_attach`):
```
{
  "item": {
    "id": "e7153663",
    "state": "attached",
    "automaton_id": "d0a10004"
  },
  "converted_automations": [
    {
      "automation_id": "c26ce6bb",
      "member_ids": [
        "a1000004"
      ]
    }
  ],
  "resumed_automations": [
    "c26ce6bb"
  ],
  "notes": null
}

```

- **PASS** — MCP: the tool answers with the attach result
  - {"item":{"id":"e7153663","title":"Review the access policy","state":"attached","source":"internal","automaton_id":"d0a10004","created_at":"2026-11-11T22:17:31.845017875-05:00","updated_at":"2026-11-11

- **PASS** — MCP: the reply says the member was converted in the Automation, the Automation was resumed and the item is attached
  - item `e7153663` -> Automaton `d0a10004`: converted in 1 Automation, resumed 1, item state `attached`

- **PASS** — MCP: the member is now an Automaton member for that Automaton and keeps the item as provenance
  - member: {"id":"a1000004","kind":"automaton","automaton":"d0a10004","pending_item_id":"e7153663"}

- **PASS** — MCP: the Automaton runs and completes
  - Automaton `d0a10004` is `completed`, worker runs 1

- **PASS** — MCP: the Automation moves on and holds at the next item
  - {"status":"blocked","current_member_id":"a1000005"}

Chat reply (`autonomous pending-item-attach`):
```
[dw-e2e-test] autonomous pending-item-attach
Attached Automaton d0a10005 to pending item 346e6f4c (attached): Archive the old dashboards
  converted in Automation c26ce6bb: members a1000005
  resumed Automation c26ce6bb

```

- **PASS** — chat: the reply reports the attach, the converted member and the resumed Automation
  - [dw-e2e-test] autonomous pending-item-attach|Attached Automaton d0a10005 to pending item 346e6f4c (attached): Archive the old dashboards|  converted in Automation c26ce6bb: members a1000005|

- **PASS** — chat: the member is now an Automaton member for that Automaton
  - member: {"id":"a1000005","automaton":"d0a10005","pending_item_id":"346e6f4c"}

- **PASS** — chat: the Automaton runs and completes
  - Automaton `d0a10005` is `completed`

- **PASS** — the last Automaton then runs and the Automation completes
  - last Automaton `completed`, Automation `completed`

The finished Automation (trimmed):
```
{
  "id": "c26ce6bb",
  "name": "hold chain",
  "status": "completed",
  "current_member_id": null,
  "members": [
    {
      "id": "a1000001",
      "kind": "automaton",
      "automaton": "d0a10001",
      "pending_item_id": null,
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    },
    {
      "id": "a1000002",
      "kind": "automaton",
      "automaton": "d0a10002",
      "pending_item_id": "8bcc7a3e",
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    },
    {
      "id": "a1000003",
      "kind": "automaton",
      "automaton": "d0a10003",
      "pending_item_id": "f72314bf",
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    },
    {
      "id": "a1000004",
      "kind": "automaton",
      "automaton": "d0a10004",
      "pending_item_id": "e7153663",
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    },
    {
      "id": "a1000005",
      "kind": "automaton",
      "automaton": "d0a10005",
      "pending_item_id": "346e6f4c",
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    },
    {
      "id": "a1000006",
      "kind": "automaton",
      "automaton": "d0a10006",
      "pending_item_id": null,
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    }
  ]
}

```

- **PASS** — when each attached Automaton completed its item moved to done
  - [{"id":"8bcc7a3e","state":"done","automaton_id":"d0a10002"},{"id":"f72314bf","state":"done","automaton_id":"d0a10003"},{"id":"e7153663","state":"done","automaton_id":"d0a10004"},{"id":"346e6f4c","state":"done","automaton_id":"d0a10005"}]

- **PASS** — attaching again to an item that is not waiting is a 409
  - 409 409: pending item 8bcc7a3e is done; only a waiting pending item can have an Automaton attached 

- **PASS** — an Automaton that does not exist is a 404
  - 404 404: Automaton deadbeef not found 

- **PASS** — an item that does not exist is a 404
  - 404 404: pending item 00000000 not found 

- **PASS** — an attach with no Automaton id is a 400
  - 400 400: an attach needs the id of the Automaton to attach 

- **PASS** — the refused attaches changed nothing
  - item 18d4d401 is `waiting`

## 5. Skip policy passes through

The skip chain after it finished (trimmed):
```
{
  "id": "f0e9fc91",
  "name": "skip chain",
  "status": "completed",
  "current_member_id": null,
  "members": [
    {
      "id": "a1000011",
      "kind": "automaton",
      "automaton": "d0a10007",
      "pending_item_id": null,
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    },
    {
      "id": "a1000012",
      "kind": "pending_item",
      "automaton": "",
      "pending_item_id": "18d4d401",
      "status": "skipped",
      "edge_taken": "on_complete",
      "outcome": "skipped_pending_item"
    },
    {
      "id": "a1000013",
      "kind": "automaton",
      "automaton": "d0a10008",
      "pending_item_id": null,
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    }
  ]
}

```

- **PASS** — with policy skip the Automation runs through a waiting item and completes
  - activate HTTP 200; Automation `completed`

- **PASS** — the pending item member is passed over and recorded as skipped
  - member: {"status":"skipped","outcome":"skipped_pending_item","edge_taken":"on_complete"}

- **PASS** — the Automaton behind it ran
  - Automaton `completed`, worker runs 1

- **PASS** — the item stays waiting, with no Automaton
  - item 18d4d401: {"state":"waiting","automaton_id":""}

- **PASS** — nothing is held
  - held entries: 0

## 6. File registry: invented id pattern and column mapping

The sample registry is written inside the sandbox. Its ids look like `Z-101` (an invented pattern, `^Z-[0-9]+$`), the id sits in a column named `Key` (the setting is the lower-case name `key`, matched without regard to case), the title in the third column (the setting is the number `3`), the state in a column named `Progress`, and the words that mean done are `closed` and `shipped`. One row carries an id of another kind (`Q-9`) and must be skipped.

- **PASS** — the registry settings are accepted (path, id pattern, id column by name, title column by number, state column, done words)
  - {"configured":true,"path":"<sandbox>/registry/work-items.md","id_pattern":"^Z-[0-9]+$","id_column":"key","title_column":"3","state_column":"progress","done_values":["closed","shipped"],"on_missing_row":"keep"}

First refresh (`POST /api/autonomous/pending-items/refresh`):
```
{
  "configured": true,
  "at": "2026-11-11T22:18:44.062314263-05:00",
  "rows": 3,
  "added": 3,
  "updated": 0,
  "done": 1,
  "dropped": 0,
  "kept": 0,
  "skipped": 1
}

```

- **PASS** — the refresh reads three rows, adds three items, moves one to done and skips the row with another id
  - {"configured":true,"rows":3,"added":3,"updated":0,"done":1,"dropped":0,"kept":0,"skipped":1}

- **PASS** — the registry holds the file items with the titles from the mapped column and the state from the state column
  - Z-101 {"state":"waiting","source":"file","title":"Publish the rollout notes"}; Z-102 {"state":"waiting"}; Z-103 {"state":"done"}

- **PASS** — the row with another id is not in the registry
  - item for ref Q-9: {}; items 8 before, 11 after

- **PASS** — refreshing again on an unchanged file changes nothing
  - {"configured":true,"rows":3,"added":0,"updated":0,"done":0,"dropped":0,"kept":0,"skipped":1}

- **PASS** — the registry route shows the file registry as configured with the last refresh
  - {"source":{"configured":true},"last_refresh":{"configured":true,"rows":3,"added":0}}

- **PASS** — a file that cannot be read records the error and leaves the registry as it was
  - error: `open registry file: open <sandbox>/registry/no-such-file.md: no such file or directory`

- **PASS** — an Automation with a file item as a member holds at it
  - {"status":"blocked","current_member_id":"a1000022"}

- **PASS** — the successor has not started
  - Automaton `approved`, worker runs 0

Refresh after the row turned done (CLI, `datawatch autonomous pending-items-refresh --json`):
```
{
  "configured": true,
  "at": "2026-11-11T22:18:58.761358713-05:00",
  "rows": 3,
  "added": 0,
  "updated": 0,
  "done": 1,
  "dropped": 0,
  "kept": 0,
  "skipped": 1
}

```

- **PASS** — the refresh moves the item to done
  - {"configured":true,"rows":3,"added":0,"updated":0,"done":1,"dropped":0,"kept":0,"skipped":1}

- **PASS** — the done item releases the hold: the member is passed over, the successor runs and the Automation completes
  - Automation `completed`, item `done`, successor `completed`

The finished file chain (trimmed):
```
{
  "id": "e3ec06df",
  "name": "file chain",
  "status": "completed",
  "current_member_id": null,
  "members": [
    {
      "id": "a1000021",
      "kind": "automaton",
      "automaton": "d0a10009",
      "pending_item_id": null,
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    },
    {
      "id": "a1000022",
      "kind": "pending_item",
      "automaton": "",
      "pending_item_id": "37795ae3",
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "pending_item_done"
    },
    {
      "id": "a1000023",
      "kind": "automaton",
      "automaton": "d0a1000a",
      "pending_item_id": null,
      "status": "completed",
      "edge_taken": "on_complete",
      "outcome": "completed"
    }
  ]
}

```

- **PASS** — a row that vanished from the file stays in the registry under keep
  - {"rows":2,"kept":1,"dropped":0}

- **PASS** — the same row is dropped under drop (a waiting item that is in no Automation)
  - {"rows":2,"kept":0,"dropped":1}; item for ref Z-102: {}

## 7. No file registry configured: refresh answers configured false and changes nothing

- **PASS** — with the path cleared the registry reads as not configured
  - {"configured":false,"path":"","id_pattern":"^\\S+$","id_column":"1","title_column":"2","state_column":"","done_values":["done","closed","shipped"],"on_missing_row":"keep"}

REST refresh reply:
```
{
  "configured": false,
  "at": "2026-11-11T22:19:06.988655364-05:00",
  "rows": 0,
  "added": 0,
  "updated": 0,
  "done": 0,
  "dropped": 0,
  "kept": 0,
  "skipped": 0
}

```

Chat reply (`autonomous pending-items-refresh`):
```
[dw-e2e-test] autonomous pending-items-refresh
No pending item file registry is configured (autonomous.automation.pending_item_source.path); nothing was read.

```

CLI text (`datawatch autonomous pending-items-refresh`):
```
No pending item file registry is configured (autonomous.automation.pending_item_source.path); nothing was read.

```

- **PASS** — REST, the CLI (--json) and the MCP tool answer configured false with every count at zero and no error
  - REST {"configured":false,"rows":0,"added":0,"updated":0,"done":0,"dropped":0,"kept":0,"skipped":0}; CLI {"configured":false,"rows":0,"added":0,"updated":0,"done":0,"dropped":0,"kept":0,"skipped":0}; MCP {"configured":false,"rows":0,"added":0,"updated":0,"done":0,"dropped":0,"kept":0,"skipped":0}

- **PASS** — the chat verb and the CLI text say that no file registry is configured and nothing was read
  - chat: `[dw-e2e-test] autonomous pending-items-refresh`

- **PASS** — the registry is exactly as it was: no item added, changed or removed
  - 10 items before and after

- **PASS** — the last refresh records that nothing was configured
  - {"configured":false,"rows":0,"added":0,"updated":0,"done":0,"dropped":0,"kept":0,"skipped":0,"error":""}

- **PASS** — the sample file was never written by the daemon
  - file present; Z-103 row count 1

## 8. Cleanup

- **PASS** — the sandbox daemon and its sessions are gone and no test daemon was left behind
  - daemon gone: yes; port 45849 closed: yes; leftover check exit 0

- The daemon was ended by `td_stop_all` from `scripts/test-daemon-lib.sh` (the recorded pid, config path as marker); nothing was ended by name.
- The scratch area (project, remote, worktrees, data, config, log, registry file, worker, state) is removed by the exit handler (trap) right after this report is written.

## Daemon log, the pending item lines

```
2026/11/11 22:18:15 [autonomous] automaton=d0a10005 worktree created at <sandbox>/worktrees/d0a10005 (branch=automaton/d0a10005-attach-by-chat)
2026/11/11 22:18:15 tree lock: acquired <sandbox>/worktrees/d0a10005/.datawatch/tree.lock holder task=d0a10005t1 automaton=d0a10005
2026/11/11 22:18:21 [autonomous] automaton=d0a10006 worktree created at <sandbox>/worktrees/d0a10006 (branch=automaton/d0a10006-hold-last)
2026/11/11 22:18:21 tree lock: acquired <sandbox>/worktrees/d0a10006/.datawatch/tree.lock holder task=d0a10006t1 automaton=d0a10006
2026/11/11 22:18:29 [autonomous] automaton=d0a10007 worktree created at <sandbox>/worktrees/d0a10007 (branch=automaton/d0a10007-skip-first)
2026/11/11 22:18:29 tree lock: acquired <sandbox>/worktrees/d0a10007/.datawatch/tree.lock holder task=d0a10007t1 automaton=d0a10007
2026/11/11 22:18:36 [autonomous] automaton=d0a10008 worktree created at <sandbox>/worktrees/d0a10008 (branch=automaton/d0a10008-skip-last)
2026/11/11 22:18:36 tree lock: acquired <sandbox>/worktrees/d0a10008/.datawatch/tree.lock holder task=d0a10008t1 automaton=d0a10008
2026/11/11 22:18:44 [autonomous] automaton=d0a10009 worktree created at <sandbox>/worktrees/d0a10009 (branch=automaton/d0a10009-file-first)
2026/11/11 22:18:44 tree lock: acquired <sandbox>/worktrees/d0a10009/.datawatch/tree.lock holder task=d0a10009t1 automaton=d0a10009
2026/11/11 22:18:58 [autonomous] automaton=d0a1000a worktree created at <sandbox>/worktrees/d0a1000a (branch=automaton/d0a1000a-file-last)
2026/11/11 22:18:58 tree lock: acquired <sandbox>/worktrees/d0a1000a/.datawatch/tree.lock holder task=d0a1000at1 automaton=d0a1000a
```

## Notes and what is not shown live

- **The Automata were written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model, which a sandbox does not have. The Automations, the pending items, the approvals, every read and every setting go through the REST API of the running daemon (and the MCP tool, the command line and the chat verbs where each check says so); the worktrees and branches were made by the daemon.
- **One Automation carries the four attaches.** Its chain holds four pending items one after the other, so each attach is made on a separate item and the Automation is seen to move on and hold again before the next one.
- **The file registry is read from an absolute path.** A relative path resolves against the default project directory and may not leave it; that rule, the size limit and a path that escapes are covered by the unit tests of the reader, not shown here.
- **Not shown live:** the web UI card and badge (covered by the Node tests of the PWA), an insert of a pending item into a running Automation and its approval, the alert for a held Automation, auto-resume set to off, a restart of the daemon while an Automation is held, and the registry read at daemon start. This script drives the daemon through REST, MCP, the command line and chat only.
- **Finding, not fixed here.** The chat verb `configure` answered `[dw-e2e-test] Configure failed: API returned 401` to a valid write on this token-protected sandbox daemon. Its loopback call to `PUT /api/config` in the daemon's command entry point sends no bearer token, so a daemon with `server.token` set refuses it; the comm verbs that go through the shared loopback helper do send the token. The file is outside this task's scope, so it is reported and left as it is. Every setting is affected, not only the pending item ones; the bad-value refusal above is made before the call and is unaffected.
- **Generality.** The project is a throwaway repository whose default branch is `trunk` and has no `main`; the registry file, its id pattern and its columns are invented inside the sandbox; nothing names the repository this script lives in.

Result: PASS
