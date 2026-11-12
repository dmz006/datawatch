# Parity issue draft — the Merge shadow read: the five read-only operator surfaces the daemon now serves, with no datawatch-app counterpart

**Not filed. Fileable now — this step built the surface the obligation was waiting for.**
This is a file-write only task (template: parity_issue_draft): no CLI, no tracker.
When the operator files it, this is the text for the issue. This draft is the
read-side half of the sibling set for this phase — the
[settings](merge-shadow-settings-parity.md) draft owns the three
`autonomous.merge_shadow.*` keys and the [shadow merge plan](shadow-merge-plan-parity.md)
and [shadow merge comparison](shadow-merge-compare-parity.md) drafts own the
record and its grading; both of them said the app-side obligation fires the
moment a read surface exists. It exists now, and this draft is what files it.

## Parent work

"Part of finishing review and merge by the daemon (Phase 4 of the autonomy
handover plan)" — the read-side step of that work (operator, 2026-11-07), the
one that gave the shadow merge state its first operator-facing reads, on all
five surfaces the operator already uses:

- `internal/server/merge_shadow.go` + `internal/server/api.go` — the REST
  read `GET /api/autonomous/merge/shadow` (read-only, an autonomous read
  capability required, `405` with `Allow: GET` for anything else):
  `projects[]`, each with the stored `plans`, the stored `comparisons` and
  `counts` per release size (`patch`, `minor`, `major`, `none`); an empty
  `projects` list is the valid empty state, and entries whose Automaton has no
  stored plan land in the `unknown` project.
- `internal/mcp/merge_shadow_tools.go` + `internal/mcp/server.go` — the
  read-only MCP tool `autonomous_merge_shadow_status`, proxying the same route
  with an optional `project` argument.
- `cmd/datawatch/merge_shadow.go` + `cmd/datawatch/cli_autonomous.go` —
  `datawatch autonomous merge-shadow status` (matched and different per release
  size, or `no merge shadow comparison exists yet` while nothing is stored) and
  `datawatch autonomous merge-shadow compare <automaton-id>` (that Automaton's
  matched / different / not-compared and each difference with its field,
  severity, planned and landed values).
- `internal/router/merge_shadow_comm.go` + `internal/router/commands.go` — the
  comm verb `autonomous merge-shadow`, one summary line
  (`Merge shadow: patch 4 matched, 1 different; …`), `No merge comparisons yet`
  while nothing is stored.
- `internal/server/web/app-merge-shadow-card.js` (with its test) and
  `internal/server/web/app.js` — the **Merge shadow** card on the Automation
  Setup and policy sub-tab: matched and different per release size, summed over
  every project, rendered only when the shadow is enabled **and** at least one
  comparison has been stored — absent otherwise, which is the expected state,
  not a failure.
- `internal/server/web/locales/{en,de,es,fr,ja}.json` — seven new keys in all
  five bundles: `merge_shadow_card_title`, `merge_shadow_matched`,
  `merge_shadow_different`, `merge_shadow_failed`, and the four size labels
  `merge_shadow_size_patch`, `merge_shadow_size_minor`,
  `merge_shadow_size_major`, `merge_shadow_size_none`. In `de`, `es`, `fr` and
  `ja` the values are currently the English text, shipped as the placeholder
  the localization process starts from.
- The docs set names every read surface and the empty states (the "Where the
  shadow state is read" section of `docs/howto/merge-shadow.md`, the matching
  paragraph of `docs/operations.md`, the shadow merge plan entry in
  `docs/datawatch-definitions.md`).

What this closes on the parity side: the standing condition in
`shadow-merge-plan-parity.md` — "the plan is a daemon-internal record with no
operator-facing reads at all — no REST route, no MCP tool, no CLI verb, no comm
channel verb, no PWA card … there is no app issue to file today" — and the
trigger in `shadow-merge-compare-parity.md` — "file the moment any of the
intended surfaces … exists in the daemon" — are both satisfied by this step.
The comparison-read clause of the compare draft is satisfied in the route, the
tool, the verb, the CLI and the card; the data they serve is still empty until
the post-ship flow persists a comparison record, and this draft names that as
the one live contract question below.

## Affected area

The operator surface of the shadow merge read, on the datawatch-app side: the
Android and iPhone apps have a settings and status surface of their own (the
app mirrors the operator's screens and is a required parity surface), and this
step is a new read with operator-visible labels, a closed JSON contract, a
closed size vocabulary and a closed empty-state behavior. The daemon contract
the app would render against:

- `GET /api/autonomous/merge/shadow` → `projects[]`, each entry `project`,
  `plans[]`, `comparisons[]` and `counts` keyed by the four release sizes;
  `projects: []` is the valid empty state, not an error;
- each comparison: `automaton_id`, `release_size`, `matched`, `different`,
  `not_compared`, and `differences[]` each with `field`, `planned`, `landed`
  and `severity` (`major` or `minor`);
- the read-only posture: the route answers `405` to everything but GET, the
  MCP tool carries the read-only annotation, and no surface on this path writes
  anything — the plan is a record, never a queue, and filing the defect
  candidates into the defect queue is a separate job that no read triggers.

Nothing in this work touched the app.

## Current behavior

An operator can now read the shadow merge state on every daemon-side surface —
REST, the MCP tool, the CLI (`status` and `compare`), the comm channel and the
PWA card — with the stored plans listed per project today and one shared
contract across all five: the four release sizes in the fixed order `patch`,
`minor`, `major`, `none`, the matched / different / not-compared vocabulary,
the `major` / `minor` severity of each difference, and a distinct, expected
empty state on every surface (`projects: []`, "no merge shadow comparison
exists yet", `No merge comparisons yet`, no card). The stored **comparison**
reads are empty today on purpose: the post-ship flow persists the defect
candidates but the `merge-comparisons/` store exists in the code with nothing
in the production flow writing to it yet, so the `comparisons` lists and
`counts` stay empty, the card stays hidden and the CLI answers with its
empty-state line — the docs say exactly this, and the daemon never invents a
comparison. The defect candidates themselves have no read surface at all —
they are JSON under the daemon's data directory. On the client side the
behavior is the pre-this-step behavior: an operator who opens the Android or
iPhone app has no Merge shadow card, no read of this route and no knowledge of
the size or severity vocabulary this feature introduces, and the seven new
datawatch-side locale strings have no app-side counterpart at all; the four
non-English bundles carry English placeholder text for the card on the daemon
side as well.

## Expected behavior

The app carries the Merge shadow read as a first-class read, the way it
already carries the other daemon reports: a card or section with the matched
and different comparison counts for each of the four release sizes, summed over
every project; a hidden card or an explicit empty state — mirroring the PWA's
"the card is absent, and that is the expected state, not a failure" — when the
shadow is disabled or when zero comparisons are stored, and an empty
`projects` list rendered as a valid empty state, never as a broken one. When
the comparison reads fill with data (the moment the post-ship flow persists
comparison records), the app should be able to read one Automaton's comparison
— matched, different, not-compared — and each difference with its field,
planned value, landed value and severity, using the `major` / `minor`
vocabulary as-is and rendering a missing landed value plainly the way the
sibling compare draft requires, without inventing its own grading. The read is
read-only on the app side too: nothing it renders should write, and the
defect-queue filing stays the daemon's separate job. The seven keys are
translated into the app's German, Spanish, French and Japanese via the Compose
Multiplatform localization path — the `datawatch-i18n` rule's step 4, i.e.
this is precisely the `feat(i18n): add <key>(s) for <feature> parity` issue
its fourth step requires, not an optional enhancement — with
Automaton/Automata wording in all five locales. Capability parity is required;
implementation parity is not, per the Mobile-Parity Rule: the app does not
have to build the same card, but it has to be able to show the same read.

## Suggested next step

1. File one datawatch-app issue (check the tracker first; it was not searched
   for this file-only task). Title along the lines of
   `feat(i18n): add merge-shadow read keys for merge-shadow-read parity`;
   name this issue as parent release; include the seven key names from the
   affected-area list with the current English values as the reference copy to
   translate into the four non-English locales.
2. In the same issue or a linked one: the read itself — the
   `GET /api/autonomous/merge/shadow` contract above, the `patch` / `minor` /
   `major` / `none` size vocabulary, the matched / different / not-compared and
   `major` / `minor` severity words, the four empty states (one per surface,
   the PWA's being "card absent"), and the read-only posture. Note explicitly
   that the comparison data is empty today by design (the store is not wired
   into the post-ship flow yet) so the app builds against the contract, not
   against captured sample data.
3. Cross-link the three siblings — the [settings draft](merge-shadow-settings-parity.md)
   for the three keys, the [plan draft](shadow-merge-plan-parity.md) for the
   record it reads, and the [compare draft](shadow-merge-compare-parity.md)
   for the severity and `(missing)` vocabulary — and hold the defect-queue
   read of the candidates to the compare draft: the candidates have no daemon
   read surface yet, and an app that renders them would be ahead of the
   daemon's own contract.
4. Re-check this draft when the post-ship flow persists a comparison record
   (the `comparisons` lists and `counts` stop being empty) — that is the
   moment the app's comparison-detail behavior becomes specifiable against
   real data — and record the app-issue link in the filing record below when
   it is filed.

## Parity surface (this step)

| Surface | Status |
|---|---|
| REST | **Done** — `GET /api/autonomous/merge/shadow`, read-only, empty `projects` list the valid empty state |
| MCP | **Done** — `autonomous_merge_shadow_status`, read-only, optional `project` |
| CLI | **Done** — `datawatch autonomous merge-shadow status` / `compare <automaton-id>` |
| Comm channel | **Done** — `autonomous merge-shadow`, one summary line |
| PWA | **Done** — Merge shadow card, Setup and policy sub-tab, five locale bundles (four holding EN placeholders) |
| Android | **GAP** — this draft |
| iPhone | **GAP** — this draft |

## Filing record

- Not filed yet, and fileable now: the read surface this draft contracts
  against exists on all five daemon-side surfaces.
- Filing rule: datawatch-app issue, title
  `feat(i18n): add merge-shadow read keys for merge-shadow-read parity`,
  parent release = this issue; keys to translate:
  `merge_shadow_card_title`, `merge_shadow_matched`,
  `merge_shadow_different`, `merge_shadow_failed`,
  `merge_shadow_size_patch`, `merge_shadow_size_minor`,
  `merge_shadow_size_major`, `merge_shadow_size_none` (the reference copy is
  the current English values); contract: the route, the size and severity
  vocabulary, the empty states, the read-only posture; cross-links: the three
  sibling drafts above.
- Drafted 2026-11-09, from the worktree of Automaton `d2f10001` (Phase 4 of
  the autonomy handover, the read-side step); the code-level claims above
  (the read-only route and its `405` posture, the five surfaces, the
  card-render conditions, the seven keys with English placeholder values in
  `de`/`es`/`fr`/`ja`, and the empty-by-design comparison reads) were checked
  against the current tree on 2026-11-09.
