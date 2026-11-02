# Local reproduction of the open CodeQL alerts (2026-10-27)

**Date:** 2026-10-27
**Version:** v9.22.0 at planning (the branch's base). Not yet released: it ships in the next release after the merge of `automaton/d1d30001`, and this line is updated with that version then.
**Scope:** the local CodeQL run and the code fixes it drove. Files: `internal/gitguard/` (`contain.go`, `policy.go`), `internal/session/` (`git.go`, `manager.go`), `internal/llm/claudecode/backend.go`, `internal/pipeline/quality.go`, `internal/server/api_gzip.go`, `internal/mcp/transport_debug.go`, `internal/server/web/app.js`, their tests, and the story `scripts/test-stories/pwa/TS-826.mjs` with its wrapper. Docs: `CHANGELOG.md`, `docs/testing.md`, `docs/testing-tracker.md`, this file.
**Status:** code Done on the branch; GitHub's default-setup scan runs only after a merge to `main`, so the alerts stay listed as open on GitHub until then.

Summary of the local CodeQL run. The full working files live in `.datawatch/codeql/`, which is
gitignored (`.gitignore` line 1), so they are not committed: `rescan.sh`, `REPRODUCE.md` (dataflow
source, steps and sink for each of the 21 alerts), `baseline/*.sarif`, `out/*.sarif`.

## Phases

| # | Phase | Status |
|---|-------|--------|
| 1 | Reproduce the 21 open alerts with the local CodeQL scan (rule, file and line match) | Done |
| 2 | go/path-injection: the checked value is the one handed to the filesystem (gitguard, session manager, session git helpers, claude-code backend, pipeline quality) | Done |
| 3 | go/command-injection: commit and tag messages go to git on standard input, not into its argument list | Done |
| 4 | go/reflected-xss: `nosniff` and escaping of untyped markup bodies in the gzip middleware and the MCP debug transport | Done |
| 5 | js/xss and js/unvalidated-dynamic-method-call in `app.js`: escape dynamic values, look wait causes up with a `switch` | Done |
| 6 | Final local scan and full verification (`go test ./...`, the web node tests, lint) | Done |
| 7 | Review follow-up: CHANGELOG entry, this plan's fields, tracker section, live tests (header contract, `TS-826`), `datawatch-app` issue [#282](https://github.com/dmz006/datawatch-app/issues/282), plan link | Done |

Shipped in: not yet released (Done on the branch, awaiting merge).

## Result: the local scan does not match the live list

Every one of the 21 open alerts (GitHub `state=open`) is reproduced locally at the same rule, file and
line. The local scan reports 108 results in total, because a local run cannot know about dismissals
and reports the alerts GitHub has dismissed again.

| rule | live open | local |
|---|---|---|
| go/path-injection | 14 | 63 |
| go/command-injection | 2 | 5 |
| go/reflected-xss | 3 | 4 |
| js/unvalidated-dynamic-method-call | 1 | 1 |
| js/xss | 1 | 3 |

All 84 distinct extra rule/file/line triples match an alert GitHub reports as `dismissed`.
`.datawatch/verify/reproduce-local-codeql-alerts.sh` expects 14, 2, 3, 1, 1, so it exits 1 with
"local scan does not reproduce the open alert counts". The expected numbers were not changed.
To compare like with like, filter the local results to the rule/file/line triples of the open alerts.

## Commands

Run from the project root. `CODEQL=.datawatch/codeql/bundle/codeql/codeql` (CLI 2.27.3).
`.github/workflows/codeql.yaml` sets no `queries:` block, so each language uses its default
code-scanning suite.

```bash
.datawatch/codeql/rescan.sh

env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db/go --language=go --build-mode=autobuild --source-root=. --overwrite
env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db/javascript-typescript --language=javascript-typescript --build-mode=none --source-root=. --overwrite
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db/go codeql/go-queries:codeql-suites/go-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out/go.sarif
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db/javascript-typescript codeql/javascript-queries:codeql-suites/javascript-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out/javascript-typescript.sarif
python3 -I .datawatch/verify/codeql_sarif.py list .datawatch/codeql/out

gh api 'repos/dmz006/datawatch/code-scanning/alerts?state=open&per_page=100' --paginate
```

## Parity surface

| Surface | Touched | Reason |
|---|---|---|
| REST | Yes | Every `/api` response gains `X-Content-Type-Options: nosniff`. An untyped HTML, XML or SVG body is sent as `text/plain` and HTML-escaped, with its declared `Content-Length` dropped. JSON and typed bodies are unchanged. Section in `docs/testing-tracker.md`: "Untyped markup bodies and nosniff on API responses". |
| MCP | Header only | The MCP debug SSE transport pins `nosniff` and escapes a payload whose type is missing or HTML. No tool, parameter or result changed, so `docs/mcp.md` and `docs/cursor-mcp.md` need no edit. |
| CLI | No | No command, flag or output changed. Commit and tag messages now reach git on standard input, which is internal to the daemon. |
| comm channel | No | No command or reply changed. |
| YAML/config | No | No new field. The allowed project roots (worktree base, workspace root, `server.allowed_project_roots`) are unchanged, so `docs/config-reference.yaml` and the six registration points are untouched. |
| PWA | Yes | Dynamic values in the task card, progress bar, chat bubble header and Stats cards are escaped; the wait cause chip is chosen by a `switch`. Normal values look the same as before. No new `t('key')`, so no locale change. Live story `TS-826`. |
| Android | Issue filed | `datawatch-app` [#282](https://github.com/dmz006/datawatch-app/issues/282) records the contract change and what the native client must tolerate. No new strings. |
| iPhone/iOS | Issue filed | Same issue, #282. |

Nothing is excluded without a reason: the CLI, comm channel and YAML/config rows are untouched because no
operator-facing command, option or setting changed.

## Phase completion checklist (phase 7)

- Functionality: done per the phase table.
- Unit tests: added for every changed package; see the tracker section above.
- Live test: `TS-826` passed on a daemon built from this tree (2026-10-27); the header contract was read with `curl` on a sandbox daemon. The escape of an untyped markup body has no live route to exercise, so only its unit tests cover it (recorded in the tracker row).
- `go test ./...` and `node --test internal/server/web/*.test.js`: both green on 2026-10-27 (`go test ./...` exit 0 with `TMUX` and `TMUX_TMPDIR` unset; 1223 of 1223 node tests), plus `make lint` (0 issues) and `scripts/check-docs-sync.sh`.
- Documentation checklist: `CHANGELOG.md` `[Unreleased]` entry added; `docs/config-reference.yaml`, `docs/operations.md`, `README.md` and the doc indexes N/A (no config, deployment, interface or doc-file change); `docs/testing-tracker.md` section added; no tracker IDs in operator-facing docs; this plan has a Parity surface section.
- Mobile-Parity Rule: `datawatch-app` #282.
- Localization Rule: N/A, no new `t('key')` usage.
- Version bump: N/A for this unit; the merge helper bumps both `Version` variables (`cmd/datawatch/main.go`, `internal/server/api.go`) at release.
- Commit subjects: the first six commits of this branch used the form `task(d1d30001): ...`; they cannot be rewritten from inside the Automaton, so the merge uses a conventional subject of the form `fix(security): ...` with the trailer `tracker: Untyped markup bodies and nosniff on API responses`.

## Round 3: alerts 661, 714, 715

**Date:** 2026-10-27
**Note (2026-10-28):** the result for 661 below is for the subprocess rewrite on the Automaton branch, which the operator dropped before merge; 661 is dismissed as "won't fix" and stays reported on `main` (see `2026-10-28-codeql-alert-661-decision.md`). The 714 and 715 results stand.
**Scope:** the final local scan for the last three open alerts, after their fixes on branch `automaton/24030001`: 661 `go/command-injection` (`internal/session/subprocess_resume.go`), 715 `go/reflected-xss` (`internal/mcp/transport_debug.go`) and 714 `js/insecure-randomness` (`internal/server/web/app.js`). The scan before the fixes is saved in `.datawatch/codeql/baseline-r3/` (see `docs/plans/2026-10-28-codeql-last-three-alerts.md`). The working files are in the gitignored `.datawatch/codeql/`, so they are not committed.

### Commands

Run from the project root, each under `env -u TMUX -u TMUX_TMPDIR`, with `CODEQL=.datawatch/codeql/bundle/codeql/codeql` (CLI 2.27.3). Both databases were rebuilt with `--overwrite`, and the same default code-scanning suites as the first scan were used (`.github/workflows/codeql.yaml` sets no `queries:` block). The output goes to `out-r3/` so the baseline files stay untouched. No query was excluded and nothing in `.github/` or the code-scanning settings changed.

```bash
env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db/go --language=go --build-mode=autobuild --source-root=. --overwrite
env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db/javascript-typescript --language=javascript-typescript --build-mode=none --source-root=. --overwrite
mkdir -p .datawatch/codeql/out-r3
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db/go codeql/go-queries:codeql-suites/go-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out-r3/go.sarif
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db/javascript-typescript codeql/javascript-queries:codeql-suites/javascript-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out-r3/javascript-typescript.sarif
```

Both databases were rebuilt after the last fix commit, and both analyses wrote their SARIF file to `out-r3/`. The two SARIF files were then compared with `.datawatch/codeql/baseline-r3/` by reducing each result to its rule, file and start line (`python3 -I`, `runs[0].results[].locations[0].physicalLocation`).

### Before and after

| Alert | Rule | File | Baseline line | After |
|-------|------|------|---------------|-------|
| 661 | `go/command-injection` | `internal/session/subprocess_resume.go` | 41 | Gone |
| 715 | `go/reflected-xss` | `internal/mcp/transport_debug.go` | 128 | Gone |
| 714 | `js/insecure-randomness` | `internal/server/web/app.js` | 6982 | Gone |

Whole-scan totals (results; distinct rule/file/line triples in brackets):

| Language | Baseline | After | Change |
|----------|----------|-------|--------|
| Go | 68 (66) | 66 (64) | `go/command-injection` 4 to 3, `go/reflected-xss` 2 to 1 |
| JavaScript/TypeScript | 17 (16) | 16 (15) | `js/insecure-randomness` 1 to 0 |

The Go difference is exactly the two triples above. The JavaScript/TypeScript difference is `js/insecure-randomness` at `app.js:6982`, plus one line-number shift described next.

### Triples not in the baseline

Compared literally by rule, file and start line, the after-scan has **one triple that is not in the baseline**: `js/xss-through-dom` at `internal/server/web/app.js:10893`. The baseline has the same rule and file at line 10882, and that triple is absent from the after-scan. No Go triple is new.

That result is the baseline result moved down by 11 lines, and this was checked on CodeQL's own result identity and not inferred from the line numbers. Every SARIF result carries a `partialFingerprints.primaryLocationLineHash`, a hash of the source line that does not depend on the line number (GitHub uses it to keep an alert the same when code above it moves). Both results carry the same hash and column fingerprint (`fdbd04ea15251b02:1`, column 18), the same rule, the same file and the same message, and both lines read `fBody.innerHTML = html;`. The other `js/xss-through-dom` result (`app.js:141`) sits above the edit and did not move. The fix for the insecure-randomness alert added the 11-line `_autoMicTextareaId` helper at `app.js:7204` (`git diff 3bd9b1c95 HEAD -U0 -- internal/server/web/app.js`), which is above line 10882 and therefore moves every result below it.

Comparison, run with `python3 -I` over both SARIF files: results were reduced to rule, file, start line, `primaryLocationLineHash` and message text, then compared as multisets.

| Check | Go | JavaScript/TypeScript |
|-------|----|-----------------------|
| Literal rule/file/line triples only in the after-scan | 0 | 1 (`js/xss-through-dom`, `app.js:10893`) |
| Results only in the after-scan by rule, file, fingerprint and message | 0 | 0 |
| Results only in the baseline by rule, file, fingerprint and message | 2 (`go/command-injection`, `go/reflected-xss`) | 1 (`js/insecure-randomness`) |

So by the line-independent identity the after-scan is a strict subset of the baseline, and the three alerts are the only results that left it. A literal line-number comparison cannot hold for any fix that adds or removes lines above an existing result in the same file, so the one literal difference is a line shift and not a new finding.

### Result

The three alerts 661, 714 and 715 no longer appear in a fresh local scan with the default suites, and no result appears that was not in the baseline. The only literal difference is `js/xss-through-dom` moving from `app.js:10882` to `app.js:10893` (an 11-line shift from the fix for 714), which the matching fingerprint shows is the same result. The rest of the whole-scan list is unchanged: it still includes alerts GitHub has dismissed (see the result section above), which a local run cannot know about. GitHub's default-setup scan runs only after a merge to `main`, so the three stay listed as open on GitHub until that scan has run; they are expected to close then.
