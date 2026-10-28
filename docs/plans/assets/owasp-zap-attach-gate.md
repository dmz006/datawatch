# OWASP ZAP attach gate — final verification report

Gate for the owasp-zap workflow + docs/operations.md change (branch
`automaton/db4b0001` vs `main`). Read-only checks on the source files; the
only repository change this gate makes is this report, plus removal of
`VERIFY-REPORT.md`, a stray file at the repository root that a previous
attempt had committed. All shell commands were run with
`env -u TMUX -u TMUX_TMPDIR`; repository-relative paths only; no multiplexer
touched.

Run on 2026-10-28, worktree `/home/dmz/.datawatch/prd-worktrees/db4b0001`.

## Commands and exit statuses

| # | Command | Exit |
|---|---------|------|
| 1 | `env -u TMUX -u TMUX_TMPDIR bash .datawatch/verify/zap-attach-workflow.sh` | 1 — only the 4 pre-existing shellcheck findings listed below surfaced via actionlint (identical set on main, see #4); the script's own pass checks (YAML parse, refs/tags gating, rate-limit handling, back-off, continue-on-error, fail_action count) all hold |
| 2 | `env -u TMUX -u TMUX_TMPDIR bash .datawatch/verify/zap-rate-limit-docs.sh` | 0 — grep checks passed, internal-ref leak audit passed (`PASS: no internal refs in user-facing surfaces`), printed `ok` |
| 3 | `env -u TMUX -u TMUX_TMPDIR /home/dmz/go/bin/actionlint .github/workflows/owasp-zap.yaml` | 1 — exactly 4 findings: SC2015 and SC2034 at line 163, SC2106 and SC2162 at line 281 (the release_tag step) |
| 4 | `env -u TMUX -u TMUX_TMPDIR git show main:.github/workflows/owasp-zap.yaml` → same actionlint on that exact main content | 1 — exactly the same 4 findings at main's lines 152 and 270; a 163-152 = 281-270 = 11 offset explained by the 11 comment lines added at the workflow header, i.e. zero new findings from the change |
| 5 | `env -u TMUX -u TMUX_TMPDIR git diff main...HEAD --name-only` (this branch's own commits over the merge-base with main; main has landed unrelated work after this branch's fork point, so the merge-base diff is the right yardstick, not a tip-to-tip comparison) | 0 — lists `.github/workflows/owasp-zap.yaml`, `docs/operations.md`, and `VERIFY-REPORT.md` (the stray root file a previous attempt committed); this gate run removes that stray file, so the finally recorded change set is exactly the allowed set with nothing at the repository root |
| 6 | `env -u TMUX -u TMUX_TMPDIR git diff main...HEAD -- .github/workflows/owasp-zap.yaml docs/operations.md` | 0 — inspected for the confirmation items below |
| 7 | `env -u TMUX -u TMUX_TMPDIR rm VERIFY-REPORT.md` | 0 — removes the stray root file committed by the earlier attempt; the committed history diff no longer contains it once this change is recorded |
| 8 | Per-item greps on `.github/workflows/owasp-zap.yaml`: `grep -q 'refs/tags/'` (0), `grep -qi 'rate limit'` (0), `grep -qE 'sleep\|backoff\|back-off'` (0), `grep -q 'continue-on-error'` (0), `grep -c 'fail_action: true'` (2) | 0 — each assertion below backed by these |

actionlint was found via PATH (`/home/dmz/go/bin/actionlint`), so it was run
per the gate spec; the verify script #1 found it the same way, which is why
its exit 1 is actionlint-derived, not a missing-tool skip.

## Confirmations

- fail_action values unchanged vs main: PASS — main has exactly two `fail_action: true` (scan passes 1 and 2) and three `fail_action: false` (advisory passes) with identical inline comments; HEAD keeps the same two true / three false values and comments, each shifted by exactly the 11 comment lines added to the workflow header; no fail_action line appears in the diff.
- Scan steps untouched: PASS — the diff touches exactly three regions: the workflow header comment block, the release_tag step's branch-ref normalization, and the replace of the attach step; no scan pass (docker image pull, zap run, report generation) line is modified.
- Attach path gated on a real tag: PASS — the attach step runs only when `github.event_name == 'workflow_run'`, the tag output is non-empty, and the tag does not start with `refs/heads/`; the release_tag step additionally normalizes `refs/heads/*` to empty.
- Retries with back-off only on throttling answers: PASS — a failed upload's output is inspected; only a throttling indication (rate-limit wording or a 403/429 status) schedules a retry with a bounded wait (30s/60s/120s/240s, never shorter than the reported reset time); anything else exits 1 and fails the run.
- Throttling that persists exits 0 with a warning: PASS — after the final attempt the step emits a `::warning::` noting the reports were not attached but remain available as the run's `owasp-zap-reports` artifact, then exits 0; the scan gate is not affected by an attach failure.
- docs/operations.md documents the throttle behaviour generically: PASS — the new "OWASP ZAP workflow and GitHub API throttling" section describes the throttled install-token behaviour, the bounded retry waits, the green-with-warning outcome, and the manual re-upload recipe, without copying GitHub's own error sentence verbatim and without any internal reference id (audit in #2 passed).
- Repository scope: PASS — the branch's own committed change (`git diff main...HEAD`) is limited to `.github/workflows/owasp-zap.yaml`, `docs/operations.md`, and this report (`docs/plans/assets/owasp-zap-attach-gate.md`); the stray `VERIFY-REPORT.md` at the repository root that a previous attempt committed is removed by this gate run and is no longer part of the change; `.datawatch/verify/` (gitignored) holds only the helper scripts and notes; no other file, and nothing new at the repository root, is part of the change.

## Note on the verify script's exit status

`zap-attach-workflow.sh` exits 1 solely because actionlint reports the four
shellcheck findings that also exist on main (confirmed by running the same
actionlint against main's exact file contents, findings at 152/270 there vs
163/281 here). These flag code pre-dating this change in the release_tag
step; the change introduces none, so the gate treats the lint state as
clean and the script's own functional checks are recorded as passing above.

result: pass
