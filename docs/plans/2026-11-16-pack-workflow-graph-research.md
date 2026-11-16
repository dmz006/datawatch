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

## Where it belongs

Tie this to the pack design (`72bc0001`) rather than to a new Automaton: the pack format question and the workflow field are the same decision. When the pack draft is picked up, its open-questions list gains "does a pack carry a workflow graph, and in which schema".

## Not in scope

No change to how Automations run, no new execution engine, and no dependency on a Python runtime.
