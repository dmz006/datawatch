# BL465 — Move the primary git commit out of the worker's own tool loop

**Status:** shipped (v9.2.0) · **Date:** 2026-10-16 · **Tracker:** BL465
**Scope:** design, then implemented the same day (PRD `137ed868`, pi-prompt pilot).

## Decisions (resolved)

All seven decisions below shipped as designed:

1. **Commit timing** — the deterministic commit (`commitOnCompletion`) fires the moment the worker signals `DATAWATCH_COMPLETE`, before verification reads any diff.
2. **Workers no longer commit** — every backend's completion instructions were rewritten to say the opposite of "commit your work": commits happen automatically, do not run `git commit`/`git add` yourself.
3. **Commit message** — the worker's own one-line summary becomes the commit subject; falls back to the task title when no summary is found.
4. **Applies to every backend** — not backend-specific; verified on pi-prompt specifically, the backend whose first real pilot run exposed the original bug.
5. **One commit per task attempt** — an accepted, deliberate tradeoff, not a regression to fix later.
6. **Unconditional default** — `session.auto_git_commit` is deprecated and ignored; `PostSessionCommit` (the old parallel mechanism) is deleted.
7. **Verifier recognizes landed work** — a retry whose real work already landed in an earlier attempt is now recognized as done instead of failing as "produced no output."

## The problem

Tonight's autonomous runs repeatedly hit a structural trap: a task genuinely
finishes and commits real, correct work, but a later retry's baseline
(`Task.PreTaskSHA`) gets rebased to a HEAD that already includes that commit,
so `git diff PreTaskSHA..HEAD` is empty by construction and the task is
reported as "no output" — even though the work is done. Hit repeatedly on
PRDs `5154c783` and `2713dbb9` (task `352e89e7`, `682a1ba9`). Separately,
opencode's own tool-calling crash (`AI_APICallError: no user query found in
messages`, see `project_opencode_no_user_query_bug.md`) fires during the
worker's own tool-call turns — every tool call the worker makes, including
its own `git commit`, is a chance to trigger it.

Both problems share a cause: the worker LLM decides when and whether to
commit, as one of its own tool calls, and the verifier then has to infer
"did real work happen" entirely from git history shaped by that decision.

## Current state

The primary commit is already the worker's own action — it calls `git
commit` itself as part of executing the task. There is already a
deterministic fallback: `internal/session/git.go`'s `ProjectGit.PreSessionCommit`
(captures a clean baseline before the worker starts) and `PostSessionCommit`
(sweeps up anything the worker edited but never committed, fired from
`internal/session/manager.go` when a session reaches `StateComplete`). So
the architecture already has a non-LLM git actor — it's a safety net today,
not the primary path.

## The proposal

Make the deterministic path primary, not a fallback. The worker edits files,
runs its own tests, and signals done (`DATAWATCH_COMPLETE:`) — nothing in
its own toolset calls `git commit`. A separate, deterministic step (already
most of the way built as `PostSessionCommit`) always performs the actual
commit once the session reports done, using the real diff it finds on disk
as the source of truth.

This changes what "did the task produce output" means: instead of asking
"is there a new commit since an attempt-scoped baseline", the check becomes
"does the working tree differ from the task's own pre-task baseline, right
now" — read directly from the filesystem, not inferred from a chain of
retry-scoped git history that a worker's own commit timing can confuse.

## What this would and would not fix

- **Opencode's tool-calling crash** — reduces exposure (one fewer tool-call
  type per task) but does not eliminate it; the crash has also hit plain
  file-edit and shell-command turns.
- **The empty-diff retry trap** — this is the real target. Removing the
  worker's own commit timing as a variable, and checking live file state
  against the pre-task baseline instead of a retry-scoped git-diff chain,
  closes the specific failure mode seen tonight. Still needs the verifier's
  own "no output" check rewritten to use that comparison; moving who commits
  is necessary but not sufficient on its own.
- **The claude-code completion-detection gap** (`DATAWATCH_COMPLETE:` present
  in output, never detected) — unrelated. That is about reading the
  session's own text/pane output for a marker, not about git.

## Open questions

1. Exactly where does the deterministic commit happen — still
   `PostSessionCommit` on `StateComplete`, or does it need to run earlier,
   before verification reads the diff?
2. What commit message does the deterministic step write? The worker's own
   commits tonight read like real descriptive messages (e.g. "test(datawatch):
   BL455 record full-fix pre-fix confirmation") — losing that would hurt
   history readability. Possibly: have the worker still write a short summary
   as plain text (not a git action), and the deterministic step uses it as
   the commit message.
3. Does this apply to every backend (claude-code, opencode-acp, goose) or
   only the ones shown fragile tonight?
4. Multi-commit tasks — some tasks currently make several small commits as
   they go (readable history, incremental checkpoints). A single
   end-of-session commit collapses that. Is that an acceptable tradeoff, or
   does the deterministic step need to commit at tool-call boundaries too
   (which reintroduces some of the same git-during-the-loop surface this is
   trying to remove)?
5. `ForceAutoGitCommit` / `session.auto_git_commit` already exist as flags —
   does this become the new default behavior, or a new mode alongside today's?

## Prompt audit results

Audit of all worker-facing prompt text for instructions telling the worker to
commit or stage (commit/stage/recommit wording in task-spec builders, retry
hints, guardrail hints, session guardrails/templates, backend prompt code,
decompose/verify builders, howto text rendered into worker prompts, and
skills). Each instruction found was rewritten to the opposite: work is
committed automatically; do NOT run `git add`/`git commit`; finish with
`DATAWATCH_COMPLETE` + a one-line summary.

### Changed (before → after)

1. `cmd/datawatch/main.go` (TUI task delivery, appended to every TUI task
   text; previously the `DATAWATCH_COMPLETE` protocol literal appended on
   session task delivery):
   - Before: `When you have fully completed this task, output the following
     line as your final response (plain text, not in a code block):
     DATAWATCH_COMPLETE: <one-sentence summary of what was done>`
   - After: same, with `DATAWATCH_COMPLETE: <one-sentence summary of what was
     done, used as the commit message>` — still says "one-sentence summary",
     now also says the summary is used as the commit message. The
     placeholder-skip guard (`strings.Contains(l, "<one-sentence summary")`,
     `completionSummaryPlaceholder`) still matches because the prefix is
     unchanged.

2. `internal/session/tracker.go` — `minimalSessionGuardrails`, the
   constraint block injected into every session's CLAUDE.md:
   - Before: `- Commit changes to git frequently`
   - After: `- Do not run git add or git commit — your work is committed
     automatically when you finish`

3. `templates/session-CLAUDE.md` — full session-guardrails template (Git
   Requirements section, rendered for claude-code sessions):
   - Before:
     ```
     - **Commit frequently** — after each logical unit of work (feature, fix, refactor).
     - Use conventional commit messages: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`
     - Never force-push or rewrite history.
     - If you create new files, add them to git with `git add`.
     - Before finishing, run `git status` and commit any remaining changes.
     ```
   - After:
     ```
     - **Do not run `git add` or `git commit`** — your work is committed automatically by the harness when you finish.
     - Never force-push or rewrite history.
     - When your task is fully complete, finish with `DATAWATCH_COMPLETE: <one-sentence summary of what was done, used as the commit message>`.
     ```

4. `internal/autonomous/executor.go` — `contentFixHint`, the BL410 Phase 2
   guardrail retry hint fed to the worker in `SpawnRequest.RetryHint`:
   - Before: `Fix these issues, re-run the project's own build/lint/test
     commands yourself, and recommit.`
   - After: `Fix these issues and re-run the project's own
     build/lint/test commands yourself. Do not run git add or git commit —
     your work is committed automatically when you finish; finish with
     DATAWATCH_COMPLETE: <one-line summary of what you did>.`

5. `internal/autonomous/manager.go` — `PriorAttemptHint` reset/retry prompt
   (v8.36.2):
   - Before: `…fix the specific issues above (or reconfirm correctness) by
     editing and re-committing the actual required deliverable files
     themselves — verification only sees this run's own diff…`
   - After: `…by editing the actual required deliverable files themselves —
     do not run git add or git commit; your work is committed automatically
     when you finish. Verification only sees this run's own diff…`

### No change needed (audited, clean or not worker commit instructions)

- `internal/autonomous/scope.go` — `TaskScopeBlock`/`renderScopeBlock`
  (task-spec scope block for `autonomous_run` and per-task spawn): scope
  boundaries only, no commit/stage wording.
- `internal/autonomous/decompose.go`, `verification_script_prompt.go`,
  `verification_script_coverage.go` (decompose + verify prompt builders):
  no commit/stage instructions.
- `internal/autonomous/git_completion.go` and other `internal/autonomous/`
  files: only code comments about the harness's own auto-commits.
- `cmd/datawatch/autonomous_effort.go` — harness block already says the
  target text verbatim: "Do not run git add or git commit — your work is
  committed automatically when you finish. Finish with a line:
  DATAWATCH_COMPLETE: <one-line plain-text summary…> — that summary becomes
  the commit message."
- `internal/llm/backends/opencode/` (backend.go, acpbackend.go),
  `internal/llm/backends/pi/`, `aider/`, `gemini/`, `goose/`, `shell/`,
  `openwebui/`: no commit/stage instructions in prompt-building code; all
  use the `DATAWATCH_COMPLETE: …` completion marker only.
- `internal/llm/claudecode/`: only the shell-level
  `echo 'DATAWATCH_COMPLETE: claude done'` marker; no worker commit
  instructions.
- `.datawatch/skills/web-search-guidance/SKILL.md`: no commit/stage
  instructions.
- `internal/mcp/autonomous.go`, `guardrail_tools.go`: mentions of "commit"
  are orchestrator-facing MCP tool descriptions (describing what the
  automaton does / telling the operator to commit its own decision to the
  repo), not worker commit instructions — out of scope.
- `docs/howto/file-drift-guardrail.md`, `ci-watch-guardrail.md`,
  `release-checklist.md`: operator-facing prose (describing drift causes,
  telling the operator how to unblock); not instructions rendered into a
  worker's commit behavior.
- `internal/session/manager.go`: only implementation comments describing the
  auto-commit mechanism; no worker-facing prompt text (the delivered task
  literal lives in `cmd/datawatch/main.go`, covered above).

### Verification

- `gofmt` clean on all touched Go files.
- `go build ./cmd/datawatch/` OK.
- `go test ./internal/session/ ./internal/autonomous/` — 1040 tests pass
  (includes `completion_summary_test.go`, which keys on the
  `<one-sentence summary` prefix preserved in the new literal).
