# Pack workflow graphs: research first, display only, then editing

Status: research. No code is planned until the operator accepts this note. Raised by the operator on 2026-11-16.

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

| Step | What the operator gets | Reuses |
|---|---|---|
| 0. Research | This note answered and accepted | existing docs only |
| 1. Display only | Each pack shows its workflow as a read-only graph in the app | the Automation pipeline strip; the pack's manifest |
| 2. Editable | The operator can change a pack's workflow: add, remove or reorder steps and edit edges, with the pack version pinned and a change recorded | Automation insert, remove and edge controls; pack trust rules |
| 3. Interchange | Export and import of a pack workflow as a LangGraph-compatible description | the import-graph pattern |

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

### Q1

Which packs define a workflow: Automaton template packs, not standard task packs, and not a hypothetical bundle pack.

The unit of a workflow in this system is an Automaton: an Automation is a named chain of Automata, and each member is "a goal that is decomposed, approved, run and verified" (`docs/howto/automation-chains.md`). An Automaton template is exactly the reusable definition of such a unit — "a complete spec (type, stories, tasks, backend, effort, model, skills) stored in the template store" that an operator uses to start a whole Automaton (`docs/howto/automata-standard-tasks.md`). A standard task, by contrast, does not own any chain: it is "a built-in task definition the decomposer appends to a story", one task inside someone else's Automaton, and the built-in list is a fixed set (`docs/howto/automata-standard-tasks.md`). So a standard task pack defines a task catalogue, not a workflow; a pack of Automaton templates defines one, because each of its templates is one node of the existing graph model.

The "bundle pack" option is not in the repository yet. The row for the archived draft "Installable packs for standard tasks and Automaton templates" in the archived-unmerged inventory records it as 111 commits ahead with 152 files, plus or minus about ten thousand lines, the largest item on the list, with the pack format deliberately left open (`docs/plans/2026-11-14-archived-unmerged-automata-inventory.md`). Nothing that ships today can decide for it. The only place where several things are composed into one workflow today is the Automation chain and the earlier orchestrator DAG (`docs/howto/automata-orchestrator.md`), and both compose Automata — never standard tasks.

Recommendation for the operator: a pack carries a workflow graph when it is a pack of Automaton templates. A pack that also holds standard tasks keeps them as a list on the side, since they cannot be graph nodes; if the operator wants one object that bundles both, that object is the draft's bundle entry, and the workflow field on it describes the templates it contains. This keeps the answer inside what already exists and leaves the draft's open question — the pack format — to be decided by whatever work picks the draft up, as this note's Recommendation already asks.

### Q2

What a bundle pack is: a workflow whose nodes are packs, not a pack of packs.

That is the only reading that the existing graph model supports. The Automation data model is a per-outcome edge graph, and the design that chose it explicitly rejected a flat ordered list because "a flat ordinal cannot express 'if X is blocked, run the fix-up Automaton Y, otherwise continue with Z'" (`docs/plans/2026-10-14-automation-orchestration-design.md`). Every node in that graph is a thing that runs — a member Automaton, a fork, a join, or a nested Automation (`docs/howto/automation-chains.md`) — and the importer from the older orchestrator graphs maps fan-out and fan-in onto paired fork and join and refuses a shape an Automation cannot express rather than inventing a new node kind (`docs/commands.md`, and `docs/howto/automation-chains.md` section 8).

A "pack of packs" would need a node kind that is a collection of work rather than work itself, and no such node exists anywhere in the model: standard tasks are added to a story by the decomposer and are not nodes of any chain (`docs/howto/automata-standard-tasks.md`). So a bundle pack should be defined as a named workflow — a set of Automaton template packs as the nodes, plus the edges between them, using the same one-edge-per-outcome shape — and the operator's phrasing that "each [pack] defined a workflow" already says this: every node is a pack that defines a step, and the bundle is the graph over those steps.

Two follow-ups that fall out for step 1 (display only): the graph to display is the bundle's — templates as nodes, outcome edges as the links — and the display can reuse the Automation pipeline strip, which already draws chains, forks, joins and the live branches (`docs/howto/automation-chains.md` section 7). Because the draft has the pack format as an open design question (`docs/plans/2026-11-14-archived-unmerged-automata-inventory.md`), this definition is offered as the answer to that question, not as a settled fact.

### Q3

How LangGraph's concepts map to this system, mapping by mapping:

- **State schema to Automaton and task states.** LangGraph's channel-and-reducer state is the shared object every node reads and writes. Here the shared object is the Automation record itself: its status lifecycle (draft, active, paused, completed, blocked, cancelled), its member statuses, and the per-member outcome it observed, `docs/howto/automation-chains.md`, with the same pattern in the older orchestrator's state list (draft, planning, needs_review, approved, running, paused, completed, failed) in `docs/howto/automata-orchestrator.md`. The design record lists the statuses a member can reach and the edge each one triggers (`docs/plans/2026-10-14-automation-orchestration-design.md`).
- **Nodes to templates or Automata.** A node here is an Automaton — the member's reference, with an Automaton template as the reusable definition of one (`docs/howto/automata-standard-tasks.md`). The import converter reads this directly: an orchestrator graph's Automaton nodes become chain members, and a graph with no Automaton nodes is refused (`docs/commands.md`).
- **Conditional edges to the three edges plus decisions.** LangGraph branches on node output; an Automation branch is explicit per outcome: `on_complete`, `on_blocked` and `on_failed` on every member, an empty edge meaning "stop here" (`docs/howto/automation-chains.md`). Decisions are the recorded reasons attached to an Automation — the same shape carried by the members' records in the design record — and the design chose the per-outcome edge graph over alternatives for exactly this reason (`docs/plans/2026-10-14-automation-orchestration-design.md`).
- **Interrupts and human review to held questions and review states.** The operator gates in an Automation are the approval gate on each member, blocked members that need operator action, held inserts that wait for approval, and review-type states that are non-terminal and simply wait (`docs/howto/automation-chains.md`, `docs/plans/2026-10-14-automation-orchestration-design.md`). The orchestrator's `needs_review` and the guardrail-refusal block are the same pattern in the older model (`docs/howto/automata-orchestrator.md`).
- **Checkpointing to the persisted records.** Every Automation write is versioned, audited, and recoverable: the design persists the records so a restart can resume the chain, an edit conflict carries its own error, and a member that is already done is immutable history (`docs/plans/2026-10-14-automation-orchestration-design.md`). The operator-visible controls — skip, insert, remove, reorder edges, pause, resume, cancel — are all edits onto that same persisted record rather than a runtime to attach to (`docs/howto/automation-chains.md`).

The consequence for the interchange step: an export to a LangGraph-compatible description would name the templates (or Automata) as nodes, the three outcome edges plus the decision notes as conditional edges, the review and held states as interrupt points, and the persisted Automation record as the checkpoint — which is the same content the operator sees in the strip, already in this system's own vocabulary.

## Where it belongs

Tie this to the pack design (`72bc0001`) rather than to a new Automaton: the pack format question and the workflow field are the same decision. When the pack draft is picked up, its open-questions list gains "does a pack carry a workflow graph, and in which schema".

## Not in scope

No change to how Automations run, no new execution engine, and no dependency on a Python runtime.
