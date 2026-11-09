# [sync] Release approval in the merge pipeline read: view field, text answers, config read-back and OpenAPI contract (Phase 4 of the autonomy handover plan)

**Filed 2026-11-09 as dmz006/datawatch-app#419, one consolidated issue for all the release approval drafts** (shipped in v9.116.0).

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Story** (253e0001, "Part of finishing review and merge by the daemon (Phase
4 of the autonomy handover plan)"; operator, 2026-11-07, quoted): the release
approval modes — `autonomous.merge_pipeline.release_approval.{patch|minor|major}`,
each `auto`, `ask` or `manual`, with per-project overrides, resolved
project → global → default — now travel through the read surfaces. This draft
closes the "read surfaces don't report the resolved mode" gap tracked in
[2026-11-08-release-approval-parity.md](2026-11-08-release-approval-parity.md)
and leaves the client-rendering gap named below.

## Affected area

The gap this work addressed sat between a resolved setting and the answers an
operator reads. The previous story landed the setting, its validation and its
resolution in `internal/config`, but the merge pipeline reads did not report the
answer to "who approves this release, for this project": `GET
/api/autonomous/merge-pipeline` gave the conflict policy and model but not the
approval mode, and the CLI, chat and MCP text answers did not spell it out.

What this work lands on the datawatch side:

- **The view field.** `internal/server/merge_pipeline.go` adds
  `release_approval_in_effect` to the body of `GET
  /api/autonomous/merge-pipeline`, resolved for the requested project (its
  override, else the global value, else the default). Every size is always in
  the body — `patch`, `minor` and `major`, each always one of the three modes —
  because an unset size resolves to its default and an unreadable release size
  resolves to `ask` rather than shipping. The shared renderers live in
  `internal/mergepipeline/view.go`, where the shape and the table tests are
  defined once.
- **The text answers.** The same resolution is spelled out in one line the CLI
  (`datawatch autonomous merge-pipeline show`), the chat verb (`autonomous
  merge-pipeline`) and the MCP tool (`autonomous_merge_pipeline_get`) all print:
  `Release approval: patch …, minor …, major …`. The wording is written once in
  `internal/mergepipeline` so the three surfaces cannot drift.
- **The config read-back.** `GET /api/config` reads the block's effective
  `release_approval` back with the same per-size resolution, so an operator can
  see where an unset key landed without guessing through the precedence chain.
- **The write path.** The three dotted keys (and their
  `autonomous.merge_pipeline.projects.<project>.release_approval.<size>`
  variants) are wired into the config write path with refusal for anything
  outside the three modes — the key names, not a silent store — and `null` for
  a size clears that size back to inheriting instead of being kept as an empty
  mode.
- **The contract.** `internal/server/web/openapi.yaml` documents the three
  setting keys (global and per-project shapes, the `release_approval` object
  with its `patch`/`minor`/`major` members), the `release_approval_in_effect`
  view object, and what each mode does in plain terms — so Swagger UI and any
  spec-driven client see the same vocabulary the docs use.
- **The docs.** [docs/howto/merge-pipeline.md](../../howto/merge-pipeline.md)
  (*Choose the release approval*, *Which one wins, and how to see it*), the
  `release_approval` row of the merge pipeline block in
  [docs/config-reference.yaml](../../config-reference.yaml), the *Release
  approval* entry of [docs/datawatch-definitions.md](../../datawatch-definitions.md)
  and the merge pipeline section of
  [docs/operations.md](../../operations.md) all name the new surfaces, and each
  of them — like the howto — is explicit about the one surface that did **not**
  catch up: the PWA card does not show the in-effect modes yet and points the
  operator to the text and JSON answers until it does (tracked as a follow-up
  in [docs/plans/README.md](../README.md), roadmap row 435). The
  `## [Unreleased]` CHANGELOG entry records the shipped scope.

## Current behavior

On the datawatch side the "who approves this" answer is one read away. For any
project, `GET /api/autonomous/merge-pipeline` carries
`release_approval_in_effect` with all three sizes resolved; the CLI, chat and
MCP text answers all carry the same line, built from the same renderer; and
`GET /api/config` answers the effective block back, so the precedence
(project's value → global → default `auto`/`auto`/`ask`) is visible rather than
reconstructed. The setting itself behaves as the docs promise: settable on
every config surface, refused with the key named when it is not one of the
three modes, and `null`-clearable per size. And it still does not act — the
daemon's own review-and-merge step that will honour the modes is the remaining
Phase 4 work, and the operator guidance says so plainly: until it lands, a
release is made exactly as before.

The gap this leaves sits on the operator-facing clients. The OpenAPI spec —
the contract Android and iPhone render against — now carries two things a
client that predates them never saw: a new object in the merge pipeline GET
body (`release_approval_in_effect`, always populated, three enum members) and
three new setting keys with a per-project nested shape. A client that renders
the merge pipeline card from the GET body will meet the field and, if it
renders strictly, either drop it or misrender it; and the app-settings surfaces
that mirror the daemon's configuration have no control for a setting an
operator can already change on every other surface. The PWA has the same card
gap on the datawatch side — this is the surface the howto flags for operator
review — so the client story and the card fix belong in one tracked decision
rather than two.

## Expected behavior

Once parity is met, a client on any platform can do what the text and JSON
answers already do:

- **Render the in-effect modes.** On the merge pipeline screen, read
  `release_approval_in_effect` from `GET /api/autonomous/merge-pipeline` and
  show the resolved mode per size — `patch`, `minor`, `major` — alongside the
  conflict policy and model the card already shows, with the same plain wording
  the howto uses (`auto` ships without waiting, `ask` raises the operator's
  decision before the release ships, `manual` leaves the ship to the operator)
  and the note that this is the value the daemon's merge path will honour once
  that step lands. All three fields are always present and always one of the
  three modes, so there is no absent-field case to degrade.
- **Expose the settings.** Add the three keys —
  `autonomous.merge_pipeline.release_approval.patch`, `.minor`, `.major` — to
  the configuration surface the rest of the `autonomous.merge_pipeline` block
  uses, each a select over `auto` / `ask` / `manual`; per-project values use the
  existing `projects.<project>` path the daemon validates. A refused write shows
  the daemon's message, and `null` for a size is available as "inherit".
- **Read the effective block back** from `GET /api/config` where the client
  currently reads the block's other keys.
- **Carry the caution.** The operator guidance attached to moving a size toward
  `auto` — above all `major`, the operator's by default — is a recorded
  decision, because once the daemon's merge runs, `auto` means it ships that
  size without anyone.

At minimum, until any of that lands, a client must not misrender or drop
`release_approval_in_effect`: unknown-but-valid body fields should read as
their payload says, not as errors, and the older app stays forward-compatible
with the newer daemon exactly as before this change.

## Suggested next step

File one `feat(autonomous):`-prefixed issue in datawatch-app covering the two
items — the `release_approval_in_effect` render on the merge pipeline screen
and the three `autonomous.merge_pipeline.release_approval.<size>` settings
controls — referencing this draft, the companion draft
[2026-11-08-release-approval-parity.md](2026-11-08-release-approval-parity.md)
for the setting's provenance, and the howto, definitions and OpenAPI spec as
the source of truth for wording and shape. The acceptance criteria are a
pass-through test where the app already renders the merge pipeline card from
its payload: a seeded `release_approval_in_effect` body displays all three sizes
correctly, each of the three settings round-trips through the config endpoint
(including a refused bad mode showing the daemon's message and `null`
clearing), and every label and option is present in EN, DE, ES, FR and JA with
Automaton wording intact. In the same decision, close the PWA card gap tracked
as roadmap row 435 — the card should show the in-effect modes from the same
body it already reads, so the howto's "read them through the text and JSON
answers" stopgap is retired. Nothing here changes any REST or MCP contract a
client must integrate — the field is additive and the keys use the generic
config path — so the issue is a render-and-settings story, not an integration
one. The one sequencing note: enforce nothing client-side. The modes are
descriptive until the daemon's merge step honours them, so the client's only
obligation is to show the truth the daemon reports and the caution attached to
changing it.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included, closed | `GET /api/autonomous/merge-pipeline` carries `release_approval_in_effect` (all three sizes always resolved); `GET`/`PUT /api/config` read back and write the three keys (and per-project variants), refusal naming the key, `null` clearing a size; no new route. |
| MCP | included, closed | `autonomous_merge_pipeline_get` returns the body with the new field and its text carries the `Release approval: …` line; the keys travel through `config_set` with the dotted keys. |
| CLI | included, closed | `datawatch autonomous merge-pipeline show` prints the line; `datawatch config set autonomous.merge_pipeline.release_approval.<size> <mode>` writes the keys. |
| comm channel | included, closed | `autonomous merge-pipeline` prints the line; `configure autonomous.merge_pipeline.release_approval.<size>=<mode>` writes through the existing config path. |
| YAML/config | included, closed | `release_approval` (and per-project `projects.<project>.release_approval`) under `autonomous.merge_pipeline` in `config.yaml`; documented in `docs/config-reference.yaml`. |
| PWA | included, **gap tracked** | The body is available on the existing card's read and the keys are settable through the generic config path, but the card does not show the in-effect modes and there are no dedicated settings controls — roadmap row 435, to be closed with the client items above in one decision. |
| Android | excluded, **gap tracked** | No app code lives in this repo. Capability parity is required and is tracked by this draft: render the in-effect modes, expose the three keys (plus per-project path), carry the caution, 5-locale wording. Implementation parity is not required. |
| iPhone/iOS | excluded, **gap tracked** | Same as Android: no code in this repo, and the same items are tracked here for the native client. |
