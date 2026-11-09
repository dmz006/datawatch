---
docs:
  index: true
  topics: [release, size, milestone, version, semver, merge, pipeline, automation, automata, autonomous]
exec_params:
  - {name: project, required: false, description: "Project name; leave empty for the daemon-wide values"}
exec_steps:
  - tool: autonomous_merge_pipeline_get
    description: Read the release settings in effect for the project (the size by kind, the major trigger, the cadence and the version scheme), the pipeline they apply to and where it came from
    args: {project: "{{params.project}}"}
    read_only: true
---
# How-to: Release size and milestones

Every time the merge pipeline ships an Automaton, the project's version moves
by a step. This guide explains how that step is decided. It covers the four
layers that decide the **release size**, the **breaking-change flag**, the
**milestone Automation**, the **project kind map** with the **major trigger**,
the **release cadence** and the **version scheme**, and then follows one idea
from its start to the major release it grows into. It shows each setting on
every surface: the web UI, REST, MCP, the CLI, chat and YAML.

It belongs to the [merge pipeline](merge-pipeline.md): the pipeline decides
*how* a branch ships, and the release size decides *how far* the version moves
when it does.

> **What is wired today.** The daemon stores every setting and flag in this
> guide and resolves the release size of an Automaton from them, the same way
> on every surface. The merge runner's version step consuming that size, and the
> approval modes the breaking-change flag and a major step point at, arrive in
> the work that follows this release. Until then, setting a size, a flag or a
> milestone does not change what a ship bumps. Everything below is accurate
> about what is stored and resolved, and about the order of the layers; the
> worked story shows the intended result once the version step reads them.

## Base requirements

- A running daemon you can reach from the surface you prefer
  ([setup](setup-and-install.md)).
- A project with a merge pipeline, or the built-in one
  ([merge pipeline](merge-pipeline.md)). The release settings apply to
  whichever pipeline is in effect.
- Nothing else. Every setting is unset by default and reads the built-in
  default, so a project that never touches them resolves exactly as it did
  before.

## The four layers of release size

A release size is one of four words:

| Size | What the merge adds to the version |
|---|---|
| `patch` | the third number: 1.4.2 becomes 1.4.3 |
| `minor` | the second number, and the third goes back to 0: 1.4.2 becomes 1.5.0 |
| `major` | the first number, and the others go back to 0: 1.4.2 becomes 2.0.0 |
| `none` | nothing: the merge makes no release |

The daemon resolves the size of each Automaton by asking four layers in turn,
most specific first. The first layer that has an answer decides, and the result
also reports which layer it was.

| # | Layer | What decides it | Example |
|---|---|---|---|
| 1 | **Explicit** | The `release_size` set on the Automaton itself: `patch`, `minor`, `major` or `none`. It wins over everything below, including an explicit `none` and an explicit `major` under any major trigger. | You set `release_size: patch` on an Automaton whose kind is `feature`. It resolves to `patch`, from the Automaton. |
| 2 | **Milestone** | The Automaton belongs to an Automation (an arc) that is marked as a milestone, every member has finished, and the project's `major_trigger` is `milestone`. | The last member of a milestone arc ships while `major_trigger` is `milestone`. It resolves to `major`, from the Automation milestone. |
| 3 | **Project kind map** | The `release_size_map` entry for the Automaton's kind, with the project's entries layered over the daemon-wide ones. | You add `docs: none`. An Automaton of kind `docs` resolves to `none`, from the project map. |
| 4 | **Built-in default** | Kind `feature` steps `minor`, kind `bug` steps `patch`, and any other kind steps `none`. | A `feature` Automaton with nothing set resolves to `minor`, from the built-in default. |

Two properties hold for every Automaton:

- **A major is never inferred.** The daemon never reads the diff to decide a
  size, and a kind alone never produces a major. A `major` entry in the kind map
  is accepted and stored because you wrote it, but the resolver skips it and
  falls through to the next layer. A major comes only from an explicit
  `release_size: major` on an Automaton, or from a completed milestone under the
  `milestone` trigger.
- **Unset means unchanged.** An Automaton with no `release_size`, an Automation
  with no milestone mark and a project with no settings all read the built-in
  default.

### Set and read the size of one Automaton

The word `clear` removes the explicit size, so the layers below decide again.

| Surface | How |
|---|---|
| Web UI | Open the Automaton, then its settings: choose **Release size** (resolve again, patch, minor, major or none). The list row and the detail header show a chip with the size and the layer that decided it. |
| CLI | `datawatch autonomous prd-set-release-size <automaton-id> <patch\|minor\|major\|none\|clear>` |
| MCP | `autonomous_prd_set_release_size` with `id` and `release_size` |
| REST | `POST /api/autonomous/prds/{id}/set_release_size` with `{"release_size": "patch"}` (an empty string clears it) |
| Chat | `autonomous set-release-size <automaton-id> <patch\|minor\|major\|none\|clear>` |

## The breaking-change flag

An Automaton can carry a `breaking_change` flag. Set it when the change breaks
what an operator or a caller relies on: a removed setting, a changed response, a
renamed command.

The flag is separate from the size. It does not change the size the layers
resolve: an Automaton marked as breaking that resolves to `patch` still resolves
to `patch`. What it adds is a second answer: the result also reports that this
merge must go through the **major approval mode**, so a breaking change is never
merged quietly, even when it would otherwise ship as a patch or a minor. That is
how a breaking change stays visible to you whatever step it lands on.

| Surface | How |
|---|---|
| Web UI | Open the Automaton settings and switch **Breaking change** on or off. The row and detail header show a breaking marker. |
| CLI | `datawatch autonomous prd-set-breaking-change <automaton-id> <true\|false>` |
| MCP | `autonomous_prd_set_breaking_change` with `id` and `breaking_change` |
| REST | `POST /api/autonomous/prds/{id}/set_breaking_change` with `{"breaking_change": true}` (the field is required, so an omitted value never changes the flag) |
| Chat | `autonomous set-breaking-change <automaton-id> <true\|false>` |

## The milestone Automation

An [Automation](automation-chains.md) is a named chain of Automata. Mark one as
a **milestone** when the whole arc is a major piece of work: a big feature grown
out of many smaller merges. Marking it does nothing while its members ship. Each
member resolves its own size by the four layers, so the arc ships as ordinary
minors and patches. When every member has finished, the Automation completes,
and from then on the milestone layer applies: if the project's `major_trigger`
is `milestone`, the release raises the **major** step. With any other trigger
the flag changes nothing, and an Automation without the flag is an ordinary arc.

| Surface | How |
|---|---|
| Web UI | In the Automation tab, open the Automation and use **Mark as milestone** (or **Unmark milestone**) in the detail header. A **Milestone** badge shows on the list row and the header. |
| MCP | `automation_create` or `automation_update` with `milestone: true` (or `false` to clear it) |
| REST | `POST /api/automations` or `PUT /api/automations/{id}` with `{"milestone": true}`; on an update an omitted value keeps the stored one |
| CLI | no dedicated flag: send the `milestone` field through REST, or use the MCP tool |
| Chat | not available: use the web UI, REST or MCP |

## The project kind map and the major trigger

Layers 3 and 4 read the **kind map**, `release_size_map`: a table from an
Automaton's kind to the size its merge adds. The built-in map is
`feature: minor` and `bug: patch`. Anything you add layers over it for that
kind only; kinds you do not name keep the built-in size, and a kind named by no
layer steps `none`.

```yaml
autonomous:
  merge_pipeline:
    release_size_map:
      feature: minor
      bug: patch
      docs: none
    major_trigger: milestone
    projects:
      my-service:
        release_size_map:
          chore: patch
        major_trigger: never
```

The **major trigger**, `major_trigger`, decides when the daemon is allowed to
raise a major step on its own:

| Value | Meaning |
|---|---|
| `manual` (default) | A major happens only when you name it: an explicit `release_size: major` on an Automaton. A completed milestone does not raise one. |
| `milestone` | A completed milestone Automation raises a major as well. |
| `never` | A completed milestone never raises a major. An explicit `release_size: major` on an Automaton still applies, because you wrote it. |

Each value is checked when it is written. A size outside `patch`, `minor`,
`major`, `none`, a kind name with a space in it, or a trigger outside the three
is refused with the key named, and nothing is stored.

## Cadence

`release_cadence` says how often a release is cut. Only **`per_merge`** is
available: one release step for every merged Automaton, which is the default.
`batch` and `manual` are reserved names. Writing either one is refused with a
message saying that cadence is not available yet, and nothing is stored. Any
other value is refused as invalid, and the message names `per_merge` as the one
to use.

## Version scheme

`version_scheme` says how versions are numbered. Only **`semver`** is available:
major.minor.patch, which is the default, and is why the table above reads the
way it does. Any other name is refused with a message saying that scheme is not
available yet.

## Set the four keys

All four keys live under `autonomous.merge_pipeline`, and each can be overridden
for one project under `autonomous.merge_pipeline.projects.<project>.<key>`. An
unset key inherits the daemon-wide value, then the built-in default.

| Key | Default | Accepts |
|---|---|---|
| `release_size_map` | `feature: minor`, `bug: patch` | kind to `patch`, `minor`, `major` or `none` |
| `major_trigger` | `manual` | `manual`, `milestone` or `never` |
| `release_cadence` | `per_merge` | `per_merge` only |
| `version_scheme` | `semver` | `semver` only |

| Surface | How |
|---|---|
| Web UI | Settings > Automation > Merge pipeline: **Release size by kind** (a row per kind with its size, then **Save release sizes**), **Major release trigger**, **Release cadence** and **Version scheme**. Cadence `batch` and `manual`, and a scheme other than `semver`, are listed but the save is refused with the daemon's reason. Each of the four controls has a `?` link that opens the [Release size](../datawatch-definitions.md#release-size) entry in the definitions. |
| YAML | The block above in `~/.datawatch/config.yaml` |
| CLI | `datawatch config set autonomous.merge_pipeline.major_trigger milestone` and `datawatch config set autonomous.merge_pipeline.release_size_map docs=none,chore=patch`; per project: `datawatch config set autonomous.merge_pipeline.projects.my-service.major_trigger never` |
| REST | `PUT /api/config` with `{"autonomous.merge_pipeline.major_trigger": "milestone"}`; a bad value rejects the whole request with 400 and writes nothing |
| MCP | `autonomous_config_set` with `merge_pipeline_release_size_map`, `merge_pipeline_major_trigger`, `merge_pipeline_release_cadence` or `merge_pipeline_version_scheme`, and `merge_pipeline_project` for one project |
| Chat | `configure autonomous.merge_pipeline.major_trigger=milestone` |

To read what is in effect for a project, including the four keys: the `Merge
pipeline` card in the web UI, `autonomous_merge_pipeline_get` on MCP,
`datawatch autonomous merge-pipeline show [--project P]` on the CLI,
`GET /api/autonomous/merge-pipeline` on REST, or `autonomous merge-pipeline` in
chat.

## Worked story: from an idea to a major release

You keep a project called `billing-service`. It is at version **3.7.2**, and you
want to replace its invoice engine, a change big enough that you want it to end
in a major release. You have not planned the steps yet; you only have the idea.

**1. The root idea becomes a milestone Automation.** You create an Automation
named *New invoice engine* and mark it as a milestone. You set
`major_trigger: milestone` for the project, because by default only a named
major is allowed:

```
datawatch config set autonomous.merge_pipeline.projects.billing-service.major_trigger milestone
```

On MCP you create the Automation with `automation_create` and `milestone: true`;
in the web UI you press **Mark as milestone**. The list row now shows the
**Milestone** badge.

**2. The members are ordinary Automata.** You add four members, one after
another: a schema change (kind `feature`), the new calculation core (`feature`),
a rounding fix found along the way (`bug`) and the switch-over that deletes the
old engine (`feature`). Nothing about them is special; none of them sets a
`release_size`, so each resolves through the layers.

**3. They ship as minors and patches.** When each member merges, its size is
resolved the same way: the daemon has the layers, but the version step that
applies the result is not wired yet (see the note at the top), so the table
below is the intended result, not what a ship bumps today. Before the arc
completes, the milestone layer has nothing to say, so layer 3 and 4 decide:

| Member | Kind | Resolved size | Layer | Intended version |
|---|---|---|---|---|
| Schema change | feature | minor | project map or built-in | 3.7.2 to 3.8.0 |
| Calculation core | feature | minor | project map or built-in | 3.8.0 to 3.9.0 |
| Rounding fix | bug | patch | project map or built-in | 3.9.0 to 3.9.1 |

The arc is not finished, so nobody has been surprised by a major. Users on 3.x
keep getting working releases.

**4. The last member completes the Automation.** The switch-over is the last
member. The Automation is marked as a milestone, every member has now finished,
and the project's `major_trigger` is `milestone`, so the milestone layer
decides: the size is **major**, from the Automation milestone. Without the
trigger it would have resolved to `minor` like its siblings, and the arc would
never have produced the major you intended.

**5. The major approval mode holds it for you.** A major is the step you do not
want taken quietly. The release raises the major step under the **major approval
mode**, so it is put in front of you rather than shipping on its own. Once the
approval modes are wired in (see the note at the top), you review the release and
approve it, and the version moves **3.9.1 to 4.0.0**. Today the daemon resolves
the size and reports it; it does not yet take the step.

**6. A breaking change stays visible even on a patch.** Say the rounding fix had
changed how an invoice amount is rounded for existing customers. You would have
set `breaking_change` on that Automaton. It would still resolve to `patch`, but
the result would also report that it must go through the major approval mode,
so you would have been asked about it at step 3, not after the fact.

**7. Check the result.** Read the project's release settings with
`autonomous_merge_pipeline_get`, and each Automaton's chip in the web UI shows
the size and the layer that decided it. A mistake in a setting is refused at
write time with the key named.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| A milestone Automation completed but the release is not a major | `major_trigger` is `manual` or `never`. Set it to `milestone` for the project. |
| A `major` entry in the kind map does nothing | By design: a major is never inferred from a kind. Use an explicit `release_size: major` on the Automaton, or a milestone Automation. |
| `batch` or `manual` cadence is refused | Not available yet. Use `per_merge`. |
| A scheme other than `semver` is refused | Not available yet. Use `semver`. |
| An Automaton shows no size chip | It has no explicit size, and its kind resolves to `none` (or the server did not report a resolved size). A size of `none` and an unset size show nothing unless the breaking-change flag is set. |
| A new kind is not releasing | A kind no layer names steps `none`. Add it to `release_size_map`. |
| A setting saved but a ship still bumps as before | The merge runner's version step does not read the resolved size yet (see the note at the top). |

## See also

- [How-to: Merge pipeline](merge-pipeline.md) for the pipeline these settings apply to.
- [How-to: Automation chains](automation-chains.md) for building the arc.
- [How-to: Post-ship hooks](post-ship-hooks.md) for what runs after a ship.
- [The definitions](../datawatch-definitions.md#release-size) for the short entry of each term.
