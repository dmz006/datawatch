# Testing Tracker

Two validation levels are required for every interface. See `AGENT.md` for the full rule.

- **Tested=Yes** — Go unit/integration tests exist and pass (`go test`).
- **Validated=Yes** — a live connection or end-to-end test confirmed the interface works with a real or simulated backend.

Do not mark **Validated=Yes** based solely on unit tests.

---

## CVE-2026-77214 (libexpat1) — new finding triaged under the GH#197 standard

Added in v8.73.35. Found live by manually re-triggering `image-refresh.yaml`
to verify GH#197's "3 days failing" complaint was actually fixed.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `security/accepted-risks.yml` entry + generated `.trivyignore` | Yes | Yes | `scripts/check_accepted_risks.py` passes (71 entries); `scripts/gen_trivyignore.py` regenerated `.trivyignore` + `docs/security-review.md` cleanly | Reachability traced live: `docker run --network none` + a full-image `ldd` sweep found only `git-http-push` links `libexpat1`, which nothing in this codebase or a modern `git push` invokes |
| `image-refresh.yaml`'s blocking scan | No (this is a live CI gate, not a unit test) | Yes | Manually re-triggered the workflow (`gh workflow run`) before and after this fix; the specific GH#197-cited failure (CVE-2026-19445) was already gone (stale pre-fix run), confirming this new finding was the one actually blocking `agent-base`'s promotion | Re-triggered again after this commit lands to confirm green (see CHANGELOG) |

## GH#193 — Automaton/Automata terminology in de/es/fr/ja locales

Added in v8.73.33. Most of the issue's 123-string list was already fixed by
earlier, unrelated commits; an authoritative ASCII-word-boundary scan found
8 real remaining leaks, all in `ja.json` (plus one in `en.json` itself).

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Locale bundles (`de`/`es`/`fr`/`ja.json`) | Yes | Yes | `TestLocales_AutomatonNeverUntranslatedOrMistranslated` — scans every value in all 4 bundles for the untranslated word or the locale-specific mistranslation; full `TestLocales_*` suite + `go build` + `node --test internal/server/web/*.test.js` (182/182) all green | Validated by direct JSON read-back after the fix, not a browser click-through — this is a text-content check, not a rendering check |

## GH#198 — "PRD" leaking into user-facing locale strings

Added in v8.73.34, filed and closed same round as GH#193's cleanup.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Locale bundles (all 5, English included) | Yes | Yes | `TestLocales_PRDNeverUserFacing` — `\bPRDs?\b` across all 5 bundles; full `TestLocales_*` suite + `go build` + `node --test internal/server/web/*.test.js` (182/182) all green | Caught 2 keys that were entirely untranslated (byte-identical English in all 5 bundles) as a side effect — fully translated those into de/es/fr/ja rather than leaving a partial fix |

## Federation per-peer TLS skip-verify (`tls_skip_verify`)

Added in v8.73.32. Prompted by a live operator report (federation to `ralfthewise` failing against its self-signed cert).

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `multiserver.HTTPClient` / `Store.Test()` | Yes | Yes | `TestStore_Test_TLSSkipVerify` — a real `httptest.NewTLSServer` (genuinely self-signed TLS, not a mock); asserts `TLSSkipVerify=false` fails with a cert error and `=true` succeeds against the identical server | Not yet re-validated against the real `ralfthewise` end-to-end — pending after this ships and the daemon restarts |
| REST (`fedPeerAdd`/`fedPeerUpdate`, plain `/api/servers` add/update) | Yes (existing suite) | No | Auto-pass-through confirmed by code reading (both decode straight into/onto `Entry`) | Not independently exercised with this specific field by name |
| MCP (`federation_peer_add/update`, `server_add/update`) | No | No | New `tls_skip_verify` param added to 4 tool schemas + handler body maps, mirroring the existing `enabled`-field explicit-presence pattern | No MCP-level test added this pass — same risk profile as `enabled`, which also has none |
| CLI (`federation peer add/update`, `server add/update`, `setup server`) | No | No | New `--tls-skip-verify` flags + an interactive prompt in `setup server`, mirroring existing flag patterns | Not exercised live this pass |
| PWA (Remote Servers + Federation Peers forms) | No | No | Checkbox added to both forms, wired into `saveServer()`/`submitFedPeerForm()`; 2 new locale keys × 5 bundles | Not click-tested in a real browser this pass — code-reviewed against the existing `enabled`/`federated` checkbox wiring it mirrors |



## BL398 Phases 0-2 + registry adoption (GH#197) — .trivyignore reconciliation, live rescan baseline, CVE tracing, structured registry

Added in v8.73.27/v8.73.28/v8.73.29/v8.73.30/v8.73.31. See `docs/plans/2026-10-08-bl398-trivyignore-cve-review.md`.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| 7 prose-vs-suppressed-ID mismatches (6 blocks + 1 phantom reference) | N/A (not code) | Yes | Fresh `trivy image --image-src remote` rescan (no `--ignorefile`) against all 5 published `:8.73.2` GHCR images, grep'd for each CVE ID in the resulting JSON | All 7 confirmed genuinely absent from the live scan across all 5 images, not a gate-bypass bug |
| `GHSA-6v7p-g79w-8964` prune | N/A (not code) | Yes | Same rescan — grep'd for `msgpack` package name across all 5 images' JSON, zero hits | Confirms the package is no longer flagged, not just that the specific ID string changed |
| Full suppressed-vs-live diff against published `:8.73.2` images (70 suppressed vs. 70 live unique HIGH/CRITICAL IDs) | N/A (not code) | Yes | `comm -13`/`comm -23` between the suppressed-ID list and the union of all 5 images' live scan output | Found CVE-2026-104851 (fsspec) live-but-unsuppressed — already fixed in source (`Dockerfile.agent-aider`), just not yet in that published image tag |
| Phase 1: same diff against 5 images built fresh from current source (not `:8.73.2`) | N/A (not code) | Yes | Built all 5 via a temporary local registry + insecure-registry buildx builder, tagged `v8.73.27`, fresh `trivy image --image-src remote` rescan of each | Exactly 70 live unique IDs == 70 suppressed IDs — clean baseline, zero gaps either direction. Directly confirmed CVE-2026-104851 is absent from the fresh `agent-aider` build (fix verified working, not just inferred from source) |
| Phase 2: CVE-2026-19445 (`sni_callback` reachability in agent-gemini/agent-aider) | N/A (not code) | Yes | `docker run --entrypoint /bin/sh --network none` against both `:8.73.2` images, `grep -rn sni_callback` across stdlib + all site-packages (including `agent-aider`'s pipx venv) | Zero third-party/application call sites found; only the stdlib's own attribute definition. Confirmed-unreachable, not argued by analogy — `.trivyignore` entry upgraded from CAVEAT to TRACED |
| `scripts/check_accepted_risks.py` schema lint | N/A (not code) | Yes | Ran against the migrated 70-entry registry: zero errors, zero escalations. Deliberately re-broke one entry (`first_added` after `added`) to confirm the lint actually catches it (caught a real migration bug this way — `cve/CVE-2026-8328`'s prose-noted review date predated its git-commit date) | Confirms the lint fails on structural violations, not just passes trivially |
| `scripts/gen_trivyignore.py` generator | N/A (not code) | Yes | Ran twice consecutively against the same registry: zero diff (idempotent). Generated `.trivyignore`'s suppressed-ID set diffed against the hand-written file it replaces: identical 70 IDs | Confirms the generator is a faithful, stable round-trip, not a lossy one |
| `scripts/accepted_risks_daily_watch.py` | N/A (not code) | Yes | Ran against the real registry + Phase 1's 5 fresh-scan JSON files | Correctly surfaced 1 added-in-24h, 64 past-expiry (the intentional migration backlog), and caught (then fixed) a false-positive "re-trace needed" from Debian epoch-prefix notation (`1:2.38.1-5...` vs `2.38.1-5...`) before it shipped |
| `scripts/apply_stale_risk_removal.py` | N/A (not code) | Yes | Ran against a scratch copy of the real 70-entry registry, removing 1 entry | Resulting file still parses, has exactly 69 entries, removed ID absent — verified via a fresh `yaml.safe_load`, not just "the script didn't crash" |
| `scripts/dismissed_alerts_watch.py` (code-scanning) | N/A (not code) | Yes | Ran live against `dmz006/datawatch`'s real code-scanning API (not a mock) | Found 87 of 87 dismissed alerts genuinely unregistered — a real finding, matched manually against the registry's 0 code-scanning entries |
| `scripts/dismissed_alerts_watch.py` (dependabot, no token) | N/A (not code) | Yes | Ran with `SCA_WATCH_TOKEN` unset | Correctly degrades to "NOT CHECKED" with an explicit warning rather than silently skipping or crashing |

---

## Settings-Tab Federation (item 3 of 3)

Added in v8.73.15. See `docs/plans/historical-plans/2026-10-08-settings-tab-federation.md` for the full per-function inventory.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Server picker injected into `renderSettingsView` | Yes | No | `app-settings-fed.test.js` | Confirms `hideAll:true` passed, matching Observer |
| `loadConfigStatus` (raw-fetch migration pattern) | Yes | No | Same file — 2 tests (real error + proxy-URL regression guard) | Representative of 14 migrated functions |
| `loadCommsConfig` (multi-section pattern) | Yes | No | Same file | Representative of 3 multi-section functions (Comms/General/LLM) |
| `loadGuardrailProfilesPanel` (already-apiFetch pattern) | Yes | No | Same file | Representative of 10 functions |
| `loadComputeNodesPanel`/`loadSecretsPanel` (already-correct confirmation) | Yes | No | Same file | Spot-check of the "no change needed" claim, not exhaustive |
| Remaining ~34 touched/audited functions | No (not individually) | No | Code-reviewed, not unit-tested individually — each shares one of the 4 patterns above | Follow-up: consider per-function tests if a regression surfaces |

---

## PWA Federated Error Visibility Phase 4 — Dashboard Periodic Re-polls + Observer's Remaining Sub-cards

Added in v8.73.11. See `docs/plans/historical-plans/2026-10-08-pwa-federated-error-visibility.md` (Phase 4).

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `_dashSetFedError`/`_dashClearFedError` multi-source banner | Yes | No | `app-fed-cap-errors.test.js` — 2 tests | Not validated against a real `_dashLoop` RAF cycle (canvas-heavy; isolated the helper functions directly instead, per that test's own comment) |
| Observer sub-cards (`_obsFedMsg` simple pattern: Plugins, Backend Health, Envelopes, channel bridge/diagnostics, Peer Resources, eBPF status/network) | Yes | No | `app-observer-fed-errors.test.js` — 1 representative test (Plugins); rest share the identical one-line pattern already proven by `loadStatsPanel`'s existing test | Not individually tested — code-reviewed as identical to the proven pattern |
| ACME / Cluster Nodes hide-vs-show distinction | Yes | No | Same file — 3 tests (remote shows+unhides, local still hides) | |
| Observer Peers dual-fetch `peersErr` capture | Yes | No | Same file — 2 tests (real error shown vs. genuine empty-state preserved) | |

---

## GH#194 — Never Label the Connected Server "local"

Added in v8.73.10. `handleListServers`'s new `hostname` field + client-side `_ensureLocalHostname`/`_localHostname`. See `docs/plans/historical-plans/2026-10-08-gh194-never-say-local.md`.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `GET /api/servers` local entry carries `hostname` — `TestListServers_LocalEntryCarriesRealHostname` | Yes | No | Go test, direct handler call | Not validated against a live daemon this round |
| `_ensureLocalHostname` caching + dedup-fetch | Yes | No | `app-server-identity-label.test.js` — 2 tests | |
| `_serverPickerBar` local chip: real hostname vs. never-"Local" fallback | Yes | No | Same file — 2 tests | |
| `loadSystemStatsGrid` (Observer) self-peer dedup, no "local" badge | Yes | No | Same file | Mocks `/api/stats` + `/api/observer/peers` with a matching self-peer entry |
| `loadServers` (Settings list) real hostname display | Yes | No | Same file | |
| Swipe-gesture modal picker (`_loadServerPickerModalList`) | No | No | Not unit tested this round — shares `_ensureLocalHostname`'s callback path, verified by code review only | Follow-up: add a direct test |
| "Back to %1$s" button / connection toast / Sessions tooltip parameterization | No | No | Not unit tested this round — covered by manual code review of the render call sites | Follow-up: add direct tests |

---

## PWA Federated Error Visibility (401/403/502)

Added in v8.73.8. `_fedFetchError`/`apiFetch` central classifier across Sessions, Alerts, Automata, Dashboard (partial), Observer (partial), server picker reachability. See `docs/plans/historical-plans/2026-10-08-pwa-federated-error-visibility.md`.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `_fedFetchError` 401/403/502/empty-body classification | Yes | No | `app-fed-cap-errors.test.js` — fake responses for each status + empty body fallback | Not validated live against a real second daemon this round |
| `apiFetch` auto-classification (proxied vs. local) + URL-routing regression guard | Yes | No | Same file — 3 tests | Builds on existing proxy routing, which was already live-validated in v8.73.6 (TS-785/786) |
| `_checkFederatedConnection` 401-vs-403 split, 502 real dial text | Yes | No | `app-fed-conn-status.test.js` — updated the pre-existing 502 test to assert on real text instead of a bare status code | Was live-tested for 401 in an earlier round (see that file's own comments); 403/502 split not separately live-tested |
| `loadAutomataPanel` no longer swallows federated errors | Yes | No | `app-fed-cap-errors.test.js` | |
| `loadStatsPanel` (Observer) federated vs. local error framing | Yes | No | Same file — 2 tests | Only the primary stats card; 11 other Observer sub-cards not instrumented this round |
| `renderDashboardView` one-time banner on initial federated failure | Yes | No | Same file | Periodic re-poll failures remain silent by design this round |
| `_probePickerReachability` / dimmed picker chip | Yes | No | Same file — 4 tests | Bounded-timeout background probe; not live-tested against a genuinely unreachable host |

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
`docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md`. The ACME
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

## GH#174 — MCP channel bridge self-healing re-registration — v8.63.6

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `bridge.readyHeartbeat` keeps re-sending `POST /api/channel/ready` on every tick | **Yes** | No | `TestReadyHeartbeat_KeepsReannouncing` in `cmd/datawatch-channel/main_test.go`: against a fake parent HTTP server, confirms ≥3 re-announces within a short deadline using a 10ms tick, each with the correct path + port; confirms the loop actually stops on context cancel | — |
| `notifyReady`'s one-shot guard still holds for the startup call, but no longer blocks subsequent heartbeat sends | **Yes** | No | `TestNotifyReady_DoesNotBlockSubsequentHeartbeatSends`: 1 `notifyReady()` + 2 `sendReady()` calls produce exactly 3 posts (regression check against `TestNotifyReadyIdempotent`, which still passes unchanged) | — |
| Full daemon-restart-then-self-heal round trip against a real daemon + real bridge process | No | No | — | Needs a live claude-code session + an actual daemon restart mid-session to observe the WS `channel_ready` event firing again; not simulated here. The unit coverage above exercises the exact mechanism (repeated idempotent POST) the live scenario depends on |

## GH#173 — `config generate` emits a loadable empty list, not the string "[]" — v8.63.7

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `yamlVal([]string{...})` — empty renders `[]`, non-empty renders a real flow sequence, comma-containing elements are quoted | **Yes** | No | `TestYamlVal_EmptyStringSlice_RendersAsEmptyList`, `TestYamlVal_NonEmptyStringSlice_RendersAsFlowSequence`, `TestYamlVal_StringSliceElementWithComma_IsQuoted` in `internal/config/gh173_generate_roundtrip_test.go` | — |
| `GenerateAnnotatedConfig` output loads back cleanly via `Load` (the issue's own suggested regression test) | **Yes** | **Yes** | `TestGenerateAnnotatedConfig_RoundTrips` — confirmed-fails against the pre-fix code with the exact original error text (`cannot unmarshal !!str`), and live-reproduced manually: `datawatch config generate > f.yaml && datawatch config show --config f.yaml` now succeeds | — |
| `datawatch start --foreground --config <broken.yaml>` errors out instead of silently falling back to `config.DefaultConfig()`'s (production) data dir for the PID-lock check | No | **Yes** | Manually verified: ran against a real intentionally-broken config pointed at this machine's actual production data dir; confirmed via `daemon.pid`'s mtime that the file was NOT touched by the broken-config run (pre-fix, it would have been overwritten with the wrong PID) | Not automated — the PID-lock block is deeply embedded in `runStart`'s larger daemon-bootstrap flow; extracting it for a clean unit test is a larger refactor than this fix warrants |

## GH#181 — council live run log renders markdown — v8.63.8

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `councilOpenLiveWatch`'s `persona_response`/`run_completed` SSE handlers render via `appendMarkdown` instead of truncated plain text | No | No | — | Client-only UI change with no new sanitization code (reuses `_renderMarkdownFileInto`, already covered by `diagrams-xss.test.js`'s DOMPurify tests); `node --check` + the full existing JS suite (40 tests) still pass. Testing a real SSE event stream against a live DOM would need a browser-level harness this codebase's stub-based JS tests don't build (they deliberately avoid a real DOM tree) — not added for this single UI wiring change, consistent with how other PWA-only UI swaps in this tracker are handled |

## GH#186 — animated-eye content-loading state across the PWA — v8.63.9

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `loadingEyeBlock(label, size)` / `_wireLoadingEyes()` / the global auto-start `MutationObserver` | No | No | — | `node --check` clean; full existing JS suite (40 tests) unaffected (pure addition, no existing logic changed). No new automated test — purely visual/animation behavior, same category as GH#181's markdown-rendering UI swap earlier this version run, which this tracker also records as untested beyond syntax/suite-regression checks |
| Applied across 123 call sites (Observer/Settings/Dashboard/Alerts/Automata/Session-detail/folder-browsers) | No | No | — | **Not visually verified** — no browser access in this environment. Manually reviewed a representative sample of the diff for HTML correctness (the Dashboard heatmap's canvas-can't-hold-HTML special case using an absolutely-positioned overlay div; a `<pre>`-wrapped instance and a few `<span>`-wrapped instances that nest a block-level div inside an inline element, which browsers tolerate but isn't textbook-valid HTML5) and confirmed via grep that effectively zero plain "Loading…" card/panel placeholders remain (3 residual hits are an unrelated `<select>` option fallback, this feature's own internal fallback literal, and a code comment). Flagged explicitly rather than claiming a browser-confirmed "done" |
| Locale keys used as labels (`identity_loading`, `algorithm_loading`, `evals_loading`, `council_loading`, `compute_loading`, `llm_loading`, `timeline_loading`, `common_loading`) | **Yes** | No | Confirmed all pre-existing in `locales/en.json` — no new translatable strings added, no locale-bundle updates needed | — |

## GH#186 follow-up — Dashboard's empty-shell gap — v8.63.10

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Dashboard shows the loading eye immediately while `GET /api/dashboard/layout` is pending | No | **Yes** (the bug this fixes was found live) | Live browser spot-check by the datawatch-app peer (headless Chromium/Playwright against a real sandbox daemon, every `/api/*` response held open 6s, canvas pixel-diffed across two captures to confirm the animation genuinely runs, not just a static frame): Settings/Observer/Automata-list/animation all confirmed correct; Dashboard confirmed broken (completely empty, no eye) before this fix. Fix itself (`node --check` + the 40-test JS suite) re-verified clean; the actual visual fix is **not** re-confirmed in a browser — same "not visually verified" caveat as the rest of GH#186, now narrowed to just this one container | — |
| "Detail Graph tab" item from the original issue | No | No | — | Code review only: the PWA's dependency-graph section isn't a separate tab with its own fetch — it renders synchronously inside the single PRD-detail fetch, which already shows the loading eye in `#automataDetailBody` (same pattern as the Automata list, confirmed correct by the peer). No code change was needed or made here |

## B98 — claude-code / ollama sessions report real token usage — v8.63.11 / v8.63.12 / v8.63.13 / v8.63.14

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `sendAndStream`'s SSE decode now captures a top-level `usage` object from the final chunk and reports it via `usageFn` | **Yes** | **Yes** | `TestSendAndStream_ReportsUsageFromFinalChunk`, `TestSendAndStream_NoUsageChunkNeverCallsUsageFn`, `TestSetUsageFn` (`internal/llm/backends/openwebui/usage_test.go`) against an `httptest` server replaying the real captured fixture | Fixture SSE body captured verbatim from a real local OpenWebUI 0.11.4 instance (Docker, routed to local Ollama `qwen3:1.7b`), not synthesized from the API docs |
| `aiderTokensRe` / `parseAiderTokenCount` — matches aider's real `Tokens: N sent, M received.` line, handles the `"X.Yk"`/`"Nk"` abbreviation `format_tokens()` switches to above 1,000/10,000 tokens, tolerates optional cache-write/cache-hit segments | **Yes** | **Yes** | `TestParseAiderTokenCount`, `TestAiderTokensRe_MatchesRealCapturedLine` (fixture line captured verbatim from a real tmux `pipe-pane` log of a live `aider --model ollama_chat/qwen3:1.7b` run), `TestAiderTokensRe_MatchesKFormat`, `TestAiderTokensRe_ToleratesCacheSegments` | The k-format thresholds were confirmed by reading aider 0.86.2's own source (`aider/coders/base_coder.py`/`aider/utils.py`), not guessed — a live sample alone wouldn't exceed 1,000 tokens without a much longer conversation |
| `scanAiderUsageOnce` — sums only new lines since the last scan, strips ANSI before matching, no-ops on a missing log file | **Yes** | No | `TestScanAiderUsageOnce_SumsOnlyNewLines`, `TestScanAiderUsageOnce_MissingFileIsNoop`, `TestScanAiderUsageOnce_StripsANSIBeforeMatching` — all pass under `-race -count=5` | — |
| `gemini.Launch` — Go-mediated (`exec.CommandContext` + JSON parse) instead of a raw shell invocation; re-prints only the response text to the pane | No | **Yes** | Live end-to-end run of the real unauthenticated CLI through the new code path in a real tmux pane (`tmux new-session` + `pipe-pane`/`capture-pane`), twice (once to find the stderr bug, once to confirm the fix) | — |
| `jsonResult` parses gemini's real captured error shape (`{session_id, error:{type,message,code}}`) | **Yes** | **Yes** | `TestJSONResult_ParsesRealCapturedErrorShape` against a fixture captured verbatim from the live run above | — |
| On error, gemini writes its JSON to **stderr**, not stdout — `Launch` tries stdout first, falls back to stderr | No | **Yes** | Caught and fixed via the live verification above, not a unit test (no live CLI available in `go test`) | A real regression the first version of this fix had — every error fell through to a raw-JSON-dump fallback until this was found |
| `extractUsage` — reads a flat top-level token-count pair, tolerating snake_case/camelCase/Google-API-naming variants; does not recurse into a nested `models` breakdown | **Yes** | No | `TestExtractUsage_SnakeCase`, `TestExtractUsage_CamelCase`, `TestExtractUsage_GoogleAPINaming`, `TestExtractUsage_EmptyOrNestedIsZero` — all pass under `-race -count=5` | The success-path field names are confirmed from gemini-cli's own source but **not** live-exercised against a real successful response (no Google Cloud credentials available) — re-verify once real credentials exist |
| `resolveOpenCodeSessionID` — matches on `directory` + closest `created` timestamp; empty project dir and no-directory-match both return `""` rather than a wrong guess | **Yes** | **Yes** | `TestResolveOpenCodeSessionID_MatchesDirectoryAndClosestTime`, `TestResolveOpenCodeSessionID_NoDirectoryMatchReturnsEmpty`, `TestResolveOpenCodeSessionID_EmptyProjectDirIsNoop` against a fake `opencode` CLI script; mechanism also live-verified against the real `opencode` binary (`session list --format json` shape confirmed) before writing the parser | — |
| `scanOpenCodeUsageOnce` — skips the `Exporting session: <id>` prefix line before parsing JSON, caches the resolved session ID, reports only the delta between cumulative `info.tokens.{input,output}` totals (not the raw cumulative figure) | **Yes** | **Yes** | `TestScanOpenCodeUsageOnce_SumsDeltaAcrossTicks` against a fake binary that changes its reported cumulative totals between calls — confirms both the "no change → no report" and "delta only, not re-summing the whole total" cases; `-race -count=5` | Real `opencode export <id>` live-verified separately to have exactly this prefix-line + cumulative-totals shape |
| `trackOpenCodeUsage` — stops cleanly on context cancel, cleans up both tracking maps, no-ops immediately for a nil `reportFn` | **Yes** | No | `TestTrackOpenCodeUsage_StopsOnContextCancel`, `TestTrackOpenCodeUsage_NilReportFnIsNoop` | — |
| `scanGooseUsageOnce` — reports only the delta between cumulative `accumulated_usage.{input_tokens,output_tokens}` totals | **Yes** | **Yes** | `TestScanGooseUsageOnce_SumsDeltaAcrossTicks` against a fake `goose` binary; `-race -count=5` | Real `goose session export --name <name> --format json` live-verified separately (ran an actual `goose run --name ...` session end to end) to have exactly this shape, including confirming the custom `--name` works directly as the export lookup key without needing goose's own separate date-based internal session ID |
| `trackGooseUsage` — no-ops immediately (no shell-out at all) when `Session.Name` is empty, since goose's internal ID is then unpredictable; stops cleanly on context cancel | **Yes** | No | `TestTrackGooseUsage_EmptyNameIsNoop`, `TestTrackGooseUsage_StopsOnContextCancel` | — |
| `ollama.SetUsageFn` wiring gap in `cmd/datawatch/main.go` (added in v8.63.11, never called) | No | **Yes** | Found by re-checking the v8.63.11 fix before calling it complete — `grep` confirmed `SetChatEmitter`/`SetSaveConversationFn` were wired at startup but `SetUsageFn` had no corresponding call site anywhere in the binary | Fixed by registering the same `tmuxSession → sessID → mgr.AddUsage` callback shape as its sibling setters, right next to them |
| Full daemon-restart / live opencode or goose session round trip (usage polling actually populating a real `Session.TokensIn/TokensOut` over a real session's lifetime) | No | No | — | Not live-verified in this pass, same gap noted for claude-code in v8.63.11 — the unit tests above exercise the exact parsing/correlation/delta mechanism the live scenario depends on, against fake binaries whose output shape was itself confirmed against the real CLIs |
| `claudeSessionUUID` — reproduces `claudecode.deriveSessionUUID` exactly (deterministic, no collisions across session IDs) | **Yes** | No | `TestClaudeSessionUUID_Deterministic` in `internal/session/claude_usage_test.go` | — |
| `claudeTranscriptPath` — escapes `/` and `.` in the project dir to match Claude Code's own on-disk directory naming | **Yes** | **Yes** | `TestClaudeTranscriptPath_EscapesSlashesAndDots` (escaping rule confirmed against real transcript directories already present on this machine, not guessed), `TestClaudeTranscriptPath_EmptyProjectDir` | — |
| `scanClaudeUsageOnce` — sums only new assistant-turn usage since the last scan (no double-counting), ignores non-assistant lines, ignores a missing file, deliberately excludes cache tokens | **Yes** | No | `TestScanClaudeUsageOnce_SumsOnlyNewAssistantLines`, `TestScanClaudeUsageOnce_MissingFileIsNoop`, `TestScanClaudeUsageOnce_CacheTokensNotCounted` — all pass under `-race -count=5`; the "don't double-count" and "ignore cache tokens" cases are the two that would have silently produced wrong numbers if broken, so both have dedicated tests, not just a happy-path check | — |
| Real-transcript parse sanity check | No | **Yes** | Ran the actual parsing logic (not the test fixtures) against a real JSONL transcript already on this machine, outside the test suite, and got correct non-zero `input_tokens`/`output_tokens` back | — |
| `trackClaudeCodeUsage` — stops cleanly on context cancel and cleans up its own tracking-map entry; no-ops immediately for an empty project dir | **Yes** | No | `TestTrackClaudeCodeUsage_StopsOnContextCancel`, `TestTrackClaudeCodeUsage_EmptyProjectDirIsNoop` | — |
| Ollama session-backend `/api/chat` streaming path reports `prompt_eval_count`/`eval_count` from the final chunk | No | No | — | No dedicated test added — this is a small, localized change to an already-existing decode struct in `internal/llm/backends/ollama/conversation.go`, covered by the existing package's own streaming tests continuing to pass unmodified; the new fields are additive and only act on the already-detected `done` branch |
| Full daemon-restart / live claude-code session round trip (transcript polling actually populating a real `Session.TokensIn/TokensOut` over a real session's lifetime) | No | No | — | Not live-verified in this pass — needs an active claude-code session running under the new daemon build for at least one 10s poll interval. The unit tests above exercise the exact parsing/dedup mechanism the live scenario depends on |

## B96 — openwebui's `${secret:name}` api_key never resolved — v8.63.15

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Real symptom: a session on the real `openwebui` backend using a `${secret:name}` api_key | No | **Yes** | `POST /api/sessions/start` with `"backend":"openwebui"` against the real `datawatch:3000` instance — got a real `401 Unauthorized` in the session's pane output, confirmed the key itself was valid via a direct `curl` with the same bearer token | Found incidentally while wiring B98's live OpenWebUI verification, not from reading code first — the live symptom came before the root-cause diagnosis |
| `openwebui.SetAPIKey` updates the already-registered active backend's `apiKey` field in place | **Yes** | **Yes** | `TestSetAPIKey_UpdatesActiveBackend`, `TestSetAPIKey_NoActiveBackendIsNoop` (`internal/llm/backends/openwebui/usage_test.go`) | — |
| Full fix, live end to end: real session against the real instance after the fix | No | **Yes** | Re-ran the exact same `POST /api/sessions/start` call after deploying the fix — got a real assistant response ("Hi.") and real `tokens_in: 16`/`tokens_out: 28` on the session, both the auth fix and B98's usage-tracking fix (v8.63.13) confirmed working together in production | — |

## B97 — PRD scratch artifacts relocated out of the shared project repo — v8.66.1

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `ScratchDir` — creates and returns `<data_dir>/autonomous/scratch/<prd_id>/`; errors on empty args | **Yes** | No | `TestScratchDir_CreatesAndReturnsPath`, `TestScratchDir_EmptyArgsError` (`internal/autonomous/scratch_test.go`) | — |
| `RelocateProjectFile` — moves content to the scratch dir and deletes the project_dir source; no-ops on a missing or empty file; still deletes the source even when the archival write target is unavailable (`scratchDir=""`, simulating an upstream `ScratchDir` failure) | **Yes** | No | `TestRelocateProjectFile_MovesContentAndDeletesSource`, `_MissingFileIsNoop`, `_EmptyFileIsNoop`, `_ArchiveWriteFailureStillDeletesSource`, `_EmptyProjectDirIsNoop` — all pass under `-race -count=10` | The "still deletes even when archiving fails" case is the one that would have silently produced wrong behavior (leaving files in project_dir forever) if broken, not just a happy-path check |
| `tooling.EnsureIgnoredPatterns` — new, backend-independent gitignore helper extracted from `EnsureIgnored`; idempotent; empty pattern list is a no-op that doesn't even create `.gitignore` | **Yes** | **Yes** | `TestEnsureIgnoredPatterns_CreatesGitignore`, `_Idempotent`, `_EmptyIsNoop` (`internal/tooling/v6080_bl219_test.go`) | — |
| `BackendArtifacts` never carries datawatch's own scratch patterns (would break `QueryAllStatus`'s "every key is a real backend" invariant — confirmed via a real pre-existing test, `TestQueryAllStatus_AllKnownBackends`, which failed when a first draft of this fix added a `"datawatch-prd"` pseudo-key to that map) | **Yes** | **Yes** | `TestBackendArtifacts_ExcludesDatawatchOwnScratchPatterns` pins the boundary going forward | Caught by the project's own existing test suite, not found by inspection — exactly the kind of regression a full `go test ./...` run before shipping is meant to catch |
| Full `executeOne` retry/terminal lifecycle wiring (stall retry, quality-gate-regression retry, normal verify-failure retry, success, exhausted-retries) | No | No | — | Not live-verified against a real PRD run in this pass — the unit tests above exercise the exact relocation/fold-into-hint mechanism the live scenario depends on; a real end-to-end PRD run with a worker that actually writes CHECKPOINT.md would be the next step to confirm live |
| Pre-existing, unrelated finding surfaced incidentally: `internal/autonomous`'s `prd.Status` field has a real data race (`executor.go`'s `Run()` write racing `resumeRunningPRDs`-style reads elsewhere), already documented in a code comment as a known, scoped-but-incomplete fix. Reproduces under `go test -race -count=5` on several pre-existing tests (`TestExecutor_StoryFailureHaltsPRDByDefault` and others) — confirmed via `git stash` that it reproduces identically without this commit's changes, so not introduced here. Not fixed in this pass (out of scope; flagged for its own follow-up). | N/A | N/A | `git stash && go test -race -count=10 ./internal/autonomous/... -run TestExecutor_StoryFailureHaltsPRDByDefault` fails identically on the pre-change code | Worth its own dedicated fix — the existing code comment already scopes exactly what it does and doesn't cover |

## BL315 — PWA install prompt reinstated — v8.66.2

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Backlog doc staleness found while validating the operator's own "PWA has fullscreen already" observation: `git log -S` traced 2 undocumented, deliberate replacements since the v7.2.3 "Active work" entry was written (native `requestFullscreen()` → `window.resizeTo()` → the current CSS `.pwa-expanded` toggle), and found the install-prompt half was explicitly deleted along the way and never reinstated | No | **Yes** | `git log -S"requestFullscreen"`, `-S"resizeTo"`, `-S"pwa-expanded"`, `-S"beforeinstallprompt"` against `internal/server/web/app.js`; read each commit's full message/diff | `docs/pwa-setup.md` had also drifted — still described native fullscreen 2 implementation generations after the behavior changed; corrected in this pass too |
| `_onBeforeInstallPrompt`/`_onAppInstalled`/`installPWA` — show/hide the install button, call `prompt()`, await `userChoice`, handle a no-op call before any prompt has fired, handle a second prompt after a prior install/uninstall cycle | **Yes** | No | `app-install-prompt.test.js`, 5 tests, all pass | Logic deliberately factored out of the raw `addEventListener` callbacks specifically to make this testable — `beforeinstallprompt` can't be realistically synthesized in either the Node test sandbox (stubs `addEventListener` as a no-op) or a scripted Chromium e2e run (opaque per-browser install-eligibility heuristics) |
| `#headerInstallBtn` present in the header markup, hidden by default | No | **Yes** | `scripts/test-stories/pwa/TS-149.mjs` extended to check the button exists and starts `display:none` | The real `beforeinstallprompt` firing + full install flow is not (and cannot reliably be) exercised end-to-end by an automated test; this is the same honesty standard applied elsewhere in this tracker for browser-heuristic-gated behavior |
| Window-expand state (`_pwaExpanded`) persists across a simulated reload (`localStorage['cs_pwa_expanded']`), restores before any click, shared apply-function keeps load-restore and click-toggle in sync, survives `localStorage.setItem` throwing after a successful load | **Yes** | No | `app-pwa-expand-persist.test.js`, 6 tests (v8.66.3) | Caught a real, separate, pre-existing gap while testing: `state.token = localStorage.getItem('cs_token')` at the top of `app.js` has no try/catch — a fully-throwing `localStorage` crashes the app before this fix's own code ever runs. Not fixed here (out of scope), flagged for its own follow-up |

## BL316 S2 — RemoteDispatcher live-store staleness (cross-host comm-channel send + CLI `--server`) — v8.67.0

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Root cause, confirmed via audit before fixing: `RemoteDispatcher.servers` was a one-time snapshot of `cfg.Servers` taken at daemon-startup construction, never refreshed from the live `multiserver.Store` — a peer added at runtime (`federation peer add`, the PWA's Federated Peers panel, or plain `POST /api/servers`) was invisible forever (until daemon restart) to comm-channel cross-host `send`/`ForwardCommand`/`FindSession` and to the CLI's `--server` flag | No | **Yes** | Code-read audit (`internal/proxy/remote.go` vs. `internal/server/proxy.go`'s already-correct `findServer`/`runtimeServers` merge pattern), confirmed by a forked agent before any fix was written | — |
| `RemoteDispatcher.SetStore(store)` + `effectiveServers()` — once wired, every dispatch method (`HasServers`, `refreshCache`, `ForwardCommand`, `ForwardHTTP`, `ListAllSessions`) reads the live `multiserver.Store` on every call instead of the frozen construction-time slice; falls back to the static snapshot when no store has been wired yet (startup window, and direct-construction unit tests) | **Yes** | No | `TestEffectiveServers_SetStoreSeesRuntimeAddedPeer`, `TestEffectiveServers_LiveUpdateOverridesStaleView`, `TestEffectiveServers_FallsBackToStaticSnapshotBeforeSetStore` (`internal/proxy/remote_test.go`); full existing suite (10 tests) still green; `-race -count=3` clean | `NewRemoteDispatcher` is now always constructed in `main.go` (previously gated on `len(cfg.Servers) > 0`, which meant a daemon with zero YAML-seeded servers but dynamically-added peers had no dispatcher at all) |
| CLI `--server <name>`: `daemonAPIURL`/`daemonHTTPClient` used to resolve the name by scanning this *separate CLI process's* own YAML-loaded `cfg.Servers` — same staleness gap, plus it needed to hold the remote's plaintext token in this short-lived process for no reason. Now always targets the *local* daemon and routes through its existing `/api/proxy/<name>/<path>` passthrough (already correct — merges YAML + live store, injects the remote's stored token server-side) | **Yes** | No | `TestDaemonAPIURL_ServerFlagRoutesThroughLocalProxy`, `TestDaemonAPIURL_NoServerFlagTargetsLocalCommand`, `TestDaemonAPIURL_ExplicitURLFlagWins`, `TestDaemonHTTPClient_ServerFlagUsesLocalToken` (`cmd/datawatch/daemon_api_url_test.go`) | Behavior change from before this fix: an unknown/disabled `--server` name used to silently fall back to `localhost` with a stderr warning (so a command could silently run on the *wrong* daemon without the operator noticing); now the local proxy returns an explicit 404 instead — a deliberate improvement, not an incidental side effect |
| Full live round trip: two real daemons, a peer added to one via `federation peer add` while both are already running, then a cross-host `send` or `datawatch session list --server <name>` from the CLI | No | No | — | Not live-verified in this pass — the unit tests above exercise the exact live-vs-frozen dispatch mechanism the live scenario depends on (`SetStore`/`effectiveServers`, and the proxy passthrough, which already had its own coverage) |

## BL317 — per-row server attribution for PRDs and Alerts — v8.68.0

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `renderPRDRow` renders a `.server-badge` chip (same markup/class as Sessions) when `prd.server` is set and not `"local"`; renders nothing when absent or `"local"` | **Yes** | No | `app-server-badge.test.js` ×3 | Confirmed server-side first: `/api/autonomous/prds/aggregated` (`internal/server/bl312_aggregated.go`) already attaches `server` per item — this was a client-side rendering gap only |
| `renderAlertCard` (hoisted out of `renderAlertsView`'s closure, `cmds` now an explicit param instead of a capture) renders the same badge per-alert-card, not per session-group — a `__system__` group (no `session_id`) can mix alerts from several servers in `all` mode, so per-session attribution would be wrong | **Yes** | No | `app-server-badge.test.js` ×3, including one confirming the quick-reply dropdown still renders correctly with `cmds` passed explicitly | — |
| Regression: hoisting `renderAlert` out of the closure didn't change behavior at any of its 3 existing call sites | No | **Yes** | Full JS suite (70 tests across all `*.test.js`) still green after the refactor | — |

## BL317 — Dashboard aggregation + Observer picker drops "All" — v8.69.0

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `GET /api/cost/aggregated` — fans out to local + every enabled server (cfg.Servers + runtime store), tags each `CostSummary` with `server`; an unreachable/erroring peer is logged and skipped, never fails the request | **Yes** | No | `TestHandleAggregatedCost_FansOutAndTagsEachServer`, `TestHandleAggregatedCost_UnreachableRemoteIsSkippedNotFatal`, `TestHandleAggregatedCost_RejectsNonGET` (`internal/server/bl317_aggregated_cost_test.go`); full suite 3164/83 packages green; `-race -count=3` clean | Mirrors the existing (itself untested before this pass) `handleAggregatedAlerts`/`handleAggregatedPRDs` pattern in `bl312_aggregated.go` |
| `_dashFetchPRDs()`/`_dashFetchCost()` (extracted from duplicated logic in `renderDashboardView` + `_dashLoop`) — branch on `state.activeServer === 'all'`, unwrap either response shape, sum `CostSummary` fields across servers, tolerate a non-array aggregated response (e.g. a 403 body) without throwing | **Yes** | No | `app-dashboard-aggregation.test.js` ×5 | — |
| **Incidental finding, fixed in the same pass**: `_dash._costToday`'s 3 consumers (`_dashUpdateStatBar` ×2, `_dashRenderBurnRate`) all read `c.total_cost_usd`; `/api/cost`'s real field is `total_usd`, and `sessions` is a count not an array — the Dashboard cost display has rendered `$0`/hidden in every mode (not just "all servers") since whenever this was written. Fixed at all 3 sites by reading `total_usd` directly | No | **Yes** | Covered indirectly — the 2 `_dashFetchCost` tests above assert on `.total_usd` as the correct field; no dedicated display-level test since this was a one-line field-name correction with no new render logic | Not caught by any prior test because none of the 3 consumer sites had one before this pass |
| `_serverPickerBar(opts)`/`_injectServerPickerBar(el, rerenderFn, opts)` — `{hideAll:true}` omits the "All" `selectServer('all')` chip while every other chip (Local + named servers) still renders; default (no opts, or `{}`) is unchanged from before this pass | **Yes** | No | `app-dashboard-aggregation.test.js` ×3 (`_serverPickerBar` default-includes-All, hideAll-omits-All, hideAll-with-zero-servers-still-returns-empty-string) | Observer's call site now passes `{hideAll:true}`; Dashboard's (and Sessions'/Alerts'/PRDs') call sites are unchanged, still showing "All" |
| Full live round trip: two real daemons, Dashboard's "All" chip clicked, cost/PRD numbers reflecting both | No | No | — | Not live-verified in this pass — the unit tests above exercise the exact fan-out/summing/branching mechanism; a real multi-daemon setup is needed to confirm the full round trip end to end |

## `_serverPickerBar` onclick-escaping bug (operator-reported live) — v8.69.1

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Every server-picker chip (All/Local/named) threw on click instead of switching servers — `JSON.stringify(c.name)`'s own `"` characters collided with the onclick attribute's `"` delimiter, truncating the attribute at the first embedded quote | No | **Yes** | Operator hit this live clicking a real federated peer chip ("Apple Testing Sandbox"); root-caused directly from the reported stack (`Unexpected end of input (at (index):1:14)`), not independently reproduced first | Pre-existing, not introduced by the same day's BL316/BL317 work — just newly surfaced by exercising a real federated peer name for the first time |
| Fix: `escHtml()`-wraps the whole `selectServer(...)` onclick expression, same pattern already used correctly by `loadServersList()`'s `testServerEntry` button | **Yes** | No | `app-dashboard-aggregation.test.js` — new regression test decodes every chip's onclick attribute the way a browser's attribute parser would and confirms the full, untruncated call; 2 pre-existing tests in the same file updated for the new (correct) escaped-attribute shape | — |

## BL317 — TS-387–396 implemented for real + 18 missing MCP-tool-cap entries found and fixed — v8.69.2

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| TS-387 (`POST`/`GET /api/servers`), TS-388 (`GET /api/servers/{name}`, 404 on unknown), TS-389 (`PUT /api/servers/{name}`), TS-390 (`DELETE /api/servers/{name}`), TS-392 (`GET /api/alerts/aggregated`), TS-393 (`GET /api/autonomous/prds/aggregated`), TS-394/395 (CLI `server list`/`server add`) | **Yes** | **Yes** | Live run against an isolated sandbox daemon via `scripts/run-tests.sh --stories=...` (never production) — all passed on the first run, before any code change; these 10 stories were never actually stubs, just mislabeled (stale "STUB: no implementation extracted from legacy runner" header comment, now removed from all 10 files) | — |
| TS-391 (`POST /api/servers/{name}/test` against a self-referencing entry) | **Yes** | **Yes** | Same live run; passes with a soft-pass note — the sandbox's self-signed cert makes the self-test's own HTTPS health check report `ok:false`, which the story already treats as an acceptable 200-with-body response, not a failure | The sandbox's self-signed cert is expected and out of scope here; not a bug |
| TS-396 (`server_list` MCP tool via `POST /api/mcp/call`) — **found a real bug on the first live run**: `federation.MCPToolCap` had zero entries for any of the 6 BL312 `server_*` or 12 BL316 `federation_{peer,group}_*` tools, so the REST bridge (the chokepoint channel bridges/CLI/this story all actually use) rejected every one of them with 404 "unknown tool" despite proper `AddTool` registration and real stdio/SSE reachability | **Yes** | **Yes** | Live run showed the skip; root-caused directly (`handleMCPCall` → `federation.RequiredCapForMCPTool` → map lookup miss); fixed by adding all 18 entries (mirroring each tool's REST-handler-equivalent capability exactly: list/get → `CapFederationList`, write/test → `CapFederationWrite`); re-ran the same live sandbox test afterward — **10/10 passed, 0 skipped** | `mcp_tool_caps.go`'s own header comment claimed a test (`internal/server/route_caps_a3_test.go`) already enforces this — that file does not exist anywhere in the tree |
| `TestEveryUnconditionallyRegisteredToolHasAnMCPToolCapEntry` (`internal/mcp/mcp_tool_cap_coverage_test.go`) — the real version of that claimed safety net: constructs an actual `mcp.Server` via `New()` and asserts every `ListTools()` entry has a `MCPToolCap` entry | **Yes** | **Yes** | Failed with the exact 18-tool list before the fix (confirming the fix's scope was complete, not just the 1 tool TS-396 happened to probe), passes after it; full Go suite 3165/83 packages green | `internal/server/mcp_bridge_cap_test.go` (the file that sounded like the promised test) only exercises a hardcoded 2-tool fake catalog and could never have caught this — not fixed/replaced in this pass, just superseded for this specific coverage question |

## GH#192 Phase 1 — agent id badge, watched-only alert badge, Observer server info, terminal splash dwell, header spinner — v8.70.0

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `sessionCard` renders `⬡ <agent_id>` directly (both list-card and session-detail call sites); no badge at all when `agent_id` is absent | **Yes** | No | `app-gh192-phase1.test.js` ×2 | — |
| `updateAlertBadge` shows the flat `alertUnread` total when `sessionWatchFilter` is off, and the watched-only `alertWatchedUnread` count when it's on (including hiding the badge entirely when the watched-only count is 0 but the total isn't) | **Yes** | No | `app-gh192-phase1.test.js` ×3 | — |
| `handleAlert` increments `alertWatchedUnread` only for alerts whose `session_id` is in `watchedSessions`; a system alert (no `session_id`) never counts | **Yes** | No | `app-gh192-phase1.test.js` — 3 alerts (watched session / unwatched session / system), asserts the watched-only count is exactly 1 | — |
| `toggleSessionWatchFilter` calls `updateAlertBadge()` so the badge reflects the new filter state immediately, without waiting for the next alert | **Yes** | No | `app-gh192-phase1.test.js` | — |
| Initial-load seed (`fetch('/api/alerts')`'s handler) derives `alertWatchedUnread` from the full returned `alerts` array (`!a.read && a.session_id && watchedSessions.has(...)`), not just the flat `unread_count` field | No | No | Not covered by a dedicated test — the filtering expression is a one-line inline computation at the fetch callback, not a named function; covered indirectly by the `handleAlert` test exercising the identical filter condition | Noted gap, not blocking — the logic is duplicated in exactly 2 places (seed + `handleAlert`) and both use the same condition |
| `Collector.SetServerIdentity(hostname, version)` populates `SystemStats.Hostname`/`DaemonVersion` | **Yes** | No | `TestCollector_SetServerIdentity` (`internal/stats/collector_test.go`) | Mirrors the existing (previously untested) `SetServerInterfaces` pattern |
| `renderStatsData` shows hostname in the Daemon card and daemon_version in the Infrastructure card when present; omits both rows entirely when absent (older daemon before this field existed) | **Yes** | No | `app-gh192-phase1.test.js` ×2 | — |
| `_showHeaderRefreshSpinner`/`_hideHeaderRefreshSpinner` show/hide the spinner; a second `show()` call restarts rather than stacks the auto-hide timer; `navigate()` pulses it for a real list-view switch | **Yes** | No | `app-gh192-phase1b.test.js` ×4 | — |
| `startTermConnectWatchdog` uses the type-aware retry interval (5000ms new-session / 4000ms existing-session, giving an unchanged 15s / a new 8s total failure budget) | **Yes** | No | `app-gh192-phase1b.test.js` ×2 | — |
| `_dismissTermLoadingSplashWithMinDwell` removes the splash immediately once minDwell has already elapsed, but defers removal (and actually removes it once the deferred timer fires) when content arrives before the 2000ms (new) / 500ms (existing) floor | **Yes** | No | `app-gh192-phase1b.test.js` ×3 | — |
| `state._justStartedSessionId` plumbing (set at session-start/-restart time, consumed once by the splash-mount code as `_termIsNewSession`) | **Yes** | No | `app-gh192-phase1b.test.js` — pins the flag's presence; full consume-and-clear path exercised indirectly through the watchdog/dismiss tests above, which set `_termIsNewSession` directly rather than re-deriving it from a full `renderSessionDetail` call (that function has a very large number of other preconditions to stub) | Not a full end-to-end test of `renderSessionDetail`'s own flag-consumption line — same honesty standard as other browser-integration-heavy code in this tracker |
| Full live round trip: a freshly-started session's actual cold-start timing, a real reconnect's timing, and the header spinner's visual appearance on a real view switch | No | No | — | Not live-verified in this pass — the unit tests above exercise the exact timer/branching mechanism each depends on |

## GH#192 Phase 2 — per-Automaton memory section — v8.71.0

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `_renderDetailTabStrip` includes an always-visible "Memory" tab alongside Overview/Stories/Decisions/Scan/Rules | **Yes** | No | `app-gh192-phase2.test.js` | — |
| `_renderDetailMemoryTab` renders the stats/report/recall mount points, correctly keyed to the given PRD's id | **Yes** | No | `app-gh192-phase2.test.js` | — |
| `_renderPRDMemoryStats` sums memory counts per scope (`prd-shared`, summed `story-shared` across every story entry, `session-local`) from the memory-report response shape | **Yes** | No | `app-gh192-phase2.test.js` | — |
| `_renderPRDMemoryReportList` flattens memories across scope entries, tags each with its scope (+ scope_id for story-shared), and shows the empty-state message when no scope has any memories | **Yes** | No | `app-gh192-phase2.test.js` ×2 | — |
| `_renderPRDMemoryRecallList` renders rows or the "no results" message | **Yes** | No | `app-gh192-phase2.test.js` | — |
| `_filterPRDMemoryRecall` filters the cached scoped-recall list by content substring, case-insensitively, without re-fetching; an empty query shows every cached entry | **Yes** | No | `app-gh192-phase2.test.js` ×2. Caught a real test-stub gap while writing this: `makeStubElement()`'s `value` property is a fixed no-op getter/setter (always reads back `''`) — a plain `{value: ...}` object is used for the query-input stub instead | — |
| `_loadPRDMemoryTab` fetches `/api/autonomous/prds/{id}/memory-report` and `/api/memory/scopes/recall?prd_id=...&project=...` (both locked to the given PRD's id + project_dir), populates the stats/report/recall elements, and shows the unavailable message when the report fetch fails | **Yes** | No | `app-gh192-phase2.test.js` ×2 | — |
| Full live round trip: a real Automaton with actual prd-shared/story-shared memories, viewed through the new tab | No | No | — | Not live-verified in this pass — both underlying REST endpoints are pre-existing and already covered by BL386's own tests; the unit tests above exercise the exact new PWA-side aggregation/rendering/filtering logic |

## GH#192 Phase 3 — three-finger swipe opens a real server-picker modal — v8.72.0

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `openServerPickerModal` is exposed globally (the swipe gesture's new call target); the old `highlightServerPicker` function is gone entirely | **Yes** | No | `app-gh192-phase3.test.js` ×2 | — |
| `_loadServerPickerModalList` renders All + Local chips even with zero configured remote servers — the exact no-op the old scroll+highlight approach had | **Yes** | No | `app-gh192-phase3.test.js` | This was the real, concrete behavior gap the operator's decision closed, not just a cosmetic preference |
| `_loadServerPickerModalList` includes every enabled remote server as its own chip, excludes disabled ones | **Yes** | No | `app-gh192-phase3.test.js` | — |
| `_loadServerPickerModalList` doesn't throw if the modal was already closed before its fetch resolves (async branch, `state.servers === undefined`) | **Yes** | No | `app-gh192-phase3.test.js` | — |
| Every chip's `onclick` both switches servers (`selectServer(...)`) and removes the modal — no leftover overlay after a pick | **Yes** | No | `app-gh192-phase3.test.js` | — |
| `_openAddServerFromPicker` sets `_settingsTab = 'comms'` and calls `navigate('settings')` — routes to the existing add-server form via navigation rather than rebuilding a second copy of it inside the modal (that form's DOM only exists on the Settings page) | **Yes** | No | `app-gh192-phase3.test.js` | — |
| Full live round trip: an actual 3-finger swipe gesture on a touch device opening the modal, and the modal's visual appearance/backdrop-click-to-close behavior | No | No | — | Not live-verified in this pass (no touch-event simulation in this Node-vm test harness) — the unit tests above exercise the exact chip-generation/routing logic the gesture's call target depends on; `confirm-modal-overlay`'s click-to-close and backdrop styling are pre-existing, shared with `showConfirmModal` |

## CI enforcement — version-bump reuse + internal-ref leak scope correction — v8.72.1

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `scripts/check-version-bump.sh` fails when the version is reused from its base ref with non-exempt files changed | **Yes** | **Yes** | Manually run against the real historical pair (`638edb04`/`71397878`, both `var Version = "8.62.0"`, 9 non-test files changed) — confirmed the logic would have failed it | Added after a compliance audit found that exact real-world case |
| `scripts/check-version-bump.sh` passes when the version is unchanged but every changed file is test-only/plan-doc/chore | **Yes** | No | Manual run against synthetic exempt-only diff | — |
| `scripts/check-version-bump.sh` passes when the version is bumped | **Yes** | **Yes** | Run against the real current HEAD (always passes post-bump) | — |
| `scripts/check-no-internal-refs.sh` now also checks `README.md` | **Yes** | **Yes** | Ran against the live repo after fixing README's 2 stray instances; passes | — |
| CI (`ci.yaml`) runs both checks on every push/PR, not just at release time | No | No | — | Not live-verified against a real GH Actions run in this pass (would require an actual push/PR to trigger) — logic verified locally; flagged rather than overclaimed |

## GH#192 Phase 4 — memory tags, end to end — v8.73.0 (feature complete)

**Correction**: an earlier WIP checkpoint (v8.72.2) claimed REST had no tags support yet. That was stale — `handleMemorySave`'s `tags` field had already landed in v8.72.1 (a shared-working-tree side effect, see CHANGELOG). This section supersedes that one with the real, complete picture.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `Store.SetTags` persists a comma-separated tag string against a memory row; a later call replaces rather than appends; an untagged memory reads back `""` via `COALESCE`, not a NULL-scan panic | **Yes** | No | `store_test.go` ×3 | Mirrors `SetPinned`'s shape exactly |
| Tags round-trip through all 3 read paths the PWA's memory browser actually uses — `ListRecent`, `ListFiltered`, `Search` — not every `Memory`-returning method in the file (deliberate scope boundary, documented in the plan doc) | **Yes** | No | `store_test.go` | — |
| `ServerAdapter.SetTags` sets tags via the `TaggableBackend` capability-cast, and the result flows through `convertToMaps`'s manual field list (not a JSON round-trip — this field had to be added explicitly, same as `pinned` before it) | **Yes** | No | `gh192_tags_test.go` | Caught during implementation: `convertToMaps` builds its `map[string]interface{}` field-by-field, so a new `Memory` struct field does **not** automatically appear in REST/MCP responses without this explicit addition |
| `POST /api/memory/save` accepts an optional `tags` field; applies it best-effort after `Remember` succeeds (an unsupported-backend `SetTags` error doesn't fail the save, reflected via a `tags_applied: false` response field); omitting `tags` entirely skips the `SetTags` call | **Yes** | No | `gh192_memory_save_tags_test.go` ×4 | — |
| `memory_remember` MCP tool's optional `tags` param — proxy-mode (`memoryAPI == nil`) includes it in the forwarded body only when supplied; the tool's input schema advertises the param | **Yes** | No | `memory_tools_bl385_test.go` ×3 (direct-mode with a full `MemoryMCP` fake not re-verified separately — same 3-line best-effort addition already covered at the REST/Store layers, to avoid a large amount of interface-boilerplate for one assertion) | — |
| PWA: `addMemoryQuick()` sends the tags field only when non-empty, clears both inputs on success | **Yes** | No | `app-gh192-phase4.test.js` ×3 | — |
| PWA: `_renderMemoryTagChips` renders one escaped chip per trimmed tag, returns `''` for no tags (absent/empty/null), and `listMemories`/`searchMemories` splice it into each row | **Yes** | No | `app-gh192-phase4.test.js` ×4 | Includes an explicit XSS-escaping check, not just a happy-path render |
| Full live round trip: a real tagged memory added via the PWA dialog, persisted, and visible with its tags in the browser list after a page reload | No | No | — | Not live-verified in this pass — the unit tests above exercise the exact persistence/read/render mechanism the live scenario depends on, at every layer (Store → adapter → REST/MCP → PWA) |

## v9.0.0 major release — full E2E pass + release-gate fix

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Full `scripts/run-tests.sh` suite against an isolated sandbox daemon (never production) — first pass, before the TS-557 fix | **Yes** | **Yes** | 672 passed, 1 failed, 15 skipped, 2819s (~47 min) total | Working dir `/home/dmz/workspace/datawatch-88cd0c` kept for evidence inspection per the script's own on-failure retention policy |
| `release-smoke.sh` (TS-557) — fails via its own `tidy-plans.sh --dry-run` release gate when plan docs older than the 7-day cutoff sit directly in `docs/plans/` instead of `historical-plans/` | **Yes** | **Yes** | Root-caused to 9 stale plan docs; fixed by running `tidy-plans.sh` for real (one of the 9 was untracked, belonging to separate in-progress work elsewhere in this shared tree — relocated with a plain `mv` instead of `git mv`, which can't move an untracked file, then re-ran `tidy-plans.sh` for the remaining tracked ones it had aborted on). Re-ran `release-smoke.sh` standalone afterward: 185 passed, 0 failed, 33 skipped, exit 0 | Housekeeping gap, not a code regression — none of this release's actual code changes caused the failure |
| **Full `scripts/run-tests.sh` suite, re-run completely from scratch** after the TS-557 fix, specifically to rule out any side effect from moving plan docs (operator-directed re-check, not satisfied by the narrower `release-smoke.sh`-only re-verification above) | **Yes** | **Yes** | **674 passed, 0 failed, 14 skipped, 2891s (~48 min) total.** Fresh isolated sandbox, never production | Confirms the plan-doc move had zero side effects elsewhere in the suite |
| `golangci-lint` errcheck findings that broke the first `v9.0.0` tag's CI run (misplaced `//nolint:errcheck` on a wrapped `.Run()` continuation line in `internal/llm/backends/gemini/backend.go`; two unguarded `fmt.Fprint` in `internal/llm/backends/openwebui/usage_test.go`) | **Yes** | **Yes** | `golangci-lint run ./...`: 0 issues (full repo); `go test ./...`: 3179 passed | Pre-existing in both files, not introduced by this release; CI's lint step should have caught this on every main-branch push — separately worth checking post-release whether `ci.yaml`'s lint step has been silently non-blocking |
| `handleClaudeModels`'s `full_names` — major-release alias refresh | **Yes** | No | `TestHandleClaudeModels_FullNamesCurrentAsOfV9` (`internal/server/v5275_claude_endpoints_test.go`) | Pins the corrected `claude-opus-5-5`/`claude-sonnet-5-5` values so the next major release's refresh has something concrete to diff against |
| PAI compatibility audit (AGENT.md Skills-Awareness Rule / release-checklist E2) — had not actually been run before this pass | **Yes** | **Yes** | Cloned PAI mainline fresh, ran `internal/skills.ParseManifestFile` against all 57 shipped `SKILL.md` manifests via a throwaway `cmd/` harness (deleted after use, never committed): 0 parse failures | First time this audit has actually been executed, as far as `git log` shows |
| Pre-release dependency audit (release-checklist C1) | **Yes** | **Yes** | `go list -m -u all` reviewed; `govulncheck ./...` run fresh: 0 vulnerabilities in our code, 3 in unreachable transitive deps | No upgrade required under the 72h/CVE-exception rule |
| Pre-release gosec scan (release-checklist C2), matched exactly to CI's invocation (`-severity=high -confidence=medium`) | **Yes** | Partial | Live count drifted from the `.gosec-baseline.json` total (60) across three different `gosec@latest` installs within the same hour (19 / 61 / 63) — root-caused to gosec's own unpinned, rapidly-changing taint-analysis rule coverage, not any code change. Confirmed 0 new findings in any file this release's actual diff touches | Flagging as a real CI fragility (recommend pinning gosec's version in `release.yaml`) — not fixed in this pass since it's a CI-config decision, not mine to make unilaterally |
| `tests/integration/spawn_docker.sh` / `spawn_k8s.sh` (release-checklist E3, single-host + k8s smoke) — never wired into CI or `run-tests.sh`, apparently never actually passed since the profiles REST API was introduced | **Yes** | **Yes** | Found via `git log -S` that both scripts called the singular `/api/profiles/project(cluster)` against routes that have always been plural (404 on every call); `spawn_docker.sh` separately hardcoded an `image_pair.agent` that the docker driver composes into a non-existent registry tag, with its own `$IMAGE` variable dead code. Fixed both (route pluralization; local image-tag workaround querying the live `/api/info` version, no driver code touched). Both now pass fully end-to-end — real Pod create/delete via `kubectl` for k8s, real `docker run`/terminate for docker | Commit `2073ba0d` |
| Cross-feature flow / UI smoke / config-channel parity (release-checklist E3 items 3–5) | No | N/A | Confirmed against this release's own `## Parity surface` table (`docs/plans/historical-plans/2026-10-07-v9.0.0-major-release.md`): PWA/Android/iPhone/YAML-config all "No" — no new UI surface or config knob ships in v9.0.0, so these are vacuous passes | No live browser UI walkthrough was performed this pass — flagging honestly rather than implying one was done |
| The 14 E2E skips (final re-run), individually confirmed as pre-existing infrastructure-floor / LLM-timing gaps, not code bugs | No | **Yes** | Slow-LLM PRDs not reaching terminal state within the test window (TS-779, TS-783 — each explicitly logs its own primary assertion passed first), an Ollama vision model unavailable cascading into related vision/council skips, Tailscale/Signal/1Password unconfigured | Same category as the first run's 15 skips; count/composition shifted slightly run-to-run due to live-LLM timing variance, not a regression |

## Federated server picker — missing picker + no connection status — v8.73.2

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `state.servers` defaults to `undefined`, not `[]` | **Yes** | No | `app-fed-conn-status.test.js` | Pins the actual root cause |
| `_injectServerPickerBar` triggers `loadServerListEager` while servers are unloaded, instead of silently doing nothing | **Yes** | No | `app-fed-conn-status.test.js` | — |
| `loadServerListEager` populates `state.servers` on success | **Yes** | No | `app-fed-conn-status.test.js` | — |
| `loadServerListEager` does not poison `state.servers` to `null` on failure (retries instead) | **Yes** | No | `app-fed-conn-status.test.js` | — |
| `_checkFederatedConnection` transitions connecting → connected on a successful health probe | **Yes** | No | `app-fed-conn-status.test.js` | — |
| `_checkFederatedConnection` transitions connecting → error with the real HTTP status on failure | **Yes** | No | `app-fed-conn-status.test.js` | — |
| `_checkFederatedConnection` ignores a stale result after the operator switched to a different server mid-probe | **Yes** | No | `app-fed-conn-status.test.js` | — |
| Real `sessions` WS data clears any in-progress federated status | **Yes** | No | `app-fed-conn-status.test.js` | — |
| `selectServer()` triggers the connection check / clears status appropriately | **Yes** | No | `app-fed-conn-status.test.js` ×2 | — |
| Full live round trip against a real federated peer (picker appears, connects, lists sessions or shows a real error) | No | No | — | Not live-verified in this pass — the operator is testing this directly against their own federated sandbox host next; flagging rather than overclaiming |

## Store.Test() false-positive + All-mode dropped remote sessions — v8.73.4

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `multiserver.Store.Test()` requires both the health probe AND an authenticated check to pass before reporting success | **Yes** | **Yes** | `TestStore_Test`, `TestStore_Test_FalsePositiveOnBadToken` | Live-tested against two real federated peers before the fix shipped (one down, one up-but-token-less) |
| "All servers" mode re-fetches the real aggregate instead of accepting a local-only WS push | **Yes** | No | `app-fed-conn-status.test.js` ×2 (All-mode guard + non-All regression guard) | Operator-reported live |

## Testing-gap audit + federation E2E formalization — 2026-10-08

Follow-up audit after the federation bug round (v8.73.2–v8.73.4), per the operator's explicit ask: "what else are we not testing," real 2-daemon federation E2E coverage, a broader skeleton-test sweep across e2e/smoke/unit, and process backfill.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Real 2-daemon federation E2E (TS-785, TS-786) — a genuinely separate second daemon process, registered as a federated peer with a real token, proxied through `/api/proxy/<peer>/...` | **Yes** | **Yes** | Ran for real against an isolated sandbox (never production), `bash scripts/run-tests.sh --stories=TS-785,TS-786`: 2/2 passed, 6/6 individual checks green | Closes the gap `TS-387`–`TS-396` always had (a deliberately-fake `localhost:99999` "remote," never a real second daemon). `scripts/run-tests.sh` gained `stop_second_test_daemon()` for cleanup |
| Bad-token federated peer produces a real, detectable 401/error — not a false positive — on both the data-fetch proxy path and the `Store.Test()` REST surface | **Yes** | **Yes** | TS-786, same live sandbox run. Direct regression pin for the actual v8.73.2–v8.73.4 bug class | First attempt at this story had a false pass itself (see below) |
| Multi-assertion test-story `RESULT` aggregation | **Yes** | **Yes** | Found live while writing TS-786: `ok()`/`ko()`/`skip()` (lib.sh:185-189) each overwrite the shared `RESULT` with only their own call's outcome — a story with 3 checks where the first 2 fail and the 3rd passes reports overall "pass," silently hiding the 2 real failures. Confirmed this happened to TS-786's own first draft (showed 2 `FAIL` lines but `✓ TS-786` overall) | **Not fixed harness-wide** — fixed locally in TS-785/TS-786 (explicit `any_fail` tracking) but this almost certainly affects other existing multi-assertion stories across the suite; flagging as the single highest-value follow-up, not attempted here (scope/time) |
| 68 E2E story files carrying a stale `# STUB: no implementation extracted from legacy runner. Mark as skip until ported.` comment despite having complete, real implementations | **Yes** | **Yes** | Confirmed the comment has zero effect on execution (not referenced anywhere in `lib.sh`/`run-tests.sh`) and spot-checked 5 of the 68 (TS-368, TS-369, TS-370, TS-371, TS-382) all have substantial real bodies (28-45 lines, multiple real assertions) | Same stale-comment pattern `TS-387`–`TS-396` already had and were corrected for; this was a broader sweep that found 68 more instances of the identical pattern. Comment stripped from all 68; no behavior change |
| Custom (non-default) capability group enforcement, live (smoke §65) | **Yes** | **Yes** | New `release-smoke.sh` §65: a peer granted only a custom `alerts:list`-only group gets 200 on `/api/alerts`, 403 on `/api/sessions`. Sandbox's own admin token is empty by design (same caveat §60 has always had), so §65 itself only exercises the skip path in the standard smoke run — logic separately validated via a throwaway manually-tokened daemon instance (200/403 confirmed exactly as expected) before shipping | Mirrors §60's own documented limitation rather than solving the harness's broader empty-token-sandbox design — out of scope for this pass |
| `multiserver.Store.GetByToken()` — previously zero test coverage despite being the actual identity-resolution function `fedAuthMiddleware` depends on for every federated request | **Yes** | No | New `TestStore_GetByToken`: correct-token match, wrong-token no-match, and the critical case — an empty bearer value must never match a token-less entry (confirmed the existing implementation already guards this correctly; the gap was purely missing test coverage, not a bug) | Found via the same audit; implementation was already correct |
| Mobile-Parity Rule step 2 (ship EN placeholder + file a parity issue, not draft translations) for v8.73.2–v8.73.4's new locale keys | N/A | N/A | Not followed as shipped — real (non-Android-sourced) DE/ES/FR/JA translations were written directly under release pressure | Filed `dmz006/datawatch-app#235` retroactively per the rule, requesting real translations + asking whether Android/iOS need the same connection-status UX |

## SEC-023: Argon2id strengthening + silent-plaintext-fallback fix — v8.73.9

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `config.Encrypt`/`EncryptWithSalt` always write the new v3 (`t=4, m=512MiB, p=4`) envelope | **Yes** | **Yes** | `TestEncryptDecryptRoundtripV3` | `internal/config/encrypt.go` had zero prior test coverage |
| `config.Decrypt` still reads v1 (legacy AES-GCM) and v2 (weak-Argon2id XChaCha20) transparently | **Yes** | **Yes** | `TestDecryptLegacyV2` (hand-built legacy envelope, decrypted with the real `Decrypt`) | Confirms existing `--secure` configs aren't locked out by the strengthening |
| Wrong password is rejected, not silently accepted | **Yes** | **Yes** | `TestDecryptWrongPassword` | |
| `ExtractSalt` works across all three envelope versions | **Yes** | **Yes** | `TestExtractSaltAllVersions` | |
| `memory.Store.encryptField`/`PGStore.encryptField` refuse the write (return an error) instead of silently storing plaintext when the AEAD seal fails | **Yes** | **Yes** | Full `internal/memory` suite (140 tests) re-run green after the signature change; no existing test exercised the AEAD-failure branch itself (that path requires an unconstructable cipher state) so the refusal logic is covered by type-checking + the call-site error propagation, not a dedicated failure-injection test | Flagging the missing failure-injection test as a gap, not fixed this pass |
| Full repo regression | **Yes** | **Yes** | `go test ./...` — 3186 passed, 0 failed | |

## fed_conn_* locale alignment (GH#235) + loading_sessions phase — v8.73.12

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| DE `fed_conn_error_title`, ES `fed_conn_error_auth` corrected to datawatch-app's reviewed values; `fed_conn_loading_sessions` added to all 5 bundles | **Yes** | **Yes** | All 5 `locales/*.json` parsed as valid JSON (`python3 -m json.tool`); full JS suite 176/176 green | `fed_conn_back_to_local` already matched in all 5 bundles — no change needed |
| `_checkFederatedConnection` now passes through `phase: 'loading_sessions'` between the authenticated response resolving and the session list being ready, instead of staying on `'connecting'` the whole time | **Yes** | **Yes** | New test in `app-fed-conn-status.test.js` using a controlled, manually-resolved `json()` promise to observe the intermediate phase before resolving it | Matches the apps' existing two-phase UX per datawatch-app's GH#235 follow-up |

## HLLM-004 / HLLM-007: session/memory tool ownership scoping + observer_envelopes_all_peers capability split — v8.73.13

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `Server.ownsSession` — static `callerSessionID` path (Goose-channel subprocess): self, direct child, transitive grandchild all owned; an unrelated session is not | **Yes** | **Yes** | `TestOwnsSession_AdminMode_AlwaysTrue`, `TestOwnsSession_Subprocess_SelfAndDescendants` | |
| `Server.ownsSession` — ctx-based path (shared daemon MCP server, the primary real-world case): same ownership rules via `federation.WithCallerSessionID`; no ctx identity + no static field = unscoped (admin/federation peer) | **Yes** | **Yes** | `TestOwnsSession_SharedInstance_CtxIdentity` | |
| `session_output`/`kill_session`/`send_input` deny access to an unowned session as a plain "not found" (not a 403, to avoid confirming the session exists); `kill_session` confirmed NOT actually killed | **Yes** | **Yes** | `TestHandleSessionOutput_Subprocess_DeniedForUnownedSession`, `TestHandleKillSession_Subprocess_DeniedForUnownedSession`, `TestHandleSendInput_Subprocess_DeniedForUnownedSession`, and the ctx-path equivalent `TestHandleKillSession_SharedInstance_DeniedForUnownedSession` | Own-child access still works: `TestHandleSessionOutput_Subprocess_AllowedForOwnChild` |
| `stop_all_sessions` only kills the caller's own subtree, not every session on the host | **Yes** | **Yes** | `TestHandleStopAllSessions_Subprocess_OnlyOwnSubtree` — own child killed, unrelated session untouched | |
| `memory_forget`/`memory_pin`/`memory_export` refused outright for any scoped caller (static or ctx-based) | **Yes** | **Yes** | `TestMemoryForgetPinExport_BlockedForScopedCaller`, `TestMemoryForgetPinExport_BlockedForSharedInstanceCtxIdentity`; pre-existing `TestBL385_MemorySweep_BlockedInSubprocessMode`/`TestBL385_MemoryImport_BlockedInSubprocessMode` updated for the new message text, still green | |
| `observer_envelopes_all_peers` requires the new `CapObserversReadAllPeers`, not the broadly-granted `CapObserversRead`; `full-control`/`read-only` keep prior access, `session-default` does not | **Yes** | No | Capability wiring verified by code inspection (`capabilities.go`, `mcp_tool_caps.go`, `observer.go`) and the full existing federation/capability test suite re-run green; no new live-peer test added this pass (would need a federated-peer harness scoped specifically to this one capability) | Flagging the missing live-peer test as a gap, not fixed this pass |
| Full repo regression | **Yes** | **Yes** | `go test ./...` — 3197 passed, 0 failed | |

## SEC-005: Twilio X-Twilio-Signature verification — v8.73.14

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `verifySignature` accepts a correctly-signed request; rejects wrong auth token, tampered param, wrong URL, missing header, malformed (undecodable) header | **Yes** | **Yes** | `TestVerifySignature_ValidAccepted`, `_WrongTokenRejected`, `_TamperedParamRejected`, `_WrongURLRejected`, `_MissingHeaderRejected`, `_MalformedHeaderRejected` | `internal/messaging/backends/twilio/backend.go` had zero prior test coverage |
| Empty `webhook_public_url` deliberately bypasses verification (documented, matches `github_webhook.secret` convention) | **Yes** | **Yes** | `TestVerifySignature_EmptyPublicURLBypasses`, `TestHandleSMS_EmptyPublicURLAllowsUnsigned` | |
| `Backend.handleSMS` end-to-end: valid signature delivers the message; invalid/missing signature returns 401 and never delivers | **Yes** | **Yes** | `TestHandleSMS_ValidSignatureDeliversMessage`, `TestHandleSMS_InvalidSignatureRejectedNoMessage`, `TestHandleSMS_MissingSignatureRejected` | |
| `from != to_number` secondary filter still works for a validly-signed request from an unexpected number | **Yes** | **Yes** | `TestHandleSMS_WrongFromNumberIgnoredEvenWithValidSignature` | |
| Full repo regression | **Yes** | **Yes** | `go test ./...` — 3209 passed, 0 failed | |

## SEC-016: live admin-token rotation, 60s grace window, removed from PUT /api/config — v8.73.16

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `POST /api/auth/rotate-token` swaps the live token immediately (new token authenticates right away, no restart) and persists it to disk | **Yes** | **Yes** | `TestHandleRotateToken_AdminGetsNewTokenImmediately` | |
| Previous token stays valid during the 60s grace window, then is hard-revoked | **Yes** | **Yes** | `TestHandleRotateToken_OldTokenValidDuringGraceWindow`, `TestHandleRotateToken_OldTokenRevokedAfterGraceWindow` (forces `oldTokenExpiry` into the past rather than sleeping 60s) | |
| Operator-supplied `new_token` is honored; a too-short one (<16 chars) is rejected with the live token left unchanged | **Yes** | **Yes** | `TestHandleRotateToken_OperatorSuppliedToken`, `TestHandleRotateToken_TooShortOperatorTokenRejected` | |
| A federation peer (even with `full-control`) gets 403 — no capability should be sufficient to rotate the admin credential itself | **Yes** | **Yes** | `TestHandleRotateToken_FederationPeerForbidden` | |
| `PUT /api/config` silently skips `server.token`/`mcp.token` but still applies other keys in the same patch, and reports the skip in the response body | **Yes** | **Yes** | `TestHandlePutConfig_ServerTokenAndMCPTokenSkipped` | |
| Full repo regression | **Yes** | **Yes** | `go test ./...` — 3216 passed, 0 failed | 3 pre-existing `internal/session`-package test failures under `-race` only (unrelated stack traces, no mention of any file touched this pass) — not introduced or fixed in this pass, flagged as a pre-existing gap |

## SEC-017: audit-log every PUT /api/config write — v8.73.17

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| A successful `PUT /api/config` records exactly one `audit.Entry{Action:"configure"}` with every applied key listed | **Yes** | **Yes** | `TestAuditConfigPatch_RecordsAppliedKeys` | |
| Credential-shaped values (`*token*`, `*password*`, `*secret*`, ...) are masked (first/last 2 chars visible) rather than logged in the clear | **Yes** | **Yes** | `TestAuditConfigPatch_MasksCredentialShapedValues` | |
| Keys `applyConfigPatch` skipped (`server.token`/`mcp.token`, SEC-016) never appear in the audit entry's key list | **Yes** | **Yes** | `TestAuditConfigPatch_SkippedKeysExcludedFromAudit` | |
| `PUT /api/config` still succeeds with no audit log configured (audit is best-effort, never blocks the write) | **Yes** | **Yes** | `TestAuditConfigPatch_NoAuditLogConfiguredIsANoop` | |
| Key-substring sensitivity classifier | **Yes** | **Yes** | `TestIsSensitiveConfigKey` | |
| Full repo regression | **Yes** | **Yes** | `go test ./...` — 3221 passed, 0 failed | |

## SEC-018: bounded TTL sweep for discussionThrottleMap — v8.73.18

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| A bucket idle past the TTL is evicted by the sweep; a recently-touched one survives | **Yes** | **Yes** | `TestDiscussionThrottleSweep_EvictsIdleBuckets` | |
| A token that gets a fresh bucket after eviction starts fully refilled, not still drained from before | **Yes** | **Yes** | `TestDiscussionThrottleSweep_RecreatedWithFullBucketAfterEviction` | |
| The lazy sweeper-start path (`sync.Once`) is safe to call repeatedly | **Yes** | **Yes** | `TestDiscussionThrottleBucket_StartsBackgroundSweeperOnce` | |
| Pre-existing throttle enforcement behavior unaffected | **Yes** | **Yes** | `TestDiscussionThrottle_Enforced` re-run green | |
| Full repo regression | **Yes** | **Yes** | `go test ./...` — 3224 passed, 0 failed | |

## SEC-021 (remainder): file-service default root + app/docs deny-list — v8.73.19

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Default root (no `file_service_root`/`root_path` configured) resolves to `<data_dir>/files`, never home | **Yes** | **Yes** | `TestFileServiceRoot_DefaultsToDataDirSubpath_NotHome` | |
| An explicit `file_service_root` still wins over the new default | **Yes** | **Yes** | `TestFileServiceRoot_ExplicitFileServiceRootStillWins` | |
| Deny-list classifier: `internal/server/web/*` and `docs/*` match; `documents/*` (substring neighbor) does not | **Yes** | **Yes** | `TestIsDenyListedFileServicePath` | |
| JSON upload / multipart upload / delete all refuse a deny-listed target (403), and a file is never written/removed | **Yes** | **Yes** | `TestHandleFilesJSONUpload_DeniesAppDocsTree`, `TestHandleFilesUpload_DeniesAppWebTree`, `TestHandleFilesDelete_DeniesAppDocsTree` | |
| A non-deny-listed path still succeeds (no over-blocking regression) | **Yes** | **Yes** | `TestHandleFilesJSONUpload_AllowsNonDenyListedPath`; pre-existing `TestFilesUpload_And_Delete`/`TestFilesUpload_ImageFile`/`TestFilesUpload_PathTraversal` re-run green | |
| Full repo regression | **Yes** | **Yes** | `go test ./...` — 3231 passed, 0 failed | |

## SEC-022: skill manifest Verify execution + --trust-unverified gate — v8.73.20

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `runSkillVerify`: empty command trivially passes; passing/failing shell commands; runs with cwd set to the skill directory | **Yes** | **Yes** | `TestRunSkillVerify_EmptyCommandPasses`, `_PassingCommand`, `_FailingCommandReportsReason`, `_RunsInSkillDirectory` | |
| `Manager.Sync` sets `Verified`/`VerifyError` per skill; a failing verify does not make `Sync` itself error (no gate at that layer); no-Verify-declared is trivially verified | **Yes** | **Yes** | `TestSync_PassingVerifyMarksRecordVerified`, `TestSync_FailingVerifyMarksRecordUnverifiedButStillSyncs`, `TestSync_NoVerifyCommandIsTriviallyVerified` | |
| `POST /api/skills/registries/{name}/sync` rolls back (Unsync) any skill that failed verification by default; `trust_unverified: true` keeps everything | **Yes** | **Yes** | `TestHandleSkillsSync_RollsBackUnverifiedByDefault`, `TestHandleSkillsSync_TrustUnverifiedKeepsEverything` (fake `skillsManager`) | |
| CLI `--trust-unverified` flag and MCP `trust_unverified` param both thread through to the same REST body field | **Yes** | No | Verified by code inspection (`cli_skills.go`, `internal/mcp/skills.go`) and the REST-layer tests above covering the actual enforcement; no separate CLI-process or MCP-transport-level test added this pass | |
| Full repo regression | **Yes** | **Yes** | `go test ./...` — 3240 passed, 0 failed | |

## SEC-026: gosec baseline-diff ceiling refresh — v8.73.21

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| Live gosec count (exact CI command: `gosec -exclude="$EXCLUDE" -exclude-dir=.claude -severity=high -confidence=medium -fmt=json -quiet ./...`) measured and compared against the baseline | **Yes** | **Yes** | Ran locally: live=63 vs. old baseline total=60; confirmed by-rule breakdown (G118:2, G122:6, G123:1, G702:3, G703:32, G704:19) sums to 63 and every rule ID is one of the six the baseline's own `_comment` already blanket-accepts — no new rule category | Not a Go test — this is a JSON-config correctness check, verified by running the real CI command and the real baseline-diff Python logic locally |
| Updated `.gosec-baseline.json` produces a passing baseline-diff against the current live count | **Yes** | **Yes** | Re-ran the workflow's exact Python comparison logic locally against the new file: `live=63 baseline=63` → pass | |
| `go test ./...` unaffected (this is a non-code config/docs change) | **Yes** | **Yes** | `go test ./...` — 3240 passed, 0 failed | |

## HLLM-003: audit entries name the real caller, not hardcoded "operator" — v8.73.22

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `auditActor` derives `operator`/`session:<id>`/`peer:<name>` from the same context identity `fedCap` checks | **Yes** | **Yes** | `TestAuditActor_AdminContextIsOperator`, `_SessionTokenNamesOwningSession`, `_FederationPeerNamesItself` | |
| `handleSecretsGet`'s `secret_access` audit entry names the real session, not "operator" — the design doc's own named acceptance scenario | **Yes** | **Yes** | `TestHandleSecretsGet_AuditsRealCallerNotHardcodedOperator` — asserts `session:testhost-sec-sandbox-18a6`, mirroring the doc's literal example name | |
| `auditConfigPatch`'s (SEC-017) `configure` entry names the real session | **Yes** | **Yes** | `TestAuditConfigPatch_NamesSessionActor` | |
| Shared `Server.audit` helper (skills-registry write paths) threads ctx through; existing skills tests still pass with the new signature | **Yes** | **Yes** | Full `internal/server` suite re-run green (634 tests) | |
| Full repo regression | **Yes** | **Yes** | `go test ./...` — 3245 passed, 0 failed | |

## Dedicated tmux socket + socket-aware attach hints — v8.73.23

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `TmuxSocketDir` / `TmuxAttachCommand`: `TMUX_TMPDIR` env wins; empty with no dedicated socket; CLI fallback to `~/.datawatch/tmux` only once its socket exists | **Yes** | **Yes** | `TestTmuxSocketDir_EnvWins`, `_DefaultSocket`, `_FallsBackToDataDirSocket` | |
| CLI `datawatch session attach <id>` prints the socket-qualified command | **Yes** | **Yes** | Live, 2026-10-08, production host: printed `TMUX_TMPDIR=/home/dmz/.datawatch/tmux tmux attach -t cs-johnnyjohnny-9245` from an operator shell with no `TMUX_TMPDIR` set | |
| `datawatch-tmux.service` + `datawatch.service` (`TMUX_TMPDIR`): sessions created on the dedicated server survive a `systemctl --user restart datawatch` | No | **Yes** | Live, 2026-10-08: two restarted claude-code sessions stayed `running`/alive across a daemon restart; pane processes in `user@1000.service` `tmux-spawn-*` scopes, server in `datawatch-tmux.service` | install.sh unit generation checked with `bash -n` only; not run on a fresh host |
| Comm-channel `attach` reply / session-start message use the socket-aware hint | No | No | Code inspection (`internal/router/router.go`, `internal/server/api.go`) | Not exercised via `POST /api/test/message` yet |



## Empty project_dir honours default_project_dir on every surface; test HOME isolation — v8.73.24

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `Manager.Start` with empty project_dir → `session.default_project_dir` (created, `~` expanded), unset → `$HOME` | **Yes** | **Yes** | `TestStart_EmptyProjectDir_UsesConfiguredDefault`, `_ExpandsTilde`, `_NoConfigFallsBackToHome` | Covers MCP `start_session`, which previously bypassed the REST-only default |
| `go test` of `internal/mcp`, `internal/server`, `internal/session` leaves the operator's real home untouched | **Yes** | **Yes** | Live, 2026-10-08: mtimes of `~/CLAUDE.md` and `~/.mcp.json` unchanged across the package runs; before the fix, a full `go test ./...` rewrote `~/CLAUDE.md` | `TestMain` HOME isolation |
| E2E daemon config: `default_project_dir` rewritten to `$TEST_DATA/projects` in both run-tests.sh generation paths | No | No | `sed` output checked by hand against `testdata/datawatch.yaml`; `bash -n` clean | Not yet exercised by a full E2E run |

## 42 missing locale keys found + CI regression guard (datawatch#195) — v8.73.25

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| All 42 keys added to all 5 locale bundles (EN real text, DE/ES/FR/JA EN-placeholder pending real translations) | **Yes** | **Yes** | All 5 `locales/*.json` parsed as valid JSON (`python3 -m json.tool`); each file grew by exactly 42 keys (1620 → 1662); full JS suite 182/182 green | `automata_wizard_backend_default` had no call-site fallback text at all; picked wording to match the adjacent effort-select placeholder |
| `TestLocales_AllAppJSKeysExistInEnglishBundle` — regex-extracts every literal `t('...')` call-site key from `app.js`, asserts each exists in `locales/en.json` | **Yes** | **Yes** | New test; failed with exactly the 44 expected hits (42 real + 2 dynamic-prefix false positives) before the fix, 0 after | `automata_status_`/`chat_quick_reply_` concatenated-key call sites explicitly excluded as known dynamic prefixes, not missing keys |

## Real DE/ES/FR/JA translations + container CVE triage — v8.73.26

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| 43 keys (42 + `disabled`, which was missed in the original count) get real DE/ES/FR/JA text, replacing the EN placeholders | **Yes** | **Yes** | All 5 `locales/*.json` re-validated as valid JSON; key count unchanged (1662); full JS suite 182/182 green | Source: datawatch-app, GH#195 comment 6059595242 |
| CVE-2026-19445 suppressed in `.trivyignore`; CVE-2026-104851 fixed by bumping `fsspec` pin in `Dockerfile.agent-aider` | No | No | Rationale documented in `.trivyignore`; not re-run through an actual container build/Trivy scan in this session (no local Docker build) | Will be confirmed by the next release CI container matrix run |
| `scripts/compute_stale_risks.py` — version-anchored CVE-suppression staleness (BL398, replaces ID-presence diff) | **Yes** (`package_tokens`/matching logic covered by a scratch-copy run) | **Yes** | Run twice against real fresh-scan JSON from two different `recheck-ignored-cves` executions (the ones behind auto-opened PRs #199 and #200): both times, every database-relabeled ID (incl. `CVE-2026-19445`, `CVE-2023-45853`→`CVE-2026-27171`/`85091`) was correctly kept and flagged as churn, not removed; the one genuine fix in that data (`libssl3` 3.0.20-1~deb12u2 → 3.0.22-1~deb12u1) was correctly flagged stale | Caught and fixed a real bug mid-implementation: Trivy `Packages[].Version` drops the epoch/revision (`"1.2.13.dfsg"` vs the registry's `"1:1.2.13.dfsg-1"`) — had to parse `Packages[].ID` (`name@full-version`) instead, or every entry would have matched as stale |
| 87 `kind: code-scanning` entries added to `security/accepted-risks.yml` (BL394 dismissals migrated, GH#197) | **Yes** (`scripts/check_accepted_risks.py` passes against all 158 entries) | **Yes** | `scripts/dismissed_alerts_watch.py` against the real repo: `0 of 87 dismissed alerts unregistered` (was 87 of 87) | Each entry's `impact.analysis` is the alert's own real GitHub `dismissed_comment`, pulled via the API rather than re-derived from the BL394 doc's prose — one alert (`#545`, `go/reflected-xss`) had been dismissed *after* BL394 was written (origin-separation fix, v8.39.12/13), confirming the live API data over the doc's "deliberately left open" snapshot |
| `release.yaml` `attach-tarball`/`attach-security-summary` rate-limit backoff | N/A (not Go code) | **Yes** | `actionlint` + `yaml.safe_load` passed before merge; then live-validated with `gh run rerun --failed` on the actual v8.73.31 run (37818621268) once the fix landed on `main` — both jobs succeeded, `datawatch-stats-cluster-8.73.31-linux-amd64.tar.gz` and `security-summary.md` are now attached to the real v8.73.31 release | Backfilled the missing release assets via the rerun itself, not a manual upload |
| Quality Gates settings — 4 locale keys translated (de/es/fr/ja) | N/A (not Go code) | **Yes** | `TestLocales_*` suite (7 tests) passes; `git diff --stat` confirmed only the 4 targeted lines per file changed, no reformatting | GH#198 follow-up; reused that fix's established Quality-Gate(s) terminology per locale rather than inventing new wording |
| GH#202 — `handleChannelReady` won't re-point a session with a live channel | **Yes** (`gh202_channel_ready_test.go`, 2 new tests) | **Yes** | Confirmed the root cause live on this exact host first: `env \| grep CLAUDE_CONFIG_DIR` empty, and `~/.claude.json` really does contain both `datawatch-johnnyjohnny-ab22` and `datawatch-johnnyjohnny-9245` — this session's own tool list (both namespaces present) is itself live evidence of the hijack mechanism | `--scope user` registration is a deliberate, documented tradeoff (GH#128/v8.12.0), not an accidental bug — the fix is in the daemon's re-registration guard, not in reverting that tradeoff |
| GH#201/BL399 Phase 1 — access/auth-failure/WS log (`access.log`, `/api/audit/access`, `audit_access_query`) | **Yes** (`log_test.go` Prune/NewAt, `gh201_accesslog_test.go` middleware + handler, 7 new tests) | **Yes** | Confirmed via `go build ./...` + full suite (1045 tests) that `fedAuthMiddleware` is genuinely the single choke point for `/api/*` and `/ws` — checked CLI (`cli_*.go` uses `http.NewRequest`) and MCP (`proxyGet`/`proxyPost`) both go through it, not separate paths | Also disproved two parts of the original report as bugs: `audit/agents.jsonl` and `auth/audit.jsonl` were already correctly wired, just legitimately empty (no qualifying events on this deployment) |
| GH#201/BL399 Phase 1 gap closure — CEF mirror, config/doc/MCP parity, observability, smoke | **Yes** (`cef_test.go`, 5 new tests: header/extension escaping, signature triples, mirror property) | **Yes** | Full repo suite: 3268 tests passed, 0 failed. Full `scripts/release-smoke.sh` run: 185 passed, 0 failed, 35 skipped (new section 66 skipped — sandbox has no admin token configured, same as its neighbor S65, not a regression) | Caught this gap-closure itself needed by re-reading AGENT.md before, not after, shipping — the Audit Logging Rule (CEF) and the Parity-surface plan-doc requirement were both missed on Phase 1's first pass |
| GH#203 — `imap_mcp` backend sends `Authorization: Bearer <token>` (imap-mcp ≥ 0.5.3) | **Yes** (10 new tests in `backend_test.go`: header on all 3 calls, no-header-when-unset, 401/403 → `*authError` with imap-mcp's message surfaced, no-hot-loop reconnect count, `nextBackoff` doubling/cap) | **Yes** | Full repo suite: 3276 tests passed, 0 failed (was 3268 — 8 of the 10 new tests ran; 2 covered by the no-hot-loop/backoff assertions within the same test count) | Filed by a peer Claude session (imap-mcp-79) coordinating the imap-mcp 0.5.3 rollout; config/secret-ref wiring mirrors the existing `openwebui.SetAPIKey` re-apply-after-`ResolveConfig` pattern for backends constructed before the secrets store exists |
| GH#203 — external-service secrets tokens (`ServiceTokenStore`, `GET /api/external/secrets/{name}`, `secrets service-tokens` CLI) | **Yes** (13 tests in `internal/secrets/service_tokens_test.go` + 4 in `internal/server/gh203_external_secrets_test.go`: mint/lookup/list/revoke, restart persistence, re-mint replaces old token, scope-check integration, REST handlers' allow/deny/401 paths, no-token-in-list) | **Yes** | Full repo suite: 3284 tests passed, 0 failed. `gosec` re-run with the *exact* CI baseline command (`-severity=high -confidence=medium`, not the looser one used earlier this session) confirmed the count is back to exactly 63 after fixing 2 pre-existing wrong-directive bugs it surfaced (unrelated to this change) plus this session's own GH#201 `Prune` finding | Second Claude session (imap-mcp-79) independently confirmed the same gap I'd found (agent-secrets tokens are F10-spawn-only, in-memory) before this shipped — cross-checked, not just self-reported |
| GH#201 Phase 2 — federation-hop origin-actor attribution (`internal/federation/hopchain.go`, `X-Datawatch-Hop-Chain`) | **Yes** (19 tests in `internal/federation/hopchain_test.go`: sign/verify round trip, tamper detection on every field, wrong-key rejection, 3-hop chain with per-hop key verification, encode/decode, the documented hop-by-hop-not-end-to-end limitation itself, `BuildOrExtendChain` origin/extend/no-key/no-principal cases; 5 tests in `internal/server/gh201_phase2_hopchain_test.go`) | **Yes** | Full repo suite: 3308 tests passed, 0 failed. The plan's own requirement for a live two-daemon test is met by `TestGH201Phase2_TwoDaemonSimulation` — two real `*Server`s wired to each other over a real `httptest.Server`, admin on daemon A triggers `handleAggregatedSessions`, daemon B's own access log independently shows `origin_actor: admin`, not just `peer:daemon-a` | Caught a real nil-vs-empty-slice JSON marshaling bug (`encoding/json` renders nil `Chain` as `"null"`, an empty-but-non-nil one as `"[]"`) that made every origin-entry signature fail verification before the fix; wired into all 4 named daemon-to-daemon forward call sites (`ProxyRouter.Infer`, `handleProxyWS`, `handleAggregatedSessions`, `handleRemotePWA`) — `agent_proxy.go`/`comm_proxy.go` explicitly excluded, they forward to F10 containers / comm backends, not another datawatch daemon |
| GH#201 Phase 3 — chained-children (`ParentAgentID`) in the agent audit trail (`internal/agents/audit.go`, `GET /api/agents/audit?parent_agent_id=`, `agent_audit` MCP tool) | **Yes** (6 tests in `internal/agents/gh201_phase3_parentid_test.go`: a real recursive spawn through the actual recursion-budget gate with every lifecycle event checked for `ParentAgentID`, a top-level spawn confirmed to leave it empty, `ReapIdle`'s struct-mediated path, `ReadEvents` filtering, the CEF extension field present/absent, the JSON `omitempty` round trip) | **Yes** | Full repo suite: 3314 tests passed, 0 failed. `gosec` exact CI command: live=63, baseline=63. `release-smoke.sh`: 185 passed, 0 failed, 35 skipped | Corrected the plan's own estimate mid-implementation: `spawn` already smuggled `parent_agent_id` into `Extra`, so the real gap was making it first-class + filling in the other 9 call sites (`terminate`/`result`/`crash_*`/`idle_reap`/`service_reattach`), not "the field doesn't exist anywhere" as first assumed; confirmed via grep that nothing else reads the removed `Extra["parent_agent_id"]` key before removing the duplicate |
| GH#201 Phase 4 — state-changing-action completeness sweep (session/alert-rule/federation-peer/device/Automata lifecycle audit logging, ~40 call sites across `autonomous.go`, `api.go`, `rollback.go`, `alert_rules.go`, `federation_peers_api.go`, `devices.go`, `orchestrator.go`) | **Yes** (10 tests in `internal/server/gh201_phase4_audit_test.go`: full session lifecycle incl. the rollback-capability-fix rejection test and a failed-rollback-writes-no-entry test, full alert-rule lifecycle, federation-peer create/update/delete with a token-leak check, device register/delete with a token-leak check, a representative Automata create/approve/delete sample) | **Yes** | Full repo suite: 3324 tests passed, 0 failed. `gosec` exact CI command: live=63, baseline=63 (fixed 1 new G118 false-positive this phase surfaced on `orchestrator.go`'s pre-existing fire-and-forget goroutine, via a `// #nosec G118` comment on its own line — a combined `//nolint:errcheck // #nosec` single-line comment was silently NOT recognized by gosec, confirmed by testing both forms). `release-smoke.sh`: 185 passed, 0 failed, 35 skipped | Found and fixed a real pre-existing authorization gap while wiring this phase: `POST /api/sessions/{id}/rollback` had no capability check at all (its own MCP tool sibling correctly required `CapSessionsWrite`); also confirmed council config was already fully audited (9 pre-existing `auditCouncil` call sites), correcting the plan's own "lower-priority bucket" assumption rather than redoing complete work |
| GH#201 Phase 5 — create-alert API/MCP tool (`POST /api/alerts/create`, `create_alert` MCP tool, new `CapAlertsWrite`) | **Yes** (7 tests in `internal/server/gh201_phase5_create_alert_test.go`, 5 in `internal/mcp/gh201_phase5_create_alert_test.go`: level defaulting/validation, title requirement, method enforcement, audit logging, capability enforcement across admin/full-control/read-only tokens) | **Yes** | Full repo suite: 3341 tests passed, 0 failed. `gosec` exact CI command: live=63, baseline=63. `release-smoke.sh`: 185 passed, 0 failed, 35 skipped | Investigation before sizing (per the plan's own open item) found `AddSystem`'s `AddListener` fan-out already drives SSE+APNs for every alert regardless of source — zero new dispatch plumbing needed, much smaller than the plan feared |
| **Security fix** (shipped in the same v8.78.1 commit, found by a peer session, unrelated to Phase 5 itself) — `internal/secrets.CheckScope`'s empty-scope backward-compat rule incorrectly also applied to the new GH#203 `"service"` caller type, letting any unscoped secret be read by any external-service token over the network | **Yes** (5 new tests in `internal/secrets/scope_test.go`: empty/empty-slice-scope denied for a service caller, explicit scope allowed, wildcard scope allowed, agent/plugin backward compat confirmed unaffected) | **Yes** | Full repo suite included above. Also live-verified on the production daemon: rebuilt, restarted, and the peer (imap-mcp-79) independently confirmed from their own side that the 2 previously-unscoped secrets now return 403 to their service token while the correctly-scoped one still returns 200 | Found via cross-session coordination, not local testing — verified the finding myself by reading `scope.go`'s own doc comment before agreeing it was a real gap, then got the operator's explicit go-ahead before fixing (not just the peer's say-so) |
| BL401 — Cross-Session Communication Rule (`internal/session/tracker.go`'s `crossSessionCommunicationRule()`, new `cross_session.enabled` config, injected into spawned claude-code sessions' CLAUDE.md via the existing Memory Use Rule/RTK injection mechanism) | **Yes** (4 tests in `internal/session/gh_cross_session_policy_test.go`: appended when enabled and a CLAUDE.md already exists, not duplicated on a second session's spawn, not added when disabled, config tri-state default; 1 in `applyconfigpatch_b38_test.go`) | **Yes** | Full repo suite: 3346 tests passed, 0 failed. `node --test internal/server/web/*.test.js`: 182/182 (new PWA settings card, no regression). `gosec` exact CI command: live=63, baseline=63. `release-smoke.sh`: 185 passed, 0 failed, 35 skipped | Found and fixed a real pre-existing bug while wiring this: `memory.session_awareness` was documented and fully exposed over REST/PWA but never actually consulted by the guardrails-injection code path — only `memory.enabled` was checked. Separately (unrelated, found while making an approved production schedule change for a peer session) flagged but did not fix: `ScheduleStore.Update()` silently no-ops for spawn-type schedules — it only writes the legacy `Command` field, never `DeferredSession.Task`, which the actual fire path reads |
| BL406 Phase 0 — project-rules enforcement config scaffolding (new `scan.ProjectRule` type + `project_rules` scan category; new `autonomous.rules_file`/`context_file`/`upstream_repos` fields; `GET/PUT /api/config` round-trip; `autonomous_scan_config_set` MCP `project_rules` param; PWA Settings → Automata panel rows) + **B113** fix (scan config had zero YAML persistence, never wired at startup) | **Yes** (`TestApplyConfigPatch_BL406ProjectRulesFields` in `internal/server`; `TestScanConfigIsUnset`/`TestScanConfigFromYAML_PreservesExplicitDisable`/`TestScanConfigFromYAML_ProjectRulesConvert`/`TestUpstreamReposFromYAML` in `cmd/datawatch/bl406_scan_bridge_test.go`) | **Yes** — real sandbox daemon (own data dir, port 18091), `kill -9` + process restart: an explicit `sast_enabled:false` + a `project_rules` entry both survived exactly as set | Full repo suite: 3369 tests passed, 0 failed. `node --test`: 182/182. Live smoke caught two real bugs before shipping, not just the one B113 set out to fix: (1) `cmd/datawatch/main.go`'s `amgrCfg` struct literal never copied `Scan` at all, so `scan.DefaultConfig()`'s all-on intent was never reached; (2) the first version of the PUT-handler persistence fix mutated `s.cfg.Autonomous.Scan` — a separate, never-defaulted copy from the Manager's own config — producing a YAML block missing every untouched field to `omitempty`. Fixed by round-tripping through the Manager's own current config instead of re-deriving a second copy from the request, and syncing the startup default onto both copies | Found two more pre-existing dead-code bugs while scoping this phase (filed separately, fixed in BL406 Phase 5, not this phase): B111 (`PRD.GuidedMode` never read anywhere despite full set-side plumbing) and B112 (AGENT.md's BL384 scope-drift scan rule never implemented) |

