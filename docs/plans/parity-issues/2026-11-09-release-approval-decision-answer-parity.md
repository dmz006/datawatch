# [sync] The release approval decision is one ordinary question: answer it with the existing question answer — no new route, no new tool, no new verb (Phase 4 of the autonomy handover plan)

**Filed 2026-11-09 as dmz006/datawatch-app#419, one consolidated issue for all the release approval drafts** (shipped in v9.116.0).

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Story** (22da0015, "Docs set for Part of finishing review and merge by the
daemon (Phase 4 of the autonomy handover plan)", operator, 2026-11-07,
quoted): the daemon's own merge run now honours the release approval modes —
`auto`, `ask` or `manual` per release size, resolved project → global →
default — through the release gate, and in `ask` it raises one yes/no
decision ending "Ship this release?". What this story lands is the operator
guidance around that decision: the surfaces that name it, where it lives in
the operator's question list, and the one and only path that answers it.

## Affected area

The gap this work closed sat between the release gate and the surfaces an
operator actually reads. The gate itself already shipped in `internal/mergepipeline`
(pure decision in release_gate.go, the ask in release_gate_ask.go, the answer
read-back in release_gate_answer.go, the once-per-run wiring in
release_gate_run.go), and the sibling drafts cover the setting
([2026-11-08-release-approval-parity.md](2026-11-08-release-approval-parity.md)),
the read surfaces
([2026-11-08-release-approval-view-parity.md](2026-11-08-release-approval-view-parity.md))
and the Settings page
([2026-11-08-release-approval-settings-web-parity.md](2026-11-08-release-approval-settings-web-parity.md)).
None of them, and no doc, named what the operator does when the gate asks.

What this work lands, in the docs that carry the contract:

- [docs/api-mcp-mapping.md](../../api-mcp-mapping.md) — the mapping table for
  the merge pipeline block gains a row that says, in one place, that the
  release decision adds **no route**: the daemon raises it as one ordinary
  stored operator question asked by the daemon's release approval for the
  project, and answering it is the existing `POST /api/questions/{id}/answer`
  with option `yes` (ship) or `no` (hold, the default), or the MCP tool
  `question_answer` with the same ids — release approval adds no route, CLI
  verb or tool of its own.
- [docs/howto/merge-pipeline.md](../../howto/merge-pipeline.md) — the
  *Answer the decision* section of the release approval walkthrough: the
  decision's identity in the question list (asked by the daemon's release
  approval for the project, not a question another person asked), its two
  options and their labels ("yes (ship it)", "no (do not ship)"), "no" as the
  default so an unanswered decision never ships, the answer paths (the
  question list in the web UI, the chat channel, the `question_answer` MCP
  tool), a later answer replacing an earlier one, and a re-run for the same
  project, Automaton and size finding the decision already raised rather than
  raising a second one.
- [docs/mcp.md](../../mcp.md) — the merge pipeline tool entry now says the
  decision the daemon's own merge run raises in `ask` mode is one ordinary
  stored question, answered with `question_answer` — option `yes` ships,
  `no` (the default) holds — and that there is no release-specific tool.
- The supporting entries the walkthrough points at: the *Release approval*
  and *Release gate* sections of
  [docs/datawatch-definitions.md](../../datawatch-definitions.md), the
  `release_approval` row of the `autonomous.merge_pipeline` block in
  [docs/config-reference.yaml](../../config-reference.yaml), and the PWA
  wording in the five web locale bundles — all of them agree that the modes
  are set through the ordinary config paths and that the decision is
  answered where questions are answered.
- The tracked app contract that the docs describe but do not implement:
  [release-approval-modes.md](release-approval-modes.md), the pending `[sync]`
  file the operator files as the app issue.

## Current behavior

On every datawatch and PWA surface the shipped gate and its documentation
agree. In `ask` mode the merge run raises exactly one decision per project,
Automaton and size, carrying the merge plan in plain words — the project, the
Automaton, the size, the version the release would get, the branches it
merges, the checks and their results, and the steps that would leave the
machine in the order they would run. It lands in the existing question list;
answering it uses the existing question answer path — the web question list,
the chat channel's question answer, `POST /api/questions/{id}/answer`, or
`question_answer` over MCP — and nothing release-specific is called anywhere:
no release-specific route is registered, no release-specific MCP tool is
listed, no release-specific CLI verb or comm verb exists. "No" is the default
and an unanswered decision holds, a later answer replaces an earlier one, and
a re-run finds the open decision instead of stacking a second one. A failed
check holds the release in every mode and is never asked about.

Before this guidance the same behavior existed in code but was unnamed in the
surfaces: the MCP tool description for the merge pipeline named the three
tools and the config keys but not the decision, the API/MCP mapping table had
no row for it, and the howto described the modes and the gate without saying
how an operator — or a client — answers the question. A reader looking for
"how do I answer Ship this release? from MCP or a device" had to find the
generic question answer by itself, and could reasonably have assumed a
release-specific tool or endpoint existed or was missing. That assumption gap
is what this guidance closes.

## Expected behavior

Parity on the operator side — the datawatch, PWA, MCP, CLI and comm surfaces
— is met by what ships: nothing new to integrate, because the decision is a
stored question and the answer is the stored-question answer.

The gap this leaves is the companion app. The datawatch-app (Android and
iPhone) question list is where the operator reads and answers questions away
from the web UI, and it has no contract yet for this one specific question:

- The decision should appear in the app's existing questions list as an
  ordinary question — no release-specific screen, control or call — with the
  asker shown as the daemon's release approval for the project (asker kind
  system, label `decision`), the plan as the question text, and the two
  options rendered with their labels, "yes (ship it)" and "no (do not
  ship)", with "no" the presented default.
- Answering from the app uses the app's existing question answer path, so an
  unanswered decision never ships, a changed mind replaces the earlier answer,
  and a re-raise is never a duplicate the operator has to pick apart.
- The wording for all of it is already set in the locale bundles and the
  howto, so the app renders the daemon's words rather than coining its own.

The tracked contract for exactly that is
[release-approval-modes.md](release-approval-modes.md); the issue it
describes is "pending, filed at the merge".

## Suggested next step

File the issue recorded in
[release-approval-modes.md](release-approval-modes.md) when the operator
files it, with this draft as the reference for why the app story is a
render-and-answer story rather than an integration one: the decision adds no
route, no tool and no verb, so the app's obligation is to render one
recognizable kind of question it already knows how to answer — the seeded
question displays the plan, both labelled options and the "no" default
indistinguishably from any other question, an answer round-trips through the
existing answer path and flips ship and hold on the daemon side, and the
labels are present in EN, DE, ES, FR and JA with Automaton wording intact. Do
not enforce the modes client-side in that story: the daemon is the only
enforcer, and the app's job is to show the decision and carry the answer.
Until it files, the web question list and the `question_answer` MCP tool are
the answer paths, which is what the docs now state.

One ordering note for the plan, not for the app: the Phase 4 exit evidence —
the shadow comparisons where the daemon's merge plan is checked against the
supervisor's manual merge, at least ten including two with conflicts and two
with PWA changes, before the modes move toward `auto` — still accrues under
the handover plan's own supervision, and each mode change is a recorded
decision with its evidence. The draft's acceptance bar does not depend on
that; the app renders the decision the gate raises either way.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included, closed | the decision is one stored question (`asked_by` the daemon's release approval, options `yes`/`no`, `no` the default); the answer is the existing `POST /api/questions/{id}/answer`; release approval registers no route of its own. |
| MCP | included, closed | `question_answer` with the same ids answers it; `mcp.md` and `api-mcp-mapping.md` both say plainly that there is no release-specific tool. |
| CLI | included, closed | no new `datawatch` verb; the decision is answered through the ordinary question answer, and the settings it turns on are set with the existing `config set` keys. |
| Comm channel | included, closed | the chat question answer path answers the decision; the `configure` verb sets the modes; no release-specific verb exists. |
| PWA | included, closed | the decision appears in the question list and is answered there; the merge pipeline card and the Settings page already carry the per-size modes. |
| Companion app (Android/iPhone) | open, tracked | the question-list render of the decision — asker, plan text, labelled options, `no` default, existing answer path, five-locale wording — is the pending `[sync]` contract in `release-approval-modes.md`, issue pending the operator's filing. |
