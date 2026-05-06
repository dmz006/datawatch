# How-to: Review and approve an autonomous PRD

[`autonomous-planning.md`](autonomous-planning.md) showed the spawn →
decompose → run loop. This howto zooms into the review gate — what to
look at when a PRD lands in `needs_review`, how to approve / reject /
request-revision per-story or whole-PRD, and the audit trail you get
back.

## What it is

After `decompose`, a PRD sits in `needs_review` until the operator
acts. Three actions:

- **Approve** — promotes to `approved`, ready to run. Per-story
  approve is finer-grained: only approved stories run when you hit
  Run.
- **Reject** — terminal; the PRD won't run. Use for "this isn't
  what I meant; throw it away".
- **Request revision** — bounces back to the LLM for re-decomposition
  with operator notes. Status returns to `planning`.

Every action is recorded in the Decisions tab + audit log.

## Base requirements

- A PRD in `needs_review` (i.e. you've already run `decompose`).
- Operator role (review actions are gated).

## Setup

No setup beyond having a PRD to review.

## Two happy paths

### 4a. Happy path — CLI

```sh
PRD_ID=abc123

# 1. Confirm status.
datawatch autonomous get $PRD_ID | jq .status
#  → "needs_review"

# 2. Skim the decomposition.
datawatch autonomous get $PRD_ID | jq '.stories[] | {title, n_tasks: (.tasks|length)}'
#  → {"title":"Audit current auth flow","n_tasks":3}
#  → {"title":"Implement JWT issuance","n_tasks":4}
#  → ...

# 3. Read a specific story / task in full.
datawatch autonomous get $PRD_ID --story 0 --task 1

# 4. Approve all stories + the PRD.
datawatch autonomous approve $PRD_ID
#  → approved; ready to run

# 5. Or approve per-story (finer control).
datawatch autonomous approve $PRD_ID --story 0
datawatch autonomous approve $PRD_ID --story 1
datawatch autonomous reject  $PRD_ID --story 2 --reason "Out of scope; defer to a separate PRD"

# 6. Or request revision.
datawatch autonomous request-revision $PRD_ID \
  --notes "Story 2 is too broad — split into 'Migrate sessions' + 'Migrate cookies' as two separate stories"
#  → status: planning; LLM re-decomposing

# 7. Read the audit trail.
datawatch autonomous decisions $PRD_ID
#  → 2026-05-06T14:23:01Z  approve_story  story=0  actor=operator
#    2026-05-06T14:23:18Z  approve_story  story=1  actor=operator
#    2026-05-06T14:23:30Z  reject_story   story=2  reason="Out of scope..."
#    2026-05-06T14:24:02Z  approve_prd    actor=operator
```

### 4b. Happy path — PWA

1. Automata list → click the PRD with `needs_review` badge.
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
5. To request a re-decomposition: toolbar → **Request Revision**.
   Modal asks for notes (free text); on submit, status flips back to
   `planning`.
6. To reject the whole PRD: toolbar → **Delete** (or set status to
   `rejected` from the Settings modal). Rejected PRDs persist for
   audit; archive when ready.
7. **Decisions** tab shows every action with timestamp, actor, and
   reason. Click any row to expand the raw `details` payload.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same 4-tab detail view + per-story approve. Toolbar buttons render
as a bottom-sheet on narrow viewports.

### 5b. REST

```sh
# Approve whole PRD.
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

# Request revision (whole PRD).
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

Use case: a top-level autonomous coordinator that spawns sub-PRDs and
auto-approves the ones meeting a confidence threshold (with operator
escalation for the rest).

### 5d. Comm channel

```
You: review abc123
Bot: PRD abc123 (auth-refactor) — needs_review
     Stories:
       0  Audit current auth flow         (3 tasks)
       1  Implement JWT issuance          (4 tasks)
       2  Migrate session middleware      (5 tasks)
       3  Update tests + docs             (3 tasks)

You: approve abc123 stories=0,1,3
Bot: approved 3/4 stories; story 2 still pending

You: revise abc123 notes="Story 2 too broad — split"
Bot: PRD abc123 → planning; re-decomposing
```

### 5e. YAML

Per-PRD state lives in the PRD YAML at
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
hand-edit story / task content between decompose + approve to refine
the spec — the daemon picks up the changes on next run.

## Diagram

```
                        decompose
   draft ────────────────────────► planning
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
  unapproved ones stay `needs_review` and the PRD continues without
  them. May or may not be what you want.
- **Reject vs Cancel.** Reject = "this PRD shouldn't exist"
  (terminal). Cancel = "stop this run; the PRD itself is fine"
  (PRD goes to `cancelled`; can be re-run).
- **Lost decisions trail.** Decisions are append-only and persist
  with the PRD (not deleted on archive). To prune, hard-delete the
  PRD.

## Linked references

- See also: [`autonomous-planning.md`](autonomous-planning.md) — full lifecycle.
- See also: [`prd-dag-orchestrator.md`](prd-dag-orchestrator.md) — composing PRDs into DAGs.
- See also: [`evals.md`](evals.md) — per-story graded verification.
- Architecture: `../architecture-overview.md` § Autonomous executor.

## Screenshots needed (operator weekend pass)

- [ ] Stories tab with per-story Approve / Reject buttons
- [ ] Per-story expanded view with task list
- [ ] Request Revision modal
- [ ] Decisions tab with expandable `details` rows
- [ ] CLI `datawatch autonomous decisions` output
