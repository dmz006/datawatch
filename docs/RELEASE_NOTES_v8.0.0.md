# datawatch v8.0.0 — Release Notes

**Released:** 2026-05-19  
**Previous release:** v7.2.3 (2026-05-15)  
**Commits since v7.2.3:** 88

---

## What v8.0.0 Is

v8.0.0 is the federation and routing release. Where v7.0 made datawatch the operator plane for local LLM workloads, v8.0 extends that authority outward — across hosts, across trust boundaries, and across container and cloud runtimes. Federated peers are now first-class citizens with precisely scoped access, compute nodes can be wired to Docker networks or remote datawatch proxies, and every surface from the REST API to the PWA to the CLI participates in the new capability model.

The release also ships a complete, battle-hardened end-to-end test suite — 567 stories across all seven product surfaces, zero skips, zero failures. Getting from "most features covered" to "all features, all surfaces, all passing" took 30+ commit iterations and is an achievement worth naming. The confidence floor for every merge on this codebase just rose substantially.

Under the hood: the MCP SSE transport now accepts federated peer tokens (not just the admin token), sessions can be marked one-shot so they self-terminate on completion, and a batch of long-standing rough edges in CLI authentication, inference tombstoning, and PWA display have been resolved.

---

## Highlights

- **Capability-Based Access Control for Federation** — 50 individual capabilities, 13 built-in groups, custom groups via any surface, and a systematic sweep of every REST handler ensuring federated peers receive 403 on any capability mismatch rather than a generic 503.
- **Compute Node Routing Modes** — three new modes (`docker-network`, `datawatch-proxy`, `k8s-sidecar` stub) with two new LLM adapters (Gemini and OpenCode) and a Docker lifecycle manager that requires no SDK.
- **Multi-Server Management** — register and manage remote datawatch instances, aggregate their sessions/alerts/PRDs, and proxy requests through to them — all accessible from a per-tab server chip bar in the PWA.
- **MCP SSE Federated Auth** — the MCP SSE port now enforces capability gates per-tool for federated peer tokens, bringing MCP into full parity with the REST and CLI federation model.
- **567 E2E stories, all passing** — the most complete test coverage this project has ever shipped, covering every surface, every subsystem, and verified by a meta-test that enforces minimum suite size.

---

## Major Features

### Federation Capability-Based Access Control (CBAC)

v8.0 ships a production-ready access control system for federated peers built around 50 individual capabilities organized across 18 surfaces:

| Surface group | Capabilities |
|---|---|
| Sessions | `sessions:list`, `sessions:read`, `sessions:write`, `sessions:kill`, `sessions:input` |
| Agents | `agents:list`, `agents:read`, `agents:write`, `agents:kill` |
| Observers | `observers:list`, `observers:read`, `observers:write` |
| LLMs | `llms:list`, `llms:read`, `llms:write` |
| Compute | `compute:list`, `compute:read`, `compute:write` |
| Analytics | `analytics:read` |
| Health | `health:read` |
| Config | `config:read`, `config:write` |
| Secrets | `secrets:list`, `secrets:read`, `secrets:write` |
| Pipelines | `pipelines:list`, `pipelines:read`, `pipelines:write` |
| Autonomous | `autonomous:list`, `autonomous:read`, `autonomous:write` |
| Council | `council:list`, `council:read`, `council:write` |
| Federation | `federation:list`, `federation:read`, `federation:write` |
| Docs | `docs:read` |
| Audit | `audit:read` |
| Comm | `comm:read`, `comm:write` |
| Alerts | `alerts:list`, `alerts:read`, `alerts:write` |
| Dashboard | `dashboard:read`, `dashboard:write` |

**13 built-in capability groups** cover the most common operator configurations:

| Group | Intended use |
|---|---|
| `monitor` | Read-only visibility into sessions and health |
| `session-viewer` | Full session read access |
| `session-operator` | Create, kill, and send input to sessions |
| `inference-admin` | Full LLM registry + compute node management |
| `config-reader` | Read system config without write access |
| `config-admin` | Full config read/write |
| `analytics-viewer` | Analytics and audit read |
| `autonomous-operator` | Autonomous PRD and pipeline management |
| `council-operator` | Council debate management |
| `federation-peer` | Safe default for a peer that needs cross-host session input |
| `comm-bridge` | Comm channel read/write for relay peers |
| `read-only` | All `:read` and `:list` capabilities, no writes |
| `full-control` | All 50 capabilities (admin equivalent) |

Operators can create custom groups via REST, MCP, CLI, or YAML-seeded peer configurations. The federation peer registry exposes full CRUD at `POST/GET/PUT/DELETE /api/federation/peers` and `POST/GET/PUT/DELETE /api/federation/groups`.

**Cross-host session input** (`POST /api/sessions/<peer_name>/<session_id>/input`) routes keystrokes and commands to sessions on a registered peer, gated by the peer's `sessions:input` capability.

The implementation covers all seven product surfaces: REST API, MCP tools, CLI commands, comm channel, PWA (Observer tab federation peers panel), YAML-seeded peer configuration, and docs. A systematic sweep of 45+ handler files and 110+ call sites ensures that `fedCap()` always precedes the nil-guard — federated peers receive a 403 with a capability description, never a 503 or a nil-pointer panic.

---

### MCP SSE Federated Authentication

The MCP SSE transport (running on its own port) previously accepted only the admin token. v8.0 adds a second authentication path for federated peer tokens, bringing the MCP surface into full capability parity with the REST API.

`mcpFedAuthMiddleware` intercepts every SSE connection and resolves the token to either admin context (full access) or a peer context (CBAC-gated). Per-tool `mcpFedCap()` guards on all write and mutating MCP tools enforce the resolved capability set at call time, not just at connection time.

The implementation includes 13 new unit tests covering: middleware auth resolution, peer context tagging, capability enforcement at the middleware layer, and handler-level gates on individual tools.

---

### Multi-Server Management

Operators managing fleets of datawatch instances can now register them by name and treat them as first-class participants in any datawatch interface.

**Registration and management:**
- `POST /api/servers/{name}` — register a remote instance with URL, bearer token (or `${secret:name}` reference), and optional `builtin: true` flag for YAML-seeded read-only entries
- `GET/PUT/DELETE /api/servers/{name}` — retrieve, update, or remove a registered server
- `POST /api/servers/{name}/test` — test connectivity and surface reachability

**Aggregated views:**
- `GET /api/sessions/aggregated` — sessions from all registered servers merged into a single response
- `GET /api/alerts/aggregated` — alerts aggregated across all servers
- `GET /api/autonomous/prds/aggregated` — PRDs aggregated across all servers

**Proxy mode:**
- `GET /api/proxy/{name}/...` — transparent proxy to any remote API endpoint using the stored bearer token

**PWA integration:** Sessions, Alerts, Automata, Observer, and Dashboard tabs each display a server chip bar (`All / Local / <name>`) allowing operators to scope any view to a specific registered instance.

Secret references (`${secret:name}`) in stored server tokens mean that credentials are resolved from the secrets store at request time — no plaintext tokens at rest in the server registry.

---

### Compute Node Routing Modes

v8.0 extends the compute node model with three routing modes beyond the existing direct (host:port) default:

#### `docker-network` mode

The daemon manages the full container lifecycle via Docker CLI (no Docker SDK dependency):

- `EnsureRunning` — start the container if not running, optionally with `auto_pull` to fetch the image first
- `EnsureNetwork` — create the Docker network if absent and attach the container
- `Teardown` — stop and optionally remove the container
- `Status` — return current container and network state

Configuration fields:
```yaml
routing_mode: docker-network
image: ollama/ollama:latest      # required
network_name: datawatch-net      # optional, created if absent
port: 11434
container_name: dw-ollama        # optional, derived from node name if omitted
auto_start: true                 # start container at daemon startup
auto_pull: true                  # pull image before start if absent
env:                             # environment variables injected into container
  OLLAMA_MODELS: /models
```

#### `datawatch-proxy` mode

Forward inference requests through a registered federated peer's LLM proxy endpoint. Useful for routing requests to a GPU host without exposing the LLM directly:

```yaml
routing_mode: datawatch-proxy
peer: gpu-host
remote_llm_name: llama3-fast
timeout_seconds: 120
```

Error classification distinguishes transient errors (5xx, 429 — eligible for failover to the next node in the LLM's priority list) from final errors (4xx — no failover, return to caller).

#### `k8s-sidecar` mode (stub)

Reserved for a future Kubernetes sidecar injection pattern. The routing mode is accepted and stored but not yet implemented — nodes configured with this mode fall back to direct routing.

**New LLM adapters:**

`gemini-api` — Google Generative Language API (`POST /v1beta/models/<model>:generateContent?key=<api_key>`). Handles Gemini's request/response envelope format including `contents[]` message structure and `candidates[].content.parts` response extraction.

`opencode-api` — OpenAI-compatible `/v1/chat/completions` endpoint, registered as a distinct adapter kind from `openwebui` to allow separate configuration defaults and capability gates.

**PWA routing:** The compute node edit form now shows a routing mode dropdown with conditional sub-field panels that appear based on the selected mode. All sub-fields are validated before save.

---

### OneShot Session Mode

Sessions created with `one_shot: true` monitor their own pane output and terminate automatically when `DATAWATCH_COMPLETE:` appears — no operator intervention required.

Available on all surfaces:
- **REST:** `POST /api/sessions` with `{"one_shot": true, ...}`
- **MCP:** `start_session` tool with `one_shot` parameter
- **CLI:** `datawatch session start --one-shot`

The Autonomous task runner uses one-shot sessions as its execution substrate — each task gets a session that cleans itself up when the task signals completion.

A 30-second post-restart grace window suppresses spurious `DATAWATCH_COMPLETE:` detections that can appear in pane capture during daemon restarts, preventing premature session termination.

---

## Testing — A Milestone Worth Celebrating

v8.0.0 ships the most comprehensive test suite this project has ever had. This is not a minor increment — it is a complete, production-hardened end-to-end harness built from scratch and driven to zero failures through 16+ full test run iterations.

### By the numbers

| Metric | Count |
|---|---|
| E2E stories (shell + Playwright) | **567** |
| Shell stories | 560 |
| PWA Playwright stories | 7 |
| Release smoke sections | **85** |
| Go unit tests | **~1,736** |
| Go test files | 259 |
| Surface tags covered | 7 |
| Test run iterations to reach all-pass | 16+ |

### Surface coverage

| Surface | Stories |
|---|---|
| `surface:api` | ~220 |
| `surface:mcp` | ~60 |
| `surface:cli` | ~50 |
| `surface:pwa` | ~30 (7 Playwright + ~23 mixed) |
| `surface:comm` | ~20 |
| `surface:meta` | 5 |

### Feature areas covered

Sessions, LLM registry, compute nodes, federation and CBAC, memory, autonomous PRDs, council debates, plugins, skills, secrets, observer, Tailscale, routing modes, docs-as-MCP, alerts, dashboard, evals, algorithm mode, identity, profiles, and security.

### Infrastructure built for this release

**Parallel test runner** with configurable worker pools — the full suite runs in parallel rather than serially, with per-worker port allocation ensuring no conflicts even when dozens of stories run simultaneously.

**Dynamic port allocation** — every test story gets its own port range. Port collisions are architecturally impossible.

**TEST_BASE always HTTPS** — all test stories run against a TLS endpoint, matching production deployment conditions. HTTP-only assumptions are eliminated.

**Pre-seeded test skills and plugins** — the test environment starts with a known-good set of skills and plugins installed, enabling isolated testing of skill/plugin behavior without cross-story contamination.

**Inline peer daemon** — routing and federation stories spin up a second datawatch daemon in-process for the duration of the test, then tear it down. No long-lived test infrastructure required.

**Docker simulation** — container routing stories use a controlled Docker environment with cleanup traps that ensure no containers or images leak between stories.

**PWA Playwright automation** — seven stories drive Chrome in headless mode against the full PWA, testing federation peers panel, session display, and PWA-specific UI behaviors (fullscreen, keyboard handling).

**Evidence directory per story** — every story captures its HTTP responses, daemon logs, and assertion output to a timestamped evidence directory. Debugging a failure means looking at a file, not reproducing it.

**Meta-test TS-556** — a story that counts all other stories and fails if the suite drops below 550. The test suite is self-enforcing.

**Release smoke CI gate** — 85 sections in `release-smoke.sh` covering every subsystem, run as a CI gate. Includes tidy-plans lint, internal-ref integrity, and webfs sync checks.

**Per-feature timing stats** — the test runner emits per-feature wall-clock timings so slow stories are immediately visible.

Getting to zero failures from a standing start required fixing auth propagation in CLI tooling, dynamic port exhaustion in parallel runs, Docker lifecycle edge cases, PWA selector brittleness, MCP envelope wrapping, ollama model load timing, algorithm mode field names, docs-apply parameter passing, and more. Each iteration found real bugs — not just test bugs — in the product itself.

---

## Fixes

- **Inference tombstoning** — auto-created LLM entries (those created implicitly by a compute node registration) are now properly tombstoned when deleted. The disable operation on these entries was a no-op; both behaviors are corrected.
- **CLI Bearer auth** — `daemonGet` and `daemonJSON` now attach the `Authorization: Bearer` header from the local CLI config or the `DATAWATCH_TOKEN` environment variable. Previously, CLI commands against a token-protected daemon would fail silently.
- **GET `/api/llms/{name}/models`** — new endpoint to retrieve the model list for a specific named LLM entry, matching the existing pattern for compute node model lists.
- **MCP `resources/read`** — switched from reading the resource identifier from a POST body to a GET query parameter, matching the MCP spec.
- **PWA fullscreen** — replaced the native browser Fullscreen API (which does not work in standalone PWA mode) with a window resize approach that works correctly in all PWA display contexts.
- **PWA expand button** — the session expand button is now hidden on displays 600px wide or narrower, where it overlapped touch targets.
- **`DATAWATCH_COMPLETE:` grace window** — a 30-second suppression window after daemon restart prevents stale pane output from triggering premature one-shot session termination.
- **tmux session kill** — the daemon no longer kills tmux sessions on exit unless the `KillSessionsOnExit` configuration flag is explicitly set, preventing session loss during daemon restarts.
- **Locale keys** — missing i18n keys `push_topic_alerts` and `observer_peers_by_node` added to all locale files.
- **PWA terminal refit** — the terminal view now correctly refits on soft keyboard show/hide on mobile.

---

## Security

- **16 Dependabot vulnerabilities patched** — a full sweep of Go module dependencies addressed all outstanding Dependabot security advisories.
- **Trivy container scanning** — the CI pipeline now runs Trivy on the container image as part of every build. Results are uploaded as CI artifacts.
- **CVE exception documentation** — known-acceptable CVEs (where the vulnerable code path is not reachable in datawatch's deployment model) are documented in `docs/security-review.md` with rationale and accepted-risk sign-off.
- **gitleaks false-positive suppression** — CI gitleaks scanning now correctly ignores test fixture files that contain non-secret values that match secret patterns (e.g., test bearer tokens in E2E story assertions).

---

## Upgrade Guide

### From v7.x

There are no breaking changes. The v8.0.0 daemon reads all v7.x configuration files without modification.

**Federation CBAC** — existing federated peers defined before v8.0 have no capability grants by default. After upgrading, assign the `federation-peer` built-in group (or a custom group) to each peer to restore cross-host access. Peers without capability grants receive 403 on all gated endpoints rather than the previous undefined behavior.

**Compute node routing** — existing compute nodes default to `direct` routing mode. No migration required. New routing modes are additive.

**Routing modes** — `docker-network` requires that the Docker CLI (`docker`) be available on the daemon host's `PATH`. The daemon does not use the Docker SDK. If Docker is not installed, nodes configured with `docker-network` routing will fail at `EnsureRunning` with a clear error.

**OneShot sessions** — the `DATAWATCH_COMPLETE:` sentinel was already in use by autonomous agents. The one-shot session mode subscribes to the same signal. No changes required to existing autonomous task configurations.

**Version check:**
```
datawatch version
# → v8.0.0
```

---

## Commit Log (feat/fix/security)

The following commits represent the feature and fix work in this release. Test iteration commits (of which there were many — a testament to the rigor of the release process) are omitted for brevity.

```
65e9ef55  feat(#BL316): S1 federation CBAC + peer registry REST API
77c3d268  feat(#BL316): S2 federation fan-out, cross-host input, MCP/CLI/comm/PWA 7-surface parity
d944c4ea  feat(#BL316): S3 systematic fedCap sweep — enforce CBAC across all REST handlers
08aefa91  feat(#BL317): MCP SSE federated authentication and capability enforcement
78d6ee1f  feat(BL312): Remote Servers card — card order, federated access controls, CBAC UI
dfa63a7f  feat(BL318-BL321): v8.0 routing foundation + docker/proxy/gemini/opencode-api adapters
4c50e451  feat(#BL318-BL323): v8.0.0 compute node routing, new adapters, E2E tests
3ac3a2c0  feat(e2e): complete E2E test harness — fixtures, Dockerfile.dev, mock-opencode
a50d5dbd  feat(BL318-BL323): proper v8.0 routing test catalog + parallel runner
65971f15  feat(session): add OneShot mode — DATAWATCH_COMPLETE terminates session
fc55e46b  feat(test): enable autonomous/memory/ollama in test config; add PWA stories
ac1dbca7  feat(test): plugins/skills/peer E2E coverage
c8ddc3ff  security: add Trivy container scanning + document CVE exceptions for v8.0.0
cf359dbe  fix(security): patch all 16 Dependabot vulnerabilities
87afc5e6  fix(inference): tombstone auto-created LLM deletes; fix disable no-op
648b5980  fix(llm): add GET /api/llms/{name}/models endpoint
14302692  fix(mcp): add webDo helper to include Bearer auth on internal HTTP calls
dba2c117  fix(cli): daemonGet/daemonJSON add Bearer auth token from config or DATAWATCH_TOKEN env
9a3b44e8  fix: suppress DATAWATCH_COMPLETE: during 30s post-restart grace window
cbebe234  fix: strip DATAWATCH_COMPLETE: lines from pane_capture instead of dropping frame
d3e6f1e0  fix(BL315): replace native browser fullscreen with PWA window resize
95cbfc7b  fix(BL315): hide PWA expand button on narrow displays (≤600px)
24fc7a16  fix(BL312): merge Remote Servers card into Comms tab, remove servers tab
cb78d37e  fix: gate tmux session kill on KillSessionsOnExit config flag
28f51911  fix(locale): add missing push_topic_alerts and observer_peers_by_node keys
cb764be4  fix(pwa): refit terminal on keyboard show; restructure testing docs
86c0faf3  fix(ci): suppress gitleaks false positives in test story fixtures
734463cc  docs: add GPU/ollama howto and docker-compose GPU config
b742c419  docs(howtos): add pre-condition cross-references to all 27 remaining howtos
```

---

*datawatch v8.0.0 — built with care, tested to exhaustion.*
