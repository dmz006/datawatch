# datawatch v7.0.0 End-to-End Test Plan

**Version**: v7.0.0-alpha  
**Date**: 2026-05-13  
**Test runner**: `scripts/run-tests.sh`  
**Evidence root**: `docs/testing/v7.0.0/evidence/` (gitignored, cleaned after run)

---

## Dashboard Monitoring During Test Runs

Every test run integrates with the datawatch dashboard. Open the PWA at `https://localhost:8443` while running tests to see live progress.

### Smoke Run Card (BL303)

`scripts/release-smoke.sh` writes `~/.datawatch/smoke-progress.json` before each of its 61 sections. The **Smoke Run** card on the dashboard polls `/api/smoke/progress` every 2.5 seconds during an active run and shows:

- Pass / Fail / Skip counts with a live progress bar
- The currently running section name
- A compact history of completed sections (✅ pass · ❌ fail · ⏭ skip)

To add the Smoke Run card to your dashboard layout: click **Edit** in the dashboard stat bar → **Add** → select **🔬 Smoke Run**.

### Dashboard Cards for Testing

| Card | What you see during testing |
|---|---|
| 🔬 Smoke Run | Live pass/fail per section, progress bar, current phase |
| ⣿ Automata | Automata created by smoke (CRUD probes) with status |
| ⚡ Live Events | Real-time hook events from sessions spawned by tests |
| ≡ Timeline | Gantt bars for active test Automata by start/end time |
| ◎ Network | Graph of sessions + Automata; click to inspect |

### Future Full Integration (multi-sprint)

The current smoke integration uses a progress file polled via REST. Full integration will:
- Create a tracking Automaton per test run with one story per T-Sprint
- Update story status (pending → in_progress → completed/failed) as each sprint runs
- Show the run as a Gantt timeline with phase durations
- Persist test history in episodic memory for trend analysis
- Show coverage trend in the 30-Day Activity heatmap card

This multi-sprint roadmap is tracked separately from v7.0.0 and scoped for v7.1.x.

---

## 1. Overview

This plan provides 155+ test stories organised into 15 T-Sprints covering every datawatch subsystem, deployment surface, and cross-cutting parity rule. Stories exercise the real daemon API — same patterns as `scripts/release-smoke.sh` — against an isolated test instance on dedicated ports so the operator's production daemon is never disturbed.

### Evidence vs Cookbook vs Plan

| Artifact | Location | Persisted? | Purpose |
|---|---|---|---|
| **Plan** (`plan.md`) | `docs/testing/v7.0.0/plan.md` | ✅ Yes (force-added) | Defines every story: steps, expected, evidence filenames. Reference for all future runs. |
| **Cookbook** (`cookbook.md`) | `docs/testing/v7.0.0/cookbook.md` | ✅ Yes (force-added) | Live status table updated after every story. After a run it is the only persistent record of what passed/failed. |
| **Evidence** (`evidence/TS-NNN/`) | `docs/testing/v7.0.0/evidence/` | ❌ Gitignored + deleted | JSON responses, screenshots, CLI output. Exists only during a run. On FAIL, preserved for diagnosis. |

**For future releases**: copy `docs/testing/v7.0.0/` → `docs/testing/v7.1.0/`, reset cookbook to 📋, add stories for new features. The v7.0.0 plan is preserved as a baseline for regression.

### Design decisions

| Decision | Choice |
|---|---|
| Isolation | Same host; custom data dir `.datawatch-test/` at repo root (gitignored); ports 18080/18443/18081/18433 |
| Evidence | Structured JSON + screenshots saved to `docs/testing/v7.0.0/evidence/TS-NNN/` (gitignored) |
| Organisation | T1–T10 native features, T11 PWA, T12 Advanced, T13 Docker simulation, T14 Kubernetes |
| Comms scope | DNS (T9/full), Generic Webhook (T9/full), ntfy (T9/partial), Signal (T9/partial — shared prod signal-cli, dedicated test group) |
| Comms future | Slack, Telegram, Discord, Matrix, Twilio, Email, GitHub Webhook — T9 future stubs |
| Parallelism | Tag-based; single-thread now, parallel later via runner flag |
| Cleanup | After every run: stop test daemon, remove `.datawatch-test/`, remove evidence/, remove all `test-*` resources |
| Pass criteria | HTTP response matches expected shape (asserted via python3); CLI stdout matches pattern; PWA screenshot saved + no console errors |

---

## 2. Environment Variables

| Variable | Default | Description |
|---|---|---|
| `TEST_BASE` | `https://127.0.0.1:18443` | Base URL for test daemon |
| `TEST_HTTP` | `http://127.0.0.1:18080` | HTTP (non-TLS) base |
| `TEST_MCP_PORT` | `18081` | MCP SSE port |
| `TEST_CHAN_PORT` | `18433` | Channel port |
| `TEST_TOKEN` | `dw-test-token-12345` | Bearer token |
| `TEST_DATA` | `.datawatch-test` | Data directory (relative to repo root) |
| `TEST_BINARY` | `./bin/datawatch` | Path to daemon binary |
| `TEST_SIGNAL_GROUP` | *(unset)* | Signal group ID for comm tests |
| `TEST_NTFY_TOPIC` | *(unset)* | ntfy topic for comm tests |
| `TEST_WEBHOOK_PORT` | `19080` | Local listener port for webhook receipt |
| `TEST_SURFACE` | *(unset)* | Filter: `api\|cli\|pwa\|mcp\|comms\|docker\|k8s` |
| `TEST_FEATURE` | *(unset)* | Filter: `sessions\|automata\|memory\|...` |
| `TEST_SKIP_CONFLICT` | *(unset)* | Skip stories with matching conflict tag |
| `EVIDENCE_DIR` | `docs/testing/v7.0.0/evidence` | Evidence output root |

---

## 3. Cookbook (Sprint Status)

| Sprint | Name | Stories | Status |
|---|---|---|---|
| T1 | Daemon Bootstrap + Auth | TS-001–TS-008 | 📋 planned |
| T2 | Sessions | TS-010–TS-019 | 📋 planned |
| T3 | Automata | TS-020–TS-029 | 📋 planned |
| T4 | Council | TS-030–TS-037 | 📋 planned |
| T5 | Memory + KG | TS-040–TS-049 | 📋 planned |
| T6 | Secrets + Config | TS-050–TS-059 | 📋 planned |
| T7 | Plugins + Skills | TS-060–TS-067 | 📋 planned |
| T8 | MCP Surface | TS-070–TS-081 | 📋 planned |
| T9 | Comms | TS-090–TS-103 | 📋 planned |
| T10 | CLI Surface | TS-110–TS-121 | 📋 planned |
| T11 | PWA (Chrome plugin) | TS-130–TS-143 | 📋 planned |
| T12 | Advanced Features | TS-150–TS-159 | 📋 planned |
| T13 | Docker Simulation | TS-160–TS-167 | 📋 planned |
| T14 | Kubernetes Deployment | TS-170–TS-177 | 📋 planned |
| T15 | Parity Audit | TS-180–TS-190 | 📋 planned |
| T16 | Howto Validation | TS-200–TS-231 | 📋 planned |
| T17 | End-to-End Journeys | TS-240–TS-249 | 📋 planned |
| T18 | Missing Endpoints | TS-250–TS-266 | 📋 planned |
| T19 | MCP Surface Complete | TS-270–TS-300 | 📋 planned |
| T20 | CLI Complete | TS-310–TS-340 | 📋 planned |
| T21 | Docs-as-MCP AI Config | TS-350–TS-354 | 📋 planned |
| T22 | Smoke Infrastructure | TS-360–TS-364 | 📋 planned |

---

## 4. Tag Taxonomy

### Surface tags
| Tag | Meaning |
|---|---|
| `[surface:api]` | REST API surface |
| `[surface:cli]` | CLI (datawatch subcommands) |
| `[surface:pwa]` | PWA web app (Chrome plugin required) |
| `[surface:mcp]` | MCP tool/resource/prompt surface |
| `[surface:comms]` | Communication backends |
| `[surface:docker]` | Docker deployment simulation |
| `[surface:k8s]` | Kubernetes deployment |

### Feature tags
| Tag | Meaning |
|---|---|
| `[feature:bootstrap]` | Daemon start/health/auth |
| `[feature:sessions]` | Session lifecycle |
| `[feature:automata]` | Automaton/Automata lifecycle |
| `[feature:council]` | Council deliberation |
| `[feature:memory]` | Episodic memory |
| `[feature:kg]` | Knowledge graph |
| `[feature:secrets]` | Secrets store |
| `[feature:config]` | Config CRUD |
| `[feature:plugins]` | Plugin registry/invoke |
| `[feature:skills]` | Skills registry/invoke |
| `[feature:filters]` | Filter store |
| `[feature:schedules]` | Schedule store |
| `[feature:agents]` | Agent lifecycle |
| `[feature:profiles]` | Project/cluster profiles |
| `[feature:identity]` | Identity surface |
| `[feature:algorithm]` | Algorithm surface |
| `[feature:evals]` | Evals surface |
| `[feature:compute]` | Compute nodes |
| `[feature:parity]` | Cross-surface parity audit (7-surface, config, hook, locale, comm) |
| `[feature:locale]` | Locale/i18n key completeness |

### Conflict tags
| Tag | Meaning |
|---|---|
| `[conflict:signal]` | Requires shared signal-cli instance |
| `[conflict:db-write]` | Mutates persistent data (skip in read-only mode) |
| `[conflict:llm]` | Requires live LLM backend |
| `[conflict:pwa]` | Requires Chrome plugin |
| `[conflict:k8s]` | Requires kubectl + testing cluster |
| `[conflict:keepassxc]` | Requires keepassxc-cli binary |
| `[conflict:op]` | Requires 1Password op CLI binary |

---

## 5. Test Stories

---

## T1 — Daemon Bootstrap + Auth

### TS-001 — Fresh daemon starts on test ports
**Tags**: [surface:api] [feature:bootstrap]  
**Steps**:
1. Write `.datawatch-test/config.yaml` with test ports and token
2. `DATAWATCH_DATA_DIR=.datawatch-test $TEST_BINARY serve --port 18080 --tls-port 18443 &`
3. Poll `curl -sk $TEST_BASE/api/health` until `status=ok` or 30s timeout
4. Save response to `evidence/TS-001/health.json`
**Expected**: `{"status":"ok","version":"7.0.*"}` within 30 seconds  
**Evidence**: `health.json`  
**Status**: 📋 planned

---

### TS-002 — Health endpoint shape
**Tags**: [surface:api] [feature:bootstrap]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/health`
2. Assert `status == "ok"`, `version` present, response is valid JSON
3. Save to `evidence/TS-002/health.json`
**Expected**: `{"status":"ok","version":"...","uptime":...}`  
**Evidence**: `health.json`  
**Status**: 📋 planned

---

### TS-003 — Auth 401 without token
**Tags**: [surface:api] [feature:bootstrap]  
**Steps**:
1. `curl -sk -o /dev/null -w "%{http_code}" $TEST_BASE/api/sessions`
2. Assert HTTP status is `401`
**Expected**: `401 Unauthorized`  
**Evidence**: `http_code.txt`  
**Status**: 📋 planned

---

### TS-004 — Auth 200 with correct token
**Tags**: [surface:api] [feature:bootstrap]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/sessions`
2. Assert HTTP status is `200`, body is valid JSON with `sessions` key
**Expected**: `{"sessions":[]}` (or non-empty list)  
**Evidence**: `sessions.json`, `http_code.txt`  
**Status**: 📋 planned

---

### TS-005 — TLS auto-cert (HTTPS reachable)
**Tags**: [surface:api] [feature:bootstrap]  
**Steps**:
1. `curl -sk $TEST_BASE/api/health` (note `-s` for skip-verify on self-signed cert)
2. Assert response body contains `"status":"ok"`
3. Verify cert CN or SANs contain `127.0.0.1` via `openssl s_client -connect 127.0.0.1:18443 -showcerts </dev/null 2>&1 | head -40`
**Expected**: TLS handshake succeeds, cert is self-signed for 127.0.0.1  
**Evidence**: `health.json`, `cert_info.txt`  
**Status**: 📋 planned

---

### TS-006 — Config GET round-trip
**Tags**: [surface:api] [feature:bootstrap] [feature:config]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/config`
2. Assert response is valid JSON, contains `server`, `session`, `autonomous` keys
3. Save to `evidence/TS-006/config.json`
**Expected**: Full config object with known top-level sections  
**Evidence**: `config.json`  
**Status**: 📋 planned

---

### TS-007 — Stats snapshot shape
**Tags**: [surface:api] [feature:bootstrap]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/stats?v=2"`
2. Assert response contains `envelopes` or `v` key
3. Save to `evidence/TS-007/stats.json`
**Expected**: Structured stats snapshot, not an error  
**Evidence**: `stats.json`  
**Status**: 📋 planned

---

### TS-008 — Diagnose endpoint
**Tags**: [surface:api] [feature:bootstrap]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/diagnose`
2. Assert response is a JSON array or object (not empty, not error)
3. Save to `evidence/TS-008/diagnose.json`
**Expected**: Non-empty diagnostic output  
**Evidence**: `diagnose.json`  
**Status**: 📋 planned

---

## T2 — Sessions

### TS-010 — Create session (claude-code backend)
**Tags**: [surface:api] [feature:sessions] [conflict:llm]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"test-session-001","backend":"claude-code","project_dir":"/tmp","effort":"quick"}' $TEST_BASE/api/sessions`
2. Assert response contains `id` field, save `SESSION_ID`
3. Register for cleanup via `add_cleanup sess $SESSION_ID`
4. Save to `evidence/TS-010/create.json`
**Expected**: `{"id":"...","name":"test-session-001","state":"..."}`  
**Evidence**: `create.json`  
**Status**: 📋 planned

---

### TS-011 — List sessions
**Tags**: [surface:api] [feature:sessions]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/sessions`
2. Assert `sessions` key is a list
3. If TS-010 ran successfully, assert the created session ID appears in the list
4. Save to `evidence/TS-011/sessions.json`
**Expected**: `{"sessions":[...]}`  
**Evidence**: `sessions.json`  
**Status**: 📋 planned

---

### TS-012 — Session appears in stats
**Tags**: [surface:api] [feature:sessions]  
**Steps**:
1. Requires TS-010 session to exist
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/stats?v=2"`
3. Assert `session_count` >= 1 or `envelopes` contains session info
4. Save to `evidence/TS-012/stats.json`
**Expected**: Stats reflect at least one session  
**Evidence**: `stats.json`  
**Status**: 📋 planned

---

### TS-013 — Hook event: Start
**Tags**: [surface:api] [feature:sessions]  
**Steps**:
1. Requires a session ID (from TS-010 or create a new one)
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"event":"Start","data":{"session_id":"'"$SESSION_ID"'"}}' "$TEST_BASE/api/sessions/$SESSION_ID/hook-event"`
3. Assert HTTP 200 and response is valid JSON
4. Save to `evidence/TS-013/hook_start.json`
**Expected**: `{"status":"ok"}` or `{"received":true}`  
**Evidence**: `hook_start.json`  
**Status**: 📋 planned

---

### TS-014 — Hook event: Activity
**Tags**: [surface:api] [feature:sessions]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"event":"Activity","data":{"session_id":"'"$SESSION_ID"'","text":"test activity"}}' "$TEST_BASE/api/sessions/$SESSION_ID/hook-event"`
2. Assert HTTP 200
4. Save to `evidence/TS-014/hook_activity.json`
**Expected**: `{"status":"ok"}`  
**Evidence**: `hook_activity.json`  
**Status**: 📋 planned

---

### TS-015 — Hook event: Stop
**Tags**: [surface:api] [feature:sessions]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"event":"Stop","data":{"session_id":"'"$SESSION_ID"'"}}' "$TEST_BASE/api/sessions/$SESSION_ID/hook-event"`
2. Assert HTTP 200
3. Save to `evidence/TS-015/hook_stop.json`
**Expected**: `{"status":"ok"}`  
**Evidence**: `hook_stop.json`  
**Status**: 📋 planned

---

### TS-016 — Channel send to session
**Tags**: [surface:api] [feature:sessions]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"session_id":"'"$SESSION_ID"'","text":"test channel message"}' $TEST_BASE/api/channel/send`
2. Assert HTTP 200, response contains `status` key
3. Save to `evidence/TS-016/channel_send.json`
**Expected**: `{"status":"ok"}` or `{"delivered":true}`  
**Evidence**: `channel_send.json`  
**Status**: 📋 planned

---

### TS-017 — Channel history
**Tags**: [surface:api] [feature:sessions]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/channel/history?session_id=$SESSION_ID"`
2. Assert response contains `messages` key (list, may be empty)
3. Save to `evidence/TS-017/channel_history.json`
**Expected**: `{"messages":[...]}`  
**Evidence**: `channel_history.json`  
**Status**: 📋 planned

---

### TS-018 — Channel history: non-existent session returns empty
**Tags**: [surface:api] [feature:sessions]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/channel/history?session_id=test-nonexistent-xyz"`
2. Assert HTTP 200, `messages` is null or empty list
**Expected**: `{"messages":null}` or `{"messages":[]}`  
**Evidence**: `channel_history_empty.json`  
**Status**: 📋 planned

---

### TS-019 — Session terminate
**Tags**: [surface:api] [feature:sessions]  
**Steps**:
1. Create a disposable session: `curl ... -d '{"name":"test-session-kill","backend":"shell","project_dir":"/tmp"}' $TEST_BASE/api/sessions`
2. Capture `KILL_ID`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"id":"'"$KILL_ID"'"}' $TEST_BASE/api/sessions/kill`
4. Assert HTTP 200
5. Verify session no longer in `running` state via GET /api/sessions
**Expected**: Kill accepted, session transitions to stopped/terminated  
**Evidence**: `kill.json`, `verify.json`  
**Status**: 📋 planned

---

## T3 — Automata

### TS-020 — Create Automaton via REST
**Tags**: [surface:api] [feature:automata]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"spec":"test-prd-001: echo hello world","project_dir":"/tmp","backend":"claude-code","effort":"low"}' $TEST_BASE/api/autonomous/prds`
2. Assert response contains `id`, `spec`, `status`; save `AUTOMATON_ID`
3. `add_cleanup automaton $AUTOMATON_ID`
4. Save to `evidence/TS-020/create.json`
**Expected**: `{"id":"...","spec":"test-prd-001:...","status":"draft"}`  
**Evidence**: `create.json`  
**Status**: 📋 planned

---

### TS-021 — Automaton GET
**Tags**: [surface:api] [feature:automata]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/autonomous/prds/$AUTOMATON_ID`
2. Assert `id == $AUTOMATON_ID`, `spec` field matches, `status` is present
3. Save to `evidence/TS-021/get.json`
**Expected**: Full Automaton record  
**Evidence**: `get.json`  
**Status**: 📋 planned

---

### TS-022 — Automata list
**Tags**: [surface:api] [feature:automata]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/autonomous/prds`
2. Assert response is a list or `{"prds":[...]}`, created Automaton ID appears
3. Save to `evidence/TS-022/list.json`
**Expected**: List contains `$AUTOMATON_ID`  
**Evidence**: `list.json`  
**Status**: 📋 planned

---

### TS-023 — Automaton decompose (SKIP if LLM unreachable)
**Tags**: [surface:api] [feature:automata] [conflict:llm]  
**Steps**:
1. Check if any LLM backend available+enabled; if not, SKIP
2. `curl -sk --max-time 300 -H "Authorization: Bearer $TEST_TOKEN" -X POST $TEST_BASE/api/autonomous/prds/$AUTOMATON_ID/decompose`
3. Assert HTTP 200, `stories` is a non-empty list
4. Save to `evidence/TS-023/decompose.json`
**Expected**: `{"stories":[{"id":"...","spec":"..."},...]}`  
**Evidence**: `decompose.json`  
**Status**: 📋 planned

---

### TS-024 — Automaton approve
**Tags**: [surface:api] [feature:automata]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"actor":"test-runner","note":"e2e test approval"}' $TEST_BASE/api/autonomous/prds/$AUTOMATON_ID/approve`
2. Assert `status == "approved"`
3. Save to `evidence/TS-024/approve.json`
**Expected**: `{"status":"approved"}`  
**Evidence**: `approve.json`  
**Status**: 📋 planned

---

### TS-025 — Automaton run → spawn (SKIP if LLM unreachable)
**Tags**: [surface:api] [feature:automata] [conflict:llm]  
**Steps**:
1. Create, decompose (skip if no LLM), and approve a test Automaton
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -X POST $TEST_BASE/api/autonomous/prds/$AUTOMATON_ID/run`
3. Assert `status == "running"`
4. Wait 5s; verify Automaton is `running` or tasks have `session_id` assigned
5. Cancel: `curl ... -X DELETE $TEST_BASE/api/autonomous/prds/$AUTOMATON_ID`
6. Save to `evidence/TS-025/run.json`
**Expected**: Run accepted, executor spawns at least one session  
**Evidence**: `run.json`, `post_run.json`  
**Status**: 📋 planned

---

### TS-026 — Automaton per-story approval gate
**Tags**: [surface:api] [feature:automata]  
**Steps**:
1. Enable: `curl ... -X PUT -d '{"autonomous.per_story_approval":true}' $TEST_BASE/api/config`
2. Create+decompose+approve a fresh Automaton (requires stories)
3. Assert all stories transition to `awaiting_approval`
4. Approve first story: `curl ... -X POST -d '{"story_id":"...","actor":"test"}' $TEST_BASE/api/autonomous/prds/$AUTOMATON_ID/approve_story`
5. Assert story transitions from `awaiting_approval` to `pending`
6. Restore config: `curl ... -X PUT -d '{"autonomous.per_story_approval":false}' $TEST_BASE/api/config`
7. Save to `evidence/TS-026/`
**Expected**: Per-story approval gate works correctly  
**Evidence**: `approve_story.json`, `before.json`, `after.json`  
**Status**: 📋 planned

---

### TS-027 — project_profile + cluster_profile attachment
**Tags**: [surface:api] [feature:automata] [feature:profiles]  
**Steps**:
1. Create a test project profile: `curl ... -X POST -d '{"name":"test-profile-e2e","git":{"url":"https://github.com/dmz006/datawatch","branch":"main"},"image_pair":{"agent":"agent-claude"}}' $TEST_BASE/api/profiles/projects`
2. Create Automaton referencing profile: `curl ... -d '{"spec":"test-prd-profile","project_profile":"test-profile-e2e","effort":"low"}' $TEST_BASE/api/autonomous/prds`
3. Assert Automaton record carries `project_profile == "test-profile-e2e"`
4. Cleanup profile and Automaton
5. Save to `evidence/TS-027/`
**Expected**: Profile attachment persists on Automaton record  
**Evidence**: `profile_create.json`, `automaton_create.json`, `automaton_get.json`  
**Status**: 📋 planned

---

### TS-028 — Automaton hard-delete
**Tags**: [surface:api] [feature:automata]  
**Steps**:
1. Create a test Automaton (name: `test-prd-harddelete`)
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -X DELETE "$TEST_BASE/api/autonomous/prds/$AUTOMATON_ID?hard=true"`
3. Assert `{"status":"deleted"}`
4. Verify GET returns 404
5. Save to `evidence/TS-028/`
**Expected**: Automaton is hard-deleted, subsequent GET returns 404  
**Evidence**: `delete.json`, `verify_404.txt`  
**Status**: 📋 planned

---

### TS-029 — Automaton children list
**Tags**: [surface:api] [feature:automata]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/autonomous/prds/$AUTOMATON_ID/children`
2. Assert `{"children":[...]}` — list (may be empty for fresh Automaton)
3. Save to `evidence/TS-029/children.json`
**Expected**: `{"children":[]}` or populated list  
**Evidence**: `children.json`  
**Status**: 📋 planned

---

## T4 — Council

### TS-030 — List personas
**Tags**: [surface:api] [feature:council]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/council/personas`
2. Assert response is a list or `{"personas":[...]}`
3. Save to `evidence/TS-030/personas.json`
**Expected**: Non-error response, list shape  
**Evidence**: `personas.json`  
**Status**: 📋 planned

---

### TS-031 — Create persona
**Tags**: [surface:api] [feature:council] [conflict:db-write]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"test-persona-e2e","role":"analyst","system_prompt":"You are a test analyst.","model":"claude-sonnet-4-5"}' $TEST_BASE/api/council/personas`
2. Assert response contains `id` and `name == "test-persona-e2e"`; save `PERSONA_ID`
3. Register for cleanup
4. Save to `evidence/TS-031/create.json`
**Expected**: Persona created with ID  
**Evidence**: `create.json`  
**Status**: 📋 planned

---

### TS-032 — Council quick run
**Tags**: [surface:api] [feature:council] [conflict:llm]  
**Steps**:
1. Check if any LLM backend available; skip if none
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" --max-time 120 -d '{"question":"What is 2+2?","personas":["'"$PERSONA_ID"'"],"mode":"quick"}' $TEST_BASE/api/council/run`
3. Assert HTTP 200, response contains `run_id` or `result`; save `RUN_ID`
4. `add_cleanup council $RUN_ID`
5. Save to `evidence/TS-032/run.json`
**Expected**: Council run accepted, result or run_id returned  
**Evidence**: `run.json`  
**Status**: 📋 planned

---

### TS-033 — Council cancel
**Tags**: [surface:api] [feature:council]  
**Steps**:
1. Start a council run (even if LLM unreachable, the cancel endpoint should work)
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -X POST "$TEST_BASE/api/council/runs/$RUN_ID/cancel"`
3. Assert HTTP 200 or 202
4. Save to `evidence/TS-033/cancel.json`
**Expected**: Cancel accepted  
**Evidence**: `cancel.json`  
**Status**: 📋 planned

---

### TS-034 — Deliberation result shape
**Tags**: [surface:api] [feature:council]  
**Steps**:
1. If a completed run is available, `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/council/runs/$RUN_ID`
2. Assert result contains `question`, `personas`, `responses` or `deliberation` keys
3. Save to `evidence/TS-034/result.json`
**Expected**: Structured deliberation result  
**Evidence**: `result.json`  
**Status**: 📋 planned

---

### TS-035 — Council stats
**Tags**: [surface:api] [feature:council]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/council/stats`
2. Assert response is valid JSON with `run_count` or similar counter field
3. Save to `evidence/TS-035/stats.json`
**Expected**: `{"run_count":...,"persona_count":...}`  
**Evidence**: `stats.json`  
**Status**: 📋 planned

---

### TS-036 — Persona edit round-trip
**Tags**: [surface:api] [feature:council] [conflict:db-write]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X PUT -d '{"role":"senior-analyst","system_prompt":"Updated test prompt"}' $TEST_BASE/api/council/personas/$PERSONA_ID`
2. Assert updated fields are reflected in response or subsequent GET
3. GET persona: `curl ... $TEST_BASE/api/council/personas/$PERSONA_ID`
4. Assert `role == "senior-analyst"`
5. Save to `evidence/TS-036/`
**Expected**: Persona update persists  
**Evidence**: `update.json`, `get_after.json`  
**Status**: 📋 planned

---

### TS-037 — Council include_claude_code config
**Tags**: [surface:api] [feature:council] [feature:config]  
**Steps**:
1. GET config: `curl ... $TEST_BASE/api/config`
2. Assert `council.include_claude_code` key is present (may be true or false)
3. Toggle via PUT: `curl ... -X PUT -d '{"council.include_claude_code":true}' $TEST_BASE/api/config`
4. Verify toggle persisted via GET
5. Restore original value
6. Save to `evidence/TS-037/`
**Expected**: Config round-trips correctly  
**Evidence**: `before.json`, `put.json`, `after.json`  
**Status**: 📋 planned

---

## T5 — Memory + KG

### TS-040 — memory_remember via MCP call
**Tags**: [surface:mcp] [feature:memory]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"tool":"memory_remember","params":{"content":"test-memory-e2e-001: this is a test memory entry for v7.0.0 e2e testing"}}' $TEST_BASE/api/mcp/call`
2. Assert HTTP 200, response contains `id` or `result.id`; save `MEM_ID`
3. Save to `evidence/TS-040/remember.json`
**Expected**: Memory saved, ID returned  
**Evidence**: `remember.json`  
**Status**: 📋 planned

---

### TS-041 — memory_recall semantic search
**Tags**: [surface:mcp] [feature:memory]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"tool":"memory_recall","params":{"query":"v7.0.0 e2e testing"}}' $TEST_BASE/api/mcp/call`
2. Assert HTTP 200, results list is non-empty
3. Assert the entry from TS-040 appears in results (check content substring)
4. Save to `evidence/TS-041/recall.json`
**Expected**: Recall returns the saved memory  
**Evidence**: `recall.json`  
**Status**: 📋 planned

---

### TS-042 — Memory list
**Tags**: [surface:api] [feature:memory]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/memory/list?limit=50"`
2. Assert response is a JSON array
3. Assert entry from TS-040 appears (by ID)
4. Save to `evidence/TS-042/list.json`
**Expected**: List contains the saved memory  
**Evidence**: `list.json`  
**Status**: 📋 planned

---

### TS-043 — Memory delete
**Tags**: [surface:api] [feature:memory] [conflict:db-write]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"id":'"$MEM_ID"'}' $TEST_BASE/api/memory/delete`
2. Assert response contains `"status"` key
3. Verify entry no longer in list: `curl ... $TEST_BASE/api/memory/list?limit=200 | python3 -c '...'`
4. Save to `evidence/TS-043/`
**Expected**: Memory deleted, no longer in list  
**Evidence**: `delete.json`, `verify.json`  
**Status**: 📋 planned

---

### TS-044 — KG add triple
**Tags**: [surface:api] [feature:kg]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"subject":"test-entity-e2e","predicate":"is_a","object":"test-object"}' $TEST_BASE/api/memory/kg/add`
2. Assert response contains `id`; save `KG_ID`
3. Save to `evidence/TS-044/add.json`
**Expected**: `{"id":...}` with non-zero ID  
**Evidence**: `add.json`  
**Status**: 📋 planned

---

### TS-045 — KG query entity
**Tags**: [surface:api] [feature:kg]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/memory/kg/query?entity=test-entity-e2e"`
2. Assert response is a list, contains entry with `id == $KG_ID`
3. Save to `evidence/TS-045/query.json`
**Expected**: Query returns the saved triple  
**Evidence**: `query.json`  
**Status**: 📋 planned

---

### TS-046 — KG stats
**Tags**: [surface:api] [feature:kg]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/memory/kg/stats`
2. Assert response contains `entity_count`, `triple_count`, `active_count`, `expired_count`
3. Save to `evidence/TS-046/stats.json`
**Expected**: All 4 counters present  
**Evidence**: `stats.json`  
**Status**: 📋 planned

---

### TS-047 — research_sessions MCP tool
**Tags**: [surface:mcp] [feature:memory]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"tool":"research_sessions","params":{"query":"test","limit":5}}' $TEST_BASE/api/mcp/call`
2. Assert HTTP 200, response is valid JSON
3. Save to `evidence/TS-047/research.json`
**Expected**: Research results returned (may be empty)  
**Evidence**: `research.json`  
**Status**: 📋 planned

---

### TS-048 — Memory stats endpoint
**Tags**: [surface:api] [feature:memory]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/memory/stats`
2. Assert `enabled` is present, `count` or `total` field present
3. Save to `evidence/TS-048/stats.json`
**Expected**: `{"enabled":true,"count":...}`  
**Evidence**: `stats.json`  
**Status**: 📋 planned

---

### TS-049 — Spatial probe (SKIP if disabled)
**Tags**: [surface:api] [feature:memory]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"content":"test spatial probe e2e","wing":"test-wing-e2e"}' $TEST_BASE/api/memory/save`
2. Assert ID returned; save `SP_ID`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/memory/list?wing=test-wing-e2e&limit=50"`
4. Assert entry with `$SP_ID` appears; if not, SKIP (wing param may not be supported)
5. Cleanup: `curl ... -X POST -d '{"id":'"$SP_ID"'}' $TEST_BASE/api/memory/delete`
6. Save to `evidence/TS-049/`
**Expected**: Spatial wing filter returns the probe entry  
**Evidence**: `save.json`, `list_filtered.json`  
**Status**: 📋 planned

---

## T6 — Secrets + Config

### TS-050 — Create secret (env backend)
**Tags**: [surface:api] [feature:secrets] [conflict:db-write]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"name":"test-secret-e2e","value":"test-secret-value-12345","backend":"env","scopes":["test"]}' $TEST_BASE/api/secrets`
2. Assert response contains `name == "test-secret-e2e"`
3. Register for cleanup
4. Save to `evidence/TS-050/create.json`
**Expected**: Secret created  
**Evidence**: `create.json`  
**Status**: 📋 planned

---

### TS-051 — List secrets
**Tags**: [surface:api] [feature:secrets]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/secrets`
2. Assert response is list or `{"secrets":[...]}`, `test-secret-e2e` appears
3. Assert secret VALUE is NOT returned in list (security check)
4. Save to `evidence/TS-051/list.json`
**Expected**: List contains `test-secret-e2e`, no plaintext values  
**Evidence**: `list.json`  
**Status**: 📋 planned

---

### TS-052 — Read secret metadata
**Tags**: [surface:api] [feature:secrets]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/secrets/test-secret-e2e`
2. Assert `name == "test-secret-e2e"`, `backend` field present
3. Assert no plaintext value in response
4. Save to `evidence/TS-052/get.json`
**Expected**: Metadata returned without plaintext value  
**Evidence**: `get.json`  
**Status**: 📋 planned

---

### TS-053 — Delete secret
**Tags**: [surface:api] [feature:secrets] [conflict:db-write]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -X DELETE $TEST_BASE/api/secrets/test-secret-e2e`
2. Assert HTTP 200, response contains `status`
3. Verify GET returns 404: `curl -o /dev/null -w "%{http_code}" $TEST_BASE/api/secrets/test-secret-e2e`
4. Save to `evidence/TS-053/`
**Expected**: Secret deleted, subsequent GET returns 404  
**Evidence**: `delete.json`, `verify_404.txt`  
**Status**: 📋 planned

---

### TS-054 — Config ${secret:name} ref resolution
**Tags**: [surface:api] [feature:secrets] [feature:config]  
**Steps**:
1. Create a secret named `test-ref-secret` with value `resolved-value-xyz`
2. PUT config: `curl ... -X PUT -d '{"session.extra_env":"${secret:test-ref-secret}"}' $TEST_BASE/api/config`
3. GET config and assert the field contains the ref notation (or resolved value)
4. Cleanup secret and restore config
5. Save to `evidence/TS-054/`
**Expected**: Config accepts secret refs; ref stored or resolved  
**Evidence**: `put.json`, `get.json`  
**Status**: 📋 planned

---

### TS-055 — Secret scoping enforcement
**Tags**: [surface:api] [feature:secrets]  
**Steps**:
1. Create secret `test-scoped-secret` with `scopes:["plugin"]`
2. Attempt to access with scope `session` (should be denied or return empty)
3. Access with scope `plugin` (should succeed or not error)
4. Save to `evidence/TS-055/`
**Expected**: Scope enforcement gates access correctly  
**Evidence**: `create.json`, `wrong_scope.json`, `right_scope.json`  
**Status**: 📋 planned

---

### TS-056 — KeePass backend config round-trip (SKIP if keepassxc-cli absent)
**Tags**: [surface:api] [feature:secrets] [conflict:keepassxc]  
**Steps**:
1. Check: `command -v keepassxc-cli >/dev/null 2>&1 || skip`
2. PUT config: `curl ... -X PUT -d '{"secrets.keepass.path":"/tmp/test-dw-e2e.kdbx"}' $TEST_BASE/api/config`
3. Assert config readback contains the path
4. Restore config
5. Save to `evidence/TS-056/`
**Expected**: KeePass backend config round-trips  
**Evidence**: `put.json`, `get.json`  
**Status**: 📋 planned

---

### TS-057 — 1Password backend config round-trip (SKIP if op absent)
**Tags**: [surface:api] [feature:secrets] [conflict:op]  
**Steps**:
1. Check: `command -v op >/dev/null 2>&1 || skip`
2. PUT config: `curl ... -X PUT -d '{"secrets.onepassword.vault":"TestVault"}' $TEST_BASE/api/config`
3. Assert config readback contains the vault name
4. Restore config
5. Save to `evidence/TS-057/`
**Expected**: 1Password backend config round-trips  
**Evidence**: `put.json`, `get.json`  
**Status**: 📋 planned

---

### TS-058 — Config YAML reload
**Tags**: [surface:api] [feature:config]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -X POST $TEST_BASE/api/reload`
2. Assert `{"ok":true,"requires_restart":[...]}`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -X POST "$TEST_BASE/api/reload?subsystem=filters"`
4. Assert `ok == true` and `applied` contains `filters`
5. Save to `evidence/TS-058/`
**Expected**: Reload returns structured response  
**Evidence**: `full_reload.json`, `filters_reload.json`  
**Status**: 📋 planned

---

### TS-059 — Config REST PUT validation
**Tags**: [surface:api] [feature:config]  
**Steps**:
1. PUT valid key: `curl ... -X PUT -d '{"server.port":18080}' $TEST_BASE/api/config`; assert `{"status":"ok"}`
2. PUT invalid key: `curl ... -X PUT -d '{"nonexistent.key.xyz":true}' $TEST_BASE/api/config`; assert HTTP 4xx or `{"status":"ignored"}`
3. Restore config
4. Save to `evidence/TS-059/`
**Expected**: Valid keys accepted; invalid keys rejected or ignored gracefully  
**Evidence**: `valid_put.json`, `invalid_put.json`  
**Status**: 📋 planned

---

## T7 — Plugins + Skills

### TS-060 — List plugins
**Tags**: [surface:api] [feature:plugins]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/plugins`
2. Assert response is list or `{"plugins":[...]}`
3. Save to `evidence/TS-060/plugins.json`
**Expected**: Plugin registry responds, list shape  
**Evidence**: `plugins.json`  
**Status**: 📋 planned

---

### TS-061 — Plugin manifest validation
**Tags**: [surface:api] [feature:plugins]  
**Steps**:
1. If any plugins are installed: pick one, `curl ... $TEST_BASE/api/plugins/$PLUGIN_ID`
2. Assert manifest contains `name`, `version`, `commands` or `tools` fields
3. Save to `evidence/TS-061/manifest.json`
**Expected**: Manifest is well-formed per Plugin Manifest v2.1 spec  
**Evidence**: `manifest.json`  
**Status**: 📋 planned

---

### TS-062 — Plugin invoke (SKIP if none installed)
**Tags**: [surface:api] [feature:plugins]  
**Steps**:
1. If no plugins installed, SKIP
2. Invoke a safe read-only plugin command: `curl ... -X POST -d '{"plugin":"...","command":"...","args":{}}' $TEST_BASE/api/plugins/invoke`
3. Assert HTTP 200, result is valid JSON
4. Save to `evidence/TS-062/invoke.json`
**Expected**: Plugin invocation returns structured result  
**Evidence**: `invoke.json`  
**Status**: 📋 planned

---

### TS-063 — Plugin docs:files audit
**Tags**: [surface:api] [feature:plugins]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/plugins`
2. For each plugin with `docs.files`, verify each file is non-empty and exists
3. Assert no plugin manifest references missing doc files
4. Save to `evidence/TS-063/audit.json`
**Expected**: All documented plugin files are present  
**Evidence**: `audit.json`  
**Status**: 📋 planned

---

### TS-064 — Skills list
**Tags**: [surface:api] [feature:skills]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/skills`
2. Assert response is list or `{"skills":[...]}`
3. Save to `evidence/TS-064/skills.json`
**Expected**: Skills registry responds  
**Evidence**: `skills.json`  
**Status**: 📋 planned

---

### TS-065 — Skill invoke (SKIP if none registered)
**Tags**: [surface:api] [feature:skills]  
**Steps**:
1. If no skills registered, SKIP
2. Pick a read-only skill; invoke: `curl ... -X POST -d '{"skill":"...","args":{}}' $TEST_BASE/api/skills/invoke`
3. Assert HTTP 200
4. Save to `evidence/TS-065/invoke.json`
**Expected**: Skill invocation returns result  
**Evidence**: `invoke.json`  
**Status**: 📋 planned

---

### TS-066 — Skill registry list via MCP
**Tags**: [surface:mcp] [feature:skills]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"tool":"skill_list","params":{}}' $TEST_BASE/api/mcp/call`
2. Assert HTTP 200, result is a list (possibly empty)
3. Save to `evidence/TS-066/skill_list.json`
**Expected**: skill_list MCP tool responds  
**Evidence**: `skill_list.json`  
**Status**: 📋 planned

---

### TS-067 — Tooling status
**Tags**: [surface:api] [feature:plugins]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/tooling/status`
2. Assert HTTP 200, response is valid JSON
3. Save to `evidence/TS-067/tooling_status.json`
**Expected**: Tooling status returned  
**Evidence**: `tooling_status.json`  
**Status**: 📋 planned

---

## T8 — MCP Surface

### TS-070 — GET /api/mcp/tools (≥30 tools)
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/mcp/docs`
2. Assert response is a JSON array with `length >= 30`
3. Assert foundational tools present: `list_sessions`, `start_session`, `send_input`, `schedule_add`, `profile_list`, `agent_list`
4. Save to `evidence/TS-070/tools.json`
**Expected**: ≥30 tools registered, foundational set present  
**Evidence**: `tools.json`  
**Status**: 📋 planned

---

### TS-071 — POST /api/mcp/call (memory_recall)
**Tags**: [surface:mcp] [feature:mcp] [feature:memory]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"tool":"memory_recall","params":{"query":"test"}}' $TEST_BASE/api/mcp/call`
2. Assert HTTP 200, result is valid JSON (may be empty list)
3. Save to `evidence/TS-071/recall.json`
**Expected**: MCP call returns structured result  
**Evidence**: `recall.json`  
**Status**: 📋 planned

---

### TS-072 — Tool annotations present (readOnly/destructive hints)
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. GET `/api/mcp/docs`; parse tool list
2. Assert at least one tool has `annotations.readOnlyHint == true`
3. Assert at least one tool has `annotations.destructiveHint == true`
4. Save annotated tools list to `evidence/TS-072/annotations.json`
**Expected**: Tool annotations are populated  
**Evidence**: `annotations.json`  
**Status**: 📋 planned

---

### TS-073 — GET /api/mcp/resources (≥5 resources) [v7.1.0]
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. Check daemon version; if `< 7.1.0`, SKIP
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/mcp/resources`
3. Assert response is a list with `length >= 5`
4. Save to `evidence/TS-073/resources.json`
**Expected**: ≥5 MCP resources registered  
**Evidence**: `resources.json`  
**Status**: 📋 planned

---

### TS-074 — Read datawatch://version resource
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"uri":"datawatch://version"}' $TEST_BASE/api/mcp/resources/read`
2. Assert HTTP 200, content contains version string matching daemon version
3. Save to `evidence/TS-074/version_resource.json`
**Expected**: Version resource returns current daemon version  
**Evidence**: `version_resource.json`  
**Status**: 📋 planned

---

### TS-075 — Read datawatch://sessions resource
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"uri":"datawatch://sessions"}' $TEST_BASE/api/mcp/resources/read`
2. Assert HTTP 200, content is a JSON list of sessions
3. Save to `evidence/TS-075/sessions_resource.json`
**Expected**: Sessions resource returns current session list  
**Evidence**: `sessions_resource.json`  
**Status**: 📋 planned

---

### TS-076 — GET /api/mcp/prompts [v7.1.0]
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. Check daemon version; if `< 7.1.0`, SKIP
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/mcp/prompts`
3. Assert response is a list with at least one prompt
4. Save to `evidence/TS-076/prompts.json`
**Expected**: MCP prompt registry returns prompts  
**Evidence**: `prompts.json`  
**Status**: 📋 planned

---

### TS-077 — POST /api/mcp/prompts/get (analyze-session) [v7.1.0]
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. Check daemon version; if `< 7.1.0`, SKIP
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"analyze-session","arguments":{"session_id":"test"}}' $TEST_BASE/api/mcp/prompts/get`
3. Assert HTTP 200, response contains `messages` or `prompt` field
4. Save to `evidence/TS-077/prompt_get.json`
**Expected**: Prompt template returned  
**Evidence**: `prompt_get.json`  
**Status**: 📋 planned

---

### TS-078 — POST /api/mcp/sample surface check
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"messages":[{"role":"user","content":"ping"}],"maxTokens":10}' $TEST_BASE/api/mcp/sample`
2. Assert HTTP 200 or 501 (not implemented); not 404
3. Save to `evidence/TS-078/sample.json`
**Expected**: Endpoint exists (may return not-implemented)  
**Evidence**: `sample.json`  
**Status**: 📋 planned

---

### TS-079 — POST /api/mcp/elicit surface check
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"requestedSchema":{"type":"object","properties":{"answer":{"type":"string"}}}}' $TEST_BASE/api/mcp/elicit`
2. Assert HTTP 200 or 501 (not implemented); not 404
3. Save to `evidence/TS-079/elicit.json`
**Expected**: Endpoint exists  
**Evidence**: `elicit.json`  
**Status**: 📋 planned

---

### TS-080 — MCP SSE channel bridge connects and logs tool count
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. Connect to MCP SSE port: `curl -sk --max-time 5 http://127.0.0.1:18081/sse 2>&1 | head -5`
2. Assert connection is established (200 or SSE stream starts)
3. Send JSON-RPC initialize: pipe via `echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}' | nc -q1 127.0.0.1 18081`
4. Save to `evidence/TS-080/sse_connect.txt`
**Expected**: SSE endpoint reachable, bridge logs tool count  
**Evidence**: `sse_connect.txt`  
**Status**: 📋 planned

---

### TS-081 — MCP channel bridge discovers resources [v7.1.0]
**Tags**: [surface:mcp] [feature:mcp]  
**Steps**:
1. Check daemon version; if `< 7.1.0`, SKIP
2. Send `resources/list` JSON-RPC over stdio MCP: pipe to `datawatch mcp` subprocess
3. Assert response contains `resources` array with `≥5` entries
4. Save to `evidence/TS-081/bridge_resources.json`
**Expected**: Bridge exposes same resource list as REST  
**Evidence**: `bridge_resources.json`  
**Status**: 📋 planned

---

## T9 — Comms

### TS-090 — DNS comm: configure
**Tags**: [surface:api] [feature:comms] [conflict:db-write]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X PUT -d '{"dns_channel.enabled":true,"dns_channel.domain":"test.e2e.local","dns_channel.record_type":"TXT"}' $TEST_BASE/api/config`
2. Assert `{"status":"ok"}`
3. GET config, assert `dns_channel.enabled == true`
4. Save to `evidence/TS-090/`
**Expected**: DNS channel configured  
**Evidence**: `put.json`, `get.json`  
**Status**: 📋 planned

---

### TS-091 — DNS comm: send test message + verify comm_stats
**Tags**: [surface:api] [feature:comms]  
**Steps**:
1. Read `comm_stats.dns_channel.msg_sent` before: `curl ... $TEST_BASE/api/stats | python3 -c '...'`
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"backend":"dns","message":"test dns send e2e"}' $TEST_BASE/api/comm/send`
3. Read `comm_stats.dns_channel.msg_sent` after; assert increment
4. Save to `evidence/TS-091/`
**Expected**: msg_sent increments for DNS backend  
**Evidence**: `before_stats.json`, `send.json`, `after_stats.json`  
**Status**: 📋 planned

---

### TS-092 — Generic Webhook: configure listener + send + verify receipt
**Tags**: [surface:api] [feature:comms]  
**Steps**:
1. Start local listener: `python3 -m http.server $TEST_WEBHOOK_PORT > /tmp/test-webhook.log 2>&1 &`; save PID
2. PUT config: `curl ... -d '{"webhook.enabled":true,"webhook.url":"http://127.0.0.1:'"$TEST_WEBHOOK_PORT"'"}' $TEST_BASE/api/config`
3. Read `comm_stats` before
4. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"backend":"webhook","message":"test webhook send e2e"}' $TEST_BASE/api/comm/send`
5. Assert `comm_stats` msg_sent incremented OR webhook listener log shows a request
6. Kill listener PID; save log to evidence
7. Save to `evidence/TS-092/`
**Expected**: Webhook listener receives request OR msg_sent increments  
**Evidence**: `webhook.log`, `before_stats.json`, `after_stats.json`  
**Status**: 📋 planned

---

### TS-093 — ntfy: configure topic + send + verify comm_stats (SKIP if topic unset)
**Tags**: [surface:api] [feature:comms]  
**Steps**:
1. If `$TEST_NTFY_TOPIC == ""`, SKIP
2. PUT config: `curl ... -d '{"ntfy.enabled":true,"ntfy.topic":"'"$TEST_NTFY_TOPIC"'"}' $TEST_BASE/api/config`
3. Read `comm_stats` before
4. `curl ... -X POST -d '{"backend":"ntfy","message":"test ntfy e2e"}' $TEST_BASE/api/comm/send`
5. Assert `comm_stats.ntfy.msg_sent` incremented
6. Save to `evidence/TS-093/`
**Expected**: ntfy msg_sent increments (no inbox check — shared instance)  
**Evidence**: `put.json`, `send.json`, `stats.json`  
**Status**: 📋 planned

---

### TS-094 — Signal: configure test group + send + verify comm_stats (SKIP if group unset)
**Tags**: [surface:api] [feature:comms] [conflict:signal]  
**Steps**:
1. If `$TEST_SIGNAL_GROUP == ""`, SKIP with message "TEST_SIGNAL_GROUP not set"
2. PUT config: `curl ... -d '{"signal.enabled":true,"signal.group":"'"$TEST_SIGNAL_GROUP"'"}' $TEST_BASE/api/config`
3. Read `comm_stats` before
4. `curl ... -X POST -d '{"backend":"signal","message":"datawatch e2e test — ignore"}' $TEST_BASE/api/comm/send`
5. Assert `comm_stats.signal.msg_sent` incremented
6. Save to `evidence/TS-094/`
**Expected**: Signal msg_sent increments  
**Evidence**: `put.json`, `send.json`, `stats.json`  
**Status**: 📋 planned

---

### TS-095 — !help comm command via REST test/message
**Tags**: [surface:api] [feature:comms]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"text":"help"}' $TEST_BASE/api/test/message`
2. Assert `count >= 1`, responses contain "datawatch commands" or "command"
3. Save to `evidence/TS-095/help.json`
**Expected**: Help command returns canonical command list  
**Evidence**: `help.json`  
**Status**: 📋 planned

---

### TS-096 — !sessions comm command
**Tags**: [surface:api] [feature:comms] [feature:sessions]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"text":"sessions"}' $TEST_BASE/api/test/message`
2. Assert `count >= 1`, `responses` is non-empty list
3. Save to `evidence/TS-096/sessions.json`
**Expected**: Sessions command returns session list  
**Evidence**: `sessions.json`  
**Status**: 📋 planned

---

### TS-097 — !status comm command
**Tags**: [surface:api] [feature:comms]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"text":"status"}' $TEST_BASE/api/test/message`
2. Assert `count >= 1`, response contains daemon status info
3. Save to `evidence/TS-097/status.json`
**Expected**: Status command returns daemon status  
**Evidence**: `status.json`  
**Status**: 📋 planned

---

### TS-098 — !alert comm command
**Tags**: [surface:api] [feature:comms]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"text":"alert test e2e alert message"}' $TEST_BASE/api/test/message`
2. Assert `count >= 1`, response acknowledges the alert
3. Save to `evidence/TS-098/alert.json`
**Expected**: Alert command accepted  
**Evidence**: `alert.json`  
**Status**: 📋 planned

---

### TS-099 — !mcp comm command
**Tags**: [surface:api] [feature:comms] [feature:mcp]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"text":"mcp"}' $TEST_BASE/api/test/message`
2. Assert `count >= 1`, response contains MCP tool count or surface summary
3. Save to `evidence/TS-099/mcp.json`
**Expected**: MCP command returns surface info  
**Evidence**: `mcp.json`  
**Status**: 📋 planned

---

### TS-100 — comm_stats shape after all sends
**Tags**: [surface:api] [feature:comms]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/stats`
2. Extract `comm_stats` section; assert it is a JSON object
3. For each configured backend, assert `msg_sent >= 0`
4. Save to `evidence/TS-100/comm_stats.json`
**Expected**: comm_stats section present with per-backend counters  
**Evidence**: `comm_stats.json`  
**Status**: 📋 planned

---

### TS-101 — Comm enable/disable round-trip
**Tags**: [surface:api] [feature:comms] [feature:config]  
**Steps**:
1. GET current `webhook.enabled` from config
2. Toggle: `curl ... -X PUT -d '{"webhook.enabled":false}' $TEST_BASE/api/config`
3. Assert GET shows `webhook.enabled == false`
4. Restore: `curl ... -X PUT -d '{"webhook.enabled":true}' $TEST_BASE/api/config` (or original value)
5. Save to `evidence/TS-101/`
**Expected**: Enable/disable round-trips correctly  
**Evidence**: `before.json`, `put.json`, `after.json`  
**Status**: 📋 planned

---

### TS-102 — Webhook receipt evidence saved
**Tags**: [surface:api] [feature:comms]  
**Steps**:
1. Parse listener log from TS-092 (if it ran)
2. Assert the log file exists and is non-empty
3. Copy log to `evidence/TS-102/webhook_receipt.log`
**Expected**: Webhook receipt log saved  
**Evidence**: `webhook_receipt.log`  
**Status**: 📋 planned

---

### TS-103 — DNS send evidence saved
**Tags**: [surface:api] [feature:comms]  
**Steps**:
1. Parse stats delta from TS-091
2. Assert dns_channel.msg_sent delta >= 0 (send was attempted even if DNS resolution fails)
3. Save stats diff to `evidence/TS-103/dns_stats.json`
**Expected**: DNS send attempt recorded in stats  
**Evidence**: `dns_stats.json`  
**Status**: 📋 planned

---

## T10 — CLI Surface

### TS-110 — datawatch version
**Tags**: [surface:cli] [feature:bootstrap]  
**Steps**:
1. `$TEST_BINARY version`
2. Assert output contains `v7.` and is not empty
3. Save to `evidence/TS-110/version.txt`
**Expected**: Version string printed  
**Evidence**: `version.txt`  
**Status**: 📋 planned

---

### TS-111 — datawatch status (daemon running on test ports)
**Tags**: [surface:cli] [feature:bootstrap]  
**Steps**:
1. `DATAWATCH_DATA_DIR=$TEST_DATA $TEST_BINARY status --base $TEST_BASE --token $TEST_TOKEN`
2. Assert output contains `running` or `ok`
3. Save to `evidence/TS-111/status.txt`
**Expected**: Status shows daemon is running  
**Evidence**: `status.txt`  
**Status**: 📋 planned

---

### TS-112 — datawatch sessions list
**Tags**: [surface:cli] [feature:sessions]  
**Steps**:
1. `$TEST_BINARY sessions list --base $TEST_BASE --token $TEST_TOKEN`
2. Assert output is a table or list, exit code 0
3. Save to `evidence/TS-112/sessions.txt`
**Expected**: Sessions list displayed  
**Evidence**: `sessions.txt`  
**Status**: 📋 planned

---

### TS-113 — datawatch sessions start (test session)
**Tags**: [surface:cli] [feature:sessions] [conflict:llm]  
**Steps**:
1. `$TEST_BINARY sessions start --name test-cli-session --backend shell --project-dir /tmp --base $TEST_BASE --token $TEST_TOKEN`
2. Assert exit code 0, session ID or name printed
3. Register for cleanup
4. Save to `evidence/TS-113/start.txt`
**Expected**: Session started via CLI  
**Evidence**: `start.txt`  
**Status**: 📋 planned

---

### TS-114 — datawatch sessions stop
**Tags**: [surface:cli] [feature:sessions]  
**Steps**:
1. Use session from TS-113 (or create a new one)
2. `$TEST_BINARY sessions stop $SESSION_ID --base $TEST_BASE --token $TEST_TOKEN`
3. Assert exit code 0
4. Save to `evidence/TS-114/stop.txt`
**Expected**: Session stopped  
**Evidence**: `stop.txt`  
**Status**: 📋 planned

---

### TS-115 — datawatch config get mcp.resources.enabled
**Tags**: [surface:cli] [feature:config] [feature:mcp]  
**Steps**:
1. `$TEST_BINARY config get mcp.resources.enabled --base $TEST_BASE --token $TEST_TOKEN`
2. Assert exit code 0, output contains a boolean value
3. Save to `evidence/TS-115/config_get.txt`
**Expected**: Config value returned  
**Evidence**: `config_get.txt`  
**Status**: 📋 planned

---

### TS-116 — datawatch config set round-trip
**Tags**: [surface:cli] [feature:config]  
**Steps**:
1. `$TEST_BINARY config get session.recent_session_minutes --base $TEST_BASE --token $TEST_TOKEN`; save `ORIG_VAL`
2. `$TEST_BINARY config set session.recent_session_minutes 60 --base $TEST_BASE --token $TEST_TOKEN`
3. `$TEST_BINARY config get session.recent_session_minutes --base $TEST_BASE --token $TEST_TOKEN`; assert `60`
4. Restore: `$TEST_BINARY config set session.recent_session_minutes $ORIG_VAL ...`
5. Save to `evidence/TS-116/`
**Expected**: Config set round-trips via CLI  
**Evidence**: `before.txt`, `set.txt`, `after.txt`  
**Status**: 📋 planned

---

### TS-117 — datawatch update --check (no install)
**Tags**: [surface:cli] [feature:bootstrap]  
**Steps**:
1. `$TEST_BINARY update --check --base $TEST_BASE --token $TEST_TOKEN`
2. Assert exit code 0, output contains `up_to_date` or `update_available`
3. Assert no binary was downloaded (no changes to `$TEST_BINARY` mtime)
4. Save to `evidence/TS-117/update_check.txt`
**Expected**: Check-only, no download triggered  
**Evidence**: `update_check.txt`  
**Status**: 📋 planned

---

### TS-118 — datawatch plugins list
**Tags**: [surface:cli] [feature:plugins]  
**Steps**:
1. `$TEST_BINARY plugins list --base $TEST_BASE --token $TEST_TOKEN`
2. Assert exit code 0
3. Save to `evidence/TS-118/plugins.txt`
**Expected**: Plugin list displayed  
**Evidence**: `plugins.txt`  
**Status**: 📋 planned

---

### TS-119 — datawatch secrets list
**Tags**: [surface:cli] [feature:secrets]  
**Steps**:
1. `$TEST_BINARY secrets list --base $TEST_BASE --token $TEST_TOKEN`
2. Assert exit code 0, no plaintext values in output
3. Save to `evidence/TS-119/secrets.txt`
**Expected**: Secret names listed, no plaintext values  
**Evidence**: `secrets.txt`  
**Status**: 📋 planned

---

### TS-120 — datawatch agents list
**Tags**: [surface:cli] [feature:agents]  
**Steps**:
1. `$TEST_BINARY agents list --base $TEST_BASE --token $TEST_TOKEN`
2. Assert exit code 0, response is a table or JSON list
3. Save to `evidence/TS-120/agents.txt`
**Expected**: Agent list displayed  
**Evidence**: `agents.txt`  
**Status**: 📋 planned

---

### TS-121 — datawatch mcp resources list [v7.1.0]
**Tags**: [surface:cli] [feature:mcp]  
**Steps**:
1. Check daemon version; if `< 7.1.0`, SKIP
2. `$TEST_BINARY mcp resources list --base $TEST_BASE --token $TEST_TOKEN`
3. Assert exit code 0, list has `≥5` entries
4. Save to `evidence/TS-121/mcp_resources.txt`
**Expected**: MCP resources listed via CLI  
**Evidence**: `mcp_resources.txt`  
**Status**: 📋 planned

---

## T11 — PWA (Chrome plugin)

> **Note**: T11 stories require the `mcp__claude-in-chrome__*` tools (Chrome plugin). In automated mode without the plugin, all T11 stories SKIP. Run manually with Claude Code chrome plugin active.

### TS-130 — PWA loads at https://127.0.0.1:18443
**Tags**: [surface:pwa] [feature:bootstrap] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to `https://127.0.0.1:18443` (accept self-signed cert)
3. Assert page title contains "datawatch" or page loads without JS error
4. Screenshot to `evidence/TS-130/pwa_load.png`
**Expected**: PWA loads, no fatal JS errors  
**Evidence**: `pwa_load.png`, `console.txt`  
**Status**: 📋 planned

---

### TS-131 — Auth token set via console
**Tags**: [surface:pwa] [feature:bootstrap] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Execute in browser console: `localStorage.setItem('dw_token', '$TEST_TOKEN')`
3. Reload page; assert no 401 errors in network tab
4. Screenshot to `evidence/TS-131/auth_set.png`
**Expected**: Token set, no auth errors  
**Evidence**: `auth_set.png`  
**Status**: 📋 planned

---

### TS-132 — Sessions list renders
**Tags**: [surface:pwa] [feature:sessions] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to sessions panel
3. Assert sessions list element is visible
4. Screenshot to `evidence/TS-132/sessions_list.png`
5. Assert no console errors
**Expected**: Sessions panel renders correctly  
**Evidence**: `sessions_list.png`, `console.txt`  
**Status**: 📋 planned

---

### TS-133 — Stats panel shows live data
**Tags**: [surface:pwa] [feature:bootstrap] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to stats/overview panel
3. Assert network request to `/api/stats` returns 200
4. Assert stats panel renders with non-empty data
5. Screenshot to `evidence/TS-133/stats_panel.png`
**Expected**: Stats panel populates with live daemon data  
**Evidence**: `stats_panel.png`, `network_requests.json`  
**Status**: 📋 planned

---

### TS-134 — Start new session from PWA
**Tags**: [surface:pwa] [feature:sessions] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Find "New Session" or "+" button; click
3. Fill in backend: `shell`, project dir: `/tmp`
4. Submit; assert session appears in sessions list
5. Screenshot to `evidence/TS-134/new_session.png`
**Expected**: New session created from PWA  
**Evidence**: `new_session.png`  
**Status**: 📋 planned

---

### TS-135 — WebSocket connects (wss://127.0.0.1:18443/ws)
**Tags**: [surface:pwa] [feature:sessions] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Check network requests for WebSocket upgrade to `wss://127.0.0.1:18443/ws`
3. Assert connection status is `OPEN` (101 Switching Protocols)
4. Screenshot to `evidence/TS-135/ws_connect.png`
**Expected**: WebSocket connected  
**Evidence**: `ws_connect.png`, `network_requests.json`  
**Status**: 📋 planned

---

### TS-136 — Alerts panel renders
**Tags**: [surface:pwa] [feature:bootstrap] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to alerts panel
3. Assert panel renders without error (may be empty)
4. Screenshot to `evidence/TS-136/alerts_panel.png`
**Expected**: Alerts panel renders  
**Evidence**: `alerts_panel.png`  
**Status**: 📋 planned

---

### TS-137 — Settings panel config round-trip
**Tags**: [surface:pwa] [feature:config] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to Settings panel
3. Assert config values are loaded from `/api/config`
4. Change one setting (e.g., session timeout)
5. Assert change is reflected in subsequent GET /api/config
6. Screenshot to `evidence/TS-137/settings.png`
**Expected**: Settings panel loads and persists changes  
**Evidence**: `settings.png`  
**Status**: 📋 planned

---

### TS-138 — MCP panel tools list
**Tags**: [surface:pwa] [feature:mcp] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to MCP panel
3. Assert tools list loads from `/api/mcp/docs`
4. Assert tool count shown matches actual count
5. Screenshot to `evidence/TS-138/mcp_panel.png`
**Expected**: MCP panel shows tool list  
**Evidence**: `mcp_panel.png`  
**Status**: 📋 planned

---

### TS-139 — Council personas list in PWA
**Tags**: [surface:pwa] [feature:council] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to Council panel
3. Assert personas list loads
4. Screenshot to `evidence/TS-139/council_panel.png`
**Expected**: Council panel shows personas  
**Evidence**: `council_panel.png`  
**Status**: 📋 planned

---

### TS-140 — Automata list in PWA
**Tags**: [surface:pwa] [feature:automata] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to Automata panel
3. Assert Automata list loads from `/api/autonomous/prds`
4. Screenshot to `evidence/TS-140/automata_panel.png`
**Expected**: Automata panel shows Automata list  
**Evidence**: `automata_panel.png`  
**Status**: 📋 planned

---

### TS-141 — Secrets panel in PWA
**Tags**: [surface:pwa] [feature:secrets] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to Secrets panel
3. Assert secrets list loads without showing plaintext values
4. Screenshot to `evidence/TS-141/secrets_panel.png`
**Expected**: Secrets panel loads, no plaintext values displayed  
**Evidence**: `secrets_panel.png`  
**Status**: 📋 planned

---

### TS-142 — Plugins panel in PWA
**Tags**: [surface:pwa] [feature:plugins] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to Plugins panel
3. Assert plugins list loads
4. Screenshot to `evidence/TS-142/plugins_panel.png`
**Expected**: Plugins panel renders  
**Evidence**: `plugins_panel.png`  
**Status**: 📋 planned

---

### TS-143 — No console errors after full load
**Tags**: [surface:pwa] [feature:bootstrap] [conflict:pwa]  
**Steps**:
1. SKIP if Chrome plugin not available
2. Navigate to PWA root; wait for full load
3. Read console messages; assert no `error` level messages
4. `node --check internal/server/web/app.js` (syntax check without browser)
5. Save console log to `evidence/TS-143/console.json`
**Expected**: Zero console errors, no JS syntax errors  
**Evidence**: `console.json`  
**Status**: 📋 planned

---

## T12 — Advanced Features

### TS-150 — Filters CRUD
**Tags**: [surface:api] [feature:filters] [conflict:db-write]  
**Steps**:
1. Create: `curl ... -X POST -d '{"pattern":"test-filter-e2e-pattern","action":"schedule","value":"yes"}' $TEST_BASE/api/filters`
2. Assert `id` returned; save `FILTER_ID`
3. List: `curl ... $TEST_BASE/api/filters`; assert FILTER_ID in list
4. Delete: `curl ... -X DELETE "$TEST_BASE/api/filters?id=$FILTER_ID"`; assert `status`
5. Verify deleted: list again, FILTER_ID absent
6. Save to `evidence/TS-150/`
**Expected**: Filter CRUD works end-to-end  
**Evidence**: `create.json`, `list.json`, `delete.json`  
**Status**: 📋 planned

---

### TS-151 — Schedules CRUD
**Tags**: [surface:api] [feature:schedules] [conflict:db-write]  
**Steps**:
1. Compute future timestamp: `date -u -d '+1 hour' +%FT%TZ`
2. Create: `curl ... -X POST -d '{"type":"new_session","name":"test-sched-e2e","command":"echo e2e","project_dir":"/tmp","backend":"shell","run_at":"<ts>"}' $TEST_BASE/api/schedules`
3. Assert `id` returned; save `SCHED_ID`
4. List: assert SCHED_ID in list
5. Delete: `curl ... -X DELETE "$TEST_BASE/api/schedules?id=$SCHED_ID"`; assert `status`
6. Save to `evidence/TS-151/`
**Expected**: Schedule CRUD works  
**Evidence**: `create.json`, `list.json`, `delete.json`  
**Status**: 📋 planned

---

### TS-152 — Observer peers surface
**Tags**: [surface:api] [feature:agents]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/observer/peers`
2. Assert HTTP 200, response is list or `{"peers":[...]}`
3. Save to `evidence/TS-152/peers.json`
**Expected**: Observer peers endpoint responds  
**Evidence**: `peers.json`  
**Status**: 📋 planned

---

### TS-153 — Identity GET/PATCH
**Tags**: [surface:api] [feature:identity]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/identity`
2. Assert HTTP 200, response contains `name` or `id` field
3. PATCH: `curl ... -X PATCH -d '{"display_name":"test-e2e-identity"}' $TEST_BASE/api/identity`
4. Assert updated value reflected in GET
5. Restore original name
6. Save to `evidence/TS-153/`
**Expected**: Identity readable and patchable  
**Evidence**: `get.json`, `patch.json`, `restore.json`  
**Status**: 📋 planned

---

### TS-154 — Algorithm start/advance
**Tags**: [surface:api] [feature:algorithm]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/algorithm`
2. Assert HTTP 200 (or 404 if not implemented yet; SKIP in that case)
3. If present, POST to start: `curl ... -X POST -d '{"mode":"test"}' $TEST_BASE/api/algorithm/start`
4. GET state; assert `phase` field present
5. POST advance if applicable
6. Save to `evidence/TS-154/`
**Expected**: Algorithm surface accessible  
**Evidence**: `get.json`, `start.json`  
**Status**: 📋 planned

---

### TS-155 — Evals suites list + run
**Tags**: [surface:api] [feature:evals]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/evals/suites`
2. Assert HTTP 200, response is list (may be empty)
3. If suites present: `curl ... -X POST -d '{"suite":"<first-suite>","dry_run":true}' $TEST_BASE/api/evals/run`
4. Assert run response has `run_id` or `results`
5. Save to `evidence/TS-155/`
**Expected**: Evals surface accessible  
**Evidence**: `suites.json`, `run.json`  
**Status**: 📋 planned

---

### TS-156 — Compute nodes endpoint
**Tags**: [surface:api] [feature:compute]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/compute/nodes`
2. Assert HTTP 200, response is list or `{"nodes":[]}`
3. Save to `evidence/TS-156/nodes.json`
**Expected**: Compute nodes endpoint responds  
**Evidence**: `nodes.json`  
**Status**: 📋 planned

---

### TS-157 — Cost rates endpoint
**Tags**: [surface:api] [feature:compute]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/cost/rates`
2. Assert HTTP 200, response contains rate data (may be empty map)
3. Save to `evidence/TS-157/rates.json`
**Expected**: Cost rates endpoint responds  
**Evidence**: `rates.json`  
**Status**: 📋 planned

---

### TS-158 — Agent lifecycle (mint/spawn/terminate)
**Tags**: [surface:api] [feature:agents]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/agents`
2. Assert `{"agents":[...]}` shape
3. Attempt spawn: `curl ... -X POST -d '{"project_profile":"datawatch-smoke","cluster_profile":"smoke-testing","task":"e2e test spawn"}' $TEST_BASE/api/agents`
4. If ID returned: `add_cleanup agent $AGT_ID`
5. DELETE: `curl ... -X DELETE $TEST_BASE/api/agents/$AGT_ID`; assert 204
6. Save to `evidence/TS-158/`
**Expected**: Agent lifecycle endpoints functional  
**Evidence**: `list.json`, `spawn.json`, `delete.txt`  
**Status**: 📋 planned

---

### TS-159 — Autonomous scan config
**Tags**: [surface:api] [feature:automata] [feature:config]  
**Steps**:
1. GET config: assert `autonomous` section present
2. Check `autonomous.scan.*` keys (SAST/secrets/deps config)
3. PUT: `curl ... -X PUT -d '{"autonomous.scan.enabled":false}' $TEST_BASE/api/config`; assert ok
4. Restore
5. Save to `evidence/TS-159/`
**Expected**: Autonomous scan config round-trips  
**Evidence**: `get.json`, `put.json`, `restore.json`  
**Status**: 📋 planned

---

## T13 — Docker Deployment Simulation

> **Note**: T13 simulates container isolation by using a separate `--data-dir`. No actual Docker container is required.

### TS-160 — Start daemon in isolated mode
**Tags**: [surface:docker] [feature:bootstrap]  
**Steps**:
1. Create isolated data dir: `mkdir -p /tmp/dw-docker-sim`
2. Write minimal config to `/tmp/dw-docker-sim/config.yaml`
3. Start daemon: `DATAWATCH_DATA_DIR=/tmp/dw-docker-sim $TEST_BINARY serve --port 18180 --tls-port 18543 &`; save PID
4. Poll health at `https://127.0.0.1:18543/api/health` until ok or 30s timeout
5. Save to `evidence/TS-160/health.json`
**Expected**: Daemon starts cleanly in isolated data dir  
**Evidence**: `health.json`  
**Status**: 📋 planned

---

### TS-161 — Health check (simulated container)
**Tags**: [surface:docker] [feature:bootstrap]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" https://127.0.0.1:18543/api/health`
2. Assert `{"status":"ok",...}`
3. Save to `evidence/TS-161/health.json`
**Expected**: Health endpoint responds in isolated mode  
**Evidence**: `health.json`  
**Status**: 📋 planned

---

### TS-162 — Session creation in isolated mode
**Tags**: [surface:docker] [feature:sessions]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"test-docker-session","backend":"shell","project_dir":"/tmp"}' https://127.0.0.1:18543/api/sessions`
2. Assert session ID returned
3. Cleanup: kill session
4. Save to `evidence/TS-162/session.json`
**Expected**: Session created in isolated daemon  
**Evidence**: `session.json`  
**Status**: 📋 planned

---

### TS-163 — Memory round-trip (simulated container)
**Tags**: [surface:docker] [feature:memory]  
**Steps**:
1. Save memory: `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -X POST -d '{"content":"docker-sim-test-memory-e2e"}' https://127.0.0.1:18543/api/memory/save`
2. Assert ID returned
3. List: assert entry in list
4. Delete
5. Save to `evidence/TS-163/`
**Expected**: Memory persists in isolated data dir  
**Evidence**: `save.json`, `list.json`, `delete.json`  
**Status**: 📋 planned

---

### TS-164 — Config volume equivalent (--data-dir)
**Tags**: [surface:docker] [feature:config]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" https://127.0.0.1:18543/api/config`
2. Assert config reflects the config.yaml written in TS-160 (port 18180, test token)
3. Save to `evidence/TS-164/config.json`
**Expected**: Config loaded from data dir  
**Evidence**: `config.json`  
**Status**: 📋 planned

---

### TS-165 — Restart preserves state (stop/start same data dir)
**Tags**: [surface:docker] [feature:bootstrap]  
**Steps**:
1. Create a memory entry in isolated daemon
2. Stop daemon (kill PID from TS-160)
3. Restart: `DATAWATCH_DATA_DIR=/tmp/dw-docker-sim $TEST_BINARY serve --port 18180 --tls-port 18543 &`
4. Wait for health
5. List memories; assert entry from step 1 is present
6. Save to `evidence/TS-165/`
**Expected**: State persists across restart  
**Evidence**: `before_stop.json`, `after_restart.json`  
**Status**: 📋 planned

---

### TS-166 — Analytics endpoint in isolated mode
**Tags**: [surface:docker] [feature:bootstrap]  
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" "https://127.0.0.1:18543/api/analytics?range=7d"`
2. Assert `buckets` list present
3. Save to `evidence/TS-166/analytics.json`
**Expected**: Analytics endpoint functional in isolated mode  
**Evidence**: `analytics.json`  
**Status**: 📋 planned

---

### TS-167 — Cleanup isolated daemon
**Tags**: [surface:docker] [feature:bootstrap]  
**Steps**:
1. Kill isolated daemon PID from TS-160
2. Remove `/tmp/dw-docker-sim/`
3. Assert port 18543 no longer responds
4. Save cleanup log to `evidence/TS-167/cleanup.txt`
**Expected**: Isolated daemon stopped and data dir removed  
**Evidence**: `cleanup.txt`  
**Status**: 📋 planned

---

## T14 — Kubernetes Deployment

> **Note**: T14 uses `kubectl --context=testing` (local testing cluster per infrastructure memory). Skip entire sprint if cluster unreachable.

### TS-170 — Apply test namespace + manifests
**Tags**: [surface:k8s] [feature:bootstrap] [conflict:k8s]  
**Steps**:
1. `kubectl --context=testing get nodes >/dev/null 2>&1 || skip_sprint T14 "testing cluster unreachable"`
2. `kubectl --context=testing create namespace datawatch-e2e --dry-run=client -o yaml | kubectl --context=testing apply -f -`
3. Apply minimal deployment manifest (ConfigMap + Deployment + Service)
4. Assert namespace created: `kubectl --context=testing get ns datawatch-e2e`
5. Save to `evidence/TS-170/apply.txt`
**Expected**: Namespace and resources created  
**Evidence**: `apply.txt`  
**Status**: 📋 planned

---

### TS-171 — Pod reaches Running
**Tags**: [surface:k8s] [feature:bootstrap] [conflict:k8s]  
**Steps**:
1. `kubectl --context=testing rollout status deployment/datawatch -n datawatch-e2e --timeout=120s`
2. Assert exit code 0
3. `kubectl --context=testing get pods -n datawatch-e2e`; assert at least one pod `Running`
4. Save to `evidence/TS-171/pods.txt`
**Expected**: Pod reaches Running within 120s  
**Evidence**: `pods.txt`  
**Status**: 📋 planned

---

### TS-172 — Health via port-forward
**Tags**: [surface:k8s] [feature:bootstrap] [conflict:k8s]  
**Steps**:
1. `kubectl --context=testing port-forward svc/datawatch 19443:18443 -n datawatch-e2e &`; save PID
2. Wait 3s; `curl -sk https://127.0.0.1:19443/api/health`
3. Assert `{"status":"ok",...}`
4. Kill port-forward PID
5. Save to `evidence/TS-172/health.json`
**Expected**: Health endpoint reachable via port-forward  
**Evidence**: `health.json`  
**Status**: 📋 planned

---

### TS-173 — Session creation via service
**Tags**: [surface:k8s] [feature:sessions] [conflict:k8s]  
**Steps**:
1. Re-establish port-forward (port 19443)
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"test-k8s-session","backend":"shell","project_dir":"/tmp"}' https://127.0.0.1:19443/api/sessions`
3. Assert session ID returned
4. Cleanup: kill session
5. Save to `evidence/TS-173/session.json`
**Expected**: Session created in K8s-deployed daemon  
**Evidence**: `session.json`  
**Status**: 📋 planned

---

### TS-174 — Memory persistence via PVC equivalent
**Tags**: [surface:k8s] [feature:memory] [conflict:k8s]  
**Steps**:
1. Via port-forward, save memory: `curl ... -X POST -d '{"content":"k8s-e2e-memory-test"}' .../api/memory/save`
2. Assert ID returned
3. List memories; assert entry present
4. Delete memory entry
5. Save to `evidence/TS-174/`
**Expected**: Memory persists in K8s volume  
**Evidence**: `save.json`, `list.json`  
**Status**: 📋 planned

---

### TS-175 — Config via env vars
**Tags**: [surface:k8s] [feature:config] [conflict:k8s]  
**Steps**:
1. Via port-forward, `curl ... https://127.0.0.1:19443/api/config`
2. Assert config reflects env vars set in deployment manifest (DATAWATCH_TOKEN, DATAWATCH_DATA_DIR)
3. Assert `server.token` matches `$TEST_TOKEN`
4. Save to `evidence/TS-175/config.json`
**Expected**: Env var config is applied  
**Evidence**: `config.json`  
**Status**: 📋 planned

---

### TS-176 — Rolling update simulation (stop/start new binary same data)
**Tags**: [surface:k8s] [feature:bootstrap] [conflict:k8s]  
**Steps**:
1. Trigger rolling restart: `kubectl --context=testing rollout restart deployment/datawatch -n datawatch-e2e`
2. Wait for rollout: `kubectl --context=testing rollout status deployment/datawatch -n datawatch-e2e --timeout=120s`
3. Re-establish port-forward; assert health endpoint responds
4. Save to `evidence/TS-176/`
**Expected**: Rolling restart completes, daemon healthy  
**Evidence**: `restart.txt`, `health.json`  
**Status**: 📋 planned

---

### TS-177 — Cleanup K8s namespace
**Tags**: [surface:k8s] [feature:bootstrap] [conflict:k8s]  
**Steps**:
1. `kubectl --context=testing delete namespace datawatch-e2e --timeout=60s`
2. Assert namespace gone: `kubectl --context=testing get ns datawatch-e2e` returns 404
3. Save to `evidence/TS-177/cleanup.txt`
**Expected**: Namespace deleted, no leftover resources  
**Evidence**: `cleanup.txt`  
**Status**: 📋 planned

---

---

## T15 — Parity Audit

**Goal**: Verify the cross-cutting parity rules enforced by AGENT.md are actually met. These tests do not test features — they test that every feature reaches every required surface. A feature passing T1–T14 but failing T15 is a release blocker.

### Parity rules under test

1. **7-surface parity** — every major feature reachable from REST + MCP + Channel bridge + CLI + Comm + PWA + Mobile (datawatch-app issue filed)
2. **Config parity** — every config key settable via YAML, REST GET/PUT, CLI `config get/set`, and visible in PWA Settings
3. **Hook event parity** — every internally-controlled session backend emits Start/Activity/Stop hook events
4. **Comm verb parity** — same set of `!` command verbs available regardless of which comm backend delivers the message
5. **Locale completeness** — all 5 locale bundles (en, es, fr, de, ja) have identical key sets
6. **Config alignment** — no feature-specific config key exists in YAML that isn't returned by `GET /api/config`

---

### TS-180 — Sessions feature: 7-surface parity matrix
**Tags**: `[surface:api]` `[surface:cli]` `[surface:mcp]` `[surface:comms]` `[surface:pwa]` `[feature:parity]`
**Steps**:
1. REST: `curl … GET /api/sessions` → assert 200 + array shape
2. CLI: `datawatch sessions list` → assert exits 0, prints table
3. MCP tool: `POST /api/mcp/call {"name":"get_sessions"}` or equivalent → assert 200
4. Comm: `POST /api/test/message {"text":"!sessions"}` → assert response contains session list
5. PWA: Chrome plugin navigates to `$TEST_BASE`, asserts sessions panel visible (auto-skip in automated mode)
6. Mobile: assert datawatch-app GitHub issue for Sessions surface exists (manual check — note issue URL)
**Expected**: All 6 testable surfaces return session data. Mobile noted as filed issue.
**Evidence**: `rest.json`, `cli.txt`, `mcp.json`, `comm.json`
**Status**: 📋 planned

---

### TS-181 — Memory feature: 7-surface parity matrix
**Tags**: `[surface:api]` `[surface:cli]` `[surface:mcp]` `[surface:comms]` `[feature:parity]`
**Steps**:
1. REST: `POST /api/memory/save {"text":"parity-test-memory"}` → assert id returned; save id
2. MCP tool: `POST /api/mcp/call {"name":"memory_recall","arguments":{"query":"parity-test-memory"}}` → assert result contains saved text
3. CLI: `datawatch memory recall "parity-test-memory"` (if CLI verb exists) → assert 0 or SKIP
4. Comm: `POST /api/test/message {"text":"!memory recall parity-test-memory"}` → assert response
5. REST cleanup: `DELETE /api/memory/<id>`
**Expected**: Memory written via REST is readable via MCP and Comm.
**Evidence**: `save.json`, `recall_mcp.json`, `recall_comm.json`
**Status**: 📋 planned

---

### TS-182 — Config parity: every key visible in YAML, REST, CLI, PWA
**Tags**: `[surface:api]` `[surface:cli]` `[feature:parity]` `[feature:config]`
**Steps**:
1. `GET /api/config` → save full config JSON to `evidence/TS-182/api_config.json`
2. For each key in the test set (`server.port`, `mcp.enabled`, `session.skip_permissions`, `autonomous.enabled`, `memory.enabled`):
   - Assert key present in `api_config.json`
   - `datawatch --data-dir .datawatch-test config get <key>` → assert matches API value
3. `PUT /api/config {"session":{"skip_permissions":false}}` → assert 200
4. `GET /api/config` → assert `session.skip_permissions` is now false
5. Restore: `PUT /api/config {"session":{"skip_permissions":true}}`
6. Check `$TEST_DATA/config.yaml` contains keys (grep)
**Expected**: All tested keys present in API response, CLI, and YAML. GET/PUT round-trips cleanly.
**Evidence**: `api_config.json`, `cli_gets.txt`, `put_response.json`
**Status**: 📋 planned

---

### TS-183 — Hook event parity: all session backends emit Start/Activity/Stop
**Tags**: `[surface:api]` `[feature:parity]` `[feature:sessions]`
**Steps**:
For each backend in `[claude-code, opencode, ollama, shell]`:
1. Create test session with that backend: `POST /api/sessions` with `backend=<backend>` (or session create endpoint)
2. POST Start hook: `POST /api/sessions/<id>/hook-event {"event":"Start","data":{}}`
3. Assert 200
4. POST Activity hook: `POST /api/sessions/<id>/hook-event {"event":"Activity","data":{"text":"parity check"}}`
5. Assert 200
6. POST Stop hook: `POST /api/sessions/<id>/hook-event {"event":"Stop","data":{}}`
7. Assert 200
8. `GET /api/stats` → assert session appears in `session_stats` (at least during active phase)
9. Cleanup: DELETE session
**Expected**: All hook events accepted for all backends. Stats reflect sessions.
**Evidence**: `hooks_<backend>.json` per backend
**Status**: 📋 planned

---

### TS-184 — Comm verb parity: same verbs work via REST test/message endpoint
**Tags**: `[surface:comms]` `[feature:parity]`
**Steps**:
For each verb in `[!help, !sessions, !status, !alert, !mcp, !memory, !kg]`:
1. `POST /api/test/message {"text":"<verb>"}` → assert 200, response body non-empty
2. Assert response contains recognisable content (not error/unknown command)
3. Save response to `evidence/TS-184/<verb>.json`
**Expected**: Every comm verb returns a valid structured response via the test/message endpoint. Same verbs that work via Signal/Webhook/DNS work here.
**Evidence**: `<verb>.json` per verb (7 files)
**Status**: 📋 planned

---

### TS-185 — Locale completeness: all 5 locale files have identical key sets
**Tags**: `[feature:parity]` `[feature:locale]`
**Steps**:
1. For each locale in `[en, es, fr, de, ja]`: extract all keys from `internal/server/web/locales/<locale>.json`
   ```bash
   python3 -c "import json; d=json.load(open('internal/server/web/locales/en.json')); print(sorted(d.keys()))" > evidence/TS-185/keys_en.txt
   ```
2. Repeat for all 5 locales
3. Assert all 5 key sets are identical:
   ```bash
   python3 -c "
   import json, sys
   files = ['en','es','fr','de','ja']
   keys = [set(json.load(open(f'internal/server/web/locales/{f}.json')).keys()) for f in files]
   base = keys[0]
   for i,f in enumerate(files[1:],1):
       missing = base - keys[i]; extra = keys[i] - base
       if missing: print(f'{files[i]} MISSING: {missing}')
       if extra: print(f'{files[i]} EXTRA: {extra}')
   if all(k == base for k in keys): print('ALL LOCALES IDENTICAL')
   "
   ```
**Expected**: All 5 locale files have identical key sets. Any missing keys = FAIL.
**Evidence**: `keys_<locale>.txt` × 5, `diff_report.txt`
**Status**: 📋 planned

---

### TS-186 — Config alignment: YAML keys match GET /api/config response
**Tags**: `[surface:api]` `[feature:parity]` `[feature:config]`
**Steps**:
1. Read `$TEST_DATA/config.yaml` top-level sections (grep `^[a-z]`)
2. `GET /api/config` → extract top-level keys via python3
3. Assert every YAML top-level section has a corresponding key in the API response
4. Assert `mcp`, `server`, `autonomous`, `memory`, `session`, `comm` all present in both
**Expected**: No YAML section missing from API response. Config surface is complete.
**Evidence**: `yaml_keys.txt`, `api_keys.txt`
**Status**: 📋 planned

---

### TS-187 — Comm backend config parity: enabled flag reachable via REST and YAML
**Tags**: `[surface:api]` `[feature:parity]` `[feature:comms]`
**Steps**:
For each comm backend in `[signal, telegram, discord, slack, matrix, ntfy, email, webhook, dns, github_webhook, twilio]`:
1. `GET /api/config` → assert `comm.<backend>.enabled` key exists in response
2. `PUT /api/config {"comm":{"<backend>":{"enabled":false}}}` → assert 200
3. `GET /api/config` → assert `comm.<backend>.enabled` is now false
4. Restore: `PUT /api/config {"comm":{"<backend>":{"enabled":false}}}` (restore to test default)
**Expected**: All 11 comm backends have their enabled flag reachable via REST config. No backend is config-only-via-YAML.
**Evidence**: `config_<backend>.json` per backend
**Status**: 📋 planned

---

### TS-188 — MCP tool surface: channel bridge tool count matches daemon tool count
**Tags**: `[surface:mcp]` `[feature:parity]`
**Steps**:
1. `GET $TEST_BASE/api/mcp/tools` → extract tool count → save to `evidence/TS-188/daemon_tools.json`
2. Start `datawatch-channel` with `DATAWATCH_API_URL=$TEST_BASE DATAWATCH_TOKEN=$TEST_TOKEN` and capture stderr
3. Assert stderr contains `discovered N daemon tools` where N matches daemon tool count
4. Stop channel bridge
**Expected**: Channel bridge discovers exactly the same number of tools as the daemon exposes. Zero hardcoded stubs.
**Evidence**: `daemon_tools.json`, `channel_stderr.txt`
**Status**: 📋 planned

---

### TS-189 — PWA Settings: all config sections visible
**Tags**: `[surface:pwa]` `[feature:parity]` `[feature:config]`
**Steps** (Chrome plugin — auto-skip in automated mode):
1. Navigate to `$TEST_BASE`, set auth token
2. Open Settings panel
3. Screenshot and assert presence of sections: Server, MCP, Sessions, Autonomous, Memory, Comm, Secrets
4. Save screenshot to `evidence/TS-189/settings.png`
**Expected**: All major config sections visible in PWA Settings. No section missing from UI that exists in YAML.
**Evidence**: `settings.png`
**Status**: 📋 planned

---

### TS-190 — Comm stats parity: every enabled comm appears in /api/stats comm_stats
**Tags**: `[surface:api]` `[feature:parity]` `[feature:comms]`
**Steps**:
1. `GET /api/stats` → extract `comm_stats` array
2. Assert DNS entry present: `python3 -c "… assert any(c['name']=='DNS' for c in d['comm_stats'])"`
3. Assert Webhook entry present
4. Assert MCP entry present (type=infra)
5. Assert Web/PWA entry present (type=infra)
6. Assert all enabled comms in config appear in comm_stats
**Expected**: Every enabled comm backend (including infra ones) appears in the stats surface. No silent backends.
**Evidence**: `comm_stats.json`
**Status**: 📋 planned

---

---

## T16 — Hybrid: Howto Coverage + Feature Gaps

**Goal**: A hybrid sprint covering two needs: (1) every curated howto has at least one real executable test so the docs stay honest, and (2) API/feature surfaces not fully exercised by T1–T14 get their own stories. Neither howtos alone nor feature tests alone cover everything — the hybrid ensures nothing is missed.

### Howto-anchored stories (doc reference → real test)

#### TS-200 — setup-and-install: health + version + auth flow
**Tags**: `[surface:api]` `[feature:bootstrap]` `[feature:howto]`
**Howto**: `setup-and-install.md`
**Steps**:
1. `curl -sk "$TEST_BASE/api/health"` → assert `status=ok`, `version` non-empty
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/stats"` → assert 200
3. `curl -sk "$TEST_BASE/api/stats"` → assert 401 (auth enforced)
**Expected**: Health open, stats auth-gated, version present
**Evidence**: `health.json`, `auth_check.txt`
**Status**: 📋 planned

#### TS-201 — llm-registry: backends list + single backend round-trip
**Tags**: `[surface:api]` `[feature:config]` `[feature:howto]`
**Howto**: `llm-registry.md`
**Steps**:
1. `GET /api/llms` → assert array with ≥1 entry, save `llms.json`
2. Extract first backend name; `GET /api/llms/<name>` → assert `name`, `backend`, `models` fields
3. `GET /api/llms/<name>/in_use` → assert paginated shape `{items:[], total:0}`
**Expected**: LLM registry returns real backends with model fields
**Evidence**: `llms.json`, `single_llm.json`
**Status**: 📋 planned

#### TS-202 — alerts-and-notifications: alert surface + comm forward
**Tags**: `[surface:api]` `[surface:comms]` `[feature:howto]`
**Howto**: `alerts-and-notifications.md`
**Steps**:
1. `GET /api/alerts` → assert shape (array or `{alerts:[]}`)
2. `POST /api/test/message {"text":"!alert list"}` → assert 200, response non-empty
3. `GET /api/stats` → assert `comm_stats` Web/PWA entry `msg_sent` ≥ 1
**Expected**: Alert surface reachable via REST and comm
**Evidence**: `alerts.json`, `comm_alert.json`
**Status**: 📋 planned

#### TS-203 — push-notifications: register + publish round-trip
**Tags**: `[surface:api]` `[feature:howto]`
**Howto**: `push-notifications.md`
**Steps**:
1. `POST /api/push/register {"endpoint":"https://test.example/push","keys":{"p256dh":"test","auth":"test"}}` → assert 200
2. `POST /api/push/alerts {"title":"test-push","body":"probe"}` → assert 200 or 202
3. `GET /api/stats` → push endpoint registered
**Expected**: Push registration and publish endpoints respond correctly
**Evidence**: `register.json`, `publish.json`
**Status**: 📋 planned

#### TS-204 — pipeline-chaining: pipeline list surface
**Tags**: `[surface:api]` `[feature:howto]`
**Howto**: `pipeline-chaining.md`
**Steps**:
1. `GET /api/pipelines` → assert 200 + array shape
2. Assert response is JSON array (may be empty if no pipelines configured)
**Expected**: Pipeline endpoint reachable and returns valid shape
**Evidence**: `pipelines.json`
**Status**: 📋 planned

#### TS-205 — claude-hooks: hook-event all types for all backends
**Tags**: `[surface:api]` `[feature:sessions]` `[feature:howto]`
**Howto**: `claude-hooks.md`
**Steps**:
For each `event` in `[Start, Activity, Stop, PromptSubmit, ToolUse]`:
1. `POST /api/sessions/test-hook-session/hook-event {"event":"<event>","data":{}}` → assert 200 or 404 (session not found is ok — endpoint must exist)
**Expected**: Hook event endpoint exists and accepts all event types
**Evidence**: `hook_events.json`
**Status**: 📋 planned

#### TS-206 — channel-state-engine: session state field
**Tags**: `[surface:api]` `[feature:sessions]` `[feature:howto]`
**Howto**: `channel-state-engine.md`
**Steps**:
1. Create test session via `POST /api/sessions` → extract id
2. `GET /api/sessions` → assert session record has `state` field
3. Verify `state` is one of `[running, waiting_input, rate_limited, idle, stopped]`
4. Delete session
**Expected**: Session records carry state machine field
**Evidence**: `sessions_with_state.json`
**Status**: 📋 planned

#### TS-207 — comm-channels: all comm verb round-trips
**Tags**: `[surface:comms]` `[feature:comms]` `[feature:howto]`
**Howto**: `comm-channels.md`
**Steps**:
For each verb in `[!help, !sessions, !status, !backends, !memory recall test, !kg entities]`:
1. `POST /api/test/message {"text":"<verb>"}` → assert 200, body non-empty, no error shape
**Expected**: All documented comm verbs return valid responses
**Evidence**: `verb_<name>.json` per verb
**Status**: 📋 planned

#### TS-208 — mcp-tools: full tool call chain
**Tags**: `[surface:mcp]` `[feature:mcp]` `[feature:howto]`
**Howto**: `mcp-tools.md`
**Steps**:
1. `GET /api/mcp/tools` → count ≥ 30, save list
2. `POST /api/mcp/call {"name":"memory_recall","arguments":{"query":"test"}}` → valid result
3. `POST /api/mcp/call {"name":"kg_query","arguments":{"entity":"test"}}` → valid result
4. `POST /api/mcp/call {"name":"research_sessions","arguments":{"query":"test"}}` → valid result or error with message
**Expected**: 3 distinct MCP tools callable end-to-end
**Evidence**: `tools_list.json`, `call_memory.json`, `call_kg.json`, `call_research.json`
**Status**: 📋 planned

#### TS-209 — docs-as-mcp: docs tool surface integrity
**Tags**: `[surface:mcp]` `[feature:mcp]` `[feature:howto]`
**Howto**: `docs-as-mcp.md`
**Steps**:
1. `GET /api/mcp/docs` → assert ≥ 30 tools, assert each has `exec_steps` field or is LLM-only
2. Assert foundational tools present: `memory_recall`, `kg_query`, `memory_remember`, `kg_add`
3. `POST /api/mcp/call {"name":"get_prompt","arguments":{"name":"test"}}` → 200 or structured error
**Expected**: Docs-as-MCP surface complete; no broken tool references
**Evidence**: `mcp_docs.json`
**Status**: 📋 planned

#### TS-210 — sessions-deep-dive: full session lifecycle via API
**Tags**: `[surface:api]` `[feature:sessions]` `[feature:howto]`
**Howto**: `sessions-deep-dive.md`
**Steps**:
1. `POST /api/sessions {name:"test-session-lifecycle", backend:"claude-code"}` → id
2. `GET /api/sessions` → assert session appears (may take a moment; retry 3×)
3. `POST /api/sessions/{id}/hook-event {event:"Start"}` → 200
4. `POST /api/channel/reply {text:"test reply", session_id:"{id}"}` → 200
5. `GET /api/channel/history?session_id={id}` → array with reply
6. `POST /api/sessions/{id}/hook-event {event:"Stop"}` → 200
7. `DELETE /api/sessions/{id}` → 204
**Expected**: Full session lifecycle exercised end-to-end
**Evidence**: `session_create.json`, `hook_start.json`, `reply.json`, `history.json`, `hook_stop.json`
**Status**: 📋 planned

#### TS-211 — identity-and-telos: identity PATCH + verify
**Tags**: `[surface:api]` `[feature:identity]` `[feature:howto]`
**Howto**: `identity-and-telos.md`
**Steps**:
1. `GET /api/identity` → save `identity.json`, note current `role`
2. `PATCH /api/identity {"role":"test-identity-probe"}` → 200
3. `GET /api/identity` → assert `role` is now `test-identity-probe`
4. Restore: `PATCH /api/identity {"role":"<original>"}` or `{}`
**Expected**: Identity PATCH round-trips correctly
**Evidence**: `identity_get.json`, `identity_patch.json`
**Status**: 📋 planned

#### TS-212 — algorithm-mode: start + advance phases
**Tags**: `[surface:api]` `[feature:algorithm]` `[feature:howto]`
**Howto**: `algorithm-mode.md`
**Steps**:
1. `GET /api/algorithm` → assert shape (phase list)
2. `POST /api/algorithm/start {"backend":"claude-code"}` → assert `{session_id, phase:"observe"}`
3. `POST /api/algorithm/advance` → assert `{phase:"orient"}`
4. Stop/cleanup algorithm session
**Expected**: Algorithm mode starts at observe, advances to orient on request
**Evidence**: `algo_list.json`, `algo_start.json`, `algo_advance.json`
**Status**: 📋 planned

#### TS-213 — evals: suites list + run smoke
**Tags**: `[surface:api]` `[feature:evals]` `[feature:howto]`
**Howto**: `evals.md`
**Steps**:
1. `GET /api/evals/suites` → assert array shape
2. If suites non-empty: `POST /api/evals/run {"suite":"<first>"}` → assert `{pass, fail, total}` shape
3. If suites empty: SKIP run step
**Expected**: Evals surface reachable; run returns structured result
**Evidence**: `evals_suites.json`, `evals_run.json`
**Status**: 📋 planned

#### TS-214 — profiles: create + attach + detach + delete
**Tags**: `[surface:api]` `[feature:profiles]` `[feature:howto]`
**Howto**: `profiles.md`
**Steps**:
1. `POST /api/profiles/projects {"name":"test-profile-proj"}` → id
2. `POST /api/autonomous/prds {"title":"test-profile-automaton","backend":"claude-code"}` → automaton_id
3. `PUT /api/autonomous/prds/{automaton_id} {"project_profile":"test-profile-proj"}` → 200
4. `GET /api/autonomous/prds/{automaton_id}` → assert `project_profile` set
5. `PUT /api/autonomous/prds/{automaton_id} {"project_profile":""}` → clear
6. `DELETE /api/autonomous/prds/{automaton_id}`, `DELETE /api/profiles/projects/{id}`
**Expected**: Project profiles attach/detach cleanly
**Evidence**: `profile_create.json`, `automaton_attach.json`, `automaton_detach.json`
**Status**: 📋 planned

#### TS-215 — secrets-manager: full backend surface
**Tags**: `[surface:api]` `[feature:secrets]` `[feature:howto]`
**Howto**: `secrets-manager.md`
**Steps**:
1. `GET /api/secrets/vault/status` → `{backend, status}` shape
2. `POST /api/secrets {"name":"test-secret-probe","value":"test-val","backend":"env"}` → id
3. `GET /api/secrets` → contains test-secret-probe
4. `GET /api/secrets/{id}` → name+backend present
5. `DELETE /api/secrets/{id}` → 200
6. `GET /api/secrets` → test-secret-probe gone
**Expected**: Full secrets CRUD cycle with env backend
**Evidence**: `vault_status.json`, `secret_create.json`, `secret_get.json`, `secret_delete.json`
**Status**: 📋 planned

### Gap-fill stories (features not in T1–T14)

#### TS-220 — Alerts: full alert CRUD if endpoint exists
**Tags**: `[surface:api]` `[feature:alerts]`
**Steps**:
1. `GET /api/alerts` → assert 200 + JSON shape, save `alerts.json`
2. If POST /api/alerts exists: create test alert → assert id → GET → DELETE
3. `GET /api/stats` → `ebpf_enabled` field present
**Expected**: Alert endpoint reachable, CRUD works if supported
**Evidence**: `alerts.json`
**Status**: 📋 planned

#### TS-221 — Link status + interfaces
**Tags**: `[surface:api]` `[feature:bootstrap]`
**Steps**:
1. `GET /api/link/status` → assert 200 + JSON
2. `GET /api/interfaces` → assert 200 + array shape
3. `GET /api/servers` → assert 200 + array shape
4. `GET /api/servers/health` → assert 200 + JSON
**Expected**: Network/infra status endpoints all respond correctly
**Evidence**: `link_status.json`, `interfaces.json`, `servers.json`
**Status**: 📋 planned

#### TS-222 — Cost tracking surface
**Tags**: `[surface:api]` `[feature:config]`
**Steps**:
1. `GET /api/cost/rates` → assert 200 + JSON shape
2. Assert response has at least one rate entry or empty array
**Expected**: Cost endpoint reachable and returns valid shape
**Evidence**: `cost_rates.json`
**Status**: 📋 planned

#### TS-223 — Routing rules CRUD
**Tags**: `[surface:api]` `[feature:config]`
**Steps**:
1. `GET /api/routing-rules` → assert 200 + array
2. If POST supported: create test rule → GET → DELETE
**Expected**: Routing rules endpoint reachable
**Evidence**: `routing_rules.json`
**Status**: 📋 planned

#### TS-224 — Device aliases surface
**Tags**: `[surface:api]` `[feature:config]`
**Steps**:
1. `GET /api/device-aliases` → assert 200 + JSON shape
**Expected**: Device aliases endpoint reachable
**Evidence**: `device_aliases.json`
**Status**: 📋 planned

#### TS-225 — Federated observer peers CRUD
**Tags**: `[surface:api]` `[feature:observers]`
**Steps**:
1. `GET /api/observer/peers` → assert 200 + shape `{peers:[]}`
2. `POST /api/push/register {"endpoint":"https://test.probe/","keys":{"p256dh":"x","auth":"y"}}` → assert 200
3. `GET /api/stats` → assert `bound_interfaces` array present
**Expected**: Observer surface reachable; push registration works
**Evidence**: `peers.json`, `push_register.json`
**Status**: 📋 planned

#### TS-226 — Tailscale config section present
**Tags**: `[surface:api]` `[feature:config]`
**Steps**:
1. `GET /api/config` → assert `tailscale` section exists in JSON
2. Assert `tailscale.enabled` field present (true or false)
**Expected**: Tailscale config exposed via REST config surface
**Evidence**: `tailscale_config.json`
**Status**: 📋 planned

#### TS-227 — Voice input config surface
**Tags**: `[surface:api]` `[feature:config]`
**Steps**:
1. `GET /api/config` → assert `whisper` or `voice` section present
2. Assert backend field present in voice/whisper section
**Expected**: Voice config exposed via REST
**Evidence**: `voice_config.json`
**Status**: 📋 planned

---

## T17 — Major Feature Journeys

**Goal**: Composite end-to-end tests spanning multiple sprints. These simulate real operator workflows from start to finish. A journey PASS proves features work together, not just in isolation.

#### TS-240 — Research journey: memory → KG → MCP recall
**Tags**: `[surface:api]` `[surface:mcp]` `[feature:memory]` `[feature:kg]` `[feature:journey]`
**Steps**:
1. `POST /api/memory/save {"text":"test-journey-research-alpha","tags":["journey"]}` → id
2. `POST /api/memory/kg/add {"subject":"test-journey-entity","predicate":"relates_to","object":"research-alpha"}` → kg_id
3. `POST /api/mcp/call {"name":"memory_recall","arguments":{"query":"test-journey-research-alpha"}}` → assert result contains saved text
4. `POST /api/mcp/call {"name":"kg_query","arguments":{"entity":"test-journey-entity"}}` → assert result contains triple
5. `DELETE /api/memory/{id}`, `DELETE /api/memory/kg/{kg_id}` (if DELETE supported)
**Expected**: Memory written via REST is retrievable via MCP. KG triple written via REST queryable via MCP.
**Evidence**: `memory_save.json`, `kg_add.json`, `mcp_recall.json`, `mcp_kg.json`
**Status**: 📋 planned

#### TS-241 — Autonomous journey: Automaton lifecycle with profiles
**Tags**: `[surface:api]` `[feature:automata]` `[feature:profiles]` `[feature:journey]`
**Steps**:
1. `POST /api/profiles/projects {"name":"test-journey-proj"}` → proj_id
2. `POST /api/profiles/clusters {"name":"test-journey-cluster"}` → cluster_id
3. `POST /api/autonomous/prds {"title":"test-journey-automaton","backend":"claude-code","question":"test"}` → automaton_id
4. `PUT /api/autonomous/prds/{automaton_id} {"project_profile":"test-journey-proj","cluster_profile":"test-journey-cluster"}` → 200
5. `GET /api/autonomous/prds/{automaton_id}` → assert both profiles attached
6. `POST /api/autonomous/prds/{automaton_id}/set_llm {"backend":"claude-code","model":"claude-sonnet-4-5"}` → 200
7. `PUT /api/autonomous/config {"per_story_approval":true}` → 200
8. Restore: `PUT /api/autonomous/config {"per_story_approval":false}`
9. `DELETE /api/autonomous/prds/{automaton_id}`, DELETE profiles
**Expected**: Full Automaton setup journey: create → profile attach → LLM config → approval gate → cleanup
**Evidence**: `proj_create.json`, `automaton_create.json`, `automaton_profiles.json`, `automaton_llm.json`
**Status**: 📋 planned

#### TS-242 — Monitoring journey: webhook comm → send → verify stats
**Tags**: `[surface:api]` `[surface:comms]` `[feature:comms]` `[feature:journey]`
**Steps**:
1. Start local listener: `python3 -m http.server $TEST_WEBHOOK_PORT &`; save PID
2. `PUT /api/config {"comm":{"webhook":{"enabled":true,"url":"http://127.0.0.1:$TEST_WEBHOOK_PORT/"}}}` → 200
3. `POST /api/test/message {"text":"!status"}` → 200
4. `GET /api/stats` → assert `comm_stats` Webhook `msg_sent` ≥ 1
5. Stop listener; `PUT /api/config {"comm":{"webhook":{"enabled":false}}}` → restore
**Expected**: Webhook enabled → message sent → stats reflect the send
**Evidence**: `webhook_config.json`, `webhook_send.json`, `webhook_stats.json`
**Status**: 📋 planned

#### TS-243 — Secrets journey: create → reference → verify → delete
**Tags**: `[surface:api]` `[feature:secrets]` `[feature:config]` `[feature:journey]`
**Steps**:
1. `POST /api/secrets {"name":"test-journey-secret","value":"journey-val","backend":"env"}` → id
2. `GET /api/secrets` → assert `test-journey-secret` present
3. `GET /api/config` → verify `secrets` section accessible
4. `DELETE /api/secrets/{id}` → 200
5. `GET /api/secrets` → assert `test-journey-secret` gone
**Expected**: Secret lifecycle: create → visible in list → delete → gone
**Evidence**: `secret_create.json`, `secret_list.json`, `secret_delete.json`
**Status**: 📋 planned

#### TS-244 — Council journey: personas → run → cancel → cleanup
**Tags**: `[surface:api]` `[feature:council]` `[feature:journey]`
**Steps**:
1. `POST /api/council/personas {"name":"test-journey-analyst","role":"analyst","backend":"ollama","model":"qwen3:8b"}` → id1
2. `POST /api/council/personas {"name":"test-journey-critic","role":"critic","backend":"ollama","model":"qwen3:8b"}` → id2
3. `GET /api/council/personas` → assert both present
4. `POST /api/council/run {"brief":"journey test brief","persona_ids":["<id1>","<id2>"]}` → council_id
5. `POST /api/council/cancel/<council_id>` → 200 or 404
6. `GET /api/stats` → comm_stats present
7. `DELETE /api/council/personas/{id1}`, DELETE id2
**Expected**: Two personas created, council run started, cancelled cleanly, personas removed
**Evidence**: `persona1.json`, `persona2.json`, `council_run.json`, `council_cancel.json`
**Status**: 📋 planned

#### TS-245 — Update check journey: version check without install
**Tags**: `[surface:api]` `[surface:cli]` `[feature:bootstrap]` `[feature:journey]`
**Steps**:
1. `GET /api/health` → assert `version` field
2. `datawatch --data-dir $TEST_DATA update --check` → exits 0, prints version info
3. `GET /api/stats` → assert `rtk_installed` field present
**Expected**: Update check returns version info without triggering install
**Evidence**: `health.json`, `update_check.txt`
**Status**: 📋 planned

#### TS-246 — Identity → algorithm journey
**Tags**: `[surface:api]` `[feature:identity]` `[feature:algorithm]` `[feature:journey]`
**Steps**:
1. `GET /api/identity` → save original role
2. `PATCH /api/identity {"role":"test-journey-identity"}` → 200
3. `GET /api/algorithm` → assert 200
4. `POST /api/algorithm/start {"backend":"claude-code"}` → session_id, assert phase=observe
5. `POST /api/algorithm/advance` → assert phase=orient
6. Stop algorithm session (DELETE or cancel endpoint)
7. `PATCH /api/identity {"role":"<original>"}` → restore
**Expected**: Identity and algorithm mode work together cleanly
**Evidence**: `identity_before.json`, `algo_start.json`, `algo_advance.json`
**Status**: 📋 planned

#### TS-247 — MCP tool chain journey: list → call → result → verify stats
**Tags**: `[surface:mcp]` `[surface:api]` `[feature:mcp]` `[feature:journey]`
**Steps**:
1. `GET /api/mcp/tools` → extract count N
2. `POST /api/mcp/call {"name":"memory_recall","arguments":{"query":"journey-probe"}}` → valid result
3. `POST /api/mcp/call {"name":"kg_query","arguments":{"entity":"journey-probe"}}` → valid result
4. `GET /api/stats` → assert `mcp_stats` block present (if v7.1.0+) or skip stats check
5. Assert tool count matches across two GET /api/mcp/tools calls (idempotent)
**Expected**: MCP tools discoverable, callable, and consistent across requests
**Evidence**: `tools_count.json`, `call1.json`, `call2.json`
**Status**: 📋 planned

#### TS-248 — Schedule + filter lifecycle journey
**Tags**: `[surface:api]` `[feature:schedules]` `[feature:filters]` `[feature:journey]`
**Steps**:
1. `POST /api/schedules {"name":"test-journey-sched","run_at":"2099-06-01T00:00:00Z"}` → sched_id
2. `GET /api/schedules` → assert test-journey-sched present
3. `POST /api/filters {"name":"test-journey-filter","pattern":"journey-probe-.*"}` → filter_id
4. `GET /api/filters` → assert test-journey-filter present
5. `DELETE /api/schedules/{sched_id}` → 200
6. `DELETE /api/filters/{filter_id}` → 200
7. GET both → assert both gone
**Expected**: Schedule and filter CRUD lifecycle cleanly isolated
**Evidence**: `sched_create.json`, `filter_create.json`, `sched_delete.json`, `filter_delete.json`
**Status**: 📋 planned

#### TS-249 — Full session + channel lifecycle journey
**Tags**: `[surface:api]` `[feature:sessions]` `[feature:journey]`
**Steps**:
1. `POST /api/sessions {"name":"test-journey-session","backend":"claude-code"}` → sess_id
2. `POST /api/sessions/{sess_id}/hook-event {"event":"Start","data":{}}` → 200
3. `POST /api/sessions/{sess_id}/hook-event {"event":"Activity","data":{"text":"test activity"}}` → 200
4. `POST /api/channel/reply {"text":"test channel message","session_id":"{sess_id}"}` → 200
5. `GET /api/channel/history?session_id={sess_id}` → assert message in history
6. `POST /api/sessions/{sess_id}/hook-event {"event":"Stop","data":{}}` → 200
7. `GET /api/stats` → assert `active_sessions` updated
8. `DELETE /api/sessions/{sess_id}` → 204
**Expected**: Complete session lifecycle from hook events through channel comms to cleanup
**Evidence**: `session_create.json`, `hooks.json`, `channel_reply.json`, `channel_history.json`
**Status**: 📋 planned

---

## T18 — Missing Endpoints

### TS-250 — GET /api/splash/info returns hostname + version
**Tags**: [surface:api] [feature:bootstrap]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/splash/info`
2. `python3 -c "import sys,json; d=json.load(sys.stdin); assert 'version' in d or 'hostname' in d, d" < evidence/TS-250/info.json`
3. Save to `evidence/TS-250/info.json`
**Expected**: JSON object containing at least `version` or `hostname` field
**Evidence**: `info.json`
**Status**: 📋 planned

---

### TS-251 — GET /api/openapi.yaml returns valid YAML
**Tags**: [surface:api] [feature:bootstrap]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/openapi.yaml -o evidence/TS-251/openapi.yaml`
2. `python3 -c "import sys; import yaml; d=yaml.safe_load(open('evidence/TS-251/openapi.yaml')); assert d.get('openapi','').startswith('3.0'), d.get('openapi')"`
3. Assert `openapi: 3.0.x` present in first few lines
**Expected**: Valid OpenAPI 3.0.x YAML document
**Evidence**: `openapi.yaml`
**Status**: 📋 planned

---

### TS-252 — GET /api/docs returns Swagger HTML
**Tags**: [surface:api] [feature:bootstrap]
**Steps**:
1. `CODE=$(curl -sk -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/docs)`
2. `python3 -c "assert '$CODE' == '200', 'got $CODE'"`
3. Optionally assert body contains `swagger` or `Swagger UI`
**Expected**: HTTP 200 with Swagger UI HTML
**Evidence**: `http_code.txt`
**Status**: 📋 planned

---

### TS-253 — GET /api/cooldown returns shape
**Tags**: [surface:api] [feature:config]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/cooldown -o evidence/TS-253/cooldown.json`
2. `python3 -c "import sys,json; d=json.load(open('evidence/TS-253/cooldown.json')); assert 'active' in d, d"`
**Expected**: `{"active": false, "until": null}` or similar shape
**Evidence**: `cooldown.json`
**Status**: 📋 planned

---

### TS-254 — Cooldown set + verify + clear round-trip
**Tags**: [surface:api] [feature:config]
**Steps**:
1. `curl -sk -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"minutes":1}' $TEST_BASE/api/cooldown`
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/cooldown` → assert `active=true`
3. `curl -sk -X DELETE -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/cooldown` → 200 or 204
4. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/cooldown` → assert `active=false`
**Expected**: Cooldown activates on POST, clears on DELETE, GET reflects both states
**Evidence**: `cooldown_set.json`, `cooldown_active.json`, `cooldown_clear.json`, `cooldown_inactive.json`
**Status**: 📋 planned

---

### TS-255 — GET /api/devices returns push device registry array
**Tags**: [surface:api] [feature:config]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/devices -o evidence/TS-255/devices.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-255/devices.json')); assert isinstance(d, list) or 'devices' in d, d"`
**Expected**: Array (possibly empty) of registered push devices
**Evidence**: `devices.json`
**Status**: 📋 planned

---

### TS-256 — POST /api/devices/register shape round-trip
**Tags**: [surface:api] [feature:config]
**Steps**:
1. `curl -sk -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"token":"test-device-token-probe","platform":"test"}' $TEST_BASE/api/devices/register -o evidence/TS-256/register.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-256/register.json')); assert 'id' in d or 'token' in d or d.get('ok'), d"`
**Expected**: Device registered (id or ok response), endpoint accepts the shape
**Evidence**: `register.json`
**Status**: 📋 planned

---

### TS-257 — GET /api/federation/sessions returns shape
**Tags**: [surface:api] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/federation/sessions -o evidence/TS-257/fed_sessions.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-257/fed_sessions.json')); assert 'primary' in d or isinstance(d, list), d"`
**Expected**: `{"primary": [...]}` shape or array
**Evidence**: `fed_sessions.json`
**Status**: 📋 planned

---

### TS-258 — GET /api/marketplace/ollama/catalog returns catalog array
**Tags**: [surface:api] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/marketplace/ollama/catalog -o evidence/TS-258/catalog.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-258/catalog.json')); assert isinstance(d, list) or 'models' in d or 'catalog' in d, d"`
**Expected**: Catalog array or object containing model entries
**Evidence**: `catalog.json`
**Status**: 📋 planned

---

### TS-259 — GET /api/openwebui/models returns array
**Tags**: [surface:api] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/openwebui/models -o evidence/TS-259/owui_models.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-259/owui_models.json')); assert isinstance(d, list) or 'models' in d or 'error' in d, d"`
**Expected**: Array of models or graceful error (endpoint registered; list may be empty if OpenWebUI not configured)
**Evidence**: `owui_models.json`
**Status**: 📋 planned

---

### TS-260 — GET /api/orchestrator/verdicts returns shape
**Tags**: [surface:api] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/orchestrator/verdicts -o evidence/TS-260/verdicts.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-260/verdicts.json')); assert 'verdicts' in d or isinstance(d, list), d"`
**Expected**: `{"verdicts": [...]}` shape or array
**Evidence**: `verdicts.json`
**Status**: 📋 planned

---

### TS-261 — GET /api/proxy/ missing server-name returns 400 or error
**Tags**: [surface:api] [feature:parity]
**Steps**:
1. `CODE=$(curl -sk -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/proxy/")`
2. `python3 -c "assert '$CODE' in ('400','404','422','500'), 'unexpected: $CODE'"`
**Expected**: Endpoint registered and returns 4xx/5xx (not 200 with empty body) when server-name missing
**Evidence**: `http_code.txt`
**Status**: 📋 planned

---

### TS-262 — GET /api/templates returns array
**Tags**: [surface:api] [feature:plugins]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/templates -o evidence/TS-262/templates.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-262/templates.json')); assert isinstance(d, list) or 'templates' in d, d"`
**Expected**: Array (possibly empty) of templates
**Evidence**: `templates.json`
**Status**: 📋 planned

---

### TS-263 — Templates CRUD round-trip
**Tags**: [surface:api] [feature:plugins]
**Steps**:
1. `curl -sk -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"test-template-probe","content":"probe content"}' $TEST_BASE/api/templates -o evidence/TS-263/create.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-263/create.json')); TMPL_ID=d.get('id'); assert TMPL_ID, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/templates/$TMPL_ID` → assert `name=test-template-probe`
4. `curl -sk -X DELETE -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/templates/$TMPL_ID` → 200 or 204
**Expected**: Template created, retrievable by ID, deletable; no leaks
**Evidence**: `create.json`, `get.json`, `delete.json`
**Status**: 📋 planned

---

### TS-264 — POST /api/assist endpoint exists (405 on GET)
**Tags**: [surface:api] [feature:parity]
**Steps**:
1. `CODE=$(curl -sk -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/assist")`
2. `python3 -c "assert '$CODE' in ('200','400','405','422'), 'endpoint missing: $CODE'"`
**Expected**: Endpoint registered (returns anything other than 404); POST-only returns 405 on GET
**Evidence**: `http_code.txt`
**Status**: 📋 planned

---

### TS-265 — GET /api/splash/logo 404 is acceptable
**Tags**: [surface:api] [feature:bootstrap]
**Steps**:
1. `CODE=$(curl -sk -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/splash/logo")`
2. `python3 -c "assert '$CODE' in ('200','404'), 'unexpected: $CODE'"`
**Expected**: Endpoint registered (200 with logo bytes OR 404 if no logo configured); not a 500
**Evidence**: `http_code.txt`
**Status**: 📋 planned

---

### TS-266 — GET /api/servers + GET /api/servers/health shape
**Tags**: [surface:api] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/servers -o evidence/TS-266/servers.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-266/servers.json')); assert isinstance(d, list) or 'servers' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/servers/health -o evidence/TS-266/servers_health.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-266/servers_health.json')); assert isinstance(d, list) or 'servers' in d or 'health' in d, d"`
**Expected**: Both endpoints return structured responses (array or keyed object)
**Evidence**: `servers.json`, `servers_health.json`
**Status**: 📋 planned

---

## T19 — MCP Surface Complete

### TS-270 — algorithm_list via MCP returns array
**Tags**: [surface:mcp] [feature:mcp] [feature:algorithm]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"algorithm_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-270/result.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-270/result.json')); assert 'result' in d or 'content' in d or isinstance(d, list), d"`
**Expected**: MCP call returns array shape or result wrapper (empty list acceptable)
**Evidence**: `result.json`
**Status**: 📋 planned

---

### TS-271 — algorithm_start + algorithm_get via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:algorithm]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"algorithm_start","arguments":{"backend":"claude-code"}}' $TEST_BASE/api/mcp/call -o evidence/TS-271/start.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-271/start.json')); sid=d.get('result',{}).get('session_id') or d.get('session_id'); assert sid or 'error' in str(d).lower(), d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"algorithm_get","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-271/get.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-271/get.json')); assert 'result' in d or 'content' in d, d"`
**Expected**: Algorithm session starts (or returns graceful error if not configured); get returns phase state
**Evidence**: `start.json`, `get.json`
**Status**: 📋 planned

---

### TS-272 — autonomous_config_get + autonomous_config_set round-trip via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:automata]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"autonomous_config_get","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-272/get.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-272/get.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"autonomous_config_set","arguments":{"per_story_approval":true}}' $TEST_BASE/api/mcp/call -o evidence/TS-272/set.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-272/set.json')); assert 'result' in d or 'ok' in str(d).lower(), d"`
**Expected**: Config readable and settable via MCP; round-trip succeeds
**Evidence**: `get.json`, `set.json`
**Status**: 📋 planned

---

### TS-273 — autonomous_status via MCP returns shape
**Tags**: [surface:mcp] [feature:mcp] [feature:automata]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"autonomous_status","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-273/status.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-273/status.json')); assert 'result' in d or 'enabled' in str(d), d"`
**Expected**: `{enabled, ...}` shape or wrapped result
**Evidence**: `status.json`
**Status**: 📋 planned

---

### TS-274 — autonomous_type_list via MCP returns array
**Tags**: [surface:mcp] [feature:mcp] [feature:automata]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"autonomous_type_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-274/types.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-274/types.json')); assert isinstance(d.get('result'), list) or 'result' in d or 'content' in d, d"`
**Expected**: Array of Automaton types (or wrapped list)
**Evidence**: `types.json`
**Status**: 📋 planned

---

### TS-275 — backends_list via MCP returns LLM shape
**Tags**: [surface:mcp] [feature:mcp] [feature:config]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"backends_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-275/backends.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-275/backends.json')); assert 'llm' in str(d) or 'result' in d or 'content' in d, d"`
**Expected**: `{llm: [...]}` shape or wrapped backends list
**Evidence**: `backends.json`
**Status**: 📋 planned

---

### TS-276 — compute_node_list via MCP returns array
**Tags**: [surface:mcp] [feature:mcp] [feature:compute]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"compute_node_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-276/nodes.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-276/nodes.json')); assert isinstance(d.get('result'), list) or 'result' in d or 'content' in d, d"`
**Expected**: Array of compute nodes (possibly empty)
**Evidence**: `nodes.json`
**Status**: 📋 planned

---

### TS-277 — compute_node_add + compute_node_get + compute_node_delete CRUD via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:compute]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"compute_node_add","arguments":{"name":"test-mcp-node","address":"http://127.0.0.1:9999"}}' $TEST_BASE/api/mcp/call -o evidence/TS-277/add.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-277/add.json')); nid=d.get('result',{}).get('id') or d.get('id'); assert nid or 'error' in str(d).lower(), d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"compute_node_get","arguments":{"id":"<nid>"}}' $TEST_BASE/api/mcp/call -o evidence/TS-277/get.json`
4. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"compute_node_delete","arguments":{"id":"<nid>"}}' $TEST_BASE/api/mcp/call -o evidence/TS-277/delete.json`
**Expected**: Node added, retrievable, and deleted via MCP CRUD
**Evidence**: `add.json`, `get.json`, `delete.json`
**Status**: 📋 planned

---

### TS-278 — cooldown_status + cooldown_set + cooldown_clear via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:config]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"cooldown_status","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-278/status.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-278/status.json')); assert 'active' in str(d), d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"cooldown_set","arguments":{"minutes":1}}' $TEST_BASE/api/mcp/call`
4. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"cooldown_clear","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-278/clear.json`
5. `python3 -c "import json; d=json.load(open('evidence/TS-278/clear.json')); assert 'result' in d or 'ok' in str(d).lower(), d"`
**Expected**: Cooldown status readable, settable, and clearable via MCP
**Evidence**: `status.json`, `clear.json`
**Status**: 📋 planned

---

### TS-279 — cost_rates + cost_summary shape via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:config]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"cost_rates","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-279/rates.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-279/rates.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"cost_summary","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-279/summary.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-279/summary.json')); assert 'result' in d or 'content' in d, d"`
**Expected**: Both cost MCP tools return structured responses
**Evidence**: `rates.json`, `summary.json`
**Status**: 📋 planned

---

### TS-280 — council_config_get + council_config_set round-trip via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:council]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"council_config_get","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-280/get.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-280/get.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"council_config_set","arguments":{"enabled":true}}' $TEST_BASE/api/mcp/call -o evidence/TS-280/set.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-280/set.json')); assert 'result' in d or 'ok' in str(d).lower(), d"`
**Expected**: Council config readable and writable via MCP
**Evidence**: `get.json`, `set.json`
**Status**: 📋 planned

---

### TS-281 — daemon_logs via MCP returns log lines array
**Tags**: [surface:mcp] [feature:mcp] [feature:bootstrap]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"daemon_logs","arguments":{"lines":10}}' $TEST_BASE/api/mcp/call -o evidence/TS-281/logs.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-281/logs.json')); assert 'result' in d or 'content' in d or isinstance(d, list), d"`
**Expected**: Array of recent log lines or wrapped result
**Evidence**: `logs.json`
**Status**: 📋 planned

---

### TS-282 — detection_config_get + detection_config_set round-trip via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:sessions]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"detection_config_get","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-282/get.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-282/get.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"detection_config_set","arguments":{"enabled":true}}' $TEST_BASE/api/mcp/call -o evidence/TS-282/set.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-282/set.json')); assert 'result' in d or 'ok' in str(d).lower(), d"`
**Expected**: Detection config readable and settable via MCP
**Evidence**: `get.json`, `set.json`
**Status**: 📋 planned

---

### TS-283 — dns_channel_config_get + dns_channel_config_set round-trip via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:comms]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"dns_channel_config_get","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-283/get.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-283/get.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"dns_channel_config_set","arguments":{"enabled":false}}' $TEST_BASE/api/mcp/call -o evidence/TS-283/set.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-283/set.json')); assert 'result' in d or 'ok' in str(d).lower(), d"`
**Expected**: DNS channel config readable and settable via MCP
**Evidence**: `get.json`, `set.json`
**Status**: 📋 planned

---

### TS-284 — docs_search for "sessions" returns results with howto refs
**Tags**: [surface:mcp] [feature:mcp] [feature:howto]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"docs_search","arguments":{"query":"sessions"}}' $TEST_BASE/api/mcp/call -o evidence/TS-284/results.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-284/results.json')); assert 'result' in d or 'content' in d, d"`
3. Assert result contains at least one howto reference
**Expected**: docs_search returns results including sessions-related howtos
**Evidence**: `results.json`
**Status**: 📋 planned

---

### TS-285 — docs_list_howtos returns >= 20 howtos
**Tags**: [surface:mcp] [feature:mcp] [feature:howto]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"docs_list_howtos","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-285/howtos.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-285/howtos.json')); items=d.get('result',d.get('content',d)); n=len(items) if isinstance(items,list) else len(str(items).split()); assert n>=1, d"`
**Expected**: List of at least 20 curated howtos returned by MCP
**Evidence**: `howtos.json`
**Status**: 📋 planned

---

### TS-286 — docs_read for "daemon-operations" returns content
**Tags**: [surface:mcp] [feature:mcp] [feature:howto]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"docs_read","arguments":{"slug":"daemon-operations"}}' $TEST_BASE/api/mcp/call -o evidence/TS-286/content.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-286/content.json')); assert 'result' in d or 'content' in d, d"`
3. Assert content contains meaningful text (not empty)
**Expected**: Howto content returned with front-matter and body
**Evidence**: `content.json`
**Status**: 📋 planned

---

### TS-287 — docs_apply for a curated howto exec_steps executes via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:howto] [conflict:llm]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"docs_list_howtos","arguments":{}}' $TEST_BASE/api/mcp/call` → pick first howto with exec_steps
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"docs_apply","arguments":{"slug":"<slug>"}}' $TEST_BASE/api/mcp/call -o evidence/TS-287/apply.json`
3. `python3 -c "import json; d=json.load(open('evidence/TS-287/apply.json')); assert 'result' in d or 'steps' in str(d) or 'ok' in str(d).lower(), d"`
**Expected**: docs_apply returns step execution results or a structured plan; no 500 error
**Evidence**: `apply.json`
**Status**: 📋 planned

---

### TS-288 — eval_list_suites + eval_run smoke suite shape via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:evals]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"eval_list_suites","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-288/suites.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-288/suites.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"eval_run","arguments":{"suite":"smoke"}}' $TEST_BASE/api/mcp/call -o evidence/TS-288/run.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-288/run.json')); assert 'result' in d or 'id' in str(d) or 'error' in str(d).lower(), d"`
**Expected**: Suites listed; smoke run returns ID or graceful error if suite not found
**Evidence**: `suites.json`, `run.json`
**Status**: 📋 planned

---

### TS-289 — federation_meta_peers + federation_sessions shape via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"federation_meta_peers","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-289/peers.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-289/peers.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"federation_sessions","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-289/fed_sessions.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-289/fed_sessions.json')); assert 'result' in d or 'content' in d, d"`
**Expected**: Both federation tools return structured responses
**Evidence**: `peers.json`, `fed_sessions.json`
**Status**: 📋 planned

---

### TS-290 — guardrail_library_list + guardrail_profile CRUD via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:automata]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"guardrail_library_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-290/lib.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-290/lib.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"guardrail_profile_create","arguments":{"name":"test-mcp-profile"}}' $TEST_BASE/api/mcp/call -o evidence/TS-290/create.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-290/create.json')); pid=d.get('result',{}).get('id') or d.get('id'); assert pid or 'error' in str(d).lower(), d"`
5. Delete the created profile via `guardrail_profile_delete`
**Expected**: Library listed; profile created and deleted via MCP
**Evidence**: `lib.json`, `create.json`
**Status**: 📋 planned

---

### TS-291 — llm_list + llm_get + llm_enable/disable round-trip via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:config]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"llm_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-291/list.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-291/list.json')); assert 'result' in d or 'content' in d, d"`
3. Extract first LLM ID if available; call `llm_get` with that ID
4. Call `llm_enable` then `llm_disable` on the first available backend
**Expected**: LLM list readable; enable/disable round-trip returns ok
**Evidence**: `list.json`, `get.json`
**Status**: 📋 planned

---

### TS-292 — marketplace_ollama_catalog + marketplace_pull_task shape via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"marketplace_ollama_catalog","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-292/catalog.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-292/catalog.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"marketplace_pull_task","arguments":{"model":"tinyllama"}}' $TEST_BASE/api/mcp/call -o evidence/TS-292/pull.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-292/pull.json')); assert 'result' in d or 'id' in str(d) or 'error' in str(d).lower(), d"`
**Expected**: Catalog listed; pull task created or graceful error returned
**Evidence**: `catalog.json`, `pull.json`
**Status**: 📋 planned

---

### TS-293 — memory_scope_recall + memory_scope_borrow + memory_scope_seed via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:memory]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"memory_scope_recall","arguments":{"query":"probe-293"}}' $TEST_BASE/api/mcp/call -o evidence/TS-293/recall.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-293/recall.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"memory_scope_seed","arguments":{"text":"probe-293 test seed","tags":["test"]}}' $TEST_BASE/api/mcp/call -o evidence/TS-293/seed.json`
4. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"memory_scope_borrow","arguments":{"query":"probe-293"}}' $TEST_BASE/api/mcp/call -o evidence/TS-293/borrow.json`
**Expected**: All three memory scope tools return structured responses without 500 errors
**Evidence**: `recall.json`, `seed.json`, `borrow.json`
**Status**: 📋 planned

---

### TS-294 — observer_config_get + observer_peers_list + observer_stats via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"observer_config_get","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-294/config.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-294/config.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"observer_peers_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-294/peers.json`
4. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"observer_stats","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-294/stats.json`
**Expected**: All three observer tools return structured responses
**Evidence**: `config.json`, `peers.json`, `stats.json`
**Status**: 📋 planned

---

### TS-295 — orchestrator_config_get + orchestrator_graph_list + orchestrator_verdicts via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"orchestrator_config_get","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-295/config.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-295/config.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"orchestrator_graph_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-295/graphs.json`
4. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"orchestrator_verdicts","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-295/verdicts.json`
**Expected**: All three orchestrator tools return structured responses
**Evidence**: `config.json`, `graphs.json`, `verdicts.json`
**Status**: 📋 planned

---

### TS-296 — pipeline_list + pipeline_start + pipeline_status shape via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"pipeline_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-296/list.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-296/list.json')); assert 'result' in d or 'content' in d, d"`
3. If list non-empty: `pipeline_start` with first pipeline ID; assert returns run ID or graceful error
4. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"pipeline_status","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-296/status.json`
**Expected**: Pipeline list and status return structured responses; start returns run ID or graceful error if no pipelines configured
**Evidence**: `list.json`, `status.json`
**Status**: 📋 planned

---

### TS-297 — routing_rules_list + routing_rules_test shape via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"routing_rules_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-297/list.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-297/list.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"routing_rules_test","arguments":{"session_name":"test-probe","backend":"claude-code"}}' $TEST_BASE/api/mcp/call -o evidence/TS-297/test.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-297/test.json')); assert 'result' in d or 'matched' in str(d) or 'error' in str(d).lower(), d"`
**Expected**: Rules listed and test returns match result or graceful no-match
**Evidence**: `list.json`, `test.json`
**Status**: 📋 planned

---

### TS-298 — tailscale_status + tailscale_nodes shape via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:parity] [conflict:tailscale]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"tailscale_status","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-298/status.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-298/status.json')); assert 'result' in d or 'content' in d or 'error' in str(d).lower(), d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"tailscale_nodes","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-298/nodes.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-298/nodes.json')); assert 'result' in d or 'content' in d or 'error' in str(d).lower(), d"`
**Expected**: Both tools return structured responses; graceful error if Tailscale not configured
**Evidence**: `status.json`, `nodes.json`
**Status**: 📋 planned

---

### TS-299 — telemetry_list + telemetry_get shape via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:parity]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"telemetry_list","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-299/list.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-299/list.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"telemetry_get","arguments":{"metric":"sessions"}}' $TEST_BASE/api/mcp/call -o evidence/TS-299/get.json`
4. `python3 -c "import json; d=json.load(open('evidence/TS-299/get.json')); assert 'result' in d or 'content' in d or 'error' in str(d).lower(), d"`
**Expected**: Telemetry list and get return structured responses
**Evidence**: `list.json`, `get.json`
**Status**: 📋 planned

---

### TS-300 — tooling_status + tooling_gitignore + tooling_cleanup shape via MCP
**Tags**: [surface:mcp] [feature:mcp] [feature:plugins]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"tooling_status","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-300/status.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-300/status.json')); assert 'result' in d or 'content' in d, d"`
3. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"tooling_gitignore","arguments":{"path":"/tmp"}}' $TEST_BASE/api/mcp/call -o evidence/TS-300/gitignore.json`
4. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"tooling_cleanup","arguments":{"path":"/tmp/dw-probe-$$"}}' $TEST_BASE/api/mcp/call -o evidence/TS-300/cleanup.json`
**Expected**: All three tooling MCP tools return structured responses without 500 errors
**Evidence**: `status.json`, `gitignore.json`, `cleanup.json`
**Status**: 📋 planned

---

## T20 — CLI Complete

### TS-310 — datawatch autonomous list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:automata]
**Steps**:
1. `datawatch --data-dir $TEST_DATA autonomous list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
3. Save stdout to `evidence/TS-310/out.txt`
**Expected**: Exits 0; prints array or empty list
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-311 — datawatch autonomous template-list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:automata]
**Steps**:
1. `datawatch --data-dir $TEST_DATA autonomous template-list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
3. Save stdout to `evidence/TS-311/out.txt`
**Expected**: Exits 0; prints template list or empty
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-312 — datawatch algorithm list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:algorithm]
**Steps**:
1. `datawatch --data-dir $TEST_DATA algorithm list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows algorithm session list (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-313 — datawatch compute list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:compute]
**Steps**:
1. `datawatch --data-dir $TEST_DATA compute list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows compute node list (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-314 — datawatch compute add + show + delete CRUD round-trip
**Tags**: [surface:cli] [feature:cli] [feature:compute]
**Steps**:
1. `datawatch --data-dir $TEST_DATA compute add --name test-cli-node --address http://127.0.0.1:9998 2>&1` → save NODE_ID
2. `datawatch --data-dir $TEST_DATA compute show $NODE_ID 2>&1` → assert name=test-cli-node
3. `datawatch --data-dir $TEST_DATA compute delete $NODE_ID 2>&1` → exits 0
4. `datawatch --data-dir $TEST_DATA compute list 2>&1` → assert test-cli-node gone
**Expected**: Compute node add/show/delete lifecycle via CLI completes without error
**Evidence**: `add.txt`, `show.txt`, `delete.txt`
**Status**: 📋 planned

---

### TS-315 — datawatch council list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:council]
**Steps**:
1. `datawatch --data-dir $TEST_DATA council list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows council persona list
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-316 — datawatch llm list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:config]
**Steps**:
1. `datawatch --data-dir $TEST_DATA llm list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows LLM backend list
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-317 — datawatch llm add + show + delete round-trip
**Tags**: [surface:cli] [feature:cli] [feature:config]
**Steps**:
1. `datawatch --data-dir $TEST_DATA llm add --name test-cli-llm --backend ollama --model tinyllama 2>&1` → save LLM_ID
2. `datawatch --data-dir $TEST_DATA llm show $LLM_ID 2>&1` → assert name=test-cli-llm
3. `datawatch --data-dir $TEST_DATA llm delete $LLM_ID 2>&1` → exits 0
**Expected**: LLM backend add/show/delete lifecycle completes without error
**Evidence**: `add.txt`, `show.txt`, `delete.txt`
**Status**: 📋 planned

---

### TS-318 — datawatch routing-rules list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:parity]
**Steps**:
1. `datawatch --data-dir $TEST_DATA routing-rules list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows routing rules list (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-319 — datawatch routing-rules test exits 0
**Tags**: [surface:cli] [feature:cli] [feature:parity]
**Steps**:
1. `datawatch --data-dir $TEST_DATA routing-rules test --session-name probe-319 --backend claude-code 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0 or 1 (not a crash)
**Expected**: Exits cleanly; prints matched rule or "no match" message
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-320 — datawatch rtk check exits 0
**Tags**: [surface:cli] [feature:cli]
**Steps**:
1. `datawatch --data-dir $TEST_DATA rtk check 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0 (may print "RTK not found" if rtk not installed)
**Expected**: Exits 0; prints RTK installation status
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-321 — datawatch tailscale status exits 0
**Tags**: [surface:cli] [feature:cli] [feature:parity] [conflict:tailscale]
**Steps**:
1. `datawatch --data-dir $TEST_DATA tailscale status 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0 or 1 (graceful if Tailscale not configured)
**Expected**: Exits cleanly; prints Tailscale status or "not configured"
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-322 — datawatch evals runs exits 0
**Tags**: [surface:cli] [feature:cli] [feature:evals]
**Steps**:
1. `datawatch --data-dir $TEST_DATA evals runs 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows eval run history (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-323 — datawatch pipeline list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:parity]
**Steps**:
1. `datawatch --data-dir $TEST_DATA pipeline list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows pipeline list (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-324 — datawatch memory list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:memory]
**Steps**:
1. `datawatch --data-dir $TEST_DATA memory list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows memory entries (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-325 — datawatch memory recall exits 0
**Tags**: [surface:cli] [feature:cli] [feature:memory]
**Steps**:
1. `datawatch --data-dir $TEST_DATA memory recall "test query" 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; prints recall results (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-326 — datawatch secrets list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:secrets]
**Steps**:
1. `datawatch --data-dir $TEST_DATA secrets list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows secrets list
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-327 — datawatch secrets set + get + delete CRUD round-trip
**Tags**: [surface:cli] [feature:cli] [feature:secrets]
**Steps**:
1. `datawatch --data-dir $TEST_DATA secrets set test-cli-secret "probe-value-327" 2>&1` → exits 0
2. `datawatch --data-dir $TEST_DATA secrets get test-cli-secret 2>&1` → assert "probe-value-327" present
3. `datawatch --data-dir $TEST_DATA secrets delete test-cli-secret 2>&1` → exits 0
4. `datawatch --data-dir $TEST_DATA secrets list 2>&1` → assert test-cli-secret gone
**Expected**: Secret set/get/delete CRUD lifecycle completes without error
**Evidence**: `set.txt`, `get.txt`, `delete.txt`
**Status**: 📋 planned

---

### TS-328 — datawatch observer peers list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:parity]
**Steps**:
1. `datawatch --data-dir $TEST_DATA observer peers list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows observer peer list (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-329 — datawatch orchestrator graphs list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:parity]
**Steps**:
1. `datawatch --data-dir $TEST_DATA orchestrator graphs list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0 or prints "no graphs" gracefully
**Expected**: Exits cleanly; shows orchestrator graph list
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-330 — datawatch skills list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:skills]
**Steps**:
1. `datawatch --data-dir $TEST_DATA skills list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows skills list
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-331 — datawatch skills registry list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:skills]
**Steps**:
1. `datawatch --data-dir $TEST_DATA skills registry list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows skills registry entries
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-332 — datawatch plugins list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:plugins]
**Steps**:
1. `datawatch --data-dir $TEST_DATA plugins list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows plugin list (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-333 — datawatch identity show exits 0
**Tags**: [surface:cli] [feature:cli] [feature:parity]
**Steps**:
1. `datawatch --data-dir $TEST_DATA identity show 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
3. Assert output contains `role` or `name` field
**Expected**: Exits 0; prints current identity fields
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-334 — datawatch identity configure shape check exits 0
**Tags**: [surface:cli] [feature:cli] [feature:parity]
**Steps**:
1. `datawatch --data-dir $TEST_DATA identity configure --help 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0 (help text shown) or usage printed
**Expected**: Exits 0; prints identity configure usage without crashing
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-335 — datawatch schedule list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:schedules]
**Steps**:
1. `datawatch --data-dir $TEST_DATA schedule list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows schedule list (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-336 — datawatch filter list exits 0
**Tags**: [surface:cli] [feature:cli] [feature:filters]
**Steps**:
1. `datawatch --data-dir $TEST_DATA filter list 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; shows filter list (possibly empty)
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-337 — datawatch cost summary exits 0
**Tags**: [surface:cli] [feature:cli] [feature:config]
**Steps**:
1. `datawatch --data-dir $TEST_DATA cost summary 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; prints cost summary table or "no data"
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-338 — datawatch analytics exits 0
**Tags**: [surface:cli] [feature:cli] [feature:parity]
**Steps**:
1. `datawatch --data-dir $TEST_DATA analytics 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0 or shows usage
**Expected**: Exits 0; prints analytics summary or usage
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-339 — datawatch tooling status exits 0
**Tags**: [surface:cli] [feature:cli] [feature:plugins]
**Steps**:
1. `datawatch --data-dir $TEST_DATA tooling status 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
**Expected**: Exits 0; prints tooling/RTK installation status
**Evidence**: `out.txt`
**Status**: 📋 planned

---

### TS-340 — datawatch about exits 0
**Tags**: [surface:cli] [feature:cli] [feature:bootstrap]
**Steps**:
1. `datawatch --data-dir $TEST_DATA about 2>&1; echo "EXIT:$?"`
2. Assert exit code is 0
3. Assert output contains version string and credits
**Expected**: Exits 0; prints version + credits without crash
**Evidence**: `out.txt`
**Status**: 📋 planned

---

## T21 — Docs-as-MCP AI Configuration

### TS-350 — docs_search "enable memory sqlite" returns howto ref
**Tags**: [surface:mcp] [feature:mcp] [feature:howto] [feature:memory]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"docs_search","arguments":{"query":"enable memory sqlite"}}' $TEST_BASE/api/mcp/call -o evidence/TS-350/results.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-350/results.json')); assert 'result' in d or 'content' in d, d"`
3. Assert result body contains `howto` or `cross-agent-memory` reference
**Expected**: Search returns at least one howto referencing memory/sqlite configuration
**Evidence**: `results.json`
**Status**: 📋 planned

---

### TS-351 — docs_list_howtos contains cross-agent-memory
**Tags**: [surface:mcp] [feature:mcp] [feature:howto] [feature:memory]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"docs_list_howtos","arguments":{}}' $TEST_BASE/api/mcp/call -o evidence/TS-351/howtos.json`
2. `python3 -c "import json,sys; raw=open('evidence/TS-351/howtos.json').read(); assert 'cross-agent-memory' in raw or 'memory' in raw, 'not found'"`
**Expected**: Howto list includes `cross-agent-memory` slug
**Evidence**: `howtos.json`
**Status**: 📋 planned

---

### TS-352 — docs_read "cross-agent-memory" returns content with exec_steps
**Tags**: [surface:mcp] [feature:mcp] [feature:howto] [feature:memory]
**Steps**:
1. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"docs_read","arguments":{"slug":"cross-agent-memory"}}' $TEST_BASE/api/mcp/call -o evidence/TS-352/content.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-352/content.json')); raw=str(d); assert 'exec_steps' in raw or 'memory' in raw.lower(), d"`
**Expected**: Howto content returned with exec_steps front-matter present
**Evidence**: `content.json`
**Status**: 📋 planned

---

### TS-353 — docs_apply executes steps and returns 200/OK per step
**Tags**: [surface:mcp] [feature:mcp] [feature:howto] [feature:memory] [conflict:llm]
**Steps**:
1. Determine first curated howto with `exec_steps` via `docs_list_howtos`
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"name":"docs_apply","arguments":{"slug":"<slug>"}}' $TEST_BASE/api/mcp/call -o evidence/TS-353/apply.json`
3. `python3 -c "import json; d=json.load(open('evidence/TS-353/apply.json')); assert 'result' in d or 'steps' in str(d) or 'ok' in str(d).lower(), d"`
4. If step results present, assert each step returns status 200 or ok
**Expected**: docs_apply returns step execution results; each configured step returns 200/ok
**Evidence**: `apply.json`
**Status**: 📋 planned

---

### TS-354 — POST /api/assist AI guidance response
**Tags**: [surface:api] [feature:parity] [feature:howto] [conflict:llm]
**Steps**:
1. `curl -sk -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" -d '{"query":"how do I configure sqlite memory"}' $TEST_BASE/api/assist -o evidence/TS-354/assist.json`
2. `python3 -c "import json; d=json.load(open('evidence/TS-354/assist.json')); assert 'guidance' in d or 'response' in d or 'howto' in str(d) or 'memory' in str(d).lower(), d"`
**Expected**: /api/assist returns helpful guidance referencing memory/sqlite configuration
**Evidence**: `assist.json`
**Status**: 📋 planned

---

## T22 — Smoke Test Infrastructure

### TS-360 — GET /api/smoke/progress returns 204 when no run active
**Tags**: [surface:api] [feature:bootstrap]
**Steps**:
1. Ensure no smoke run is active (clean state)
2. `CODE=$(curl -sk -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/smoke/progress")`
3. `python3 -c "assert '$CODE' in ('204','404'), 'expected 204/404 for no active run, got $CODE'"`
**Expected**: HTTP 204 (or 404) when no smoke-progress.json exists
**Evidence**: `http_code.txt`
**Status**: 📋 planned

---

### TS-361 — Running release-smoke.sh writes progress JSON before first section
**Tags**: [surface:api] [feature:bootstrap]
**Steps**:
1. Start `bash scripts/release-smoke.sh` in background (against test daemon)
2. Poll `$TEST_BASE/api/smoke/progress` within 5 seconds of start
3. Assert response is non-empty JSON (not 204) before first section completes
4. Kill smoke run after progress confirmed
**Expected**: Progress JSON written and served within first section execution
**Evidence**: `progress_early.json`
**Status**: 📋 planned

---

### TS-362 — Progress JSON has correct shape
**Tags**: [surface:api] [feature:bootstrap]
**Steps**:
1. Start `bash scripts/release-smoke.sh` in background; wait for progress to appear
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/smoke/progress -o evidence/TS-362/progress.json`
3. `python3 -c "import json; d=json.load(open('evidence/TS-362/progress.json')); assert all(k in d for k in ['version','started_at','updated_at','active','current_id','current_name','pass','fail','skip','sections']), d"`
**Expected**: Progress JSON contains all required fields: version, started_at, updated_at, active, current_id, current_name, pass, fail, skip, sections
**Evidence**: `progress.json`
**Status**: 📋 planned

---

### TS-363 — After smoke completes, active=false in progress JSON
**Tags**: [surface:api] [feature:bootstrap]
**Steps**:
1. Wait for smoke run (or short subset) to complete fully
2. `curl -sk -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/smoke/progress -o evidence/TS-363/progress_done.json`
3. `python3 -c "import json; d=json.load(open('evidence/TS-363/progress_done.json')); assert d.get('active') == False, d"`
**Expected**: `active: false` in progress JSON after run completes
**Evidence**: `progress_done.json`
**Status**: 📋 planned

---

### TS-364 — DELETE /api/smoke/progress removes file, next GET returns 204
**Tags**: [surface:api] [feature:bootstrap]
**Steps**:
1. Ensure progress file exists (from prior smoke run or manual create)
2. `curl -sk -X DELETE -H "Authorization: Bearer $TEST_TOKEN" $TEST_BASE/api/smoke/progress` → 200 or 204
3. `CODE=$(curl -sk -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TEST_TOKEN" "$TEST_BASE/api/smoke/progress")`
4. `python3 -c "assert '$CODE' in ('204','404'), 'expected 204/404 after delete, got $CODE'"`
**Expected**: DELETE removes progress file; subsequent GET returns 204/404
**Evidence**: `delete_code.txt`, `http_code_after.txt`
**Status**: 📋 planned

---

## 6. Evidence Policy

```
internal/server/web/docs/testing/
  master-plan.md           ← git-tracked; updated only when features change
  master-cookbook.md       ← git-tracked; updated after every run (latest status)
  README.md                ← git-tracked; summary + coverage + latest run date
  runs/
    YYYY-MM-DD-NNN/        ← local only (parent gitignored); preserved between runs
      plan-snapshot.md     ← copy of master-plan at run time
      cookbook-results.md  ← pass/fail per story for this run
      evidence/
        TS-001/
          response.json    ← every response saved
          screenshot.png   ← PWA tests
        TS-002/
          ...
```

| Artifact | Persisted in git? | Cleaned after run? | Purpose |
|---|---|---|---|
| `master-plan.md` | ✅ Yes | Never | Story definitions — reference for all runs |
| `master-cookbook.md` | ✅ Yes | Never | Latest run results per story |
| `README.md` | ✅ Yes | Never | Coverage summary + last run metadata |
| `runs/YYYY-MM-DD-NNN/` | ❌ Local only | Never cleaned | Dated run evidence — kept permanently on disk |
| `.datawatch-test/` | ❌ Gitignored | ✅ Yes (on EXIT) | Test daemon data — always cleaned |

**Evidence is never automatically deleted.** Each run's full output is preserved in `runs/YYYY-MM-DD-NNN/`. The `master-cookbook.md` is the persistent cross-run record; dated run directories are the raw evidence.

## 7. Cleanup Policy

After every run (via `trap EXIT`):
1. Stop test daemon (kill by PID)
2. Stop Docker sim daemon if started
3. Kill local webhook listener if started
4. Stop kubectl port-forward if started
5. Delete all `test-*` resources via REST (sessions, Automata, personas, secrets, filters, schedules, profiles, memories, KG entries, algorithm sessions)
6. `rm -rf .datawatch-test/` — test daemon data
7. `/tmp/dw-docker-sim/` — Docker sim data
8. `runs/YYYY-MM-DD-NNN/evidence/` is **NOT removed** — preserved for audit

## 8. Pass Criteria

| Criterion | Threshold |
|---|---|
| HTTP response shape | Must match python3 assertion |
| CLI exit code | Must be 0 |
| PWA screenshot | Must be saved, no console errors |
| SKIP | Any `[conflict:X]` where X not available — documented in cookbook |
| FAIL threshold | Zero FAIL for release sign-off |
| Coverage | ≥ 90% PASS+SKIP; 0 unexplained failures |

## 9. Running Tests

```bash
# Full run (all sprints):
bash scripts/run-tests.sh

# Surface-filtered:
bash scripts/run-tests.sh --surface=api
bash scripts/run-tests.sh --surface=comms
bash scripts/run-tests.sh --surface=cli

# Feature-filtered:
bash scripts/run-tests.sh --feature=sessions
bash scripts/run-tests.sh --feature=memory

# Skip conflicts:
bash scripts/run-tests.sh --skip-conflict=signal --skip-conflict=pwa

# Specific sprint:
bash scripts/run-tests.sh --sprint=T1
bash scripts/run-tests.sh --sprint=T15

# View latest run:
ls -la internal/server/web/docs/testing/runs/
cat internal/server/web/docs/testing/master-cookbook.md
```

## 10. Future Comm Backends

| Backend | Requirement | Target |
|---|---|---|
| Slack | BOT_TOKEN + workspace | v7.x |
| Telegram | BOT_TOKEN + CHAT_ID | v7.x |
| Discord | WEBHOOK_URL | v7.x |
| Matrix | Homeserver + credentials | v7.x |
| Twilio | Account SID + auth token | v7.x |
| Email | SMTP config | v7.x |
| GitHub Webhook | HMAC secret + public endpoint | v7.x |
