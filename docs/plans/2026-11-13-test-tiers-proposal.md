# Test tiers proposal: what runs before a commit, in CI, before a minor release, and before a major release

Date: 2026-11-13. Status: proposal for the operator's decision. Nothing here is
implemented. No Automata are filed. No code, workflow or script was changed to
write this.

The operator's request, in short: `go test ./...` takes about 12 minutes in CI
and in the Release workflow, the full run used to be done locally before
tagging, CI should not carry host-speed-dependent tests, and the end-to-end
(e2e) suite used to be major-release only, for a reason. This document says
where the tests really run today, how that came about, and proposes four tiers
with owners and time budgets.

Method: read `AGENT.md`, the `Makefile`, `scripts/`, every workflow in
`.github/workflows/`, `docs/testing.md` and `docs/testing/`; pulled run and step
timings with `gh`; read the per-package times out of one CI log; searched git
history with `git log -S`; and used the supervisor's audit in
the supervisor's scratch folder (`testdiff.txt`, `added.txt`,
`tests.txt`) as one input. I did not run the test suite.

## Where we are

### The tiers as they really exist

| What | Where it runs today | How long | What it needs |
|---|---|---|---|
| `go test ./... -count=1 -timeout 12m` | CI on every push to main and every PR (job `test + lint`); again in the Release workflow job `go test + lint` on every tag push; locally by hand (`make test`) | The step took 12 min 28 s in the 2026-11-13 12:13 run (12:14:11 to 12:26:39). The whole `test + lint` job took 15 min. Successful CI runs over the last 40: median 17.9 min, range 13.8 to 23.0 min, end to end | The `go` toolchain only. A few tests start `tmux`, bash scripts, `git`, `nice` and `timeout` |
| golangci-lint | CI and Release | About 1.5 min | Go toolchain |
| Fast repo checks (version bump, internal refs, unscoped tmux, Dockerfile Go version drift, toolchain pins, trivy single version, certifi floor) | CI on every push, and part of them in Release | Under 1 s each | bash, git history (`fetch-depth: 0`) |
| `make lint` (adds leak check, plans links, no-config-copies and similar) | Local; the supervisor merge helper runs only `check-merge-version.sh` and `make check-internal-refs` | Not measured | bash, python |
| `make test` extras (about 11 bash and python test scripts under `tests/` and `scripts/tests/`) | Local only. CI does not run them | Not measured | bash, python |
| PWA unit tests, `node --test internal/server/web/*.test.js` (353 test files) | Local only. Neither `ci.yaml` nor `release.yaml` has a node step | Not measured | node |
| `scripts/perf-budget.sh` (Go budget test, plus a slow-4G tab measurement when `TEST_HTTP` and `TEST_TOKEN` are set) | Local, opt-in | Not measured | A sandbox daemon and node for the second half |
| `scripts/release-smoke.sh` (7,312 lines, starts its own isolated daemon) | Local by hand per `AGENT.md`; in CI only through the `kind-smoke` workflow, which runs on pull requests that touch charts, Dockerfiles, `cmd/datawatch/**`, `internal/server/**` or the smoke script. Not on tag push. The `release-smoke-*.sh` companions are local | `kind-smoke` has a 30 min job limit. Local time not measured | A built binary, ports, tmux; in CI also kind, kubectl and helm |
| Live and sandbox smoke (opt-in Go tests gated on `DW_LIVE_*` environment variables, scripts such as `*-live-check.sh`) | Local, against the sandbox daemon (HTTP 18080, TLS 18443) | Not measured | A running sandbox daemon, never production |
| e2e story runner `scripts/run-tests.sh` (about 117 story scripts under `scripts/test-stories`) and the Playwright PWA stories (`make test-e2e-pwa`, `scripts/e2e-pwa.sh`) | Local or docker compose only. No workflow runs them | Not measured | Sandbox daemon, docker compose, a browser (Playwright), sometimes ollama and a peer |
| `tests/integration/spawn_docker.sh`, `spawn_k8s.sh` (the AGENT.md "major only" single-host and Kubernetes smoke) | Manual | Not measured | docker, or a kubectl context |
| gosec, govulncheck | Release workflow only (gosec about 15.5 min, govulncheck under 1 min) | | Network |
| goreleaser and container matrix, Trivy scans | Release workflow only | goreleaser about 20 min; containers 2 to 7 min each | Network, registry |
| CodeQL, secret scan, security scan, dependency review, docs sync, ZAP (after Release), matrix integration (path filtered) | Own workflows | Not measured | Network |

A full tag run is therefore: the same `go test` the push already ran, then
gosec, then goreleaser, then containers. In the 2026-11-13 12:13 Release run the
`go test + lint` job ran from 12:13:25 to 12:28:54, gosec to 12:44:24, goreleaser
to 13:04:59, and the run ended at 13:13:16, one hour after it started. The CI
run for the same commit started within a second of the Release run and did the
same tests on a second runner. The tag-time test job is a pure duplicate of the
push-time job for the same commit.

### Where the 12 minutes go

Per-package times from the CI log of the 2026-11-13 12:13 run (packages run in
parallel on a small runner, so these overlap):

| Package | Seconds |
|---|---|
| `internal/session` | 418 |
| `cmd/datawatch` | 317 |
| `tests/ci` | 288 |
| `internal/router` | 269 |
| `internal/server` | 207 |
| `internal/autonomous` | 142 |
| `internal/messaging/backends/email` | 134 |
| everything else (about 95 packages) | each under 17 |

Seven packages carry the wall time. The `internal/session` package contains a
temp-leak guard (`TestNoNewTmpLeakEntries`) that runs a nested `go test` of
other packages; the 2026-10-28 commit message says it alone made the session
package take 4.5 to 5 minutes. `tests/ci` is not product code: it tests the CI
and release tooling (version bump check, merge helpers, cross build) against
throwaway git repositories, so it belongs with the release path, but 288 s is
long for it.

### The supervisor's audit, as found in the tree

| Finding | What I confirmed |
|---|---|
| `mergeshadow` load test with CPU burner workers | Confirmed. `internal/mergeshadow/replay_load_only_failure_test.go` starts a quarter of the cores as `nice -n 19` busy-loop workers for the length of a concurrent run, with a 300 s hard timeout. It is deliberately load dependent, and runs inside CI's `go test` |
| `internal/plugins/supervisor_test.go` two-sided stop timing | Confirmed in shape. The test uses a 50 ms health interval, 50 ms and 200 ms backoff, a 500 ms HTTP client timeout and a `stopWithin` helper that measures stop duration. I did not re-derive which assertion is two-sided; the audit names it |
| `kind-smoke` runs `release-smoke.sh` on PRs and fails on config | Confirmed that it runs on PRs. The last runs in the list are automated stale-CVE-suppression pull requests; five of them show `failure`, one `action_required`, and the two older docs and fix PRs from 2026-10-14 passed. I did not determine the failure cause; the audit says config |
| Many 50 ms to 750 ms ceilings in Go tests | Confirmed. My narrow grep alone finds nine wall-clock ceilings (for example 50 ms in `lifecycle_session_push_test.go`, 200 ms in `infra_retry_comm_test.go` and `typing_idle_test.go`, 500 ms in `pinkeeper_test.go`, `peer_usage_test.go` and `node_usage_test.go`, 750 ms in `question_raise_notify_test.go`). The audit's count is higher |
| The 12 minute timeout raised from 5 | Confirmed, 2026-10-28; see the next section |

Only six test files use `testing.Short()`, so `go test -short` today skips almost
nothing. A short mode exists as a mechanism but not as a practice.

## How it got here

| Date | Event |
|---|---|
| 2026-04-27 | The first `release-smoke.sh` lands, from the operator directive that every release must test every subsystem functionally, not only through unit tests |
| 2026-04-28 | AGENT.md directive: smoke required on minor and major releases and on the first patch that adds a feature; targeted `SMOKE_ONLY` smoke for later patches; skip only for docs-only |
| 2026-05-13 | The e2e plan, runner and scaffold (`scripts/run-tests.sh`, the story files) are added |
| 2026-05-17 | `ci.yaml` and `release.yaml` are created in one commit, and both already run `go test ./... -count=1 -timeout 5m`. That is when CI began running the full suite. It has run the full suite on every push since; the only later change to that line is the timeout |
| 2026-09-06 | An AGENT.md checklist audit adds Section A to E. A2 says `go test ./...` passes on every commit and push; Section C sets the smoke cadence; Section E (major releases only) lists LLM alias refresh, the compatibility audit and E3, "Full test suite: single-host Docker smoke + k8s smoke + cross-feature flows + UI smoke", results in `docs/testing.md` |
| 2026-10-20 | AGENT.md "every commit adds its own tests at every level it touches" rule: unit tests, smoke tests, and, for any web or PWA change, an e2e story run through `run-tests.sh` against the sandbox |
| 2026-10-28 | CI and Release `go test` timeout raised from 5 to 12 minutes. The commit message gives the reason: the session package alone took 4.5 to 5 minutes on a loaded runner and a release run hit "test timed out after 5m0s", publishing nothing |
| 2026-11-13 | CI step takes 12.5 minutes |

What I could and could not establish about the major-only e2e tier:

- I found no commit that dropped it. The e2e and Section E major-only text is
  still in AGENT.md and still says what it said when it was written.
- What the history shows is that it was never wired to anything that enforces
  it. No workflow runs the e2e runner, the Playwright stories, or the
  `spawn_docker.sh` and `spawn_k8s.sh` scripts. The required tokens in the
  AGENT.md Section A to E tables (`tests: ok`, `smoke: ...`) are commit-message
  conventions; no script checks for them. The only tag-time automated gate is
  the CI workflow content itself.
- The 2026-10-20 rule moved part of e2e to per-commit for web changes, in
  prose only. That blurs the old line between "every commit" and "major only".
- The release rate makes a literal reading of the cadence impossible. Between the
  `v9.0.0` tag on 2026-10-09 and 2026-11-13 the repository carries 464 `v9.*`
  tags, 168 of them minors (`v9.N.0`) and 296 patches. That is about 13 tags a
  day. "Full smoke at every minor" would mean nearly five full smoke runs a day.
  I have no evidence that this was done at that rate; I cannot tell from the
  tree whether it was.

What AGENT.md says today about running before tagging (quoted):

- Testing Requirements: "Run all tests — `go test ./...` must pass before committing".
- Section A, "Every commit/push (no exceptions)", row A2: "`go test ./...` passes (not just compile — full test suite)", token `tests: ok`.
- Row A12: "`make lint` clean (`golangci-lint run ./...` — the exact invocation CI's required `test + lint` check runs; `go test` alone does not catch errcheck/staticcheck-class issues)".
- Release testing: "smoke runs are **required on minor and major releases**, plus on the **first patch that introduces a new feature**".
- Section C: "Full smoke (`bash scripts/release-smoke.sh`) — required for: Every minor (X.Y.0) or major (X.0.0) release; The first patch in a minor that introduces a new feature".
- Section E: "Major releases only (X.0.0)" with E3, the full suite.
- Testing Requirements item 6: a web or PWA change "also needs an e2e story ... run through `scripts/run-tests.sh --stories=...` against the sandbox".

So the rule for what runs before tagging exists. It is a human checklist. There
is no pre-tag script, no git hook (`.git/hooks` has only samples and
`core.hooksPath` is unset), and the supervisor merge helper
(`scripts/merge-branches-bump-version.sh`) does not run the tests itself; it
says that every branch handed to it "must already have passed `make lint`
itself, which the script cannot enforce".

## Proposed tiers

Principle: a check runs in the earliest tier where it is deterministic and
cheap enough, and in CI only if it validates the build and release path rather
than the product's behaviour on a particular host. A test whose pass or fail
depends on how fast the host is does not run in CI.

| Tier | When | Exact checks | Where | Time budget | Enforced by |
|---|---|---|---|---|---|
| 0 Before every commit and before every tag | Every commit by a person or an Automaton; and again on the exact commit about to be tagged | `go build ./...`; `go vet ./...`; `make lint` (golangci-lint plus the repo checks); `go test` on the packages the change touches; the plain full `go test ./... -count=1` once on the tag candidate; `node --test internal/server/web/*.test.js` when `internal/server/web` changed; the `make test` shell and python scripts | Local, on the committer's machine or the Automaton's worktree | Changed packages: under 3 min. Full tag-candidate run: whatever it costs on the operator's host (not measured; see Open facts) | A new pre-tag script that writes a record for the commit sha; the merge helper (or the tag step) refuses to tag a sha with no record. A git hook only for the fast part |
| 1 CI per push and per PR | Every push to main, every PR | Version bump check, internal-ref, tmux and Dockerfile and toolchain pin checks; `go build`; `go vet`; golangci-lint; `tests/ci` (the release-tooling tests); doc and parity checks (`docs-sync`, howto coverage); a short deterministic unit run (see decision 1); CodeQL, secret scan, dependency review as separate workflows | GitHub Actions | 6 to 8 min end to end (about 4 min for tests) | Required status checks on main |
| 2 Minor release, local | The release-train candidate (see decision 4), not literally every `X.Y.0` | Full `go test ./... -count=1` including every host-speed and load test; the PWA node suite; `scripts/perf-budget.sh` with a sandbox daemon; full `scripts/release-smoke.sh` on an isolated daemon; the `release-smoke-*.sh` companions; the live-daemon and sandbox smoke and `DW_LIVE_*` tests; the pre-release dependency audit and gosec (AGENT.md C1 and C2) | Operator's machine and the sandbox daemon (TLS 18443), never production | 30 to 60 min (estimate, not measured) | The release-candidate record file and the checklist the supervisor merge helper runs; Release workflow checks the record exists |
| 3 Major release only | `X.0.0` | Everything in tier 2, plus the full e2e story suite on the sandbox (`scripts/run-tests.sh` full run); the Playwright PWA suite including the load, mobile-fit and shared-nav groups; `spawn_docker.sh` and `spawn_k8s.sh` (single-host and Kubernetes smoke, the `kind-smoke` job run on demand); cross-feature flows; the config-channel parity audit; evals if the operator decides they are in scope; the LLM alias refresh and compatibility audit; results written to `docs/testing.md` | Operator's machine, sandbox, a docker host, a real or kind cluster | Hours | AGENT.md Section E as a checklist; failures block the release; the Release workflow refuses an `X.0.0` tag without the tier 3 record |

### Where each current CI check goes

| Check | Today | Proposed tier | Why |
|---|---|---|---|
| Fast repo checks (version bump, internal refs, tmux, pins, certifi) | CI and Release | 1 (and 0 through `make lint`) | They validate the release path, deterministic, under 1 s |
| golangci-lint, `go build`, `go vet` | CI and Release | 0 and 1 | Deterministic and quick |
| `tests/ci` release-tooling tests | CI (288 s) | 1, slimmed | Validates the release path. The slow ones (cross-build test) move to tier 2 if they stay over a minute |
| Full `go test ./...` | CI and Release, 12.5 min | 0 for every tag candidate; 2 for the full run with the slow tests; 1 only a short run | The operator's rule: do it before tagging, not in CI |
| `mergeshadow` load test with CPU burner workers | CI via `go test` | 2 | Load dependent by design |
| `internal/plugins` supervisor two-sided stop timing and the 50 to 750 ms ceilings | CI via `go test` | 2 until each test is rewritten without a wall-clock ceiling; then back to 1 | Host-speed dependent |
| `internal/session` temp-leak guard with nested `go test` | CI via `go test` | 2 (`make test-tmp-guard` already exists for the full mode) | The nested run is most of that package's 418 s |
| Soak, store benchmark and large-volume tests (the `testing.Short()` set) | CI | 2 | Volume, not correctness |
| `kind-smoke` running `release-smoke.sh` on PRs | PR | 3 (on demand through `workflow_dispatch`), plus tier 2 locally; PR runs limited to chart lint and `helm template` | It is smoke, it fails on config, and it is not deterministic |
| `release-smoke.sh`, `release-smoke-*.sh` | Local and `kind-smoke` | 2 | Needs a daemon |
| PWA node tests | Local only | 0 and 2 | Not in CI today; see decision 5 |
| e2e stories, Playwright | Local only | 3 (a targeted story for a PWA change stays at tier 0 as AGENT.md already says) | The operator's major-only intent |
| Evals | Not in CI | 3 if applicable | Model-dependent, costly |
| gosec, govulncheck, goreleaser, containers, Trivy | Release | Stay in Release | They need the tag and the network, which is what the release path is |

## Decisions for the operator

Each decision has options, tradeoffs and my recommendation.

1. Does CI run the full `go test ./...` at all?
   - A. Keep it as it is. Zero change, 18 minutes per push, host-dependent
     failures keep appearing, and the same run is repeated at tag time.
   - B. Replace it with a deterministic short run: `go test -short ./...`
     (4-minute target), after the host-speed, load and soak tests are marked to
     skip under `-short`. Costs an Automaton to classify the tests (the audit
     gives the starting list; only six test files use `testing.Short()` today).
     Risk: a regression in a skipped test is not seen until tier 0 or tier 2,
     so tier 0 has to be real.
   - C. No Go tests in CI; CI is build, vet, lint and release-path checks only.
     Fastest (about 3 minutes) and the strictest reading of the request, but a
     push from a session that skipped tier 0 reaches main with no test at all.
   - Recommendation: B. It keeps one deterministic net under the operator's
     rule without the 12-minute wait, and it creates the `-short` convention
     that tier 0 and tier 2 reuse. Until the classification Automaton lands,
     the interim step is to move the slowest packages out of CI with an explicit
     package list (not the seven named above wholesale; those hold most of the
     product tests), which is cheaper but coarser.

2. Does release.yaml re-run the tests at tag time?
   - A. Keep re-running the full suite. Safest against a tag on an untested
     commit; costs 12.5 min of the hour and duplicates the push-time run.
   - B. Trust the pre-tag run and the CI run for the same sha. Release only
     checks that the commit has a green CI run (and, for tier 2 and 3, a
     record), then builds. Saves about 15 minutes per release; depends on the
     record being honest.
   - C. Re-run only the tier 1 short run, which is cheap and catches a bad tag
     of the wrong commit.
   - Recommendation: B for the Go suite, with the cheap tier 1 build, vet and
     fast checks kept in Release (they take seconds). Because tag and push fire
     at the same moment today, the Release job needs a "CI for this sha is green"
     gate or it is trusting nothing.

3. How is tier 0 enforced: script, git hook or checklist?
   - A. Checklist only (today). No cost, not enforced, and no Automaton can be
     held to it mechanically.
   - B. A `scripts/pre-tag.sh` (and a `make pre-tag` target) that runs tier 0
     and writes a small record keyed by the commit sha; the merge helper and the
     Release workflow check for it.
   - C. A git `pre-push` or `pre-commit` hook running the same script.
   - Recommendation: B for tags, plus a hook for only the fast part (build, vet,
     lint, changed-package tests), kept optional because hooks are per clone and
     the Automata work in many worktrees. The merge helper already stops a merge
     on `check-merge-version.sh`; adding "record exists for this sha" there fits
     its design. Hooks are weaker because `--no-verify` and fresh worktrees skip
     them.

4. What marks a release as tier 2 or tier 3, given about 13 tags a day?
   - A. By version number, as AGENT.md says: `X.Y.0` is tier 2, `X.0.0` is
     tier 3. With 168 minors in 35 days, tier 2 on every minor is not
     practical.
   - B. An explicit release candidate marker chosen by the operator or the
     supervisor, for example a `release-candidate` annotated tag or a file in
     `docs/plans/`, run on a cadence (daily, or every N minors, or before
     anything user-facing is announced). Tier 3 stays the `X.0.0` rule.
   - C. A commit trailer `Release-tier: 2` or `3` read by the Release
     workflow.
   - Recommendation: B for tier 2, and the version number for tier 3 (major is
     rare and unambiguous). The operator owns the cadence number; I am not
     proposing one. Section C of AGENT.md then needs rewording, since it ties
     smoke to every minor today.

5. Do PWA node tests run in CI?
   - A. Local only (today). 353 files, never run by CI, so a PWA break reaches
     main unless the author ran them.
   - B. Add a node job to tier 1, if the suite is quick and has no host-speed
     tests.
   - Recommendation: B only after measuring the suite; if it is under 2 minutes
     and deterministic it belongs in tier 1, because it validates a shipped
     artifact and not a host. I could not measure it.

6. What happens to `kind-smoke`?
   - A. Leave it on PRs. It currently fails on the automated CVE-suppression
     PRs and teaches people to ignore it.
   - B. Make it `workflow_dispatch` plus a tier 3 step, and keep a chart lint
     or `helm template` check on PRs that touch charts.
   - Recommendation: B. A fault in the chart still gets a quick check, and the
     full in-cluster smoke runs when the operator wants it.

7. The timing ceilings.
   - A. Leave them and rely on tier 2 for them.
   - B. Rewrite each to wait for an event or condition instead of asserting an
     elapsed time, then return them to tier 1.
   - Recommendation: A first (cheap, is part of the classification work in
     decision 1), B over time. The audit's list is the starting point.

## What changes if approved

No Automata are filed by this document. If the operator approves, the work
falls into these separable pieces, each one an Automaton, in this order:

1. Classify the Go tests: mark host-speed, load, soak and wall-clock-ceiling
   tests (the audit list is the input) to skip under `-short`; keep the leak
   guard's nested run behind its existing full-mode switch.
2. Add `scripts/pre-tag.sh` and `make pre-tag`, with the per-sha record, and
   teach the supervisor merge helper to require it.
3. Edit `ci.yaml`: short test run, remove duplicated work, optional node job.
4. Edit `release.yaml`: gate on a green CI run for the sha and the tier record
   instead of re-running the Go suite.
5. Edit `kind-smoke.yaml` per decision 6.
6. Rewrite AGENT.md Sections A, C and E and the "Release testing" text to match
   the four tiers and the release-candidate rule, and put the same table in
   `docs/testing.md`.
7. Wire a tier 3 checklist (sandbox e2e, Playwright, the spawn scripts) as one
   runnable entry point so the major run is a command, not a reading list.

Expected result: the push-time CI run falls from about 17.9 min median to about
6 to 8 min; the Release run falls by the 12.5 minutes of duplicate tests; the
full suite and the smoke still run, but at tier 0, 2 and 3 on the operator's
machine where host speed is a known quantity.

## Open facts

Things I could not determine:

- How long the full `go test ./...` takes on the operator's own machine, and
  how long tier 0's changed-package run would take. I did not run the suite and
  the CI runner's time is not the operator's.
- How long `release-smoke.sh`, the PWA node suite, the e2e stories and the
  Playwright suite take. None is in CI, and I did not run them.
- Whether `scripts/perf-rollup-check.sh` exists. The request names it; there is
  no such file in the tree. The nearest existing script is
  `scripts/perf-budget.sh`, which is what I used.
- Why `kind-smoke` fails on the recent PRs. The audit says config; I did not
  open those logs.
- Which exact `plugins/supervisor_test.go` assertion is two-sided. I confirmed
  the file's timing machinery, not the specific assertion.
- Whether the AGENT.md smoke cadence was actually followed at about 13 tags a
  day. The tree holds no per-release smoke records that I could match to tags;
  `docs/testing.md` has a checkpoint section but I did not audit it release by
  release.
- Which tests in the seven heaviest packages are slow because of host speed and
  which are slow because there are many of them. The per-package split above is
  from one CI run, and runs overlap on a small runner, so shifting one package
  will not shift the wall time one-for-one.
- The failing recent Release container job for the base agent image is an
  image and scan matter, not a test matter, and is out of scope here.
- The audit files were used as read; I spot-checked the findings against the
  tree and did not re-derive the full list.
