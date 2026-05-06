# How-to: Autonomous planning

Describe a feature in plain English; datawatch decomposes it into a
hierarchical PRD (Product Requirements Document) of stories + tasks,
queues it for review, then runs the approved tasks under verification.
This howto walks the spawn → decompose → review → run loop.

## What it is

An Automaton is a structured PRD spec that the daemon's autonomous
executor walks through: each story has tasks; each task spawns a
session against a configured backend; per-story Evals + scan
guardrails verify output before advancing. Operator-gated approval
(per-story or whole-PRD) keeps the human in the loop.

States: `draft` → `planning` → `needs_review` → `approved` → `running`
→ `blocked` / `completed` / `failed` / `rejected` / `cancelled` /
`archived`.

## Base requirements

- `datawatch start` — daemon up.
- A configured LLM backend (claude-code, opencode-acp, ollama, etc.).
- For containerised execution: a Cluster Profile. For local execution
  the daemon's host runs each task directly.
- (Optional) An Evals suite per story for graded verification.
- (Optional) A Project Profile pre-baked with workspace + skills.

## Setup

No setup beyond a backend + (optional) profile.

```sh
# Confirm autonomous subsystem is on (default: yes).
datawatch config get autonomous.enabled
#  → true
```

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Submit a free-form spec — daemon decomposes it.
PRD_ID=$(datawatch autonomous create \
  --name "auth-refactor" \
  --title "Refactor auth to use JWT-only sessions" \
  --goal "Replace cookie+CSRF with JWT-only; deprecate the cookie middleware" \
  --backend claude-code \
  --profile prod-audit 2>&1 | grep -oP 'id=\K[0-9a-f]+')

# 2. Trigger decomposition (LLM-driven; async).
datawatch autonomous decompose $PRD_ID
sleep 60
datawatch autonomous get $PRD_ID
#  → status: needs_review
#    stories:
#      - "Audit current auth flow" (3 tasks)
#      - "Implement JWT issuance" (4 tasks)
#      - "Migrate session middleware" (5 tasks)
#      - "Update tests + docs" (3 tasks)

# 3. Review (see autonomous-review-approve.md for the full review flow).
#    Quick approve all if it looks good:
datawatch autonomous approve $PRD_ID

# 4. Run.
datawatch autonomous run $PRD_ID

# 5. Watch progress.
watch -n 5 "datawatch autonomous get $PRD_ID | jq '{status, stories: .stories|map({title, status, completed_tasks})}'"

# 6. Inspect a specific story / task on completion.
datawatch autonomous get $PRD_ID --story 1 --task 2

# 7. Cancel mid-run if needed.
datawatch autonomous cancel $PRD_ID

# 8. Clean up after archival.
datawatch autonomous delete $PRD_ID
```

### 4b. Happy path — PWA

1. Bottom nav → **Automata** tab.
2. Click the **⚡** FAB → wizard:
   - Top strip: **Start from template** (if you have any saved).
   - Intent: free-text spec.
   - Inferred: type / workspace (auto-derived from intent).
   - Execution: backend / effort.
   - Advanced (collapsed): guided mode, scan, rules, story-approval.
   - **Start**.
3. The PRD appears in the list with status `planning`. Click it to
   open the detail view.
4. Detail view 4-tab layout:
   - **Overview** — spec + status + persistent toolbar (Edit Spec,
     Settings, Request Revision, Clone to Template, Delete).
   - **Stories** — per-story state + Edit / Profile / Files /
     Approve / Reject. Each task under a story exposes Edit / LLM /
     Files.
   - **Decisions** — every state-changing event with expandable
     `details` payload.
   - **Scan** — Run Scan + history.
5. When `status: needs_review`, click **Approve** in the toolbar (or
   per-story approve in the Stories tab — see
   [`autonomous-review-approve.md`](autonomous-review-approve.md)).
6. Click **Run** in the toolbar. The status transitions to `running`;
   tasks spawn sessions; the per-story progress bar fills.
7. To cancel mid-run: **Cancel** in the toolbar. To archive when
   complete: **Archive**.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same Automata tab + ⚡ FAB + 4-tab detail view. Multi-select bar with
state-aware actions matches the PWA.

### 5b. REST

```sh
# Create.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"auth-refactor","title":"...","goal":"...","backend":"claude-code"}' \
  $BASE/api/autonomous/prds

# Decompose.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/autonomous/prds/$PRD_ID/decompose

# Get.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/autonomous/prds/$PRD_ID

# Approve / Run / Cancel / Archive.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/autonomous/prds/$PRD_ID/approve
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/autonomous/prds/$PRD_ID/run
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/autonomous/prds/$PRD_ID/cancel
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/autonomous/prds/$PRD_ID/archive

# Hard delete.
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/autonomous/prds/$PRD_ID?hard=true"
```

### 5c. MCP

Tools: `prd_create`, `prd_get`, `prd_list`, `prd_decompose`,
`prd_approve`, `prd_run`, `prd_cancel`, `prd_archive`, `prd_delete`.

Useful when an autonomous LLM coordinator spawns work for itself —
calls `prd_create` + `prd_decompose` to plan, then iterates the
result.

### 5d. Comm channel

```
You: automaton: Refactor auth to use JWT-only sessions
Bot: started PRD abc123; decomposing...
Bot (~60s later): PRD abc123 needs_review
       4 stories / 15 tasks
       Reply "approve abc123" to run as-is
       Reply "review abc123" for the per-story review
You: approve abc123
Bot: PRD abc123 running; tasks 0/15
Bot (per-task completion): task 1/15 done — "Audit current auth flow"
...
```

### 5e. YAML

PRD spec lives at `~/.datawatch/autonomous/prds/<id>.yaml`:

```yaml
id: abc123
name: auth-refactor
title: Refactor auth to use JWT-only sessions
goal: Replace cookie+CSRF with JWT-only; deprecate the cookie middleware
backend: claude-code
profile: prod-audit
status: needs_review
stories:
  - title: Audit current auth flow
    status: needs_review
    evals_suite: ""
    tasks:
      - title: Map all uses of CookieAuthMiddleware
      - title: Document the existing CSRF token flow
      - title: List endpoints currently relying on Set-Cookie
  - title: Implement JWT issuance
    ...
guardrails:
  - rules
  - security
  - release-readiness
```

Edit the YAML directly between `decompose` and `approve` to refine
the spec without re-decomposing.

## Diagram

```
   draft ──► planning ──► needs_review ──► approved ──► running
                                  │              │           │
                                  │ Reject       │ Cancel    │ Block
                                  ▼              ▼           ▼
                                rejected     cancelled    blocked
                                                              │ resolve
                                                              ▼
                                                          running
                                                              │
                                                              ▼
                                                          completed
                                                              │
                                                              ▼ archive
                                                          archived
```

## Common pitfalls

- **Decompose hangs.** Backend slow or unreachable. `datawatch
  autonomous get $PRD_ID` shows `status: planning` for >5 min →
  check backend health.
- **Approve without review.** Easy to fall into; for non-trivial
  PRDs always at least read the Stories tab. Per-story approve gives
  finer control.
- **Run without approve.** Returns 400 (`approval required`). Approve
  first.
- **Story blocks on Evals failure.** That's the safety net working —
  inspect the failed grader, fix or accept-with-rationale.
- **Spec changes mid-run.** Edit-Spec + Run-Continue is supported,
  but the LLM only sees the new spec on the next task spawn; in-flight
  tasks finish under the old spec.

## Linked references

- See also: [`autonomous-review-approve.md`](autonomous-review-approve.md) — the review flow.
- See also: [`prd-dag-orchestrator.md`](prd-dag-orchestrator.md) — composing PRDs into DAGs.
- See also: [`evals.md`](evals.md) — per-story graded verification.
- See also: [`profiles.md`](profiles.md) — Project + Cluster Profiles.
- Architecture: `../architecture-overview.md` § Autonomous executor.

## Screenshots needed (operator weekend pass)

- [ ] Launch Automation wizard with intent + inferred fields
- [ ] PRD detail Overview tab
- [ ] PRD detail Stories tab with per-story state
- [ ] PRD detail Decisions tab with expanded `details`
- [ ] PRD detail Scan tab with verdicts
- [ ] Multi-select bar with state-aware buttons
- [ ] CLI `datawatch autonomous get` JSON output
