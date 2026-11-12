# Response to the self-building-loop questions (issues #207, #209, #210)

Status: DRAFT for the operator to iterate on. Nothing here has been posted. Items marked
**[operator to decide]** are not decided; the text beside them is a proposal.

Date: 2026-10-26. Written from the repository as it stands at v9.18.0.

## Short version

Both observations are accurate in substance. What we found when we checked:

1. The `AGENT.md` commit (b77c6503) was written by datawatch's own pre-session machinery, not by a
   worker deciding to loosen its rules. But the underlying question stands, and today nothing stops
   a worker from changing its own rules (details in question 1).
2. The "supervisor snapshot" commit (d398b7b0) is real. It was made by the supervisor session, and
   it did go around the final judge. The tests, lint and CI that run before and after a merge were
   not skipped. The judge was.
3. We can confirm the smaller notes and have fixed what we could.

## The two worries

### 1. Commit b77c6503 (2026-10-11) edits AGENT.md

What it is: the diff adds 47 lines to `AGENT.md`, and all of them are the "Cross-Session
Communication Rule" block. That block is injected into a project's rules file by datawatch's session
tracker when cross-session messaging is enabled (`internal/session/tracker.go`, the
`cross-session-communication-rule` marker). The commit message, `pre-session[]: You are running
non-interactively as part of an automated pi...`, is the pre-session git snapshot using the first
characters of the task prompt as its message (`internal/session/git.go`). So the daemon injected the
text and its own snapshot committed it. We did not find a worker editing its rules in this commit.

What is still wrong:

- The daemon writes into the file that holds the workers' operating rules, and the snapshot gives
  that write a misleading message under the same git identity as every other commit.
- Every commit in this repository, from the operator, the supervisor and workers, is authored as
  the same user. Nothing in git tells you who wrote a change to a rules file.

### 2. Commit d398b7b0 (in v9.17.4) went in as a "supervisor snapshot"

What happened: the live held-task review change (a PRD of 7 tasks, about 1,000 lines) had all seven
tasks done and each one passed its own task verifier. The PRD-level final judge, which runs on a
local model, timed out at 15 minutes on that diff and was respawned. The finished work was sitting
uncommitted in the worktree. The supervisor session committed it on the PRD branch and merged it.

What did and did not run before it reached main:

| Gate | Ran? |
|---|---|
| Task verifier for each of the 7 tasks | yes |
| Merge in a throwaway worktree, `make lint`, full `go test ./...` (exit 0 required) | yes |
| CI on main after the push (tests, lint, govulncheck, gosec, secret scan, security scan, docs-sync) | yes |
| PRD-level final judge (independent review of the whole diff) | **no, bypassed** |

You are right about the shape of the problem: the bypass opens when a diff is large, which is when an
independent read of the whole change matters most. It was also a standing supervisor practice, not a
one-off. The supervisor's own notes said that when the judge times out (an infrastructure fault) and
all tasks are done, merge through the pipeline rather than wait.

What changed since: the judge now has its own routing and time limit (v9.18.0). A judge that times
out moves to the next backend, one attempt per backend. After the last backend the Automaton is held
for review with an alert. It no longer sits waiting and tempting a bypass.

**[operator to decide]** Proposal, as you suggest: a judge timeout holds the merge. The supervisor
does not snapshot around the judge. If a large diff cannot be judged on any backend, it is held for
the operator. The old "merge via the pipeline when the judge is an infra fault" lesson is removed
from the supervisor rules.

## Smaller notes

- **Roadmap rows 47 and 49 lost their description.** Confirmed. A registry edit that marked them
  shipped wrote the status into the Item column and dropped the text. Row 47 was the MCP SSE
  listener bind retry (BL444) and row 49 was the pi backend echoing `DATAWATCH_COMPLETE` on a
  non-zero exit (BL468). Both are restored in `docs/plans/README.md`.
- **#210 is fixed in v9.5.84 but still open.** Confirmed: the fix (BL444) shipped in 9.5.84 and the
  issue was never closed. **[operator to decide]** whether to close it with a note naming the
  release.
- **`CHANGELOG.md` has two `v9.0.0` headings.** Confirmed: `## v9.0.0 — Memory Lifecycle Complete`
  appears twice, once with "(major release)". We will merge them into one after checking that
  neither heading hides content the other lacks. **[operator to decide]** whether to do it now.
- **#209 (empty token, `0.0.0.0` default, no warning).** Confirmed on the current code:
  `internal/config/config.go` still defaults the host to `0.0.0.0`, and there is no startup warning
  when the token is empty. See question 2.

## Your three questions

### 1. What stops a worker from loosening its own rules today?

Nothing that you would count as a gate. We checked each place a gate could be:

| Possible gate | State |
|---|---|
| `CODEOWNERS` lists `AGENT.md`, `SECURITY.md`, workflows and the security review for the owner | present, but **not enforced**: `main` has no branch protection and every change is pushed directly under one identity, so code-owner review never runs |
| Pre-commit or server-side hook that rejects edits to rules files | none (the repository has no hooks) |
| Per-Automaton read and write directory scope | exists, but `AGENT.md` sits inside the default scope, so it does not protect it unless an operator narrows the scope on every Automaton |
| The rules-review judge | **works against the gate**: it reads the rules file from the Automaton's own working tree (`internal/autonomous/rules_review.go`), so a worker that edits `AGENT.md` also changes the rubric it is judged by |
| Task verifiers and the final judge | review the work, but are not written to treat a change to a rules file as special |

So a worker, or the daemon's own injection, can change the rules, and the review compares the diff
to the changed rules.

**[operator to decide]** Proposal, in the order we would build it:

1. The rules-review judge reads the rules from the base commit on `main`, never from the worker's
   tree.
2. A protected-paths check in the daemon: a task commit that touches `AGENT.md`, the supervisor rules,
   `CODEOWNERS` or the workflows is rejected and the task is held for the operator, unless the
   operator has authorised that Automaton for that path.
3. Distinct git identities for the operator, the supervisor and workers, so a history query answers
   who changed a rules file.
4. Branch protection on `main` with code-owner review for the protected paths, once the loop can
   raise pull requests.
5. The daemon's own injection into rules files stops writing to `AGENT.md`, or writes only through a
   path the operator has approved.

### 2. Is #209 still planned before v10?

Yes. The owner's reply on the issue (2026-10-15) says it is part of the pre-v10.0.0 security
remediation track and that the decided fix is a startup warning plus a per-listener auth flag in
`/api/health` and the diagnose output. That has not been implemented yet. Your point that a loud
warning respects an empty token as a deliberate operator choice while still catching the people who
did not make it is the reason that design was chosen, and we are keeping the default host and token
behaviour as they are.

One thing we found while checking: the roadmap registry had no row for it. It is now tracked as its
own item (see "Work this opens", below).

### 3. Which tag is stable for a long-running deployment? Can memory eviction be turned off per namespace?

**Stable tag.** There is no stable or long-term-support line today. In the last 10 days we cut 127
releases (13 minor, 114 patch), including 126 patches on the 9.5 line alone. We would not call any
9.5.x release stable. **[operator to decide]** which tag to name. The honest candidate today is the
newest release with all workflows green and assets published, which is **v9.17.6**; **v9.18.0** is
newer but its release was still building when this was written. We suggest naming a stable tag only
after it has run for a defined soak period, and we would rather say that than name one now.

**Memory eviction.** Here is what exists, from the code:

- The similarity-stale sweep is the `memory_sweep_stale` tool. It is **manual only**: nothing calls
  it on a schedule. It defaults to a dry run, and it exempts pinned and manual rows. So no
  background job runs that sweep on your memories.
- The only automatic deletion is time-based `retention_days`. Its default is `0`, which means keep
  forever, with optional overrides for session summaries and output chunks.
- Deleting a PRD with the archive strategy copies its memories to the archive scope and then purges
  the original scope. That is deliberate and visible.
- We did not find a per-namespace setting that turns eviction off, and there is no "demote instead of
  delete" state in the memory store. Your house rule, never deleted, only demoted, is not something
  the store can express today.

**[operator to decide]** Proposal: a configuration guard that disables every destructive memory
operation globally and per namespace, and a demoted state that keeps the row out of search results
without deleting it. The sweep would demote when the guard is on.

## Work this opens

None of this is filed as a plan yet unless noted.

| Item | State |
|---|---|
| #209 startup warning and per-listener auth flag | tracked, design decided by the owner on 2026-10-15 |
| Protect rules files from workers and from the daemon (question 1) | needs the operator's design decision |
| Judge timeout holds the merge; remove the bypass lesson | needs the operator's confirmation |
| Memory: destructive-operation guard and a demoted state | needs the operator's design decision |
| Close #210, merge the duplicate `v9.0.0` heading | operator's call, see "Smaller notes" |
