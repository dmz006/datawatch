---
docs:
  index: true
  topics: [acceptance, done-when, autonomous, operator]
exec_steps:
  - tool: acceptance_view
    description: Show every "done when" list in short, with open items grouped by reason (needs acceptance.mode report)
    args: {}
    read_only: true
  - tool: acceptance_report
    description: Run the standing check; it only reports
    args: {}
    read_only: true
---

# "Done when" lists

A request can carry a short list of **done when** statements. The list is fixed
before work starts, and each item reads **met**, **not met**, **cannot tell** or
**unchecked**. Only a list whose every item is met reads done.

**This is report only.** With the setting on, datawatch keeps and checks lists.
It does not hold, block or merge anything because of them.

**What this version does not do yet:**

- datawatch does not create, fix or run a list by itself when work starts. You
  create lists through the surfaces below.
- Nothing supplies the version of the work, live sources, receipts or a
  reviewer yet. So command, state, receipt and review items on a list you make
  here read **cannot tell** or unchecked. Confirm items and acceptance without
  proof work now.
- It does not stop a worker from changing its own list. It records every change
  and shows it against the first fixed version. Rules about who may change a
  list come with the authentication work.

## Turn it on

```bash
datawatch config set acceptance.mode report
```

The other values are `off` (default) and nothing else. The MCP tools appear in
the tool list after a daemon restart; the other surfaces work at once.

## Make a list

```bash
datawatch acceptance edit caterer --source research \
  --ask "Compare three caterers for 40 people. Say how each handles allergies."
datawatch acceptance show caterer
```

The ask is kept word for word. Every sentence of it that no item covers becomes
an open item. To cover a sentence yourself, give an item whose `from` quotes the
whole sentence:

```json
[{"id": "compared", "statement": "Three caterers are compared, each price with a source",
  "from": "Compare three caterers for 40 people", "method": "confirm"}]
```

```bash
datawatch acceptance edit caterer --items items.json
datawatch acceptance freeze caterer --hash <hash shown by show>
```

`freeze` fixes exactly the list you read. If it changed since, it is refused.

## Answer

```bash
datawatch acceptance confirm caterer compared --answer yes --words "I checked all three quotes"
datawatch acceptance confirm caterer list --answer yes --hash <hash> --words "Nothing in my ask is missing"
datawatch acceptance accept caterer d-answers --reason "no reviewer yet" --words "accept it"
```

- Confirming `list` says the list covers the whole ask. Without that (or a
  reviewer) a list reads "met, but nobody has checked that the list covers the
  ask".
- An accepted item reads **accepted without proof**, never met.
- An answer counts only when datawatch can show it did not come from the doer.
  Otherwise it is shown as a claim. A model tool with no known session, a
  session while the doers are unknown, and a federation peer are all claims.
  **Limit:** a worker that uses the daemon's own token outside datawatch's tools
  and command line looks like you. Caller identity comes with the
  authentication work.

## How a list reads

| Reads | Meaning |
|---|---|
| done | every item is met on the current version |
| done, accepted without proof | as above, with one or more items you accepted |
| not met / open / waiting | something failed, is unproven, or is not due yet |
| failure now reported, still broken | a change has an item that only reports the failure |
| the list is too thin for the ask | only default or receipt items |
| met, but nobody has checked that the list covers the ask | coverage is unconfirmed |
| met on a changed list | the list changed after it was fixed. It is not plain done; how it should read is still an open operator choice |
| the ask changed | later words were added and the list has not been revised |
| delivered / filed | receipt only. Neither means done |
| no criteria | there is no list, or it has no items |

## Checks that run a command

A command item names a **project check**: a saved command you register in the
config file. No API, tool, chat command or web page can write one.

```yaml
acceptance:
  mode: report
  project_checks:
    export-test:
      command: go test -count=1 -v -run TestExport ./export/
      inputs: [Makefile, export/testdata]
      expect:
        - pass: "--- PASS: TestExport"
          fail: "--- FAIL: TestExport"
```

- datawatch runs it on a clean copy of the exact version. Untracked and
  uncommitted files are not there, and the copy has no `.git` folder.
- Exit 0 alone proves nothing: every `pass` line must appear.
- An item that has to show work was done (`change: true`,
  `before: fail_first`) needs a start run that finished and showed the `fail`
  line.
- `inputs` are frozen. If one differs, the item reads cannot tell until you save
  the item again after reviewing it. That shows as a change.

## Defaults

Four kinds, set from the source: written deliverable (`report`, `research`,
`design`, `docs`), change to something live (`automaton`, `bug`, `code`,
`operation`, `rule`), delivery only (`message`, `intake`, `handoff`,
`question`) and other (anything else). A `schedule` source adds two run items.
`datawatch acceptance catalog` shows them. `acceptance.defaults_file` (config
file only) replaces a kind's list or adds a pack; the `software` pack is opt-in.

## The standing check

```bash
datawatch acceptance report
```

It lists requests that started without a fixed list (I18), done without current
proof or on a changed list (I19) and changes without a record or without words
(I20). I18 needs the request sources to report their starts; until they do,
"starts seen" is 0 and I18 has checked nothing.

## Every surface

| Surface | Use |
|---|---|
| Web | the **Done when** table on an Automaton's overview; **All lists**; Settings → Done when |
| Command line | `datawatch acceptance list`, `show`, `edit`, `freeze`, `confirm`, `accept`, `report`, `catalog` |
| REST | `GET /api/acceptance`, `POST /api/acceptance/edit`, `/confirm`, `/accept`, `GET /api/acceptance/report`, `/catalog` |
| MCP | `acceptance_view`, `acceptance_edit`, `acceptance_confirm`, `acceptance_accept`, `acceptance_report`, `acceptance_catalog` |
| Chat | `acceptance`, `acceptance show <id>`, `acceptance confirm <id> <item> yes <words>` (ids and results only on an encrypted install) |
