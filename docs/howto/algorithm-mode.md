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
| 6 | **Learn** | Reflects on what surprised us, what was wrong about the prior phases, what should we update. | When a learning is captured. |
| 7 | **Improve** | Names what to change next time. Updates rules / docs / procedures. | When the change is recorded. |

Each phase's captured output lives at
`~/.datawatch/algorithm/<session-id>/<phase>.md`.

## Base requirements

- An existing session (any backend). `datawatch sessions list` shows
  available IDs.
- (Optional) An Evals suite at `~/.datawatch/evals/<name>.yaml` if you
  want the Measure phase to auto-grade. See `evals.md`.

## Setup

No setup beyond having a session. Algorithm Mode toggles per-session,
on demand, against any running session.

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Start a session in your project workspace.
SID=$(datawatch sessions start --backend claude-code \
  --project-dir ~/work/foo \
  --task "Decide whether to migrate the cron jobs to k8s CronJobs" 2>&1 \
  | grep -oP 'session \K[a-z0-9-]+')
echo "session: $SID"

# 2. Enter Algorithm Mode.
datawatch algorithm start $SID
#  → algorithm started; phase=observe

# 3. Send messages in the session as normal until the LLM has
#    surveyed the situation. Then advance.
datawatch sessions input $SID "Survey what we have today. Cron jobs running on the worker VM, list them, what they do, who owns them, what depends on them."
sleep 30   # let the LLM think
datawatch algorithm advance $SID
#  → captured observe.md (4214 chars); phase=orient

# 4. Walk through Orient → Decide → Act → Measure → Learn → Improve
#    the same way. Each `advance` snapshots the current phase to
#    ~/.datawatch/algorithm/$SID/<phase>.md and unlocks the next.
datawatch sessions input $SID "Now lay out 2-3 options for the migration with their tradeoffs."
sleep 60
datawatch algorithm advance $SID    # → decide

# 5. (Optional) On Measure, auto-run an Evals suite.
datawatch sessions input $SID "Migrate the first cron job; show me the new manifest + a dry-run apply output."
sleep 120
datawatch algorithm advance $SID    # → measure
datawatch algorithm measure $SID --evals cronjob-correctness
#  → grader: passes 4/5 (80%) — meets capability threshold
datawatch algorithm advance $SID    # → learn

# 6. Inspect the trace any time.
ls ~/.datawatch/algorithm/$SID/
#  → observe.md  orient.md  decide.md  act.md  measure.md  learn.md  improve.md  trace.json
cat ~/.datawatch/algorithm/$SID/decide.md
```

### 4b. Happy path — PWA

1. PWA → Sessions → click into the session you want to drive through
   the harness.
2. In the session detail, click the **Algorithm** button in the
   toolbar (next to Stop / Restart).
3. The phase strip appears at the top of the session detail. The
   current phase (Observe) glows.
4. Use the input bar normally. Send your "survey what we have" message;
   wait for the LLM's reply.
5. When the response covers the phase, click the **Advance** button
   on the phase strip. A toast confirms what was captured. The strip
   re-colors — Observe checkmarks; Orient glows.
6. Repeat through all 7 phases. Each phase exposes:
   - **Edit** (pencil icon) — opens the captured markdown in an
     in-page editor so you can correct the LLM's record.
   - **Reset** — back to Observe; keeps captured history.
   - **Abort** — exit Algorithm Mode; session continues normally.
7. On the **Measure** phase, an **Evals** dropdown appears next to
   Advance. Pick a suite, click **Run**; the verdict folds into the
   captured `measure.md` and unlocks Advance.
8. After Improve, the strip shows all checkmarks. The full trace is
   linked at the bottom of the session detail; click **Open trace**
   to render the 7 captured phases as a single document.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Phase strip surfaces in the session detail, same buttons (Advance,
Edit, Measure with Evals dropdown, Reset, Abort). Long-hold a phase
chip to see its captured timestamp.

### 5b. REST

```sh
TOKEN=$(cat ~/.datawatch/token); BASE=https://localhost:8443

# Start
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"session_id":"ralfthewise-abcd"}' $BASE/api/algorithm/start

# Advance (current phase → next)
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"session_id":"ralfthewise-abcd"}' $BASE/api/algorithm/advance

# Status
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/algorithm/status/ralfthewise-abcd
#  → {"session_id":"...","phase":"orient","captured":["observe.md"]}

# Measure with eval
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"session_id":"ralfthewise-abcd","evals":"cronjob-correctness"}' \
  $BASE/api/algorithm/measure

# Reset / Abort
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"session_id":"ralfthewise-abcd"}' $BASE/api/algorithm/reset
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"session_id":"ralfthewise-abcd"}' $BASE/api/algorithm/abort
```

### 5c. MCP

Tools: `algorithm_start`, `algorithm_advance`, `algorithm_measure`,
`algorithm_status`, `algorithm_reset`, `algorithm_abort`.

Args shape: `{ "session_id": "<full-id>" }` for most. `algorithm_measure`
accepts an optional `{"evals": "<suite-name>"}` to fold an eval verdict
into the captured Measure output.

Useful when the LLM itself drives the harness — claude-code can call
`algorithm_advance` once it judges the current phase complete.

### 5d. Comm channel

From any configured chat:

| Verb | Example |
|---|---|
| `algorithm start <session>` | Enter the harness for that session. |
| `algorithm advance <session>` | Snapshot + move to next phase. |
| `algorithm status <session>` | Returns current phase + captured list. |
| `algorithm measure <session> evals=<name>` | Run an evals suite into Measure. |
| `algorithm abort <session>` | Exit harness; keep the session running. |

### 5e. YAML

Algorithm Mode itself has no config file; per-session captured output
lives at `~/.datawatch/algorithm/<session-id>/`:

```
observe.md
orient.md
decide.md
act.md
measure.md      ← contains eval verdict if Measure ran one
learn.md
improve.md
trace.json      ← state transitions + timestamps
```

Edit any captured `.md` directly to correct what the LLM recorded;
edits are picked up by `Open trace` immediately.

## Diagram

```
   Observe ──► Orient ──► Decide ──► Act ──► Measure ──► Learn ──► Improve
       ▲                                                                │
       └──────────────────── reset ─────────────────────────────────────┘
       │                                                                │
       └─── abort (exits algorithm mode at any phase, keeps session) ──┘
```

For richer architecture context (how Algorithm Mode interacts with the
session state engine + Evals + Council), see
`../architecture-overview.md` § per-session state machines.

## Common pitfalls

- **Skipping Learn / Improve.** Easy phases to drop. The daemon
  requires *something* captured before letting you advance through
  them — even *"no surprises this round; nothing to update"* counts.
- **Pre-deciding before Decide.** Operators often arrive at the
  session knowing what they want. Walk Observe + Orient honestly; if
  the answer doesn't change, you've at least documented the rejected
  alternatives.
- **Forgetting Measure.** Easy to skip on a phase the LLM produced
  confidently. Capture *something* — even *"output appears correct on
  visual inspection; no automated grader"* counts.
- **Using on chat-style work.** Algorithm Mode is overhead. For chat
  / quick lookups / "explain this code" the structure obstructs.
- **Concurrent advance from multiple operators.** Two operators
  hitting Advance simultaneously will race. The daemon serializes
  per-session; the second wins. Coordinate.

## Linked references

- See also: `evals.md` for Measure-phase grading
- See also: `council-mode.md` for invoking debate during Decide
- Architecture: `../architecture-overview.md` § per-session state machines
- Plan attribution: PAI Algorithm Mode in `../plan-attribution.md`

## Screenshots needed (operator weekend pass)

- [ ] PWA phase strip in session detail (Observe glowing)
- [ ] Phase strip after Advance (Observe checkmarked, Orient glowing)
- [ ] Captured `observe.md` example in the in-page editor
- [ ] Measure phase Evals dropdown + Run button
- [ ] Open trace view (full 7-phase render as single doc)
- [ ] CLI `datawatch algorithm status` output
