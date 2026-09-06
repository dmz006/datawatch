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
| PWA 📷 button → upload → preview → send `[image:<path>]` | No | No | — | E2E test needed: headless browser clicks attach, selects fixture image, confirms thumbnail, sends message, confirms `[image:...]` in channel history |
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
| MCP `vision_describe` tool | No | No | — | MCP tool wires directly to the same HTTP handler; no standalone unit test. Requires a running daemon with vision enabled. |
| Router image injection (comms → `msg.Text`) | Yes | No | `internal/router/bl368_vision_test.go` (5 tests: CmdRemember injection, plain text, non-command regression) | Verifies `Parse()` still recognises `remember:` after image description is injected. Live test with an actual image attachment via SMS or Matrix not yet performed. |
| `AcceptsImages` manifest field (skills) | Yes | No | `internal/skills/manifest_test.go` (4 tests: true/false/default/no-extra-leak) | Manifest parsing verified. Live test with a skill that declares `accepts_images: true` receiving an image context not yet performed. |
| Council `image_path` field (`POST /api/council/run`) | No | No | — | Wired in `internal/server/council.go`; no dedicated unit test for the image injection path. Requires a running daemon with vision enabled and a valid local file path. |

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
| Data-boundary tags in `decomposeFn` prompt | No | No | — | Verified by code inspection; prompt wraps `req.Spec` in `<user_data>` with preamble. Live test requires decompose call to a running LLM. |
| Security preamble in `autonomousVerify` prompt | No | No | — | Verified by code inspection; preamble prepended to specPart+diffSection. |
| Security preamble + tags in `autonomousGuardrail` prompt | No | No | — | Verified by code inspection; UnitTitle and UnitSpec wrapped. |

## PWA Session-List Select-All (v8.19.2)

Fixed: select-all / select-none scoped to filtered visible sessions. Counter, selection set, and bulk-delete all honour the active chip + backend filter + text search + history toggle. Filter changes clear selection.

| Test case | Tested | Live-validated | Coverage details | Notes |
|---|---|---|---|---|
| Select-all uses `_visibleDone` (filtered done sessions) not `state.sessions` | No | No | Covered by code inspection; PWA JS — no automated unit test harness | Live test: PWA → filter to "failed" → click Select All → verify counter matches filtered count and not total |
| Filter change clears active selection (chip, backend, text, clear button) | No | No | Code inspection of 4 call sites in app.js | Live test: select sessions → change state chip → verify selection clears |
| Toggle "None" deselects only visible filtered set | No | No | Code inspection | Live test: select all filtered → click again → verify deselected only filtered, not unrelated |

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
| PWA: upload image → attach → send → vision runs before session receives | No | No | — | Live test: PWA session → 📎 attach image → type prompt → send → check session input shows `[image: <desc> \| path: ...]` not raw path |
| WebSocket path (WS send_input) also runs expandImageTags | No | No | Code inspection of `executeCommand` CmdSend branch | Live test: send via WebSocket with image tag |
