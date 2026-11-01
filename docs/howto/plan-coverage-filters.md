---
docs:
  index: true
  topics: [autonomous, prd, plan coverage, filters, acknowledgement, findings]
exec_params:
  - {name: suppress_created_files, required: false, description: "Created-files filter to set: true (default) or false"}
  - {name: scoped_requirements, required: false, description: "Scoped-requirements filter to set: true (default) or false"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the four plan-coverage filter values in force
    args: {}
    read_only: true
  - tool: autonomous_prd_plan_coverage_get
    description: Read the stored plan-coverage record: report, findings, the suppressed counts and the filters it was computed under
    args:
      id: "{{params.id}}"
    read_only: true
  - tool: autonomous_prd_plan_coverage_acknowledge
    description: Acknowledge a genuine failing surface so approval can proceed in block mode
    args:
      id: "{{params.id}}"
      note: "{{params.note}}"
    read_only: false
  - tool: autonomous_config_set
    description: Turn a filter off or tighten the requirement scope
    args:
      plan_coverage_suppress_created_files: "{{params.suppress_created_files}}"
      plan_coverage_scoped_requirements: "{{params.scoped_requirements}}"
    read_only: false
---

# How-to: The plan-coverage findings filters

## 1. When you need it

Before an Automaton is approved, the deterministic **plan requirement
coverage gate** reports the spec requirements the plan does not visibly
cover, plus the structural findings (dangling references, a write task with
no files, and so on). In `block` mode (the default) a failing report locks
Approve until you acknowledge it.

The filters matter when the report fails for **noise instead of gaps** —
the two shapes it shows up in:

- **the plan shows dozens of findings about files it creates itself** —
  every new source file, test or doc the plan itself is about to write is
  reported as a "missing file", because it does not exist in the tree yet;
- **or about single words from the spec** — a file name, a backend name or
  a quoted phrase in the spec body is counted as a missing requirement by
  itself.

In both cases you are acknowledging noise, not a real gap. The two
on-by-default filters remove exactly those two kinds of finding and put a
visible count of everything they withheld next to the findings that remain.

## 2. What the two filters do

### Filter 1 — a file the plan creates is not a missing file

`autonomous.plan_coverage_suppress_created_files` (on by default).

A file that a task's title or spec says it creates, adds, writes or
generates — or that is new in the plan's own file list — is the plan's own
output, not a reference to something that should already exist. It is no
longer reported as a missing file.

The allowance has one limit: the file's **parent folder has to exist or be
created by the plan**. A path whose folder nothing in the plan creates is
still reported, so a reference into a folder that does not exist and is not
built is never silently skipped.

**Worked example.** A plan for an alert summary digester names
`internal/digest/summary.go` in its "Write the digest summarizer" task and
lists it in that task's files. Before the filter, if that task also said
"then the tests in `internal/digest/summary_test.go`", everything missing
in `internal/digest/` was a finding. With the filter on, both files are the
plan's own output — the folder appears in the plan too, so both are
suppressed. The same plan also says "wire it into
`internal/scheduler/daily.go`, which does not exist and no task creates" —
that finding still shows up and still fails the report.

### Filter 2 — only scoped sentences count as requirements

`autonomous.plan_coverage_scoped_requirements` (on by default).

Requirements are extracted only from the sections named by
`autonomous.plan_coverage_requirement_sections` (default: the **scope** and
**constraints** sections). Within those sections, a path or config key
counts only through the sentence that holds it — no more standalone file
names — and a sentence shorter than
`autonomous.plan_coverage_min_requirement_chars` characters (default 30) is
dropped before the coverage ratio is computed. A spec that does not carry
any of the named sections falls back to the whole spec, so the filter never
shrinks a spec that did not label its sections.

**Worked example.** A spec body says the daemon should read
`~/.datawatch/datawatch.yaml`, then lists a long *Read first* block of file
names and a quote from a discussion. Before the filter, each of those loose
names can surface as a missing requirement on its own. With the filter on,
only the scope and constraints sentences count; the `datawatch.yaml`
mention inside a constraint sentence counts once, through that sentence,
and nothing in the *Read first* block does.

## 3. Reading the stored record

The record — the report, the findings, the **suppressed counts** (the
created-file findings and the requirements each filter withheld) and the
filters it was computed under — is stored on the Automaton at decompose
time and readable on every surface:

**PWA.** Open the Automaton; the plan coverage panel shows the status line,
the covered/missing requirement list, the findings, and the suppressed
line — "Suppressed by the plan coverage filters: N created-file findings,
M requirement findings". The **`?`** link next to the panel title opens the
[Plan coverage filters glossary entry](../datawatch-definitions.md#plan-coverage-filters).

**REST.**

```sh
curl -sH "Authorization: Bearer $TOKEN" "$BASE/api/autonomous/prds/<id>/plan_coverage"
```

**MCP.**

```
autonomous_prd_plan_coverage_get(id="<id>")
```

**CLI.**

```sh
datawatch autonomous prd-coverage <id>        # adds --json for raw JSON
```

**Chat.**

```
autonomous plan-coverage <id>
```

## 4. Tuning it

The four settings, all under `autonomous.`:

| Key | Default | Meaning |
|---|---|---|
| `autonomous.plan_coverage_suppress_created_files` | on | A file a task says it creates, or that is new in the plan's own file list, is not a missing-file finding while its folder exists or is created by the plan |
| `autonomous.plan_coverage_scoped_requirements` | on | Requirements are extracted only from the named sections; a path or key counts only through the sentence that holds it |
| `autonomous.plan_coverage_requirement_sections` | `scope,constraints` | The comma-separated section names whose sentences count as requirements; names are trimmed and lowercased, an empty list restores the default. If your specs use a `## Requirements` heading, set it to `scope,constraints,requirements` |
| `autonomous.plan_coverage_min_requirement_chars` | `30` | The shortest sentence, in characters, that counts as a requirement; `0` reads the default of 30. Raise it to 60 to drop shorter obligation sentences |

Set them through the **automaton settings card in PWA Settings** (the four
*Plan coverage: …* rows, each with its own **?** help link), or:

```sh
# CLI
datawatch config set autonomous.plan_coverage_requirement_sections=scope,constraints,requirements
datawatch config set autonomous.plan_coverage_min_requirement_chars=60

# REST
curl -sX PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"autonomous.plan_coverage_min_requirement_chars": 60}' "$BASE/api/config"

# Chat
configure autonomous.plan_coverage_requirement_sections=scope,constraints,requirements
```

## 5. Turning a filter off

Both booleans are on when the key is absent:

```sh
datawatch config set autonomous.plan_coverage_suppress_created_files=false   # report every missing file again
datawatch config set autonomous.plan_coverage_scoped_requirements=false      # extract requirements from the whole spec again
```

Or flip either toggle off on the automaton card in Settings. A change
applies to the **next decompose** — an already-stored report keeps the
values it was computed under, and the suppressed counts it shows belong to
the filters it was pinned with.

## 6. What still needs your acknowledgement

Genuine findings still fail the report in `block` mode and are cleared
only by acknowledging that exact failing surface, with an actor and a
non-empty note:

```sh
# Read the failing surface first.
datawatch autonomous prd-coverage <id>

# Acknowledge it.
datawatch autonomous prd-ack-coverage <id> --actor alice \
  --note "the scheduler reference is out of scope for this Automaton; acknowledged"
```

The same path exists on the PWA panel (the **Acknowledge** control), over
REST (`POST /api/autonomous/prds/<id>/plan_coverage/acknowledge`), MCP
(`autonomous_prd_plan_coverage_acknowledge`) and chat. The acknowledgement
is bound to the stored report's fingerprint, and the **suppressed count is
not part of that fingerprint**: an acknowledgement of a genuine finding
stays valid when only the filters change, and a plan change still voids it.

## 7. How to tell a genuine finding from a suppressed one

A suppressed finding is not hidden — it is **counted**. Read the record and:

- **Trust the suppressed line as accounting, not evidence.** It only says
  how much each filter withheld ("N created-file findings, M requirement
  findings"), not what those were.
- **A created-file suppression is honest while the folder exists or is
  created by the plan.** A remaining missing-file finding names a path
  whose folder nothing in the plan builds — that is the kind of genuinely
  broken reference the gate exists to catch, and its path is named in the
  report, so you can verify it against the tree.
- **Check what the record was computed under.** The stored record carries
  the four filter values pinned at decompose time; if the panel still looks
  like it has noise, confirm those values are the ones you expect before
  concluding a filter is off.

See also:

- [autonomous planning](autonomous-planning.md) — the spawn → plan →
  review → run loop and the write-task file declarations.
- [acknowledge flow](autonomous-review-approve.md) — the review flow that
  the gate gates.
- [datawatch-definitions — Plan coverage filters](../datawatch-definitions.md#plan-coverage-filters) — the glossary definition both filters link to from the PWA.
- [operations — Plan requirement coverage gate](../operations.md#plan-requirement-coverage-gate-autonomousplan_coverage_check) — modes, the ack, and the filters reference.
