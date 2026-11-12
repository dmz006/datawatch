# Parity issue draft — merge shadow: the Phase 4 shadow step as landed today

**Not filed. Fileable now — the parent work made the obligation concrete.**
This is a file-write only task (template: parity_issue_draft): no CLI, no
tracker. When the operator files it, this is the text for the issue.
File-writing only: this draft never runs a hosting CLI (no gh) and creates
nothing on any tracker or host. The sibling drafts
`shadow-merge-plan-parity.md` (the plan record itself), `merge-shadow-read-parity.md`
(the five read-only surfaces) and `merge-shadow-settings-parity.md` (the three
settings) are still open; this draft records what the shadow step as a whole has
already exposed to clients and what it deliberately has not, so the client work
is not re-invented piece by piece.

## Parent work

"Part of finishing review and merge by the daemon (Phase 4 of the autonomy
handover plan)." Operator, 2026-11-07, quoted: "b finsih now we need to be
using it to debug it and monitor its right." The shadow step of that finish
added, beside the supervisor's manual merge, the daemon's shadow side of a
finished Automaton's release: one recorded plan per Automaton describing what
the merge would do (release size, version, changelog heading and body, README
line, registry row, parity-issue flag, conflicts, gates, findings), a
background comparison of that plan with the merge that actually landed, each
graded difference recorded as a defect candidate, the three `autonomous.merge_shadow`
settings on every config surface, and five read-only operator surfaces (REST,
MCP, CLI, comm channel, PWA card) over one shared daemon endpoint. The
operator-visible record is `docs/howto/merge-shadow.md`, the "The shadow merge
plan" section of `docs/howto/merge-pipeline.md`, and the Merge shadow and
Shadow merge plan entries of `docs/datawatch-definitions.md`. The supervisor
side is unchanged: the supervisor's manual merge remains the source of truth,
and `scripts/supervisor-watch.py` already reads the same endpoint — its
two-minute wake prints one informational line per release size with the
matched and different comparison counts, alongside its longstanding
`automaton_not_merged` findings while a finished Automaton's merge has not
landed on main.

## Affected area

The operator-facing contract the Android and iPhone clients must be prepared
for, all of it read-only or settings-only so far: a `GET /api/autonomous/merge/shadow`
per-project read (stored plans, stored comparisons, per-release-size
matched/different counts, with `projects: []` as the valid empty state); the
`autonomous_merge_shadow_status` MCP tool; the CLI verbs `autonomous merge-shadow
status` and `compare <automaton-id>`; the comm-verb one-line summary; and a
PWA **Merge shadow** card on the Automation Setup and policy sub-tab that
renders only when the shadow is enabled and at least one comparison is stored.
Plus the three settings an app must be able to set and read
(`autonomous.merge_shadow.enabled`, `.gate_timeout`, `.plan_keep`; defaults on,
`20m`, `200`) against the same validation every other surface rejects with, and
the seven PWA locale keys in all five bundles — with the DE, ES, FR and JA
values currently the English text shipped as the placeholder the translation
process starts from. Out of client scope by design: the shadow plan is a record,
never a queue — `applied` is stuck at `false` — so no client ever gets an
apply, block or approval interaction from this step.

## Current behavior

On the server side, the shadow step landed end to end exactly within the shadow
bound: the daemon records the plan and, after ship, compares it and records the
defect candidates, touching nothing — the trial merge that finds the conflicts
runs in a throwaway worktree outside the project tree, uncommitted, with the
scratch tree removed, no ref created or moved, nothing pushed. On the client
side, nothing is mirrored yet: Android and iPhone do not read the shadow state,
do not render a Merge shadow card, do not set the three keys, and do not carry
the seven locale keys. Two surfaces of the draft contract read empty by
construction today and a client must treat that as a fact, not a fault: the
comparison reads (`comparisons`, `counts`, the CLI `compare` answer, the comm
summary, the card's visibility) are empty while the store wiring stays as it
is, and the defect candidates have no read surface at all — they are the JSON
under the data directory only. And the switch out of shadow does not exist as a
setting yet: the release approval modes per release size (`automatic`, `ask`,
`manual`), decided in the handover plan, are drafted but not landed; until
they do, `enabled` is the only switch and it only stops the plan being written.

## Expected behavior

Capability parity, not implementation parity: each client reads the shadow
state from the same endpoint shape (one read, per project, with the empty
states above rendered plainly, the card rule — hidden until enabled and at
least one comparison — mirrored rather than shown empty), sets the three
settings through the platform's idiom and sees the same validation refusal
named, and carries the seven locale keys through the Compose Multiplatform
translation pipeline with the current English placeholders replaced by real
DE/ES/FR/JA values. What must *not* be expected of a client yet: anything that
acts on a plan or a difference — there is no apply button, no approval answer
for a defect, no queue filing, and no mode-per-release-size to switch — because
the server's own policy gives none of those an effect, and a client with them
would be ahead of the server. The supervisor relationship stays as is on every
platform: the supervisor's manual merge is the source of truth and the shadow
is the evidence; a client that displays a match count is displaying evidence,
never a gate.

## Suggested next step

File this draft as one datawatch-app issue (checking the tracker first for an
existing one, per the rule) covering the read, the card, the three settings
and the locale keys, linking the three sibling drafts so the per-surface
contracts are not rewritten, and asking for the placeholder translations
alongside. Record in the issue what is explicitly *not* client work: the
defect-candidate filing (no read surface exists), the comparison-store wiring
(the empty reads are the valid state), and the approval-mode interaction
(nonexistent until the mode-setting lands — at which point that step's own
draft must name the pending-approval view and the answer flow, because no
client has an `ask`-mode approval screen today). Keep this file updated as
each of those lands, and replace this status line with the issue reference the
day the operator files it.

## Filing record

Not filed. Nothing has been run on a tracker and nothing was created on any
host: file write only, per the template and the session boundary.
