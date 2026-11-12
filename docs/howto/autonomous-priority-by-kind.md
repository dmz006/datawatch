---
docs:
  index: true
  topics: [autonomous, automata, capacity, priority, defect-intake]
exec_params:
  - {name: priority_by_kind, required: false, default: "", description: "Kind → priority table to set, as kind=priority pairs (e.g. bug=90,feature=25) or a JSON object string (e.g. {\"bug\":90}). Empty = clear the table back to the shipped defaults"}
  - {name: defect_intake_automation, required: false, default: "", description: "Name of the Automation that new kind:bug defect filings are offered to; empty = clear it back to the unset state"}
  - {name: automaton_id, required: false, default: "", description: "Automaton to pin an explicit priority on (optional; omit to leave all Automata on the table)"}
  - {name: priority, required: false, default: "0", description: "Integer priority (>= 0) to pin on the Automaton; an explicit value, even 0, wins over the table for that Automaton"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the current priority_by_kind table and defect_intake_automation
    args: {}
  - tool: autonomous_config_set
    description: Set the kind → priority table and, optionally, the defect-intake Automation
    args: {priority_by_kind: "{{params.priority_by_kind}}", defect_intake_automation: "{{params.defect_intake_automation}}"}
  - tool: autonomous_prd_set_priority
    description: Pin an explicit priority on one Automaton (always wins for that Automaton)
    args: {id: "{{params.automaton_id}}", priority: "{{params.priority}}"}
  - tool: autonomous_prd_get
    description: Read the Automaton's priority and priority_source
    args: {id: "{{params.automaton_id}}"}
  - tool: capacity_status
    description: See the admission queue; each waiting entry carries its priority
    args: {}
---
# How-to: Autonomous Priority by Kind

The capacity queue serves waiting Automata by priority, then
round-robin across Automata, then in order within one. This howto
covers the two knobs that decide where an Automaton lines up:

- **The kind table** — `autonomous.priority_by_kind`: a daemon-wide
  map from the kind of work (`bug`, `maintenance`, `feature`, `other`,
  or your installation's own kind names) to a whole-number priority.
  The higher the number, the closer to the front.
- **An explicit pin** — a priority set directly on one Automaton,
  which always wins for that Automaton no matter what the table says.

Every queue entry also carries `priority_source` — `explicit`, `kind`
or `default` — naming which rule set its priority, so you can always
tell what happened and why.

Defect intake rides the same config surface: naming an Automation in
`autonomous.defect_intake_automation` sends new kind:bug filings to
it. Both settings are documented here because they are set, read and
cleared the same way on every channel.

## What ships

| Kind | Priority | Intent |
|---|---|---|
| `bug` | 80 | fast lane for fixes |
| `maintenance` | 70 | upkeep ahead of new work |
| `feature` | 20 | new work |
| `other` | 0 | the fallback row for a kind with no row of its own |

The table is a starting ranking, not a fixed rule — the daemon ships
these four rows and you tune them to how you queue work. A kind you
leave out keeps its shipped value; a kind you name is set exactly as
given (a zero you write means "back of the queue"); a kind of your own
that is not in the shipped table is kept as written, and if a filing's
kind has no row of its own it falls back to the `other` row.

## How a priority is chosen

When an Automaton enters the capacity queue, its priority is resolved
in this order:

1. **Explicit pin.** A priority set directly on the Automaton wins,
   even when the value is 0 or the table holds a higher one for its
   kind. `priority_source` is `explicit`.
2. **The kind table.** The Automaton's effective kind is looked up in
   `autonomous.priority_by_kind`. Kinds without a row of their own
   take the `other` row. `priority_source` is `kind`.
3. **Nothing set.** If the table has no row for the kind and no row
   for `other` either, the priority is 0 and `priority_source` is
   `default`. In a running daemon the four shipped rows are always
   present — clearing the table restores them — so this outcome only
   applies when a table was rebuilt without any of them.

An explicit 0 is still explicit: it pins the Automaton to the back of
the queue and is not re-resolved from the kind table.

## Setting the kind table

The table accepts a JSON object (`{"bug":90,"maintenance":50}`) or
`kind=priority` pairs (`bug=90,maintenance=50`). Values are whole
numbers 0 or greater; a non-number, a fraction or a boolean is refused
with a clear message. An empty value clears the table back to the
shipped defaults.

### CLI

```console
# Show the current table (and the whole autonomous config)
datawatch autonomous config-get

# Set two rows (the shipped values for the other kinds stay)
datawatch autonomous config-set '{"priority_by_kind":{"bug":90,"maintenance":50}}'

# Clear the table back to the shipped defaults
datawatch autonomous config-set '{"priority_by_kind":{}}'
```

The same field is also a plain config key:

```console
datawatch config set autonomous.priority_by_kind '{"bug":90,"feature":25}'
datawatch config get autonomous.priority_by_kind
```

### REST

```console
# Read
curl -s https://localhost:8443/api/autonomous/config | jq .priority_by_kind

# Write (PUT merges; other autonomous fields are untouched)
curl -s -X PUT https://localhost:8443/api/autonomous/config \
  -H 'Content-Type: application/json' \
  -d '{"priority_by_kind":{"bug":90,"feature":25}}'

# Same field through the general config endpoint
curl -s -X PUT https://localhost:8443/api/config \
  -H 'Content-Type: application/json' \
  -d '{"autonomous.priority_by_kind":{"bug":90,"feature":25}}'
```

### MCP

```
autonomous_config_get                                    # read
autonomous_config_set priority_by_kind="bug=90,maintenance=50"
autonomous_config_set priority_by_kind='{"bug":90}'      # JSON object also works
autonomous_config_set priority_by_kind=""                # clear back to defaults
```

Omitting the `priority_by_kind` argument leaves the stored table
unchanged.

### Web app (PWA)

Settings > General > Automata > **Capacity priority by kind**. The
field takes `kind=priority` pairs (the placeholder shows the shipped
rows, `bug=80, maintenance=70, feature=20, other=0`); saving an empty
value clears the table back to the shipped defaults.

### Comm channel

```
configure autonomous.priority_by_kind=bug=90,maintenance=50
configure autonomous.priority_by_kind=        ← clears the table
```

Both keys are listed in the comm channel's `configure` help output.

## Pinning one Automaton

A priority pinned directly on an Automaton wins for that Automaton no
matter what the table says — including an explicit 0, which holds it
at the back of the queue.

### MCP

```
autonomous_prd_set_priority id=<automaton-id> priority=100
```

### REST

```console
curl -s -X POST https://localhost:8443/api/autonomous/prds/<automaton-id>/set_priority \
  -H 'Content-Type: application/json' \
  -d '{"priority":100}'
```

### Web app (PWA)

Open the Automaton and set **Priority** in its settings (the same
field the create/edit form shows as *Capacity queue priority — higher
runs first*).

There is no CLI subcommand for the per-Automaton pin; the CLI surface
is the kind table above. (The related `datawatch session priority
<id> <n>` command sets a *session's* queue priority, not an
Automaton's.)

## Reading the result

- **`autonomous_prd_get`** / `GET /api/autonomous/prds/{id}` returns
  `priority` and `priority_source` on the Automaton.
- **`datawatch autonomous prd-get <id>`** prints the same fields.
- **`capacity_status`** / `datawatch capacity` / the PWA Capacity card
  show the wait queue; each entry carries its priority so you can see
  the order the queue will actually serve.
- **Web app:** Automaton entries that carry a priority source always
  show a priority badge — the value plus the source label
  (`explicit` / `kind` / `default`) — with a tooltip explaining where
  it came from. (An entry with a value but no source on record, e.g.
  written before sources existed, shows the value alone.)

## Defect intake automation

`autonomous.defect_intake_automation` names the Automation that new
kind:bug defect filings are offered to. It is empty by default, on
purpose — each installation names its own Automation, and a filing
without one set is recorded, not filed into any Automation. The daemon
never guesses one.

When an Automaton is filed with kind `bug`:

- the setting names an Automation → the filing is **offered** to that
  Automation's bugs stage (on a non-draft Automation the insert lands
  as held until an operator approves; on a draft it lands at once).
  Outcome: `offered`.
- the setting is empty → the filing is recorded with outcome
  `not_configured`; nothing is offered.
- the Automaton's kind is not `bug` → outcome `skipped`.
- the offer was attempted but failed → outcome `error`, with the cause
  in the record's message.

The outcome lands on the Automaton itself (`defect_intake`) so every
read surface — `autonomous_prd_get`, the PWA, the CLI `prd-get` —
shows what happened. The intake never fails the filing and never
blocks it; an offer error is recorded, not raised.

### Setting it

```console
# CLI
datawatch autonomous config-set '{"defect_intake_automation":"MyAutomation"}'
datawatch autonomous config-set '{"defect_intake_automation":""}'   # clear

# REST
curl -s -X PUT https://localhost:8443/api/autonomous/config \
  -d '{"defect_intake_automation":"MyAutomation"}' -H 'Content-Type: application/json'

# MCP
autonomous_config_set defect_intake_automation="MyAutomation"
autonomous_config_set defect_intake_automation=""     # clear

# Web app: Settings > General > Automata > "Defect intake Automation"
# Comm:  configure autonomous.defect_intake_automation=MyAutomation
```

## A full walk-through

1. See where things stand:

   ```console
   datawatch autonomous config-get | jq .priority_by_kind
   # {"bug":80,"maintenance":70,"feature":20,"other":0}
   ```

2. Raise the fast lane and drop new work further back:

   ```console
   datawatch autonomous config-set '{"priority_by_kind":{"bug":90,"feature":10}}'
   datawatch autonomous config-get | jq .priority_by_kind
   # {"bug":90,"maintenance":70,"feature":10,"other":0}
   ```

3. File a bug and a feature, then watch the queue when capacity is
   tight:

   ```console
   datawatch autonomous prd-get <bug-automaton-id>   # priority 90, priority_source "kind"
   datawatch autonomous prd-get <feature-automaton-id>  # priority 10, priority_source "kind"
   datawatch capacity                                # the bug lines up ahead
   ```

4. One Automaton is urgent regardless of its kind — pin it:

   ```
   autonomous_prd_set_priority id=<automaton-id> priority=100
   ```

   It now reads `priority 100, priority_source "explicit"` and stays
   first even after the table changes.

5. Point defect filings at your intake Automation and file a kind:bug
   Automaton; the record shows `defect_intake.outcome: offered` with
   the Automation name.

## Rules worth knowing

- **Whole numbers 0 or greater only.** `bug=high`, `bug=10.5` and
  `bug=true` are all refused with a clear message; nothing is stored.
- **Explicit always wins**, including an explicit 0.
- **`other` is the fallback row**, not only a kind name — a kind with
  no row of its own (including your own custom kinds) takes `other`'s
  priority.
- **Clearing the table** (`{}`, an empty string, or the empty comm
  value) restores the four shipped rows, so a kind of your own
  without a row of its own still falls back to `other`'s value and
  resolves with `priority_source: kind`.
- **Defect intake is bug-kind only** and never guesses an Automation;
  an empty setting means "record, don't file".
- Priority changes the *order of admission*, not the pools: an
  Automaton still needs a free slot in every pool it uses, and it
  still waits the normal way (with the normal wait timeout) when none
  frees — see [Automata capacity](automata-capacity.md).
