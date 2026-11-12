# Throttle detector — final gate

PRD: ea7c0001 (BL646 / BL406) — per-backend anchored rate-limit (throttle) line detection for
`internal/session`, `internal/config`, `internal/server`.

Gate scope: no source changes in this task; the only repository change is this report file.
Branch `automaton/ea7c0001`, branch base `4f7d913` (changelog top `v9.30.1`). All commands run
from the repository root.

Go files changed on this branch (checked with gofmt, listed, not written):

- `cmd/datawatch/main.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/server/api.go`
- `internal/session/manager.go`
- `internal/session/rate_limit_lines.go`
- `internal/session/rate_limit_lines_test.go`
- `internal/session/ratelimit_parser_test.go`
- `internal/session/throttle_detector_quoted_phrase_test.go`
- `internal/session/v5274_ratelimit_modern_test.go`
- `internal/session/v5276_scrollback_ratelimit_test.go`

## Checks

### 1. gofmt on the Go files changed on this branch — **FAIL**

Command: `gofmt -l cmd/datawatch/main.go internal/config/config.go internal/config/config_test.go internal/server/api.go internal/session/manager.go internal/session/rate_limit_lines.go internal/session/rate_limit_lines_test.go internal/session/ratelimit_parser_test.go internal/session/throttle_detector_quoted_phrase_test.go internal/session/v5274_ratelimit_modern_test.go internal/session/v5276_scrollback_ratelimit_test.go`

Exit status: 0 (empty output would mean all files formatted).

Output (the file listing is the failure: `internal/session/rate_limit_lines_test.go` is not gofmt-clean), verbatim:

```
internal/session/rate_limit_lines_test.go
```

Failure detail verbatim (`gofmt -d internal/session/rate_limit_lines_test.go`):

```
diff internal/session/rate_limit_lines_test.go.orig internal/session/rate_limit_lines_test.go
--- internal/session/rate_limit_lines_test.go.orig
+++ internal/session/rate_limit_lines_test.go
@@ -230,7 +230,7 @@
 		if err != nil {
 			t.Fatalf("read ordinary fixture %s: %v", e.Name(), err)
 		}
-		for _, raw := range strings.Split(string(data), "\n"){
+		for _, raw := range strings.Split(string(data), "\n") {
 			line := raw
 			if line == "" || line == "\r" {
 				continue
```

Diagnosis (reported, not repaired): one missing space before `{` in `internal/session/rate_limit_lines_test.go`
(inside `TestRateLimitOrdinaryTextDoesNotMatch`, ordinary-fixture loop over the list entries of
`testdata/rate_limit_lines/ordinary/`). The test itself passes; the failure is purely formatting.

### 2. `go build ./...` — PASS

Exit status: 0. No output. (19:31:01, 3 s wall-clock for the build+vet pair.)

### 3. `go vet ./internal/session/... ./internal/config/... ./internal/server/...` — PASS

Exit status: 0. No output.

### 4. `go test ./...` (full suite, required by AGENT.md) — PASS

Exit status: 0. 82 packages `ok`, 0 `FAIL` (a few packages report `[no test files]`).
162 s wall-clock (19:31:07 → 19:33:49). Tail verbatim:

```
ok  	github.com/dmz006/datawatch/internal/server	62.831s
ok  	github.com/dmz006/datawatch/internal/server/multiserver	0.015s
ok  	github.com/dmz006/datawatch/internal/session	154.906s
ok  	github.com/dmz006/datawatch/internal/shutdowncause	0.224s
?  	github.com/dmz006/datawatch/internal/signal	[no test files]
ok  	github.com/dmz006/datawatch/internal/skills	0.009s
ok  	github.com/dmz006/datawatch/internal/stats	0.110s
ok  	github.com/dmz006/datawatch/internal/summarizer	0.011s
ok  	github.com/dmz006/datawatch/internal/tagregistry	0.004s
ok  	github.com/dmz006/datawatch/internal/tailscale	0.003s
ok  	github.com/dmz006/datawatch/internal/testhome	0.005s
ok  	github.com/dmz006/datawatch/internal/tlsutil	0.005s
ok  	github.com/dmz006/datawatch/internal/tooling	0.004s
ok  	github.com/dmz006/datawatch/internal/transcribe	18.564s
ok  	github.com/dmz006/datawatch/internal/usagecalc	0.062s
ok  	github.com/dmz006/datawatch/internal/validator	0.005s
ok  	github.com/dmz006/datawatch/internal/vision	0.005s
ok  	github.com/dmz006/datawatch/internal/websearch	0.064s
?  	github.com/dmz006/datawatch/internal/wizard	[no test files]
ok  	github.com/dmz006/datawatch/tests/ci	43.615s
?  	github.com/dmz006/datawatch/tests/live-smoke/commitguard	[no test files]
```

### 5. `scripts/check-no-unscoped-tmux.sh` — PASS

Exit status: 0. Output verbatim:

```
==> Unscoped tmux kill audit (kill-server / kill-session -a)
OK: no unscoped tmux kills
```

### 6. golangci-lint on `internal/session` and `internal/config` — PASS (lint was run; golangci-lint is installed)

Command: `golangci-lint run ./internal/session/... ./internal/config/...`

Exit status: 0. Output verbatim:

```
0 issues.
```

### 7. Required targeted tests — PASS

Command: `go test ./internal/session/ -count=1 -run 'TestRateLimitDetectorIgnoresQuotedPhraseInThrottleTaskOutput|TestRateLimitLineFixturesMatchPerBackend|TestRateLimitOrdinaryTextDoesNotMatch' -v`

Exit status: 0. Output verbatim:

```
=== RUN   TestRateLimitLineFixturesMatchPerBackend
--- PASS: TestRateLimitLineFixturesMatchPerBackend (0.00s)
=== RUN   TestRateLimitOrdinaryTextDoesNotMatch
--- PASS: TestRateLimitOrdinaryTextDoesNotMatch (0.00s)
=== RUN   TestRateLimitDetectorIgnoresQuotedPhraseInThrottleTaskOutput
--- PASS: TestRateLimitDetectorIgnoresQuotedPhraseInThrottleTaskOutput (0.00s)
PASS
ok  	github.com/dmz006/datawatch/internal/session	0.012s
```

- Regression test `TestRateLimitDetectorIgnoresQuotedPhraseInThrottleTaskOutput`: **PASS**
- Two table tests in `internal/session/rate_limit_lines_test.go` (`TestRateLimitLineFixturesMatchPerBackend`,
  `TestRateLimitOrdinaryTextDoesNotMatch`, whose tables run against the fixtures whose list entries live under
  `testdata/rate_limit_lines/`): **PASS**

### 8. Version files and CHANGELOG entry - not applicable on the feature branch

This branch does not edit either version variable and does not add a versioned CHANGELOG heading (the feature-branch rule forbids it). The merge helper stamps the next version above main in both version files and the changelog heading at merge time. An earlier revision of this report wrongly claimed that a bump had been made on the branch.

