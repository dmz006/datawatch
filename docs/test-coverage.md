# Test Coverage

Snapshot updated through **v3.11.0 release** (Sprint S7 — plugin
framework). **1156 tests across 52 packages**, all passing. CI
runs `go test ./...` on every push to `main`.

## v3.11.0 additions (+8 tests vs. v3.10.0)

- **BL33 plugin framework** — `internal/plugins/plugins_test.go`
  covers manifest discovery (finds/skips non-plugin dirs), subprocess
  invoke with line replacement, per-call timeout → pass response,
  fan-out chaining across two plugins, `disabled:` list honored at
  discovery, `SetEnabled` toggle, and the global `enabled:false`
  no-discovery path. Uses the `testdata/`-style pattern with shell
  scripts; Windows is skipped (subprocess path is POSIX-only in v1).

## v3.10.0 additions (+15 tests vs. v3.9.0)

- **BL24 autonomous package** —
  `internal/autonomous/autonomous_test.go` covers:
  - Store: CreatePRD round-trip with JSONL reload, empty-spec
    rejection, SetStories ID assignment (Story + Task), AddLearning
    + ListLearnings round-trip.
  - Decompose parser: plain JSON, fenced + trailing `//` comments,
    `stripLineComment` respects `//` inside string literals (URLs).
  - Security scan: finds `os.system` pattern in `.py`; clean file
    produces zero findings.
  - Manager: Decompose wires parsed stories into store +
    PRDStatus→Active; Status counts ActivePRDs/QueuedTasks/RunningTasks.
  - Executor: Run respects `depends_on` dependency order;
    AutoFixRetries re-spawns + re-verifies until success; topoSort
    detects cycles.
  - API adapter: SetConfig unmarshals a `json.RawMessage` back to
    `Config` (snake_case JSON tags).

## v3.9.0 additions (+7 tests vs. v3.8.0)

- **BL20 routing rules** — `internal/server/bl20_routing_test.go`
  covers first-match wins, no-match fallthrough, invalid regex
  skipped, POST persist round-trip, `/test` endpoint matched +
  unmatched paths.

## v3.8.0 additions (+ Sprint S4 tests)

- **BL42 assist / BL31 device aliases / BL69 splash** — REST handler
  tests in `internal/server/` exercise each YAML block end-to-end via
  the `bl90_server_test.go` httptest harness.

## v3.7.3 additions (+14 tests vs. v3.7.2)

- **Sprint Sx2 router parser** — `internal/router/sx2_parity_test.go`
  covers all 5 new comm-channel commands (`cost`, `stale`, `audit`,
  `cooldown` × 3 verbs, `rest` × 2 forms) and the loopback
  unconfigured-error path for `commGet` / `commJSON`.

## v3.7.2 additions (+14 tests vs. v3.7.1)

- **Sprint Sx MCP tools** — `internal/mcp/sx_parity_test.go` verifies
  all 20 new tool registrations have correct names + required-arg
  schemas, and that `proxyGet`/`proxyJSON` surface a clear error
  when `webPort=0` (loopback unavailable).
- **Sprint Sx CLI** — `cmd/datawatch/cli_sx_parity_test.go` verifies
  9 new commands register with the right names and have the
  expected flags + sub-subcommands.

## v3.7.2 functional verification

Live daemon smoke run on `127.0.0.1:18080` (separate temp data dir,
no impact on operator's running daemon). Every Sx endpoint returned
valid JSON; POST/DELETE round-trips for `/api/projects` and
`/api/cooldown` persisted. `session.cost_rates.claude-code` override
correctly applied to live `Manager` (rate showed `0.005/0.020`
instead of the built-in `0.003/0.015` defaults).

## v3.7.0 additions (+23 tests vs. v3.6.0)

- **BL6** — `internal/session/bl6_cost_test.go` (7 tests) covers
  EstimateCost math, default rate table, SummaryFor aggregation,
  Manager.AddUsage round-trip, family fallback, override.
  `internal/server/bl6_cost_test.go` (5 tests) covers the REST
  surface for both summary modes + usage POST.
- **BL9** — New `internal/audit` package: 7 tests covering
  open/close, write+read round-trip, actor/session/since/until/limit
  filters, newest-first ordering, missing-file empty.
  `internal/server/bl9_audit_test.go` (4 tests) covers the REST
  surface including not-enabled, method check, returns-entries,
  actor filter.

## v3.6.0 additions (+34 tests vs. v3.5.0)

- **BL5** — `internal/server/bl5_templates_test.go`: empty list, full
  CRUD round-trip, missing-name validation.
- **BL26** — `internal/session/bl26_recur_test.go`: recurring success
  reschedules, RecurUntil ends recurrence, failed recurring still fails.
- **BL27** — `internal/server/bl27_projects_test.go`: empty list, full
  CRUD round-trip, absolute-dir validation, missing-name/dir validation.
- **BL29** — `internal/session/bl29_checkpoint_test.go`: pre+post tag
  round-trip in real git repo, invalid kind rejected, not-a-repo no-op,
  rollback success, missing-tag error, dirty-tree refused without force.
- **BL30** — `internal/session/bl30_cooldown_test.go` + REST tests:
  inactive default, set/clear, expired auto-inactive, rate_limit_global_pause
  setter, sentinel error, REST GET/POST/DELETE happy paths + past-time reject.
- **BL40** — `internal/session/bl40_stale_test.go` + REST: nil session,
  zero threshold disables, non-running excluded, host filter, REST 405,
  REST happy path with seeded sessions.

## v3.5.0 additions (+21 tests vs. v3.4.x)

- **BL1** — `internal/server/bl1_listen_test.go`: `joinHostPort` IPv4 /
  IPv6 literal / dual-stack `[::]` / hostname / 0.0.0.0 cases.
- **BL34** — `internal/server/bl34_ask_test.go`: method check + empty
  question + unsupported backend + ollama-not-configured + happy
  path against a fake Ollama httptest server.
- **BL35** — `internal/server/bl35_summary_test.go`: method check +
  missing `dir` + relative `dir` + no-git + with seeded sessions +
  real git repo round-trip.
- **BL41** — `internal/session/bl41_effort_test.go`: `IsValidEffort`
  table + `Manager.DefaultEffort` setter/getter + `resolveEffort`
  precedence (opt > manager default > "normal").

## v3.4.0 additions (+10 tests vs. v3.3.0)

- **BL17** — `internal/server/bl17_reload_test.go` covers reload
  happy-path (config edit propagates to live Manager), missing-
  config-path error, HTTP method check, missing-file tolerance.
- **BL37** — `internal/server/bl37_diagnose_test.go` covers
  composite OK contract, JSON shape, method check, allOK helper
  edge cases.

## v3.3.0 additions (+22 tests vs. v3.2.0)

- **BL10** — `internal/session/bl10_diff_test.go` parses standard
  shortstat lines, insertion-only output, empty input, and the
  not-a-repo path.
- **BL11** — `internal/session/bl11_anomaly_test.go` covers
  `DetectStuckLoop` threshold + varied-tail + disable-by-zero,
  `DetectLongInputWait` happy + non-waiting state, and
  `DetectDurationOutlier` over and under threshold.
- **BL12** — `internal/stats/bl12_history_test.go` validates day
  bucketing, empty-day inclusion, success rate, and avg duration.
  `internal/server/bl12_analytics_test.go` covers the REST handler
  contract (default range, 30d range, seeded session reflected,
  POST 405, parser edge cases).

## v3.2.0 additions (+4 tests vs. v3.1.0)

- **BL39** — `TestNewPipeline_RejectsCycle` and
  `TestDetectCycles_PathFormat` in
  `internal/pipeline/pipeline_test.go` validate constructor cycle
  rejection and that the DFS path-reconstruction returns all cycle
  nodes.
- **BL28** — `TestBL28_SetQualityGates` and
  `TestBL28_CompareResults_SummaryFormat` in
  `internal/pipeline/bl28_executor_test.go` cover the executor
  setter and the comparison-summary contract.
- Pre-existing scaffolded `TestQualityGate_*` tests are now
  exercised against the wired-in `quality.go` implementation.

## v3.1.0 additions (+22 tests vs. v3.0.0)

- **3 B30 tests** in `internal/session/tmux_b30_test.go` — exercise
  `SendKeysWithSettle` via a PATH-injected fake tmux shell script,
  assert settle=0 one-shot path, settle>0 two-call path, and the
  Manager config setter/getter pair.
- **4 BL89 tests** in `internal/session/bl89_fake_tmux_test.go` —
  verify the `TmuxAPI` interface swap works via `mgr.WithFakeTmux()`
  for scheduler + user paths and error propagation.
- **9 BL90 tests** in `internal/server/bl90_api_test.go` — health,
  info/version, sessions list (empty + seeded), config GET/PUT,
  devices register/list/delete, federation method check.
- **6 BL91 tests** in `internal/mcp/bl91_handler_test.go` — direct
  MCP handler invocation covering list_sessions (empty + seeded),
  get_version, send_input (not-found + missing-id), rename_session
  missing-args.

## Per-package counts (F10-relevant + supporting)

| Package | Tests | Focus |
|---------|------:|-------|
| `internal/agents` | 78 | Spawn manager, Docker driver, K8s driver, bootstrap client, TLS pinning, worker clone, git-token wiring, post-session PR hook, PQC tokens (ML-KEM + ML-DSA round-trip, tamper detection) |
| `internal/memory` | +2 | Namespace isolation (S6.1) — federated reads via SearchInNamespaces, legacy Save defaults to __global__ |
| `internal/auth` | 15 | Token broker (mint/revoke/sweep), audit log, persistence, periodic sweeper |
| `internal/session` | 63 | Session manager, store, tracker, AgentID round-trip, ProjectGit (push/branch/token URL) |
| `internal/git` | 13 | GitHub CLI shell-out, GitLab stub, Provider interface, Resolve routing |
| `internal/profile` | 18 | Project + Cluster Profile schema, validation, encrypted store, Smoke + driver-CLI reachability |
| `internal/server` | 43 | REST handlers (incl. agent + agent-proxy + bind + ca.pem), config GET/PUT, session forwarding |
| `internal/router` | 68 | Comm-channel parser + handlers (incl. agent verbs + bind), profile + agent integration |
| `internal/config` | 34 | Config Load/Save round-trip incl. AgentsConfig with all fields |

## Integration smoke (manual / CI-skipped by default)

| Script | Drives | Prereqs |
|--------|--------|---------|
| `tests/integration/spawn_docker.sh` | F10 docker driver e2e | docker CLI, jq, running daemon |
| `tests/integration/spawn_k8s.sh` | F10 K8s driver e2e | kubectl CLI, jq, reachable apiserver |
| `tests/integration/container_smoke.sh` | F10 Sprint 1 slim image | docker CLI |
| `tests/integration/k8s_smoke.sh` | F10 Sprint 1 K8s deferred smoke | kubectl |

Both spawn scripts walk the full REST chain (profile create → agent
spawn → agent get → bootstrap token validation → terminate → cleanup)
and accept `RUN_BOOTSTRAP=1` to additionally wait for `state=ready`
against a real worker image.

## What's covered well

- **Driver behaviour** — every `Driver` method on both Docker and K8s
  drivers has a fake-binary fixture test asserting argv shape, env
  injection (deadline + fingerprint), output parsing, and error
  surfacing. New env vars added in S3.4 / S4.3 each got a "set →
  injected / unset → absent" pair.
- **Token broker invariants** — supersede on mint, idempotent revoke,
  sweep distinguishes orphaned vs expired in audit, JSON-line audit
  validates as parseable, store reload after restart preserves
  records. RunSweeper has start-immediate, ctx-cancel, and periodic-
  tick coverage.
- **TLS pinning** — `PinnedTLSConfig` exercised against a real
  `httptest.NewTLSServer` for matching, mismatching, and openssl
  colon-format pins. Worker side: `CallBootstrap` honours the env
  var and refuses unpinned certs.
- **Session forwarding** — bind + AgentID persistence + read-path
  forwarding (output) covered with a fake worker httptest backend.
- **Config parity** — every new `AgentsConfig` field gets a
  `TestSave_RoundTrip_AgentsConfig` extension so disk persistence
  matches the in-memory shape; PUT-handler cases live alongside the
  GET-side serialisation in `internal/server/api.go`.

## Known thinly-covered areas

These are currently exercised only by integration scripts; flagged
here so future contributors know where to add unit tests when the
shape stabilises:

- **End-to-end clone-with-real-token** — `worker_clone_test.go`
  exercises the local bare-repo path; the HTTPS-with-token path is
  exercised by `spawn_docker.sh` with `RUN_BOOTSTRAP=1`.
- **PR open against live forge** — S5.4 in flight; once landed,
  `internal/git/provider_test.go` already has the OpenPR happy path
  via fake `gh`, but only an integration smoke can verify a real
  PR appears.
- **Reverse-proxy WebSocket relay under load** — single client
  bidirectional echo covered; concurrent clients deferred until a
  legitimate need surfaces.
- **REST handler matrix** — covered via individual handler tests; an
  httptest-server mock loop (BL90) would give us full request/
  response contract coverage in one place.

## Backlog driving more coverage

- **BL89** — mock session manager for unit tests (would let router-
  level tests run without `session.NewManager` setup churn)
- **BL90** — httptest server harness for all 65 API endpoints
- **BL91** — MCP tool handler tests (44 tools, currently driven only
  by manual MCP client smokes)

## Running locally

```sh
# Full suite (RTK-filtered output)
rtk go test ./...

# Single package with verbose output
go test -v ./internal/agents -run TestSpawn

# Race detector pass
go test -race ./...

# Integration smoke against a running daemon
DATAWATCH_BASE_URL=http://localhost:8080 \
  tests/integration/spawn_docker.sh
```

CI: `make ci-tests` (in the project Makefile) runs the equivalent of
`rtk go test ./...` plus `go vet ./...`. PRs that drop coverage on
F10-relevant packages should explain why in the description.
