# [parity] every worker session starts small: the pinned MCP set and the deterministic starting context (datawatch-app follow-up)

**Filed 2026-11-09 as dmz006/datawatch-app#415** (shipped in v9.115.0).

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened on the
`dmz006/datawatch-app` repository when the operator files it. Filing the issue does
not change a line of this repo. Draft written 2026-11-08 against the work for
"Make every worker session start small, with context and tools chosen for its
job, configurable for every datawatch user." (PRD `24230001`), specifically the
slice this story covers: the per-session MCP config writer
(`internal/channel/worker_mcp_config.go`) and the deterministic
starting-context builder (`internal/autonomous/worker_starting_context.go`),
both landed and tested. The companion draft
`2026-11-08-worker-session-profiles-parity.md` covers the settings surface and
the attempt/cost read-back from the app's point of view; this draft covers the
two mechanisms a launch applies and the surfaces that still cannot see or set
what they apply.

Parent release: the datawatch release that ships worker session profiles — the
feature currently under `[Unreleased]` in `CHANGELOG.md`.

## Affected area

Two mechanisms that decide, at launch, what a worker, verifier or judge session
may use and what it opens with:

- **The pinned MCP set.** A launch on a profile gets its own session-scoped
  `worker-mcp.json` in the session's working directory. It pins exactly the MCP
  servers the profile allows — none (`mcp: none` leaves an empty server
  object), only the datawatch server (`mcp: datawatch` keeps the single
  `datawatch` entry), or exactly the named list (`mcp: list`) — and no
  other. The project-level and instance `.mcp.json` files are untouched, so a
  small session does not change what anything else on the machine or in the
  project sees. The file is the thing a mobile or external client would
  inherit if it ever ran a tool against that session directory, and it can
  carry server environment including tokens, so its handling is a parity
  surface, not just an implementation detail.
- **The starting context.** The text block a launch on a profile opens with is
  assembled by one deterministic builder: fixed section order (Role, Profile,
  Constraints, Rules digest at `digest` and above, full rules text at `full`,
  Task last), no timestamps, run identifiers or session-specific paths — the
  same role and profile always produce the same bytes. That determinism is
  what a retry, a ladder hop, a verifier and a judge on the same
  role-profile share for prompt-cache reuse, and what the cost report
  compares profiles against.

From the operator's point of view, the affected areas are: what a worker
session is actually allowed to call (the server set is now a per-session,
readable artifact instead of an implicit global), what it begins reading
(the starting context has a stable, comparable shape), and every surface that
reads back or sets the profile behind either mechanism — config file, web
settings, REST, MCP, CLI, comm channel, and the Android and iPhone clients.

## Current behavior

The mechanism and its behavior today, on the daemon:

- `worker-mcp.json` is written mode `0600`, atomically (a temporary file in
  the session directory is renamed into place, so a reader never observes a
  partial file), with deterministic bytes for the same input. A `list`
  selection naming a server the daemon has not configured fails before
  anything is written, and the error names every missing server — a refused
  set is never a silently-shrunk one. An empty or unknown MCP mode is also
  refused with a named error.
- The starting context is always built in the same order and never invents
  content; a level that has a blank digest or rules text omits that section
  rather than printing an empty one, and constraints are always present. The
  profile is rendered as one fixed-order line, so two sessions on the same
  role and profile are byte-comparable.
- **Worker, verifier and judge sessions still start with the full operator
  harness.** Both mechanisms above are built and tested behind the Claude
  Code launcher's application path, but the launches themselves have not yet
  been wired to resolve and pass a profile — that is the sessions' own start
  work the `[Unreleased]` changelog entry names. Until it lands, `worker-mcp.json`
  is never the file a real Automaton session reads, no session carries a
  starting context from this builder, and the profile the operator configured
  has no visible effect on any live session.
- The per-surface read-back of the resolved profile, and the per-surface tool
  names that set and read `autonomous.worker_profiles`,
  `autonomous.worker_profile_defaults` and `autonomous.worker_profile`, are
  also named by the how-to as landing with the start work — so today the
  settings are settable and validated at config load, but no surface yet
  reports, for the profile in force, the server set it would pin or the
  starting-context level it would build.
- A backend that cannot honour a profile field records the field as not
  applied; that record mechanism lands per field (mcp, tools, skills,
  context, model, effort) — but the record is only as visible as the
  surfaces that will show it, which are the attempt record, the cost report
  and the stats, all of them still pending the start work.

## Expected behavior

Once the start work lands, an operator on any of the standard surfaces — the
config file, the web settings, REST, MCP, CLI, comm channel, and the Android
and iPhone clients of `datawatch-app` — should be able to:

- **Set and read back the profile, and see what it means.** The profile in
  force for a role, task kind, project or the daemon should be readable on the
  same surface it was set from, together with the two things this slice makes
  concrete: the exact MCP server set a launch on it will be pinned to (so a
  `list` the daemon has not configured is visible as refused, naming the
  missing servers, on that surface), and the starting-context level it as
  builds (so a `minimal` worker is distinguishable from a `full` one before
  either has run). A refusal — unknown profile name, unknown role/kind key, a
  user profile named like a built-in — should be reported at the surface where
  it was set, naming the offending key, the same way config-load refusal does
  today.
- **See the effect, not just the setting.** When a session has started under
  a profile, the surfaces the operator already reads — the Automaton and task
  detail, the cost report, the usage advice line — should show the profile
  the attempt ran under, the starting-context level it began with, and, per
  field, what applied and what a backend could not honour. A worker pinned to
  `mcp: none` and a worker on the full server set are the same kind of thing
  a `minimal` and a `full` start are: both differences are invisible until the
  read-back exists on the surface the operator actually looks at.
- **Behave consistently across client platforms.** Capability parity is
  required; implementation parity is not — each platform delivers it the
  idiomatic way. A mobile client that cannot express a setting must say so
  explicitly instead of silently hiding the surface that would carry it, per
  the Mobile-Parity Rule.

The daemon-side mechanisms this slice lands need no app change to be correct:
the 0600 atomic write, the deterministic bytes, the named refusals — none of
them depend on which surface set the profile. The parity gap is that nothing
outside the daemon can yet confirm a profile's two most operator-visible
consequences: what the worker may call, and what it begins with.

## Suggested next step

Fold this into the same follow-up issue as the companion draft rather than
filing two; if it is filed separately, file it **after** the sessions' own
start work lands on the daemon, because until then no launch exercises either
mechanism and the issue would describe behavior a user cannot yet observe.
At minimum, the app should: (1) in its settings or Automaton detail, show for
the profile in force per role and task kind the MCP server set the profile
pins and the starting-context level it builds, and surface a refused profile
or server list with the server names the daemon refused it for; (2) in the
Automaton/task detail and the cost and usage surfaces, show per attempt the
profile it ran under, its starting-context level with the measured size, and
the per-field applied / not-applied record so a field a backend could not
honour is visible rather than assumed; (3) if it ever exposes a session's
working directory to an operator — a file browser, a log viewer, a download —
treat `worker-mcp.json` as credential-bearing (it can carry server tokens)
and apply the same masking any server-environment surface already uses; and
(4) leave a help link to the daemon's how-to
(`docs/howto/worker-session-profiles.md`) from wherever the profile is set
or shown. If the app team decides profile assignment and per-field read-back
are a daemon/terminal-only advanced surface, record that decision in the
issue as an intentional exclusion with its reason — but the attempt-level
showing (profile run, context level, what applied) is the part that should
not be excluded, because it is the operator's only way to see that the small
start actually happened.

## Filing record

- Filed: — (pending; issue number / URL once opened; file after the
  sessions' start wiring lands, at which point this draft's "Current
  behavior" section should be re-verified against the live daemon before
  filing)
