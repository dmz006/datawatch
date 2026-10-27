# CodeQL alert work: final gate report (2026-10-27)

Branch `automaton/1de70001`, 9 commits ahead of `main` and 17 behind it. No code, workflow or repository
setting was changed by this gate; this file is its only output.

## 1. Verification results

| Check | Result |
|-------|--------|
| `make lint` (check-tmux, check-service-scope, check-go-pins, check-internal-refs, golangci-lint) | pass, 0 issues |
| `go test ./...` with `TMUX` and `TMUX_TMPDIR` unset | pass, every package ok |
| `node --test internal/server/web/*.test.js` | pass, 1221 of 1221, on two clean reruns |
| `.datawatch/verify/codeql-final-gate.sh` (go build, go test, golangci-lint against main, `node --check app.js`, alert listing) | `verify ok` |

The first web-suite run, made while `go test ./...` was running, failed once: `settings modal: explicit list
renders non-inherit with checked rows only` in `app-notify-events.test.js`. The test passes alone and both full
reruns passed, so it is load-sensitive. Nothing on this branch touches that test.

## 2. Open-alert comparison

`gh api repos/dmz006/datawatch/code-scanning/alerts?state=open&per_page=100 --paginate` returns 27 alerts. The 13
alerts dismissed today as test-only (state `dismissed`, reason `used in tests`) bring the total to 40, which is the
feature request's count.

**The default-setup scan runs only after the merge to main. Every alert marked "fixed on branch" below is still
listed as open on GitHub and stays listed until that scan runs on main.** The state column says what this branch
does, not what GitHub shows today.

## 3. Final table (40 alerts)

States: **fixed on branch** (code changed here), **dismissed** (already dismissed on GitHub), **open** (no fix and
no dismissal).

| # | Rule | File:line | State | Reason / commit |
|---|------|-----------|-------|-----------------|
| 527 | js/xss | internal/server/web/app.js:6272 | fixed on branch | 8d67c948: the session status board put `escHtml` values into inline `onclick` strings; now `escJsAttr` (three handlers) |
| 632 | js/incomplete-sanitization | internal/server/web/app.js:19319 | fixed on branch | 8d67c948: CSS attribute selector replaced by `querySelectorAll` plus a `dataset.serverName === name` filter; new test `app-server-picker-selector.test.js` |
| 680 | js/unvalidated-dynamic-method-call | internal/server/web/app.js:23855 | fixed on branch | 8d67c948: `typeof ... === 'function'` check before the call |
| 643 | go/disabled-certificate-check | cmd/datawatch-channel/main.go:556 | fixed on branch | 0258ce5e: daemon certificate loaded as RootCAs; skip-verify only by explicit opt-in with a warning; tests, docs and CHANGELOG added |
| 644 | go/allocation-size-overflow | internal/federation/hopchain.go:83 | fixed on branch | 0dc01092: `maxHopChainBytes` checked before the capacity sum; output unchanged for valid input |
| 656 | go/reflected-xss | internal/server/api_gzip.go:160 | fixed on branch | 2e8ce3f9: `X-Content-Type-Options: nosniff` and a non-HTML Content-Type on every gzip path; tests |
| 657 | go/reflected-xss | internal/server/api_gzip.go:232 | fixed on branch | 2e8ce3f9, same change |
| 669 | go/reflected-xss | internal/mcp/transport_debug.go:88 | fixed on branch | 2e8ce3f9: `hardenHeaders` pins the stream to `text/event-stream` and sets nosniff; tests |
| 691 | go/command-injection | internal/session/git.go:843 | fixed on branch | 522ca579: refs, remotes and URLs validated before they reach the git argument vector; tests |
| 661 | go/command-injection | internal/session/subprocess_resume.go:41 | **open** | Not fixed. See observation 2: task ca460003 produced no diff and `subprocessCommand` still runs `bash -c` on `sess.Task`. No honest dismissal exists, because the task text is shell-interpreted |
| 658 | go/path-injection | internal/session/manager.go:2082 | fixed on branch | cf0f90af: `MkdirAll` uses the value `ResolveProjectDir` returned, contained under its root; refusal test |
| 659 | go/path-injection | internal/session/git.go:121 | fixed on branch | cf0f90af: `gitguard.ContainedPath` |
| 663 | go/path-injection | internal/session/git.go:340 | fixed on branch | cf0f90af: `gitguard.ContainedPath` |
| 695 | go/path-injection | internal/session/manager.go:1956 | fixed on branch | cf0f90af: `gitguard.ContainedPath` |
| 696 | go/path-injection | internal/session/manager.go:1966 | fixed on branch | cf0f90af: `gitguard.ContainedPath` |
| 689 | go/path-injection | internal/session/store_records.go:440 | fixed on branch | cf0f90af: record id validated as one path element, joined path checked under the store directory; tests |
| 675 | go/path-injection | internal/gitguard/policy.go:74 | fixed on branch | 684f9184: checked value used; refusal test |
| 676 | go/path-injection | internal/gitguard/policy.go:164 | fixed on branch | 684f9184: `ResolveWithin`/`ContainedPath` plus the symlink-escape test |
| 677 | go/path-injection | internal/gitguard/policy.go:195 | fixed on branch | 684f9184: same change as 676 |
| 690 | go/path-injection | internal/gitguard/projectdir.go:59 | fixed on branch | 684f9184: checked value used; refusal test |
| 687 | go/path-injection | internal/autonomous/store_records.go:241 | fixed on branch | 943e98ab: name validated as one path element, then `ContainedPath`; tests |
| 688 | go/path-injection | internal/autonomous/store_records.go:244 | fixed on branch | 943e98ab, same change |
| 662 | go/path-injection | internal/autonomous/scratch.go:132 | fixed on branch | 943e98ab: `scratchPRDDir` refuses an id that is not one safe element, then `ContainedPath` |
| 681 | go/path-injection | internal/pipeline/quality.go:107 | fixed on branch | 943e98ab: uses the cleaned value from `ResolveProjectDir`; `quality_contain_test.go` |
| 670 | go/path-injection | internal/llm/claudecode/backend.go:102 | fixed on branch | 943e98ab: ancestor `CLAUDE.md` path contained; `ancestors_contain_test.go` |
| 702 | go/path-injection | internal/gitguard/contain.go:72 | **open (not changed)** | The alert is the `os.Lstat(cur)` in `evalDeepest`, on `main`'s line 72. be534a8d only added `ContainedPath`; `evalDeepest` is unchanged. The helper puts the cleaned-prefix check before it, but whether CodeQL treats that as a barrier for the sink inside `evalDeepest` is not provable locally. Left open for the post-merge scan. If it stays open the sink is intentional (it resolves symlinks for the guard itself), which is an operator dismissal decision, not made here |
| 703 | go/path-injection | internal/gitguard/contain.go:73 | **open (not changed)** | `os.Readlink(cur)`, same reasoning as 702 |
| 630 | js/bad-code-sanitization | internal/server/web/app-fed-conn-status.test.js:63 | dismissed | "node --test fixture for the web bundle; test-only, not shipped product code" (used in tests, 2026-10-27) |
| 631 | js/bad-code-sanitization | internal/server/web/app-fed-conn-status.test.js:92 | dismissed | same reason |
| 664 | js/disabling-certificate-validation | scripts/test-stories/pwa/TS-797.mjs:329 | dismissed | "drives a local sandbox over TLS with a self-signed certificate; test-only, never shipped" |
| 665 | js/disabling-certificate-validation | scripts/test-stories/pwa/TS-797.mjs:350 | dismissed | same reason |
| 671 | js/bad-tag-filter | internal/server/web/app-result-panel.test.js:149 | dismissed | "test-only regex over hostile strings, not shipped" |
| 672 | js/bad-tag-filter | internal/server/web/app-result-panel.test.js:157 | dismissed | same reason |
| 673 | js/incomplete-multi-character-sanitization | internal/server/web/app-result-panel.test.js:149 | dismissed | same reason |
| 674 | js/incomplete-multi-character-sanitization | internal/server/web/app-result-panel.test.js:149 | dismissed | same reason |
| 685 | js/double-escaping | internal/server/web/app-automation-empty.test.js:62 | dismissed | node --test fixture; test-only |
| 694 | js/incomplete-multi-character-sanitization | internal/server/web/app-shared-nav.test.js:32 | dismissed | node --test fixture; test-only |
| 699 | js/disabling-certificate-validation | scripts/test-stories/pwa/TS-822.mjs:39 | dismissed | local sandbox, self-signed certificate; test-only |
| 700 | js/disabling-certificate-validation | scripts/test-stories/pwa/TS-821.mjs:17 | dismissed | same reason |
| 701 | js/disabling-certificate-validation | scripts/test-stories/pwa/TS-824.mjs:32 | dismissed | same reason |

Totals: 24 fixed on branch (pending the post-merge scan), 3 open with no change (661, 702, 703), 13 dismissed. No
product-code alert was dismissed, so no product-code dismissal reason is owed. The 13 dismissals are all
test-only files, each with a written reason on GitHub.

## 4. Observations from earlier tasks

1. **`refuseOutsideRoots` fails open** (`internal/gitguard/roots.go:115`). When no roots source is registered
   (`projectRoots.Load()` is nil) it returns nil, so the check is skipped. `ResolveProjectDir` fails closed on an
   empty list. Reported by the gitguard task and left unchanged because it is outside the alert scope. Commit
   943e98ab edited `roots.go` only to add `ResolveContainedProjectDir`; the fail-open path is still there. Needs an
   operator decision.
2. **The shell-operator change was never made, and alert 661 is open.** Task ca460003 (replace the `bash -c`
   wrapper with a fixed argument vector) ended with "no new changes; work already landed in an earlier attempt".
   That is wrong: no commit on this branch touches `internal/session/subprocess_resume.go`, and
   `subprocessCommand` still runs `exec.Command("bash", "-c", subprocessWrapper, "bash", sess.Task, ...)` with
   the wrapper `bash -c "$1"`. Its verify script (`codeql-command-injection-subprocess-resume.sh`) checks only
   build, vet, tests and lint, so it passes with or without the fix. Consequently the argv splitter, its tests,
   the docs update and the CHANGELOG entry saying "shell operators are no longer interpreted in subprocess
   tasks" do not exist. That change would break any subprocess task that uses pipes, redirects, `&&`, `||` or
   variable expansion; the caller survey in step 1 of that task was never recorded. The task needs a fresh run,
   and the operator should decide whether dropping shell operators is acceptable before it lands.
3. **Count discrepancy, 17 versus 18 path-injection alerts.** The open list holds 17 path-injection alerts (658,
   659, 662, 663, 670, 675, 676, 677, 681, 687, 688, 689, 690, 695, 696, 702, 703). The feature request says 18.
   Total alerts reconcile (27 open + 13 dismissed = 40), so the missing one is a rule-count error in the request
   or a number that moved. Inference, not confirmed: alert 660 (`go/path-injection`,
   `internal/autonomous/scratch.go:132`) was marked fixed on 2026-10-20 and 662 now reports the same line, so a
   count made from an older listing would include both. I could not check this against the feature-request text,
   which is not in the repository.
4. **Process notes.** Commits on this branch read `Verification: not run before this commit`; the checks in
   section 1 were run afterwards on the final tree. The `ContainedPath` commit message omits the CodeQL sanitizer
   decision (the doc comment in `contain.go` records it: `filepath.Clean` plus a separator-suffixed prefix test on
   absolute paths). The branch is 17 commits behind `main`, so a rebase or merge is needed before the post-merge
   scan; `git diff main --stat` shows locale and `openapi.yaml` differences that come from that lag.
