# upstream-monitoring final gate — read-only

- **Date:** 2026-11-02 (local 2026-11-01 23:24), branch worktree `fb0a0001`
- **Head:** `068d15477` over base `d897c68c1` (merge-base with main)
- **Scope of this session:** run `bash .datawatch/verify/upstream-monitoring-final.sh`, re-run each
  per-task script independently, confirm the seven manual checks by hand, report each result.
  Read-only: nothing fixed, no commits by this session.

## Overall result: FAIL (two named check/script failures, both tooling-side)

| # | Check | Result |
|---|-------|--------|
| 0 | `bash .datawatch/verify/upstream-monitoring-final.sh` | **FAIL** (see F-1, F-2) |
| 1 | `.github/dependabot.yml` parses; covers gomod, docker and github-actions; daily; no auto-merge | **PASS** |
| 2 | `actionlint .github/workflows/upstream-watch.yaml` clean; every `uses:` SHA-pinned | **PASS** |
| 3 | `python3 -I -m unittest scripts/tests/test_upstream_watch.py` passes, no network | **PASS via the project's documented equivalent runner**; the literal form fails on a documented Python loader quirk (see F-3) |
| 4 | `go test ./tests/ci -run VersionBump -count=1` passes; Dependabot go.mod change exempt, human-authored not | **PASS** (literal command is a vacuous pass — no test name matches `VersionBump`; the exemption semantics are proven by the Dependabot/HumanGoMod tests, all green) |
| 5 | `make check-tmux check-test-daemons` and `bash scripts/check-no-unscoped-tmux.sh` | **PASS** |
| 6 | Diff against main limited to the allowed set | **PASS** against the task wording; **FAIL** against the final script's narrower regex (see F-2) |
| 7 | No version bumped; no release number in the new docs | **PASS** |

## Evidence per check

### Check 1 — dependabot configuration: PASS
- `.datawatch/verify/dependabot-config.sh` → exit 0, `dependabot.yml OK` / `PASS: dependabot-config`
  (PyYAML parse, `version: 2`, directory existence checks).
- `.github/dependabot.yml`:
  - **gomod**: `directory: "/"`, `interval: daily`, grouped version-updates (minor/patch) so
    security updates stay ungrouped; no auto-merge key, no `target-branch`.
  - **docker**: `directories: ["/docker/dockerfiles", "/"]` (root covers `Dockerfile.dev`),
    `interval: daily`; comment records the intentional gap (FROM lines built from ARGs) that the
    upstream-watch job covers.
  - **github-actions**: `directory: "/"`, `interval: daily`.
  - No `auto-merge`/`*auto*merge*` keys anywhere; merge policy comment states a human merges every
    Dependabot PR first.

### Check 2 — workflow lint and pinning: PASS
- `actionlint .github/workflows/upstream-watch.yaml` → exit 0 (`actionlint`).
- Exactly two `uses:` lines, both 40-hex commit SHAs:
  - `actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd  # v5`
  - `actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1  # v6`
- `.datawatch/verify/upstream-watch-workflow.sh` → exit 0 (schedule + workflow_dispatch triggers only,
  `contents: read`, no `git push`, runs `scripts/upstream_watch.py`).

### Check 3 — offline unit tests: PASS (documented equivalent runner); literal form quirk
- Documented runner (as fixed in `.datawatch/verify/upstream-watch-tests.sh`):
  `PYTHONDONTWRITEBYTECODE=1 python3 -I scripts/tests/test_upstream_watch.py -v`
  → exit 0, `Ran 19 tests ... OK`. Same `-I` isolation (no PYTHONPATH / user-site / network);
  the test file self-bootstraps `sys.path` from `__file__`.
- Network discipline: no live `urlopen`/`urllib.request` in the test file (gated by the script).
- **F-3 (named, not fixed):** the literal `python3 -I -m unittest scripts/tests/test_upstream_watch.py`
  fails with `ModuleNotFoundError: No module named 'scripts'` (`Ran 1 test ... FAILED (errors=1)`).
  This is the documented Python ≥3.11 `-I` quirk: isolated mode drops the cwd from `sys.path`, and
  the stdlib loader does `__import__("scripts...")` which cannot resolve for any test file on this
  image (Python 3.14). File: `scripts/tests/test_upstream_watch.py`.

### Check 4 — version-bump gate exemption: PASS
- Literal `go test ./tests/ci -run VersionBump -count=1` → exit 0, `ok ... 0.002s [no tests to run]`
  (vacuous: no test name in `tests/ci/check_version_bump_test.go` matches the regex `VersionBump`;
  noted here for accuracy).
- The exemption semantics the check asks about are covered by tests (all PASS,
  `-run 'Dependabot|HumanGoMod' -count=1`):
  - `TestRangePassDependabotNoBump` — Dependabot-authored go.mod range, no bump → pass (exempt).
  - `TestCommitModePassDependabotNoBump` — same in commit mode.
  - `TestRangeFailHumanGoModNoBump` — **human-authored** go.mod change without bump → **fail** (not exempt).
  - `TestRangeFailMixedDependabotAndHuman`, `TestRangeFailSpoofedDependabotAuthor`,
    `TestRangePassDependabotThenHumanMerge`.
- `scripts/check-version-bump.sh` exemption: every non-merge commit author exactly
  `dependabot[bot]`; mixed or spoofed authors follow normal bump rules; authorship explicitly noted
  as unauthenticated, exemption only.
- `.datawatch/verify/dependabot-version-gate.sh` → exit 0 (gofmt, shellcheck -S error, pattern
  `VersionBump|Range|CommitMode|Dependabot` suite `ok`).

### Check 5 — tmux gates: PASS
- `make check-tmux check-test-daemons` → exit 0 (`OK: no daemon cleanup violations`,
  `OK: no unscoped tmux kills`).
- `bash scripts/check-no-unscoped-tmux.sh` → exit 0 (`OK: no unscoped tmux kills`).

### Check 6 — scope of the diff: PASS (task wording) / FAIL (final-script regex, see F-2)
Diff `d897c68c1..068d15477` — exactly these files, all inside the task's allowed set; nothing under
`internal/`, `cmd/`, `README.md`, `CHANGELOG.md`, `docs/` outside plans; no config key surface, no
PWA string (also: final-script gate `scripts/check-no-internal-refs.sh` → `PASS: no internal refs in
user-facing surfaces`):

```
.github/dependabot.yml
.github/workflows/upstream-watch.yaml
docs/plans/2026-10-29-upstream-monitoring-proposal.md
docs/plans/2026-11-01-upstream-monitoring-maintainer-guide.md
docs/plans/README.md
docs/plans/reports/archive/2026-11/upstream-watch-live-smoke.md
scripts/check-version-bump.sh
scripts/tests/test_upstream_watch.py
scripts/upstream_watch.py
tests/ci/check_version_bump_test.go
```

No untracked files. Tracker-ID filename check: no match.

### Check 7 — no version bump, no release number in new docs: PASS
- `cmd/datawatch/main.go` and `internal/server/api.go` are **not** in the diff; at both base and
  head: `var Version = "9.53.0"` (in both files). go.mod/go.sum untouched. The release number is
  taken at merge time as the next patch above main, per the branch's standing.
- New docs (`2026-11-01-upstream-monitoring-maintainer-guide.md`, the delivered-note added to
  `2026-10-29-upstream-monitoring-proposal.md`, the row-198 update in `docs/plans/README.md`):
  no datawatch release number (`x.y.z`) anywhere in the changed/new text. The live-smoke report
 (`docs/plans/reports/archive/2026-11/upstream-watch-live-smoke.md`) cites **upstream** tool versions only, as
  observed advisory evidence (e.g. pnpm 10.33.0, Go stdlib 1.26.6) — not datawatch release numbers.

## Named failures (not fixed, per instructions)

- **F-1** — `.datawatch/verify/upstream-watch-script.sh` fails step "no literal system binary path":
  it greps `/(usr/)?s?bin/[a-z]` and matches **line 1 of `scripts/upstream_watch.py`** — the shebang
  `#!/usr/bin/env python3`. Everything else in that script passes (`py_compile`, stdlib-only AST
  import check, `--help` run). The check pattern collides with the idiomatic portable shebang: as
  written it can only be satisfied by a script with no shebang at all.
- **F-2** — scope allowlist in `.datawatch/verify/upstream-monitoring-final.sh`:
  `docs/plans/[^/]+\.md` does not match the nested report
  `docs/plans/reports/archive/2026-11/upstream-watch-live-smoke.md` (verified: the path fails the exact
  `grep -E '^docs/plans/[^/]+\.md$'`). The task's manual scope wording ("files under docs/plans")
  does cover it, so the two specs disagree; the final script's regex is the failing check.
- **F-3** — literal `python3 -I -m unittest scripts/tests/test_upstream_watch.py`
  `ModuleNotFoundError: No module named 'scripts'`; documented Python ≥3.11 `-I` loader quirk; the
  project's equivalent runner (same suite, 19 tests) passes.
- **F-4 (observation, no failure)** — `go test ./tests/ci -run VersionBump` matches **zero** tests
  (`[no tests to run]`); the command "passes" vacuously while the real coverage sits under
  `Dependabot`/`HumanGoMod` names. Not a fix item here; flagged so the gate wording and the test
  names can be reconciled deliberately.

## Final-script chain status

`bash .datawatch/verify/upstream-monitoring-final.sh` → **exit 1**, aborted at step 3
(upstream-watch-script.sh, F-1). With each step run independently: dependabot-config PASS,
dependabot-version-gate PASS, upstream-watch-script **FAIL** (F-1), upstream-watch-tests PASS,
upstream-watch-workflow PASS, upstream-watch-docs PASS, then the scope check **FAIL** (F-2). Steps
after the scope check (check-no-unscoped-tmux PASS, check-no-internal-refs PASS, actionlint PASS,
tracker-ID filename PASS) were run by hand. If F-1 and F-2 were cleared, the chain would print
`PASS: upstream-monitoring-final`.
