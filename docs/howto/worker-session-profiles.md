---
docs:
  index: true
  topics: [worker, session, launch, profile, context, tokens, mcp, cost, verifier, judge, planner, autonomous]
---
# How-to: worker session profiles

## What changes at launch

A worker, verifier or judge session does not have to start with the operator's full harness — the complete rules and context files, the platform context, and the full MCP tool definitions. Instead, its launch **resolves a worker session profile** by the rule in [How a profile is resolved](#how-a-profile-is-resolved), and the session starts with a starting context and a tool set **chosen for the job it is about to do**.

- **Where it applies.** Worker, verifier and judge launches each resolve their profile and apply it to the session start. The verifier and judge launches resolve it when they start their one-shot session and send the resolved profile with the start request, so the rollout step that moves the `verifier` role to `minimal` takes effect for those sessions (see [Rollout](#rollout)).
- **With nothing configured, no profile is applied.** The session starts with the full harness, exactly as it did before profiles existed.
- **Once a role or kind default is configured**, the launch resolves against it. A configured role default applies to kinds that have no configured kind entry.
- **A profile that cannot be applied falls back to the full harness.** The fallback is recorded on the attempt with its reason, so the session is never silently smaller than the operator expects.

The four built-in profiles and the defaults table are described below. Each launch's outcome is visible on the task record, on the Automaton's decision list, and in the profile-in-force read (see [Setting and reading it](#setting-and-reading-it)).

What a worker session gets from any profile:

- everything the task needs to be judged — its spec, its constraints, its verification script path, its changed-file scope;
- the project's rules, in the level its profile calls for (full file pointer, rules digest, or neither);
- the tools its profile allows.

What does not change:

- **Sessions you start yourself are exactly as before.** A session you begin from the PWA, the CLI, a comm channel or a chat client starts with the full harness, as it always has. Profiles describe worker, verifier and judge sessions only.
- **What a task finishes is unchanged.** A task still ends with the same result, the same verification, the same reports. Only what the session starts with is smaller.

## Why this exists

Measured by the operator on 2026-11-08 (recorded in [the autonomy handover plan](../plans/2026-11-03-autonomy-handover-plan.md)): a task session starts with a **median of about 85,000 tokens** of context and runs a median of 13 turns for about 8,000 output tokens. A bare `claude -p` start in a task-like folder carries about **64,000 tokens by default and about 28,000 with the MCP servers switched off** — so the MCP tool definitions alone are **about 36,000 tokens, roughly 56%, of every session start**. A task that needs a small start pays that 85,000 on every attempt; a verifier and a judge pay it on every run, and every retry and ladder hop pays it again.

The operator's direction: keep every session that starts with the full context only when the job needs it, and make the starting context and the tools **configurable by every datawatch user** — not by editing a file in the daemon, but through the settings the user already uses.

## What a worker session profile is

A profile is a **named, configurable set** of the things a session starts with:

| Field | What it chooses |
|-------|-----------------|
| **mcp** | which MCP servers the session can use: **none**, **the datawatch server only**, or a **named list of servers** |
| **tools** | the session's tool set: **all tools**, or an **allow-list** of named tools |
| **skills** | whether skills are on or off |
| **context** | the level of starting context: **minimal**, **digest** or **full** — the task's spec and constraints, the project's rules in the level the profile names (full, rules digest, or neither), and the daemon's platform context |
| **model and effort** | which model the session runs and at what effort, so a cheap job does not start on the strongest model |

A profile with `mcp: none` and a small context level starts a session with none of the roughly 36,000 MCP tokens measured on 2026-11-08 and none it did not need — while still having everything the task's spec names.

## The built-in profiles

Four built-in profiles ship, **named after what they do**:

- **minimal** — the smallest that works: the task's spec and constraints, a fixed tool set for coding work (the file and shell tools, allow-listed in the code as `Read`, `Edit`, `Write`, `Bash`, `Glob`, `Grep`), no MCP servers, no skills. The context level is `minimal`.
- **digest** — `minimal` with the datawatch MCP server (the session keeps the ability to read its own session, the Automaton state and its own records) and the full tool set, and the project's rules as a [rules digest](automata-rules-and-context.md#the-rules-digest) (every heading plus the mandatory lines, cached by content hash) instead of the full rules files. No skills. The context level is `digest`.
- **datawatch-tools** — the task's context as above, plus the datawatch MCP server but no other MCP servers and no skills. The context level is `digest`. This is the profile the planner names on a task's spec when that task needs a tool or an MCP server.
- **full** — what every session starts with: the full rules and context, the datawatch MCP server, the full tool set, skills on. This is "the old way", and it is **one setting away** from any task or role.

**Operator to review.** The field values of the four built-in profiles, and the per-role and per-task-kind defaults in the table below, are a supervisor judgment call made to be "the smallest that works" — check them on first sight and correct them (see the review list at the bottom of this howto).

## The defaults: the smallest that works, with full one setting away

Every **role** — planner, worker, verifier, judge — and every **task kind** — implementation, docs, note, final gate — has a default profile. This is the built-in table the resolution rule falls back to when nothing else is set:

| Default profile | Who has it |
|-----------------|--------------------|
| `full` | the **planner** — its own start is the unchanged one, and it is the one that names the profile on a task |
| `digest` | the **worker** and the **verifier**, each matched to doing or checking a coding job; and **implementation**, **docs** and **final-gate** tasks |
| `minimal` | the **judge**, reading a verdict, and **note**-kind tasks |

That is the "smallest that works" line in the code. `full` is reachable from every other default by one setting — on the task, on the Automaton, on the project or daemon-wide — so a job that needs the old, heavy start has it, and nothing else pays for it.

You can move the defaults with `autonomous.worker_profile_defaults` (the `role` and `kind` keys; see [Setting and reading it](#setting-and-reading-it)). A configured value replaces the built-in default for its role or kind.

## Giving one task the tools it needs

You do not need to change a role default for one job. The planner, when it writes a task, can attach a **profile marker to the task spec** — one line — naming the profile that task needs: the name lands on the task's `worker_profile` field, the task runs under it at launch, and the rest of the Automaton keeps its defaults. This is the same override a planner already makes when it attaches an existing verification script to a task: the marker is part of the spec, not a daemon change.

Reading it back: the marker is the exact line `worker_profile: <name>` in the task's spec. The first such line is the one that counts, and it sets the task's own `worker_profile` field — a value you set on the task's field directly is never overridden by a spec line. Because the task layer outranks every layer below it, the line overrides a profile the Automaton, the project or the daemon-wide setting carries, for that one task only; the Automaton and its other tasks keep theirs.

## Giving a whole project more tools

The resolution has a **project** layer between the Automaton and the daemon-wide name, but there is no project setting that names a profile yet, so today you give a project's work more tools in one of two ways:

1. **Per Automaton.** Set the same profile name on each Automaton that works in the project — `autonomous_prd_set_worker_profile` over MCP, or the Automaton's `set_worker_profile` call over the REST API. Every task in that Automaton launches under it unless a task names its own.
2. **Daemon-wide.** Define the profile you want under `autonomous.worker_profiles` (for example, a copy of `digest` with an `mcp` list that adds one more server), then name it in `autonomous.worker_profile`. This applies to every project that names nothing more specific, so use it only when most of your work needs the extra tools.

For one task rather than a project, use the `worker_profile: <name>` spec line described above.

## How a profile is resolved

A launch resolves its profile **by name, in this order** — the first name that is set wins:

1. the **task** — its own `worker_profile`, set as a field or by the planner's `worker_profile: <name>` spec line;
2. the **Automaton** — its `worker_profile`;
3. the **project** — the profile your project names;
4. the **daemon** — the `autonomous.worker_profile` setting, if you set one;
5. the **task-kind default** — the `kind` key of `autonomous.worker_profile_defaults` for that kind, or the built-in kind default when you have not set one;
6. the **role default** — the `role` key of `autonomous.worker_profile_defaults` for that role, or the built-in role default when you have not set one.

A tier that is set but empty is just unset, so resolution continues to the next tier. That is also how a profile you define for yourself is used: define it once (per project or daemon-wide), and every role, task kind or task that names it gets it.

With nothing configured at any tier, no profile is applied and the session starts with the full harness.

**A profile name the daemon does not know is refused** at the surface you set it — the refusal names the valid names — so a typo can never silently widen or narrow a session. The one place the rule handles an unknown name instead of refusing it: if a name is set on a task or an Automaton and is not present when the rule runs (for example, removed after being named), the rule stops the search at that tier, applies the role's default, and records the decision — the requested name, the tier it came from, the profile applied and the reason. That decision shows on the task's record and in the Automaton's decision list.

**A profile that cannot be applied falls back to the full harness.** When a resolved profile cannot be applied at launch, the session starts with the full harness instead, and the attempt records the fallback with its reason. Nothing is silently dropped.

## The shape of the start

**The MCP selection, pinned.** The launcher writes the session's own MCP config into its working directory — `worker-mcp.json`, mode `0600`, written atomically (a temporary file renamed into place), with deterministic bytes, so a launch under the same name and the same profile is reproducible. Mode `none` lists no servers; mode `datawatch` lists exactly the datawatch server; mode `list` lists exactly the named servers. A `list` entry that is missing from the daemon's own configuration fails the launch before anything is written, and the refusal names every missing one — a session on `list` never starts with a partial server set. The launch carries that file as its **only** MCP configuration, so the rest of the daemon's server set does not leak in, and the project's or the instance's own `.mcp.json` is never touched.

**The tools and the skills switch, on the launch.** The tool field maps to the launcher's allow-list — an allow-list only, so a profile can narrow a session's tools and never widen them; `all` emits nothing, and profiles carry no deny-list. The skills field is an on/off switch on the launch: `false` turns skills off for that one launch, `true` leaves them as the daemon sets them.

**A deterministic starting context.** For a given role and profile, the starting block the session reads first is built **deterministically**: the same bytes, every time, in a fixed section order — the **role** line, the **profile** line (name, MCP mode and servers, tools, skills, context level), the **constraints** from the starting pieces, then the **rules digest** (for `digest` and above), then the **full rules** (for `full`), and the **task** — always last. Empty rules text is skipped, so a level never produces a blank section. The block is written into the working directory as `worker-starting-context.md` (mode `0600`) and appended to the session's system prompt for that one launch. Two effects:

- **Prompt-cache reuse.** A retry, a ladder hop to the next worker, a verifier and a judge on the same role-profile start share the context prefix byte-for-byte, so the model provider's prompt cache applies across them where the profile is the same. A session that reuses the role and profile does not re-send the same 85,000 tokens it re-sent before; it re-sends the delta its profile actually adds.
- **The cost report can compare profiles.** Because the start is deterministic per role and profile, the tokens a profile costs are a stable figure, not a per-session accident.

**The "not applied" record.** Not every backend can honour every field — a skills switch, a tool allow-list, an MCP restriction it does not implement. When a session starts with a profile, its start record carries **one outcome per profile field — mcp, tools, skills, context, model, effort, in that stable order** — from the backend that launched it. A field the backend honours is recorded as applied; a field it cannot is recorded as **not applied** with the reason "backend cannot honour this field", and the session still gets everything the task needs to complete the work: what a worker gets is never smaller than the task's spec and its verification requirement, the profile can only ever shrink the start, not the work. Claude Code honours every field in its own launch path; a backend that honours none records every field as not applied. The record is one line to summarise — one `field=applied` or `field=unapplied: <reason>` entry per field, in the same order.

**The measured starting context, where it shows.** An attempt's record carries the **name of the profile** its worker started under **and the size of its starting context** (an attempt under no named profile carries both empty, and records no size — never a zero). That size is the estimate made from the starting context generated at launch (one token per four bytes, source `estimated`), and it becomes the reported input tokens when a runner reports them (source `reported`). Read them where you already read cost:

- **The cost report.** A group's start — a worker, a verifier, the whole Automaton — shows the starting context with the profile it ran under, so you can read "worker on `digest` started at ~X tokens; judge on `minimal` at ~Y" instead of one flat figure (about 85,000 when measured on 2026-11-08) you had to know was mostly MCP definitions. The profile name is on the attempt record, so any grouping (Automaton, Automation, backend, week) can carry it.
- **The stats and the usage advice line.** A session's stats and the usage advice line that reads your spend against the week carry the starting context and profile the same way, so the advice you get says which profile is costing what.
- **The attempt record.** A task's record (the Automaton detail, the REST read, the MCP read) names the profile it ran and the tokens it started with, so a task that ran on `full` is marked as such, not hidden in the average.

Compared against the 85,000-token median baseline from 2026-11-08, a session on `minimal` or `digest` that also turns its MCP servers off should start a large fraction lower — the 36,000 MCP definition tokens and the difference between the full rules files and a digest are the two blocks this change removes. Re-measure on your own fleet before you price anything; the baseline was one machine, one day.

## Setting and reading it

The profiles sit in the daemon's autonomous config, right beside every other autonomous setting — the same config a datawatch user already sets — and every surface of it sets and reads the keys:

| Surface | Set | Read back |
|---------|-----|-----------|
| **Settings file** | the `autonomous.` block in `~/.datawatch/config.yaml`: `worker_profiles`, `worker_profile_defaults` (`role` and `kind`), `worker_profile` | the three keys as stored |
| **Web** | the **Worker session profiles** group in Settings: the daemon-wide name (empty clears it), a select per role and per task kind, and your user profiles as one JSON object (empty clears them) — each field saved when it changes | the group shows the values as set; the selects offer the four built-ins plus the user profiles you define |
| **REST API** | `PUT /api/config` with the `autonomous.worker_profiles`, `autonomous.worker_profile_defaults` and `autonomous.worker_profile` keys; `POST /api/autonomous/prds/{id}/set_worker_profile` sets the one profile an Automaton's sessions start with (empty clears the override) | `GET /api/config` carries the three keys as set, and its `autonomous.in_force` block carries the profile in force for each role and task kind |
| **MCP** | `config_set` with the same three keys; `autonomous_prd_set_worker_profile` sets the one Automaton's profile name | `get_config section=autonomous` reads the keys as set; `worker_profile_status` reports the built-in names, the profile in force for each role and task kind (its `in_force` block), and the latest start-up averages |
| **CLI** | `datawatch config set` on `worker_profile`, `worker_profiles`, `worker_profile_defaults.role` and `worker_profile_defaults.kind` — checked against a local copy of the config before anything is sent, so a refused value never reaches the daemon; any other `worker_profile*` key is unknown and refused | `datawatch config get` on those keys, read from the `autonomous` block, followed by the profile in force for each role and task kind |
| **Comm channel** | `configure autonomous.worker_profiles=...`, `configure autonomous.worker_profile_defaults=...` and `configure autonomous.worker_profile=...` | `configure help` and `configure list` name the three keys; a bare key answers with its one-line help, pointing here, and the profile in force for each role and task kind |
| **Web settings page** | the **Profile in force** section of the Worker session profiles group | one line per role and task kind: the profile it launches with, and the tier that set it |

- `autonomous.worker_profiles` — the profiles you define, keyed by name; each value is a profile's fields (`mcp`, `tools`, `skills`, `context`, `model`, `effort`). A user profile may not reuse a built-in name.
- `autonomous.worker_profile_defaults` — the `role` and `kind` defaults, which replace the built-in table above for the roles and kinds you set; the valid role keys are `planner`, `worker`, `verifier`, `judge`, the valid kind keys are `implementation`, `docs`, `note`, `final_gate`, and every value must name a built-in or one of your profiles.
- `autonomous.worker_profile` — the daemon-wide (global-layer) profile name, if you want one for everything that names none.
- on a **task** or an **Automaton** — the `worker_profile` field, the per-task and per-Automaton names that beat the daemon-wide name for the task in question.

Any value a surface does not accept is refused where you set it, and the refusal names the valid ones — a built-in name a user profile reuses, a profile that fails its own checks, a defaults or global name that matches nothing, an unknown role or kind key.

The valid profile names are the four built-ins or a profile you have defined in `autonomous.worker_profiles`. The reference is in [config-reference.yaml](../config-reference.yaml) (the `autonomous.` block) and [implementation.md](../implementation.md) (the profile-settings section).

## Rollout

Move a task kind or role to a smaller profile one step at a time, and widen a step only when the evidence allows it.

1. **The docs kind on `minimal`.** Set `autonomous.worker_profile_defaults.kind.docs` to `minimal` through any surface in the [table above](#setting-and-reading-it) — the settings file, the web settings group, `PUT /api/config`, the MCP `config_set`, `datawatch config set`, or the comm channel `configure`. Docs tasks cover the parity-issue-draft work too: that work has no task kind of its own in the code, so it runs under the `docs` kind's default.
2. **Changelog and final-gate kinds.** Once step 1 is steady, move the `final_gate` kind (and the changelog work that runs under it) to the next profile down.
3. **Verifiers.** Then move the `verifier` role to `minimal`. Verifier launches apply the profile in force for their role (see [What changes at launch](#what-changes-at-launch)), so this step changes the verifier sessions as well as the profile in force read. Check the verifier's start record names the profile you set before you widen the step.
4. **Code tasks.** Last, move the `implementation` kind and the `worker` role.

Widen a step — take on the next step's kind or role — only while the per-profile attempt cost report in [cost-report.md](cost-report.md#the-start-up-tables) shows the **failed-attempt rate not rising** for the profile you have already moved. If the failed-attempt rate rises, put the kind or role back on its previous profile and leave the next step alone.

## What to review when you first see it

The operator direction allows corrections, and these are the places the correction is due:

- **The defaults table against your fleet.** The built-in table above — the planner on `full`, the worker, the verifier and implementation, docs and final-gate tasks on `digest`, the judge and note-kind tasks on `minimal` — is what a launch falls back to when nothing is configured, set to the smallest that works. If a role is under-equipped on your fleet (a worker that needs a tool the `digest` default does not give it), the spec-line marker on one task fixes that one task, and `autonomous.worker_profile_defaults` fixes the rest.
- **Verifier and judge start records.** Confirm on your build that a verifier or judge session's start record names the profile the profile-in-force read shows for its role. The resolver is `ResolveSessionProfile` in `internal/autonomous/worker_profile_launch.go`, called by `withSessionProfile` in `cmd/datawatch/session_judgment.go` on the verifier and judge launch paths. The field-level "applied" outcomes for these sessions were not re-checked in this docs pass.
- **The field values of the four built-ins and your own profiles** as they apply on your backends: the allow-lists in `tools`, the named server list in `mcp`, and the context level of each.
- **The per-surface names and read-backs.** The surfaces above set and read the keys under the names in the table — check they read the way you'd say them, and that a refused value names the valid ones, and that the profile-in-force read names the right tier on each surface.
- **The measured starting context in your own cost report.** The 85,000-token baseline and the 36,000 MCP block are one machine's 2026-11-08 figures. When you see the profile name and the measured start against them across a representative window, you can price the difference and, where a profile costs more than its job needs, move it down one level.

## Related

- [Rules and context](automata-rules-and-context.md) — the levels a profile's context field names: the rules file, the context files, the digest, the planning record.
- [Platform context](sessions-deep-dive.md#what-a-session-is-told-about-datawatch) — the daemon's own "what is datawatch" block that every session, including workers, still receives.
- [Cost report](cost-report.md) — the per-profile and per-role start-up tables that read the attempt records, and where you read the difference between profiles.
- [Failure escalation](failure-escalation.md) — the ladder and the retries this change makes cheaper: each hop to the next worker re-sends only the delta its profile adds, over a shared, cache-friendly prefix.
