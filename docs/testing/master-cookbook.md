# datawatch Master Test Cookbook

**How to update**: Run `bash ../datawatch-testing/run-tests.sh` from anywhere — the script lives outside the repo in the sibling `datawatch-testing/` folder. After each run it automatically syncs results back to `datawatch/docs/testing/`. Commit with the suggested git command printed at the end.

**Testing folder**: `../datawatch-testing/` (sibling of the `datawatch` repo, not inside it)
- Script: `../datawatch-testing/run-tests.sh`
- Data dir: `../datawatch-testing/.datawatch-test-<hash>/` — unique per invocation (hash = shell PID); prevents parallel-run conflicts
- Evidence: `../datawatch-testing/runs/YYYY-MM-DD-NNN/`
- Canonical docs (this file + plan): `datawatch/docs/testing/` (auto-synced from testing folder after each run)

**Parallel run isolation**: Each invocation automatically gets a unique `TEST_RUN_HASH` (from `$$`) so data dirs don't collide. For full port isolation between parallel runs, set `TEST_PORT_OFFSET=<N>` (shifts all daemon ports by N) or override `TEST_BASE`/`TEST_TLS` directly:
```bash
# Two parallel runs on different port sets:
TEST_PORT_OFFSET=0 bash run-tests.sh  &  # ports 18080/18443/18081/18433
TEST_PORT_OFFSET=10 bash run-tests.sh &  # ports 18090/18453/18091/18443
```

**Monitor live**: Open the dashboard at `https://localhost:8443` — the **🔬 Smoke Run** card shows real-time progress while `release-smoke.sh` or `run-tests.sh` runs. The card polls `GET /api/smoke/progress` and shows a selectable list of run envelopes written to `~/.datawatch/smoke-runs/`. Add it via Edit → Add Card → Smoke Run.

---

## Test Environment

### Infrastructure

| Component | Value |
|-----------|-------|
| LLM backend (default) | Ollama at `http://datawatch:11434`, model: `qwen3:1.7b` |
| LLM backend (Claude) | Requires `CLAUDE_API_KEY` env var; uses `claude-haiku-4-5` with `quick` effort |
| Memory embedder | Ollama `nomic-embed-text` at `http://datawatch:11434` |
| Signal | Account `+18435409771`, group: `YOJtFDXm8WQCjna6dVGTOM8b4+aINRx4D4QgQ8Nmo54=`, config: `/home/dmz/.local/share/signal-cli` |
| Kubernetes | `kubectl --context=testing` (3-node cluster) — use `import-kubectl-context.sh` to inject |
| GitHub | Via local `gh` CLI authenticated to operator account; test repos created with random names + deleted on cleanup |
| PWA testing | Chrome headless via CDP (`pwa_cdp.py`) — auto-detected in `run-tests.sh`; falls back to API-only if Chrome unavailable |
| KeePass | NOT AVAILABLE — `keepassxc-cli` not installed |
| 1Password | NOT AVAILABLE — `op` CLI not installed |
| ntfy | NOT AVAILABLE by default — set `TEST_NTFY_TOPIC` to enable TS-099 |
| Slack / Discord / Telegram / Matrix / Twilio / Email | NOT CONFIGURED — always skip |

### Credential Management (Secrets-Driven Architecture)

**Isolation model**: Test daemon (ports 18xxx) runs completely isolated from production (8xxx). All external service credentials are injected via secrets manager at startup.

**GitHub credentials**:
- Master script creates random private test repo: `gh repo create datawatch-test-<timestamp> --private`
- Repo deleted during cleanup: `gh repo delete --confirm`
- Test code uses local `gh` CLI with operator's authenticated account
- No hardcoded GitHub PATs in test scripts

**Claude credentials** (major releases only, via `CLAUDE_API_KEY` env):
```bash
# Before running tests:
export CLAUDE_API_KEY="sk-ant-..."
bash scripts/run-tests.sh

# At daemon startup:
# - Config includes claude block with api_key_ref: ${secret:claude-test-api-key}
# - Secrets manager injects CLAUDE_API_KEY automatically
```

**Kubernetes credentials**:
```bash
# Import kubectl context to test daemon:
./scripts/import-kubectl-context.sh --context=testing --target-daemon=http://localhost:18080 --token=$TEST_TOKEN

# Kubectl config retrieved from secrets during test setup
```

**Auth Import/Export Utilities**:
- `scripts/import-kubectl-context.sh` — export K8s context to secrets
- `scripts/import-claude-credentials.sh` — import Claude API key to secrets
- `scripts/export-ssh-pubkey.sh` — export SSH public key for agent auth

**PID Validation** (prevents accidental production daemon kill):
- All test daemon kill commands validate: PID exists AND process listens on port 18080 (not 8080)
- `_validate_test_daemon_pid()` helper ensures kill targets correct daemon

### Cannot Be Tested

The following items are excluded from automated runs. Gaps are documented, not hidden.

- **KeePass backend** (`[conflict:keepassxc]`): `keepassxc-cli` not installed. TS-058 always skips.
- **1Password backend** (`[conflict:op]`): `op` CLI not installed. TS-059 always skips.
- **ntfy** (`[conflict:ntfy]`): `TEST_NTFY_TOPIC` not set. TS-099 skips unless the env var is provided at runtime.
- **Slack, Discord, Telegram, Matrix, Twilio, Email comm backends**: Not configured. T9 stubs always skip.
- **K8s full deployment** (TS-172, TS-173, TS-174, TS-176): Requires full Helm deployment workflow — skip in unit runs; namespace/configmap/probe-pod/configmap-shape stories (TS-170, TS-171, TS-175, TS-177) run normally against `kubectl --context=testing`.
- **T11 PWA stories**: Fully integrated into `run-tests.sh` via `pwa_cdp.py` (Chrome DevTools Protocol). Chrome headless starts automatically on port `CHROME_DEBUG_PORT` (default 19222). If Chrome is not available, each test falls back to an API-only assertion. Suppress with `--skip-conflict=pwa`.

---

## Latest Run Summary

| Field | Value |
|-------|-------|
| Version | — |
| Date | — |
| Pass | 0 |
| Skip | 0 |
| Fail | 0 |
| Total | 0 |
| Coverage | 0% |

---

## Story Status

| Sprint | TS-ID | Title | Tags | Status | Last Run | Notes |
|--------|-------|-------|------|--------|----------|-------|
| T1 | TS-001 | Health endpoint on test port returns ok + version | surface:api feature:bootstrap | 📋 planned | — | — |
| T1 | TS-002 | Version matches expected | surface:api feature:bootstrap | 📋 planned | — | — |
| T1 | TS-003 | 401 without token on /api/stats | surface:api feature:bootstrap | 📋 planned | — | — |
| T1 | TS-004 | 200 with correct token on /api/stats | surface:api feature:bootstrap | 📋 planned | — | — |
| T1 | TS-005 | TLS cert auto-generated | surface:api feature:bootstrap | 📋 planned | — | — |
| T1 | TS-006 | GET /api/config returns structured config | surface:api feature:bootstrap feature:config | 📋 planned | — | — |
| T1 | TS-007 | GET /api/stats returns full snapshot | surface:api feature:bootstrap | 📋 planned | — | — |
| T1 | TS-008 | GET /api/diagnose returns result array | surface:api feature:bootstrap | 📋 planned | — | — |
| T2 | TS-010 | POST /api/autonomous/prds creates Automaton | surface:api feature:sessions feature:automata | 📋 planned | — | — |
| T2 | TS-011 | GET /api/sessions returns array | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-012 | hook-event Start returns 200 | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-013 | hook-event Activity returns 200 | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-014 | hook-event Stop returns 200 | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-015 | GET /api/channel/history shape | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-016 | POST /api/channel/reply returns 200 | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-017 | PUT /api/config session.recent_session_minutes round-trip | surface:api feature:sessions feature:config | 📋 planned | — | — |
| T2 | TS-018 | GET /api/stats session_stats present | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-019 | DELETE /api/autonomous/prds/{id} hard delete (Automaton) | surface:api feature:sessions feature:automata | 📋 planned | — | — |
| T3 | TS-020 | POST /api/autonomous/prds creates Automaton with backend field | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-021 | GET /api/autonomous/prds/{id} Automaton round-trip | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-022 | GET /api/autonomous/prds/{id}/children empty array | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-023 | PUT /api/autonomous/prds/{id} title update | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-024 | POST /api/autonomous/prds/{id}/decompose | surface:api feature:automata conflict:llm | 📋 planned | — | — |
| T3 | TS-025 | POST /api/autonomous/prds/{id}/set_llm round-trip | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-026 | Project profile create + attach to Automaton | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-027 | Cluster profile create + attach to Automaton | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-028 | PUT /api/autonomous/config per_story_approval round-trip | surface:api feature:automata feature:config | 📋 planned | — | — |
| T3 | TS-029 | DELETE Automaton + profiles cleanup | surface:api feature:automata | 📋 planned | — | — |
| T4 | TS-030 | GET /api/council/personas returns array | surface:api feature:council | 📋 planned | — | — |
| T4 | TS-031 | POST /api/council/personas creates persona | surface:api feature:council | 📋 planned | — | — |
| T4 | TS-032 | GET /api/council/personas/{id} round-trip | surface:api feature:council | 📋 planned | — | — |
| T4 | TS-033 | POST /api/council/run returns id | surface:api feature:council | 📋 planned | — | — |
| T4 | TS-034 | POST /api/council/cancel/{council_id} returns 200/404 | surface:api feature:council | 📋 planned | — | — |
| T4 | TS-035 | GET /api/stats comm_stats council entry | surface:api feature:council | 📋 planned | — | — |
| T4 | TS-036 | PUT /api/council/personas/{id} role update | surface:api feature:council | 📋 planned | — | — |
| T4 | TS-037 | DELETE /api/council/personas/{id} returns 204 | surface:api feature:council | 📋 planned | — | — |
| T5 | TS-040 | GET /api/memory/stats enabled + count | surface:api feature:memory | 📋 planned | — | — |
| T5 | TS-041 | POST /api/memory/save returns id | surface:api feature:memory | 📋 planned | — | — |
| T5 | TS-042 | GET /api/memory/list contains saved id | surface:api feature:memory | 📋 planned | — | — |
| T5 | TS-043 | MCP memory_recall finds saved text | surface:api surface:mcp feature:memory | 📋 planned | — | — |
| T5 | TS-044 | DELETE /api/memory/{id} returns 200 | surface:api feature:memory | 📋 planned | — | — |
| T5 | TS-045 | GET /api/memory/kg/stats returns shape | surface:api feature:kg | 📋 planned | — | — |
| T5 | TS-046 | POST /api/memory/kg/add returns id | surface:api feature:kg | 📋 planned | — | — |
| T5 | TS-047 | GET /api/memory/kg?entity= returns triple | surface:api feature:kg | 📋 planned | — | — |
| T5 | TS-048 | MCP kg_query entity non-empty result | surface:api surface:mcp feature:kg | 📋 planned | — | — |
| T5 | TS-049 | DELETE /api/memory/kg/{id} or stats decrement | surface:api feature:kg | 📋 planned | — | — |
| T6 | TS-050 | GET /api/secrets/vault/status shape | surface:api feature:secrets | 📋 planned | — | — |
| T6 | TS-051 | POST /api/secrets creates secret | surface:api feature:secrets | 📋 planned | — | — |
| T6 | TS-052 | GET /api/secrets contains test-secret | surface:api feature:secrets | 📋 planned | — | — |
| T6 | TS-053 | GET /api/secrets/{id} name + backend present | surface:api feature:secrets | 📋 planned | — | — |
| T6 | TS-054 | DELETE /api/secrets/{id} returns 200 | surface:api feature:secrets | 📋 planned | — | — |
| T6 | TS-055 | GET /api/config mcp.enabled present | surface:api feature:config | 📋 planned | — | — |
| T6 | TS-056 | PUT /api/config skip_permissions round-trip | surface:api feature:config | 📋 planned | — | — |
| T6 | TS-057 | PUT /api/config autonomous.enabled round-trip | surface:api feature:config | 📋 planned | — | — |
| T6 | TS-058 | keepass config section present | surface:api feature:secrets feature:config conflict:keepassxc | 📋 planned | — | always skip — keepassxc-cli not installed |
| T6 | TS-059 | 1Password config section present | surface:api feature:secrets feature:config conflict:op | 📋 planned | — | always skip — op CLI not installed |
| T7 | TS-060 | GET /api/plugins returns array | surface:api feature:plugins | 📋 planned | — | — |
| T7 | TS-061 | GET /api/tooling/status returns shape | surface:api feature:plugins | 📋 planned | — | — |
| T7 | TS-062 | GET /api/skills/registries returns array | surface:api feature:skills | 📋 planned | — | — |
| T7 | TS-063 | GET /api/skills returns array | surface:api feature:skills | 📋 planned | — | — |
| T7 | TS-064 | MCP memory_recall via POST /api/mcp/call | surface:api surface:mcp feature:mcp | 📋 planned | — | — |
| T7 | TS-065 | GET /api/mcp/docs >= 30 tools | surface:api surface:mcp feature:mcp | 📋 planned | — | — |
| T7 | TS-066 | GET /api/autonomous/scan/config shape | surface:api feature:automata | 📋 planned | — | — |
| T7 | TS-067 | GET /api/templates array shape | surface:api feature:plugins | 📋 planned | — | — |
| T8 | TS-070 | GET /api/mcp/tools count >= 30 | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-071 | POST /api/mcp/call memory_recall result shape | surface:mcp feature:mcp feature:memory | 📋 planned | — | — |
| T8 | TS-072 | MCP tool annotations.readOnlyHint field | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-073 | GET /api/mcp/resources count >= 5 | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-074 | GET /api/mcp/resources/read?uri=datawatch://version | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-075 | GET /api/mcp/resources/read?uri=datawatch://sessions | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-076 | GET /api/mcp/resources/templates count >= 4 | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-077 | GET /api/mcp/prompts count >= 5 | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-078 | POST /api/mcp/prompts/get analyze-session messages array | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-079 | POST /api/mcp/sample structured response | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-080 | POST /api/mcp/elicit structured response | surface:mcp feature:mcp | 📋 planned | — | — |
| T8 | TS-081 | Channel bridge discovers same tool count | surface:mcp feature:mcp | 📋 planned | — | — |
| T9 | TS-090 | DNS comm backend enable round-trip | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-091 | POST /api/test/message !status via DNS | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-092 | GET /api/stats DNS entry enabled:true | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-093 | DNS comm backend disable restore | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-094 | Start local webhook listener | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-095 | Webhook comm backend enable round-trip | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-096 | POST /api/test/message triggers webhook | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-097 | GET /api/stats Webhook msg_sent >= 1 | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-098 | Webhook comm backend disable restore | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-099 | ntfy comm send + stats | surface:comms feature:comms conflict:ntfy | 📋 planned | — | — |
| T9 | TS-100 | Signal comm send + stats | surface:comms feature:comms conflict:signal | 📋 planned | — | — |
| T9 | TS-101 | !help via POST /api/test/message | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-102 | !sessions via POST /api/test/message | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-103 | !status via POST /api/test/message | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-104 | !alert list via POST /api/test/message | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-105 | !memory recall via POST /api/test/message | surface:comms feature:comms feature:memory | 📋 planned | — | — |
| T9 | TS-106 | GET /api/commands list | surface:comms feature:comms | 📋 planned | — | — |
| T9 | TS-107 | GET /api/stats comm_stats Web/MCP present | surface:comms feature:comms | 📋 planned | — | — |
| T10 | TS-110 | CLI: version matches TEST_VERSION | surface:cli feature:cli | 📋 planned | — | — |
| T10 | TS-111 | CLI: status returns running | surface:cli feature:cli | 📋 planned | — | — |
| T10 | TS-112 | CLI: sessions list exits 0 | surface:cli feature:cli feature:sessions | 📋 planned | — | — |
| T10 | TS-113 | CLI: config get server.port | surface:cli feature:cli feature:config | 📋 planned | — | — |
| T10 | TS-114 | CLI: config set + verify + restore | surface:cli feature:cli feature:config | 📋 planned | — | — |
| T10 | TS-115 | CLI: update --check exits 0 | surface:cli feature:cli | 📋 planned | — | — |
| T10 | TS-116 | CLI: plugins list exits 0 | surface:cli feature:cli feature:plugins | 📋 planned | — | — |
| T10 | TS-117 | CLI: secrets list exits 0 | surface:cli feature:cli feature:secrets | 📋 planned | — | — |
| T10 | TS-118 | CLI: agents list exits 0 | surface:cli feature:cli feature:automata | 📋 planned | — | — |
| T10 | TS-119 | CLI: mcp resources list exits 0 | surface:cli feature:cli feature:mcp | 📋 planned | — | — |
| T10 | TS-120 | CLI: mcp prompts list exits 0 | surface:cli feature:cli feature:mcp | 📋 planned | — | — |
| T10 | TS-121 | CLI: diagnose exits 0 | surface:cli feature:cli | 📋 planned | — | — |
| T11 | TS-130 | PWA: auth token set, no 401s in console | surface:pwa feature:pwa conflict:pwa | 📋 planned | — | — |
| T11 | TS-131 | PWA: Sessions panel renders | surface:pwa feature:pwa feature:sessions conflict:pwa | 📋 planned | — | — |
| T11 | TS-132 | PWA: Stats panel shows live data | surface:pwa feature:pwa conflict:pwa | 📋 planned | — | — |
| T11 | TS-133 | PWA: New session form visible | surface:pwa feature:pwa conflict:pwa | 📋 planned | — | — |
| T11 | TS-134 | PWA: WebSocket connects | surface:pwa feature:pwa conflict:pwa | 📋 planned | — | — |
| T11 | TS-135 | PWA: Alerts panel renders | surface:pwa feature:pwa conflict:pwa | 📋 planned | — | — |
| T11 | TS-136 | PWA: Settings panel opens | surface:pwa feature:pwa conflict:pwa | 📋 planned | — | — |
| T11 | TS-137 | PWA: Config PUT via settings | surface:pwa feature:pwa feature:config conflict:pwa | 📋 planned | — | — |
| T11 | TS-138 | PWA: MCP tools panel >= 30 tools | surface:pwa feature:pwa feature:mcp conflict:pwa | 📋 planned | — | — |
| T11 | TS-139 | PWA: Council personas panel renders | surface:pwa feature:pwa feature:council conflict:pwa | 📋 planned | — | — |
| T11 | TS-140 | PWA: Automata list renders | surface:pwa feature:pwa feature:automata conflict:pwa | 📋 planned | — | — |
| T11 | TS-141 | PWA: Secrets panel renders | surface:pwa feature:pwa feature:secrets conflict:pwa | 📋 planned | — | — |
| T11 | TS-142 | PWA: Plugins panel renders | surface:pwa feature:pwa feature:plugins conflict:pwa | 📋 planned | — | — |
| T11 | TS-143 | PWA: Full page load no console errors | surface:pwa feature:pwa conflict:pwa | 📋 planned | — | — |
| T11 | TS-144 | PWA: Dashboard panel renders smoke cards | surface:pwa feature:pwa feature:bootstrap conflict:pwa | 📋 planned | — | — |
| T11 | TS-145 | PWA: LLM edit panel shows session field toggles | surface:pwa feature:pwa feature:config conflict:pwa | 📋 planned | — | — |
| T11 | TS-146 | PWA: Guardrail library list renders | surface:pwa feature:pwa feature:automata conflict:pwa | 📋 planned | — | — |
| T12 | TS-150 | Filters CRUD round-trip | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-151 | Schedules CRUD round-trip | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-152 | GET /api/observer/peers shape | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-153 | GET /api/identity shape | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-154 | PATCH /api/identity role round-trip | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-155 | GET /api/algorithm phases shape | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-156 | Algorithm start + advance phases | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-157 | GET /api/evals/suites shape | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-158 | POST /api/evals/run result | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-159 | GET /api/compute/nodes shape | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-160 | GET /api/cost/rates shape | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-161 | GET /api/observer/peers shape (duplicate parity check) | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-162 | GET /api/routing-rules shape | surface:api feature:parity | 📋 planned | — | — |
| T12 | TS-163 | GET /api/orchestrator/graphs shape or 404 | surface:api feature:parity | 📋 planned | — | — |
| T13 | TS-164 | Second isolated daemon health check | surface:docker feature:bootstrap | 📋 planned | — | — |
| T13 | TS-165 | Session creation in isolated instance | surface:docker feature:sessions | 📋 planned | — | — |
| T13 | TS-166 | Memory save in isolated instance | surface:docker feature:memory | 📋 planned | — | — |
| T13 | TS-167 | Config GET in isolated instance | surface:docker feature:config | 📋 planned | — | — |
| T13 | TS-168 | Stop + restart: memory persists | surface:docker feature:memory | 📋 planned | — | — |
| T13 | TS-169 | Isolated stats shows separate uptime | surface:docker feature:bootstrap | 📋 planned | — | — |
| T13 | TS-170 | Stop: data dir persists on disk | surface:docker feature:bootstrap | 📋 planned | — | — |
| T13 | TS-171 | Cleanup docker-sim data dir | surface:docker feature:bootstrap | 📋 planned | — | — |
| T14 | TS-172 | kubectl create namespace datawatch-e2e | surface:k8s feature:k8s conflict:k8s | 📋 planned | — | — |
| T14 | TS-173 | Apply k8s deployment manifest | surface:k8s feature:k8s conflict:k8s | 📋 planned | — | — |
| T14 | TS-174 | Pods reach Running state | surface:k8s feature:k8s conflict:k8s | 📋 planned | — | — |
| T14 | TS-175 | Health via port-forward | surface:k8s feature:k8s conflict:k8s | 📋 planned | — | — |
| T14 | TS-176 | Session creation via forwarded port | surface:k8s feature:k8s feature:sessions conflict:k8s | 📋 planned | — | — |
| T14 | TS-177 | GET configmaps shape | surface:k8s feature:k8s conflict:k8s | 📋 planned | — | — |
| T14 | TS-178 | kubectl delete namespace datawatch-e2e | surface:k8s feature:k8s conflict:k8s | 📋 planned | — | — |
| T14 | TS-179 | Verify namespace gone | surface:k8s feature:k8s conflict:k8s | 📋 planned | — | — |
| T15 | TS-180 | Sessions 7-surface parity | surface:api surface:cli surface:mcp surface:comms feature:parity feature:sessions | 📋 planned | — | — |
| T15 | TS-181 | Memory 7-surface parity | surface:api surface:mcp surface:comms feature:parity feature:memory | 📋 planned | — | — |
| T15 | TS-182 | Config parity matrix (5 key fields) | surface:api surface:cli feature:parity feature:config | 📋 planned | — | — |
| T15 | TS-183 | Hook event parity (4 backends) | surface:api feature:parity feature:sessions | 📋 planned | — | — |
| T15 | TS-184 | Comm verb parity (5 verbs) | surface:comms feature:parity | 📋 planned | — | — |
| T15 | TS-185 | Locale completeness (5 files) | surface:pwa feature:parity feature:locale | 📋 planned | — | — |
| T15 | TS-186 | Config alignment YAML vs REST | surface:api feature:parity feature:config | 📋 planned | — | — |
| T15 | TS-187 | Comm backend config parity (11 backends) | surface:api surface:comms feature:parity | 📋 planned | — | — |
| T15 | TS-188 | MCP tool count parity (bridge vs REST) | surface:mcp feature:parity feature:mcp | 📋 planned | — | — |
| T15 | TS-189 | PWA Settings parity (7 sections) | surface:pwa feature:parity conflict:pwa | 📋 planned | — | — |
| T15 | TS-190 | Comm stats parity all enabled backends | surface:comms feature:parity feature:comms | 📋 planned | — | — |
| T16 | TS-200 | Howto: setup-and-install | surface:api feature:howto feature:bootstrap | 📋 planned | — | — |
| T16 | TS-201 | Howto: chat-and-llm-quickstart | surface:api feature:howto conflict:llm | 📋 planned | — | — |
| T16 | TS-202 | Howto: sessions-deep-dive | surface:api feature:howto feature:sessions | 📋 planned | — | — |
| T16 | TS-203 | Howto: autonomous-planning | surface:api feature:howto feature:automata | 📋 planned | — | — |
| T16 | TS-204 | Howto: autonomous-review-approve | surface:api feature:howto feature:automata | 📋 planned | — | — |
| T16 | TS-205 | Howto: council-mode | surface:api feature:howto feature:council | 📋 planned | — | — |
| T16 | TS-206 | Howto: cross-agent-memory | surface:api surface:mcp feature:howto feature:memory feature:kg | 📋 planned | — | — |
| T16 | TS-207 | Howto: secrets-manager | surface:api feature:howto feature:secrets | 📋 planned | — | — |
| T16 | TS-208 | Howto: comm-channels | surface:comms feature:howto feature:comms | 📋 planned | — | — |
| T16 | TS-209 | Howto: alerts-and-notifications | surface:api surface:comms feature:howto | 📋 planned | — | — |
| T16 | TS-210 | Howto: claude-hooks | surface:api feature:howto feature:sessions | 📋 planned | — | — |
| T16 | TS-211 | Howto: mcp-tools | surface:mcp feature:howto feature:mcp | 📋 planned | — | — |
| T16 | TS-212 | Howto: docs-as-mcp | surface:mcp feature:howto feature:mcp | 📋 planned | — | — |
| T16 | TS-213 | Howto: daemon-operations | surface:api feature:howto feature:bootstrap | 📋 planned | — | — |
| T16 | TS-214 | Howto: llm-registry | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-215 | Howto: profiles | surface:api feature:howto feature:automata | 📋 planned | — | — |
| T16 | TS-216 | Howto: pipeline-chaining | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-217 | Howto: skills-sync | surface:api feature:howto feature:skills | 📋 planned | — | — |
| T16 | TS-218 | Howto: push-notifications | surface:api feature:howto feature:comms | 📋 planned | — | — |
| T16 | TS-219 | Howto: identity-and-telos | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-220 | Howto: algorithm-mode | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-221 | Howto: evals | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-222 | Howto: federated-observer | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-223 | Howto: compute-nodes | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-224 | Howto: container-workers | surface:api feature:howto feature:automata | 📋 planned | — | — |
| T16 | TS-225 | Howto: tailscale-mesh | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-226 | Howto: ollama-marketplace | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-227 | Howto: automaton-dag-orchestrator | surface:api feature:howto feature:automata | 📋 planned | — | — |
| T16 | TS-228 | Howto: channel-state-engine | surface:api feature:howto feature:sessions | 📋 planned | — | — |
| T16 | TS-229 | Howto: voice-input | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-230 | Howto: v7-compute-migration | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-231 | Howto: screenshots (if any) | surface:api feature:howto | 📋 planned | — | — |
| T17 | TS-240 | Journey: research (memory + KG + MCP) | surface:api surface:mcp feature:journey feature:memory feature:kg | 📋 planned | — | — |
| T17 | TS-241 | Journey: autonomous (Automaton lifecycle) | surface:api feature:journey feature:automata | 📋 planned | — | — |
| T17 | TS-242 | Journey: monitoring (webhook + comm stats) | surface:api surface:comms feature:journey feature:comms | 📋 planned | — | — |
| T17 | TS-243 | Journey: secrets (create + ref + delete) | surface:api feature:journey feature:secrets | 📋 planned | — | — |
| T17 | TS-244 | Journey: council (2 personas + run + cancel) | surface:api feature:journey feature:council | 📋 planned | — | — |
| T17 | TS-245 | Journey: update check shape | surface:api feature:journey | 📋 planned | — | — |
| T17 | TS-246 | Journey: identity + algorithm | surface:api feature:journey | 📋 planned | — | — |
| T17 | TS-247 | Journey: MCP tools (recall + kg_query) | surface:mcp feature:journey feature:mcp feature:memory feature:kg | 📋 planned | — | — |
| T17 | TS-248 | Journey: schedule lifecycle | surface:api feature:journey | 📋 planned | — | — |
| T17 | TS-249 | Journey: full session lifecycle | surface:api surface:comms feature:journey feature:sessions | 📋 planned | — | — |
| T18 | TS-250 | GET /api/splash/info returns hostname+version | surface:api feature:bootstrap | 📋 planned | — | — |
| T18 | TS-251 | GET /api/openapi.yaml returns valid YAML with openapi: 3.0.x | surface:api feature:bootstrap | 📋 planned | — | — |
| T18 | TS-252 | GET /api/docs returns Swagger HTML (200) | surface:api feature:bootstrap | 📋 planned | — | — |
| T18 | TS-253 | GET /api/cooldown returns {active, until} shape | surface:api feature:config | 📋 planned | — | — |
| T18 | TS-254 | POST /api/cooldown set + GET verify + DELETE clear | surface:api feature:config | 📋 planned | — | — |
| T18 | TS-255 | GET /api/devices returns array (push device registry) | surface:api feature:config | 📋 planned | — | — |
| T18 | TS-256 | POST /api/devices/register shape round-trip | surface:api feature:config | 📋 planned | — | — |
| T18 | TS-257 | GET /api/federation/sessions returns {primary:[]} shape | surface:api feature:parity | 📋 planned | — | — |
| T18 | TS-258 | GET /api/marketplace/ollama/catalog returns catalog array | surface:api feature:parity | 📋 planned | — | — |
| T18 | TS-259 | GET /api/openwebui/models returns array | surface:api feature:parity | 📋 planned | — | — |
| T18 | TS-260 | GET /api/orchestrator/verdicts returns {verdicts:[]} shape | surface:api feature:parity | 📋 planned | — | — |
| T18 | TS-261 | GET /api/proxy/ missing-server-name 400/error | surface:api feature:parity | 📋 planned | — | — |
| T18 | TS-262 | GET /api/templates returns array | surface:api feature:plugins | 📋 planned | — | — |
| T18 | TS-263 | POST /api/templates creates; GET retrieves; DELETE removes | surface:api feature:plugins | 📋 planned | — | — |
| T18 | TS-264 | POST /api/assist endpoint exists (405 on GET) | surface:api feature:parity | 📋 planned | — | — |
| T18 | TS-265 | GET /api/splash/logo 404 is acceptable | surface:api feature:bootstrap | 📋 planned | — | — |
| T18 | TS-266 | GET /api/servers + GET /api/servers/health shape | surface:api feature:parity | 📋 planned | — | — |
| T19 | TS-270 | algorithm_list via MCP returns array | surface:mcp feature:mcp feature:algorithm | 📋 planned | — | — |
| T19 | TS-271 | algorithm_start + algorithm_get via MCP | surface:mcp feature:mcp feature:algorithm | 📋 planned | — | — |
| T19 | TS-272 | autonomous_config_get + autonomous_config_set round-trip via MCP | surface:mcp feature:mcp feature:automata | 📋 planned | — | — |
| T19 | TS-273 | autonomous_status via MCP returns {enabled,...} shape | surface:mcp feature:mcp feature:automata | 📋 planned | — | — |
| T19 | TS-274 | autonomous_type_list via MCP returns array | surface:mcp feature:mcp feature:automata | 📋 planned | — | — |
| T19 | TS-275 | backends_list via MCP returns {llm:[...]} shape | surface:mcp feature:mcp feature:config | 📋 planned | — | — |
| T19 | TS-276 | compute_node_list via MCP returns array | surface:mcp feature:mcp feature:compute | 📋 planned | — | — |
| T19 | TS-277 | compute_node_add + compute_node_get + compute_node_delete CRUD via MCP | surface:mcp feature:mcp feature:compute | 📋 planned | — | — |
| T19 | TS-278 | cooldown_status + cooldown_set + cooldown_clear via MCP | surface:mcp feature:mcp feature:config | 📋 planned | — | — |
| T19 | TS-279 | cost_rates + cost_summary shape via MCP | surface:mcp feature:mcp feature:config | 📋 planned | — | — |
| T19 | TS-280 | council_config_get + council_config_set round-trip via MCP | surface:mcp feature:mcp feature:council | 📋 planned | — | — |
| T19 | TS-281 | daemon_logs via MCP returns log lines array | surface:mcp feature:mcp feature:bootstrap | 📋 planned | — | — |
| T19 | TS-282 | detection_config_get + detection_config_set round-trip via MCP | surface:mcp feature:mcp feature:sessions | 📋 planned | — | — |
| T19 | TS-283 | dns_channel_config_get + dns_channel_config_set round-trip via MCP | surface:mcp feature:mcp feature:comms | 📋 planned | — | — |
| T19 | TS-284 | docs_search for "sessions" returns results with howto refs | surface:mcp feature:mcp feature:howto | 📋 planned | — | — |
| T19 | TS-285 | docs_list_howtos returns >=20 howtos | surface:mcp feature:mcp feature:howto | 📋 planned | — | — |
| T19 | TS-286 | docs_read for "daemon-operations" returns content | surface:mcp feature:mcp feature:howto | 📋 planned | — | — |
| T19 | TS-287 | docs_apply for curated howto exec_steps executes via MCP | surface:mcp feature:mcp feature:howto | 📋 planned | — | — |
| T19 | TS-288 | eval_list_suites + eval_run smoke suite shape via MCP | surface:mcp feature:mcp feature:evals | 📋 planned | — | — |
| T19 | TS-289 | federation_meta_peers + federation_sessions shape via MCP | surface:mcp feature:mcp feature:parity | 📋 planned | — | — |
| T19 | TS-290 | guardrail_library_list + guardrail_profile CRUD via MCP | surface:mcp feature:mcp feature:automata | 📋 planned | — | — |
| T19 | TS-291 | llm_list + llm_get + llm_enable/disable round-trip via MCP | surface:mcp feature:mcp feature:config | 📋 planned | — | — |
| T19 | TS-292 | marketplace_ollama_catalog + marketplace_pull_task shape via MCP | surface:mcp feature:mcp feature:parity | 📋 planned | — | — |
| T19 | TS-293 | memory_scope_recall + memory_scope_borrow + memory_scope_seed via MCP | surface:mcp feature:mcp feature:memory | 📋 planned | — | — |
| T19 | TS-294 | observer_config_get + observer_peers_list + observer_stats via MCP | surface:mcp feature:mcp feature:parity | 📋 planned | — | — |
| T19 | TS-295 | orchestrator_config_get + orchestrator_graph_list + orchestrator_verdicts via MCP | surface:mcp feature:mcp feature:parity | 📋 planned | — | — |
| T19 | TS-296 | pipeline_list + pipeline_start + pipeline_status shape via MCP | surface:mcp feature:mcp feature:parity | 📋 planned | — | — |
| T19 | TS-297 | routing_rules_list + routing_rules_test shape via MCP | surface:mcp feature:mcp feature:parity | 📋 planned | — | — |
| T19 | TS-298 | tailscale_status + tailscale_nodes shape via MCP | surface:mcp feature:mcp feature:parity | 📋 planned | — | — |
| T19 | TS-299 | telemetry_list + telemetry_get shape via MCP | surface:mcp feature:mcp feature:parity | 📋 planned | — | — |
| T19 | TS-300 | tooling_status + tooling_gitignore + tooling_cleanup shape via MCP | surface:mcp feature:mcp feature:plugins | 📋 planned | — | — |
| T20 | TS-310 | datawatch autonomous list exits 0 | surface:cli feature:cli feature:automata | 📋 planned | — | — |
| T20 | TS-311 | datawatch autonomous template-list exits 0 | surface:cli feature:cli feature:automata | 📋 planned | — | — |
| T20 | TS-312 | datawatch algorithm list exits 0 | surface:cli feature:cli feature:algorithm | 📋 planned | — | — |
| T20 | TS-313 | datawatch compute list exits 0 | surface:cli feature:cli feature:compute | 📋 planned | — | — |
| T20 | TS-314 | datawatch compute add + show + delete CRUD round-trip | surface:cli feature:cli feature:compute | 📋 planned | — | — |
| T20 | TS-315 | datawatch council list exits 0 | surface:cli feature:cli feature:council | 📋 planned | — | — |
| T20 | TS-316 | datawatch llm list exits 0 | surface:cli feature:cli feature:config | 📋 planned | — | — |
| T20 | TS-317 | datawatch llm add + show + delete round-trip | surface:cli feature:cli feature:config | 📋 planned | — | — |
| T20 | TS-318 | datawatch routing-rules list exits 0 | surface:cli feature:cli feature:parity | 📋 planned | — | — |
| T20 | TS-319 | datawatch routing-rules test exits 0 | surface:cli feature:cli feature:parity | 📋 planned | — | — |
| T20 | TS-320 | datawatch rtk check exits 0 | surface:cli feature:cli | 📋 planned | — | — |
| T20 | TS-321 | datawatch tailscale status exits 0 | surface:cli feature:cli feature:parity | 📋 planned | — | — |
| T20 | TS-322 | datawatch evals runs exits 0 | surface:cli feature:cli feature:evals | 📋 planned | — | — |
| T20 | TS-323 | datawatch pipeline list exits 0 | surface:cli feature:cli feature:parity | 📋 planned | — | — |
| T20 | TS-324 | datawatch memory list exits 0 | surface:cli feature:cli feature:memory | 📋 planned | — | — |
| T20 | TS-325 | datawatch memory recall "test query" exits 0 | surface:cli feature:cli feature:memory | 📋 planned | — | — |
| T20 | TS-326 | datawatch secrets list exits 0 | surface:cli feature:cli feature:secrets | 📋 planned | — | — |
| T20 | TS-327 | datawatch secrets set + get + delete CRUD round-trip | surface:cli feature:cli feature:secrets | 📋 planned | — | — |
| T20 | TS-328 | datawatch observer peers list exits 0 | surface:cli feature:cli feature:parity | 📋 planned | — | — |
| T20 | TS-329 | datawatch orchestrator graphs list exits 0 | surface:cli feature:cli feature:parity | 📋 planned | — | — |
| T20 | TS-330 | datawatch skills list exits 0 | surface:cli feature:cli feature:skills | 📋 planned | — | — |
| T20 | TS-331 | datawatch skills registry list exits 0 | surface:cli feature:cli feature:skills | 📋 planned | — | — |
| T20 | TS-332 | datawatch plugins list exits 0 | surface:cli feature:cli feature:plugins | 📋 planned | — | — |
| T20 | TS-333 | datawatch identity show exits 0 | surface:cli feature:cli feature:parity | 📋 planned | — | — |
| T20 | TS-334 | datawatch identity configure shape check exits 0 | surface:cli feature:cli feature:parity | 📋 planned | — | — |
| T20 | TS-335 | datawatch schedule list exits 0 | surface:cli feature:cli feature:schedules | 📋 planned | — | — |
| T20 | TS-336 | datawatch filter list exits 0 | surface:cli feature:cli feature:filters | 📋 planned | — | — |
| T20 | TS-337 | datawatch cost summary exits 0 | surface:cli feature:cli feature:config | 📋 planned | — | — |
| T20 | TS-338 | datawatch analytics exits 0 | surface:cli feature:cli feature:parity | 📋 planned | — | — |
| T20 | TS-339 | datawatch tooling status exits 0 | surface:cli feature:cli feature:plugins | 📋 planned | — | — |
| T20 | TS-340 | datawatch about exits 0 (version + credits) | surface:cli feature:cli feature:bootstrap | 📋 planned | — | — |
| T21 | TS-350 | docs_search "enable memory sqlite" returns result with howto ref | surface:mcp feature:mcp feature:howto feature:memory | 📋 planned | — | — |
| T21 | TS-351 | docs_list_howtos contains cross-agent-memory | surface:mcp feature:mcp feature:howto feature:memory | 📋 planned | — | — |
| T21 | TS-352 | docs_read "cross-agent-memory" returns content with exec_steps | surface:mcp feature:mcp feature:howto feature:memory | 📋 planned | — | — |
| T21 | TS-353 | docs_apply executes steps and returns 200/OK per step | surface:mcp feature:mcp feature:howto feature:memory | 📋 planned | — | — |
| T21 | TS-354 | POST /api/assist "how do I configure sqlite memory" returns guidance | surface:api feature:parity feature:howto | 📋 planned | — | — |
| T22 | TS-360 | GET /api/smoke/progress returns 204 when no run active | surface:api feature:bootstrap | 📋 planned | — | — |
| T22 | TS-361 | Running release-smoke.sh writes progress JSON before first section | surface:api feature:bootstrap | 📋 planned | — | — |
| T22 | TS-362 | Progress JSON has correct shape (version/started_at/active/sections/...) | surface:api feature:bootstrap | 📋 planned | — | — |
| T22 | TS-363 | After smoke completes, active=false in progress JSON | surface:api feature:bootstrap | 📋 planned | — | — |
| T22 | TS-364 | DELETE /api/smoke/progress removes file, next GET returns 204 | surface:api feature:bootstrap | 📋 planned | — | — |
| T23 | TS-365 | POST /api/sessions/{id}/input sends text with Enter | surface:api feature:sessions | 📋 planned | — | — |
| T23 | TS-366 | GET /api/autonomous/guardrails returns library array | surface:api feature:automata | 📋 planned | — | — |
| T23 | TS-367 | POST /api/autonomous/guardrail-profiles creates profile | surface:api feature:automata | 📋 planned | — | — |
| T23 | TS-368 | GET /api/autonomous/guardrail-profiles/{id} round-trip | surface:api feature:automata | 📋 planned | — | — |
| T23 | TS-369 | PUT /api/autonomous/guardrail-profiles/{id} updates profile | surface:api feature:automata | 📋 planned | — | — |
| T23 | TS-370 | DELETE /api/autonomous/guardrail-profiles/{id} returns 200 | surface:api feature:automata | 📋 planned | — | — |
| T24 | TS-371 | LLM add with session fields round-trip | surface:api feature:config | 📋 planned | — | — |
| T24 | TS-372 | LLM PATCH session field update | surface:api feature:config | 📋 planned | — | — |
| T24 | TS-373 | datawatch secrets import github exits 0 | surface:cli feature:cli feature:secrets | 📋 planned | — | — |
| T24 | TS-374 | datawatch secrets import claude exits 0 | surface:cli feature:cli feature:secrets | 📋 planned | — | — |
| T24 | TS-375 | GET /api/sessions/{id}/telemetry returns shape | surface:api feature:sessions feature:automata | 📋 planned | — | — |
| T25 | TS-376 | LLM enable toggle skips pretest for session-backend kinds (aider/goose/shell) | surface:api feature:config | 📋 planned | — | — |
| T25 | TS-377 | LLM enable toggle runs pretest for inference kinds (ollama/openwebui) | surface:api feature:config | 📋 planned | — | — |
| T25 | TS-378 | GET /api/evals returns {runs:[{id,name,status,score,created_at}]} shape | surface:api feature:evals | 📋 planned | — | — |
| T25 | TS-379 | GET /api/memory/search returns [] JSON (not 500) when embedder unavailable | surface:api feature:memory | 📋 planned | — | — |
| T25 | TS-380 | POST /api/autonomous/prds/{id}/decompose respects effort timeout (high→15min) | surface:api feature:automata conflict:llm | 📋 planned | — | — |
| T25 | TS-381 | GET /api/push/<topic> streams SSE events (ntfy-compat) | surface:api feature:push | 📋 planned | — | — |
| T25 | TS-382 | POST /api/push/<topic> publishes event to subscribers | surface:api feature:push | 📋 planned | — | — |
| T25 | TS-383 | GET /.well-known/unifiedpush returns discovery doc with version:1 | surface:api feature:push | 📋 planned | — | — |
| T25 | TS-384 | POST /api/push/register stores endpoint idempotent by client_id | surface:api feature:push | 📋 planned | — | — |
| T25 | TS-385 | PWA /locales/en.json, de.json, es.json, fr.json, ja.json all load 200 | surface:pwa feature:locale | 📋 planned | — | — |
| T25 | TS-386 | PWA locale switcher persists selection and reloads with translated strings | surface:pwa feature:locale | 📋 planned | — | — |

---

## Bug Workflow

When a test fails, follow this workflow. The runner does steps 1–2 automatically.
Claude handles steps 3–6 while running tests.

### Step 1 — Failure captured

The runner writes every `ko()` to `runs/YYYY-MM-DD-NNN/failures.jsonl`:
```json
{"story":"TS-042","desc":"memory recall did not return stored entry","tags":"surface:api feature:memory","blocking":false,"evidence":"...","timestamp":"2026-05-13T..."}
```

Blocking failures also print `FAIL_BLOCKING` on stdout, triggering immediate pause if `--fail-fast-blocking` was set.

### Step 2 — Plan updated

Mark the story in `v7.0.0/plan.md` and this cookbook: `📋 planned` → `🔴 failed`.

### Step 3 — BL filed (agent-spawned)

For each entry in `failures.jsonl`, spawn or run an agent to file a backlog item:

**Classification rules:**
| Condition | Severity | BL label |
|-----------|----------|----------|
| `blocking:true` | P0 — release blocker | `bug:release-blocker` |
| auth/health/daemon | P1 — critical | `bug:critical` |
| feature regression | P2 — major | `bug:major` |
| parity gap | P2 — major | `parity-gap` |
| cosmetic/skip reason | P3 — minor | `bug:minor` |

**BL entry format:**
```
**BL###** — [TS-NNN failing: short description]

Surface: <from tags>
Feature: <from tags>
Blocking: yes/no
Evidence: docs/testing/runs/YYYY-MM-DD-NNN/evidence/TS-NNN/
Story: docs/testing/v7.0.0/plan.md#TS-NNN

Steps to reproduce:
  bash scripts/run-tests.sh --story=TS-NNN

Expected: <from plan.md story Expected section>
Actual: <from failures.jsonl desc field>
```

### Step 4 — Fix (blocking bugs immediately, others queued)

**Blocking bug (`blocking:true`):** fix before continuing.
- Runner exits 2, halted at `$CURRENT_STORY`
- Fix the code, commit with `fix(BL###): ...`
- Update CHANGELOG, plan.md story status → 🔧 in-progress
- Rerun from where it stopped: `bash scripts/run-tests.sh --resume-from=TS-NNN`

**Non-blocking bug:** queue in backlog, continue running remaining stories.
- Runner continues automatically
- Fix in a follow-up commit after full run completes

### Step 5 — Retest

```bash
# Single story after fix
bash scripts/run-tests.sh --story=TS-NNN

# Resume from blocker after fix
bash scripts/run-tests.sh --resume-from=TS-NNN --fail-fast-blocking

# Full rerun
bash scripts/run-tests.sh
```

### Step 6 — Close

- Story status in plan.md + cookbook: `🔴 failed` → `✅ passed`
- Close or resolve BL entry with fix commit SHA + retest run date
- Update `CHANGELOG.md` entry for the release with the fixed BL numbers

---

## Runner Quick Reference

```bash
# Full run (all 22 sprints)
bash scripts/run-tests.sh

# Single story
bash scripts/run-tests.sh --story=TS-042

# Resume after fixing a blocker at TS-005
bash scripts/run-tests.sh --resume-from=TS-005

# Halt on first blocker (CI mode)
bash scripts/run-tests.sh --fail-fast-blocking

# Surface or feature slice
bash scripts/run-tests.sh --surface=api
bash scripts/run-tests.sh --feature=memory

# Specific sprint
bash scripts/run-tests.sh --sprint=T18
bash scripts/run-tests.sh --sprint=T19
bash scripts/run-tests.sh --sprint=T20

# Skip external-dependency tests
bash scripts/run-tests.sh --skip-conflict=llm --skip-conflict=signal --skip-conflict=tailscale

# Cost-gated tests (requires live LLM backend)
DW_MAJOR=1 bash scripts/run-tests.sh

# Exit codes
# 0 — all passed/skipped
# 1 — failures (non-blocking)
# 2 — blocking failure halted (fix + --resume-from)
```
