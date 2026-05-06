# How-to: Algorithm Mode — 7-phase structured thinking

Drive a session through Observe → Orient → Decide → Act → Measure →
Learn → Improve, with operator-gated advance and per-phase output
capture. The reasoning trace is captured alongside the result so you
have a defensible record of *why* a decision came out the way it did.

## What it is

A per-session state machine that overlays the standard session
lifecycle. The session itself stays the session you started; Algorithm
Mode just gates the phases of the conversation and snapshots output at
each gate.

The 7 phases:

| # | Phase | What the LLM is doing | What you advance on |
|---|---|---|---|
| 1 | **Observe** | Surveys the situation. Surfaces facts, constraints, prior art. | When the LLM has stated what it knows + what it doesn't. |
| 2 | **Orient** | Maps options. Names the trade-offs. | When 2+ candidate approaches are compared. |
| 3 | **Decide** | Picks an approach. States the decision + rationale. | When the decision is unambiguous. |
| 4 | **Act** | Does the work. Produces the artifact / change / answer. | When the artifact is complete. |
| 5 | **Measure** | Grades the output. Optionally runs an Evals suite. | When the grade is recorded. |
| 6 | **Learn** | Reflects: what surprised us, what was wrong about the prior phases, what should we update. | When a learning is captured. |
| 7 | **Improve** | Names what to change next time. Updates rules / docs / procedures. | When the change is recorded. |

Each phase's captured output lives at
`~/.datawatch/algorithm/<session-id>/<phase>.md`.

## When to use it

- Decisions where the trace matters as much as the answer — design
  proposals, RCAs, large refactors, security reviews.
- Work that benefits from forced reflection — Learn + Improve are the
  phases most operators skip when working ad-hoc.
- When you want gradeable output — Measure → Evals lets you prove
  whether the output meets a rubric.

**Don't use it for:** simple "do this" requests, quick lookups, chat.
The overhead isn't worth it.

## 1. Start a session in your project

Either via PWA (+ FAB) or:

```sh
datawatch sessions start --backend claude-code --project-dir ~/work/foo \
  --task "Decide whether to migrate the cron jobs to k8s CronJobs"
```

Note the session ID it returns.

## 2. Enter Algorithm Mode

PWA: open the session detail; click **Algorithm Mode → Start** in the
toolbar.

CLI:

```sh
datawatch algorithm start <session-id>
```

The PWA shows a color-coded phase strip at the top of the session
detail. Current phase has a glow; completed phases are checkmarks;
upcoming phases are dim.

## 3. Work through phases

Send messages in the session as you would normally. The LLM is told
which phase it's in (injected into its system prompt) and is steered
toward that phase's purpose.

**Advance** when the phase is done:

- PWA: click **Advance** in the phase strip.
- CLI: `datawatch algorithm advance <session-id>`

The captured output for the current phase is written to
`~/.datawatch/algorithm/<session-id>/<phase>.md` and the next phase
becomes active.

## 4. Edit a phase mid-flow

Sometimes the LLM's record of a phase is incomplete or wrong. To
correct it without losing the rest:

```sh
datawatch algorithm edit <session-id>
```

Opens `$EDITOR` on the current phase's markdown. Save → the edited
version is what's captured + carried forward.

PWA: same — click the phase strip's pencil icon.

## 5. Wire Measure to an Evals suite

The Measure phase can auto-run an Evals suite (see [`evals.md`](evals.md))
to grade the Act-phase output before you advance.

Pre-create the suite at `~/.datawatch/evals/<name>.yaml`. Then in the
Measure phase:

```sh
datawatch algorithm measure <session-id> --evals <suite-name>
```

The verdict (pass / fail / score) is folded into the captured output
for Measure. If `regression` mode and below threshold, the daemon
warns before letting you advance.

PWA: Measure phase exposes an **Evals** dropdown listing your
configured suites; pick one and click Run.

## 6. Reset / abort

```sh
datawatch algorithm reset <session-id>     # back to Observe; keeps captured history
datawatch algorithm abort <session-id>     # exits Algorithm Mode; session continues normally
```

`reset` is the recover-from-bad-Observe move. `abort` is the "this
isn't worth the structure" move.

## 7. Read the captured trace

The whole flow is in
`~/.datawatch/algorithm/<session-id>/`:

```
observe.md
orient.md
decide.md
act.md
measure.md      (with eval verdict if Measure ran one)
learn.md
improve.md
trace.json      (state transitions + timestamps)
```

The full sequence makes a clean audit trail — drop into a PR description,
attach to an incident postmortem, share with a stakeholder.

## CLI reference

```sh
datawatch algorithm list                            # which sessions are in algorithm mode
datawatch algorithm start <session-id>
datawatch algorithm advance <session-id>
datawatch algorithm edit <session-id> [--phase observe|orient|...]
datawatch algorithm measure <session-id> [--evals <suite>]
datawatch algorithm reset <session-id>
datawatch algorithm abort <session-id>
datawatch algorithm status <session-id>             # current phase + age
```

## REST / MCP

- `POST /api/algorithm/start {session_id}` → start
- `POST /api/algorithm/advance {session_id}` → advance
- `POST /api/algorithm/measure {session_id, evals?}` → measure
- `GET /api/algorithm/status/{session_id}` → current phase
- MCP: `algorithm_start`, `algorithm_advance`, `algorithm_measure`, etc.

## Common pitfalls

- **Skipping Learn / Improve.** These are the phases people drop. The
  daemon won't let you advance through them without something captured;
  if you hit a phase where you genuinely have nothing, type *"no
  surprises this time; nothing to update"* — that's a valid capture.
- **Pre-deciding before Decide.** Operators often arrive at the session
  already knowing what they want to do. Force yourself through Observe
  + Orient honestly; if the answer doesn't change, you've still
  documented the alternatives you rejected.
- **Forgetting Measure.** Easy to skip Measure on a phase the LLM
  produced confidently. Capture *something* — even *"output appears
  correct on visual inspection; no automated grader"* counts.
- **Using on chat-style work.** Algorithm Mode is overhead. For chat
  / quick lookups / "explain this code" the structure obstructs.

## Linked references

- Plan attribution: PAI Algorithm Mode in `plan-attribution.md`
- See also: `evals.md` for Measure-phase grading
- Architecture: `architecture-overview.md` § per-session state machines
- API: `/api/algorithm/*` (Swagger UI under `/api/docs`)

## All channels reference

| Channel | How |
|---|---|
| **PWA** | Phase strip in session detail (start, advance, edit, measure, reset, abort buttons). |
| **Mobile** | Phase strip in session detail; same buttons. |
| **REST** | `POST /api/algorithm/{start,advance,measure,abort,reset}`, `GET /api/algorithm/status/{id}`. |
| **MCP** | `algorithm_start`, `algorithm_advance`, `algorithm_measure`, `algorithm_status`, etc. |
| **CLI** | `datawatch algorithm {start,advance,edit,abort,reset,measure,status} <session-id>`. |
| **Comm** | `algorithm advance <id>`, `algorithm measure <id> evals=<suite>` from any chat channel. |
| **YAML** | Algorithm doesn't have YAML config — phase output lives at `~/.datawatch/algorithm/<session-id>/`. |

## Screenshots needed (operator weekend pass)

- [ ] Color-coded phase strip in session detail (current phase glowing)
- [ ] Phase strip after Advance moves to the next phase (checkmark)
- [ ] Captured `observe.md` example
- [ ] Measure phase Evals dropdown
- [ ] CLI `datawatch algorithm status` output

## Diagram

A simple state diagram for the 7 phases:

```
   Observe → Orient → Decide → Act → Measure → Learn → Improve
       ↑                                                 │
       └─────────────────── reset ──────────────────────┘
       │                                                 │
       └─── abort (exits algorithm mode at any phase) ──┘
```

For richer diagrams of how Algorithm Mode interacts with the session
state machine + Evals integration, see `architecture-overview.md` § per-session state machines.
