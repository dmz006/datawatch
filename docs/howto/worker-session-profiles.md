---
docs:
  index: true
  topics: [worker, session, launch, profile, context, tokens, mcp, cost, verifier, judge, planner, autonomous]
---
# How-to: worker session profiles

## What changed

**Worker and verifier sessions now start small.** Instead of every task session beginning with the operator's full harness — the complete rules and context files, the platform context, and the full MCP tool definitions — a worker (or verifier or judge) session now starts with a starting context and a tool set **chosen for the job it is about to do**, from a **worker session profile**.

What a worker session still gets, unchanged:

- everything the task needs to be judged — its spec, its constraints, its verification script path, its changed-file scope;
- the project's rules, in the level its profile calls for (full file pointer, rules digest, or neither);
- the tools its profile allows.

What did not change:

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

A profile with `mcp: none` and a small context level starts a session with none of the 36,000 MCP tokens and none it did not need — while still having everything the task's spec names.

## The built-in profiles

Four built-in profiles ship, **named after what they do**:

- **minimal** — the smallest that works: the task's spec and constraints, a fixed tool set for coding work (the file and shell tools: read, edit, write, bash, glob, grep), no MCP servers, no skills. The context level is `minimal`.
- **digest** — `minimal` with the datawatch MCP server (the session keeps the ability to read its own session, the PRD state and its own records) and the full tool set, and the project's rules as a [rules digest](automata-rules-and-context.md#the-rules-digest) (every heading plus the mandatory lines, cached by content hash) instead of the full rules files. No skills. The context level is `digest`.
- **datawatch-tools** — the task's context as above, plus the datawatch MCP server but no other MCP servers and no skills. The context level is `digest`. This is the profile the planner names on a task's spec when that task needs a tool or an MCP server.
- **full** — what every session starts with today: the full rules and context, the datawatch MCP server, the full tool set, skills on. This is "the old way", and it is **one setting away** from any task or role.

**Operator to review.** The field values of the four built-in profiles, and the per-role and per-task-kind defaults in the table below, are a supervisor judgment call made to be "the smallest that works" — check them on first sight and correct them (see the review list at the bottom of this howto).

## The defaults: the smallest that works, with full one setting away

Every **role** — planner, worker, verifier, judge — and every **task kind** — implementation, docs, note, final gate — starts from a default profile:

| Default profile | Who starts with it |
|-----------------|--------------------|
| `full` | the **planner** — its own start is the unchanged one, and it is the one that names the profile on a task |
| `digest` | the **worker** and the **verifier**, each matched to doing or checking a coding job; and **implementation**, **docs** and **final-gate** tasks |
| `minimal` | the **judge**, reading a verdict, and **note**-kind tasks |

That is the "smallest that works" line in the code: a verifier and a judge read a diff against the rules, not do the work; a docs or a note task does not need what an implementation task needs; and the planner's start is untouched, so it still names the profile on a task with `full` behind it. `full` is reachable from every other default by one setting — on the task, on the Automaton, on the project or daemon-wide — so a job that needs the old, heavy start has it, and nothing else pays for it.

## Giving one task the tools it needs

You do not need to change a role default for one job. The planner, when it writes a task, can attach a **profile marker to the task spec** — one line — naming the profile that task needs. That task then starts with that profile and the rest of the Automaton keeps its defaults. This is the same override a planner already makes when it attaches an existing verification script to a task: the marker is part of the spec, not a daemon change.

Reading it back: the marker is the exact line `worker_profile: <name>` in the task's spec, the first such line is the one that counts, and a `worker_profile` the task or the Automaton already carries is never overridden by a spec line.

## How a profile is resolved

A profile resolves **by name, in this order** — the first name that is set wins:

1. the **task** — its own `worker_profile`, set as a field or by the planner's `worker_profile: <name>` spec line;
2. the **Automaton** — its `worker_profile`;
3. the **project** — the profile your project names;
4. the **daemon** — the `autonomous.worker_profile` setting, if you set one;
5. the **task-kind default** — `autonomous.worker_profile_defaults.kind` for that kind, or the built-in kind default when you have not set one;
6. the **role default** — `autonomous.worker_profile_defaults.role` for that role, or the built-in role default when you have not set one.

A tier that is set but empty is just unset, so resolution continues to the next tier. That is also how a profile you define for yourself is used: define it once (per project or daemon-wide), and every role, task kind or task that names it gets it.

**A profile name the daemon does not know is refused** at the surface you set it — the refusal names the valid names — so a typo can never silently widen or narrow a session. The one place a name is not refused is at session start: if a profile named on a task or an Automaton is not present when the session launches (for example, removed after being named), the session starts on its role's default profile instead, the daemon **records that decision** on the task's record, and the task goes on.

## Backends that cannot honour a field

Not every backend can honour every field. A backend that **cannot honour a field of the resolved profile — a skills switch, a tool allow-list, an MCP restriction it does not implement — is recorded as not applied** for that session: the decision record names the field and the reason, and the session still gets everything it needs to complete the work. What a worker gets is never smaller than the task's spec and its verification requirement; what it starts with is only ever larger than the profile when a backend cannot prove it is not.

## Deterministic starting context

For a given role and profile, a session's starting context is **deterministic**: the same bytes, every time, in the same order — the task's spec and constraints, the rules at the profile's level, and nothing else that changes from start to start. The task-specific section sits at the end of the context. Two effects that follow:

- **Prompt-cache reuse.** A retry, a ladder hop to the next worker, a verifier and a judge on the same role-profile start share the context prefix byte-for-byte, so the model provider's prompt cache applies across them where the profile is the same. A session that reuses the role and profile does not re-send the same 85,000 tokens it re-sent before; it re-sends the delta its profile actually adds.
- **The cost report can compare profiles.** Because the start is deterministic per role and profile, the tokens a profile costs are a stable figure, not a per-session accident.

## The measured starting context, where you will see it

Every attempt's record carries its **starting context size** (the measured input tokens the session began with) **and the name of the profile** that produced it. Read them where you already read cost:

- **The cost report.** A group's start — a worker, a verifier, the whole Automaton — now shows the starting context with the profile it ran under, so you can read "worker on `digest` started at ~X tokens; judge on `minimal` at ~Y" instead of one flat 85,000 you had to know was mostly MCP definitions. The profile name is on the attempt record, so any grouping (Automaton, Automation, backend, week) can carry it.
- **The stats and the usage advice line.** A session's stats and the usage advice line that reads your spend against the week carry the starting context and profile the same way, so the advice you get says which profile is costing what.
- **The attempt record.** A task's record (the Automaton detail, the REST read, the MCP read) names the profile it ran and the tokens it started with, so a task that ran on `full` is marked as such, not hidden in the average.

Compared against the 85,000-token median baseline from 2026-11-08, a session on `minimal` or `digest` that also turns its MCP servers off should start a large fraction lower — the 36,000 MCP definition tokens and the difference between the full rules files and a digest are the two blocks this change removes. Re-measure on your own fleet before you price anything; the baseline was one machine, one day.

## Setting and reading it

The profiles sit in the daemon's autonomous config, right beside every other autonomous setting — the same config a datawatch user already sets, on the settings file or through the web, REST, the MCP tools, the CLI or the comm channel:

- `autonomous.worker_profiles` — the profiles you define, keyed by name; each value is a profile's fields (`mcp`, `tools`, `skills`, `context`, `model`, `effort`). A user profile may not reuse a built-in name.
- `autonomous.worker_profile_defaults` — the `role` and `kind` defaults, when you want to move them from the built-in table above; the valid role keys are `planner`, `worker`, `verifier`, `judge`, the valid kind keys are `implementation`, `docs`, `note`, `final_gate`, and every value must name a built-in or one of your profiles.
- `autonomous.worker_profile` — the daemon-wide (global-layer) profile name, if you want one for everything that names none.
- on a **task** or an **Automaton** — the `worker_profile` field, the per-task and per-Automaton overrides that beat the daemon-wide name.

Any of these values that does not name a valid profile is refused where you set it, and the refusal names the valid ones — a built-in name a user profile reuses, a profile that fails its own checks, a defaults or global name that matches nothing, an unknown role or kind key.

What still lands with the sessions' own start work: the per-surface read-back that names the profile in force per role and per task kind with the tier (task / Automaton / project / daemon) that set it, the measured starting context per attempt, and the exact tool names that set and read each of these on the surfaces above. Until then, the settings file is the surface to use, and the valid profile names are the four built-ins or a profile you have defined there. The reference is in [config-reference.yaml](../config-reference.yaml) (the `autonomous.` block) and [implementation.md](../implementation.md) (the profile-settings section).

## What to review when you first see it

The operator direction allows corrections, and these are the places the correction is due:

- **The defaults table against your fleet.** The built-in table above — the planner on `full`, the worker, the verifier and implementation, docs and final-gate tasks on `digest`, the judge and note-kind tasks on `minimal` — is set to the smallest that works. If a role is under-equipped on your fleet (a worker that needs a tool the `digest` default does not give it), the spec-line marker on one task fixes that one task, and `autonomous.worker_profile_defaults` fixes the rest.
- **The field values of the four built-ins and your own profiles** as they apply on your backends: the allow-lists in `tools`, the named server list in `mcp`, and the context level of each.
- **The per-surface read-back on your six surfaces.** When each surface carries the profile read-back, check it names the profile in force per role and per task kind with the tier that set it, and that a refused name names the valid ones.
- **The measured starting context in your own cost report.** The 85,000-token baseline and the 36,000 MCP block are one machine's 2026-11-08 figures. When you see the profile name and the measured start against them across a representative window, you can price the difference and, where a profile costs more than its job needs, move it down one level.

## Related

- [Rules and context](automata-rules-and-context.md) — the levels a profile's context field names: the rules file, the context files, the digest, the planning record.
- [Platform context](sessions-deep-dive.md#what-a-session-is-told-about-datawatch) — the daemon's own "what is datawatch" block that every session, including workers, still receives.
- [Cost report](cost-report.md) — where the measured starting context and profile show up, and where you read the difference.
- [Failure escalation](failure-escalation.md) — the ladder and the retries this change makes cheaper: each hop to the next worker now re-sends only the delta its profile adds, over a shared, cache-friendly prefix.
