# [parity] worker session profiles: the small worker start, settable and readable from the app (datawatch-app follow-up)

**Filed 2026-11-09 as dmz006/datawatch-app#414** (shipped in v9.115.0).

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened on the
`dmz006/datawatch-app` repository when the operator files it. Filing the issue does
not change a line of this repo. Draft written 2026-11-08 against the worker session
profiles work (PRD `24230001`) that landed its docs set and changelog entry on main.
The config keys have already landed and are documented (the
[how-to](../../howto/worker-session-profiles.md) names them: `autonomous.worker_profiles`,
`autonomous.worker_profile_defaults` with its `role` and `kind` keys, the daemon-wide
`autonomous.worker_profile`, and the `worker_profile` field on a task and on an
Automaton). What still lands is the per-surface tool names — the MCP, CLI and comm
surface names that set and read those keys — and this draft must be updated with
them before filing.

Parent release: the datawatch release that ships worker session profiles — the
feature currently under `[Unreleased]` in `CHANGELOG.md` (daemon version at
planning is v9.112.6). The feature needs no app-side action to work; this is the
follow-up issue for the parity surface it leaves open on Android and iPhone.

## Affected area

How a worker, verifier or judge session **starts** — the context it opens with and
the tools it begins with — and where the operator reads and sets that choice. From
the operator's point of view the affected areas are: the new profile settings
surface (the profile blocks plus the per-role and per-task-kind defaults), the
Automaton and task detail views (each attempt now names the profile it ran under
and the starting context size it began with), and the cost report and the usage
advice line (a group's start now shows the starting context with the profile that
produced it, instead of one flat median). Sessions the operator starts themselves
are **unchanged** — a session begun from the PWA, the CLI, a comm channel or a chat
client starts with the full harness, exactly as before — so the app's own
session-start flows do not change. What a task finishes is also unchanged: the same
result, the same verification, the same reports.

## What this work closes

Before this work, every task session started with the full operator harness — the
complete rules and context files, the platform context, and the full MCP tool
definitions. Measured by the operator on 2026-11-08 (recorded in the autonomy
handover plan): a task session starts with a median of about **85,000 tokens** of
context for a median of 13 turns and about 8,000 output tokens, and the MCP tool
definitions alone are about **36,000 tokens, roughly 56%, of every session start**.
A worker, a verifier and a judge each paid that on every run, and every retry and
ladder hop paid it again.

Now, on the datawatch daemon:

1. A **worker session profile** is a named, user-configurable set of the things a
   session starts with: which MCP servers it can use (none, the datawatch server
   only, or a named list), which tools it gets (all, or an allow-list), whether
   skills are on, the level of starting context (minimal, digest or full), and the
   model and effort it runs on.
2. Four built-in profiles ship — `minimal`, `digest`, `full`, `datawatch-tools` —
   and every role (planner, worker, verifier, judge) and task kind (implementation,
   docs, note, final gate) defaults to the smallest profile that can do the job,
   with `full` — the old, heavy start — one setting away on the task, the Automaton,
   the project or daemon-wide.
3. Profiles resolve the same tiered way every other autonomous setting resolves:
   task → Automaton → project → daemon default. A profile name the daemon does not
   know is refused at the surface it is set, naming the valid ones; at session start
   an absent named profile falls back to the role default with the decision
   recorded on the task's record. A backend that cannot honour a field records it
   as not applied; the session still gets everything its task needs.
4. For a given role and profile the starting context is **deterministic** — the
   same bytes, every time, in the same order, the task section at the end — so a
   retry, a ladder hop, a verifier and a judge on the same role-profile share the
   prefix the model provider's prompt cache reuses, and the cost report can compare
   profiles against a stable figure.
5. The choice is configurable by every datawatch user on the six standard settings
   surfaces — the settings file, the web settings, REST, the MCP tools, the CLI and
   the comm channel — and the read-back on any of them names the profile in force
   per role and task kind, its fields, and the tier that set them. The config keys
   are the ones the how-to names (`autonomous.worker_profiles`,
   `autonomous.worker_profile_defaults`, `autonomous.worker_profile`,
   `worker_profile` on the task and the Automaton); the per-surface tool names
   that set and read them arrive with the settings work for this change.

## The parity gap this leaves open

The mobile clients of `datawatch-app` — Android and iPhone — have no surface for
any of this yet, and none of the app's screens will show the profile name or the
starting context once the feature reads back on them:

- The app has no settings surface for the profile blocks or the per-role /
  per-task-kind defaults. A mobile operator who wants a worker on `digest` by
  default, or one task on `full`, or a verifier on `minimal`, must do it from a
  terminal, REST, the PWA on another device, or the comm channel — with no field on
  the phone to read back what is in force, from which tier, or which of the four
  built-ins (or user-defined profiles) are valid.
- The app's Automaton and task detail views do not show the profile a task ran
  under or the starting context it began with, so a task that ran on `full` — the
  expensive one — is indistinguishable in the app from one that ran on `minimal`,
  and the operator cannot see the difference this change is meant to make.
- The app's cost and usage surfaces do not carry the starting context with the
  profile that produced it, so the comparison the daemon now supports ("worker on
  `digest` started at ~X; verifier on `minimal` at ~Y") is only readable from the
  daemon's own reports.
- The web settings fields for this feature — labels and help text — will carry new
  locale keys in the five web bundles (en, de, es, fr, ja); the app's own string
  resources do not have them yet, per the Localization Rule.

This is a client-side discovery and parity gap, not a daemon defect: the daemon's
contract for app clients is unchanged for operator-started sessions, and every
worker-side behavior (the small start, the resolution tiers, the refusals, the
deterministic prefix) works identically regardless of which surface sets it.

## Expected behavior

A mobile operator should be able to do, on Android and iPhone, the same things the
web settings screen and the daemon's reports allow: read back the profile in force
per role and per task kind with its fields and the tier that set them; change a
profile assignment for a role, a task kind, a project or the daemon, including
picking one of the four built-ins or a user-defined profile, and see a refused name
named as refused with the valid ones; and read, on the Automaton and task detail
and in the cost and usage surfaces, the profile each attempt ran under and the
starting context it began with, so the difference between `minimal`, `digest` and
`full` is visible where the operator already reads spend. Capability parity is
required; implementation parity is not — each platform delivers it the idiomatic
way. A platform that cannot set a profile must say so explicitly instead of
silently hiding the surface.

## Suggested next step

Treat this as a follow-up in `datawatch-app`, filed **after** the settings work for
worker session profiles lands on the daemon with its final key and tool names —
file it before the per-surface tool names exist and the draft is wrong, so update
this draft's affected area and expected behavior with them first, then file. At minimum the app
should: (1) add a profiles settings surface that reads and writes the same profile
keys the daemon's REST config endpoint round-trips, showing the profile in force
per role and task kind with its tier, the four built-in names as the valid set
plus any user-defined ones, and the label and help keys from the web settings in
all five of the app's languages (EN, DE, ES, FR, JA), requesting translations
through the Compose Multiplatform pipeline; (2) show the profile name and the
measured starting context on each attempt in the Automaton and task detail, and in
the cost and usage surfaces, so a task that ran on `full` is marked as such rather
than hidden in the average; and (3) leave a help link to the daemon's how-to
(`docs/howto/worker-session-profiles.md`) from the settings surface. If the app
team decides profile assignment is an advanced setting that belongs on the daemon's
own surfaces only, record that decision in the issue as an intentional exclusion
with its reason — the attempt-record and cost surfacing is the part that should
not be excluded, because it is the operator's only way to see the difference the
change makes.

## Filing record

- Filed: — (pending; issue number / URL once opened; file after the per-surface
  tool names for the settings above land, and after this draft is updated with
  them)
