---
docs.index: true
docs.title: "How-to: keep your Automata grouped in the tag browser — the automatic tags"
docs.category: Operator Guides
docs.summary: What the auto tags are and where they land in the tag tree, the three templates and the kind labels with their defaults, turning them off daemon-wide or for one Automation, renaming a stage so the stage tag follows, why the system tags cannot be renamed, and filtering progress and the session list by automation, stage and kind on every surface (web, CLI, MCP, REST, chat).
topics:
  - autonomous
  - automata
  - automations
  - tags
  - progress
  - sessions
  - config
  - automation
  - mcp
  - rest
  - cli
  - chat
  - web
  - operator
  - howto
exec_params:
  enabled:
    type: string
    required: false
    description: On or off when turning auto tagging on or off.
exec_steps:
  - name: show-auto-tags
    description: Show the tag tree with the auto tags already in place, and how many Automata sit under each.
    tool: autonomous_tag_list
    required_args: []
    optional_args:
      - counts
    args:
      counts: true
    read_only: true
  - name: turn-off
    description: Turn auto tagging off for this daemon (turn it back on the same way).
    tool: config_set
    required_args:
      - key
      - value
    optional_args: []
    args:
      key: autonomous.auto_tags.enabled
      value: "false"
    read_only: false
  - name: relabel-kind
    description: Change what the kind tag renders as for "other" kind Automata (the feature and bug labels stay at their defaults).
    tool: config_set
    required_args:
      - key
      - value
    optional_args: []
    args:
      key: autonomous.auto_tags.kind_labels
      value: feature:feature,bug:bug,other:support
    read_only: false
  - name: progress-one-automation
    description: Progress for the Automata of one Automation, by its auto tag (a tag id from autonomous_tag_list).
    tool: autonomous_progress
    required_args: []
    optional_args:
      - tags
      - tag_match
    args:
      tags:
        - "1a2b3c4d"
      tag_match: all
    read_only: true
  - name: progress-one-stage
    description: Progress for the Automata of one stage, by the stage tag's id.
    tool: autonomous_progress
    required_args: []
    optional_args:
      - tags
      - tag_match
    args:
      tags:
        - "2c3d4e5f"
      tag_match: any
    read_only: true
  - name: trend-by-tags
    description: The completion trend for the last two weeks, scoped to one stage's auto tag (the tag's id).
    tool: autonomous_progress_trend
    required_args: []
    optional_args:
      - days
      - tags
      - tag_match
    args:
      days: 14
      tags:
        - "2c3d4e5f"
      tag_match: any
    read_only: true
  - name: rename-stage
    description: Rename a stage of one Automation; the stage auto tag follows the new name.
    tool: automation_rename_stage
    required_args:
      - id
      - from
      - to
    optional_args: []
    args:
      id: "<the Automation id>"
      from: test
      to: testing
    read_only: false
---

# How-to: keep your Automata grouped in the tag browser — the automatic tags

**Goal:** your Automata are already organised — grouped by the Automation they
run in, by the stage of that Automation, and by their kind — without you
assigning a single tag, and you can filter on any of those groups.

**Success looks like:** in the tag browser you see three families of tags that
keep themselves up to date as Automata come and go — the Automations,
`stage:…` for every stage name in use, and the kind labels. You can read
progress for one Automation, one stage, or one kind, and the session list can
be narrowed to the sessions of a group.

**Why this exists:** the tag browser was the one place that did not know about
the Automation a worktree lives in or the stage an Automaton is in — those
facts were on the Automation and on the member, nowhere an operator could
filter by. Auto tags copy those facts into the tag registry as ordinary tags,
kept current automatically, so every surface that already understands tags
(progress, the session list, the Automata list) can filter by them.

## What the automatic tags are

Every Automaton that belongs to an Automation gets **three tags of its own**,
computed from the Automation's settings:

| Auto tag | Where it comes from | Default name |
|---|---|---|
| Automation tag | the Automation's own name | `automation:<name>` |
| Stage tag | the member's stage label | `stage:<stage>` |
| Kind tag | the Automaton's kind, through the kind labels | `kind:feature`, `kind:bug`, or `kind:maintenance` (for the `other` kind) |

The three tags are all derived from the Automation: an Automaton that is not
a member of any Automation carries no auto tags at all. The Automation and
stage names appear **verbatim** — whatever is spelled in the Automation, that
is what the tag says.

Three things the design pins down:

- **They are ordinary tags.** They live in the same registry as the tags you
  make by hand: same tree, same subtree counts, same browsing. The only
  difference is who looks after them — the daemon reconciles them, so you
  never assign or unassign one.
- **They are system tags.** A tag the engine keeps has a `system` marker. That
  marker is what makes the tag lock visible: you can recolor a system tag, but
  you cannot rename, move, or delete it (see *The system tags* below).
- **Sessions read them live.** A session started by an Automaton shows the
  three tags as *inherited* (dashed chips, read-only). Changing the
  Automaton's tags changes the inherited set with no write to the session.

Auto tagging is on by default, and every change to it takes effect without a
restart: membership changes, a stage rename, a kind change, deleting an
Automation — the daemon re-reconciles the tags as it happens, renaming a tag
record in place where the rendered name changed (the tag id and every
assignment survive a rename) and reclaiming a tag record no member carries
any more (the stage tag of an emptied stage goes; the kind vocabulary stays).

## The settings

Six keys under `autonomous.auto_tags`, all of them live (a change applies to
existing Automata immediately):

| Key | Default | Meaning |
|---|---|---|
| `autonomous.auto_tags.enabled` | `true` | Whether auto tagging runs at all (only an *explicit* stored `false` turns it off; an unset block is on). |
| `autonomous.auto_tags.automation_template` | `automation:{automation}` | The name the Automation tag renders as. |
| `autonomous.auto_tags.stage_template` | `stage:{stage}` | The name the stage tag renders as. |
| `autonomous.auto_tags.kind_template` | `kind:{kind}` | The name the kind tag renders as. |
| `autonomous.auto_tags.kind_labels` | `feature:feature, bug:bug, other:maintenance` | What `{kind}` renders as, per Automaton kind. |

Templates may reference the placeholders `{automation}`, `{stage}` and
`{kind}` and nothing else — any other `{name}` is refused with a message that
names the placeholder (for example
`autonomous.auto_tags.kind_template: placeholder {owner} is not one of
{automation}, {stage}, {kind}`), and nothing is written. A template that is
empty, has a newline, or renders to an empty name is refused the same way.

The kind labels map the three kinds onto the strings `{kind}` renders as: `other`
becomes `maintenance` by default, so the three default kind tags read
`kind:feature`, `kind:bug` and `kind:maintenance`. Rename a label — for instance
`autonomous.auto_tags.kind_labels other:support` — and every existing kind
tag *named after the label* is renamed with it, tag id and assignments
surviving. The labels are daemon-global: a per-Automation override (below) can
carry its own templates but not its own labels.

**Set them from any surface, all live:** the YAML config file (`auto_tags`
under `autonomous`, as a `kind: label` mapping); `datawatch config set
autonomous.auto_tags.enabled false` (read one back with `config get`); MCP
`config_set` with the key (needs `mcp.allow_self_config=true`); chat
`configure autonomous.auto_tags.enabled=false`; REST `PUT /api/config
{"autonomous.auto_tags.enabled": false}`; and the web app **Settings →
Automata**, under the *Auto tags* heading (enabled toggle, the three
templates, the kind labels). A change that fails validation is refused with
the same message on every surface and nothing is written.

## Turning auto tagging off — daemon-wide or one Automation

Daemon-wide, set the enabled key to `false`:

```bash
datawatch config set autonomous.auto_tags.enabled false
```

```
# MCP
config_set(key="autonomous.auto_tags.enabled", value="false")
```

```
# chat (comm channel)
configure autonomous.auto_tags.enabled=false
```

```bash
# REST
curl -X PUT https://<host>/api/config \
  -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' \
  -d '{"autonomous.auto_tags.enabled": false}'
```

When the block is off this is a removal, not a pause: the engine strips the
three auto tags from the Automation's members and reclaims the tag records
nobody references any more — the Automation tag and the stage tags. The kind
tags are the one exception: they are shared vocabulary across the fleet, so
they stay. Turning the block back on derives the tags again from the current
membership.

One Automation, without touching the rest: give it an **override** on the
Automation itself. Only an explicit override wins (or inherits) per field —
an empty template in the override inherits the daemon's:

```bash
# off for just this Automation
datawatch automation update <id> --auto-tags off

# on, with this Automation's Automation tag spelled differently
datawatch automation update <id> --auto-tags on \
  --auto-tags-automation-template "work:{automation}"

# this Automation's stage tags under their own heading
datawatch automation update <id> --auto-tags on \
  --auto-tags-stage-template "{stage} (mine)"
```

The same four knobs ride with the Automation itself — `enabled` plus the
three templates, kind labels staying daemon-global — in the REST body's
`auto_tags` object on its create and update calls (`POST /api/automations`
/ `PUT /api/automations/<id>`), and on the CLI update above. The web app
shows the same section in the Automation's settings: *Auto tags* with an
**inherit / off / on** choice and the three templates (an empty template
field means inherit). The MCP `automation_create`/`automation_update` tools
do not expose the override as a parameter; the surfaces for it are the CLI,
the REST call and the web app. An update that does not name the block keeps
the override exactly as it was.

## Renaming a stage — the tag follows

Stages are an operator label on a member, and the stage tag is just the stage
name rendered through the template. Renaming the stage therefore renames the
tag, in place:

```bash
datawatch automation rename-stage <id> "test" "testing"
```

```
# MCP
automation_rename_stage(id="<id>", from="test", to="testing")
```

```
# chat
automation rename-stage <id> test testing
```

```bash
# REST
curl -X POST https://<host>/api/automations/<id>/stages/rename \
  -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' \
  -d '{"from": "test", "to": "testing"}'
```

With auto tagging on, every member whose stage was `test` loses the tag
record `stage:test` and gains `stage:testing` — same reconciliation, so tag id
and assignments that were already there survive the rename. Operator tags are
never touched: renaming a stage does not rename any tag an operator created,
and two members whose operator tags happened to share the old stage name keep
them. A missing or empty `from` or `to` is refused before any request goes
out (`400`).

## The system tags — why you cannot rename them

The tags the engine keeps are marked *system*, and the registry enforces it
on every write path (web, CLI, MCP, REST, chat):

- `tag-rename <id> <new-name>` — refused; the name of a system tag is fixed.
- `tag-move <id> <parent>` — refused; a system tag stays where the engine puts it.
- `tag-delete <id>` — refused.
- Recoloring is still allowed — a system tag's *appearance* is not its *identity*.

That is deliberate: the name of a system tag is a value other surfaces depend
on. The stage tag *is* the stage name rendered through your template, the kind
tag is drawn from the kind labels — rename one by hand and the filter, the
counts, and the next reconciliation all disagree about what the tag means. The
right move is to change the template or the label (which renames the record
in place with the id intact), or — if the name is genuinely no longer wanted —
to turn the block off, which pulls the tag off the members and lets the
engine reclaim it.

## Filtering on the auto tags

Because the auto tags are ordinary tags, everything that filters by tags
filters by them. The tag ids are the handles: `autonomous_tag_list` (or the
tag browser) shows the names and ids, and the web list views group the filter
chips by family — **Automation**, **Stage**, **Kind** — with your own tags
underneath (the session list keeps the saved selection; the match mode the
browser shows — ANY by default — applies to the filter too).

### Progress

Progress takes any number of tags plus the match mode. `all` counts only
Automata that carry *every* selected tag; the default `any` counts an Automaton
carrying *at least one*. The `tag=<id>` filters take tag **ids** (the `id`
`autonomous_tag_list` returns for the `stage:build` tag) — look up the id
first, then filter with it. In the examples below, `1a2b3c4d` is the id of
the `stage:build` tag and `4f5e6d7c` the id of `automation:release`.

**Any of a stage, this quarter:**

```bash
# tag 1a2b3c4d is "stage:build"
datawatch autonomous progress --tag 1a2b3c4d --since 2026-10-01
```

```
# MCP
autonomous_progress(tags=["1a2b3c4d"], since="2026-10-01")
```

```
# chat
progress tag=1a2b3c4d since=2026-10-01
```

```bash
# REST
curl 'https://<host>/api/autonomous/progress?tag=1a2b3c4d&since=2026-10-01' \
  -H "Authorization: Bearer <token>"
```

Web/PWA: the Progress card's tag filter, the `stage:build` chip ticked,
match mode ANY.

**One Automation, one stage at once** (`all`):

```bash
# 4f5e6d7c = "automation:release", 1a2b3c4d = "stage:build"
datawatch autonomous progress --tag 4f5e6d7c --tag 1a2b3c4d --tag-match all
```

```
# MCP
autonomous_progress(tags=["4f5e6d7c", "1a2b3c4d"], tag_match="all")
```

```
# chat
progress tag=4f5e6d7c tag=1a2b3c4d tag_match=all
```

```bash
# REST
curl 'https://<host>/api/autonomous/progress?tag=4f5e6d7c&tag=1a2b3c4d&tag_match=all' \
  -H 'Authorization: Bearer <token>'
```

Web/PWA: the same chips, match mode ALL. The two can be combined; `any` and
`all` are the only match modes (anything else is refused as
`tag_match: must be "any" or "all"`), and a tag id that is not in the
registry matches nothing rather than erroring — an empty count or list means
the tag is no longer there, so re-read the names from the tag browser.

The trend takes the same scope, on the snapshots:

```bash
datawatch autonomous progress-trend --days 14 --tag 1a2b3c4d
```

```
# MCP
autonomous_progress_trend(days=14, tags=["1a2b3c4d"])
```

```
# chat
progress trend 14 tag=1a2b3c4d
```

```bash
# REST
curl 'https://<host>/api/autonomous/progress/trend?days=14&tag=1a2b3c4d' \
  -H 'Authorization: Bearer <token>'
```

### The session list

The session list filters by a session's own tags **and** the tags its Automaton
gave it (the inherited set), so a filter on an auto tag reaches the sessions
of every Automaton that carries the tag (the `--tag` ids below are the same
ones as above — `4f5e6d7c`, the id of the `automation:release` tag, and
`1a2b3c4d`, the id of `stage:build`):

```bash
datawatch session list --tag 4f5e6d7c
datawatch session list --tag 4f5e6d7c --tag 1a2b3c4d --tag-match all
```

```
# MCP
list_sessions(tags=["4f5e6d7c"])
list_sessions(tags=["4f5e6d7c", "1a2b3c4d"], tag_match="all")
```

```bash
# REST
curl 'https://<host>/api/sessions?tag=4f5e6d7c&tag_match=any' \
  -H 'Authorization: Bearer <token>'
```

Web/PWA: the session list's tag filter — open **Filters**, pick the chip
under its group heading (or search for it), ANY or ALL, done. An Automaton
has no chip: pick it from the **Find an Automaton** picker. Stage tags have
no chip either; use the session grouping by stage.

### The Automata list

The tag browser's own scope is the Automata: browsing into an Automation tag
shows every Automaton in it, into a stage tag every Automaton in that stage,
into the kind labels the whole kind — with the subtree counts that have always
worked. The Automata list takes the same filter: `GET /api/autonomous/prds?tag=<id>`
lists the Automata under a tag, and `&include_sub=true` (the default) widens
the filter to the tag's whole subtree — so one URL gives you every Automaton
under `automation:release`, or under one of its stages.

## What the automatic tags do not do

- **Do not change a member.** An Automaton's tags are metadata on the tag
  registry, not on the Automation or its budget, edges, or advance rule. A
  filter by them selects; it never moves anything.
- **Do not touch operator tags.** A tag you created is never renamed, merged
  or deleted by the engine — and if the engine would create a tag with the
  name an operator tag already owns, it is refused loudly and the Automation
  is not left half-created; it never silently renames or overwrites yours.
- **Do not survive being turned off — that is the point.** Switching
  `enabled` off strips the three auto tags from the Automation's members and
  reclaims the tag records nobody references any more (the shared kind tags,
  which are fleet vocabulary, stay). Switching it back on re-derives the tags
  from the current membership.

## Next steps

- [automata-tags](automata-tags.md) — the tag registry itself: creating,
  renaming, recoloring, moving, the primary tag, and the attention group.
- [automata-progress](automata-progress.md) — the numbers the filter scopes.
- [automation-queue-advance](automation-queue-advance.md) — the Automations
  these tags describe.
