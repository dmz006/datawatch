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
- Nothing runs any check, and nothing supplies the version of the work, live
  sources, receipts or a reviewer yet. So command, state, receipt and review
  items on a list you make here stay **unchecked**. Confirm items and
  acceptance without proof work now.
- Only a run closes a list that holds something a run has to read (a command,
  state, receipt or review item, or a confirm item about the work), unless that
  item is accepted without proof. Since nothing runs yet, such a list never
  reads done today, even with every answer given. A list can read done today
  only when each item is a plain confirm item or is accepted without proof.
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
an open item, including headings and lines ending in `:`. Signs such as `>`,
`<`, `%` and a minus before a number are part of a sentence. A sentence that
asks for two things ("do A and B") is still one item. To cover a sentence yourself, give an item whose `from` quotes the
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
- An answer datawatch can see may be the doer's is shown as a claim and does not
  count. A model tool with no known session, a session while the doers are
  unknown, and a federation peer are all claims.
- **Limit: who counts is recorded, not enforced.** datawatch does not verify who
  is behind a call made with the daemon's own token, including what such a call
  says about itself, and it counts chat messages as a person's. Do not treat a
  counted answer as proof of who gave it. Caller identity comes with the
  authentication work.
- A counted answer or acceptance reopens a closed list: a run has to read the
  versions again before it reads done.

## How a list reads

| Reads | Meaning |
|---|---|
| done | every item was met on the versions the closing run read |
| answered, but the current versions have not been checked since | nothing is missing, and no run has closed the list since the last answer. It is not done |
| done, accepted without proof | as above, with one or more items you accepted |
| not met / open / waiting | something failed, is unproven, or is not due yet |
| failure now reported, still broken | an item only reports the failure |
| the list is too thin for the ask | only default or receipt items |
| met, but nobody has checked that the list covers the ask | coverage is unconfirmed |
| met on a changed list | the list changed after it was fixed. It is not plain done; how it should read is still an open operator choice |
| the ask changed | later words were added and the list has not been revised |
| delivered / filed | receipt only, on a delivery list datawatch made. Neither means done. A list you make never reads this way |
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
- Exit 0 alone proves nothing: every `pass` line must appear. A line counts only
  when it **starts** with the text (spaces before it are ignored) and says
  nothing after it about being skipped or to do (`SKIP`, `skipped`, `TODO`,
  `xfail`, `deselected`, `ignored`, `not run`). A timestamp or other prefix
  makes the item read cannot tell. The `fail` line of the start run is read the
  same way.
- An item that has to show work was done (`change: true`,
  `before: fail_first`) needs a start run that finished and showed the `fail`
  line.
- Once any check turns out to have passed before the work, the list reads done
  only if some other command or state item that had to see a change shows one. A
  person's yes does not. (Accepting an item without proof shows as that.)
- `inputs` are frozen. If one differs, the item reads cannot tell until you save
  the item again after reviewing it. That shows as a change.

## Sources and default items

datawatch sets a list's rules from what it knows. A source **you** name only adds
that source's default items: your list is held to every rule, the same as a list
of unknown source (kind `other`, or `change` for the change sources).

| Source you name | Default items added |
|---|---|
| `report`, `research`, `design`, `docs` | delivered to the requester, each question answered, what was not checked |
| `automaton`, `bug`, `code`, `operation`, `rule` | the running system shows the asked state, the change is connected and used |
| `message`, `intake`, `handoff`, `question` | the recipient received it |
| anything else | none |

A `schedule` source also adds two run items. The lighter **delivery** and
**deliverable** kinds are only for datawatch's own request sources, which do not
exist yet. On those lists, removing or changing one of datawatch's default items
shows as a change, also before the list is fixed. No request can claim to be one
of those sources. `datawatch acceptance catalog` shows the default items.
`acceptance.defaults_file` (config file only) replaces a kind's list or adds a
pack; the `software` pack is opt-in.

## Receipt

A receipt item is met only when all of these hold:

- **Who:** the item's named recipient (`state.target`) took it. On a delivery-only
  list the current holder also counts. On any other list the holder is the one
  doing the work, so only a named recipient who has not done the work counts. An
  item with no named recipient and no holder is never met.
- **When:** the source states the time. It is not in the future, and not before
  the ask, its latest added words or the last time the work was replaced.
- **Which run:** for a scheduled run, the receipt carries that run's id and was
  made after the run was due. A schedule's own list is not a run.

A channel taking a message is not receipt. No receipt source is wired yet.

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
| Chat | `acceptance`, `acceptance show <id>`, `acceptance confirm <id> <item> yes <words>` (ids and results only on an encrypted install). The word is a command only while the setting is `report`; otherwise such a message is an ordinary message |
