# [parity] worker session profiles: the configurable-for-every-user settings, settable and readable on the app (datawatch-app follow-up)

**Filed 2026-11-09 as dmz006/datawatch-app#417** (shipped in v9.115.0).

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened on the
`dmz006/datawatch-app` repository when the operator files it. Filing the issue does
not change a line of this repo. Draft written 2026-11-09 against the
"configurable for every datawatch user" half of the story "Make every worker
session start small, with context and tools chosen for its job, configurable for
every datawatch user." (PRD `24230001`). This slice — the six daemon-side settings
surfaces that set and read the worker profile keys — is **already landed and
tested**; what it still leaves open is the app. It is the companion to
`2026-11-08-worker-small-start-parity.md` (which covers the mechanisms a launch
on a profile applies — the pinned MCP set and the deterministic starting context)
and to `2026-11-08-worker-session-profiles-parity.md` (which covers the profile
concept, the built-in defaults, and the attempt/cost read-back). Together the
three answer one question from three sides: *what a small start is, what it is
made of, and where a user sets and reads it.* The `2026-11-08-worker-session-profiles-parity.md`
draft was written before the per-surface tool names were known and says so; this
draft is written now that every one of them is — it is the up-to-date settings
draft, and it names the tools and keys concretely.

Parent release: the datawatch release that ships worker session profiles — the
feature currently under `[Unreleased]` in `CHANGELOG.md`.

## Affected area

The settings a datawatch user already uses to make every other autonomous choice
— worker session profiles included. From the operator's point of view the
affected areas are: the new **Worker session profiles** group in the web settings,
the three config keys it drives, and the read-only status and
per-Automaton-override tools that surface them. Concretely one named profile —
which MCP servers a session may use (none, the datawatch server only, or a named
list), which tools it gets (all or an allow-list), whether skills are on, the
level of starting context (`minimal`, `digest` or `full`), and the model and
effort it runs on — is configured in exactly three places a user names:

- **`autonomous.worker_profiles`** — the user-defined profiles, a JSON object
  keyed by name (a user profile may not reuse a built-in name).
- **`autonomous.worker_profile_defaults`** — the built-in defaults, split into a
  `role` map (planner, worker, verifier, judge) and a `kind` map
  (implementation, docs, note, final_gate), each mapping to a profile name.
- **`autonomous.worker_profile`** — the daemon-wide profile, the value a launch
  gets when the task, the Automaton and the project have not named one.

A single profile name also rides on the Automaton and on a task. Every refusal a
good setting should get — unknown profile name, a user profile named like a
built-in, an unknown role or kind key — is reported the same way on every daemon
surface that can see the key.

## What this work closes

Before this work, the profile settings existed in the config file but the rest of
the daemon could not read or write them: `GET`/`PUT /api/config` had no keys to
round-trip, the web settings screen had no fields, the MCP `config_set`
vocabulary did not include the profile keys, the CLI's `config set` / `config get`
family did not know them, and the comm channel had no configure verb. Today, on
the daemon, all six channels round-trip the profile keys, and the refusal rules a
user expects are visible on whichever surface set the offending value:

- **Config file.** The three `autonomous.worker_profiles`,
  `autonomous.worker_profile_defaults` and `autonomous.worker_profile` keys are
  loaded and validated by `config.ValidateWorkerProfileConfig`. An empty
  `worker_profiles` reads as `{}`, an unset `worker_profile` reads as `""`, and a
  user profile that shadows a built-in name is refused at load, naming the built-in
  it collides with.
- **Web settings.** The new **Worker session profiles** group has three fields: a
  select for the daemon-wide profile, a per-role/per-task-kind select block driven
  by `autonomous.worker_profile_defaults`, and a JSON-object textarea for
  `autonomous.worker_profiles` with a live "must be a JSON object keyed by
  profile name" error. Help lines link to `docs/howto/worker-session-profiles.md`.
- **REST.** `GET /api/config` carries the three keys under `autonomous`;
  `PUT /api/config` accepts all three and refuses (HTTP 400, the offending key
  named) a write that would store a shadowed built-in, reference an unknown
  profile, or name an unknown role/kind. `PUT` pre-validates the whole combined
  state before applying anything, so a partial or cross-key conflict never
  reaches the daemon.
- **MCP.** `config_set` and `get_config` cover the three keys. Two read-only
  companions are there on their own: **`worker_profile_status`** returns the
  four built-in names, the built-in `role → profile` and `kind → profile`
  defaults, and the latest `worker_startup` stats block (the average start-up
  token sizes per profile and per role, so a user can see the difference
  `minimal`, `digest` and `full` make before any of them has run); and
  **`autonomous_prd_set_worker_profile`** sets or clears (empty name) the
  per-Automaton override, returning a named refusal for an unknown profile rather
  than a silent clear.
- **CLI.** `datawatch config get` reads the three keys as `worker_profile`,
  `worker_profiles`, `worker_profile_defaults.role` and `worker_profile_defaults.kind`
  (the CLI's shorter spelling, mapped onto the `autonomous.*` paths on the wire);
  `datawatch config set` sets them, pre-validating against the local config so a
  refused value never reaches the daemon, and refuses an unknown key that merely
  starts with `worker_profile`.
- **Comm channel.** The configure verb accepts the same three `autonomous.*`
  keys; a bare key with no `=` reads back the help line for that key, and every
  help line points at the how-to.

None of these six are the app's surfaces — they are the daemon's — and every one of
them works regardless of which surface set the config. What the app is missing is
a single surface for each of these six, on Android and on iPhone.

## Current behavior

On the datawatch-app clients (Android and iPhone), none of the six daemon
surfaces has a matching surface in the app:

- **The app has no settings group for worker profiles.** A mobile operator who
  wants the daemon-wide profile changed, a per-role or per-task-kind default
  changed, or a user-defined profile added must go to a terminal, a PWA on another
  device, a REST call from somewhere else, or a comm-channel message. There is no
  field on the phone to read back the profile in force, the built-in defaults,
  or the valid name set (built-ins plus user profiles).
- **The app's settings round-trip does not touch the keys.** Even if the app's
  mobile operator could send the request, the daemon's REST answer will carry
  keys the app's config object is not reading, and its `PUT` does not set them —
  the app's config type is older than the keys.
- **The app's per-Automaton detail view cannot set or clear the profile
  override.** `autonomous_prd_set_worker_profile` is an MCP tool the app does not
  currently call; whether an Automaton is on `full` (the expensive start) or
  `minimal` is not visible from the app's Automaton detail, and neither is the
  fact that the user set it that way, and neither is a refusal when the user typed
  a name the daemon does not know.
- **The app's stats and cost surface does not carry the `worker_startup` block.**
  The four keys `average_startup_tokens_by_profile`,
  `average_startup_tokens_by_role`, and the built-in `role_defaults` and
  `kind_defaults` are all in the daemon's stats snapshot (`GET /api/stats`, the
  `get_stats` MCP tool, `datawatch stats`, `worker_profile_status`), but they
  are not in the app's cost surface, so "how expensive is `full` versus `digest`
  on a worker" is readable from a terminal or the PWA, not from the phone.
- **Refusals are not surfaced in the app.** Every daemon refusal — unknown
  profile name, a user profile named like a built-in, an unknown role/kind key —
  lands in the daemon's log and comes back as an HTTP 400 or an MCP error; if
  the app were to send one it would show the daemon's message, but it has no
  send path for these keys in the first place.

This is not a daemon defect and is not a contract change: every worker-side
behavior — the four built-ins, the role and kind defaults, the refusal rules for
each — is in force on the daemon regardless of which of the six daemon surfaces
set it, and none of the six needs the app to work. What the app's lack of a
surface for this slice means, for the operator, is that the "configurable for
every datawatch user" part of the story is not actually configurable for a datawatch
user who only has the app in their hand.

## Expected behavior

A mobile operator on Android or on iPhone should be able to read back, change,
and see the effect of the worker profile settings on at least the same ground
the daemon's six surfaces already give:

- **Read back what is in force.** Per role and per task kind, the profile the
  daemon will give a session in the tier the user last set — the daemon-wide
  default, the role map or the kind map, whichever tier the session in
  question sits on — plus the built-in defaults (in case a user cleared them) and
  the valid name set (the four built-ins plus any user-defined ones the user
  has configured). A mobile operator should be able to look at an Automaton and
  see which profile the Automaton is currently on and whether that is a user
  set, a role default, or the built-in default, not just which profile the
  Automaton will run on next.
- **Change a setting the same way on the phone.** Set the daemon-wide profile,
  set a per-role or per-task-kind default, add or edit or delete a user profile
  (including choosing one of the four built-ins or a user-defined one for any
  role or kind on an Automaton). A refusal — unknown profile name, a user profile
  named like a built-in, an unknown role or kind key, or an Automaton override
  that is being cleared to an unknown name — should land in the app as a named
  refusal, the way it does everywhere else.
- **See the effect of the setting.** The profile a session ran on, the
  starting context size with which it began (so `minimal`, `digest` and
  `full` are distinguishable on the phone, not just in a terminal), and the
  `worker_startup` maps (the four-profile / four-role averages) so the
  operator can read "a `full` worker starts at about X tokens; a `digest`
  worker starts at about Y tokens" without leaving the app.
- **Match the PWA's vocabulary.** The app's labels should line up with the
  PWA's — "Worker session profiles," "Daemon-wide worker profile," "Default
  profile per role and task kind," "Worker profiles (JSON object keyed by name)" —
  in at least the five languages the PWA's bundle already carries (en, de, es,
  fr, ja), so a user who learned the field on the web knows it on the phone.
  Translation through the Compose Multiplatform pipeline per the Localization
  Rule for any app-specific strings.

## Suggested next step

This draft is a client-side issue on `dmz006/datawatch-app`. It is the third
companion to the two prior drafts and should be filed alongside or folded into
them so the app team files one coherent "worker session profiles parity" issue
rather than three. File **after** the sessions' own start wiring has landed —
i.e. after `GET /api/stats` is actually recording start-up sizes per profile
and per role for a live attempt — because until then the
`average_startup_tokens_by_profile` and `average_startup_tokens_by_role` maps
in the app's cost surface will be empty, and "a mobile operator can see the
difference `minimal`, `digest` and `full` make" is not yet observable. At
minimum the app should:

1. **Add a settings group** for the three profile keys, matching the PWA's
   layout (daemon-wide profile select, role/kind defaults block, user profiles
   JSON or, if the app team prefers, one field per role/kind) with the same
   five-language strings the PWA already ships (EN, DE, ES, FR, JA), and a
   help link to the daemon's how-to
   (`docs/howto/worker-session-profiles.md`) from the group's title.
2. **Read back the profile in force per role and per task kind** — from the
   same `GET /api/config` the PWA reads — showing the user's
   `autonomous.worker_profiles` table (or the built-ins if none), the
   `autonomous.worker_profile_defaults` maps, the daemon-wide
   `autonomous.worker_profile`, and, per Automaton, whether the Automaton has
   an override and which profile it is.
3. **Offer per-Automaton profile setting and clearing.** Call
   `autonomous_prd_set_worker_profile` from the Automaton's detail screen
   (or the equivalent app surface, whatever the idiomatic name there is),
   with the same empty-name-to-clear semantics the PWA already has, and
   surface the daemon's named refusal for an unknown profile rather than
   letting it silently not apply.
4. **Surface the `worker_startup` block** in the app's cost and usage surfaces
   — the two maps (`average_startup_tokens_by_profile`,
   `average_startup_tokens_by_role`) plus the built-in `role_defaults` and
   `kind_defaults` — so the comparison "a `full` worker starts at ~X; a
   `digest` worker starts at ~Y; a `minimal` judge starts at ~Z" is readable
   on the phone rather than only in a terminal. The `worker_profile_status`
   MCP tool gives exactly this shape (profiles, role defaults, kind defaults,
   startup block) as one call; the app does not need a new daemon endpoint.
5. **Surface refusals the way the REST layer returns them** — the offending
   key named, the valid set listed where relevant, the daemon's own wording.
   The app's config-PUT and config-SET surfaces already surface other daemon
   refusals the same way; this is not a new class of error, it is the same
   class with three new keys.

If the app team decides worker profile configuration is a daemon/terminal-only
advanced surface and not a first-class setting the mobile operator sees, record
that decision in the issue as an intentional exclusion with its reason. The
attempt-level showing (profile run, context size, per-role/per-profile
`worker_startup` averages) is the part that should not be excluded — that is
the operator's only way to see the difference this change is meant to make, and
the app currently has no way to show it at all.

## Filing record

- Filed: — (pending; issue number / URL once opened; file after the sessions'
  start wiring — i.e. after `GET /api/stats` actually starts reporting
  non-empty `worker_startup` maps for a live attempt — so that the expected
  behavior section above is testable on the phone before filing. This draft
  names the concrete keys and tools as of 2026-11-09; if the sessions' start
  wiring adds new keys, re-verify the six-surface list above against the live
  daemon before filing and update the "What this work closes" section
  accordingly.)
