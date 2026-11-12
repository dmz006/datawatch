---
docs:
  index: true
  topics: [automation, decision-assistant, shadow, llm, escalation, audit]
exec_params:
  - {name: automation_id, required: true, description: "Automation ID to inspect"}
exec_steps:
  - tool: llm_list
    description: List the registered LLM backends the assistant can use
    args: {}
    read_only: true
  - tool: automation_get
    description: Read the Automation, including its decision_assistant setting and decision history
    args: {id: "{{params.automation_id}}"}
    read_only: true
---

# How-to: The Automation decision assistant

The **decision assistant** is an optional, off-by-default setting that lets an
Automation (or one of its members) hand a bounded judgment call to a configured
local model instead of a hardcoded default or an operator stop. It picks among
options the Automation already allows. It never widens them.

## 1. Base requirements

- A running daemon with `autonomous.enabled: true` and at least one Automation.
- A backend and model registered in the LLM registry (an Ollama-style local
  model is the intended fit: cheap, fast, and available when you are not).
  Check with `datawatch llm list`. No new backend concept is added.

## 2. Where it can decide: the four hook points

Each hook point is enabled on its own. A hook that is not listed never consults
the model.

| Hook | Question put to the assistant | Options it can pick |
|---|---|---|
| `empty_edge` | A member blocked or failed and has no edge for that outcome. Stop, as today? | pause the Automation, skip along an existing edge, or "needs the operator" |
| `skip_member` | A member is stuck. Should it be skipped? | skip along an existing edge, or keep waiting |
| `insert_member` | A corrective member would help. Insert one? | insert a named Automaton, or do nothing |
| `budget_breach` | The budget ceiling was hit. Pause or block? | `pause` or `block`, whichever the budget policy already allows |

It reads a bounded evidence packet (failure reason, verdict, progress, spend),
with every text field cut to a fixed size.

## 3. Turn it on

Set it on the whole Automation, or on one member with `--member`. A member's
setting wins over the Automation's. The flags replace the whole setting.

```bash
datawatch automation set-decision-assistant <id> \
  --backend ollama --model <model> \
  --hooks empty_edge,skip_member
```

The same call is available as the `automation_set_decision_assistant` MCP tool,
`POST /api/automations/{id}/set_decision_assistant`, and the chat verb
`automation set-decision-assistant <id> backend=<b> model=<m> hooks=<a,b>`.
It is allowed while the Automation is draft, active, paused or blocked.

Turn it off again with `--clear` (or an empty backend).

## 4. Choose a backend and model

Pick a pair from `datawatch llm list` and `datawatch llm models list <llm-name>`. The
setting is checked against the registry, so an unknown backend or model is
refused. No default model is chosen for you; which model judges well at these
decisions is a separate evaluation and is not decided here.

## 5. Start in shadow mode

`--mode` is `shadow` by default. In shadow mode the assistant is asked, and what
it **would have** done is recorded, but the Automation behaves exactly as it
did without it. Run in shadow until the recorded decisions match what you would
have chosen, then switch:

```bash
datawatch automation set-decision-assistant <id> \
  --backend ollama --model <model> --mode live \
  --hooks empty_edge,skip_member
```

In `live` mode a decision at or above the hook's threshold is acted on.

## 6. Thresholds and conservative defaults

Each hook has its own confidence threshold. Below it the assistant must
escalate rather than decide.

| Hook | Default threshold |
|---|---|
| `empty_edge` | 0.9 |
| `skip_member` | 0.9 |
| `insert_member` | 0.9 |
| `budget_breach` | 0.95 |

Override one with a number in (0, 1]; for example `--threshold skip_member=0.97`
(repeatable). A hook without an override keeps its default.

## 7. Read assistant decisions in history

Every assistant outcome is a normal decision entry in the Automation's history
and a normal audit record, with the actor `assistant:<model>`. It reads the same
as an operator decision. Each note carries the hook, the outcome, the option,
the confidence, a short evidence line and the model's reason.

- **Web:** open the Automation. The detail view shows the setting (backend,
  model, mode, hooks, thresholds), an edit control, and the history. A shadow
  entry reads "Would have: …"; a live entry reads "Decided: …", each with its
  confidence and evidence.
- **CLI / MCP / REST:** `datawatch automation get <id>` returns the setting as
  `decision_assistant` and the entries in `decisions`.

## 8. What the assistant can never do

The assistant only picks "which already-allowed option". A guard rejects these
whatever the model answers, and whatever configuration says:

- cancel in-flight work (`cancel_in_flight`);
- remove a member (`remove_member`);
- raise, remove or otherwise change a budget ceiling;
- take any option a guardrail profile or quality gate marked as needing human
  sign-off.

A member it inserts still needs its own normal approval before the coordinator
runs it. An insert of a draft Automaton is refused.

## 9. Escalation behaviour

Anything other than a confident, allowed answer **escalates**: the Automation
does what it did before the assistant existed (a stop and an alert on an empty
edge, a held member, no insert, the existing budget response). That covers:

- confidence below the hook's threshold;
- an answer that is not valid or names an option that was not offered;
- a guard rejection;
- a model timeout (60 seconds) or an unreachable model;
- the assistant being off, or the hook not enabled;
- the Automation changing while the model was thinking (a version conflict).

Escalations are recorded in history too, so you can see why the operator was
asked.

## Related

- [automation-tab.md](automation-tab.md): the Automation view in the web app
- [automation-chains.md](automation-chains.md): edges, forks, joins, budget
- [automation-queue-advance.md](automation-queue-advance.md): automatic advance
