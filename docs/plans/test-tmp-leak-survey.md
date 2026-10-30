# Survey: test temp-file leaks (private-TMPDIR sweep)

- Date: 2026-11-01 (repo time)
- Branch: `automaton/0c000001`
- Tool: `scripts/survey-test-tmp.sh`
- Requirement: every Go package's test binary must leave **zero** files in its
  private `TMPDIR` after it exits. Prefixes to watch: `dw-*`, `svchelper-*`,
  `sched-test-*` (and any other temp-file prefix).

## How the survey works

`bash scripts/survey-test-tmp.sh [pkgs …]` (default: `./...`) builds and runs
**each package's test binary** under:

```sh
env -u TMUX -u TMUX_TMPDIR TMPDIR=<fresh private dir> go test -count=1 <pkg>
```

and then lists everything still inside the private directory, summarized by
top-level entry (prefix) with counts. A package **passes** only when its
private directory is empty. No `pkill`, no `tmux kill-server`, no shared-state
inspection — only the private directory content.

The run covers all **94 packages** in `go list ./...` (81 test packages + 13
build-only).

## What the survey found (before)

Exactly **one** leaking package, no `dw-*` / `svchelper-*` / `sched-test-*`
leaks anywhere:

| leaking package | leftover | count | creator |
|---|---|---|---|
| `internal/channel` | `node-compile-cache/` (Node v24 compile cache) | 532 entries | `EnsureExtracted` → `npm install --production` (channel.go:63) |

Cause: the two `TestEnsureExtracted_*` tests exercise `EnsureExtracted`, which
runs `npm install` when `node_modules` is absent. The npm-bundled node
(v24.15.0, nvm build) writes its compile cache to **`$TMPDIR/node-compile-cache`
by default** — and a test process cannot clean up what node created directly
under the survey's private `TMPDIR`.

All other temp-dir creators already clean up correctly (verified, not
leaking):

- `dw-test-home-*` / `dw-test-tmux-*` — `internal/testhome` `Run()`/
  `TmuxIsolation()` register `t.Cleanup` before returning the path
  (testhome.go:44, testhome.go:157).
- `svchelper-*` — `internal/plugins/supervisor_test.go` TestMain removes it
  before `os.Exit`; a plain `go test ./internal/plugins/` (no TestMain) leaves
  nothing.
- `sched-test-*` / `dw-leak-*` / `dw-tmp-leak-guard-*` — no creator found in
  the repo; the strings exist only in the regression guard list
  (`internal/session/tmp_leak_regression_test.go`).

## The fix (after)

`internal/channel/v6070_bl218_test.go`: both `TestEnsureExtracted_*` tests now
call `isolateNodeCompileCache`, which sets `NODE_COMPILE_CACHE` to a directory
**inside the test's `t.TempDir()`** (pre-created, per Node's requirement) so
node writes the cache into a directory `testing` removes automatically:

```go
func isolateNodeCompileCache(t *testing.T, dir string) {
	t.Helper()
	cacheDir := filepath.Join(dir, "node-compile-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir node-compile-cache: %v", err)
	}
	t.Setenv("NODE_COMPILE_CACHE", cacheDir)
}
```

(`NODE_COMPILE_CACHE` is honored by the same node — verified: with it set, the
532-entry cache lands in the target dir and nothing is left in `$TMPDIR`.)

## Note: one flaky *test failure* during the "before" run (not a leak)

`tests/ci` failed once in the initial full run: `TestDaemonCleanupHelper/
InterruptSighup` — "driver did not end within 30s on mode wait". It passed in
a standalone rerun and in the full "after" run, so it is a load-dependent
flake, not a temp-file leak, and out of scope for this task (the survey gate
is the private-directory content, which was clean in both runs).

| `cmd/datawatch` | OK — 113.885s | 0 | OK — 107.894s | 0 |
| `cmd/datawatch-channel` | OK — 0.059s | 0 | OK — 0.057s | 0 |
| `cmd/datawatch-stats` | OK — 0.440s | 0 | OK — 0.441s | 0 |
| `cmd/datawatch-validator` | OK — [no | 0 | OK — [no | 0 |
| `cmd/docs-index-gen` | OK — [no | 0 | OK — [no | 0 |
| `internal/acme` | OK — 1.822s | 0 | OK — 1.816s | 0 |
| `internal/agents` | OK — 4.108s | 0 | OK — 4.121s | 0 |
| `internal/alertrules` | OK — [no | 0 | OK — [no | 0 |
| `internal/alerts` | OK — 0.067s | 0 | OK — 0.067s | 0 |
| `internal/algorithm` | OK — 0.002s | 0 | OK — 0.001s | 0 |
| `internal/apns` | OK — 0.003s | 0 | OK — 0.003s | 0 |
| `internal/audit` | OK — 0.004s | 0 | OK — 0.004s | 0 |
| `internal/auth` | OK — 0.244s | 0 | OK — 0.244s | 0 |
| `internal/autonomous` | OK — 31.934s | 0 | OK — 31.928s | 0 |
| `internal/autonomous/scan` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/capacity` | OK — 1.480s | 0 | OK — 1.469s | 0 |
| `internal/channel` | OK — 2.271s | 532 | OK — 1.616s | 0 |
| `internal/compute` | OK — 0.003s | 0 | OK — 0.003s | 0 |
| `internal/config` | OK — 1.719s | 0 | OK — 1.773s | 0 |
| `internal/council` | OK — 0.010s | 0 | OK — 0.010s | 0 |
| `internal/devices` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/docsindex` | OK — 0.086s | 0 | OK — 0.086s | 0 |
| `internal/evals` | OK — 0.004s | 0 | OK — 0.004s | 0 |
| `internal/federation` | OK — 0.039s | 0 | OK — 0.038s | 0 |
| `internal/fingerprint` | OK — 0.244s | 0 | OK — 0.218s | 0 |
| `internal/git` | OK — 0.030s | 0 | OK — 0.030s | 0 |
| `internal/gitguard` | OK — 0.041s | 0 | OK — 0.038s | 0 |
| `internal/hookinstaller` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/identity` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/inference` | OK — 0.011s | 0 | OK — 0.010s | 0 |
| `internal/inference/adapters` | OK — 0.004s | 0 | OK — 0.005s | 0 |
| `internal/llm` | OK — 0.003s | 0 | OK — 0.004s | 0 |
| `internal/llm/backends/aider` | OK — 0.004s | 0 | OK — 0.004s | 0 |
| `internal/llm/backends/gemini` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/llm/backends/goose` | OK — 0.003s | 0 | OK — 0.003s | 0 |
| `internal/llm/backends/ollama` | OK — 0.013s | 0 | OK — 0.012s | 0 |
| `internal/llm/backends/opencode` | OK — 0.008s | 0 | OK — 0.008s | 0 |
| `internal/llm/backends/openwebui` | OK — 0.014s | 0 | OK — 0.014s | 0 |
| `internal/llm/backends/pi` | OK — 0.028s | 0 | OK — 0.028s | 0 |
| `internal/llm/backends/shell` | OK — 0.003s | 0 | OK — 0.003s | 0 |
| `internal/llm/claudecode` | OK — 0.008s | 0 | OK — 0.008s | 0 |
| `internal/mcp` | OK — 1.911s | 0 | OK — 1.912s | 0 |
| `internal/mcp/search` | OK — 0.004s | 0 | OK — 0.005s | 0 |
| `internal/memory` | OK — 1.290s | 0 | OK — 1.290s | 0 |
| `internal/messaging` | OK — 0.002s | 0 | OK — 0.001s | 0 |
| `internal/messaging/backends/discord` | OK — [no | 0 | OK — [no | 0 |
| `internal/messaging/backends/dns` | OK — 0.005s | 0 | OK — 0.005s | 0 |
| `internal/messaging/backends/email` | OK — 135.257s | 0 | OK — 133.480s | 0 |
| `internal/messaging/backends/github` | OK — 0.103s | 0 | OK — 0.103s | 0 |
| `internal/messaging/backends/imapmcp` | OK — 0.507s | 0 | OK — 0.507s | 0 |
| `internal/messaging/backends/matrix` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/messaging/backends/ntfy` | OK — [no | 0 | OK — [no | 0 |
| `internal/messaging/backends/signal` | OK — [no | 0 | OK — [no | 0 |
| `internal/messaging/backends/slack` | OK — [no | 0 | OK — [no | 0 |
| `internal/messaging/backends/telegram` | OK — [no | 0 | OK — [no | 0 |
| `internal/messaging/backends/twilio` | OK — 0.203s | 0 | OK — 0.203s | 0 |
| `internal/messaging/backends/webhook` | OK — 0.003s | 0 | OK — 0.002s | 0 |
| `internal/metrics` | OK — 0.003s | 0 | OK — 0.003s | 0 |
| `internal/observer` | OK — 0.942s | 0 | OK — 0.920s | 0 |
| `internal/observer/ebpf` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/observerpeer` | OK — 0.030s | 0 | OK — 0.030s | 0 |
| `internal/orchestrator` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/pathsafe` | OK — [no | 0 | OK — [no | 0 |
| `internal/pilog` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/pinkeeper` | OK — 15.768s | 0 | OK — 15.770s | 0 |
| `internal/pipeline` | OK — 3.229s | 0 | OK — 3.230s | 0 |
| `internal/plugins` | OK — 15.699s | 0 | OK — 15.720s | 0 |
| `internal/profile` | OK — 0.007s | 0 | OK — 0.006s | 0 |
| `internal/proxy` | OK — 0.216s | 0 | OK — 0.216s | 0 |
| `internal/router` | OK — 106.566s | 0 | OK — 106.570s | 0 |
| `internal/rtk` | OK — 2.136s | 0 | OK — 2.112s | 0 |
| `internal/secfile` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/secrets` | OK — 0.008s | 0 | OK — 0.009s | 0 |
| `internal/server` | OK — 67.148s | 0 | OK — 65.944s | 0 |
| `internal/server/multiserver` | OK — 0.014s | 0 | OK — 0.013s | 0 |
| `internal/session` | OK — 156.536s | 0 | OK — 156.296s | 0 |
| `internal/shutdowncause` | OK — 0.214s | 0 | OK — 0.205s | 0 |
| `internal/signal` | OK — [no | 0 | OK — [no | 0 |
| `internal/skills` | OK — 0.007s | 0 | OK — 0.007s | 0 |
| `internal/stats` | OK — 0.189s | 0 | OK — 0.190s | 0 |
| `internal/summarizer` | OK — 0.010s | 0 | OK — 0.010s | 0 |
| `internal/tagregistry` | OK — 0.009s | 0 | OK — 0.019s | 0 |
| `internal/tailscale` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/testhome` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/tlsutil` | OK — 0.003s | 0 | OK — 0.003s | 0 |
| `internal/tooling` | OK — 0.002s | 0 | OK — 0.002s | 0 |
| `internal/transcribe` | OK — 18.319s | 0 | OK — 18.303s | 0 |
| `internal/usagecalc` | OK — 0.053s | 0 | OK — 0.055s | 0 |
| `internal/validator` | OK — 0.005s | 0 | OK — 0.005s | 0 |
| `internal/vision` | OK — 0.005s | 0 | OK — 0.004s | 0 |
| `internal/websearch` | OK — 0.063s | 0 | OK — 0.061s | 0 |
| `internal/wizard` | OK — [no | 0 | OK — [no | 0 |
| `tests/ci` | FAIL (flaky, 98.486s — rerun passes) | 0 | OK — 51.872s | 0 |
| `tests/live-smoke/commitguard` | OK — [no | 0 | OK — [no | 0 |

## Totals

| run | packages | passing (empty private TMPDIR) | failing | total leftover entries |
|---|---|---|---|---|
| before | 94 | 92 (incl. 13 no-test-file) | 1 (tests/ci, flaky) | **532** (all in `internal/channel`) |
| after  | 94 | **94** | 0 | **0** |

## Reproduce

```sh
bash scripts/survey-test-tmp.sh
# =>
# OK: all 94 package(s) passed
```

## Gate (done)

```
bash scripts/survey-test-tmp.sh                    # exit 0, all 94 pass
env -u TMUX -u TMUX_TMPDIR go test -run '^TestDaemonCleanupHelper$' ./tests/ci/  # ok (flake gone)
go build ./...                                     # ok
```
