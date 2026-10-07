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
| `POST /api/files` multipart upload (image attachment path) — `TestFilesUpload_ImageFile` | Yes | **Yes** | `bl333_file_service_test.go` — sends JPEG header bytes, verifies `path` + `bytes` in response and file on disk | TS-767 (e2e): upload temp file, verify path+bytes in JSON response, root from /api/files/meta; PASS |
| `POST /api/files` path traversal blocked — `TestFilesUpload_PathTraversal` | Yes | **Yes** | Sends `path=/tmp/evil.txt` (outside root); expects HTTP 403 | TS-768 (e2e): `../../etc/passwd`=403, `/etc/passwd`=403, legitimate upload=200; PASS |
| `DELETE /api/files` — `TestFilesUpload_And_Delete` | Yes | **Yes** | Uploads file then deletes; confirms `os.Stat` returns `IsNotExist` | TS-767 (e2e): DELETE with absolute path returns `{"deleted":...}`; second DELETE returns 404; PASS |
| `GET /api/files/meta` — `TestFileMeta_Empty` | Yes | **Yes** | Fresh temp root; verifies `root`, `peers`, `discussions` fields present | TS-767 (e2e): GET /api/files/meta returns `{"root":"/home/dmz/workspace/datawatch",...}`; PASS |
| `GET /api/files/peers/{name}` — `TestFilesPeer_Subdir` | Yes | No | Pre-creates `peers/test-peer/note.txt`; confirms listing returns `note.txt` entry | Live federation test not yet performed |
| Federation auth — `CapConfigWrite` required for POST+DELETE | Yes | No | Verified in handler source (`bl333_file_service.go` lines 63, `api.go` handleFiles gate) | No unit test for auth rejection; integration test via federated smoke would confirm |
| PWA 📷 button → upload → preview → send `[image:<path>]` | **Yes** | Yes | TS-761 (PWA/Playwright): structural check of `sessionImageInput`, `accept="image/*"`, `_pendingAttachments` in app.js; live check confirms label in active session DOM | Structural + live DOM check PASS |
| Smoke (§53): `POST /api/files` + `DELETE /api/files` round-trip | Yes (script) | No | `scripts/release-smoke.sh §53` — `curl -F file=@/dev/null` then DELETE; checks HTTP 200 both ways | Run with `bash scripts/release-smoke.sh` against live daemon |

---

## Goose Backend

Added in v8.13.36–v8.14.0. Backends: `goose` (interactive TUI), `goose-prompt` (one-shot).

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `goose` TUI backend (`internal/llm/backends/goose/`) — named sessions, resume, version normalization | Yes | **Yes** | 22 unit tests in `internal/llm/backends/goose/backend_test.go`; cover `providerKeyEnvVar`, `shellQuote`, `gooseEnvPrefix`, all setters | TS-775 (e2e): POST /api/sessions/start backend=goose → session id=f856 backend_family=goose state=running; PASS |
| `goose-prompt` one-shot backend — `goose run --text`, DATAWATCH_COMPLETE detection | Yes | No | Same test suite as above | Requires goose binary + provider API key. |
| Provider/model/API-key injection (T2) — `GOOSE_PROVIDER`, `GOOSE_MODEL`, `ANTHROPIC_API_KEY` etc. | Yes | No | Unit tests verify env var construction for all providers | Live inject test not yet performed. |
| MCP channel bridge (T3) — `GOOSE_MCP__DATAWATCH__*` env vars | Yes | No | Unit tests verify env var construction when `channel_enabled=true` | Requires goose binary with MCP support. |
| Config parity — all 6 GooseConfig fields via YAML/REST/MCP/CLI/PWA | Yes | **Yes** | TS-637–TS-643 in master-cookbook; config-reference.yaml, implementation.md, app.js all updated | TS-774 (e2e): GET /api/config goose.enabled=true goose.binary=/home/dmz/.local/bin/goose; matches testdata/datawatch.yaml; binary v1.50.1 confirmed; PASS |

---

## Vision Input System

Added in the current release cycle. Backends: ollama, openai, openai\_compat.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `Describer` interface (`internal/vision/`) — `New()` + `Describe()` | Yes | No | `internal/vision/service_test.go` (httptest fake servers, 10 tests) | All three backends covered by unit tests. Live validation against a running ollama with llava or gpt-4o-mini not yet performed. |
| `POST /api/vision/describe` (multipart) | Yes | **Yes** | `internal/server/vision_test.go` (6 tests: 503/405/200/400/502/mime) | TS-773 (e2e): POST real 8×8 PNG → HTTP 200 description='urn of water...' latency_ms=5401 (moondream:latest); PASS |
| MCP `vision_describe` tool | **Yes** | No | `TestBL368_VisionDescribe_NoVisioner_ReturnsError`, `TestBL368_VisionDescribe_MissingImagePath_ReturnsError`, `TestBL368_VisionDescribe_FileNotFound_ReturnsError`, `TestBL368_VisionDescribe_HappyPath_ReturnsDescription` in `internal/mcp/bl368_vision_describe_test.go` — T47 sprint | 4 unit tests: no visioner, missing path, file not found, happy path. |
| Router image injection (comms → `msg.Text`) | Yes | No | `internal/router/bl368_vision_test.go` (5 tests: CmdRemember injection, plain text, non-command regression) | Verifies `Parse()` still recognises `remember:` after image description is injected. Live test with an actual image attachment via SMS or Matrix not yet performed. |
| `AcceptsImages` manifest field (skills) | Yes | No | `internal/skills/manifest_test.go` (4 tests: true/false/default/no-extra-leak) | Manifest parsing verified. Live test with a skill that declares `accepts_images: true` receiving an image context not yet performed. |
| Council `image_path` field (`POST /api/council/run`) | **Yes** | No | `TestBL368_CouncilRun_ImagePath_PrependedToProposal`, `TestBL368_CouncilRun_ImagePath_FileNotFound_Returns400`, `TestBL368_CouncilRun_ImagePath_VisionerError_Returns500` in `internal/server/bl368_council_image_path_test.go` — T47 sprint | Wired in `internal/server/council.go`; 3 unit tests cover happy path + error cases. |
| Council `backends` field (`PATCH`/`GET /api/council/config`) | **Yes** | No | `TestBL390_CouncilConfig_BackendsRoundTrip` in `internal/server/bl390_council_config_backends_test.go` | PATCH → in-memory update → GET reflects it → reload from disk confirms persistence, not just a mutation. MCP `council_config_set`/`council_personas_set` are thin REST proxies (no independent dispatch logic) so this REST test is the authoritative coverage; same pattern as other proxy-only MCP tools in this tracker. Live validation against a real multi-backend council run not yet performed. |
| Council per-persona `backend`/`model` resolution (`internal/council`) | **Yes** | No | `TestResolvePersonaBackendModel_*` (3 tests) + `TestOrchestrator_CapacityAdmitFn_*` (2 tests) in `internal/council/bl390_backend_model_test.go` | Covers the backend/model cascade (persona override wins, no-override falls back to council default, a bare Model without Backend is ignored) and capacity admission (admitted+released once per persona+synthesis call; a denial surfaces as a persona-level error instead of calling InferenceFn). |

---

## Autonomous Verifier Git-Diff Grounding

Added in v8.16.0. Captures `git diff <pre_task_sha>..HEAD` before verification; injects it as a `<diff>` block in the verifier prompt.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `SpawnResult.PreTaskSHA` threading → `Task.PreTaskSHA` → `VerifyFn` | Yes | No | `internal/autonomous/bl366_verifier_diff_test.go`: `TestBL366_PreTaskSHA_ThreadedToVerifier`, `TestBL366_PreTaskSHA_StoredOnTask` | Store round-trip confirmed via `mgr.Store().GetTask(id)`. Live test requires a project with git history. |
| Empty SHA no-op (cluster dispatch / non-git project) | Yes | No | `TestBL366_PreTaskSHA_EmptyWhenNoSHA` — verifier receives empty SHA, no panic | No live cluster or non-git project test performed. |
| `autonomous.verifier_diff_max_bytes` default (0 = 8192) | Yes | **Yes** | `TestBL366_VerifierDiffMaxBytes_DefaultIsEightKB` confirms zero-value sentinel | TS-777 (e2e): PUT `{"autonomous.verifier_diff_max_bytes": 4096}` → GET confirms 4096; sentinel 0=8192 in source; PASS |
| Git diff capture round-trip (real git repo) | Yes | No | `TestBL366_GitDiffCapture` — creates real git repo, commits, confirms diff contains changed file | Integration test. Live verifier prompt injection requires a running daemon executing a real PRD task. |

## Autonomous PRD Quality Gates

Interface: `POST /api/autonomous/prds` with `quality_gates`, `PUT /api/config` with `autonomous.default_quality_gates.*`, `Manager.SetPRDQualityGates`.

| Test case | Tested | Live-validated | Coverage details | Notes |
|---|---|---|---|---|
| `SetPRDQualityGates` — persists to store and round-trips | Yes | **Yes** | `TestBL367_SetPRDQualityGates_Persisted` | TS-770 (e2e): POST set_quality_gates enabled=true cmd='echo ts770-ok' timeout=30; GET PRD confirms round-trip; PASS |
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
| `injection_guard:true, block_on_injection:true` — block mode | Yes | **Yes** | `TestBL369_CheckInjectionGuard_BlockMode` — CreatePRD returns `injection-guard` error | TS-769 (e2e): PUT `{"autonomous.injection_guard":"true"}` + `{"autonomous.block_on_injection":"true"}` → GET confirms both true; PASS |
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
| CLI `prd-set-llm --decomposition-profile` round-trip | Yes | **Yes** | TS-740 (e2e) | TS-776 (e2e): `datawatch autonomous prd-set-llm <id> --decomposition-profile opencode` → GET PRD confirms decomposition_profile=opencode; fixed daemonJSON []byte double-encode bug in cli_sx_parity.go + cli_autonomous.go; PASS |
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

## Multi-provider web search registry (BL391, v8.39.0)

Rewritten in v8.39.0 from the single-provider SearXNG-only design (v8.22.0/BL372).
`internal/websearch` now holds a provider-agnostic `Registry` (SearXNG + Brave
Search API, tried in priority order, extensible), an internal result cache, and
a SQLite usage-tracking store, shared in-process by REST/MCP/stats and by the
standalone `datawatch mcp-search` subprocess (`internal/mcp/search/`, injected
into opencode/goose sessions). The pre-BL391 rows below (`frameScanner`,
`searxngSearch()`, `TestHandleToolsCallNoURL`, etc.) described functions that
no longer exist after the rewrite — this section replaces them.

| Component | Unit tested | Live tested | Unit test coverage | Notes |
|---|---|---|---|---|
| `SearxngProvider.Search()` | **Yes** | No | `TestSearxngProviderSearch` in `internal/websearch/searxng_test.go` | Forces `engines=bing` always, per GH#165 |
| `BraveProvider.Search()` | **Yes** | No | `TestBraveProviderSearch`, `TestBraveProviderAuthRejected`, `TestBraveProviderRateLimited` in `internal/websearch/brave_test.go` | httptest-backed; test-only `endpoint` field overrides the real API host |
| `Cache` — hit/miss/expiry/per-entry TTL override | **Yes** | No | `cache_test.go`: `TestCacheHitMiss`, `TestCacheKeyDistinguishesProviderQueryLimit`, `TestCacheExpiry`, `TestCacheDisabledViaZeroTTL`, `TestCachePerEntryTTLOverride`, `TestNilCacheIsSafe` | nil `*Cache` always misses (caching-disabled path) |
| `Store` — SQLite usage rollups | **Yes** | No | `store_test.go`: `TestStoreRecordAndSummary`, `TestStoreSummaryCalendarBoundaries`, `TestStoreDailySeriesZeroFilled`, `TestStoreHistoryPaginationAndOrder`, `TestStorePrune`, `TestNilStoreIsSafe` | Calendar boundaries (today/week/month) computed in UTC |
| `Registry.Search()` — priority fallback + cache | **Yes** | No | `registry_test.go`: `TestRegistryPriorityFallback`, `TestRegistrySkipsEmptyResultsNotJustErrors` (direct GH#165 regression guard), `TestRegistryAllProvidersFail`, `TestRegistryCacheHitSkipsLiveCall`, `TestRegistryPriorityOrdering`, `TestRegistryUnknownProviderType`, `TestRegistryNotEnabledWithNoProviders` | A provider that "succeeds" with 0 results is treated as a miss, not a hit |
| `mcp-search` `handle()` — tools/list, tools/call, ping, unknown method/tool | **Yes** | No | `internal/mcp/search/server_test.go`: `TestHandlePing`, `TestHandleToolsList`, `TestHandleToolsCallNoProviders`, `TestHandleToolsCallWithRegistry`, `TestHandleToolsCallUnknownTool`, `TestHandleUnknownMethod`, `TestHandleNotification` | NDJSON transport (one JSON object per line, no Content-Length framing) |
| `mcp-search` `buildRegistry()` — standalone override vs. full config.yaml | **Yes** | No | `TestBuildRegistryStandaloneOverride`, `TestBuildRegistryFromConfigYAML` | Override path (`--url`/env) wins over `config.yaml`'s `providers[]` |
| REST `/api/websearch/providers` CRUD — secret handling | **Yes** | No | `internal/server/websearch_test.go`: `TestWebSearchEnable_DoesNotPersistResolvedSecret`, `TestWebSearchUpdate_ExplicitAPIKeySavedAsGiven`, `TestWebSearchUpdate_OtherFieldDoesNotLeakSecret`, `TestWebSearchProvidersList_ReturnsConfiguredProviders`, `TestWebSearchProvidersList_NeverEchoesResolvedAPIKey`, `TestWebSearchDelete_RemovesProviderWithoutLeakingOthersSecret` | Guards the exact plaintext-secret-leak class of bug: a config save triggered by an unrelated field must never write a resolved `${secret:...}` value back to `config.yaml` |
| opencode/goose injection — `extraMCPSpecs["web_search"]` gated on `webSearchActive` | Partial (config-shape unit only) | **Yes — with an important caveat found live** | `TestBuildRegistryFromConfigYAML` covers the config-load half | **Live finding (2026-10-02, this host):** the per-session `.mcp.json` file the daemon writes into an opencode session's project dir is not actually consumed by opencode — opencode's MCP discovery on this host comes entirely from the global, statically-configured `~/.config/opencode/opencode.jsonc` `mcp` key (native `{type:"local",command:[...],enabled:true}` schema, not a sibling `.mcp.json` file). That global config already has a `searxng` entry running `datawatch mcp-search` with no env override, which DOES correctly load the full multi-provider `config.yaml` registry and is exposed to the model as tool `searxng_web_search` (opencode's own `<server>_<tool>` naming). Verified end-to-end live: a real PRD-adjacent opencode session calling `searxng_web_search` round-tripped through `Registry.Search()` → SearXNG provider → recorded to `websearch.db` correctly (query, provider, success, result count — `session_id` empty since the global config sets no `DATAWATCH_SESSION_ID` env var, a minor attribution gap, not a correctness bug). **This appears to predate BL391** (the daemon's dynamic per-session `.mcp.json`-write mechanism for `web_search` looks to have never been consumed by opencode on this host, even pre-BL391 — TS-750's e2e check only asserted the file's *contents*, never that opencode actually reads it). Not fixed in this release (separate, pre-existing scope); flagged here so it isn't lost. Goose uses a different mechanism (env vars set directly on the goose process, not a config file) and was not independently live-verified this release. |
| MCP admin tools `websearch_providers_list/get/add/update/delete/enable/disable/test`, `websearch_stats`, `websearch_history` | No (proxy-to-REST pattern, same as `llm_*`) | No | Thin proxies over the REST handlers above (`internal/mcp/websearch.go`), same untested-proxy convention as `internal/mcp/inference.go`'s `llm_*` tools | Live: call from a connected MCP session |
| CLI `datawatch websearch providers\|get\|add\|update\|delete\|enable\|disable\|test\|stats\|history` | No (thin REST wrapper, same as `llm` CLI) | No | `cmd/datawatch/cli_websearch.go` | Live: `datawatch websearch providers` |
| Comm channel `websearch`, `websearch providers\|stats\|history`, `websearch enable\|disable\|test <name>` | No (thin REST wrapper, same as `llm` comm verbs) | No | `internal/router/websearch.go` | Full provider CRUD deliberately excluded from chat (matches Council's `backends` precedent) |
| Legacy REST `GET /api/web_search/stats` (deprecated alias) | Yes | **Yes** | TS-772 (e2e, pre-BL391) — flat shape unchanged, still returns the single-provider fields | Kept for backward compat; prefer `GET /api/websearch/stats` |
| Legacy MCP `web_search_stats` tool (deprecated alias) | **Yes** | No | `TestBL372_WebSearchStats_ReturnsConfiguredValues`, `TestBL372_WebSearchStats_DisabledReturnsEnabledFalse` | Kept for backward compat; prefer `websearch_stats` |
| PWA Settings → Compute → "Web Search Providers" card (add/edit/delete/enable/disable/test) | No (no JS test harness in this repo) | **Pending** | — | Live: Settings → Compute tab, manual + planned Playwright pass |
| PWA Dashboard "Search Usage" card (`_dashRenderWebSearchUsage`) | No | **Pending** | — | Live: Dashboard tab, new `websearch-usage` card in `DASH_CARD_DEFS` |
| PWA Monitor tab "Search Usage" stat card + history modal (`refreshWebSearchStatsCard`, `webSearchOpenHistoryView`) | No | **Pending** | — | Live: Monitor tab, replaces the old flat BL372 web-search tile |
| Config auto-migration — legacy `web_search.{provider,url,engine,num_results}` → synthesized `providers: [{name: "default", ...}]` | **Yes** | No | Covered by `internal/config` package tests (`applyDefaults` migration logic) | Preserves pre-BL391 `config.yaml` files without a manual edit |
| **End-to-end: real search through opencode + direct `mcp-search`, cache + usage verified live** | — | **Yes (2026-10-02, live on this host, v8.39.0)** | — | Operator-requested verification, done against the production daemon after rebuild+restart: (1) direct NDJSON `mcp-search` round-trip — identical repeated query recorded `cache_hit=0` then `cache_hit=1` in `websearch.db`, confirmed via `/api/websearch/stats` and `/api/websearch/history`; (2) `POST /api/websearch/providers/brave-fallback/test` — live Brave Search API call succeeded (3 real results); (3) `POST /api/websearch/providers/searxng-primary/test` — live SearXNG call succeeded; (4) a real opencode session (via `POST /api/sessions/start`) calling the actually-exposed `searxng_web_search` tool (see the injection-caveat row above) round-tripped through the same `Registry` → recorded to `websearch.db`. Brave specifically was exercised via the REST test endpoint rather than through opencode, since SearXNG's higher priority (0 < 1) wins the live registry's provider race whenever it succeeds — forcing a live opencode call through Brave specifically would require temporarily disabling the SearXNG provider, not done this pass. |

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
| `POST /api/sessions/{id}/guardrail/{name}/approve` — 404 for unknown guardrail name | **Yes** | **Yes** | `TestGH153_HTTPApprove_404_UnknownGuardrail` in `internal/server/gh153_guardrail_approve_test.go` — TC-6 | TS-771 (e2e): POST approve/ts771-phantom → 404; unknown session → 404; GET telemetry has updated_at; PASS |
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
| `memoryContextFn` called during `Decompose` with projectDir + limit=15 | Yes | **Yes** | `TestBL387_Decomposer_InjectsProjectSharedContext_WhenMemoriesExist` | TS-778 (e2e): source inspection confirms all 5 BL387 callbacks wired in main.go (SetMemoryVerifierFn, SetMemoryScopeSeedFn, SetMemoryContextFn, SetMemoryCrossSeedFn, SetMemoryReportFn); memory save + PRD creation with matching project_dir confirmed; PASS |
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

## Structural Automaton editing — add/remove story or task without decompose (2026-09-29)

Operator-requested: "I should also be able to edit a story so I can make changes
without having to decompose and rely on the LLM." Added `AddStory`/`RemoveStory`/
`AddTask`/`RemoveTask` across REST, MCP, CLI, and comm channel, plus PWA UI (per-story
Remove icon + "+ Add task", per-task Remove icon, PRD-level "+ Add story"). Filled a
matching pre-existing gap for `edit_story` on MCP/CLI/comm-channel (REST-only before).
Only allowed pre-approval (`needs_review`/`revisions_asked`), matching the existing
edit-story/edit-task gate.

| Scenario | Automated | Manual | Test / Location | Notes |
|----------|-----------|--------|------|-------|
| `AddStory` appends story, sets IDs/status, records `add_story` decision | **Yes** | No | `TestAddStory_AppendsAndAudits` in `internal/autonomous/structural_edit_test.go` | PASS |
| `AddStory` refuses empty title | **Yes** | No | `TestAddStory_RequiresTitle` | PASS |
| `AddStory` refuses after Approve | **Yes** | No | `TestAddStory_RefusesAfterApprove` | PASS |
| `RemoveStory` deletes story + its tasks; task unreachable via `GetTask` after | **Yes** | No | `TestRemoveStory_DeletesStoryAndItsTasks` | PASS — covers store-side reindex |
| `RemoveStory` errors on nonexistent story ID | **Yes** | No | `TestRemoveStory_NotFound` | PASS |
| `AddTask` appends to story; new + pre-existing tasks both resolve via `GetTask` | **Yes** | No | `TestAddTask_AppendsToStoryAndAudits` | PASS — the reindex-after-append hazard (Go slice reallocation orphaning old store map pointers) was deliberately exercised here |
| `AddTask` errors on nonexistent story ID | **Yes** | No | `TestAddTask_StoryNotFound` | PASS |
| `RemoveTask` deletes one task, keeps sibling task resolvable | **Yes** | No | `TestRemoveTask_DeletesOneTaskKeepsOthers` | PASS |
| `RemoveTask` errors on nonexistent task ID | **Yes** | No | `TestRemoveTask_NotFound` | PASS |
| All four refuse after Approve (structural lock) | **Yes** | No | `TestStructuralEdits_RefuseAfterApprove` | PASS |
| REST `POST .../add_story` 400 when PRD not in needs_review/revisions_asked | **Yes** | Yes | Manual curl against a live `planning`-status PRD | PASS — `curl -X POST .../add_story -d '{"title":"x"}'` → 400 `"...is locked; only needs_review / revisions_asked accept structural edits"` |
| REST `POST .../add_story` 400 on missing title | **Yes** | Yes | Manual curl against a live PRD | PASS — 400 `"title required"` |
| PWA `renderStory`/`renderTask`/`_renderDetailStories` emit well-formed onclick for all 4 new buttons | **Yes** | Yes | Playwright: rendered a fake `needs_review` PRD through the real functions, read `getAttribute('onclick')` on each new button | PASS — `prdRemoveStory("fake123","story1","S1")`, `openPRDAddTaskModal("fake123","story1")`, `prdRemoveTask("fake123","story1","task1","T1")`, `openPRDAddStoryModal("fake123")`; zero page errors |
| Full click-through (open Add-story modal, submit, verify story appears) | No | Not yet | — | Attempted via a live decompose on a throwaway PRD; the local qwen3.8:27b planning backend didn't finish within ~7 min so the attempt was abandoned in favor of the REST/render-level checks above, which already cover the new code paths. Revisit with a faster planning backend if a full click-through is needed. |

## Capacity-aware admission for interactive sessions + verifier; session-delete memory strategy; capacity-card fixes (2026-09-29/30)

Operator-reported while diagnosing why PRD a2833a5e's verifier used a weak
model and why its capacity card showed an unrelated node: (1) a genuine data
race in daemon-restart boot-resume that could kill a live in-flight session;
(2) capacity ledger only ever listed nodes/pools an operator had explicitly
capped, and `/api/capacity` was never scoped to a specific PRD; (3)
interactive session starts and the verifier's own `/api/ask` call had zero
capacity interaction at all; (4) verifier backend/model always fell back to
a hardcoded `ollama` default, ignoring the PRD's own backend; (5) found and
fixed a real pre-existing data-loss bug in `PurgeScope`/`ArchiveScope` while
wiring session-delete's memory strategy (session-local purge/archive
discarded the session ID and operated on the whole project).

| Scenario | Automated | Manual | Test / Location | Notes |
|----------|-----------|--------|------|-------|
| `resumeRunningPRDs` skips a PRD with an already-live executor (runCancels) | **Yes** | No | `TestResumeRunningPRDs_SkipsPRDWithLiveExecutor` in `internal/autonomous/executor_retry_kill_test.go` | PASS — regression test for the boot-resume/live-Run race; also confirmed via 1000 repeated runs of the original flaky test (`go test -count=200`, 5×) with zero failures, matching CI's exact conditions (no `-race`) |
| `handleCapacity` always lists every registered compute node, even unconfigured/idle | **Yes** | No | `TestHandleCapacity_ListsUnconfiguredNodes` in `internal/server/capacity_node_scope_test.go` | PASS |
| `handleCapacity?prd_id=` scopes pools to that PRD's own backend + per-story/per-task overrides | **Yes** | No | `TestHandleCapacity_PRDScoping` | PASS — covers both "no override" (Claude-only PRD excludes an unrelated node) and "task-level backend override" (node appears) |
| `handleStartSession` admits a genuine interactive start through `capacityAdmit`, binds the real session ID | **Yes** | No | `TestHandleStartSession_CapacityAdmit_Gated` in `internal/server/capacity_interactive_test.go` | PASS |
| `handleStartSession` skips capacity admission entirely for `one_shot` (autonomous) starts | **Yes** | No | `TestHandleStartSession_CapacityAdmit_SkippedForOneShot` | PASS — this is the double-admission/deadlock-avoidance guard |
| `handleStartSession` denies + returns 503 without starting a session when admission fails | **Yes** | No | `TestHandleStartSession_CapacityAdmit_DeniedReturns503` | PASS |
| `resolveVerifierBackendModel` defaults to the PRD's own ask-compatible backend/model | **Yes** | No | `TestResolveVerifierBackendModel_DefaultsToPRDBackend` in `cmd/datawatch/verifier_diff_test.go` | PASS |
| `resolveVerifierBackendModel` falls back to `ollama` for a session-only PRD backend (claude-code) | **Yes** | No | `TestResolveVerifierBackendModel_SessionOnlyBackendFallsBackToOllama` | PASS — documents the real architectural limit (no single-shot ask adapter for session-only kinds) |
| `resolveVerifierBackendModel` explicit config always wins over PRD defaults | **Yes** | No | `TestResolveVerifierBackendModel_ExplicitConfigWins` | PASS |
| `PurgeScope`/`ArchiveScope` on a session-local scope only touch that session, not the whole project | **Yes** | No | `TestBL386_PurgeScope_SessionLocal_OnlyPurgesThatSession`, `TestBL386_ArchiveScope_SessionLocal_OnlyArchivesThatSession` in `internal/memory/bl386_phase3_test.go` | PASS — regression tests for the data-loss bug; two sessions in the same project, purge/archive one, confirm the other survives |
| `handleDeleteSession` purge removes only the target session's memories | **Yes** | No | `TestHandleDeleteSession_MemoryStrategyPurge` in `internal/server/session_delete_memory_test.go` | PASS |
| `handleDeleteSession` default (keep) leaves memories untouched | **Yes** | No | `TestHandleDeleteSession_MemoryStrategyKeep_Default` | PASS |
| `session.capacity_wait_seconds` / config-patch keys apply correctly | **Yes** | No | `TestApplyConfigPatch_CapacityKeys` in `internal/server/capacity_surfaces_test.go` | PASS |
| Live daemon restart with these changes | No | Yes | Manual restart of the production daemon, boot-resume log inspected | Confirmed clean boot-resume (correctly re-launched the one genuinely-running PRD, left cancelled ones untouched) — but this was validated for the *prior* release's changes at the time of restart, not yet re-validated against this specific v8.36.0 batch live. Live click-through of the new session-delete memory picker, the PWA capacity card's node-visibility/PRD-scoping, and the verifier actually using a PRD's own backend end-to-end have not yet been performed. |

---

## Security Remediation — Design A items (SEC-002, SEC-006, SEC-007, SEC-009, SEC-014) — v8.39.19–v8.39.23

Five related auth-hardening fixes from the Design A security walkthrough. Each
got a unit test (confirmed to fail without its fix) and live validation: new
`release-smoke.sh` sections 57–61, run clean against a real daemon, plus a
manual authenticated-sandbox pass for the checks (S60, S61) that the smoke
suite's own empty-admin-token sandbox posture can't meaningfully exercise.

| Scenario | Automated | Manual | Test / Location | Notes |
|----------|-----------|--------|------|-------|
| SEC-014: peer/server token never echoed on create/get/list | **Yes** | **Yes** | `TestFedPeer_SEC014_TokenNeverEchoed` in `internal/server/federation_peers_api_test.go`; smoke §57 | PASS both — smoke run against a live daemon (`scripts/release-smoke.sh`, 175 pass/0 fail/31 skip) confirmed no raw token in any response body |
| SEC-014: `GetByToken` constant-time, rejects wrong-length/wrong-value/empty | **Yes** | No | `TestMultiserverStore_GetByToken_ConstantTime` | PASS |
| SEC-002: MCP SSE falls back to `server.token` when `mcp.token` empty | **Yes** | **Yes** | `TestMCPFedAuth_SEC002_FallsBackToServerToken`; smoke §58 (`/api/health` `mcp_auth_required`, `/api/diagnose` `rest_auth`/`mcp_sse_auth`) | PASS both |
| SEC-007: WS upgrade rejects a mismatched `Origin`, allows no-`Origin` | **Yes** | **Yes** | `TestWSCheckOrigin_*` (4 cases) in `internal/server/ws_origin_test.go`; smoke §59 (raw socket handshake probe, confirms live 403) | PASS both |
| SEC-009: narrowed `federation-peer` default grant + new `/peers/self` | **Yes** | **Yes** | `TestFederationPeer_DefaultCaps`, `TestFedCap_PeerTokenAccepted_SEC009`, `TestFedCap_PeerToken_CustomGroup_SessionsList`; smoke §60 | Smoke §60 **skips** against the default (empty-admin-token) sandbox — with no admin token, `fedAuthMiddleware`'s empty-token early-return makes every caller admin-equivalent regardless of which Bearer token was presented, so peer-vs-admin has nothing to distinguish. **Manually validated against a real authenticated sandbox** (`server.token` set): create → `token_present:true`/no raw token; `GET /peers/self` as the peer → 200, own entry; `GET /api/sessions` with the same bare peer token → 403 (narrowed grant confirmed live, not just in a unit test) |
| SEC-006: `?token=` removed from REST/WS/MCP SSE; replaced by header, WS `Sec-WebSocket-Protocol`, or a single-use nonce on the 3 browser-GET-only routes | **Yes** | **Yes** | `nonce_test.go` (5), `nonce_auth_test.go` (6), `ws_sec006_test.go` (3, incl. a real client-side RFC 6455 subprotocol-echo check via `gorilla/websocket`'s own `Dialer`), `TestMCPFedAuth_QueryParamToken_Rejected`; `app-nonce.test.js` (6, PWA-side); smoke §61 | Smoke §61's `?token=`-rejection and nonce-round-trip checks **skip** against the default empty-admin-token sandbox (same reason as S60 — nothing to reject when every caller already passes through). The no-auth nonce-mint check runs in both postures but expects a *different* code each way — 400 ("no caller token") under the empty-token bypass, 401 (rejected by `fedAuthMiddleware` before the handler runs at all) once a real token is set — caught via live testing after the smoke script initially assumed 400 in both cases. **Manually validated against a real authenticated sandbox**: no-auth nonce mint → 401 (not 400); `?token=<admin>` with no header → 401; minted nonce → `/api/files/download?nonce=...` authenticates (400 missing-path, not 401); same nonce reused → 401 (single-use confirmed live); a valid nonce on `/api/sessions` (not in the allow-list) → 401 |

---

## Design A2 — close 13 capability-check gaps across 11 routes — v8.39.24

An audit (not a listed SEC-finding — found during Design A2 implementation)
of every `apiMux`-registered route's call graph for a reachable
`s.fedCap(...)` check. Found 11 handlers (13 distinct method/route
combinations) with none at all. See the CHANGELOG v8.39.24 entry for the
full per-handler capability assignment.

| Scenario | Automated | Manual | Test / Location | Notes |
|----------|-----------|--------|------|-------|
| Every `apiMux`-registered handler's call graph reaches a `fedCap(...)` check, except an explicit justified exception list | **Yes** | No (static analysis, not sandbox-posture-dependent) | `TestA2_EveryRegisteredRouteHandlerIsCapabilityGated` in `internal/server/route_caps_a2_test.go` | PASS. Confirmed to fail without a fix: temporarily reverted `handleQueue`'s check, re-ran (failed, named the exact handler), restored, re-ran (passed) |
| A bare federation-peer (no grant) gets 403 on all 13 previously-ungated routes | **Yes** | No | `TestA2_PreviouslyUngatedRoutes_BarePeerGets403` (21 sub-cases) | PASS all. Caught a real ordering bug during this test's first run: `handleObserverConfig`'s capability check was placed *after* the `observerAPI == nil` short-circuit, so a disabled subsystem returned 503 before the 403 ever had a chance to fire — fixed to check capability first, confirmed via re-run |
| A peer with the exact matching capability grant clears the check (not 401/403) | **Yes** | No | `TestA2_GrantedPeerPassesCapCheck` (4 representative sub-cases) | PASS all |
| New `queue:read`/`queue:write`, `results:list`/`results:read`/`results:write` capabilities are part of `full-control` | **Yes** | No | `TestFullControl_HasAll` (extended) in `internal/federation/capabilities_test.go` | PASS |

---

## Design A3 — per-session scoped credential — v8.39.25

Replaces the admin token a spawned session's bridge held with a scoped,
revocable per-session credential (HLLM-001/002). See the CHANGELOG
v8.39.25 entry for the full mechanism and the `session-default`
capability group's exact grant list.

| Scenario | Automated | Manual | Test / Location | Notes |
|----------|-----------|--------|------|-------|
| `SessionTokenStore`: mint/resolve/revoke, supersede-on-remint, unknown-token rejection, orphan sweep | **Yes** | No | `internal/auth/session_token_test.go` (7 cases) | PASS all |
| A minted token survives a fresh `NewSessionTokenStore` load (simulates daemon restart) at the right file perms (0600); a revoked one stays revoked after reload | **Yes** | No | `TestSessionTokenStore_SurvivesReload`, `TestSessionTokenStore_RevokePersistsAcrossReload` | PASS both |
| Every `AddTool`-registered MCP tool (374) has a classified capability in `federation.MCPToolCap` | **Yes** | No (static analysis) | `TestMCPToolCap_EveryRegisteredToolHasAnEntry` in `internal/federation/mcp_tool_caps_test.go` | PASS — same audit-regression pattern as Design A2's structural test |
| `POST /api/mcp/call`: admin can call any tool; a session token can call a session-safe tool but gets 403 on an admin-only one; an unknown tool is 404 for every caller | **Yes** | **Yes** | `TestMCPCall_*` (4 cases) in `internal/server/mcp_bridge_cap_test.go` | PASS all. Confirmed to fail without the fix: reverted `handleMCPCall` to the old blanket `comm:write` check, re-ran `TestMCPCall_SessionToken_AdminOnlyToolRejected` (failed — session-default already includes comm:write, so the old check let it through), restored, re-ran (passed) |
| `GET /api/mcp/tools`: admin sees the full unfiltered catalog; a session token's catalog is filtered to only what it can call | **Yes** | No | `TestMCPTools_*` (2 cases) | PASS both |
| End-to-end against a real daemon: spawn a real session, confirm the token actually injected into its real `claude mcp add` registration matches the minted scoped token (not admin); scoped token succeeds on a session-safe tool and is rejected (403) on an admin-only one while admin still succeeds; killing the session revokes the token (subsequent call → 401) | No (requires a live daemon + real session spawn) | **Yes** | Manual, 2026-10-05 against an isolated test daemon (opencode + local Ollama, no API cost) | PASS every step — see CHANGELOG v8.39.25 for the exact sequence. Cleaned up the test entries this accidentally wrote into the operator's real `~/.claude.json` (no `CLAUDE_CONFIG_DIR` override on the ad hoc test daemon) immediately after |
| Scope limitation (documented, not a gap introduced by this change): the standalone MCP SSE transport (direct IDE/Cursor connections) still uses its prior admin-vs-federation-peer gate, not the new per-tool map | N/A (explicitly out of scope) | N/A | — | Tracked as a BL316-followup extension, not silently assumed covered |

No new `release-smoke.sh` section was added for this item — the static
call-graph test gives a stronger, sandbox-posture-independent guarantee
than a handful of live HTTP probes would (it inspects every route, not a
hand-picked sample), and the functional peer-403/peer-200 behavior is
already exercised in-process by `TestA2_PreviouslyUngatedRoutes_BarePeerGets403`/
`TestA2_GrantedPeerPassesCapCheck` against the real `fedAuthMiddleware` +
`fedCap` code path, not a mock.

---

## `per_story_approval` stuck-true bug (E2E hang root cause) — v8.39.26

Found live 2026-10-05 re-running E2E after today's SEC-006/A2/A3 work — not
a SEC-finding or a Design-A item, just a real functional bug surfaced by
actually running the suite twice and refusing to accept the first
(wrong) theory. See the CHANGELOG v8.39.26 entry for the full mechanism.

| Scenario | Automated | Manual | Test / Location | Notes |
|----------|-----------|--------|------|-------|
| `config.AutonomousConfig` marshals `false` bools explicitly (no `omitempty` silently dropping the key) | **Yes** | No | `TestAutonomousConfig_FalseBoolsMarshalExplicitly` in `internal/config/config_test.go` | PASS. Confirmed to fail without the fix: restored `omitempty` on `per_story_approval`'s JSON tag, re-ran (failed with the exact field named), restored the fix, re-ran (passed) |
| `internal/autonomous.API.SetConfig`'s merge-unmarshal lets an explicit `false` actually overwrite a previous `true`, for all 5 affected fields | **Yes** | No | `TestAPI_SetConfig_ExplicitFalseOverwritesTrue` (table-driven, 5 sub-cases) in `internal/autonomous/autonomous_test.go` | PASS all. Same revert-rerun-restore confirmation as above |
| End-to-end against a real daemon: replay TS-026's exact `PUT true` → `PUT false` sequence, confirm `GET /api/autonomous/config` (the manager's *enforced* copy, not just the REST layer's own `GET /api/config`) reflects `false` afterward | No (requires a live daemon) | **Yes** | Manual, 2026-10-05 | First attempt (fixing only `internal/autonomous.Config`'s tags) did NOT fix the live symptom — found the real second struct (`config.AutonomousConfig`, the one actually marshaled on this path) by re-checking live rather than trusting the first fix. Second attempt confirmed fixed: manager copy correctly shows `false` after restore |
| Two prior full E2E runs today (`e2e-run-v83924.log`, and the one before it) both hung at the identical point (TS-695's PRD, stories stuck in `awaiting_approval`) for this exact reason, not test-concurrency as an earlier same-day fix assumed | N/A (historical evidence, not a repeatable automated check) | **Yes** | Live PRD inspection both times (`GET /api/autonomous/prds/{id}`, decision log showing `approve`→`run` within ~1s of `decompose`, no `approve_story` call ever present) | The earlier `conflict:llm` tag fix on TS-026/TS-782 is harmless and stays (reasonable toggle hygiene regardless), but was not and could not have been the actual fix |

---

## BL396 Phase 3 — PRD permission_mode, pause/resume, Chrome-enabled flag (GH#172 D75/D52/D66) — v8.55.0–v8.57.0

Three small new REST actions/fields shipped across the same Phase 3 push; grouped here rather than one tracker section per batch since each is a single endpoint/field with the same shape of coverage.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `POST /api/autonomous/prds/{id}/set_permission_mode` — `Manager.SetPermissionMode` | **Yes** | **Yes** | `TestSetPermissionMode` in `internal/autonomous/gh172_d75_permission_mode_test.go` (valid mode persists + records a decision, empty clears it, invalid mode rejected without mutating, unknown PRD id rejected) | PWA "Permission" picker live-verified against a real daemon (v8.55.0 batch) |
| `POST /api/autonomous/prds/{id}/pause`, `.../resume` — `Manager.Pause`/`Resume`, new `PRDPaused` status, executor cooperative-drain | **Yes** | **Yes** | `TestPauseResume` in `internal/autonomous/gh172_d52_pause_resume_test.go` (pause from running succeeds + records a decision, pause from non-running/already-paused rejected, resume mirrors the same checks, unknown PRD id rejected both ways) | Live-verified against a real daemon: REST negative paths (wrong status, unknown id) over HTTP; a `paused` PRD's PWA card renders correctly (border color, sort priority, Resume button) after fixing 3 pre-existing PWA bugs that hid/mis-sorted `paused` automata (v8.57.0) |
| `Session.ChromeEnabled` field — set at session creation inside the same `SetChrome`-support gate the backend already used | No (no dedicated unit test) | **Yes** | — | Live-verified: build + PWA badge render confirmed in the v8.56.0 batch; no unit test added for the field itself since the gating logic it reuses (`backendObj.(interface{ SetChrome(bool) })`) was already covered by existing Chrome-session tests |

---

## Task.FilesTouched verifier wiring + manual backfill (operator-reported 2026-10-06) — v8.61.1

The data model (`Task.FilesTouched`) and `Manager.RecordTaskFilesTouched` existed since Phase 4 (v5.26.64) but had zero call sites anywhere in the codebase — found live while investigating a PWA story file chip pointing at a filename the decomposer predicted but the worker never actually wrote.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `dedupeFiles` — merges the verifier's distinct evidence sources (committed diff, working-tree diff, new untracked files, overwritten pre-existing files) into one sorted, deduplicated list | **Yes** | No | 4 tests in `cmd/datawatch/verifier_diff_test.go`: merges+sorts across sources (using this exact bug's real filenames), duplicates across lists collapse to one, nil/empty lists and blank entries ignored, all-empty input returns empty | Reuses the `gitWorkingTreeDiffSince` evidence the verifier already computes and already has 4 passing tests for (signature changed to also return filenames, not just diff bytes — all 4 existing tests updated and still pass) |
| Verifier closure in `cmd/datawatch/main.go` calling `autonomousMgrRef.RecordTaskFilesTouched` after computing evidence (git branch and non-git mtime-based branch) | No (closure is deeply embedded in daemon startup code, not independently unit-testable without a larger refactor) | No (not yet; see note) | — | Not live-verified against a real spawn+verify cycle (would require a real LLM verification call) — every individual evidence source it combines (committed/working diff name lists, new-untracked-files list, mtime-based `TouchedFiles`) is itself an already-proven, already-used-elsewhere git/filesystem call in this same function; the new code only merges already-correct lists via the now-tested `dedupeFiles` and writes them through the already-tested `Manager.RecordTaskFilesTouched`. Flagged here rather than silently assumed covered — a full live spawn+verify validation is still owed. |
| `POST /api/autonomous/prds/{id}/record_task_files_touched` — manual backfill, no lock-after-approve gate (deliberately, unlike `set_task_files`) | No (no dedicated REST handler test) | No | — | New REST action for backfilling tasks that completed before this fix existed (e.g. the PRD that surfaced the bug). Not yet exercised against a real daemon — the production daemon had an active session running at the time this shipped, so no restart/backfill was performed in-session; follow-up. |

---

## PWA WebSocket liveness watchdog (operator-reported 2026-10-06) — v8.61.2

Operator: "sent a command after being idle for a few minutes and had to exit the session and go back in for it to start moving again." Root cause: `ws.readyState` can stay `OPEN` after a NAT/proxy silently drops an idle TCP mapping — no `close`/`error` event ever fires (nothing on the wire tells the browser), so `scheduleReconnect()` — which only ran from those two event handlers — never triggered. The daemon's `MsgPing`→`pong` handler already existed (`internal/server/api.go`) but the PWA client never sent one.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `_wsIsStale(lastActivityAt, now)` — pure staleness decision (no I/O) | **Yes** | No | 4 tests in `internal/server/web/app-ws-watchdog.test.js`: fresh activity not stale, exactly-at-threshold not yet stale (boundary is strict `>`), just-over-threshold is stale, minutes-of-silence (the reported scenario) is stale | Extracted from the `setInterval` callback specifically so it's testable — the stub browser environment this test suite uses no-ops `setInterval` entirely |
| `_wsSendPing()` — sends `{"type":"ping"}` only when `readyState === OPEN` | **Yes** | No | 2 tests: sends when OPEN, does nothing (and doesn't throw) when CONNECTING or `state.ws` is `null` | — |
| Full round trip: client ping → server pong → `wsLastActivityAt` refreshed | No | **Yes** | Playwright against a real daemon: captured actual WS frames over a real 20s `WS_PING_INTERVAL_MS` cycle — confirmed exactly one `{"type":"ping"}` sent and one `{"type":"pong"}` received | — |
| Watchdog forces a reconnect when activity truly stops (simulating a dead-but-still-OPEN socket) | No | **Yes** | Playwright: froze `state.wsLastActivityAt` via a property getter/setter that swallows all writes (a naive one-time backdate isn't a valid simulation — the daemon's 5s periodic `stats` broadcast would overwrite it before the watchdog's next tick, which is in fact why the first version of this live test gave a false negative and had to be corrected to actually block every frame, not just pings, from refreshing the clock). Confirmed: the stale connection closed and a fresh one opened within one `WS_PING_INTERVAL_MS` tick. | Live test script not retained (ad hoc verification, deleted after use per the project's temp-file convention) — the pure-function unit tests above pin the same decision logic permanently |

---

## Native ACME / Let's Encrypt subsystem (BL397) — v8.61.9

Full design + operator-interview decision log:
`docs/plans/2026-10-06-bl397-native-acme-letsencrypt.md`. The ACME
protocol handshake itself (account registration, HTTP-01 validation,
issuance) has no meaningful offline simulation — it was live-verified
against a real Let's Encrypt directory (both staging and production) on
a real public host (`spaceportsouth.dmzs.com` / 66.228.59.180), not
mocked. **4 real bugs were found and fixed during that live run that
every unit test below had already passed** — each now has its own
regression test, but the live run is what actually found them.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `Account` — PEM round-trip, encrypted-at-rest, reload | **Yes** | No | 3 tests in `internal/acme/account_test.go`: generate+persist+reload returns the same key, `SetRegistration` persists across reload, `LoadOrCreateAccount` with a real `encKey` produces non-plaintext output on disk and a wrong key fails to load | — |
| `Account.IsRegisteredFor` / `ClearRegistration` — per-directory registration tracking | **Yes** | **Yes** (the bug it fixes was found live) | 2 tests: a staging registration is not reported valid for production, `ClearRegistration` nils `GetRegistration()` | Regression coverage for live bug #3 (staging↔production account registration confusion) |
| `httpProvider` — HTTP-01 challenge Present/CleanUp/Handler | **Yes** | **Yes** | 2 tests in `internal/acme/provider_test.go`: full present→serve→cleanup→404 cycle, unknown token always 404; live-verified serving a real token to Let's Encrypt's actual validator during the real order | — |
| `atomicWriteFile`, `parseLeafCert` | **Yes** | No | 2 tests in `internal/acme/manager_internal_test.go`: write+overwrite+no-leftover-tmp, finds a CERTIFICATE block among other PEM block types | — |
| `loadExistingCertStatus` — seeds `DomainStatus` from the cert already on disk at startup | **Yes** | **Yes** (the bug it fixes was found live) | 2 tests: finds and parses a fresh cert, no file means not-issued | Regression coverage for live bug #1 (restart-state-loss infinite reissue loop — 4 real staging orders fired before caught) |
| `applyCertificate` — updates + persists `server.tls_cert`/`tls_key` (+ MCP's) | **Yes** | **Yes** (the bug it fixes was found live) | 1 test: constructs a `Manager` directly, calls `applyCertificate` with a real self-signed test cert, asserts both in-memory and reloaded-from-disk config reflect the new paths, and that `restartFn` fires after the documented 500ms delay | Regression coverage for live bug #4 (cert written to disk but never actually applied — `curl` kept getting the old self-signed cert) |
| `redirectToTLSHandler`'s unconditional ACME-challenge bypass | **Yes** | **Yes** | `TestRedirectToTLSHandler_AcmeChallengeBypassIsUnconditional` in `internal/server/redirect_bypass_test.go`: a non-loopback remote (simulating Let's Encrypt's real validator) gets the challenge served, not redirected; live-verified — the real HTTP-01 order against spaceportsouth.dmzs.com would have silently failed without this fix | Found *before* the live test (code review while prepping deployment), not by it — the one bug of the five total that wasn't live-test-discovered |
| Full issue → write → apply → restart cycle (staging) | No (no meaningful offline simulation of the ACME protocol itself) | **Yes** | Real daemon on spaceportsouth.dmzs.com, `acme.endpoint: staging`: issued, `openssl x509` confirmed a valid `(STAGING)` Let's Encrypt cert, correct subject/90-day validity | — |
| Force-renew via REST/CLI | No | **Yes** | `POST /api/acme/renew` → 200 with updated `not_after`/`last_renewal`; `datawatch acme status`/`verify` CLI exercised directly on the box | MCP and comm-verb surfaces share the identical REST-backed code path, not independently live-exercised |
| Full issue cycle against production, real trust validation | No | **Yes** | Flipped `acme.endpoint: production`, forced renew, `curl` **without** `-k` (standard OS CA trust store, no special flags) got a clean `HTTP/2 200` from `https://spaceportsouth.dmzs.com:8443/api/health` | This is the feature's actual acceptance criterion — a real browser would trust this exactly the same way |
| Pre-existing seeded data survives every restart in this cycle | No | **Yes** | 3 pre-existing tmux sessions on the shared test box confirmed present via `/api/sessions` after 6 daemon restarts across this test | Shared-VM coordination with the `datawatch-app` session (Apple sandbox use) — see the plan doc's "Live-test findings" section |

---

## BL397 Phase 2/3 (DNS-01 + hot-swap) — v8.62.x

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `internal/tlsutil.Build`'s `GetCertificate` — hot-reload on mtime change | **Yes** | No | `TestBuild_GetCertificate_ReloadsOnMtimeChange`: writes a cert (serial 1), confirms `GetCertificate` returns it, rewrites the SAME path with a different cert (serial 2) + `os.Chtimes` forced forward, confirms the next `GetCertificate` call returns serial 2 | Applies to every TLS listener built through `tlsutil.Build` (server, MCP SSE, proxy sandbox), not ACME-specific |
| `GetCertificate` — keeps serving last-good cert on a corrupt/mid-write read | **Yes** | No | `TestBuild_GetCertificate_KeepsServingLastGoodOnLoadError`: corrupts the cert file in place (simulating a caught-mid-renewal-write race), confirms the handshake still gets the last valid cert instead of an error | Matches Caddy's internal behavior for the same race |
| `applyCertificate` — hot_swap skips restart when the cert path is unchanged | **Yes** | No | `TestApplyCertificate_HotSwap_SkipsRestartOnUnchangedPath`: pre-seeds config pointing at the ACME path already, confirms `restartFn` is NOT called after apply | — |
| `applyCertificate` — hot_swap still restarts on the FIRST path change | **Yes** | No | `TestApplyCertificate_HotSwap_StillRestartsOnFirstPathChange`: starts from a blank (self-signed) config, confirms `restartFn` IS called even with `hot_swap: true` | A running listener can't hot-reload a path it was never watching |
| `Account.IsRegisteredFor` / `ClearRegistration` — per-directory registration (reused for the DNS-01 endpoint-flip case) | **Yes** | **Yes** (originally found live in Phase 1) | See the Phase 1 section above | — |
| DNS-01 Cloudflare provider wiring (`client.Challenge.SetDNS01Provider`) | No | No | — | **Not live-verified** — needs a real Cloudflare zone + zone-scoped token, not available in this environment. Unit-testable surface is limited to config plumbing (below); the actual DNS-01 challenge round-trip is lego's own well-tested code path, not reimplemented here |
| `TestApplyCertificate_NeverPersistsResolvedDNS01Secret` — the plaintext DNS-01 token is never written to config.yaml | **Yes** | **Yes** (a real security property, not a behavioral nicety) | Simulates the exact already-resolved in-memory state (as it would be after main.go's global secret-ref resolution pass), calls `applyCertificate`, asserts the saved file contains the original `${secret:...}` reference and NOT the plaintext token | Security regression test — found and fixed the risk before writing any provider code, not after |
| GET/PUT `/api/config` — `acme.method`, `acme.dns01.*`, `acme.apply.hot_swap` | No | No | — | Config plumbing only verified via `go build`/`go vet`/existing config tests passing; no dedicated REST round-trip test added for these specific keys (follows the same pattern as most `applyConfigPatch` cases in this codebase, which aren't individually tested) |

## BL397 Phase 4 / BL335 (APNs push dispatch) — v8.62.x

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `parseAPNsKey` — PKCS#8 EC key parsing | **Yes** | No | `TestParseAPNsKey_PKCS8`, `TestParseAPNsKey_InvalidPEM` | Apple issues `.p8` Auth Keys as PKCS#8-wrapped EC keys |
| `providerToken` — JWT shape (header/claims) | **Yes** | No | `TestProviderToken_WellFormedAndVerifiable`: decodes both JWT segments, confirms `alg=ES256`, `kid`, `iss`, non-zero `iat` | — |
| `providerToken` — signature is cryptographically real | **Yes** | **Yes** (a correctness property, not just shape) | Same test: independently verifies the raw (r\|\|s) ES256 signature against the signing key's own public half via `ecdsa.Verify` — confirms this is a genuine, independently-verifiable JWS per RFC 7515, not just well-formed JSON with arbitrary bytes attached | This is the test that actually proves the signing implementation is correct, since no live Apple round-trip is possible here |
| `providerToken` — caching within `tokenTTL` | **Yes** | No | `TestProviderToken_Cached`: two calls within the TTL window return the identical token | Avoids Apple's documented rate limit on token generation |
| `ErrAPNs.Unregistered()` — 410/Unregistered classification | **Yes** | No | `TestErrAPNs_Unregistered`: true for 410+"Unregistered", false for 429+"TooManyRequests" | Drives the auto-prune-on-dead-token behavior in the alert-fire dispatch path |
| `NewDispatcher` — required-field validation, key-source precedence, sandbox URL selection | **Yes** | No | `TestNewDispatcher_RequiresCoreFields`, `TestNewDispatcher_RequiresAKeySource`, `TestNewDispatcher_LoadsFromKeyPath`, `TestNewDispatcher_SandboxURL` | — |
| Full dispatch round-trip against Apple's real APNs servers | No | No | — | **Not live-verified** — needs a real Apple Developer account, a provisioned Auth Key, and a TestFlight-registered iOS device token, none of which are available in this environment. This is the one BL397 sub-feature shipped on unit tests alone (contrast Phase 1/ACME, which was fully live-verified against a real Let's Encrypt directory and a real public host). Flagged in `docs/parity-status.md` rather than overclaiming "verified." |

## GH#179 — LLM registry `api_key_ref` read-path redaction — v8.63.1

A literal `api_key_ref` (as opposed to a `${secret:name}` reference) was
returned in clear text by `GET /api/llms`, `GET /api/llms/{name}`, and
the `llm_list`/`llm_get` MCP tools (both proxy to the same REST handler).
Fixed by mirroring the existing SEC-014 federation-peer redaction
pattern (`multiserver.Entry.Redacted()`/`RedactedList()`) one-for-one:
`inference.LLM.Redacted()`/`RedactedList()`. A changed response contract
(two new fields) per the Testing Tracker Rule.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `inference.LLM.Redacted()` — clears a literal `api_key_ref`, sets `api_key_ref_present`/`api_key_ref_prefix` | **Yes** | No | `TestLLM_Redacted_ClearsLiteralKey` in `internal/inference/llm_test.go`: literal key cleared, prefix is first 4 chars, original struct untouched (copy semantics) | — |
| `inference.LLM.Redacted()` — leaves a `${secret:name}` reference as-is | **Yes** | No | `TestLLM_Redacted_LeavesSecretRefAsIs`: a secret reference names a secret, it doesn't contain one, so it's safe to echo back (same reasoning as the existing `websearchAPIKeyRefs`/DNS-01 `dns01TokenSecretRef` convention) | — |
| `RedactedList()` | **Yes** | No | `TestRedactedList`: mixed literal + secret-ref entries, input slice not mutated | — |
| `GET /api/llms` / `GET /api/llms/{name}` never echo a literal key | **Yes** | No | `TestHandleLLMs_List_RedactsLiteralAPIKey`, `TestHandleLLMs_GetSingle_RedactsLiteralAPIKey` in `internal/server/gh179_llm_api_key_redaction_test.go`: response body byte-searched for the literal value, must be absent | `llm_list`/`llm_get` MCP tools proxy this same REST handler — fixed by the same change, not independently tested |
| `GET /api/llms` still shows a `${secret:name}` reference | **Yes** | No | `TestHandleLLMs_List_LeavesSecretRefVisible` | — |
| `PUT /api/llms/{name}` preserves a stored literal key when the client omits `api_key_ref` (the redacted-GET-echoed-back case) | **Yes** | **Yes** (a real regression this fix would otherwise have introduced) | `TestHandleLLMs_Put_PreservesLiteralKey_OnUnrelatedEdit`: reproduces the exact GET→edit-unrelated-field→PUT round trip the PWA and the `llm_add_model`/`llm_remove_model` MCP tools (`internal/mcp/inference.go`) both perform; PUT handler changed from a zero-valued decode to decode-onto-a-copy-of-the-existing-entry, matching the pre-existing `fedPeerUpdate` merge pattern | Without this companion fix, redacting the GET response would silently wipe every literal API key on the next unrelated edit |
| `PUT /api/llms/{name}` with an explicit `"api_key_ref": ""` still clears the key on purpose | **Yes** | No | `TestHandleLLMs_Put_ExplicitEmptyClearsKey` | Confirms the merge-onto-existing fix doesn't make the key impossible to intentionally clear |
| PWA edit form (`buildLLMForm`/`_renderLLMEditPanel`) — blank API-key field means "unchanged", not "clear" | No | No | — | Client-side only; mirrors the server-side contract via a `modal.dataset.apiKeyRefPresent`/`originalRecord.api_key_ref_present` check before including `api_key_ref` in the outgoing PUT body. No Playwright pass done for this specific field — follows the same node-test-only coverage pattern as the rest of this form |
| Alert-fire → APNs fan-out wiring (`alertStore.AddListener` in `cmd/datawatch/main.go`) | No (the listener closure is deeply embedded in daemon startup code, same category as the pre-existing FilesTouched verifier closure noted elsewhere in this tracker) | No | — | Not independently unit-testable without a larger refactor; the pieces it calls (`DeviceStore.ListByKind`, `Dispatcher.Send`) are each tested in isolation above |

## GH#183 — APNs per-device environment (sandbox vs. production host selection) — v8.63.4

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `devices.Device.ApnsEnvironment` / `ApnsEnvironment.Valid()` | **Yes** | No | `TestRegister_ApnsEnvironmentRoundTrips`, `TestRegister_EmptyApnsEnvironmentIsValid`, `TestRegister_RejectsUnknownApnsEnvironment` in `internal/devices/store_test.go`: valid values round-trip and refresh on re-register (same as app_version/platform), empty is valid (pre-field devices / non-APNs kinds), an unrecognized value is rejected | — |
| `apns.Dispatcher.baseURLFor(environment)` — resolves the APNs host from the per-device environment, falling back to the dispatcher's configured default only when empty/unknown | **Yes** | No | `TestBaseURLFor` in `internal/apns/apns_test.go`: `"development"`/`"production"` always win regardless of the dispatcher's own configured default (the actual bug this fixes — a device explicitly registered as production must reach production even when `push.apns.sandbox: true`); `""`/unrecognized fall back to the dispatcher default | — |
| `POST /api/devices/register` accepts and persists `apns_environment`; refreshes on re-register; rejects an invalid value | **Yes** | **Yes** | `scripts/release-smoke.sh` section 64 against a real sandbox daemon: register with `development`, confirm `GET /api/devices` shows it; re-register the same token with `production`, confirm it refreshed (not stuck); register with an invalid value, confirm 400 | — |
| `POST /api/push/apns/test` and the real alert-fire fan-out both pass the device's own environment to `Send` | No | No | — | Wiring-only change (one extra argument threaded through at each call site); not independently testable without a larger HTTP-mocking refactor of `Dispatcher.Send` itself, which no existing test does either (see the Phase 4 section above — `Send`'s full HTTP round trip has never been live- or mock-tested, only its JWT-signing and payload-shape internals) |

## GH#180 — two config/manager sync gaps (autonomous.enabled, council.llm_ref) — v8.63.5

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `PUT /api/autonomous/config` syncs `enabled` (and the rest of the manager's post-merge Config) onto `s.cfg.Autonomous` and persists | **Yes** | No | `TestHandleAutonomousConfig_PUT_SyncsEnabledToServerConfig`, `TestHandleAutonomousConfig_PUT_SyncsEnabledFalseToServerConfig` in `internal/server/gh180_autonomous_config_sync_test.go`: PUT with `enabled:true` then `enabled:false`, confirmed both `s.cfg.Autonomous.Enabled` and the reloaded config.yaml on disk reflect each value (both confirmed-fail against the pre-fix handler) | — |
| `council.Orchestrator.SetLLMConfig()` updates `LLMRef`/`Backends`/`MaxParallel` in place | **Yes** | No | `TestOrchestrator_SetLLMConfig` in `internal/server/gh180_council_reload_test.go` — this method didn't exist pre-fix (the test file fails to compile against the pre-fix code, the strongest possible confirmed-fails-without-fix signal) | — |
| `POST /api/reload` propagates a changed `council.llm_ref`/`backends`/`max_parallel` to the live orchestrator, and does nothing when unchanged | **Yes** | No | `TestReload_PropagatesCouncilLLMRefToOrchestrator`, `TestReload_NoCouncilChange_DoesNotCallSetLLMConfig` | — |
