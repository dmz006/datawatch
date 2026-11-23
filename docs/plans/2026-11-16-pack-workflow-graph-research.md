# Pack workflow graphs: research first, display only, then editing

- Date: 2026-11-16 (raised by the operator). Answers written 2026-11-17.
- Version at the time of research: v9.182.3. Shipped version: n/a — research only, nothing shipped yet.
- Status: research — the note is answered (step 0 below) and sits at the operator's acceptance gate; no code is planned until the operator accepts it.
- Scope: documentation only. This note and the repository docs it cites; no code, no configuration, no shipped behavior changes.
- Operator review: the four open design questions in the final section are operator decisions and block the editable step onward; everything else is gated on the operator accepting this note.

## The ask

Operator, quoted: "add to plan that we need for pack to have lang graph for all workflow, or should be clean and research and see what we can and might be creating doing with that, display only, allow changes, tie to bundle pack since each defined a workflow".

In plain terms: every pack should carry its workflow as a graph the operator can look at (display only first), then change, and the graph belongs to the pack because each pack defines a workflow. LangGraph is named as the model to learn from.

## What already exists (read this before designing anything)

- **A graph model.** An Automation is a named chain of Automata with forks, joins and nested Automations; each member has an `on_complete`, `on_blocked` and `on_failed` edge (`docs/howto/automation-chains.md`). The Automation detail view already draws it as a pipeline strip.
- **Orchestrator graphs and a converter.** Orchestrator graphs exist (`docs/howto/automata-orchestrator.md`), and `POST /api/automations/import-graph/{graph_id}` (CLI `datawatch automation import-graph`) turns one into a draft Automation, refusing a graph an Automation cannot express.
- **Packs are only a draft.** Automaton `72bc0001`, "Installable packs for standard tasks and Automaton templates", is an archived draft with unmerged work. It deliberately leaves the pack format, the trust rules and the placement in the app as open design questions; no task may decide them.
- **Standard tasks and templates.** The built-in standard tasks (changelog entry, parity issue draft, docs set, bounded load step, version note, final gate) are a fixed list; Automaton templates are stored separately.

So the data model for a workflow graph is already datawatch's own. LangGraph would be a second model of the same thing.

## Recommendation

1. **Research first, with no code.** Answer the questions below in this note and have the operator accept it.
2. **Do not adopt the LangGraph runtime.** It is a Python library; datawatch is a Go daemon, and adopting it would add a second engine next to Automations. That runs against the pack draft's own principle of "no second plugin system".
3. **Use LangGraph as an interchange and a reference.** Describe a pack's workflow in datawatch's own graph schema, and offer an optional export to a LangGraph-compatible description for people who want to use it elsewhere.
4. **Step by step:** display only, then editable, then interchange. Each step ships on its own.

## Proposed steps

| Step | What the operator gets | Reuses | Status |
|---|---|---|---|
| 0. Research | This note answered and accepted | existing docs only | Done — the note is answered and cited to its sources; acceptance rests with the operator |
| 1. Display only | Each pack shows its workflow as a read-only graph in the app | the Automation pipeline strip; the pack's manifest | Planned — ships on its own after acceptance |
| 2. Editable | The operator can change a pack's workflow: add, remove or reorder steps and edit edges, with the pack version pinned and a change recorded | Automation insert, remove and edge controls; pack trust rules | Planned — blocked on the trust rules the pack draft leaves open |
| 3. Interchange | Export and import of a pack workflow as a LangGraph-compatible description | the import-graph pattern | Planned — the later step from the table, read-only by the Q7 answer |

## Questions the research must answer

1. Which packs define a workflow: standard task packs, Automaton template packs, both, or a new "bundle pack" that lists several?
2. What is a bundle pack: a pack of packs, or a workflow whose nodes are packs? The operator ties the graph to the bundle pack, so this decides the graph's node type.
3. How do LangGraph's concepts map to ours? State schema to Automaton and task state; nodes to templates or Automata; conditional edges to the three edges plus decisions; interrupts and human review to held questions and review states; checkpointing to the persisted records.
4. What does "display only" show: the planned graph, the live state of a run, or both?
5. What may an operator change in step 2, and what must stay fixed (for example the final gate and the review steps)?
6. How do pack versions, trust rules and a changed workflow interact: is an edited workflow a new pack version, a local override, or a fork?
7. Does the exported description need to run in LangGraph, or only to read there?

## Answers

These are grounded in the repository's own docs; every file named below is cited where it is read from.

### Q1 Which packs define a workflow

Which packs define a workflow: Automaton template packs, not standard task packs, and not a hypothetical bundle pack.

The unit of a workflow in this system is an Automaton: an Automation is a named chain of Automata, and each member is "a goal that is decomposed, approved, run and verified" (`docs/howto/automation-chains.md`). An Automaton template is exactly the reusable definition of such a unit — "a complete spec (type, stories, tasks, backend, effort, model, skills) stored in the template store" that an operator uses to start a whole Automaton (`docs/howto/automata-standard-tasks.md`). A standard task, by contrast, does not own any chain: it is "a built-in task definition the decomposer appends to a story", one task inside someone else's Automaton, and the built-in list is a fixed set (`docs/howto/automata-standard-tasks.md`). So a standard task pack defines a task catalogue, not a workflow; a pack of Automaton templates defines one, because each of its templates is one node of the existing graph model.

The "bundle pack" option is not in the repository yet. The row for the archived draft "Installable packs for standard tasks and Automaton templates" in the archived-unmerged inventory records it as 111 commits ahead with 152 files, plus or minus about ten thousand lines, the largest item on the list, with the pack format deliberately left open (`docs/plans/historical-plans/2026-11-14-archived-unmerged-automata-inventory.md`). Nothing that ships today can decide for it. The only place where several things are composed into one workflow today is the Automation chain and the earlier orchestrator DAG (`docs/howto/automata-orchestrator.md`), and both compose Automata — never standard tasks.

Recommendation for the operator: a pack carries a workflow graph when it is a pack of Automaton templates. A pack that also holds standard tasks keeps them as a list on the side, since they cannot be graph nodes; if the operator wants one object that bundles both, that object is the draft's bundle entry, and the workflow field on it describes the templates it contains. This keeps the answer inside what already exists and leaves the draft's open question — the pack format — to be decided by whatever work picks the draft up, as this note's Recommendation already asks.

### Q2 What a bundle pack is

What a bundle pack is: a workflow whose nodes are packs, not a pack of packs.

That is the only reading that the existing graph model supports. The Automation data model is a per-outcome edge graph, and the design that chose it explicitly rejected a flat ordered list because "a flat ordinal cannot express 'if X is blocked, run the fix-up Automaton Y, otherwise continue with Z'" (`docs/plans/2026-10-14-automation-orchestration-design.md`). Every node in that graph is a thing that runs — a member Automaton, a fork, a join, or a nested Automation (`docs/howto/automation-chains.md`) — and the importer from the older orchestrator graphs maps fan-out and fan-in onto paired fork and join and refuses a shape an Automation cannot express rather than inventing a new node kind (`docs/commands.md`, and `docs/howto/automation-chains.md` section 8).

A "pack of packs" would need a node kind that is a collection of work rather than work itself, and no such node exists anywhere in the model: standard tasks are added to a story by the decomposer and are not nodes of any chain (`docs/howto/automata-standard-tasks.md`). So a bundle pack should be defined as a named workflow — a set of Automaton template packs as the nodes, plus the edges between them, using the same one-edge-per-outcome shape — and the operator's phrasing that "each [pack] defined a workflow" already says this: every node is a pack that defines a step, and the bundle is the graph over those steps.

Two follow-ups that fall out for step 1 (display only): the graph to display is the bundle's — templates as nodes, outcome edges as the links — and the display can reuse the Automation pipeline strip, which already draws chains, forks, joins and the live branches (`docs/howto/automation-chains.md` section 7). Because the draft has the pack format as an open design question (`docs/plans/historical-plans/2026-11-14-archived-unmerged-automata-inventory.md`), this definition is offered as the answer to that question, not as a settled fact.

### Q3 How LangGraph concepts map to ours

How LangGraph's concepts map to this system, mapping by mapping:

- **State schema to Automaton and task states.** LangGraph's channel-and-reducer state is the shared object every node reads and writes. Here the shared object is the Automation record itself: its status lifecycle (draft, active, paused, completed, blocked, cancelled), its member statuses, and the per-member outcome it observed, `docs/howto/automation-chains.md`, with the same pattern in the older orchestrator's state list (draft, planning, needs_review, approved, running, paused, completed, failed) in `docs/howto/automata-orchestrator.md`. The design record lists the statuses a member can reach and the edge each one triggers (`docs/plans/2026-10-14-automation-orchestration-design.md`).
- **Nodes to templates or Automata.** A node here is an Automaton — the member's reference, with an Automaton template as the reusable definition of one (`docs/howto/automata-standard-tasks.md`). The import converter reads this directly: an orchestrator graph's Automaton nodes become chain members, and a graph with no Automaton nodes is refused (`docs/commands.md`).
- **Conditional edges to the three edges plus decisions.** LangGraph branches on node output; an Automation branch is explicit per outcome: `on_complete`, `on_blocked` and `on_failed` on every member, an empty edge meaning "stop here" (`docs/howto/automation-chains.md`). Decisions are the recorded reasons attached to an Automation — the same shape carried by the members' records in the design record — and the design chose the per-outcome edge graph over alternatives for exactly this reason (`docs/plans/2026-10-14-automation-orchestration-design.md`).
- **Interrupts and human review to held questions and review states.** The operator gates in an Automation are the approval gate on each member, blocked members that need operator action, held inserts that wait for approval, and review-type states that are non-terminal and simply wait (`docs/howto/automation-chains.md`, `docs/plans/2026-10-14-automation-orchestration-design.md`). The orchestrator's `needs_review` and the guardrail-refusal block are the same pattern in the older model (`docs/howto/automata-orchestrator.md`).
- **Checkpointing to the persisted records.** Every Automation write is versioned, audited, and recoverable: the design persists the records so a restart can resume the chain, an edit conflict carries its own error, and a member that is already done is immutable history (`docs/plans/2026-10-14-automation-orchestration-design.md`). The operator-visible controls — skip, insert, remove, reorder edges, pause, resume, cancel — are all edits onto that same persisted record rather than a runtime to attach to (`docs/howto/automation-chains.md`).

The consequence for the interchange step: an export to a LangGraph-compatible description would name the templates (or Automata) as nodes, the three outcome edges plus the decision notes as conditional edges, the review and held states as interrupt points, and the persisted Automation record as the checkpoint — which is the same content the operator sees in the strip, already in this system's own vocabulary.

### Q4 What display only shows

What "display only" shows: the planned graph of the pack — and the live state of a run when one exists, but that live view already exists elsewhere and the display step should not rebuild it.

Step 1 of the proposed steps says each pack shows its workflow "as a read-only graph in the app", reusing "the Automation pipeline strip; the pack's manifest". That is a planned graph: the templates as nodes and the outcome edges as links, read from the pack's manifest, before anything runs. The live half is not new work either: the Automation tab "opens a strip that draws the chain, including forks and joins and the branches live right now", with a control button on each member card (`docs/howto/automation-chains.md`, section 7). And the parts that matter while waiting on a decision are already displayed where they belong: the held-question card on the Automation view with its five held states and shadow entries (`docs/howto/automaton-held-state-questions.md`), and the completion review's hold in `needs_review` with its findings and retries spent (`docs/howto/automata-completion-review-autofix.md`).

So the answer is: display only shows the planned graph. When the pack's workflow has been turned into a live Automation for a run, the same strip already marks the branches that are live right now and owns the controls on them, and the pack's display links there instead of drawing a second live view. The one thing the display step genuinely adds is the read-only graph of the pack itself, read from its manifest; everything live is delegated to the surfaces that already render it.

### Q5 What an operator may change

What the operator may change, and what must stay fixed: the operator may add, remove, reorder and rewire the work steps of a pack's workflow, exactly along the shape of the existing edit controls; the gates never move.

The movable set is defined by the controls the system already has. They cover: insert a member, with a required reason and an optional position or stage; remove a member that has not started; rewrite a member's three outcome edges, where one left out becomes "stop"; skip a stuck or not-started member along a chosen edge; and pause, resume or cancel the whole chain (`docs/howto/automation-chains.md`, section 6, and the `skip`, `insert`, `remove`, `edges`, `approve` rows of `docs/commands.md` under `datawatch automation`). Notably, an insert made by an agent can be held for approval by the operator before it joins the chain — the editable step reuses a surface where the operator's trust decisions already sit, and where "a reason is required" is part of the contract for adding a step.

The fixed set is history and gates. "A member that has already completed, been blocked or been skipped is history and cannot be removed or rewired" (`docs/howto/automation-chains.md`). The completion review is "the last check on an Automaton": it runs whether or not the graph draws a review node for it, it auto-fixes or holds in `needs_review`, and its budget is a daemon-wide setting rather than a step the operator places (`docs/howto/automata-completion-review-autofix.md`). The held states are the same: the daemon raises them on its own initiative, every question is blocking, hold is always offered and always the default, and the daemon never invents states or options, so no edit of the workflow can delete or bypass the hold (`docs/howto/automaton-held-state-questions.md`).

So: the operator edits the order, membership, edges and stage labels of the work steps; the final gate — the completion review and the held-state stops that feed it — stays fixed on every Automaton. And because every control call is checked against the record's version, a stale one refused with a conflict, with the actor and reason recorded in the audit trail (`docs/howto/automation-chains.md`, section 6), the editable step inherits its own "change recorded" requirement from machinery that already exists.

### Q6 Versions, trust rules and a changed workflow

An edited workflow is a local override on a pinned pack version — not a new pack version, and not a fork.

Three precedents in the repository decide it. First, the direction of the interchange datawatch has already built: an orchestrator graph is turned into a draft Automation, and the source graph "is only read. It is never deleted or changed", so the source keeps working beside the derived object (`docs/howto/automation-chains.md`, section 8; the `import-graph` row in `docs/commands.md`). A new pack version would rewrite the source; a fork would split it into two; an override keeps the source intact and records the local difference. Second, the version and audit discipline that local workflow objects already carry is precisely the override model: edits are checked against the record's current `version`, a stale one is refused with a conflict, and every write names its actor and reason in the audit trail (`docs/howto/automation-chains.md`, section 6) — which is the note's step 2 row "the pack version pinned and a change recorded" applied to a pack. Third, the pack itself is still an archived, unmerged draft: the inventory records it as the largest item on the list, an archived Automaton 111 commits ahead with 152 files and 106 of them new versus main, with the rebase already decided (`docs/plans/historical-plans/2026-11-14-archived-unmerged-automata-inventory.md`), and this note reads its pack format and trust rules as the draft's open design questions. Since the trust rules do not exist yet, the editable step cannot assume that an edited workflow publishes anywhere: it may only record it locally.

So the interaction is: a pack version is the trusted, published definition the draft will give the operator — immutable once published, and the object the trust rules will reason about; an edited workflow is the operator's local state layered on one pinned version, version-checked and audit-trailed like any Automation edit, visible to the operator and exportable, but written back to nothing; and a fork is a separate published pack, which does not exist until the pack draft defines pack publishing at all. The display and editable steps stay entirely inside the override and never pretend to modify the pack itself.

### Q7 Run in LangGraph or only read there

The exported description only needs to be read in LangGraph — it does not need to run there.

The note's own Recommendation is the ground for this: adopting the LangGraph runtime would put a second engine beside Automations inside a Go daemon, and "that runs against the pack draft's own principle of 'no second plugin system'". Exporting a description does not adopt the runtime: it serializes the graph the operator already has — the templates or Automata as nodes, the three outcome edges plus the decision notes as the conditional edges, the held and review states as the interrupt points, the persisted record as the checkpoint (the Q3 mapping) — into a notation another system can parse. The import direction datawatch has already chosen shows the intended relationship: datawatch reads an external graph and turns it into a draft, refuses what it cannot express rather than guessing, and never pushes state back, "the source graph is only read. It is never deleted or changed" (`docs/howto/automation-chains.md`, section 8); and the deprecated orchestrator's own running operations were retired in favor of exactly that conversion path, with the orchestrator now standing only as a thing to read from (`docs/howto/automata-orchestrator.md`).

If the exported description were meant to run in LangGraph, it would be re-executing the same work in a second runtime — with its own state, its own gates and its own review — which is precisely the second-runtime trap the Recommendation rejects, and it would also break the Q5 answer, because the LangGraph side has no notion of the completion review's hold or of daemon-raised held questions (`docs/howto/automata-completion-review-autofix.md`, `docs/howto/automaton-held-state-questions.md`). So the interchange step is read-only by design: export is a faithful description the operator can carry elsewhere, and import, if offered at all, follows the import-graph precedent of converting to a local draft and refusing what a datawatch graph cannot express. The workflow runs here, in datawatch's own coordinator; LangGraph is where it can be seen.

## Recommended first step

The first step, on acceptance of this note, is the display-only pack workflow graph: each pack shows its workflow as a read-only graph in the app, with the templates as nodes and the one-edge-per-outcome links as the edges, read from the pack's manifest, and drawn with the existing Automation pipeline strip, which already renders chains, forks, joins and the branches that are live (`docs/howto/automation-chains.md`, section 7). The Q4 answer bounds what is added here: the display shows the planned graph and links to the Automation view for the live one, so this step builds read-only rendering of a pack's manifest — not a second live view, not controls, and no export. Nothing engine-side changes; the LangGraph-compatible description stays the later interchange step from the proposed table, and the recommendation against the Python runtime is untouched.

The cost of this step is deliberately small, and the reuse is the whole reason it is cheap. Reused, unchanged: the pipeline strip and its drawing of chains, forks and joins; the Automation graph model that defines which shapes a display can legally show; the vocabulary of the edit controls that the display will hint at without enabling; and the import-graph reading posture, in which the pack's manifest is a source that is only read and never touched (`docs/howto/automation-chains.md`, section 8; `docs/howto/automata-orchestrator.md` for the converter precedent). Not built yet, and explicitly out of this step's cost: the editable step — adding, removing, reordering and rewiring, with the version check and audit write the Q5 and Q6 answers say it will inherit; the interchange step — the LangGraph-compatible export and the optional import-to-draft; and everything the archived draft deliberately leaves open, its pack format, its trust rules and its placement in the app (`docs/plans/historical-plans/2026-11-14-archived-unmerged-automata-inventory.md`), none of which the display step needs. In rough-effort terms: one read-only view beside each pack that reads a manifest field and hands it to an existing renderer, plus the locale and docs wiring that rule already prescribes. It ships on its own: no display feature depends on the editable feature, the editable feature does not depend on the interchange feature, and accepting this note is the only precondition.

## Open design questions

These remain operator decisions rather than settled answers. The Answers above give the note's grounded recommendation for each, but the pack draft keeps its own design questions open, and no shipped mechanism exists yet to make these calls for the operator (`docs/plans/historical-plans/2026-11-14-archived-unmerged-automata-inventory.md`):

- **The node type of a bundle pack.** Is it a workflow whose nodes are packs, or a pack of packs? This decides what a node on the displayed graph is: work, or a holder of work. The note recommends the former, on the ground that the existing strip and the graph model draw only running things — members, forks, joins — and refuse a shape the model cannot express rather than invent a node kind (`docs/howto/automation-chains.md`, with the converter precedent in `docs/howto/automata-orchestrator.md`). But that recommendation answers the draft's open pack-format question; the operator may choose the other reading and the display follows suit (Q2).
- **What an edited workflow is.** When the operator changes a pack's workflow in the editable step, is the result a new pack version, a local override on a pinned version, or a fork — a separate published pack? The note recommends the local override, on the import-graph precedent that the source is only read and the version-check-plus-audit discipline that local edits already carry; but with trust rules still undefined, publishing semantics are the draft's and the operator's call, not the display step's (Q6).
- **Whether the trust rules permit editing at all.** The editable step assumes the operator may add, remove, reorder and rewire the work steps of a pack's workflow while the gates stay fixed. That assumption is safe only if the trust rules that the draft will define actually allow an operator to diverge from a trusted, published workflow — an allowance the note cannot grant in place of those rules (Q5).
- **Whether the LangGraph-compatible description is a read-only interchange.** The note holds that export is a faithful description to be read elsewhere and that import, if offered at all, follows the import-graph pattern of converting to a draft and refusing what cannot be expressed; but whether the description must additionally run in LangGraph — and what running there would change about the gates the daemon owns — is a design choice for the operator to make when step 3 is picked up (Q7, grounded in `docs/howto/automata-orchestrator.md`'s retired-runtime precedent).

## Where it belongs

Tie this to the pack design (`72bc0001`) rather than to a new Automaton: the pack format question and the workflow field are the same decision. When the pack draft is picked up, its open-questions list gains "does a pack carry a workflow graph, and in which schema".

## Not in scope

No change to how Automations run, no new execution engine, and no dependency on a Python runtime.

## Docs impact

This is a research note, so it changes no behavior and updates no howto, operations or glossary entry: the docs it cites — `docs/howto/automation-chains.md`, `docs/howto/automata-orchestrator.md`, `docs/howto/automata-standard-tasks.md`, `docs/howto/automaton-held-state-questions.md`, `docs/howto/automata-completion-review-autofix.md`, the `datawatch automation` section of `docs/commands.md`, `docs/plans/2026-10-14-automation-orchestration-design.md` and `docs/plans/historical-plans/2026-11-14-archived-unmerged-automata-inventory.md` — all existed and described shipped behavior at the time this note was written, and every cited path and section (including the pipeline strip in section 7 of the chains how-to) was checked against the tree. When a step from the table actually ships, each one writes its own howto, configuration and glossary entries and its locale/app-issue wiring through the general documentation checklist; none of that belongs in this research note.

## Parity surface

Research only: the note touches no surface, and every entry in the full parity surface set is an intentional exclusion:

| Surface | Status | Excluded because |
|---|---|---|
| REST | Excluded | The note changes nothing to serve; the export/import endpoints would arrive with step 3 and carry their own plan section and `fedCap` review at that time. |
| MCP | Excluded | No LLM-facing tool is consulted by the research; the display and editing surfaces are operator-facing and stay on the operator surfaces, as the existing Automation controls do. |
| CLI | Excluded | No command is added, renamed or rescoped by the note; the `import-graph` precedent it builds on is unchanged. |
| Comm channel | Excluded | No comm verb changes; the `automation` verbs cited are read as they already ship. |
| YAML/config | Excluded | The research adds no setting; the pack format — where any setting would live — is the archived draft's open design question, not this note's. |
| PWA | Excluded | The step-1 read-only graph view is a future PWA change tracked in the step table above, with its own locale and app-issue wiring when it ships; the note renders nothing today. |
| Android | Excluded, tracked from step 1 | No Android code lives in this repository; from the moment a pack workflow view ships, the Mobile-Parity Rule tracks it in `datawatch-app`. Not applicable to this research note. |
| iPhone | Excluded, tracked from step 1 | Same as Android: the Compose-Multiplatform app is the single mobile target, tracked per the Mobile-Parity Rule when step 1 ships. Not applicable to this research note. |
