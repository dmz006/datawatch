---
docs:
  index: true
  topics: [worker, session, launch, profile, context, tokens, mcp, cost, verifier, judge, planner, autonomous]
---
# How-to: worker session profiles

## What changes at launch

A worker, verifier or judge session does not have to start with the operator's full harness — the complete rules and context files, the platform context, and the full MCP tool definitions. Instead, its launch **resolves a worker session profile** by the rule in [How a profile is resolved](#how-a-profile-is-resolved), and the session starts with a starting context and a tool set **chosen for the job it is about to do**. Session starts you run yourself do the same: a start with launch facts (a supervisor or planner start role, a review start kind, or one-shot mode) resolves its kind's row out of the same table as the task launches, and a profile named at the start wins over everything else. All six session kinds — supervisor, operator_interactive, scheduled_command, planner, one_shot, review — sit in that table with a built-in default and the same `kind`-key setting surface; supervisor-role, planner-role, review-kind and one-shot starts are the ones that resolve their rows today (see the paragraph after the [defaults table](#the-defaults-the-smallest-that-works-with-full-one-setting-away)).

- **Where it applies.** Worker, verifier and judge launches each resolve their profile and apply it to the session start. The verifier and judge launches resolve it when they start their one-shot session and send the resolved profile with the start request, so the rollout step that moves the `verifier` role to `minimal` takes effect for those sessions (see [Rollout](#rollout)). Session starts carry the same rule at their own layer: a start names a profile (`worker_profile_name`), and a start that names one runs under it — a start that names one the daemon does not know instead starts with the full harness and records the reason on the session record.

The four built-in profiles and the defaults table are described below. Each launch's outcome is visible on the task record, on the Automaton's decision list, and in the profile-in-force read (see [Setting and reading it](#setting-and-reading-it)).

What a worker session gets from any profile:

- everything the task needs to be judged — its spec, its constraints, its verification script path, its changed-file scope;
- the project's rules, in the level its profile calls for (full file pointer, rules digest, or neither);
- the tools its profile allows.

What stays simple:

- **A plain session start you run yourself is still exactly as before** unless you name a profile or the daemon gives it one of the launch facts — and either way it runs exactly as before when you name none and it carries none: the full harness, as it has always been. A start may carry a profile name (the start's `worker_profile_name` field, the PWA New Session form's **Worker profile** select, the CLI's `--worker-profile-name`), and if it names one the daemon resolves it against the built-ins, then your user profiles, and applies it to that one start. The other way the six kinds reach a start is its launch facts: a start with `launch_role=supervisor`, `launch_role=planner`, `launch_kind=review`, or in one-shot mode resolves the table's row for its kind (a named profile first, then the daemon-wide name, then the row's default). A profile never shrinks what a session needs to finish its work — it only chooses the starting context and the tool and MCP sets — and a name the daemon does not know refuses cleanly: the start runs with the full harness and the refusal lands on the session record (see [How a profile is resolved](#how-a-profile-is-resolved)).
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

**Operator to review.** The field values of the four built-in profiles, and the per-role, per-task-kind and per-session-kind defaults in the table below, are a supervisor judgment call made to be "the smallest that works" — check them on first sight and correct them (see the review list at the bottom of this howto).

## The defaults: the smallest that works, with full one setting away

Every **role** — planner, worker, verifier, judge — and every **kind** — the task kinds implementation, docs, note, final gate, and the session kinds supervisor, operator_interactive, scheduled_command, planner, one_shot, review — has a default profile. This is the built-in table the resolution rule falls back to when nothing else is set:

| Default profile | Who has it |
|-----------------|--------------------|
| `full` | the **planner** — its own start is the unchanged one, and it is the one that names the profile on a task — and the **planner**-kind session (a planning session the daemon starts) |
| `digest` | the **worker** and the **verifier**, each matched to doing or checking a coding job; and **implementation**, **docs** and **final-gate** tasks; and the **operator_interactive** kind (an interactive session kind — see the paragraph after the table for how a start resolves) |
| `datawatch-tools` | the **supervisor** kind — it drives datawatch through its tools and does no other work |
| `minimal` | the **judge**, reading a verdict, and **note**-kind tasks; and the **scheduled_command**, **one_shot** and **review** sessions |

**What the lean supervisor profile exposes.** The `datawatch-tools` profile keeps the first-turn context small, but it does not hide the datawatch tool server: that server's tools are listed to the session and stay discoverable through tool search, so a supervisor can find and call them on demand instead of carrying every full definition in its first turn. You can expect status tools such as `supervisor_watch_stats` and `question_list` to be callable straight from a lean supervisor session, without falling back to the command line.

**How the tools stay reachable.** A profile launch that uses the datawatch server pins the session to its own MCP config (see [The shape of the start](#the-shape-of-the-start)), and the launch sets `ENABLE_TOOL_SEARCH=true` on the claude process so the server's tool definitions are always deferred behind tool search rather than inlined or silently dropped from a trimmed tool list. The variable is an environment setting, not a flag: the launch types it into the session pane as a shell assignment ahead of the binary, so it is present in the process's environment and never in the process's argument list (see [Checking a lean session](#checking-a-lean-session) for how to read it). Every launch path of a profiled session carries it: a fresh start of the session, a resume of the session, and a restart of the session — a resume re-applies the session's stored profile, so the tool set and the variable do not change under the session across restarts — and a start that names an LLM row that resolves to the claude-code backend. A named row that resolves to any other backend gets no profiled launch at all: profiles only exist on the claude-code launch path, so there is no halfway state where the session starts with some of the profile's flags and none of its guarantees. A start with no profile, or a profile whose MCP mode is `none`, gets no such setting and runs as before.

**Operator review:** if a lean supervisor still reports the server connected but finds no datawatch tools, check the session's tool search before suspecting the profile (see [Checking a lean session](#checking-a-lean-session)), and confirm your Claude Code version supports tool search.

That is the "smallest that works" line in the code. `full` is reachable from every other default by one setting — on the task, on the Automaton, on the project or daemon-wide — so a job that needs the old, heavy start has it, and nothing else pays for it.

A start you run resolves one of these rows by its own launch facts: a start with `launch_role=supervisor` resolves the supervisor row, `launch_role=planner` the planner row, a start named `launch_kind=review` (a review start) the review row, and a one-shot start the one_shot row — a named profile always wins, then the daemon-wide name, then the row's default. The **operator_interactive** and **scheduled_command** rows sit in the same table and under the same `kind` key vocabulary, and you can set and read them like any other session kind. A plain start without any of the launch facts resolves to **no row** — for it a profile can only come from a name you send with that start. The vocabularies are fixed: `launch_role` is one of `worker|verifier|planner|judge|supervisor`, and `launch_kind` is one of `task|decompose|review|rejudge|auto-fix` — a value outside either is refused with a 400 before any session is started.

A **scheduled** supervisor pass resolves its profile the same way, at the moment the schedule fires: a schedule that carries `launch_role=supervisor` resolves the supervisor row exactly as an interactive start of that role does, so a recurring pass starts on the small tool set rather than the full harness. The session record then carries the resolved profile, and — when the profile could not be applied and the pass started on the full harness instead — the reason in `profile_fallback_reason`, which the session reads return.

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
5. the **kind default** (a task kind or a session kind) — the `kind` key of `autonomous.worker_profile_defaults` for that kind, or the built-in kind default when you have not set one;
6. the **role default** — the `role` key of `autonomous.worker_profile_defaults` for that role, or the built-in role default when you have not set one.

A tier that is set but empty is just unset, so resolution continues to the next tier. That is also how a profile you define for yourself is used: define it once (per project or daemon-wide), and every role, task kind, session kind or task that names it gets it.

**Sessions you start resolve the same way at their own layer.** A start may name a profile for that one start — the PWA New Session form's **Worker profile** select does that (the built-ins, your profiles, or **Auto**, which leaves the choice to the daemon) — and a start with launch facts resolves the table's row for its kind: a start with `launch_role=supervisor` takes the supervisor row, `launch_role=planner` the planner row, `launch_kind=review` the review row, and a one-shot start the one_shot row, the daemon-wide name coming ahead of the row's default where you set one. A plain start without those launch facts resolves against **no row and no daemon-wide name** — with nothing named it starts the full harness, exactly as before. A start names no task and no Automaton, so it never sees those two tiers.

With nothing configured at any tier, no profile is applied and the session starts with the full harness.

**A profile name the daemon does not know is refused** at the surface you set it — the refusal names the valid names — so a typo can never silently widen or narrow a session. The one place the rule handles an unknown name instead of refusing it: if a name is set on a task or an Automaton and is not present when the rule runs (for example, removed after being named), the rule stops the search at that tier, applies the role's default, and records the decision — the requested name, the tier it came from, the profile applied and the reason. That decision shows on the task's record and in the Automaton's decision list.

**A profile that cannot be applied falls back to the full harness.** When a resolved profile cannot be applied at launch, the session starts with the full harness instead, and the attempt records the fallback with its reason. Nothing is silently dropped.

## The `worker_profile_name` option, on every surface

The one name a start resolves is called `worker_profile_name` on every surface that carries it:

| Surface | How you name it | What a start does with it |
|---------|-----------------|---------------------------|
| **REST API** | `POST /api/sessions/start`, body field `worker_profile_name` (empty or omitted = resolved by the start's launch facts: the daemon-wide name and then the kind's default for a supervisor, planner, review or one-shot start; no profile at all for a plain start) | resolves it against the built-ins, then `autonomous.worker_profiles`; a name the daemon does not know starts the full harness and records `unknown worker profile "<name>"; full harness applied` on the session |
| **CLI** | `datawatch session new --worker-profile-name <name>` (any session kind; the flag help says an unknown name starts the full harness and records the reason) | sent to the daemon as `worker_profile_name`; same resolution |
| **MCP** | the `worker_profile_name` argument of the start-session tool (its description: applied at launch for any session kind; an unknown name starts the full harness and records the reason; a `launch_role=supervisor` start with no name takes the lean supervisor kind default) | the daemon resolves it against the built-ins and your user profiles, and a blank name is resolved by the start's launch facts the same way as on a REST start |
| **PWA New Session form** | the **Worker profile** select — **Auto**, the four built-ins, or a profile you defined ("Auto" sends a blank name and leaves the choice to the daemon) | same resolution as the REST start behind it |
| **Comm channel** | the three `configure autonomous.worker_profile*` keys set the daemon's profile, defaults and user profiles; the channel has no per-start name field, so a start begun from it resolves the daemon-wide name and then the kind default | same resolution |
| **YAML (settings file)** | the `autonomous.` block — `worker_profile` (the daemon-wide name), `worker_profile_defaults` (`role` and `kind`), `worker_profiles` (yours); a start reads whatever the file set | same resolution |

The field on a **task** is also called `worker_profile` (set as a field or by the `worker_profile: <name>` spec line described in [Giving one task the tools it needs](#giving-one-task-the-tools-it-needs)), but it is the task-layer name, not this per-start field. One note on the LLM field: the **`llm` LLM row** a start names is a launch fact — which LLM row the session runs, and what [the `binary` field of a claude-code row does](#the-binary-field-on-a-claude-code-llm-row). It is never a profile, it does not appear in the profile in force, and a refused `binary` value is a refusal at the row, not a profile event.

## The `binary` field on a claude-code LLM row

On a claude-code LLM row, the **`binary` field is honored or refused — never ignored**. There is exactly one canonical claude-code row, the row named **`claude-code`**, and it is the only claude-code row whose `binary` field takes effect: the daemon reads that row's `binary` at daemon start and uses it for **every** claude-code session it launches (a non-empty `binary` on the row, else the name `claude` on the daemon's `PATH`), and it takes effect on a **daemon restart**. A `binary` saved on **any other** claude-code row is **refused** the moment it is saved, with the refusal saying so:

> `llm "<name>": binary "<binary>" cannot be saved on a claude-code row other than the canonical "claude-code" row: the claude-code launcher is built at daemon start with the "claude-code" row's binary (or the "claude" default) and runs exactly that binary for every claude-code session, so row "<name>" would silently ignore it. Save this row without a binary, or point the canonical "claude-code" row at the wrapper binary (takes effect on daemon restart)`

So a wrapper binary (a wrapper for a custom binary, for example) belongs on the canonical `claude-code` row if you want every claude-code session — including a profiled one — to start with it; every other claude-code row must be saved without a `binary`, and any start that names one of those rows runs the canonical binary. That is the binary a profiled start honors, and it is the one a start that refuses it named.

## When a start fails on capacity

A start that names a backend and a model is admitted against the daemon's capacity for that model **before any session exists** — so a manual session contends for its real slot instead of being invisible to the ledger — and it waits for a free slot: the interactive default is 8 seconds, or `session.capacity_wait_seconds` when you set one. If no slot opens by the end of the wait — a pool at its limit, a node's engine pool full, a slot held for a queued worker — the start fails with a **503**, and its body is the pool's own block reason:

```
capacity: <the cause> [llm=<the llm> model=<the model> node=<the node> wait_timeout=<the seconds>]
```

The cause is the ledger's own wording — a timed-out wait is prefixed with "interactive start waited 8s (session.capacity_wait_seconds) for a free slot and gave up:", followed by the pool's reason: a pool at its limit ("pool `node-verify:…` full (…)", naming the pool, the slot count and the holders), a full node ("waiting for node …: 2/2 engines in use, held by …"), or a rate limit or backpressure reason — and the bracketed fields are the ones the start named, its `llm` row, model and the node it resolved to, or `(none)` where it named none, with `wait_timeout` the wait that gave up. A one-shot start (a start in one-shot mode) skips this interactive admission entirely — one-shots are admitted through their own admission path and do not wait on it — so a start refused in the terminal, the PWA or the comm channel is an interactive one, and the message above is the whole shape of that failure: which pool, which holders, which node, and how long the start waited.

## The shape of the start

**The MCP selection, pinned.** The launcher writes the session's own MCP config into its working directory — `worker-mcp.json`, mode `0600`, written atomically (a temporary file renamed into place), with deterministic bytes, so a launch under the same name and the same profile is reproducible. Mode `none` lists no servers; mode `datawatch` lists exactly the datawatch server; mode `list` lists exactly the named servers. A `list` entry that is missing from the daemon's own configuration fails the launch before anything is written, and the refusal names every missing one — a session on `list` never starts with a partial server set. The launch carries that file as its **only** MCP configuration, so the rest of the daemon's server set does not leak in, and the project's or the instance's own `.mcp.json` is never touched.

**The tools and the skills switch, on the launch.** The tool field maps to the launcher's allow-list — an allow-list only, so a profile can narrow a session's tools and never widen them; `all` emits nothing, and profiles carry no deny-list. The skills field is an on/off switch on the launch: `false` turns skills off for that one launch, `true` leaves them as the daemon sets them.

**A deterministic starting context.** For a given role and profile, the starting block the session reads first is built **deterministically**: the same bytes, every time, in a fixed section order — the **role** line, the **profile** line (name, MCP mode and servers, tools, skills, context level), the **constraints** from the starting pieces, then the **rules digest** (for `digest` and above), then the **full rules** (for `full`), and the **task** — always last. Empty rules text is skipped, so a level never produces a blank section. The block is written into the working directory as `worker-starting-context.md` (mode `0600`) and appended to the session's system prompt for that one launch. **Where these two files stand in the checks.** Both — `worker-mcp.json` and `worker-starting-context.md` — are launcher artifacts, not the task's work: the uncommitted-work check and a read-only task's dirty check never count an untracked root copy (a launcher-named file you track, or a same-named file in a subfolder, is still a change — the match is the exact name at the repository root); the attempt snapshot and the automatic-commit staging skip both root names by name, whether tracked or not (skip reason `launcher`); and before a task branch merges the daemon removes a committed copy only when the merge target has none. See [Uncommitted work check](automaton-uncommitted-work.md#what-happens-to-each-task) and [Running the tasks of one Automaton in parallel](automaton-parallel-tasks.md#before-a-task-branch-merges-daemon-side-cleanup). Two effects:

- **Prompt-cache reuse.** A retry, a ladder hop to the next worker, a verifier and a judge on the same role-profile start share the context prefix byte-for-byte, so the model provider's prompt cache applies across them where the profile is the same. A session that reuses the role and profile does not re-send the same 85,000 tokens it re-sent before; it re-sends the delta its profile actually adds.
- **The cost report can compare profiles.** Because the start is deterministic per role and profile, the tokens a profile costs are a stable figure, not a per-session accident.

**The "not applied" record.** Not every backend can honour every field — a skills switch, a tool allow-list, an MCP restriction it does not implement. When a session starts with a profile, its start record carries **one outcome per profile field — mcp, tools, skills, context, model, effort, in that stable order** — from the backend that launched it. A field the backend honours is recorded as applied; a field it cannot is recorded as **not applied** with the reason "backend cannot honour this field", and the session still gets everything the task needs to complete the work: what a worker gets is never smaller than the task's spec and its verification requirement, the profile can only ever shrink the start, not the work. Claude Code honours every field in its own launch path; a backend that honours none records every field as not applied. The record is one line to summarise — one `field=applied` or `field=unapplied: <reason>` entry per field, in the same order.

**The measured starting context, where it shows.** An attempt's record carries the **name of the profile** its worker started under **and the size of its starting context** (an attempt under no named profile carries both empty, and records no size — never a zero). That size is the estimate made from the starting context generated at launch (one token per four bytes, source `estimated`), and it becomes the reported input tokens when a runner reports them (source `reported`). Read them where you already read cost:

- **The cost report.** A group's start — a worker, a verifier, the whole Automaton — shows the starting context with the profile it ran under, so you can read "worker on `digest` started at ~X tokens; judge on `minimal` at ~Y" instead of one flat figure (about 85,000 when measured on 2026-11-08) you had to know was mostly MCP definitions. The profile name is on the attempt record, so any grouping (Automaton, Automation, backend, week) can carry it.
- **The stats and the usage advice line.** A session's stats and the usage advice line that reads your spend against the week carry the starting context and profile the same way, so the advice you get says which profile is costing what.
- **The attempt record.** A task's record (the Automaton detail, the REST read, the MCP read) names the profile it ran and the tokens it started with, so a task that ran on `full` is marked as such, not hidden in the average.

Compared against the 85,000-token median baseline from 2026-11-08, a session on `minimal` or `digest` that also turns its MCP servers off should start a large fraction lower — the 36,000 MCP definition tokens and the difference between the full rules files and a digest are the two blocks this change removes. Re-measure on your own fleet before you price anything; the baseline was one machine, one day.

## Checking a lean session

A lean session is recognizable from its own artifacts and from what it can find. Check these, in order of directness:

- **Search the datawatch tools by name inside the session.** The lean tools are deferred, so they are not in the session's standing tool list; the model reaches them by name through tool search. Ask the session to use a datawatch tool by name — `memory_recall`, `question_list`, `supervisor_watch_stats`, `usage_backoff_set` are the ones a lean supervisor reaches for — or to look one up: a lean session finds the tool on demand, while a session that fell back to the full harness carries it as an ordinary standing tool. This is the direct check of "deferred, reachable, discoverable by name".
- **Look at `worker-mcp.json` in the session's working directory.** A lean session has exactly one entry, `datawatch`, and no others — the file the launch writes and pins (see [The shape of the start](#the-shape-of-the-start)). If the file is missing, the profile did not apply to that launch: read the session's fallback reason ([What a fallback reason means](#what-a-fallback-reason-means)) before doing anything else.
- **Check the session's claude process environment.** Find the session's claude process by its `--add-dir <project dir>` argument and read `ENABLE_TOOL_SEARCH=true` from its environment (e.g. `/proc/<pid>/environ`). The launch types the assignment into the pane rather than putting it in the argument list, so an empty search of the process command line is the wrong probe, not evidence the variable is absent.

## What a fallback reason means

A fallback reason is the answer to "why is this session not lean?" It is set when a profile named for a launch could not be applied, and it means exactly this: **the session started anyway, with the full harness** — the full MCP set, the full standing tool list, no `worker-mcp.json` pin, no `ENABLE_TOOL_SEARCH=true` — and the reason on the session record explains why the profile was not applied. Two shapes occur:

- **An unknown profile name.** The launch named a profile that neither the built-ins nor the user profiles you defined contain; the reason names the unknown profile and says the full harness was applied instead.
- **A selection that cannot resolve for this session.** The profile resolved, but something it depends on is missing or broken here — for example it selects an MCP server that is not configured on the session; the reason names the profile and the failure's cause.

The fallback sticks: a resume or a restart of a session that fell back keeps it profile-less — a session that already fell back at its first launch does not retry the profile on its next launch — so the tool set the session actually has does not change under it. Reading the fallback reason is how you tell "the session is not lean because the profile failed to apply" from "the session is lean and its tools are simply deferred behind search": the first has a reason on the record and a full standing tool list; the second has neither — it has `worker-mcp.json` with one `datawatch` entry and the tools reachable by name.

## Setting and reading it

The profiles sit in the daemon's autonomous config, right beside every other autonomous setting — the same config a datawatch user already sets — and every surface of it sets and reads the keys:

| Surface | Set | Read back |
|---------|-----|-----------|
| **Settings file** | the `autonomous.` block in `~/.datawatch/config.yaml`: `worker_profiles`, `worker_profile_defaults` (`role` and `kind`), `worker_profile` | the three keys as stored |
| **Web** | the **Worker session profiles** group in Settings: the daemon-wide name (empty clears it), a select per role, task kind and session kind, and your user profiles as one JSON object (empty clears them) — each field saved when it changes; the New Session form's **Worker profile** select names the profile one specific start should take | the group shows the values as set; the selects offer the four built-ins plus the user profiles you define |
| **REST API** | `PUT /api/config` with the `autonomous.worker_profiles`, `autonomous.worker_profile_defaults` and `autonomous.worker_profile` keys; `POST /api/autonomous/prds/{id}/set_worker_profile` sets the one profile an Automaton's sessions start with (empty clears the override); `POST /api/sessions/start` accepts `worker_profile_name` for the one start | `GET /api/config` carries the three keys as set, and its `autonomous.in_force` block carries the profile in force for each role, task kind and session kind |
| **MCP** | `config_set` with the same three keys; `autonomous_prd_set_worker_profile` sets the one Automaton's profile name | `get_config section=autonomous` reads the keys as set; `worker_profile_status` reports the built-in names, the profile in force for each role, task kind and session kind (its `in_force` block), and the latest start-up averages |
| **CLI** | `datawatch config set` on `worker_profile`, `worker_profiles`, `worker_profile_defaults.role` and `worker_profile_defaults.kind` (the `kind` object takes task-kind and session-kind keys) — checked against a local copy of the config before anything is sent, so a refused value never reaches the daemon; any other `worker_profile*` key is unknown and refused | `datawatch config get` on those keys, read from the `autonomous` block, followed by the profile in force for each role, task kind and session kind |
| **Comm channel** | `configure autonomous.worker_profiles=...`, `configure autonomous.worker_profile_defaults=...` and `configure autonomous.worker_profile=...` | `configure help` and `configure list` name the three keys; a bare key answers with its one-line help, pointing here, and the profile in force for each role, task kind and session kind |
| **Web settings page** | the **Profile in force** section of the Worker session profiles group | one line per role, task kind and session kind: the profile it launches with, and the tier that set it |

- `autonomous.worker_profiles` — the profiles you define, keyed by name; each value is a profile's fields (`mcp`, `tools`, `skills`, `context`, `model`, `effort`). A user profile may not reuse a built-in name.
- `autonomous.worker_profile_defaults` — the `role` and `kind` defaults, which replace the built-in table above for the roles and kinds you set; the valid role keys are `planner`, `worker`, `verifier`, `judge`, the valid kind keys are the task kinds `implementation`, `docs`, `note`, `final_gate` and the session kinds `supervisor`, `operator_interactive`, `scheduled_command`, `planner`, `one_shot`, `review`, and every value must name a built-in or one of your profiles.
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

- **The defaults table against your fleet.** The built-in table above — the planner (role and kind) on `full`, the worker and the verifier and implementation, docs and final-gate tasks and the operator_interactive sessions on `digest`, the supervisor sessions on `datawatch-tools`, and the judge, note-kind tasks, and the scheduled_command, one_shot and review sessions on `minimal` — is what a launch falls back to when nothing is configured, set to the smallest that works. If a role is under-equipped on your fleet (a worker that needs a tool the `digest` default does not give it), the spec-line marker on one task fixes that one task, and `autonomous.worker_profile_defaults` fixes the rest.
- **Verifier and judge start records.** Confirm on your build that a verifier or judge session's start record names the profile the profile-in-force read shows for its role. The resolver is `ResolveSessionProfile` in `internal/autonomous/worker_profile_launch.go`, called by `withSessionProfile` in `cmd/datawatch/session_judgment.go` on the verifier and judge launch paths. The field-level "applied" outcomes for these sessions were not re-checked in this docs pass.
- **The field values of the four built-ins and your own profiles** as they apply on your backends: the allow-lists in `tools`, the named server list in `mcp`, and the context level of each.
- **The per-surface names and read-backs.** The surfaces above set and read the keys under the names in the table — check they read the way you'd say them, and that a refused value names the valid ones, and that the profile-in-force read names the right tier on each surface.
- **The measured starting context in your own cost report.** The 85,000-token baseline and the 36,000 MCP block are one machine's 2026-11-08 figures. When you see the profile name and the measured start against them across a representative window, you can price the difference and, where a profile costs more than its job needs, move it down one level.
- **The operations entry.** The **Worker session profiles** section of [operations.md](../operations.md) now describes the wired state — the launches and the session-kind starts applying the profile, the fallback record, the profile-in-force reads on every surface — and the same 85,000-token baseline. Keep it and this how-to telling the same story when either side changes.

## Related

- [Rules and context](automata-rules-and-context.md) — the levels a profile's context field names: the rules file, the context files, the digest, the planning record.
- [Platform context](sessions-deep-dive.md#what-a-session-is-told-about-datawatch) — the daemon's own "what is datawatch" block that every session, including workers, still receives.
- [Cost report](cost-report.md) — the per-profile and per-role start-up tables that read the attempt records, and where you read the difference between profiles.
- [Failure escalation](failure-escalation.md) — the ladder and the retries this change makes cheaper: each hop to the next worker re-sends only the delta its profile adds, over a shared, cache-friendly prefix.
