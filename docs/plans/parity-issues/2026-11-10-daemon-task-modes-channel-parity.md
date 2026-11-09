# The daemon task-mode settings reach every channel on paper, but comm's help, the test corpus and the MCP examples teach a mode vocabulary and a template id that do not exist, and the per-project gate override has no live channel at all

**Filed 2026-11-09 as dmz006/datawatch-app#420, one consolidated issue for the three daemon task mode drafts** (shipped in v9.117.0).

## Affected area

The settings side of the "daemon does it itself" work, as it lands on the
operator-facing config channels. The contract being taught is the one in
internal/config/decompose_lint.go — `task_templates.mode` is a map of
template id to `llm`, `daemon` or `off` (`taskTemplateModeValues`), where
`daemon` is refused for `final_gate` (line 495) and accepted only for the
four daemon-capable ids `changelog_entry`, `parity_issue_draft`,
`bounded_load_step`, `version_bump_left_to_merge`
(`DaemonCapableTaskTemplateIDs`, line 201-203; refused elsewhere with the
ids named in the error, line 498) — plus the two script-gate keys in
internal/config/config.go and internal/config/script_gate.go:
`autonomous.script_gate_timeout_seconds` (whole seconds, default 900,
`0` = no cap, negative refused) and `autonomous.project_gate_command`
(one line of shell text, 1024-byte cap, empty = unset), with the per-project
override `projects.<name>.policy.project_gate_command` at the top of the
resolution (the project's command when set, else the global, first set wins —
internal/config/project_policy.go, surfaced read-only by
`GET /api/projects/{name}/policy/effective` in internal/server/projects.go).
The channels that carry the contract today: REST `GET`/`PUT /api/config`
(internal/server/api.go — the mode map whole or per-id, both gate keys,
including the per-project policy write in the project upsert), the Web
Settings panel (internal/server/web/app.js — per-template mode select plus
the two gate fields, each labelled with its cap and default), the MCP config
tools (internal/mcp/autonomous_daemon_task_keys.go — get and set, all three
keys, merge-per-id semantics, correct vocabulary and limits in the
descriptions), the CLI `config set` passthrough (cmd/datawatch), the
comm-channel `configure` verb (internal/router/router.go), and the docs set
(the 2026-11-09 how-to, the glossary entries, operations, the config
reference and the OpenAPI schema). The areas named below are where the
channels diverge from that contract.

## Current behavior after this work

The write path is genuinely at parity. Every channel that writes these
keys funs through the daemon's one validator: REST's `PUT /api/config`
accepts the whole `mode` map or a single `mode.<id>` entry and checks each
value against the three modes, the two gate keys through the shared
line/bytes and whole-seconds rules; the Web panel's per-template select and
its two gate fields save the same dotted keys one at a time, so a save
can never drop a sibling; the MCP `set` builds the same dotted patch and a
refused value comes back as a tool error with nothing stored; the CLI and
the comm `configure` verb send the operator's exact `key=value` through and
show the daemon's refusal unchanged. Refusal is therefore uniform by
construction, and the docs set — the how-to, the glossary, operations, the
config-reference entries and the API schema — all agree on what the values
mean. What is not at parity is what the channels *teach*: the comm help
text, the test fixtures that pin that text's world, and the MCP argument
examples all name a third vocabulary and a template id that belong to no
other setting, while the per-project override that outranks every global
the channels can write has no comm, CLI or MCP route at all.

## Expected behavior

An operator choosing where to set one of these three settings should get the
same contract back from wherever they ask: the same three mode values in
the same three words, the same two gate keys with the same cap and default,
and the same answer to "whose gate command runs for this project". The
`help` and `list` output of the comm `configure` verb is the one part of
that contract a chat-only operator reads in full — it is their only
vocabulary table — so it should say `llm | daemon | off` (with the
`final_gate` and daemon-capable-id restrictions, the way the MCP argument
descriptions and the Web field labels already do), it should carry the
`0 = no cap, default 900, negative refused` annotation for the timeout and
the `one line of shell text, max 1024 bytes` annotation for the command, and
it should say which gate command wins when a project has one of its own.
The test fixtures and the MCP examples should use ids that exist, so that
copying them produces a setting that does what it looks like. A mode value
for a template id that is registered nowhere should be refused with the
valid ids in the message, the same way a `daemon` value already is — not
stored as a setting that applies to nothing. And the per-project gate
command should be readable and settable from at least as many channels as
the global it shadows.

## Gaps this work leaves

The largest gap is the vocabulary the comm channel teaches. Both help
surfaces in internal/router/router.go — the `help` verb's "Common keys"
block and the `list` verb's "Configurable keys" block — carry the line
`autonomous.task_templates.mode.<template id> (off|shadow|on),
autonomous.script_gate_timeout_seconds (whole seconds),
autonomous.project_gate_command (command run as the project gate, empty =
none)`. The `(off|shadow|on)` triad is the vocabulary of half a dozen other
settings in that same help text — `repo_ci_watch.mode`, `lane_fill.mode`,
`questions.held_mode`, `merge_pending_mode`, the security gates — and this
line reads like it was copied from one of them. None of it is true of task
template modes: the valid values are `llm`, `daemon` and `off`; `daemon` is
refused for `final_gate` and allowed only for the four daemon-capable ids;
and there is no `shadow` or `on` anywhere in this setting. A chat-only
operator reading their help and typing `configure
autonomous.task_templates.mode.changelog_entry=shadow` gets `unknown mode`
back — a correct refusal, taught by the wrong help. The other two words in
the line are thinner but real: `(whole seconds)` omits that `0` means no
cap, that 900 is the default and that negative values are refused; `(command
run as the project gate, empty = none)` omits the one-line form, the
1024-byte cap and that it must be a command executable by a shell on disk —
annotations the very same panel's Web field labels and the MCP argument
descriptions carry. The line also never says that a project's own
`policy project_gate_command` outranks this one, which is the setting an
operator most needs to know is being shadowed.

The same wrong vocabulary and the same invented id are pinned as truth in
the test corpus. `TestDaemonTaskConfigureSurface_KeyCarriedThrough` in
internal/router/daemon_task_configure_test.go drives `autonomous.task_templates.mode.changelog`
with the value `shadow` — `changelog` is not a template id (the
daemon-capable one is `changelog_entry`) and `shadow` is not a mode — and
its refusal case asserts the daemon would answer `invalid value "bogus"
(valid values: "off", "shadow", "on")`, a refusal message the real validator
never produces. `TestDaemonTaskConfigSetCLI_SendsKeys` in
cmd/datawatch/cli_daemon_task_config_test.go carries the identical
`mode.changelog = shadow` pair. These pass because both tests stub the
daemon side, so they pin the router's and the CLI's passthrough rather than
the contract — but a fixture that names, in the only example the test file
has, an id and a value that do not exist teaches the same fiction the help
text does, and the invented refusal string has now been quoted into a test.
The MCP argument descriptions carry the right vocabulary and limits, but
their worked examples use the same invented id:
`({"changelog":"daemon"})` and `"changelog=daemon,final_gate=llm"` in
internal/mcp/autonomous_daemon_task_keys.go. Copying them into a real call
is refused for `daemon` ("daemon needs a daemon-capable id") — a correct
refusal, but one that arrives only after the operator has trusted an
example — while `changelog=llm` or `changelog=off` is accepted, which leads
to the fourth gap.

That gap is in the validator itself, and it is the one with the quietest
consequence. `TaskTemplatesConfig.Validate` in
internal/config/decompose_lint.go checks each `mode` entry's id only for
shape — non-empty, no surrounding space, a legal rule id — and its value
against the three modes, with the daemon-capable and `final_gate`
restrictions on the `daemon` value. An id that is not a registered
template — `changelog` included — passes for `llm` and `off` and is
stored as a live setting that applies to nothing: `EffectiveTaskTemplateMode`
resolves by the real ids, so the entry sits in the config, reads back on
every channel, and changes the behavior of no template. A setting that
looks set is the worst kind of setting for an operator who measured sessions
against it; the `daemon` value at least buys the refusal. The rest of this
work's contract refuses unknown things by name — unknown backends are
refused with the known ones offered — and the mode map is the one place that
part of the contract is missing.

The fifth gap is the per-project override, which outranks everything the
channels write. `projects.<name>.policy.project_gate_command` is the first
entry of the resolution the how-to and operations document: the project's
command when set, else the global `autonomous.project_gate_command`, first
set wins. The global is writable from REST, the Web panel, MCP, CLI and
comm — every channel this session checked — while the per-project form has
a write (the project upsert in internal/server/projects.go, which validates
it through the project-policy rules) and a rich read (the `/policy/effective`
view, value plus source per setting) but no comm, CLI or MCP route:
`configure` has no `projects.<name>.policy` key in its help, the CLI's
daemon-task config has no per-project form, and the MCP config tools carry
the global key only. An operator who sets the global gate by chat and
discovers it is being shadowed by a project policy set in a config file has
no channel to inspect, change or clear that override — and the comm help
that taught them the global never told them a stronger value exists.

The sixth gap is the one both of the 2026-11-09 drafts already half-name:
the mobile surface. The Web panel's mode select and gate fields (the labels
in internal/server/web/app.js that carry the caps and defaults) have no
counterpart in the PWA build — the daemon-task keys appear nowhere in the
mobile settings — so Android and iOS operators who can set the global gate
from chat cannot see what mode each template runs in, let alone flip one.
The 2026-11-09 drafts assign the mobile follow-on to the app side; this
gap is listed here because a fleet whose gate is tuned by comm or CLI will
be read out by mobile.

None of these gaps change what the daemon does: the runs are the same, the
refusals are the same, the docs are the same. The gaps are that the channels
which are meant to teach the contract teach a vocabulary from a different
setting, that the tests and the examples pin that wrong vocabulary as
truth, that one of the setting's axes accepts values that do nothing, and
that the one value that outranks the rest is the one the channels cannot
touch.

## Suggested next step

Fix the teaching, not the contract. In internal/router/router.go, rewrite
the `autonomous.task_templates.mode` line in both help blocks (and the
`list` variant) to the actual vocabulary, the way the MCP descriptions and
the Web labels already read it: `autonomous.task_templates.mode.<template
id> (llm|daemon|off; daemon only for changelog_entry, parity_issue_draft,
bounded_load_step, version_bump_left_to_merge; final_gate takes llm|off)`,
with the timeout annotated `(whole seconds >= 0, 0 = no cap, default 900)`
and the command `(one line of shell text, max 1024 bytes, empty = unset; a
project's own projects.<name>.policy.project_gate_command, when set, runs
instead)`. Replace the invented pairs in the two test fixtures —
`mode.changelog = shadow` and the quoted `(valid values: "off", "shadow",
"on")` refusal — with a real one, say `mode.changelog_entry = daemon`, and
assert the real validator's refusal strings where a refusal is being pinned,
so the corpus teaches what the daemon actually says. Point the MCP examples
at `changelog_entry` instead of `changelog`. Add a registered-id check to
the mode-map validation (refuse an unregistered id with the valid ids in the
message, the way the `daemon`-not-capable refusal already does) or, if
arbitrary ids are meant to be legal, say so in the help and the how-to; and
give the per-project `policy project_gate_command` the same channel reach as
the global it shadows — at minimum a `configure` help line naming it and a
route that reaches the project upsert, and, if that is out of scope, a line
in the how-to's access table saying the override is config-file and Web
panel only. Fold the mobile reading gap into the app-side follow-on the
2026-11-09 drafts already assign.

## Filing record

Not yet filed. File on the same tracker as the two 2026-11-09 drafts
(2026-11-09-daemon-template-task-modes.md and
2026-11-09-script-gate-parity.md), cross-referencing both: this draft's
gaps are narrower than the ones those two name — the write channels they
reported missing (REST, Web panel, MCP, CLI, the config reference) are
present in the current tree, and what is left is the teaching layer: the
comm help vocabulary, the test fixtures and MCP examples that inherit it,
the unregistered-id acceptance, the per-project override's channel reach,
and the mobile read. Record the URL and date here when filed. Drafted
2026-11-10, with the code-level claims — the validate rules and id lists in
internal/config/decompose_lint.go, the two keys in
internal/config/script_gate.go and config.go, the help lines in
internal/router/router.go, the fixtures in
internal/router/daemon_task_configure_test.go and
cmd/datawatch/cli_daemon_task_config_test.go, the examples in
internal/mcp/autonomous_daemon_task_keys.go, the panel labels in
internal/server/web/app.js, the project upsert and effective view in
internal/server/projects.go, and the absence of the keys from the PWA build
— checked against the current tree on the same day.
