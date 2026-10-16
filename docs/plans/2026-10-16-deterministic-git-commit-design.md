# BL465 — Move the primary git commit out of the worker's own tool loop

**Status:** needs_review · **Date:** 2026-10-16 · **Tracker:** BL465
**Scope:** design only. No code changes.

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
