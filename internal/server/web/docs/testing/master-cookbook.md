# datawatch Master Test Cookbook

**How to update**: Run `bash scripts/run-tests.sh` — updates this file automatically after every run.

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
| T2 | TS-010 | POST /api/autonomous/prds creates PRD | surface:api feature:sessions feature:automata | 📋 planned | — | — |
| T2 | TS-011 | GET /api/sessions returns array | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-012 | hook-event Start returns 200 | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-013 | hook-event Activity returns 200 | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-014 | hook-event Stop returns 200 | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-015 | GET /api/channel/history shape | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-016 | POST /api/channel/reply returns 200 | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-017 | PUT /api/config session.recent_session_minutes round-trip | surface:api feature:sessions feature:config | 📋 planned | — | — |
| T2 | TS-018 | GET /api/stats session_stats present | surface:api feature:sessions | 📋 planned | — | — |
| T2 | TS-019 | DELETE /api/autonomous/prds/{id} hard delete | surface:api feature:sessions feature:automata | 📋 planned | — | — |
| T3 | TS-020 | POST /api/autonomous/prds creates with backend field | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-021 | GET /api/autonomous/prds/{id} round-trip | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-022 | GET /api/autonomous/prds/{id}/children empty array | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-023 | PUT /api/autonomous/prds/{id} title update | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-024 | POST /api/autonomous/prds/{id}/decompose | surface:api feature:automata conflict:llm | 📋 planned | — | — |
| T3 | TS-025 | POST /api/autonomous/prds/{id}/set_llm round-trip | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-026 | Project profile create + attach to PRD | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-027 | Cluster profile create + attach | surface:api feature:automata | 📋 planned | — | — |
| T3 | TS-028 | PUT /api/autonomous/config per_story_approval round-trip | surface:api feature:automata feature:config | 📋 planned | — | — |
| T3 | TS-029 | DELETE PRD + profiles cleanup | surface:api feature:automata | 📋 planned | — | — |
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
| T6 | TS-058 | keepass config section present | surface:api feature:secrets feature:config conflict:keepassxc | 📋 planned | — | — |
| T6 | TS-059 | 1Password config section present | surface:api feature:secrets feature:config conflict:op | 📋 planned | — | — |
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
| T11 | TS-140 | PWA: Automata/PRD list renders | surface:pwa feature:pwa feature:automata conflict:pwa | 📋 planned | — | — |
| T11 | TS-141 | PWA: Secrets panel renders | surface:pwa feature:pwa feature:secrets conflict:pwa | 📋 planned | — | — |
| T11 | TS-142 | PWA: Plugins panel renders | surface:pwa feature:pwa feature:plugins conflict:pwa | 📋 planned | — | — |
| T11 | TS-143 | PWA: Full page load no console errors | surface:pwa feature:pwa conflict:pwa | 📋 planned | — | — |
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
| T16 | TS-227 | Howto: prd-dag-orchestrator | surface:api feature:howto feature:automata | 📋 planned | — | — |
| T16 | TS-228 | Howto: channel-state-engine | surface:api feature:howto feature:sessions | 📋 planned | — | — |
| T16 | TS-229 | Howto: voice-input | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-230 | Howto: v7-compute-migration | surface:api feature:howto | 📋 planned | — | — |
| T16 | TS-231 | Howto: screenshots (if any) | surface:api feature:howto | 📋 planned | — | — |
| T17 | TS-240 | Journey: research (memory + KG + MCP) | surface:api surface:mcp feature:journey feature:memory feature:kg | 📋 planned | — | — |
| T17 | TS-241 | Journey: autonomous (PRD lifecycle) | surface:api feature:journey feature:automata | 📋 planned | — | — |
| T17 | TS-242 | Journey: monitoring (webhook + comm stats) | surface:api surface:comms feature:journey feature:comms | 📋 planned | — | — |
| T17 | TS-243 | Journey: secrets (create + ref + delete) | surface:api feature:journey feature:secrets | 📋 planned | — | — |
| T17 | TS-244 | Journey: council (2 personas + run + cancel) | surface:api feature:journey feature:council | 📋 planned | — | — |
| T17 | TS-245 | Journey: update check shape | surface:api feature:journey | 📋 planned | — | — |
| T17 | TS-246 | Journey: identity + algorithm | surface:api feature:journey | 📋 planned | — | — |
| T17 | TS-247 | Journey: MCP tools (recall + kg_query) | surface:mcp feature:journey feature:mcp feature:memory feature:kg | 📋 planned | — | — |
| T17 | TS-248 | Journey: schedule lifecycle | surface:api feature:journey | 📋 planned | — | — |
| T17 | TS-249 | Journey: full session lifecycle | surface:api surface:comms feature:journey feature:sessions | 📋 planned | — | — |

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
Evidence: internal/server/web/docs/testing/runs/YYYY-MM-DD-NNN/evidence/TS-NNN/
Story: internal/server/web/docs/testing/v7.0.0/plan.md#TS-NNN

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
# Full run (all 17 sprints)
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

# Skip external-dependency tests
bash scripts/run-tests.sh --skip-conflict=llm --skip-conflict=signal

# Exit codes
# 0 — all passed/skipped
# 1 — failures (non-blocking)
# 2 — blocking failure halted (fix + --resume-from)
```
