# The last three CodeQL alerts (2026-10-28)

**Date:** 2026-10-28
**Scope:** the three code-scanning alerts that remain open after the fixes described in `docs/plans/2026-10-27-codeql-local-reproduction.md`: `go/command-injection` in `internal/session/subprocess_resume.go`, `go/reflected-xss` in `internal/mcp/transport_debug.go`, and `js/insecure-randomness` in `internal/server/web/app.js`. This document is appended to by the later tasks of the same plan (fixes, final scan, verification).
**Status:** in progress (reproduction done; fixes not started).

Working files live in `.datawatch/codeql/`, which is gitignored (`.gitignore` line 1), so they are not committed. The SARIF produced by the reproduction run is saved, unchanged, as `.datawatch/codeql/baseline-r3/go.sarif` and `.datawatch/codeql/baseline-r3/javascript-typescript.sarif`.

## Reproduction

Branch `automaton/24030001`, base commit `3bd9b1c95` (v9.28.0). Line numbers on this branch differ from the ones GitHub reports for the last scan of `main`; the table gives both.

| # | Rule | File | GitHub line | Local SARIF line | Reproduced |
|---|------|------|-------------|------------------|------------|
| 1 | `go/command-injection` | `internal/session/subprocess_resume.go` | 41 | 41 | Yes |
| 2 | `go/reflected-xss` | `internal/mcp/transport_debug.go` | 128 | 128 | Yes |
| 3 | `js/insecure-randomness` | `internal/server/web/app.js` | 6983 | 6982 | Yes (one line earlier) |

Whole-scan totals from this run: Go 68 results (`go/path-injection` 45, `go/disabled-certificate-check` 7, `go/command-injection` 4, `go/request-forgery` 4, `go/clear-text-logging` 3, `go/reflected-xss` 2, `go/uncontrolled-allocation-size` 2, `go/email-injection` 1); JavaScript/TypeScript 17 results (`js/insecure-randomness` 1, the rest other rules). The local scan also reports alerts GitHub has dismissed (see the 2026-10-27 plan), so the totals are not the open count; only the three triples above are the subject here.

### Alert 1: `go/command-injection`, `internal/session/subprocess_resume.go:41`

SARIF message: "This command depends on a user-provided value." The SARIF carries four code flows. The sink is the same in all four:

- Sink: `internal/session/subprocess_resume.go:41`, `sess.Task` passed as an argument of `exec.Command("bash", "-c", subprocessWrapper, "bash", sess.Task, ...)` in `subprocessCommand` (line 40 is the `sess` parameter of the function, line 41 the `exec.Command` call). The argument is the `$1` of the wrapper `bash -c "$1"; ...`, so the task text is run as shell code by design for a subprocess (`!cmd`) session.
- Hop common to all four flows: `internal/session/manager.go:2005` (`task` parameter) -> `manager.go:2211` -> `manager.go:2207` (`&Session{Task: task}`) -> `manager.go:2347` (`sess`) -> `manager.go:3767` / `3777` (`sess`) -> `subprocess_resume.go:40` -> `subprocess_resume.go:41`.

The four sources:

1. Chat command: `internal/server/api.go:2049` (`r.Body`, `json.NewDecoder`) -> `api.go:2054` (`.Text`) -> `internal/router/commands.go:663-664` (`text`, `strings.TrimSpace`) -> `commands.go:822` / `825` (slice, `TrimSpace`, struct literal `Text`) -> `api.go:2054` / `2056` / `2065` (`cmd`) -> `api.go:2143` (`cmd.Text`) -> `manager.go:2005`.
2. Pipeline spec: `internal/server/api.go:3503` (`r.Body`) -> `api.go:3508` (`.Spec`) -> `internal/pipeline/adapter.go:18-19` -> `pipeline.go:293-323` (`splitPipelineSpec`, `part`, task `Title`) -> `pipeline.go:298-308` (`tasks`) -> `adapter.go:23-27` -> `executor.go:72-100` -> `pipeline.go:106-133` (`ReadyTasks`) -> `executor.go:177-183` (`t.Title`) -> `internal/pipeline/session_adapter.go:34-47` -> `manager.go:2005`.
3. Session start: `internal/server/api.go:4782` (`r.Body`) -> `api.go:5241` (`.Task`) -> `manager.go:2005`.
4. Remote message: `internal/server/api.go:2326` (`msgBytes`) -> `api.go:2336` -> `api.go:2364` (`.Data`) -> `api.go:2373` (`.Task`) -> `manager.go:2005`.

Reproduced: yes, rule, file and line match the GitHub alert.

### Alert 2: `go/reflected-xss`, `internal/mcp/transport_debug.go:128`

SARIF message: "Cross-site scripting vulnerability due to user-provided value." Two code flows, same source and same sink.

- Sink: `internal/mcp/transport_debug.go:128`, `o.w.Write(p)` in `sseCloseObserver.Write` (the branch taken when `o.escape` is false; line 125 is the parameter `p`). The `html.EscapeString` branch two lines further down does not stop the alert, because the unescaped branch is reachable from the same `p`.
- Source: `internal/server/proxy.go:273` (`r.URL.Path`, `strings.TrimPrefix`) -> `proxy.go:420` (`serverName`) -> `proxy.go:439` (`serverName`) -> `proxy.go:493` / `494` (`strings.ReplaceAll` building the rewritten page, a `string` -> `[]byte` conversion at `proxy.go:496`) -> `proxy.go:420` (`rewritePWAContent` result) -> `proxy.go:423` (`rewritten`) -> `transport_debug.go:125` (`p`) -> `transport_debug.go:128`.
- Flow 2 adds, between `proxy.go:494` and `transport_debug.go:125`, the gzip middleware: `internal/server/api_gzip.go:188` / `197` (`b`) -> `api_gzip.go:328` / `334` / `275` / `277` (`encode`).

Reproduced: yes, rule, file and line match the GitHub alert. Note that the same scan reports a sibling `go/reflected-xss` at `internal/server/proxy.go:423`, which is not one of the three alerts of this plan.

### Alert 3: `js/insecure-randomness`, `internal/server/web/app.js:6982`

SARIF message: "This uses a cryptographically insecure random number generated at Math.random() in a security context." Three code flows, all with the same source and sink; they differ only in the branch taken inside `_voiceTranscribeBlob` (`app.js:7306` and `7309` versus the direct path).

- Real `Math.random()` source: `internal/server/web/app.js:7245`, `ta.id = '_autoMicTa_' + Math.random().toString(36).slice(2, 10);`. The other candidate named in the task, `app.js:6809` (`uploadName`), is a different `Math.random()` use and is not on this alert's dataflow. The scan reports no other `js/insecure-randomness` result.
- Dataflow: `app.js:7245` (`Math.random()`, `.toString(36)`, `.slice(2, 10)`, the concatenated string) -> `[post update] ta[id]` -> `app.js:7250`, `7256` (`ta.id`) -> `app.js:7305` (`targetId`) -> `app.js:7306` / `7309` (varies by flow) -> `app.js:7325` -> `app.js:7342` (`{ filename, ..., targetId }` object) -> `app.js:6981` (`opts`) -> `app.js:6982` (`const { filename, sessionId, targetId, keepId } = opts || {}`).
- Sink: `app.js:6982`, the `targetId` binding in `_voiceTranscribeBlob`, where the id of the dictation target element (generated from `Math.random()` at line 7245 when the textarea has no id) is destructured from `opts`. The same function passes `targetId` on to `_voiceKeepFailed` (line 7009). The SARIF does not say which later use CodeQL counts as the security context; that is for the fix task to establish.

Reproduced: yes, rule and file match; the line is 6982 locally and 6983 on GitHub (the file has drifted by one line).

### Commands run

All commands were run from the project root, each under `env -u TMUX -u TMUX_TMPDIR`. `.datawatch/codeql/bundle/` and `.datawatch/codeql/rescan.sh` were not present in this worktree, so the CLI was reinstalled and the scans were run command by command.

CLI install. The release is `codeql-bundle-v2.27.3` of `github/codeql-action`; the bundle was downloaded with `gh`, its published checksum compared to the downloaded file (equal), and the archive unpacked:

```bash
gh release download codeql-bundle-v2.27.3 -R github/codeql-action -D <scratch dir> -p 'codeql-bundle-linux64.tar.zst' -p 'codeql-bundle-linux64.tar.zst.checksum.txt'
cat <scratch dir>/codeql-bundle-linux64.tar.zst.checksum.txt && sha256sum <scratch dir>/codeql-bundle-linux64.tar.zst
# both print 5e7607e1b721728955e31f5402d460ad227cbd2ffed0d4cc610116755796150e
mkdir -p .datawatch/codeql/bundle
tar --zstd -xf <scratch dir>/codeql-bundle-linux64.tar.zst -C .datawatch/codeql/bundle
.datawatch/codeql/bundle/codeql/codeql version     # CodeQL command-line toolchain release 2.27.3.
```

The archive was downloaded to the session scratch directory outside the repository. It was unpacked into the gitignored `.datawatch/codeql/bundle/` rather than the scratch directory, because the scratch directory has a quota of about 1 GB and the first unpack there stopped with "Disk quota exceeded". `git status` stays clean.

Databases, default code-scanning suites (`.github/workflows/codeql.yaml` sets no `queries:` block), and the baseline copy:

```bash
CODEQL=.datawatch/codeql/bundle/codeql/codeql
env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db/go --language=go --build-mode=autobuild --source-root=. --overwrite
env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db/javascript-typescript --language=javascript-typescript --build-mode=none --source-root=. --overwrite
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db/go codeql/go-queries:codeql-suites/go-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out/go.sarif
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db/javascript-typescript codeql/javascript-queries:codeql-suites/javascript-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out/javascript-typescript.sarif
mkdir -p .datawatch/codeql/baseline-r3 && cp .datawatch/codeql/out/*.sarif .datawatch/codeql/baseline-r3/
```

Timing: Go database 46 s, JavaScript/TypeScript database 14 s, Go analysis 56 s, JavaScript/TypeScript analysis 33 s. All four commands exited 0.

The rule, file, line and code flows above were read from `.datawatch/codeql/baseline-r3/*.sarif` with a short `python3 -I` script over `runs[0].results[].locations[0].physicalLocation` and `codeFlows[].threadFlows[].locations[]`. No code, query, suppression, workflow or GitHub setting was changed.

### Result

| # | Alert | Reproduced locally |
|---|-------|--------------------|
| 1 | `go/command-injection`, `subprocess_resume.go:41` | Yes |
| 2 | `go/reflected-xss`, `transport_debug.go:128` | Yes |
| 3 | `js/insecure-randomness`, `app.js:6982` (GitHub 6983) | Yes |
