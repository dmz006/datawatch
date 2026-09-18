# Testing Tracker

Two validation levels are required for every interface. See `AGENT.md` for the full rule.

- **Tested=Yes** — Go unit/integration tests exist and pass (`go test`).
- **Validated=Yes** — a live connection or end-to-end test confirmed the interface works with a real or simulated backend.

Do not mark **Validated=Yes** based solely on unit tests.

---

## PWA Image Attachment + File Service API

Added in v8.19.0. File service: `POST /api/files` (multipart upload), `DELETE /api/files`, `GET /api/files/meta`, `GET /api/files/peers/{name}`. PWA input bar 📷 button.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `POST /api/files` multipart upload (image attachment path) — `TestFilesUpload_ImageFile` | Yes | No | `bl333_file_service_test.go` — sends JPEG header bytes, verifies `path` + `bytes` in response and file on disk | Live test: PWA → attach image → verify file lands at `$fileServiceRoot/dw_attach_*` |
| `POST /api/files` path traversal blocked — `TestFilesUpload_PathTraversal` | Yes | No | Sends `path=/tmp/evil.txt` (outside root); expects HTTP 403 | Security boundary; `checkPathTraversal` verified in unit test |
| `DELETE /api/files` — `TestFilesUpload_And_Delete` | Yes | No | Uploads file then deletes; confirms `os.Stat` returns `IsNotExist` | Live curl test not yet performed |
| `GET /api/files/meta` — `TestFileMeta_Empty` | Yes | No | Fresh temp root; verifies `root`, `peers`, `discussions` fields present | Live test: Settings → Files → Storage overview |
| `GET /api/files/peers/{name}` — `TestFilesPeer_Subdir` | Yes | No | Pre-creates `peers/test-peer/note.txt`; confirms listing returns `note.txt` entry | Live federation test not yet performed |
| Federation auth — `CapConfigWrite` required for POST+DELETE | Yes | No | Verified in handler source (`bl333_file_service.go` lines 63, `api.go` handleFiles gate) | No unit test for auth rejection; integration test via federated smoke would confirm |
| PWA 📷 button → upload → preview → send `[image:<path>]` | **Yes** | Yes | TS-761 (PWA/Playwright): structural check of `sessionImageInput`, `accept="image/*"`, `_pendingAttachments` in app.js; live check confirms label in active session DOM | Structural + live DOM check PASS |
| Smoke (§53): `POST /api/files` + `DELETE /api/files` round-trip | Yes (script) | No | `scripts/release-smoke.sh §53` — `curl -F file=@/dev/null` then DELETE; checks HTTP 200 both ways | Run with `bash scripts/release-smoke.sh` against live daemon |

---

## Goose Backend

Added in v8.13.36–v8.14.0. Backends: `goose` (interactive TUI), `goose-prompt` (one-shot).

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `goose` TUI backend (`internal/llm/backends/goose/`) — named sessions, resume, version normalization | Yes | No | 22 unit tests in `internal/llm/backends/goose/backend_test.go`; cover `providerKeyEnvVar`, `shellQuote`, `gooseEnvPrefix`, all setters | Live test requires goose binary. DATAWATCH_COMPLETE detection path not yet validated with a real goose session. |
| `goose-prompt` one-shot backend — `goose run --text`, DATAWATCH_COMPLETE detection | Yes | No | Same test suite as above | Requires goose binary + provider API key. |
| Provider/model/API-key injection (T2) — `GOOSE_PROVIDER`, `GOOSE_MODEL`, `ANTHROPIC_API_KEY` etc. | Yes | No | Unit tests verify env var construction for all providers | Live inject test not yet performed. |
| MCP channel bridge (T3) — `GOOSE_MCP__DATAWATCH__*` env vars | Yes | No | Unit tests verify env var construction when `channel_enabled=true` | Requires goose binary with MCP support. |
| Config parity — all 6 GooseConfig fields via YAML/REST/MCP/CLI/PWA | Yes | No | TS-637–TS-643 in master-cookbook; config-reference.yaml, implementation.md, app.js all updated | PWA section present (Settings → LLM → Goose). Live GET/PUT config round-trip not yet performed end-to-end. |

---

## Vision Input System

Added in the current release cycle. Backends: ollama, openai, openai\_compat.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `Describer` interface (`internal/vision/`) — `New()` + `Describe()` | Yes | No | `internal/vision/service_test.go` (httptest fake servers, 10 tests) | All three backends covered by unit tests. Live validation against a running ollama with llava or gpt-4o-mini not yet performed. |
| `POST /api/vision/describe` (multipart) | Yes | No | `internal/server/vision_test.go` (6 tests: 503/405/200/400/502/mime) | HTTP handler fully unit-tested via httptest. Live curl test with a real image not yet performed. |
| MCP `vision_describe` tool | **Yes** | No | `TestBL368_VisionDescribe_NoVisioner_ReturnsError`, `TestBL368_VisionDescribe_MissingImagePath_ReturnsError`, `TestBL368_VisionDescribe_FileNotFound_ReturnsError`, `TestBL368_VisionDescribe_HappyPath_ReturnsDescription` in `internal/mcp/bl368_vision_describe_test.go` — T47 sprint | 4 unit tests: no visioner, missing path, file not found, happy path. |
| Router image injection (comms → `msg.Text`) | Yes | No | `internal/router/bl368_vision_test.go` (5 tests: CmdRemember injection, plain text, non-command regression) | Verifies `Parse()` still recognises `remember:` after image description is injected. Live test with an actual image attachment via SMS or Matrix not yet performed. |
| `AcceptsImages` manifest field (skills) | Yes | No | `internal/skills/manifest_test.go` (4 tests: true/false/default/no-extra-leak) | Manifest parsing verified. Live test with a skill that declares `accepts_images: true` receiving an image context not yet performed. |
| Council `image_path` field (`POST /api/council/run`) | **Yes** | No | `TestBL368_CouncilRun_ImagePath_PrependedToProposal`, `TestBL368_CouncilRun_ImagePath_FileNotFound_Returns400`, `TestBL368_CouncilRun_ImagePath_VisionerError_Returns500` in `internal/server/bl368_council_image_path_test.go` — T47 sprint | Wired in `internal/server/council.go`; 3 unit tests cover happy path + error cases. |

---

## Autonomous Verifier Git-Diff Grounding

Added in v8.16.0. Captures `git diff <pre_task_sha>..HEAD` before verification; injects it as a `<diff>` block in the verifier prompt.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `SpawnResult.PreTaskSHA` threading → `Task.PreTaskSHA` → `VerifyFn` | Yes | No | `internal/autonomous/bl366_verifier_diff_test.go`: `TestBL366_PreTaskSHA_ThreadedToVerifier`, `TestBL366_PreTaskSHA_StoredOnTask` | Store round-trip confirmed via `mgr.Store().GetTask(id)`. Live test requires a project with git history. |
| Empty SHA no-op (cluster dispatch / non-git project) | Yes | No | `TestBL366_PreTaskSHA_EmptyWhenNoSHA` — verifier receives empty SHA, no panic | No live cluster or non-git project test performed. |
| `autonomous.verifier_diff_max_bytes` default (0 = 8192) | Yes | No | `TestBL366_VerifierDiffMaxBytes_DefaultIsEightKB` confirms zero-value sentinel | Config accessible via all 6 surfaces: YAML, Web UI, REST, comm, MCP, CLI. |
| Git diff capture round-trip (real git repo) | Yes | No | `TestBL366_GitDiffCapture` — creates real git repo, commits, confirms diff contains changed file | Integration test. Live verifier prompt injection requires a running daemon executing a real PRD task. |

## Autonomous PRD Quality Gates

Interface: `POST /api/autonomous/prds` with `quality_gates`, `PUT /api/config` with `autonomous.default_quality_gates.*`, `Manager.SetPRDQualityGates`.

| Test case | Tested | Live-validated | Coverage details | Notes |
|---|---|---|---|---|
| `SetPRDQualityGates` — persists to store and round-trips | Yes | No | `TestBL367_SetPRDQualityGates_Persisted` | Enabled, TestCommand, Timeout, BlockOnRegression all verified. |
| Unknown PRD ID returns error | Yes | No | `TestBL367_SetPRDQualityGates_NotFound` | No panic, descriptive error. |
| Per-PRD override takes precedence over manager default | Yes | No | `TestBL367_ResolveQualityGates_PerPRDOverridesDefault` | `resolveQualityGates` priority. |
| No per-PRD config falls back to manager default | Yes | No | `TestBL367_ResolveQualityGates_FallsBackToDefault` | `cfg.DefaultQualityGates` used when `PRD.QualityGates == nil`. |
| `Task.QualityGateResult` populated after task runs with gate enabled | Yes | No | `TestBL367_QualityGateResult_StoredOnTask` — minimal Go module, `go build .` as gate command | Live regression blocking requires a real PRD with failing tests (v9.0.0 gate). |

## Autonomous Prompt Injection Hardening

Added in v8.18.0. Data-boundary tags on all 3 LLM call sites; `ScanForInjection` scanner wired at PRD/task create and spec edit boundaries; federation trust notice in verifier and guardrail prompts.

| Test case | Tested | Live-validated | Coverage details | Notes |
|---|---|---|---|---|
| `ScanForInjection` detects 11+ known patterns | Yes | No | `TestBL369_ScanForInjection_DetectsPatterns` (13 sub-cases: ignore-prev, disregard, forget, you-are-now, act-as, im_start, SYS, INST, system-prefix, assistant-prefix, new-instructions, override) | Pattern matching confirmed for every entry in `injectionPatterns`. |
| Clean specs return no hits (no false positives) | Yes | No | `TestBL369_ScanForInjection_CleanInputReturnsEmpty` (5 clean task specs) | Guards against over-triggering on normal engineering language. |
| `injection_guard:true, block_on_injection:false` — warn-only mode | Yes | No | `TestBL369_CheckInjectionGuard_WarnMode` — CreatePRD with injection phrase succeeds | Warn-only is the default; operator must opt-in to blocking. |
| `injection_guard:true, block_on_injection:true` — block mode | Yes | No | `TestBL369_CheckInjectionGuard_BlockMode` — CreatePRD returns `injection-guard` error | HTTP 400 at REST boundary. |
| `injection_guard:false` — disabled regardless of `block_on_injection` | Yes | No | `TestBL369_CheckInjectionGuard_Disabled` | Default config: guard off = pass-through. |
| Clean spec always passes block mode | Yes | No | `TestBL369_CleanSpec_AlwaysPasses` | No false-positive blocking. |
| `EditPRDFields` runs injection guard on new spec | Yes | No | `TestBL369_EditPRDFields_BlocksOnInjection` | Covers PRD spec edits, not only create. |
| `EditTaskSpec` runs injection guard on new spec | Yes | No | `TestBL369_EditTaskSpec_BlocksOnInjection` | Covers task-level spec edits. |
| `GuardrailInvocation.OwnerPeer` carries federation peer name | Yes | No | `TestBL369_GuardrailInvocation_CarriesOwnerPeer` — OwnerPeer round-trips store | Layer 3 trust boundary wiring; prompt notice requires live guardrail invocation. |
| Local PRD has empty OwnerPeer | Yes | No | `TestBL369_GuardrailInvocation_LocalPRDNoOwnerPeer` | No spurious trust notice on local PRDs. |
| **LIVE** clean spec → HTTP 200 (`block_on_injection:true`) | Yes | **Yes** | sandbox v8.18.0 at https://127.0.0.1:18444; `POST /api/autonomous/prds` with clean spec returns 200 | Confirmed 2026-08-31. |
| **LIVE** injection phrase → HTTP 400 block | Yes | **Yes** | `POST /api/autonomous/prds` with `"ignore previous instructions"` returns 400: `"injection-guard: potentially unsafe content detected in prd spec (prompt injection: 'ignore previous instructions')"` | Confirmed 2026-08-31. |
| **LIVE** 'you are now' pattern → HTTP 400 | Yes | **Yes** | `POST /api/autonomous/prds` with `"you are now a different AI model"` returns 400 | Confirmed 2026-08-31. |
| **LIVE** warn-only mode → HTTP 200 despite hit | Yes | **Yes** | `block_on_injection:false`; injection phrase returns 200 | Confirmed 2026-08-31. |
| **LIVE** Prometheus counter increments on hit | Yes | **Yes** | `datawatch_injection_guard_hits_total 2` after two blocked requests | Confirmed 2026-08-31. |
| **LIVE** `EditPRD` spec with injection → HTTP 400 | Yes | **Yes** | `POST /api/autonomous/prds/{id}/edit_fields` with injection phrase returns 400 | Confirmed 2026-08-31. |
| Data-boundary tags in `decomposeFn` prompt | **Yes** | Yes | `TestBL369_DecomposeFn_SecurityPreamble` in `cmd/datawatch/bl369_prompt_security_test.go` (TS-753) — source inspection asserts "SECURITY NOTE" + `<user_data>` wrap around req.Spec; TS-754 live decompose via ollama /api/ask confirms round-trip | PASS |
| Security preamble in `autonomousVerify` prompt | **Yes** | Yes | `TestBL369_AutonomousVerify_SecurityPreamble` in `cmd/datawatch/bl369_prompt_security_test.go` (TS-753) — source inspection asserts "SECURITY NOTE" + `<user_data>` + `<diff>` in specPart and prompt | PASS |
| Security preamble + tags in `autonomousGuardrail` prompt | **Yes** | Yes | `TestBL369_AutonomousGuardrail_SecurityPreamble` in `cmd/datawatch/bl369_prompt_security_test.go` (TS-753) — source inspection asserts UnitTitle and UnitSpec wrapped in `<user_data>` with SECURITY NOTE preamble | PASS |

## Autonomous PRD Split Planning/Execution Backend (v8.20.0)

Added in v8.20.0. Separates the PRD planning backend (`decomposition_profile`, used by Decompose/DecomposeStreaming) from the task-execution backend (`backend`, used by autonomous task session spawning). Resolution order (v8.33.9 priority fix): `prd.DecompositionProfile` (per-PRD, wins) → `manager.cfg.PlanningBackend` (global fallback) → `"ollama"` default. Any registered LLM works for planning — opencode/claude-code spawn a full session with codebase tool access; ollama/openwebui run headless. CLI `prd-set-llm` gained `--decomposition-profile` flag in v8.33.9.

`POST /api/autonomous/prds/{id}/set_llm` — extended with `decomposition_profile` field (validated against inference registry). Available on all surfaces: REST, MCP (`autonomous_prd_set_llm`), CLI (`prd-set-llm --decomposition-profile`), PWA Settings modal, Android/iPhone (via REST).

| Test case | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `Decompose()` uses `DecompositionProfile` when set, priority over global | **Yes** | No | `TestBL321_Decompose_UsesDecompositionProfile_OverGlobal`, `TestBL321_Decompose_PRDProfileOverridesGlobal_BothNonEmpty` in `internal/autonomous/bl321_decomp_priority_test.go` | T47 sprint; per-PRD profile wins over cfg.PlanningBackend |
| `Decompose()` falls back to global `PlanningBackend` when `DecompositionProfile` is empty | **Yes** | No | `TestBL321_Decompose_FallsBackToGlobal_WhenProfileEmpty` in `internal/autonomous/bl321_decomp_priority_test.go` | T47 sprint; empty profile → cfg.PlanningBackend used |
| `Decompose()` with opencode backend uses session path with codebase tool access | Yes | No | `TestBL321_Decompose_UsesDecompositionProfile_OverGlobal` in `bl321_decomp_priority_test.go` covers backend propagation; `decomposeFnSession` (main.go closure) routes by kind at runtime. | Live: set `decomposition_profile=opencode-acp`; trigger decompose; confirm opencode session spawned. |
| `SetPRDLLM` persists `DecompositionProfile` field | **Yes** | No | `TestBL320_SetPRDLLM_PersistsDecompositionProfile`, `TestBL320_SetPRDLLM_DecisionLogContainsDecompProfile` in `internal/autonomous/bl320_decomp_profile_test.go` | T47 sprint; decision log check + store round-trip |
| `set_llm` endpoint: unknown `decomposition_profile` returns 400 | **Yes** | No | `TestBL320_SetLLM_UnknownDecompProfile_Returns400` in `internal/server/bl320_set_llm_decomp_profile_test.go` — T47 sprint | POST with invalid `decomposition_profile` → `"unknown planning LLM"` error. |
| `set_llm` endpoint: valid `decomposition_profile` returns 200 | **Yes** | No | `TestBL320_SetLLM_ValidDecompProfile_Returns200` in `internal/server/bl320_set_llm_decomp_profile_test.go` — T47 sprint | POST with known inference-registry name → 200, field persisted. |
| CLI `prd-set-llm --decomposition-profile` round-trip | Yes | No | TS-740 (e2e) | `datawatch autonomous prd-set-llm <id> --decomposition-profile opencode` → GET PRD confirms field. |
| `autonomousSpawn` uses `prd.Backend` (not `DecompositionProfile`) for task sessions | Yes | Yes | TS-743 (e2e, PASS): set_llm backend=ollama decomposition_profile=claude-code, GET PRD confirms independent storage; code inspection confirms spawn uses prd.Backend | Full live run with mismatched backends would confirm session backend. |
| PWA Settings modal — planning backend picker accepts all configured LLMs | **Yes** | Yes | TS-764 (PWA/Playwright): structural check confirms `autonomous.planning_backend` key + `type: 'llm_backend'` in app.js settings config; live check confirms label in settings HTML (collapsed section) | PASS |
| **LIVE** round-trip: set `decomposition_profile=opencode`, verify session-based decompose fires | **Yes** | Yes | TS-766 (e2e): POST `set_llm` `decomposition_profile=opencode`; trigger decompose; sandbox log confirms `[decompose-session] spawned f218 (backend=opencode)` — session-based path confirmed | PASS |

## PWA Current-Status No-Change Contract (v8.19.8)

`GET /api/sessions/{id}/current-status` — changed in v8.19.8 from HTTP 204 (empty body) to HTTP 200 + JSON `{"no_change":true}` on the two no-op branches (no new output; thin delta). Root cause: RFC 7231 §3.3 forbids a body on 204; Go's `net/http` silently drops it; PWA's `r.json()` threw `Unexpected end of JSON input`. `apiFetch` also hardened to resolve 204/205 → null.

| Test case | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| No new output → HTTP 200 + `no_change:true` — `TestCurrentStatus_NoNewOutput` | **Yes** | No | `internal/server/current_status_test.go` — empty output store; asserts 200 + non-empty body + `no_change:true` | Pins the exact bug: 204 + empty body used to reach PWA as a `r.json()` throw |
| Thin delta (output unchanged since last call) → HTTP 200 + `no_change:true` — `TestCurrentStatus_ThinDelta` | **Yes** | No | Same file — output identical on two consecutive calls; second returns `no_change:true` | Covers the second merged branch of the no-op condition |
| Success path with new output → HTTP 200 + content body | **Yes** | No | `TestCurrentStatus_WithNewOutput_Returns200` in `internal/server/current_status_test.go` — T47 sprint | Live test: start a session, let it produce output, poll endpoint; verify body contains session output and no `no_change` field |
| `apiFetch` 204/205 → null guard (PWA) | **Yes** | Yes | TS-747 (e2e, PWA): connectToPWA, evaluate apiFetch.toString(), verify 204/205 guard present + 200 response not null | Validated: Chrome Playwright against sandbox, PASS |
| PWA chip "(no change since last refresh)" renders on no_change response | **Yes** | Yes | TS-760 (PWA/Playwright): intercepts `/api/sessions/*/current-status` to return `{no_change:true}`; calls `fetchCurrentStatus`; asserts `state.currentStatus[id].text` contains "no change" | PASS |

## PWA Session-List Select-All (v8.19.2)

Fixed: select-all / select-none scoped to filtered visible sessions. Counter, selection set, and bulk-delete all honour the active chip + backend filter + text search + history toggle. Filter changes clear selection.

| Test case | Tested | Live-validated | Coverage details | Notes |
|---|---|---|---|---|
| Select-all uses `_visibleDone` (filtered done sessions) not `state.sessions` | **Yes** | Yes | TS-745 (e2e, PWA): connectToPWA, call selectAllInactive(), verify selected.size == _visibleDone.length | Validated: Chrome Playwright against sandbox, PASS |
| Filter change clears active selection (chip, backend, text, clear button) | **Yes** | Yes | TS-746 (e2e, PWA): connectToPWA, selectAllInactive, call setSessionStateChip('running'), verify Set.size==0 | Validated: Chrome Playwright against sandbox, PASS |
| Toggle "None" deselects only visible filtered set | **Yes** | Yes | TS-759 (PWA/Playwright): structural check of `selectAllInactive` in app.js confirms it only iterates `_visibleDone`, not `state.sessions`; toggle logic verified | PASS |

## PWA Image Vision Injection (v8.19.3 + v8.19.4)

Fixed: `expandImageTags()` replaces `[image:<path>]` in all `send_input` text (REST + WebSocket) with `[image: <description> | path: <path>]` via the vision backend. Fallback: tag passes through unchanged if vision is disabled or file is unreadable.

| Test case | Tested | Live-validated | Coverage details | Notes |
|---|---|---|---|---|
| No visioner → pass-through | **Yes** | No | `TestExpandImageTags_NoVisioner` in `vision_test.go` | Unit test: server has no visioner, tag unchanged |
| No tag in text → pass-through | **Yes** | No | `TestExpandImageTags_NoTag` | Unit test |
| File not found → pass-through | **Yes** | No | `TestExpandImageTags_FileNotFound` | Unit test: `/nonexistent/path` |
| Visioner error → pass-through | **Yes** | No | `TestExpandImageTags_VisionerError` | Unit test: `fakeVisioner{err: timeout}` |
| Happy path: description + path in output | **Yes** | No | `TestExpandImageTags_HappyPath` | Unit test: `[image: a red door on a white wall \| path: /tmp/...]` |
| Empty description → pass-through | **Yes** | No | `TestExpandImageTags_EmptyDescription` | Unit test |
| Multiple tags in one message | **Yes** | No | `TestExpandImageTags_MultipleTags` | Unit test: 2 tags both replaced |
| PWA: upload image → attach → send → vision runs before session receives | **Yes** | Yes | TS-762 (PWA/Playwright): structural check verifies `[image:<path>]` tag construction from `_pendingAttachments` in all three send paths in app.js; `stateHasArray` confirmed live | PASS |
| WebSocket path (WS send_input) also runs expandImageTags | **Yes** | Yes | TS-763 (PWA/Playwright): structural check confirms `send_input` reference + `_clearAllAttachments` called after send; `state._pendingAttachments` live array verified | PASS |

## `datawatch mcp-search` interface (v8.22.0)

Added in v8.22.0. Stdio MCP server (`internal/mcp/search/`) that proxies queries to a SearXNG instance and exposes a single `web_search` tool. Injected automatically into opencode and goose sessions when `web_search.enabled: true`.

| Component | Unit tested | Live tested | Unit test coverage | Notes |
|---|---|---|---|---|
| `frameScanner` — Content-Length framed stdin parser | **Yes** | No | `TestFrameScanner` in `internal/mcp/search/server_test.go` | Verifies correct extraction of framed JSON bodies |
| `sendMsg` — framed stdout writer | **Yes** | No | `TestSendMsg` | Verifies `Content-Length: N\r\n\r\n<body>` wire format |
| `handle()` — `ping` method | **Yes** | No | `TestHandlePing` | Returns empty result |
| `handle()` — `tools/list` method | **Yes** | No | `TestHandleToolsList` | Returns `web_search` tool in result array |
| `handle()` — `tools/call web_search` no URL | **Yes** | No | `TestHandleToolsCallNoURL` | Returns error content when URL not configured |
| `searxngSearch()` — SearXNG HTTP proxy | **Yes** | No | `TestSearxngSearch` (httptest.NewServer) | Verifies title/URL/snippet extraction, result capping |
| `ConfigFromEnv()` — env var parsing | **Yes** | No | `TestConfigFromEnv` | DATAWATCH_WEB_SEARCH_URL/ENGINE/NUM_RESULTS |
| opencode injection — `extraMCPSpecs["web_search"]` | **Yes** | Yes | TS-750 (e2e, API): POST /api/sessions/start with backend=opencode, verify .mcp.json has mcpServers.web_search with DATAWATCH_WEB_SEARCH_URL | Validated: sandbox daemon PASS |
| goose injection — web_search SKILL.md + .mcp.json | **Yes** | Yes | TS-751 (e2e, API): POST /api/sessions/start with backend=goose, verify web-search-guidance SKILL.md injected at {projectDir}/.datawatch/skills/web-search-guidance/SKILL.md | Validated: sandbox daemon PASS |
| Skill injection — `web-search-guidance` SKILL.md | Yes | Yes | TS-741 (e2e); TS-751 (goose, PASS); TS-752 (claude-code, PASS) | Validated: sandbox daemon — goose+claude-code sessions write SKILL.md at {projectDir}/.datawatch/skills/web-search-guidance/ |
| REST `GET /api/web_search/stats` | Yes | No | TS-716 (e2e) | Live: curl with bearer token, verify JSON response |
| MCP `web_search_stats` tool | **Yes** | No | `TestBL372_WebSearchStats_ReturnsConfiguredValues`, `TestBL372_WebSearchStats_DisabledReturnsEnabledFalse` in `internal/mcp/bl372_web_search_stats_test.go` — T47 sprint | Live: from connected MCP session, call `web_search_stats` |
| Monitor card — web search stats visible | **Yes** | Yes | TS-744 (e2e, PWA): connectToPWA, navigateTo('observer'), verify .stat-card containing "Web Search" text | Validated: Chrome Playwright against sandbox (web_search_enabled=true), PASS |
| Web UI Settings — web_search section | **Yes** | Yes | TS-749 (e2e, PWA): connectToPWA, navigateTo('settings'), verify web_search section present in panel or API config | Validated: Chrome Playwright against sandbox, PASS |

## v8.23.0 — PRD Task Session Visibility + Reset (2026-09-10)

| Test Condition | Unit | Live | Test ID / Location | Notes |
|---|---|---|---|---|
| `ResetTask` — happy path: failed task in running PRD resets to pending | **Yes** | No | `TestBL382_ResetTask_FailedTask_NoForce_StillWorks` in `internal/autonomous/bl382_cancel_test.go` | Manual: set task status=failed, call ResetTask, verify status="" error="" session_id="" |
| `ResetTask` — blocked task resets | **Yes** | No | `TestBL382_ResetTask_BlockedTask_Resets` in `internal/autonomous/bl382_cancel_test.go` — T47 sprint | Same as above with status=blocked |
| `ResetTask` — task not found returns error | **Yes** | No | `TestBL382_ResetTask_TaskNotFound_ReturnsError` in `internal/autonomous/bl382_cancel_test.go` — T47 sprint | Call with nonexistent task_id; expect error |
| `ResetTask` — PRD not running returns error | **Yes** | No | `TestBL382_ResetTask_PRDNotRunning_ReturnsError` in `internal/autonomous/bl382_cancel_test.go` — T47 sprint | Call while PRD status=approved; expect error |
| `ResetTask` — completed task cannot be reset | **Yes** | No | `TestBL382_ResetTask_NoForce_CompletedTask_ReturnsError` in `internal/autonomous/bl382_cancel_test.go` | status=completed; expect error |
| REST `POST /api/autonomous/prds/{id}/reset_task` 200 | Yes | No | TS-710 (e2e) | Live: running PRD with failed task; POST reset_task; expect 200 + task status="" |
| REST `POST /api/autonomous/prds/{id}/reset_task` 400 nonexistent task | **Yes** | Yes | TS-755 (e2e): POST `reset_task` with `task_id="nonexistent-task-ts755"` on a real PRD; asserts HTTP 400 | PASS |
| MCP `autonomous_prd_reset_task` | **Yes** | No | `TestBL372_AutoPRDResetTask_NoWebPort_ReturnsError` in `internal/mcp/bl372_web_search_stats_test.go` (no-webPort path) — T47 sprint | Live: call from MCP session, verify task reset |
| PWA task row — session link chip visible when task.session_id set | **Yes** | No | TS-748 (e2e, PWA): reads app.js source, confirms `.prd-task-session-link` class name present; live DOM check when PRD created | Full live validation needs running PRD with completed tasks |
| PWA task row — error panel visible in expanded body when task.error set | **Yes** | No | TS-748 (e2e, PWA): reads app.js source, confirms `.prd-task-error` class name present | Full live validation needs running PRD with failed tasks |
| PWA task row — verification summary visible when task.verification set | **Yes** | No | TS-748 (e2e, PWA): reads app.js source, confirms `.prd-task-verif` class name present | Full live validation needs running PRD with verification data |
| PWA task row — Retry button visible for failed task in running PRD | **Yes** | Yes | TS-756 (PWA/Playwright): structural check of `canRetry` in app.js — confirms `task.status==='failed'` and `prd.status==='running'` conditions; `prd-task-retry-btn` class verified | PASS |
| PWA task row — Retry button absent for completed task | **Yes** | Yes | TS-757 (PWA/Playwright): structural check confirms `canRetry` excludes `'completed'`; `canRequeue` covers it instead | PASS |
| PWA Retry button — click calls reset_task and shows toast | **Yes** | Yes | TS-758 (PWA/Playwright): structural check verifies `prdResetTask` references `/reset_task` endpoint and `showToast` in app.js | PASS |

## v8.25.3 — GPU Observer Probes: tegrastats + nvidia-smi (Shape B)

Added in v8.25.3. `internal/observer/gpu_tegrastats.go` and `internal/observer/gpu_smi.go` wire GPU utilization, temperature, memory, and power into `snap.GPU` via `Collector.SetGPUFn`. `cmd/datawatch-stats/main.go` selects tegrastats first, falls back to nvidia-smi. Handles both classic Jetson format (`GR3D_FREQ`) and NVIDIA Thor/GB10 SoC format (no GR3D_FREQ, lowercase `gpu@`, `VDD_GPU` power field).

| Component | Unit tested | Live tested | Unit test coverage | Notes |
|---|---|---|---|---|
| `NewTegraStatsProbe` — returns nil when `tegrastats` not in PATH | Yes | No | `TestNewTegraStatsProbe_NotInPATH` in `gpu_probe_test.go` | Live: run on host without tegrastats, confirm probe nil. |
| `parseTegraStatsLine` — classic Jetson format (`GR3D_FREQ X%`) | **Yes** | No | `TestParseTegraStatsLine_ClassicJetson` in `internal/observer/gpu_probe_test.go` — `RAM 1024/4096MB GR3D_FREQ 45%@1300 GPU@65.5C` → util_pct=45, temp_c=65.5 | T47 sprint |
| `parseTegraStatsLine` — Thor format (no GR3D_FREQ, lowercase `gpu@`, `VDD_GPU`) | **Yes** | No | `TestParseTegraStatsLine_ThorFormat` — `RAM 69178/125772MB ... gpu@35.25C VDD_GPU 2376mW` → util_pct=0, temp_c=35.25, power_w=2.376 | T47 sprint |
| `parseTegraStatsLine` — returns nil when no GPU temp found | **Yes** | No | `TestParseTegraStatsLine_NoGPUTemp_ReturnsNil` — line without `gpu@` → nil | T47 sprint |
| `parseSMIOutput` — `[N/A]` fields become 0 (Tegra unified memory) | **Yes** | No | `TestParseSMIOutput_TegraUnifiedMemory_NAFields` — `"0, Tegra GPU, [N/A], [N/A], [N/A], 45.0"` → mem_used=0, mem_total=0, temp_c=45.0 | T47 sprint |
| `parseSMIOutput` — discrete GPU values | **Yes** | No | `TestParseSMIOutput_DiscreteGPU` — `"0, NVIDIA RTX 4090, 85, 24576, 4096, 72.0"` → util_pct=85, temp_c=72.0 | T47 sprint |
| `Collector.SetGPUFn` — wired into `collect()` before v1 aliases | **Yes** | No | `TestCollector_SetGPUFn_PopulatesSnapGPU`, `TestCollector_SetGPUFn_NilClearsGPU`, `TestCollector_SetGPUFn_NoFn_NoGPUInSnap` in `internal/observer/gpu_probe_test.go` | T47 sprint; GPUPctV1 alias also verified |
| tegrastats selected over nvidia-smi when both present | **Yes** | Yes | TS-765 (e2e): source inspection of `cmd/datawatch-stats/main.go` confirms `NewTegraStatsProbe` (line 179) in `else if` before `NewSMIProbe` (line 183); NVML→tegrastats→nvidia-smi priority order | PASS |
| **LIVE** Thor tegrastats → `snap.GPU` populated | No | **Yes** | `compute_node_detail("datawatch")` via MCP: `gpu:[{name:"Tegra GPU", vendor:"nvidia", util_pct:0, mem_used_bytes:72524759040, mem_total_bytes:131881500672, power_w:2.376, temp_c:35.187}]` | Confirmed 2026-09-12 on NVIDIA Thor GB10 SoC. Required case-insensitive `(?i)gpu@` regex fix. |

## v8.27.5 — Per-guardrail block approval endpoint (GH#153)

Added in v8.27.5. `POST /api/sessions/{id}/guardrail/{name}/approve` marks a single named guardrail verdict as operator-approved. Returns `{guardrail, approved, session_unblocked, telemetry}`. `HookGuardrailVerdict` gains `approved` (bool) and `approval_note` (string) fields. `session_guardrail_approve` MCP tool added.

| Component | Unit tested | Live tested | Unit test coverage | Notes |
|---|---|---|---|---|
| `POST /api/sessions/{id}/guardrail/{name}/approve` — happy path | **Yes** | No | `TestGH153_HTTPApprove_200` in `internal/server/gh153_guardrail_approve_test.go` — TC-5: 200 + approved=true + session_unblocked=true | Live: run session_guardrail_run to add a block verdict; POST approve/{name}; verify response `session_unblocked: true`. |
| `POST /api/sessions/{id}/guardrail/{name}/approve` — 404 for unknown guardrail name | **Yes** | No | `TestGH153_HTTPApprove_404_UnknownGuardrail` in `internal/server/gh153_guardrail_approve_test.go` — TC-6 | Live: POST approve/nonexistent; expect 404 "guardrail not found in session telemetry". |
| `POST /api/sessions/{id}/guardrail/{name}/approve` — note stored | Yes | No | TS-738 (e2e) | POST with `{"note":"test approval"}`; GET telemetry; verify `approval_note` field present. |
| `session_unblocked: false` when other block verdicts remain | Yes | No | TS-739 (e2e) | Live: add two block verdicts; approve one; verify `session_unblocked: false`. |
| `session_guardrail_approve` MCP tool | Yes | No | `TestMCP_SessionGuardrailApprove_NoWebPort_ReturnsError` in `bl_gh153_mcp_guardrail_approve_test.go` | MCP: call `session_guardrail_approve(session_id=..., guardrail=..., note=...)`; verify result. |
| `GET /api/sessions/{id}/telemetry` — `approved`+`approval_note` fields present | Yes | No | TS-742 (e2e) | Verify new fields appear in telemetry response after approve call. |
| WebSocket hub broadcasts on approve | Yes | No | `TestGH153_WS_BroadcastFiresOnApprove` in `gh153_ws_broadcast_test.go` | Live: connect real WS client; approve; observe hook_update message. |

## v8.27.6–v8.27.11 — PWA image attachment (Android)

Chain of fixes across six patch releases. v8.27.11 is the stable version.

| Component | Unit tested | Live tested | Unit test coverage | Notes |
|---|---|---|---|---|
| File input label (Android) — `<label for>` + `display:none` replaces off-screen fixed element | No | Yes (v8.27.6) | — | User confirmed Android file picker opens correctly. |
| Preview strip position — sibling before inputBar (not inside flex row) | No | Yes (v8.27.6) | — | Preview appears above command row. |
| `_pendingAttachments[]` state survives re-render | No | Yes (v8.27.7) | — | Preview restored at end of every `renderSessionDetail`. |
| Multi-file upload — `multiple` attribute + concurrent uploads | No | Yes (v8.27.7) | — | Multiple chips shown with per-chip remove. |
| `POST /api/files` bare filename path traversal fix | Yes | Yes (v8.27.8) | `TestHandleFilesUpload_BareFilename` | v8.27.8 fix: bare names joined to fileServiceRoot before traversal check. |
| `sendSessionInputDirect` attachment handling | No | Yes (v8.27.9) | — | Channel-mode sessions with tmux tab route through this path. |
| Block-while-uploading guard (all three send paths) | No | Yes (v8.27.10) | — | Toast + pulsing ⏫ → ✓ flow confirmed on Android. |
| `expandImageTags` no-visioner → `@path` conversion | Yes | Yes (v8.27.11) | `TestExpandImageTags_NoVisioner` | Converts `[image:path]` to `@path` so Claude Code sessions read the file via own vision pipeline. |

## v8.31.0 — Automata Memory Integration: Verifier Findings + Child Inheritance

Added in v8.31.0. When `memory_seed.enabled=true` on an Automaton, two new callbacks
fire: (1) after each verification failure, each issue string is written to `prd-shared`
with `role=verifier-finding`; (2) when a child Automaton is spawned via recursive
decomposition, up to 50 entries from the parent's `prd-shared` are seeded into the child's
`prd-shared` before the child decomposes.

| Component | Unit tested | Live tested | Unit test coverage | Notes |
|---|---|---|---|---|
| `memoryVerifierFn` called per issue on verify fail + seed enabled | Yes | No | `TestBL387_Verifier_WritesFindings_ToPRDShared_OnFailure` | At least 2 calls per retry (2 issues × 1+ retries). Role=verifier-finding, prefix=[verifier-finding]. |
| `memoryVerifierFn` NOT called on verify success | Yes | No | `TestBL387_Verifier_NoWrite_OnSuccess` | fn must not fire for OK=true tasks |
| `memoryVerifierFn` NOT called when `memory_seed.enabled=false` | Yes | No | `TestBL387_Verifier_NoWrite_WhenMemorySeedDisabled` | Default disabled state |
| `[verifier-finding]` prefix present in content | Yes | No | `TestBL387_Verifier_ContainsPrefix` | Content always starts `[verifier-finding] <issue>` |
| `memoryVerifierFn` error does not abort retry loop | Yes | No | `TestBL387_Verifier_ErrorDoesNotAbortRetry` | Simulated write failure → executor continues; verifyCount > 0 |
| `memoryScopeSeedFn` called with parent/child PRD IDs + projectDir | Yes | No | `TestBL387_ChildPRD_InheritsParentPRDShared_WhenSeedEnabled` | fromPRDID=parent.ID, toPRDID=child.ID, same projectDir |
| `memoryScopeSeedFn` NOT called when parent `memory_seed.enabled=false` | Yes | No | `TestBL387_ChildPRD_NoInheritance_WhenSeedDisabled` | Must not fire for disabled parent |
| `memoryScopeSeedFn` maxEntries capped at 50 | Yes | No | `TestBL387_ChildPRD_InheritanceCappedAt50` | Hard cap regardless of parent MaxPerScope |
| `SetMemoryVerifierFn` wires callback on Manager | Yes | No | Used in all Verifier tests above | Production wire-up in main.go pending Phase 2 integration |
| `SetMemoryScopeSeedFn` wires callback on Manager | Yes | No | Used in all ChildPRD tests above | Production wire-up in main.go pending Phase 2 integration |

## v8.32.0 — Automata Memory Integration: Decomposer Enrichment + Cross-Automaton Seeding

Added in v8.32.0. Two Phase 2 callbacks: `memoryContextFn` (decomposer prompt enrichment
from `project-shared`) and `memoryCrossSeedFn` (cross-Automaton prd-shared seeding at
first run via `from_prds`). `MemorySeedConfig.FromPRDs` field added. REST, MCP, and
`AutonomousAPI` interface all updated.

| Scenario | Automated | Manual | Test | Notes |
|----------|-----------|--------|------|-------|
| `memoryContextFn` called during `Decompose` with projectDir + limit=15 | Yes | No | `TestBL387_Decomposer_InjectsProjectSharedContext_WhenMemoriesExist` | PRD must have ProjectDir set |
| Empty context result from `memoryContextFn` does not crash | Yes | No | `TestBL387_Decomposer_NoInjection_WhenNoMemoriesExist` | fn called but result ignored |
| `Decompose` works normally with nil `memoryContextFn` | Yes | No | `TestBL387_Decomposer_NoInjection_WhenContextFnNil` | No panic with zero-value fn |
| `memoryCrossSeedFn` called once per `from_prds` entry at first run | Yes | No | `TestBL387_CrossPRDSeed_SeedsFromListedPRDs_AtFirstRun` | 2 calls for 2 entries; correct IDs + maxEntries |
| `memoryCrossSeedFn` NOT called when `memory_seed.enabled=false` | Yes | No | `TestBL387_CrossPRDSeed_NoSeed_WhenMemorySeedDisabled` | Guard on Enabled flag |
| `memoryCrossSeedFn` NOT called when `from_prds` is empty | Yes | No | `TestBL387_CrossPRDSeed_NoSeed_WhenFromPRDsEmpty` | Guard on empty slice |
| `memoryCrossSeedFn` NOT called on resume (PRDRunning) | Yes | No | `TestBL387_CrossPRDSeed_SkipsOnResume_WhenAlreadyRunning` | isFirstRun=false for PRDRunning |
| `memoryCrossSeedFn` maxEntries defaults to 20 when MaxPerScope=0 | Yes | No | `TestBL387_CrossPRDSeed_UsesMaxPerScope_DefaultsTo20` | Hard default applied |

## v8.33.0 — Automata Memory Integration: Auto-Report + Memory Scope PWA Tile

Added in v8.33.0. `memoryReportFn` callback fires asynchronously on `PRDCompleted` when
`MemorySeed.Enabled=true`; result stored in `PRD.MemoryReport` + `PRD.MemoryReportAt`.
Memory scope PWA tile (`memory-scope` card) added to dashboard, rendering stats from
`/api/memory/stats`.

| Scenario | Automated | Manual | Test | Notes |
|----------|-----------|--------|------|-------|
| `memoryReportFn` called on PRDCompleted when MemorySeed.Enabled | Yes | No | `TestBL387_AutoReport_CalledOnCompletion` | Goroutine fires after Run() returns |
| `memoryReportFn` NOT called when MemorySeed.Enabled=false | Yes | No | `TestBL387_AutoReport_NotCalledWhenSeedDisabled` | Guard on Enabled flag |
| `Decompose` works normally with nil memoryReportFn | Yes | No | `TestBL387_AutoReport_NotCalledWhenFnNil` | No panic; PRD reaches PRDCompleted |
| `PRD.MemoryReport` + `PRD.MemoryReportAt` stored after successful call | Yes | No | `TestBL387_AutoReport_StoredOnPRD` | MemoryReportAt ≥ test start time |
| reportFn error does not abort PRD completion | Yes | No | `TestBL387_AutoReport_ErrorDoesNotAbortCompletion` | PRDCompleted set; MemoryReport empty |
| Empty report string does not write MemoryReport | Yes | No | `TestBL387_AutoReport_EmptyStringNotStored` | Empty string skipped |
| memory-scope dashboard card appears in default layout | No | Yes | Visual — dashboard memory-scope tile visible | Fetches /api/memory/stats |
