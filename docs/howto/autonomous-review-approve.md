---
docs:
  index: true
  topics: [autonomous, prd, approve]
exec_params:
  - {name: prd_id, required: true, description: "Automaton ID (8-char hex)"}
exec_steps:
  - tool: autonomous_prd_get
    description: Read the Automaton record + current plan
    args: {id: "{{params.prd_id}}"}
    read_only: true
  - tool: autonomous_prd_approve
    description: Approve the plan for execution
    args: {id: "{{params.prd_id}}"}
    read_only: false
  - tool: autonomous_prd_get
    description: Confirm the status moved to approved
    args: {id: "{{params.prd_id}}"}
    read_only: true
---
# How-to: Review and approve an Automaton

[`autonomous-planning.md`](autonomous-planning.md) showed the spawn →
plan → run loop. This howto zooms into the review gate — what to
look at when an Automaton lands in `needs_review`, how to approve / reject /
request-revision per-story or whole-Automaton, and the audit trail you get
back.

## What it is

After `plan`, an Automaton sits in `needs_review` until the operator
acts. Three actions:

- **Approve** — promotes to `approved`, ready to run. Per-story
  approve is finer-grained: only approved stories run when you hit
  Run.
- **Reject** — terminal; the Automaton won't run. Use for "this isn't
  what I meant; throw it away".
- **Request revision** — bounces back to the LLM for re-planning
  with operator notes. Status returns to `planning`.

Every action is recorded in the Decisions tab + audit log.

## Base requirements

- An Automaton in `needs_review` (i.e. you've already run `plan`). See
  [`autonomous-planning.md`](autonomous-planning.md) for the full spawn →
  plan cycle that produces an Automaton in this state.
- Operator role (review actions are gated).

> **Pre-conditions**: The "Request revision" action bounces the Automaton
> back to `planning`, which triggers an LLM re-planning call. Ensure the
> LLM backend used to create the Automaton is still available before
> requesting a revision. See [`llm-registry.md`](llm-registry.md) for
> LLM backend setup and status checks.

## Setup

No setup beyond having an Automaton to review.

## Two happy paths

### 4a. Happy path — CLI

```sh
PRD_ID=abc123

# 1. Confirm status.
datawatch autonomous get $PRD_ID | jq .status
#  → "needs_review"

# 2. Skim the plan.
datawatch autonomous get $PRD_ID | jq '.stories[] | {title, n_tasks: (.tasks|length)}'
#  → {"title":"Audit current auth flow","n_tasks":3}
#  → {"title":"Implement JWT issuance","n_tasks":4}
#  → ...

# 3. Read a specific story / task in full.
datawatch autonomous get $PRD_ID --story 0 --task 1

# 4. Approve all stories + the Automaton.
datawatch autonomous approve $PRD_ID
#  → approved; ready to run

# 5. Or approve per-story (finer control).
datawatch autonomous approve $PRD_ID --story 0
datawatch autonomous approve $PRD_ID --story 1
datawatch autonomous reject  $PRD_ID --story 2 --reason "Out of scope; defer to a separate Automaton"

# 6. Or request revision.
datawatch autonomous request-revision $PRD_ID \
  --notes "Story 2 is too broad — split into 'Migrate sessions' + 'Migrate cookies' as two separate stories"
#  → status: planning; LLM re-planning

# 7. Read the audit trail.
datawatch autonomous decisions $PRD_ID
#  → 2026-05-06T14:23:01Z  approve_story  story=0  actor=operator
#    2026-05-06T14:23:18Z  approve_story  story=1  actor=operator
#    2026-05-06T14:23:30Z  reject_story   story=2  reason="Out of scope..."
#    2026-05-06T14:24:02Z  approve_prd    actor=operator
```

### 4b. Happy path — PWA

1. Automata list → click the Automaton with `needs_review` badge.
2. Detail view opens on the **Overview** tab. Read the goal +
   summary; persistent toolbar at the top exposes Edit Spec, Settings,
   Request Revision, Clone to Template, Delete.
3. Click the **Stories** tab. Each story renders with:
   - Title + status badge.
   - Per-task list with `Edit / LLM / Files` per task.
   - **Approve / Reject** buttons per story.
   - Click into a story to see its full description, evals_suite, and
     the rendered task spec.
4. Approve story-by-story OR click the **Approve all** button at
   the top of the Stories tab.
5. To request a re-plan: toolbar → **Request Revision**.
   Modal asks for notes (free text); on submit, status flips back to
   `planning`.
6. To reject the whole Automaton: toolbar → **Delete** (or set status to
   `rejected` from the Settings modal). Rejected Automata persist for
   audit; archive when ready.
7. **Decisions** tab shows every action with timestamp, actor, and
   reason. Click any row to expand the raw `details` payload.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same 4-tab detail view + per-story approve. Toolbar buttons render
as a bottom-sheet on narrow viewports.

### 5b. REST

```sh
# Approve whole Automaton.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/autonomous/prds/$PRD_ID/approve

# Approve a specific story.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/autonomous/prds/$PRD_ID/stories/$STORY_ID/approve

# Reject a story with reason.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"reason":"Out of scope"}' \
  $BASE/api/autonomous/prds/$PRD_ID/stories/$STORY_ID/reject

# Request revision (whole Automaton).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"notes":"Story 2 too broad — split"}' \
  $BASE/api/autonomous/prds/$PRD_ID/request-revision

# Decisions audit.
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/autonomous/prds/$PRD_ID/decisions
```

### 5c. MCP

Tools: `prd_approve`, `prd_reject`, `prd_request_revision`,
`story_approve`, `story_reject`, `prd_decisions`.

Use case: a top-level autonomous coordinator that spawns sub-Automata and
auto-approves the ones meeting a confidence threshold (with operator
escalation for the rest).

### 5d. Comm channel

```
You: review abc123
Bot: Automaton abc123 (auth-refactor) — needs_review
     Stories:
       0  Audit current auth flow         (3 tasks)
       1  Implement JWT issuance          (4 tasks)
       2  Migrate session middleware      (5 tasks)
       3  Update tests + docs             (3 tasks)

You: approve abc123 stories=0,1,3
Bot: approved 3/4 stories; story 2 still pending

You: revise abc123 notes="Story 2 too broad — split"
Bot: Automaton abc123 → planning; re-decomposing
```

### 5e. YAML

Per-Automaton state lives in the Automaton YAML at
`~/.datawatch/autonomous/prds/<id>.yaml`:

```yaml
status: needs_review
stories:
  - title: Audit current auth flow
    status: needs_review                  # → approved / rejected per-story
    approval_notes: ""
    tasks:
      - title: Map all uses of CookieAuthMiddleware
        status: pending
      ...
```

Don't hand-edit `status` (race-prone); use the API. Operators CAN
hand-edit story / task content between plan + approve to refine the
spec — the daemon picks up the changes on next run.

## Diagram

```
                        plan (/decompose alias)
   draft ────────────────────────────────► planning
                                       │
                                       ▼ LLM done
                              ┌──► needs_review
                              │        │
                              │        │  approve (per-story or all)
                              │        │  reject (per-story or all)
                              │        │  request-revision
                              │        │
              request-revision│        ▼
                              │     approved
                              │        │
                              │        │  run
                              │        ▼
                              └─── running
```

## Common pitfalls

- **Approving without reading.** Cheap; common. The Stories tab is
  there for a reason. Even a 30-second skim catches most "this is
  not what I meant" moments.
- **Per-story approve forgotten.** Run only fires approved stories;
  unapproved ones stay `needs_review` and the Automaton continues without
  them. May or may not be what you want.
- **Reject vs Cancel.** Reject = "this Automaton shouldn't exist"
  (terminal). Cancel = "stop this run; the Automaton itself is fine"
  (Automaton goes to `cancelled`; can be re-run).
- **Lost decisions trail.** Decisions are append-only and persist
  with the Automaton (not deleted on archive). To prune, hard-delete the
  Automaton.

## Linked references

- See also: [`autonomous-planning.md`](autonomous-planning.md) — full lifecycle.
- See also: [`automata-orchestrator.md`](automata-orchestrator.md) — composing Automata into DAGs.
- See also: [`evals.md`](evals.md) — per-story graded verification.
- Architecture: `../architecture-overview.md` § Autonomous executor.

## Screenshots needed (operator weekend pass)

- [ ] Stories tab with per-story Approve / Reject buttons
- [ ] Per-story expanded view with task list
- [ ] Request Revision modal
- [ ] Decisions tab with expandable `details` rows
- [ ] CLI `datawatch autonomous decisions` output

---

## Story approval in the Status tab

Each story in the Automata detail view shows a "→ Session" link and a
"● Status" quick-link. Click "● Status" to jump directly to the worker
session's **Status** tab where you can see the live task tree, guardrail
verdicts, and the last 5 hook events before any failure — all the
information you need to make an informed Approve or Reject decision.

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/autonomous-planning](autonomous-planning.md)
- [howto/automata-orchestrator](automata-orchestrator.md)
- [howto/sessions-deep-dive](sessions-deep-dive.md)
- [api/autonomous](../api/autonomous.md)
