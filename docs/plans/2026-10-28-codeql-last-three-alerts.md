# The last three CodeQL alerts (2026-10-28)

**Date:** 2026-10-28
**Scope:** the three code-scanning alerts that remain open after the fixes described in `docs/plans/2026-10-27-codeql-local-reproduction.md`: `go/command-injection` in `internal/session/subprocess_resume.go`, `go/reflected-xss` in `internal/mcp/transport_debug.go`, and `js/insecure-randomness` in `internal/server/web/app.js`. This document is appended to by the later tasks of the same plan (fixes, final scan, verification).
**Version:** v9.28.0 at planning; the fixes ship in the next release cut from `main` (the version bump belongs to that release commit, not to this branch).
**Phases:** 1 reproduce the alerts (Done) · 2 subprocess launch fix (Done: no shell at all, see Decision) · 3 MCP debug transport fix (Done) · 4 PWA id fix (Done) · 5 re-scan (Done) · 6 final gate (Done, see Final result) · 7 rework after the first rules-review (Done) · 8 rework after the second rules-review: remove the shell (Done).
**Status:** Done on the branch, pending the GitHub rescan after merge. The subprocess launch now meets the spec: no shell is started by the daemon or by the helper. This changes what a `--shell` / `subprocess: true` task may contain (see the migration note in the CHANGELOG and below). The three target alerts are gone from a fresh local scan of the fixed branch tree and no new alert appeared. GitHub still lists #661, #714 and #715 as open until it re-scans `main` after the merge.

## Decision: the subprocess launch starts no shell (spec step 2)

**Spec step 2 asked for a subprocess launch with no shell at all, and this branch now does that.** The first two passes kept `bash -c` in the helper and recorded the conflict with the documented `--shell` behaviour as an open question; the rules-review rejected that because the spec is the instruction and the clearance of #661 came only from the task crossing a process boundary. This pass follows the spec.

- **What runs now.** The daemon re-runs its own binary as `<exe> __subprocess-run <rc-file> -- <task>`. The helper splits the task in `internal/session/subprocess_split.go` (quotes `'…'` and `"…"`, backslash escapes, a leading `~/`; nothing else is expanded) and runs `exec.Command(argv[0], argv[1:]...)`. No code on the launch path names a shell or `-c`; `TestSubprocessLaunchPathNamesNoShell` fails if one is added.
- **What is refused.** An unquoted `|`, `&`, `;`, `<`, `>`, `(`, `)`, `$` or backtick, a `$` or backtick inside double quotes, an unquoted newline, an unterminated quote, a trailing backslash, an empty task, a NUL byte and an unsafe rc path. Refusal happens in the daemon (no command is returned, nothing starts) and again in the helper (exit 2, `subprocess.rc` = 2). The session ends `failed` with `subprocess task uses shell syntax (...)` in its log. Refusing, instead of passing the character through as literal text, is deliberate: a task like `a && b` would otherwise run `a` with the arguments `&&` and `b` and report success.
- **What this breaks, and the migration.** Tasks that relied on a shell (`make test && make lint`, pipes, redirects, `$VAR`, multi-line scripts, `cd x && y`, `NAME=value cmd`) now fail at start. The migration is explicit and local to the task: name the shell, `--shell "bash -c 'make test && make lint'"`, or run a script file, `--shell "~/jobs/run.sh"`. `docs/howto/result-panel.md` (which used a multi-line shell task) now shows the script-file form. `docs/commands.md`, `docs/datawatch-definitions.md`, `docs/operations.md`, the `--subprocess` flag help and the code comments no longer say "run via `bash -c`". `scripts/release-smoke.sh` and `scripts/test-stories/TS-801.sh`, which post shell-syntax tasks, wrap them with `bash -c` + `shlex.quote`.
- **What this does not remove.** An operator who names `bash -c '…'` has chosen a shell as the program; that is a program choice, not the launcher interpreting a request. Anyone who can start a subprocess session can still run any program as the daemon user, as with any command runner. Narrowing who may start one (a config gate, token scoping) is a separate decision and would need the full config-parity set; it is not part of this change.
- **Not done on purpose.** `POST /api/schedules` still accepts a shell-syntax task and the session fails when it fires, instead of answering 400 at creation. Failing fast at creation would add a REST error contract (and mobile and docs parity) that this alert fix does not need; it is a reasonable follow-up. Schedules already stored on a running daemon are not rewritten; the operator edits any that use shell syntax. (The one pending `imap-hourly-rules` schedule on the local daemon is a bare program path, so it is unaffected; it was read, not changed.)
- **What the CodeQL clearance means.** `go/command-injection` (#661) is not reported on the local scan of this tree. The task is now data for `exec.Command` with the program chosen by the operator's task text and no shell in the path; there is no `bash -c` for the scanner (or a reader) to follow.
- **Operator's earlier question is answered by the spec.** The earlier open options (accept as built, no shell, gate who may start a shell) are resolved as "no shell"; the gate remains available as a later, separate plan.


Working files live in `.datawatch/codeql/`, which is gitignored (`.gitignore` line 1), so they are not committed. The SARIF produced by the reproduction run is saved, unchanged, as `.datawatch/codeql/baseline-r3/go.sarif` and `.datawatch/codeql/baseline-r3/javascript-typescript.sarif`.

## What changed

| # | Alert | Change | Files |
|---|-------|--------|-------|
| 1 | #661 `go/command-injection` | The daemon launches its own binary as a helper with a fixed argument list. The helper splits the task with shell-style quoting only and runs the program directly: no shell anywhere, shell syntax refused. Validation fails closed. | `internal/session/subprocess_resume.go`, `subprocess_helper.go`, `subprocess_split.go`, `manager.go`; docs and scripts listed in Decision |
| 2 | #715 `go/reflected-xss` | `sseCloseObserver` sends every payload through `escapePayload`: a stream with no type or an HTML type is re-typed and HTML-escaped; a declared `text/event-stream` has `<` and `>` rewritten as JSON `\u003c` / `\u003e`. `X-Content-Type-Options: nosniff` is pinned on observed streams, and (added in the rework below) on every request the listener answers. | `internal/mcp/transport_debug.go` |
| 3 | #714 `js/insecure-randomness` | A textarea without an id gets `_autoMicTa_<n>` from a page-level counter; `Math.random()` is gone from the voice path. | `internal/server/web/app.js` |
| 4 | (rework) | The unused `session_last_activity_title` locale string was removed from the five locale files so the existing "removed title key is absent" test passes. | `internal/server/web/locales/*.json` |

## Downstream review (Security-Fix Downstream-Review Rule)

Token: `sec-downstream: reviewed`. Each fix lists the surface it touches, the call sites mapped, what each expects, and the end-to-end result. The live checks used a sandbox daemon built by `make build` on this branch (loopback, REST `18081`, MCP SSE `19091`, data under the gitignored `.datawatch/sandbox/`), never the production daemon. Commands and observed output are in `docs/testing.md` ("CodeQL alerts on subprocess launch, MCP debug transport and dictation ids") and `docs/testing-tracker.md` ("MCP debug transport: nosniff header and JSON-escaped event streams").

### Fix 1: subprocess launch

- **Surface:** how the daemon starts a subprocess session, what a task may contain, what it records (`subprocess.pid`, `subprocess.rc`) and what it later reads back.
- **Call sites mapped:** `Manager.runSubprocess` (only caller of `subprocessCommand`; writes `subprocess.pid` from `cmd.Process.Pid`); `reattachSubprocess` / `subprocessAlive` (restart path: compares `/proc/<pid>/cmdline` with the task, which the helper's argument list still contains); `readSubprocessRC` (reads the helper-written `subprocess.rc`); `subprocessEnv` (sets `cmd.Env` on the helper; the helper's `exec.Command(argv[0], ...)` has no `Env`, so the program inherits it, confirmed by `TestSubprocessResultChildActuallyReceivesToken`); the `init()` hook (any binary that links the session package answers to `__subprocess-run` as first argument); the only producers of subprocess tasks: `POST /api/schedules` (`subprocess`), `datawatch schedule spawn --shell|--subprocess`, and the scripts `scripts/release-smoke.sh` and `scripts/test-stories/TS-801.sh` (updated). Chat `!cmd` does not create subprocess sessions (no caller of `StartOptions.Subprocess` outside the scheduler path).
- **Dependent behaviour checked:** exit status recorded (1, 3, 126, 127, signal 143), a task starting with `-` (program name, 127), quoted shell syntax arrives as literal arguments, a leading `~/` expands (the how-to's `--config ~/workspace/imap.yaml` still works), an rc directory named like shell syntax, an in-place binary upgrade (`" (deleted)"` suffix stripped), the `DATAWATCH_*` environment reaches the program, restart re-attach by cmdline. Windows and macOS builds of the touched packages compile.
- **What breaks (intended):** every task that needs shell syntax; see Decision for the migration and for the docs and scripts already updated. The one pending production schedule inspected (`imap-hourly-rules`) is a bare program path.
- **End to end:** sandbox daemon built by `make build` on this branch (loopback `18081`, data under the gitignored `.datawatch/sandbox/`). `POST /api/schedules` (`type: spawn`, `subprocess: true`, `run_at: now`): (a) `echo hello 'a; b | c && $HOME' "x y"` completed with output `hello a; b | c && $HOME x y`; (b) `echo one; echo two | tr a-z A-Z && exit 3` ended `failed` with `[subprocess] refused: subprocess task uses shell syntax (';' outside quotes) ...`, no process started; (c) `bash -c 'echo one; echo two | tr a-z A-Z && exit 3'` ended `failed` with `one` / `TWO` and `subprocess.rc` = `3`; (d) `no-such-program-xyz --flag` ended `failed`, `subprocess.rc` = `127`. A restart-reattach run was not repeated live; `subprocess_resume_test.go` covers it.
- **Bug-fix regression test:** this is a spec-required behaviour change, not a runtime bug with a failing input on base; the guard is `TestSubprocessLaunchPathNamesNoShell`, which fails when `exec.Command("bash", "-c", "--", task)` is put back in the helper (checked by making that edit, running the test, restoring the file).


### Fix 2: MCP debug transport

- **Surface:** response headers and event-stream bytes of the MCP SSE listener (`mcp.sse_port`).
- **Call sites mapped:** `ObserveSSEHandler` wraps the whole listener mux. A stream request (`GET` with `Accept: text/event-stream`) goes through `sseCloseObserver` (`WriteHeader`, `Write`, `Flush`); everything else (`POST /message`, a `GET /sse` without that `Accept`, `GET /api/mcp/tools`) bypasses the observer. mcp-go writes the stream and sets `Content-Type: text/event-stream` itself. Consumers: MCP clients (Claude Code, Cursor, the channel bridge, mobile clients that use the MCP stream) which parse `data:` lines as JSON-RPC.
- **Dependent behaviour checked:** a client that JSON-decodes sees the same value; `&`, quotes, an `endpoint` URL line and a literal `&lt;` arrive unchanged; `Write` reports the caller's byte count although the escaped form is longer; `Flush` still forwards, so the stream stays live; informational statuses do not skip pinning. A client that compared raw bytes of a `<` or `>` in a non-JSON line would see the escape; none is known, and mcp-go already writes `<` and `>` as `\u003c` / `\u003e` inside its JSON.
- **End to end found a gap, now fixed:** the first live run showed no `nosniff` on requests that bypass the observer. `ObserveSSEHandler` now sets the header for them too, covered by `TestSSEObserver_UnwrappedRequestsCarryNoSniff` (fails without the line, passes with it). After the fix both shapes returned `nosniff`; a `tools/call` with the name `<script>alert(1)</script>` came back as `tool '\u003cscript\u003e...' not found` with no `<` or `>` in the stream, and the decoded message equalled the original.

### Fix 3: dictation field ids

- **Surface:** the DOM id given to a textarea by `autoAttachMics`.
- **Call sites mapped:** `autoAttachMics` (the only place that generates it); the mic button's `data-mic-for` and its click handler (`startGenericVoiceInput(ta.id, btn)`); the `MutationObserver` that re-runs `autoAttachMics`; `micButtonHTML` (explicit ids supplied by callers, not generated); no CSS, test or server code selects `_autoMicTa_`.
- **Dependent behaviour checked:** ids stay unique within a page load (module counter), an existing id is kept, the counter resets on reload which is harmless because the DOM is rebuilt too. The id is not a token or capability.
- **End to end:** headless Chrome against the sandbox PWA: `_autoMicTa_1`, `_autoMicTa_2`, `keep-me`; every mic button resolves to its textarea; no page error. The microphone itself was not exercised (needs a Whisper backend and a microphone).

## Parity surface

The PWA, MCP and session surfaces are touched as security hardening; the one operator-visible behaviour change is that a subprocess task is one program, not a shell command line. No new capability or config field is added.

| Surface | Touched | Reason |
|---------|---------|--------|
| REST | Behaviour only | No route, status or body change. `POST /api/schedules` with `subprocess: true` still accepts the request; a task containing shell syntax now fails when the session starts (log says why). Field description in `docs/datawatch-definitions.md` updated. |
| MCP | Yes (wire only) | The MCP SSE listener adds `nosniff` and escapes `<` / `>` in event streams as JSON escapes (decoded value unchanged). No tool is added or changed. See the tracker section for the contract. |
| CLI | Yes (help text, behaviour) | `datawatch schedule spawn --shell` and `--subprocess` flags are unchanged; the `--subprocess` help and `docs/commands.md` say "one program, no shell" and name `bash -c` as the way to get one. |
| comm channel | No | `!cmd` does not start subprocess sessions; nothing to change. |
| YAML/config | No | No field added or changed. A gate for who may start a subprocess session would be a separate plan with the full config-parity set. |
| PWA | Yes (internal) | Only a generated DOM id changed. Nothing operator-visible. |
| Android | Excluded | No API or layout contract changed; no native counterpart reads these ids or the MCP stream's raw bytes. |
| iPhone | Excluded | Same reason as Android. |

**Mobile-parity decision (B4):** `mobile-parity: no datawatch-app issue needed`. The PWA change is a generated element id that no operator sees, and no REST or WebSocket contract mobile clients parse changed. The MCP stream's `<` / `>` escape does not change any decoded JSON-RPC value. The release supervisor still decides at merge whether to file a courtesy issue.

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

## Re-scan

The scan was repeated on the current branch tree (all fixes committed, working tree clean) with the same CodeQL CLI (bundle `codeql-bundle-v2.27.3`, CLI 2.27.3), the same build modes and the same default code-scanning suites as the reproduction. Fresh databases were built into `.datawatch/codeql/db-r4/` and the SARIF saved to `.datawatch/codeql/out-r4/`; the reproduction SARIF stays in `.datawatch/codeql/baseline-r3/`. All four commands exited 0 under `env -u TMUX -u TMUX_TMPDIR`:

```bash
CODEQL=.datawatch/codeql/bundle/codeql/codeql
env -u TMUX -u TMUX_TMPDIR mkdir -p .datawatch/codeql/db-r4 .datawatch/codeql/out-r4
env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db-r4/go --language=go --build-mode=autobuild --source-root=. --overwrite
env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db-r4/js --language=javascript-typescript --build-mode=none --source-root=. --overwrite
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db-r4/go codeql/go-queries:codeql-suites/go-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out-r4/go.sarif
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db-r4/js codeql/javascript-queries:codeql-suites/javascript-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out-r4/javascript-typescript.sarif
env -u TMUX -u TMUX_TMPDIR python3 -I .datawatch/codeql/compare.py
```

`compare.py` (gitignored with the other working files) reads `ruleId`, file, start line and `partialFingerprints` of every result in both SARIF files and matches results by (rule, file, fingerprint), never by line. Every result in both scans carries a fingerprint.

### 1. Target alerts

All three are absent from the new SARIF:

| Alert | Baseline | Now |
|-------|----------|-----|
| `go/command-injection`, `internal/session/subprocess_resume.go` | line 41 | absent |
| `go/reflected-xss`, `internal/mcp/transport_debug.go` | line 128 | absent |
| `js/insecure-randomness`, `internal/server/web/app.js` | line 6982 | absent |

### 2. New alerts, judged by fingerprint

No result with a new (rule, file, fingerprint) exists in either scan. The only differences from the baseline are the three removals above and one result whose line moved:

| Rule | File | Baseline line | Now | Fingerprint |
|------|------|---------------|-----|-------------|
| `js/xss-through-dom` | `internal/server/web/app.js` | 10882 | 10893 | identical |

Rule, file and fingerprint are unchanged; the line moved by 11 because the fix inserted 11 lines earlier in `app.js`. This is the same alert as the baseline one (the same `fBody.innerHTML = html` statement), not a new one. No other result moved.

### 3. Counts per rule (from the SARIF files)

| Rule | Before | After |
|------|--------|-------|
| `go/path-injection` | 45 | 45 |
| `go/disabled-certificate-check` | 7 | 7 |
| `go/command-injection` | 4 | 3 |
| `go/request-forgery` | 4 | 4 |
| `go/clear-text-logging` | 3 | 3 |
| `go/reflected-xss` | 2 | 1 |
| `go/uncontrolled-allocation-size` | 2 | 2 |
| `go/email-injection` | 1 | 1 |
| **Go total** | **68** | **66** |
| `js/disabling-certificate-validation` | 6 | 6 |
| `js/incomplete-multi-character-sanitization` | 3 | 3 |
| `js/bad-code-sanitization` | 2 | 2 |
| `js/bad-tag-filter` | 2 | 2 |
| `js/xss-through-dom` | 2 | 2 |
| `js/double-escaping` | 1 | 1 |
| `js/insecure-randomness` | 1 | 0 |
| **JavaScript/TypeScript total** | **17** | **16** |

### Outcome

The three target alerts no longer appear in a fresh local scan, no genuinely new alert exists, and no fix was needed in this task. No suppression, query exclusion, workflow, code-scanning configuration or GitHub setting was changed. The local scan still reports alerts GitHub has dismissed (see the 2026-10-27 plan), so the remaining totals are not the open count on GitHub.

## Rework after the rules-review (2026-10-27, evening)

The rules-review of the first pass found real problems. What was done about each, and what could not be:

| Finding | Handling |
|---------|----------|
| Launch still runs a shell; spec step 2 not met; operator not asked; plan did not disclose it; CHANGELOG overstated | First handled by disclosing the shell as an open decision. The second rules-review rejected that (the spec is the instruction), so it was superseded: the helper now runs the program directly with no shell. See Decision at the top and "Second rework" below. |
| Regression test not demonstrated for the subprocess change | `TestSubprocessCommandDaemonLaunchIsNotAShell` pins the daemon side. After the second rework `TestSubprocessLaunchPathNamesNoShell` also pins the helper side and fails if a shell is put back (checked). |
| Tracker lines naming a renamed test and "byte for byte" | Updated; the row now names the three current tests and describes the JSON escape. |
| No tracker section for the MCP wire contract (B1, checklist item 6) | Added "MCP debug transport: nosniff header and JSON-escaped event streams". Token: `tracker: MCP debug transport: nosniff header and JSON-escaped event streams`. |
| No `docs/testing.md` entry for the three fixes | Added with description, steps, expected and actual results. |
| Plan lacked Parity surface, Version and Phases | Added. |
| `sec-downstream: reviewed` missing; no call-site mapping or click-through | Added (Downstream review). The end-to-end run found a real gap: requests the MCP observer does not wrap carried no `nosniff`. Fixed in `ObserveSSEHandler`, with a test that fails without the fix. |
| Commit bodies cut off mid-sentence, "Verification: not run", no rules token; subjects use `task(24030001)` | The first-pass commits are already in the branch history and are not rewritten here (the pipeline commits automatically and this task is told not to run `git commit`, so it cannot rewrite them). That body text is produced by the pipeline's commit helper, not written by the task. The commit for this rework carries a conventional `fix(security):` subject and the tokens below. The reviewer should treat the earlier commit messages as a known pipeline limitation. |
| `go test ./internal/session/` failed on `TestNoNewTmpLeakEntries`; not confirmed on base | Confirmed on base: see Final result. It is a `/tmp` disk-quota failure of `TestEndpointMeasureDeterministic`, not caused by this branch. |
| Node suite had one failing test | Fixed by removing the unused `session_last_activity_title` string from the five locales. 1317 of 1317 pass. |
| `make build` / `make lint` not run | Both run, both clean (see Final result). |
| CodeQL CLI unpacked inside the repository instead of a new empty directory outside it | Not changed. This pipeline may only write under the project directory, so a location outside it was not available. The bundle lives in the gitignored `.datawatch/codeql/bundle/`, nothing from it is tracked, and it can be deleted with `rm -rf .datawatch/codeql`. |
| No mobile-parity decision | Recorded in Parity surface. |

Rules that fired in this rework (token `rules:`): Command Timing Rule, RTK, Git Discipline, Planning Rules, Phase Completion Checklist, General documentation checklist (items 1, 6, 7, 8), Bug testing, Security-Fix Downstream-Review Rule, Mobile-Parity Rule, per-sprint rules audit sections A and B.

Per-commit tokens for this rework: `tests: see Final result (one environmental failure, confirmed on base)` · `version: N/A, release commit bumps both files` · `changelog: added` · `readme: N/A, release commit` · `backlog: N/A` · `id-check: clean` (`scripts/check-no-internal-refs.sh` passes; the matches `grep` prints in `CHANGELOG.md` are older entries) · `leak-check: clean` · `node-check: ok` · `make-build: ok` · `lint: clean` · `tracker: MCP debug transport: nosniff header and JSON-escaped event streams` · `locales: N/A, one unused string removed, nothing added` · `mobile-parity: none needed, reason in Parity surface` · `plans: N/A, scanner alerts have no B-number` · `smoke-extended: N/A, no new operator-facing surface; sandbox live checks recorded` · `sec-downstream: reviewed` · `ci: pending, nothing pushed`.


## Second rework after the rules-review (2026-10-27, late evening)

The second rules-review found one blocker: the helper still ran `bash -c` on the task, so spec step 2 (no shell) was not met, a test pinned shell semantics, and disclosing the conflict did not resolve it. Handling:

| Finding | Handling |
|---------|----------|
| Helper runs `bash -c` on the request-supplied task (spec step 2) | Removed. `internal/session/subprocess_split.go` splits the task with shell-style quoting and `subprocess_helper.go` runs `exec.Command(argv[0], argv[1:]...)`. Shell syntax is refused by the daemon and the helper. |
| `TestSubprocessCommandRunsTaskAsShell` pins shell semantics | Replaced by `TestSubprocessCommandRunsTaskWithoutShell`, `TestSubprocessShellSyntaxIsRefused`, `TestSubprocessHelperRunsProgramWithLiteralArguments`, `TestSubprocessExplicitShellProgramStillWorks` and `TestSubprocessLaunchPathNamesNoShell`. |
| The clearance came from a process boundary, not from removing the risk | The path is removed; `out-r6` (Go) no longer reports #661 and reports nothing new. |
| Plan and CHANGELOG disclose a retained shell | Rewritten: Decision section, CHANGELOG entry with the migration, docs (`commands.md`, `datawatch-definitions.md`, `operations.md`, `howto/result-panel.md`), flag help and code comments. |
| Callers that relied on shell syntax | `scripts/release-smoke.sh` and `scripts/test-stories/TS-801.sh` wrap their scripts in `bash -c` + `shlex.quote`; `manager_subprocess_env_test.go` names bash for its pipe. |
| Commit subjects use `task(24030001)` and carry no rules token; bodies say "Verification: not run" | Not changeable from inside the task: the pipeline writes commit messages and this task is told not to run `git add` or `git commit`. The final line of this task (`DATAWATCH_COMPLETE: …`) becomes the commit message and uses a conventional `fix(security):` subject. Earlier commits stay as they are. |

A mistake during this rework: a shell heredoc that quoted a docs snippet ended early, and its tail ran as commands on this machine. One of them created a scheduled spawn (`d9d34745`, task `~/workspace/jobs/imap-run-rules.sh`) on the local production daemon. It was cancelled within about a minute, before it fired; no session was created and no file was written. Nothing else in it ran.

Rules that fired: Command Timing Rule, RTK, Git Discipline, Planning Rules, Phase Completion Checklist, General documentation checklist (items 1, 6, 7, 8), Bug testing, Security-Fix Downstream-Review Rule, Mobile-Parity Rule, per-sprint rules audit sections A and B.

Per-commit tokens for this rework: `rules: see list above` · `sec-downstream: reviewed` (Downstream review, Fix 1 rewritten) · `tests: all packages pass except one environmental test, see Gate results` · `version: N/A, release commit bumps both files` · `changelog: added` · `readme: N/A, release commit` · `backlog: N/A` · `id-check: clean` · `leak-check: clean` · `node-check: ok` · `make-build: ok` · `lint: clean` · `tracker: Subprocess session launch: one program, no shell` · `locales: N/A` · `mobile-parity: none needed, reason in Parity surface` · `plans: N/A, scanner alerts have no B-number` · `smoke-extended: N/A, the release smoke needs the release harness; the two scripts it changed pass bash -n`.

Gate results for this rework (2026-10-27 23:40-23:5x, every command under `env -u TMUX -u TMUX_TMPDIR`): `make build` exit 0; `make lint` exit 0 (`golangci-lint` 0 issues, internal-ref audit clean, toolchain pins exact); `node --test internal/server/web/*.test.js` 1317 of 1317; `scripts/check-docs-sync.sh` in sync; `GOOS=windows` and `GOOS=darwin go build ./internal/session/` pass; Go CodeQL re-scan `out-r6`: 68 -> 66 results, `go/command-injection` 4 -> 3 (#661 `GONE`), `go/reflected-xss` 2 -> 1 (#715 `GONE`), no `NEW`. Full `go test ./...`: a first run had four packages (`cmd/datawatch`, `internal/memory`, `internal/router`, `internal/server`) fail to link with `No space left on device` on `/tmp`; run again one at a time (`go test -p 1`) all four build and `cmd/datawatch`, `internal/memory` and `internal/router` pass. Every other package passed in the first run. Two tests still fail in a default run, both from the shared `/tmp` disk quota and both already shown to fail identically on the base commit (see Final result): `TestEndpointMeasureDeterministic` in `internal/server` (`copy_file_range: disk quota exceeded`, automata list sizes `90408 vs 88732`) and `TestNoNewTmpLeakEntries` in `internal/session`, which wraps it. `go test ./internal/session/ -run 'Subprocess|SplitSubprocess|Spawn|Schedule'` passes. An attempt to dodge the quota with `GOTMPDIR` inside the worktree put test temp dirs inside the repo and failed unrelated project-root tests; those results were discarded.

## Final result

Run on branch `automaton/24030001` (base `3bd9b1c95`, v9.28.0), 2026-10-27 23:00-23:35 local time, every command under `env -u TMUX -u TMUX_TMPDIR`. No suppression comment, query exclusion, workflow, code-scanning configuration or GitHub setting was edited.

### Re-scan of the final tree

CodeQL CLI 2.27.3, same build modes and default suites as before, fresh databases in `.datawatch/codeql/db-r5/`, SARIF in `.datawatch/codeql/out-r5/`, compared to the baseline by (rule, file, fingerprint): Go 68 -> 66 results (`go/command-injection` 4 -> 3, `go/reflected-xss` 2 -> 1), JavaScript/TypeScript 17 -> 16 (`js/insecure-randomness` 1 -> 0). The three target results are `GONE`. No result is `NEW`. One existing result moved (`js/xss-through-dom`, `app.js` 10882 -> 10893, same fingerprint). The first attempt at this scan was discarded because a scratch copy of the base sources inside `.datawatch/` was scanned as well and doubled every count; the copy was deleted and the scan re-run. The earlier "Re-scan" section above is the scan of the first-pass tree; this one covers the code after the rework (including the `ObserveSSEHandler` header line).

### Gate commands

| Command | Result |
|---------|--------|
| `make build` | Passes (exit 0). It runs `make sync-docs` first. |
| `make lint` | Passes: `golangci-lint` 0 issues, internal-ref audit, tmux and service-control audits, toolchain pins. |
| `node --check internal/server/web/app.js` | Passes. |
| `node --test internal/server/web/*.test.js` | Passes: 1317 of 1317. |
| `go test ./internal/mcp/` | Passes. |
| `go test ./internal/session/` | Passes except `TestNoNewTmpLeakEntries`, see below. |
| `scripts/check-docs-sync.sh`, `scripts/check-no-internal-refs.sh` | Pass. |
| `GOOS=windows` and `GOOS=darwin go build ./internal/session/ ./internal/mcp/` | Pass. |
| Full `go test ./...` | Every package passes except as follows. A first full run had five packages fail to build and two perf tests fail, each with `disk quota exceeded` on `/tmp` (the compiler's work dir, and the perf seeds); re-running those packages passes (`cmd/datawatch`, `internal/autonomous`, `internal/mcp`, `internal/router`, `internal/server` except two perf tests), and `TestPerfSeedCounts` and `TestEndpointMeasureDeterministic` pass with `TMPDIR` pointed at a roomy directory. The only test still failing in a default run is `TestNoNewTmpLeakEntries`, which wraps `TestEndpointMeasureDeterministic`; same cause. |

**`TestNoNewTmpLeakEntries` and `TestEndpointMeasureDeterministic`.** The leak guard runs `go test ./internal/mcp/ ./internal/server/ ./internal/plugins/` in a private temp dir and fails when that inner run fails. The inner failure is always `TestEndpointMeasureDeterministic` (`internal/server/perf_seed_test.go`), and its log names the cause: `write ... disk quota exceeded` under `/tmp` while the test seeds thousands of session records. The automata-list size mismatch the first pass reported (`90408 vs 88732`) is the symptom: the second seed's records are not written, so its list is smaller. This is an environment limit of the shared `/tmp`, not this branch:

- Run against the **base commit's** sources (extracted with `git archive 3bd9b1c95`) the test failed 6 of 6 times with the same `disk quota exceeded` lines and sizes `90405/90407/90409 vs 88732`.
- On this branch, with `TMPDIR` pointed at a directory with room, `TestEndpointMeasureDeterministic` passed 3 of 3 (about 56 s each).
- With the default temp dir on this branch, every other test in `internal/session`, `internal/mcp`, `internal/server` and `internal/plugins` passes; only that test, and therefore the leak guard that wraps it, fail.

The leak guard itself could not be run to completion here. It was not weakened or skipped. Setting `TMPDIR` inside the worktree to dodge the quota is not a valid workaround for the whole suite: it puts "non-repo" temp dirs inside a git repository and fails unrelated tests (`TestBL10_DiffStat_NotARepo` and similar), so those results from a first attempt were discarded.

### Open code-scanning alerts

Unchanged from the first pass: `gh api 'repos/dmz006/datawatch/code-scanning/alerts?state=open&per_page=100'` returned exactly #661 (`go/command-injection`), #714 (`js/insecure-randomness`) and #715 (`go/reflected-xss`), all last analysed at `3bd9b1c95`. They are open because GitHub only re-scans `main` after a merge. The 66 Go and 16 JavaScript/TypeScript results the local scan still prints are alerts GitHub has dismissed (see `docs/plans/2026-10-27-codeql-local-reproduction.md`) and are not open there.

### Local scan: are #661, #714 and #715 cleared?

By the scanner, yes: none of the three appears in `out-r5` (all three) or `out-r6` (Go, after the no-shell rework). For #714 and #715 the code is fixed. For #661 the path from a request to a shell is gone: the helper runs the program directly. If #661 stays open after the merge scan, that is a real regression and needs a new task; do not dismiss it.

### After the merge

Once the branch is merged to `main` and the CodeQL workflow has run, #661, #714 and #715 should close on their own. Whoever merges should read the Decision section and the CHANGELOG migration note first: subprocess tasks can no longer contain shell syntax, so any stored `--shell` job that does needs an edit (`bash -c '…'` or a script file).
