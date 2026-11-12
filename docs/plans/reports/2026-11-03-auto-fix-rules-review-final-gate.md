# Final gate — auto-fix rules-review findings (attempt 1)

Date: 2026-11-03, from the task worktree
`7db40001-tasks/auto-fix-rules-review-findings-attempt-1-5a950001` (task-7db40001,
branch `automaton/7db40001`). Go 1.27.2 (linux/amd64). All commands under
`env -u TMUX -u TMUX_TMPDIR`.

## Result

| Gate | Result |
|---|---|
| `git status` | clean apart from the intended edits (below) |
| `gofmt -l` on the changed `.go` files | clean |
| `go build ./cmd/datawatch/ ./internal/...` | ok |
| `go test ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./cmd/datawatch/ ./internal/router/ -run 'Trunc\|Spec\|Cap\|Detail\|Edit\|Marker' -count=1` | ok (5/5 packages) |
| changed set includes `docs/` files | yes |
| `go test ./internal/router/ -count=1` (full suite) | ok, 471 tests, 2026-11-03 |
| `go test ./internal/autonomous/ -count=1` (full suite) | ok, 203 tests, 2026-11-03 |
| `go test ./internal/server/ ./internal/mcp/ ./cmd/datawatch/ -count=1` (full suites) | ok (2474 / 755 / 1748 top-level tests, including the guard tests) |
| `python3 -c "import yaml..."` on `docs/api/openapi.yaml` | parses; no new duplicate path keys (the file carries a pre-existing duplicate `description` key at HEAD — untouched) |
| `spec-read-full-and-guard.sh` unit/guard steps, run against this task's changed set | **PASS** (gofmt, build, the marker/refusal test filter, docs changed) |

### The one line the earlier report got wrong

`spec-read-full-and-guard.sh` **was not run in the verification suite for the feature
commits** — the suite stopped at a failed docs gate and recorded the rest as pending,
while the report read as if it had passed. The live half of that script (an ephemeral
daemon: create a PRD with a large output-bearing task, verify the capped read names the
marker field in `truncated`, verify the marker-bearing edit is refused 400, verify
`?full=1` and `/outputs` return the uncapped text) is still owed at merge. Every unit
and guard row it depends on is green (table above), so nothing is blocked — the debt is
a live check of already-guarded behaviour. The script's unit/guard steps do now pass
against this task's changed set (2026-11-03, run above): the script lives in the parent
worktree and its own `cd` targets that worktree, so the run above is the script's steps
applied to the files this task actually changed.

## What this task changed

Code (the comm-surface guard the rules review was missing):
- `internal/router/sx2_parity.go` — `commTruncationRefusal` + refusal in all eight comm
  verb paths — `autonomous create` (spec), `autonomous edit-task` (new_spec),
  `autonomous edit-story` (new_description), `autonomous add-story` (description),
  `autonomous add-task` (spec), `autonomous template-create` (spec) and
  `autonomous template-update` (spec) — the same operator-facing error as the MCP and
  CLI guards, refused locally before any request.
- `internal/router/autonomous_failure_escalation.go` — `handleAutonomousReviewTask`
  (the review verb's `retry` replacement spec) checks the marker before forwarding.
- `internal/router/sx2_parity_test.go` — `TestSx2_Comm_TruncationRefusal`.

Docs:
- `docs/api/openapi.yaml` — refusal documented in the 400 descriptions of PRD create,
  the new `PATCH /api/autonomous/prds/{id}` edit operation, `edit_task`, `add_task`,
  `add_story`, `edit_story`, `resolve_task_review`, and the template create/update
  routes; new `PRDTaskOutputRef` + `prds/{id}` and `templates` routes; the
  `GET /api/autonomous/prds/{id}` description now states the field-cap contract
  (always-whole `spec`/story `description`/task `spec`; capped PRD `memory_report`,
  task `error`, `prior_attempt_hint`, `start_failure_tail[]`, `attempt_refs[]`,
  verdict/verifier text at `autonomous.detail_text_cap_bytes`, default 4096, 0 =
  default, negative = off), the marker, the `truncated` array and the full reads
  (`?full=1`, `/outputs`, `tasks/{tid}/outputs`).
- `docs/mcp.md` — the truncation-marker refusal and the full reads next to the
  `autonomous_prd_*` tools, with every enforced tool named.
- `docs/testing-tracker.md` — new section "Truncation-marker refusal on spec and
  description writes — unreleased" with evidence rows per surface (helper, REST,
  MCP, CLI, comm, docs, live debt) and the real test names.
- `docs/api/autonomous.md` — the capped section now enumerates the enforcing surfaces:
  CLI (`prd create`/`edit`/`edit-task`/`edit-story`), the MCP `autonomous_prd_*`
  tools, all eight comm verbs and the review verb's `retry` path, all on the same
  operator-facing message.
- `CHANGELOG.md` — [Unreleased] entry extended with the comm-surface guard and the
  docs/tracker/parity-issue additions.

Planned:
- `docs/plans/2026-11-03-truncation-marker-refusal-rule-review-fixes.md` — the fix
  plan with the verification checklist.
- `docs/plans/2026-11-03-spec-truncation-parity-issue.md` — the datawatch-app parity
  issue (ready to file, not yet filed): the field-cap contract, the refusal message
  and routes, the client behaviour (local refusal, daemon 400 surface, capped-field
  badge + full reads), EN strings and the DE/ES/FR/JA translations.

## Not done (owed, with owners)

| Item | Owed until |
|---|---|
| `spec-read-full-and-guard.sh` live sandbox run (ephemeral daemon, create → capped read → refused edit → full read) | final gate at merge |
| Filing the datawatch-app parity issue and recording the URL into the draft | supervisor, at merge |
