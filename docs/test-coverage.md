# Test Coverage

Snapshot updated through F10 Sprint 7 S7.6. 661 tests across 44
packages, all passing. CI runs `go test ./...` on every push to
`main`.

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
