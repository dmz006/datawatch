# Test Coverage

Snapshot updated through **v8.6.0** (2026-05-19).

## E2E Test Suite

The primary test surface is the shell-based E2E suite in `scripts/run-tests.sh`
and `scripts/test-stories/TS-*.sh`. Tests run against a live isolated daemon
started by the runner.

| Release | Story range | Stories | Status |
|---------|-------------|---------|--------|
| v8.0.0 | TS-001–TS-159 | 159 | ✅ Green (4 env-conditional skips: KeePassXC, 1Password, Ntfy, Signal) |
| v8.2.0 (BL327–BL330) | TS-637–TS-682 | 46 | ✅ Green |
| v8.3.0 (BL331–BL333) | TS-683–TS-718 | 36 | ✅ Green |
| v8.4.0 (BL332) | TS-719–TS-750 | 32 | ✅ Green |
| v8.5.0 (BL334 T43a–T43e) | TS-751–TS-778 | 28 | ✅ Green |

**Total: 301 E2E stories** across 7 surfaces (REST, MCP, CLI, PWA, channel, session, security).

v8.1.x and v8.6.0 delivered fixes and infrastructure changes with no new E2E story ranges;
coverage added to existing stories or validated by the existing suite.

### Running E2E

```bash
# Parallel mode (default) — starts isolated daemon, cleans up after
make test-e2e

# Serial mode
bash scripts/run-tests.sh --serial

# Single story
bash scripts/run-tests.sh --filter TS-065
```

The runner creates a temp data dir, starts a fresh daemon on random ports,
runs all stories, then kills the daemon and removes the temp dir. Artifacts
(evidence, logs) land in a dated work dir outside the repo.

---

## Unit / Package Tests

```bash
rtk go test ./...     # RTK-filtered output (failures only)
go test -race ./...   # Race detector pass
```

Key packages with dedicated test suites:

| Package | Tests | Focus |
|---------|------:|-------|
| `internal/agents` | 78 | Spawn manager, Docker/K8s drivers, bootstrap client, TLS pinning, worker clone, PQC tokens |
| `internal/router` | 68 | Comm-channel parser + handlers, profile + agent integration |
| `internal/session` | 63 | Session manager, store, tracker, AgentID round-trip, ProjectGit |
| `internal/auth` | 15 | Token broker (mint/revoke/sweep), audit log, persistence |
| `internal/git` | 13 | GitHub CLI shell-out, GitLab stub, Provider interface |
| `internal/server` | 43 | REST handlers, config GET/PUT, session forwarding |
| `internal/config` | 34 | Config Load/Save round-trip incl. all config sections |
| `internal/profile` | 18 | Project + Cluster Profile schema, validation, encrypted store |
| `internal/secfile` | 10 | Encrypt/decrypt round-trip, EncryptedLogWriter append/read, migration helpers |
| `internal/memory` | +2 | Namespace isolation, federated reads |

Unit test suite runs on every `git push` via `make ci-tests`.

---

## Integration Smoke Scripts

| Script | Drives | Prereqs |
|--------|--------|---------|
| `tests/integration/spawn_docker.sh` | F10 Docker driver E2E | docker CLI, jq, running daemon |
| `tests/integration/spawn_k8s.sh` | F10 K8s driver E2E | kubectl CLI, jq, reachable apiserver |
| `tests/integration/container_smoke.sh` | F10 Sprint 1 slim image | docker CLI |

---

## Coverage Gaps (Known)

- **PWA browser-nav E2E** — current PWA stories (TS-130, TS-143–TS-149) check API endpoints and JavaScript syntax. GH#78 tracks making them exercise real browser navigation + component interaction (Playwright). Not yet scheduled.
- **Clone-with-real-token** — worker clone tested against local bare repo; HTTPS-with-token path only covered by `spawn_docker.sh --RUN_BOOTSTRAP=1`.
- **Live forge PR open** — `internal/git/provider_test.go` has fake `gh` test; real PR requires integration smoke.
- **WebSocket relay under load** — single-client bidirectional echo covered; concurrent clients not tested.
