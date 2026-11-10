# [sync] Verifier run cap — settings surfaces and operator guidance: parity draft

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Source check (2026-11-10):** every code, test, locale and doc path named below
was confirmed to exist in this worktree, and the companion drafts it links to
are present. The draft was re-checked against the story files for this work
(`docs/config-reference.yaml`, `docs/implementation.md`, `docs/operations.md`,
`docs/mcp.md`, `docs/datawatch-definitions.md`, `docs/README.md` and
`docs/howto/automaton-verifier-run-cap.md`); no wording change was needed.

**Defect this work addresses** (found 2026-11-08): a verifier run could hold a
worker slot for hours — one stuck verifier blocks the task's verification and,
with it, the worker lane that task needs to make progress, for as long as the
verifier lives.

## Affected area

The `autonomous.verifier_max_run_seconds` setting — the hard, whole-seconds
wall-clock cap on a single verifier run — and every surface through which an
operator reads, writes or understands it. The cap itself and its default (2700
seconds, 45 minutes; 0 means no cap; negative or fractional values refused)
already ship in `internal/config` and are enforced on running verifiers; the
companion draft [verifier-max-run-seconds.md](verifier-max-run-seconds.md)
tracks the config-load step. This work lands the setting on the operator
surfaces: the REST config read and write in `internal/server/api.go`
(round-trip covered by `internal/server/verifier_run_cap_config_test.go`),
the comm channel `configure` command with its refusal path in
`internal/router/router.go` (covered by
`internal/router/verifier_run_cap_configure_test.go`), the MCP autonomous
config get/set in `internal/mcp/autonomous.go` (covered by
`internal/mcp/verifier_run_cap_mcp_test.go`), the CLI `datawatch config`
surface in `cmd/datawatch/main.go` (covered by
`cmd/datawatch/verifier_run_cap_cli_test.go`), and the PWA General settings
field in `internal/server/web/app.js` with its two locale keys in all five
bundles (`settings_verifier_max_run_seconds`,
`settings_verifier_max_run_seconds_help`; covered by
`internal/server/web/verifier_run_cap_settings.test.js`). The operator
reference for the setting is
[docs/howto/automaton-verifier-run-cap.md](../../howto/automaton-verifier-run-cap.md),
whose help link the PWA field points at. The Android and iOS clients in
datawatch-app are affected in the same way any new autonomous settings key is:
they mirror the autonomous settings and must offer the key with the same
meaning.

## Current behavior

On the datawatch side the defect is closed and the setting is reachable from
every operator surface in this repo. A verifier run now ends at the configured
cap — a verifier that would otherwise hold its worker slot for hours is ended
at 45 minutes by default, and an operator can raise or remove the limit
(0 turns it off) without a restart. The value reads back as a whole number of
seconds on `GET /api/config` (default 2700 on a fresh config, an explicit 0
reads back as 0), and every write surface — `PUT /api/config`, the comm
`configure autonomous.verifier_max_run_seconds=<n>` command, the MCP
`verifier_max_run_seconds` argument on the autonomous config tools, and the
CLI — accepts a whole, non-negative number of seconds and refuses a negative
or fractional one with an error that names the key. The PWA shows the field in
General settings with a label, a 2700 placeholder and help text that says the
cap is total wall-clock time for one verifier session, is separate from the
verifier no-progress timeout (which only ends a session that shows no
activity), that 0 turns the cap off, and links to the howto. The locale keys
are present in all five PWA bundles.

The gap this leaves is on the operator-facing mobile clients. As the
datawatch-app stands, it has no knowledge of
`autonomous.verifier_max_run_seconds`: a settings surface that mirrors the
autonomous settings will hide the key or render it as an unlabelled raw field,
it has no locale strings for it in any of its languages, and it has no
validation hint, so an operator there cannot tell the cap from the no-progress
timeout, cannot see the 45-minute default, and cannot turn the cap off. The
operator guidance the PWA links to (the howto) has no counterpart in the
app's help content.

## Expected behavior

Once parity is met, a client on any platform can do what the PWA can: show
`autonomous.verifier_max_run_seconds` on the autonomous settings surface as a
whole number of seconds, with the value the daemon reports (2700 when never
set), a label and help text that state it is the total wall-clock limit for
one verifier session, that it is separate from the verifier no-progress
timeout, that 0 means no cap, and the 2700-second default; refuse a negative
or non-whole value before sending (the daemon answers those with an error that
names the key, and nothing changes); and accept a change with no daemon
restart. The labels and help text exist in every language the client ships.
Where the client already renders a verifier that was ended by the cap, it
should read as a capped end, not as a failed verification — the operator must
be able to tell the two apart.

## Suggested next step

File one `feat(autonomous):`-prefixed issue in datawatch-app covering the
`autonomous.verifier_max_run_seconds` settings field — value, label and help
text, the 0-means-no-cap and 2700-default wording, the whole-seconds
validation, and the locale keys in all of the app's languages — referencing
this draft and
[docs/howto/automaton-verifier-run-cap.md](../../howto/automaton-verifier-run-cap.md)
as the source of truth for wording and behavior. It can be filed now: the
server, PWA and operator docs are done and the setting is live on every
surface in this repo, so the acceptance criteria are a pass-through test — the
field round-trips on both Android and iPhone, a never-set config shows 2700,
0 reads back as 0, and a refused value surfaces the daemon's error. Capability
parity is required; implementation parity is not: each platform uses its
idiomatic settings screen.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | `GET /api/config` returns `autonomous.verifier_max_run_seconds` (default 2700); `PUT /api/config` writes it with the whole-seconds, non-negative validation (a refusal is a 400 that names the key). |
| MCP | included | The autonomous config get/set tools carry the `verifier_max_run_seconds` argument, described as the hard per-run cap with 0 = no cap, default 2700. |
| CLI | included | `datawatch config` reads and writes `autonomous.verifier_max_run_seconds`; negative or non-whole values are refused the same way on every surface. |
| comm channel | included | `configure autonomous.verifier_max_run_seconds=<n>` writes the key; the refusal path for a bad value is covered by its own test. |
| YAML/config | included | The key lives under `autonomous` in the daemon config, is read live (a change needs no restart), and 0 is a valid stored value meaning no cap. |
| PWA | included | General settings gained the number field with label, help, the howto link and min 0 validation; both locale keys are in all five bundles. |
| Android | excluded | No app code lives in this repo. Capability parity is required and is tracked by this draft: the settings field, its validation and its locale keys — implementation parity is not. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same items are tracked here for the native client. |
