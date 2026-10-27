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
