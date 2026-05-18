# Docs + E2E Completion Plan — 2026-05-17

**Goal:** Close every documentation gap identified in the v6.13→v7.2 audit and achieve
comprehensive E2E test coverage (7-surface parity) for every feature that received new
or updated documentation in the audit passes of 2026-05-17.

**Approach:** Interleave — each sprint fixes one feature's howto, writes its E2E test
stories (all 7 surfaces), runs them against the live test daemon, and ends with a full
rules audit before tagging.

**LLM:** claude-sonnet, high effort
**Plan format:** Automata PRD (dashboard-tracked) + this plan file
**Sprint rule:** Every sprint ends with a complete rules audit table before commit/tag.
**DIP:** Any undocumented design decision triggers the Decision Interview Protocol.
**Error rule:** Bugs found → file GH issue; easy fixes → fix + retest + add memory/rule.
**Android rule:** UI features not sync'd to datawatch-app → file issue immediately.

---

## Sprint T31 — `sessions-deep-dive.md` update + E2E (TS-436–TS-449)

### Scope

**Howto update — `docs/howto/sessions-deep-dive.md`:**
- v7 session-start fields: `llm` (registry name) and `compute_node` fields on `POST /api/sessions/start`
- `Session.LLMRef` and `Session.ComputeNodeRef` visible in session detail + PWA badge
- `Session.BackendFamily` rename from `LLMBackend` (JSON tag `backend_family`)
- LLM and State filter collapsibles added to sessions toolbar (alpha.36)

**E2E test stories — TS-436–TS-449:**

| Story | ID | Surface | Test |
|---|---|---|---|
| Session start with llm field | TS-436 | REST | `POST /api/sessions/start` with `{"task":"...","llm":"claude-sonnet"}` → 200 + session.llm_ref = "claude-sonnet" |
| Session llm_ref visible | TS-437 | REST | `GET /api/sessions` → all items have `backend_family` field (not `llm_backend`) |
| MCP start with llm | TS-438 | MCP | `start_session` with `llm=claude-sonnet` → session created with correct llm_ref |
| CLI session start --llm | TS-439 | CLI | `datawatch session start --task "..." --llm claude-sonnet` → creates session |
| Comm new: --llm | TS-440 | Comm | `new: build a test --llm claude-sonnet` in comm channel → session starts |
| PWA LLM picker on new session | TS-441 | PWA | New session dialog has LLM dropdown; selecting one sets llm_ref |
| Locale LLM picker labels | TS-442 | Locale | `session_llm_label` key present in all 5 locale bundles |
| Android issue for LLM picker | TS-443 | Mobile | datawatch-app issue filed for LLM/compute_node session start fields |
| BackendFamily field name | TS-444 | REST | `GET /api/sessions` JSON has `backend_family` (not `llm_backend`); old field absent |
| MCP session list fields | TS-445 | MCP | `list_sessions` result items have `backend_family` field |
| Sessions filter by LLM | TS-446 | REST | `GET /api/sessions?llm_ref=claude-sonnet` returns only matching sessions |
| CLI session list filter | TS-447 | CLI | `datawatch session list --filter-llm claude-sonnet` returns subset |
| PWA filter collapsible — LLM | TS-448 | PWA | Sessions toolbar has collapsible LLM filter chip bar |
| Comm session list with filter | TS-449 | Comm | `sessions list llm=claude-sonnet` returns filtered list |

### Acceptance

- `sessions-deep-dive.md` updated with all v7 changes
- TS-436–TS-449 all PASS against live test daemon
- `go test ./...` green; smoke 91+/0/n
- Full rules audit table below

### Rules audit (T31)

| Rule | Status | Evidence |
|---|---|---|
| DIP | ✓ | No unresolved design decisions in this sprint |
| 7-surface parity | ✓ | TS-436–449 covers REST/MCP/CLI/Comm/PWA/Locale/Mobile |
| Error-Filing Rule | ✓ | Any bugs found → GH issue filed |
| Android Sync Rule | TS-443 | datawatch-app issue filed for session LLM picker |
| Localization Rule | TS-442 | Locale keys verified × 5 |
| Smoke | ✓ | release-smoke.sh passes before tag |
| No internal refs | ✓ | check-no-internal-refs.sh passes |
| Howto-Coverage | ✓ | sessions-deep-dive.md updated |
| Cookbook | ✓ | T31 entries added to master-cookbook.md |
| Plans folder hygiene | ✓ | This plan dated; tidy-plans.sh check |

---

## Sprint T32 — `federated-observer.md` update + E2E (TS-450–TS-463)

### Scope

**Howto update — `docs/howto/federated-observer.md`:**
- Self-as-peer synthesis: `GET /api/observer/peers` returns an `is_self: true` entry for the local daemon (alpha.7)
- Compute node grouping view: `GET /api/observer/peers/by-node`, `GET /api/federation/meta-peers` (alpha.24)
- Observer peer free-list and binding: `GET /api/observer/peers/free`, `PUT/DELETE /api/compute/nodes/{n}/observer-peer` (alpha.23b)
- `datawatch-stats` multi-parent registration: `--datawatch url1,url2`, `DATAWATCH_PARENTS` (alpha.8)
- Auto-poll live dot: Federated Peers card polls every 8s (alpha.11); manual ↻ button removed

**E2E test stories — TS-450–TS-463:**

| Story | ID | Surface | Test |
|---|---|---|---|
| Self-as-peer in list | TS-450 | REST | `GET /api/observer/peers` returns entry with `is_self: true` |
| MCP self-peer | TS-451 | MCP | `observer_peers_list` → has entry with `is_self=true` |
| CLI peers list local | TS-452 | CLI | `datawatch observer peers list` includes local node |
| Comm peers list | TS-453 | Comm | `observer peers` response includes local daemon |
| PWA Federated Peers local | TS-454 | PWA | Observer view Federated Peers table shows local node row |
| Peers by-node | TS-455 | REST | `GET /api/observer/peers/by-node` returns compute-node-keyed groups |
| MCP peers by-node | TS-456 | MCP | `observer_peers_by_node` returns grouped map |
| CLI compute observer-by-node | TS-457 | CLI | `datawatch compute node observer-by-node` succeeds |
| Peers free-list | TS-458 | REST | `GET /api/observer/peers/free` returns unbound peers |
| MCP peers free | TS-459 | MCP | `observer_peers_free` returns free list |
| CLI observer-free | TS-460 | CLI | `datawatch compute node observer-free` succeeds |
| Federation meta-peers | TS-461 | REST | `GET /api/federation/meta-peers` returns aggregated cross-federation peer data |
| MCP federation meta-peers | TS-462 | MCP | `federation_meta_peers` succeeds |
| PWA auto-poll live dot | TS-463 | PWA | Observer Federated Peers card has pulsing live dot (no manual refresh button) |

### Rules audit (T32)

| Rule | Status | Evidence |
|---|---|---|
| DIP | ✓ | No unresolved design decisions |
| 7-surface parity | ✓ | TS-450–463 covers all 7 surfaces |
| Error-Filing Rule | ✓ | Bugs → GH issue |
| Android Sync Rule | ✓ | federated-observer changes → datawatch-app issue |
| Localization Rule | ✓ | No new locale strings (backend changes only) |
| Smoke | ✓ | release-smoke.sh passes |
| No internal refs | ✓ | check-no-internal-refs.sh passes |
| Howto-Coverage | ✓ | federated-observer.md updated |
| Cookbook | ✓ | T32 entries added |

---

## Sprint T33 — `autonomous-planning.md` + `autonomous-review-approve.md` + E2E (TS-464–TS-477)

### Scope

**Howto updates — both autonomous howtos:**
- `planning_backend` replaces `decomposition_backend` in config (BL304, alpha.79); old key accepted but deprecated
- CLI canonical: `datawatch prd-plan <id>` (with `prd-decompose` as alias)
- HTTP alias: `POST /api/autonomous/prds/{id}/plan` (existing `/decompose` still works)
- User-facing labels: "Plan" replaces "Decompose" throughout PWA, CLI help, MCP descriptions
- MCP param rename: `planning_backend` / `planning_effort` (was `decomposition_backend` / `effort`)
- `autonomous-review-approve.md`: update approval gate section to use new planning terminology

**E2E test stories — TS-464–TS-477:**

| Story | ID | Surface | Test |
|---|---|---|---|
| /plan endpoint works | TS-464 | REST | `POST /api/autonomous/prds/{id}/plan` → 200 |
| /decompose alias still works | TS-465 | REST | `POST /api/autonomous/prds/{id}/decompose` → 200 (backward compat) |
| MCP decompose still works | TS-466 | MCP | `autonomous_prd_decompose` succeeds |
| CLI prd-plan canonical | TS-467 | CLI | `datawatch prd-plan <id>` succeeds |
| CLI prd-decompose alias | TS-468 | CLI | `datawatch prd-decompose <id>` → same result as prd-plan |
| Comm prd plan | TS-469 | Comm | `prd plan <id>` triggers decomposition |
| PWA "Plan" button | TS-470 | PWA | Automaton detail spec panel has "Plan" button (not "Decompose") |
| planning_backend config key | TS-471 | REST | Config with `autonomous.planning_backend: claude-sonnet` used on plan call |
| decomposition_backend back-compat | TS-472 | REST | Old config key `autonomous.decomposition_backend` still accepted without error |
| MCP planning_backend param | TS-473 | MCP | `autonomous_prd_decompose` accepts `planning_backend` param |
| /plan same response shape | TS-474 | REST | `/plan` returns identical JSON schema to `/decompose` response |
| PWA effort picker labels | TS-475 | PWA | Effort picker shows planning-oriented labels (not "decomposition effort") |
| Locale planning labels | TS-476 | Locale | Planning-related locale keys present in all 5 bundles |
| Android planning rename | TS-477 | Mobile | datawatch-app issue filed for Plan button rename |

### Rules audit (T33)

| Rule | Status | Evidence |
|---|---|---|
| DIP | ✓ | No unresolved design decisions |
| 7-surface parity | ✓ | TS-464–477 covers all 7 surfaces |
| Error-Filing Rule | ✓ | Bugs → GH issue |
| Android Sync Rule | TS-477 | datawatch-app issue filed for planning rename |
| Localization Rule | TS-476 | Locale keys verified × 5 |
| Smoke | ✓ | release-smoke.sh passes |
| No internal refs | ✓ | check-no-internal-refs.sh passes |
| Howto-Coverage | ✓ | Both autonomous howtos updated |
| Cookbook | ✓ | T33 entries added |
| Backward-compat | TS-465/472 | Old API path + config key still work |

---

## Sprint T34 — `llm-registry.md` enabled-models overhaul + E2E (TS-478–TS-499)

### Scope

**Howto update — `docs/howto/llm-registry.md`:**
- `LLM.Models []EnabledModel` replaces single `Model` field (alpha.37)
- `LLM.AutoAddModels` flag — newly-discovered models auto-appended
- `GET /api/llms/{name}/in_use` — paginated active bindings view with substring filter
- `POST /api/llms/{name}/reassign` — move all bindings to another LLM
- `POST /api/llms/{name}/force_delete` — cascade-cancel all bindings then delete
- `DELETE /api/llms/{name}` — returns 409 when active bindings exist
- New MCP tools: `llm_in_use`, `llm_refresh_models`, `llm_add_model`, `llm_remove_model`, `llm_list_models`
- New CLI: `datawatch llm models list|add|remove`, `llm in-use`, `llm refresh-models`, `llm reassign`, `llm force-delete`

**E2E test stories — TS-478–TS-499:**

| Story | ID | Surface | Test |
|---|---|---|---|
| LLM in-use REST | TS-478 | REST | `GET /api/llms/{name}/in_use` returns paginated active bindings |
| LLM in-use pagination | TS-479 | REST | `?page=1&size=5` pagination params honored |
| Reassign REST | TS-480 | REST | `POST /api/llms/{name}/reassign` with `to_llm` param moves bindings |
| Force-delete REST | TS-481 | REST | `POST /api/llms/{name}/force_delete` succeeds; LLM gone |
| Delete 409 on active | TS-482 | REST | `DELETE /api/llms/{name}` with active bindings returns 409 |
| MCP llm_in_use | TS-483 | MCP | `llm_in_use` with name arg returns binding list |
| MCP llm_refresh_models | TS-484 | MCP | `llm_refresh_models` triggers model-list refresh |
| MCP llm_add_model | TS-485 | MCP | `llm_add_model` with name+model+node adds to Models[] |
| MCP llm_remove_model | TS-486 | MCP | `llm_remove_model` removes entry from Models[] |
| MCP llm_list_models | TS-487 | MCP | `llm_list_models` returns Models[] for an LLM |
| CLI models list | TS-488 | CLI | `datawatch llm models list <name>` prints Models[] |
| CLI models add | TS-489 | CLI | `datawatch llm models add <name> --model <m>` adds model |
| CLI models remove | TS-490 | CLI | `datawatch llm models remove <name> --model <m>` removes model |
| CLI in-use | TS-491 | CLI | `datawatch llm in-use <name>` prints active bindings |
| CLI refresh-models | TS-492 | CLI | `datawatch llm refresh-models <name>` succeeds |
| CLI reassign | TS-493 | CLI | `datawatch llm reassign <name> --to-llm <other>` succeeds |
| CLI force-delete | TS-494 | CLI | `datawatch llm force-delete <name>` succeeds |
| Comm llm models list | TS-495 | Comm | `llm models list <name>` returns model table |
| Comm llm in-use | TS-496 | Comm | `llm in-use <name>` returns binding summary |
| PWA Models[] list | TS-497 | PWA | LLM edit panel shows Models[] table with per-model rows |
| PWA AutoAddModels toggle | TS-498 | PWA | Auto-add toggle present; toggling changes `auto_add_models` field |
| Locale LLM models labels | TS-499 | Locale | `llm_models_label`, `llm_in_use_label` keys present × 5 bundles |

### Rules audit (T34)

| Rule | Status | Evidence |
|---|---|---|
| DIP | ✓ | No unresolved design decisions |
| 7-surface parity | TS-478–499 | REST/MCP/CLI/Comm/PWA/Locale; Mobile: existing BL issue covers overhaul |
| Error-Filing Rule | ✓ | Bugs → GH issue |
| Android Sync Rule | ✓ | LLM overhaul already has datawatch-app issue from alpha.37 |
| Localization Rule | TS-499 | Locale keys verified × 5 |
| Smoke | ✓ | release-smoke.sh passes (includes llm_enabled_models_basic smoke checks) |
| No internal refs | ✓ | check-no-internal-refs.sh passes |
| Howto-Coverage | ✓ | llm-registry.md updated |
| Cookbook | ✓ | T34 entries added |
| Delete-guard | TS-482 | 409 behavior tested |

---

## Sprint T35 — `compute-nodes.md` (datawatch-stats) + `push-notifications.md` + E2E (TS-500–TS-519)

### Scope

**Howto update — `docs/howto/compute-nodes.md`:**
- `datawatch-stats --datawatch url1,url2` multi-parent registration (alpha.8)
- `DATAWATCH_PARENTS` env var (comma-separated, merged + de-duped)
- Per-parent independent token persistence (`peer-<host>.token` for multi-parent)
- `datawatch-stats --diag` envelope diagnostic (alpha.6)
- `datawatch-stats --debug-connections`, `--print-once`, `--setup-ebpf` flags
- Section: "Troubleshooting with datawatch-stats" subsection

**Howto update — `docs/howto/push-notifications.md`:**
- Session `waiting_input` auto-publishes to `session-<id>` topic and `alerts` topic (alpha.35)
- Council decision auto-publishes to `alerts` topic
- Algorithm phase completion auto-publishes
- Daemon serves push endpoint with no third-party relay required
- Event types table: which daemon events auto-publish and to which topics

**E2E test stories — TS-500–TS-519:**

| Story | ID | Surface | Test |
|---|---|---|---|
| datawatch-stats multi-parent | TS-500 | CLI | `datawatch-stats --datawatch url1,url2 --once` registers with both parents |
| DATAWATCH_PARENTS env | TS-501 | CLI | `DATAWATCH_PARENTS=url1 datawatch-stats --datawatch url2` uses both |
| Per-parent token files | TS-502 | CLI | Multi-parent run creates separate `peer-<host>.token` files |
| --diag output | TS-503 | CLI | `datawatch-stats --diag` prints envelope diagnostic |
| --print-once output | TS-504 | CLI | `datawatch-stats --print-once` prints one snapshot and exits |
| Push SSE subscribe | TS-505 | REST | `GET /api/push/{topic}` opens SSE stream; receives events |
| Push publish | TS-506 | REST | `POST /api/push/{topic}` with body → subscriber receives event |
| Push register | TS-507 | REST | `POST /api/push/register` returns registration token |
| UnifiedPush discovery | TS-508 | REST | `GET /.well-known/unifiedpush` returns discovery JSON |
| waiting_input auto-push | TS-509 | REST | Start session → set to waiting_input → `session-<id>` topic receives event |
| waiting_input alerts push | TS-510 | REST | Same waiting_input event also appears on `alerts` topic |
| CLI push subscribe | TS-511 | CLI | `datawatch push subscribe <topic>` streams events |
| Comm push subscribe | TS-512 | Comm | `push subscribe <topic>` registers comm channel as subscriber |
| PWA push card | TS-513 | PWA | Settings → Comms → Push Notifications card shows topic registration |
| Locale push labels | TS-514 | Locale | `push_topic_label`, `push_register_label` keys × 5 bundles |
| Android push issue | TS-515 | Mobile | datawatch-app issue filed for UnifiedPush + auto-emit events |
| datawatch-stats smoke | TS-516 | CLI | Smoke §7ae passes (datawatch-stats binary present + connects to daemon) |
| Council alert push | TS-517 | REST | Run council debate → `alerts` topic receives council-decision event |
| Algorithm push | TS-518 | REST | Advance algorithm phase → `alerts` topic receives phase event |
| Push no relay required | TS-519 | REST | Daemon push endpoint accessible at /api/push without ntfy relay |

### Rules audit (T35)

| Rule | Status | Evidence |
|---|---|---|
| DIP | ✓ | No unresolved design decisions |
| 7-surface parity | TS-500–519 | All 7 surfaces covered |
| Error-Filing Rule | ✓ | Bugs → GH issue |
| Android Sync Rule | TS-515 | datawatch-app issue filed |
| Localization Rule | TS-514 | Locale keys verified × 5 |
| Smoke | ✓ | release-smoke.sh passes |
| No internal refs | ✓ | check-no-internal-refs.sh passes |
| Howto-Coverage | ✓ | Both compute-nodes.md and push-notifications.md updated |
| Cookbook | ✓ | T35 entries added |

---

## Sprint T36 — Comprehensive E2E for all previously-audited features (TS-520–TS-555)

### Scope

All features that received documentation in the audit passes of this session (commits
43ea4a7 and cedb893) but were not given comprehensive 7-surface E2E coverage. Covers:

1. Memory scope hierarchy (cross-agent-memory.md)
2. HashiCorp Vault secrets backend (secrets-manager.md)
3. Council async run + SSE + cancel (council-mode.md)
4. Council AI persona wizard (council-mode.md)
5. Council comm-firehose (council-mode.md)
6. MCP channel bridge dynamic proxy (mcp-tools.md)
7. Session status board hook events (definitions.md — needs cookbook coverage)
8. Observer extended endpoints (by-node, free, federation meta-peers — as additional REST coverage)

No howto updates needed in this sprint — docs are current. Pure E2E test sprint.

**E2E test stories — TS-520–TS-555:**

#### Memory scope hierarchy

| Story | ID | Surface | Test |
|---|---|---|---|
| Scope recall REST | TS-520 | REST | `GET /api/memory/scopes/recall?persona=alice&project=/tmp/test` returns scoped memories |
| Scope borrow REST | TS-521 | REST | `GET /api/memory/scopes/borrow?scope=project-shared&project=/tmp/test` returns read-only view |
| Scope seed REST | TS-522 | REST | `POST /api/memory/scopes/seed` copies memories with breadcrumb |
| Scope promote REST | TS-523 | REST | `POST /api/memory/scopes/promote` promotes memory up hierarchy |
| MCP scope recall | TS-524 | MCP | `memory_scope_recall` with persona+project args |
| MCP scope borrow | TS-525 | MCP | `memory_scope_borrow` with scope+project args |
| MCP scope seed | TS-526 | MCP | `memory_scope_seed` with from/to args |
| MCP scope promote | TS-527 | MCP | `memory_scope_promote` with memory_id + scopes |
| CLI scope recall | TS-528 | CLI | `datawatch memory scope recall --persona alice --project /tmp/test` |
| Comm scope recall | TS-529 | Comm | `memory scope recall persona=alice project=/tmp/test` |
| Breadcrumb on promote | TS-530 | REST | Promoted memory has `_(promoted ...)_` breadcrumb suffix |

#### Vault secrets backend

| Story | ID | Surface | Test |
|---|---|---|---|
| Vault status REST | TS-531 | REST | `GET /api/secrets/vault/status` returns `{backend_active:false}` when not vault backend |
| MCP vault status | TS-532 | MCP | `secrets_vault_status` succeeds |
| CLI vault status | TS-533 | CLI | `datawatch secrets vault status` returns status |
| Comm vault status | TS-534 | Comm | `secrets vault status` returns status |

#### Council async/wizard

| Story | ID | Surface | Test |
|---|---|---|---|
| Async run returns immediately | TS-535 | REST | `POST /api/council/run` returns `{id, status:"running", events_path}` without blocking |
| SSE event stream | TS-536 | REST | `GET /api/council/runs/{id}/events` delivers SSE events as run progresses |
| Cancel in-flight run | TS-537 | REST | `POST /api/council/runs/{id}/cancel` stops run; status → cancelled |
| MCP council_run_cancel | TS-538 | MCP | `council_run_cancel` with run_id cancels run |
| CLI council cancel | TS-539 | CLI | `datawatch council cancel <id>` succeeds |
| Comm council cancel | TS-540 | Comm | `council cancel <id>` in comm channel cancels run |
| Council persona oneshot | TS-541 | MCP | `council_persona_oneshot` with name+role+focus creates persona |
| Council persona draft flow | TS-542 | MCP | `council_persona_draft_start` → `draft_answer` × 5 → `draft_save` creates persona |
| CLI persona wizard one-shot | TS-543 | CLI | `datawatch council persona-wizard one-shot --name test-persona --role "Test"` |
| Comm persona wizard start | TS-544 | Comm | `council persona-wizard start name=test-persona role="Test"` → receives Q1 |

#### Council comm-firehose

| Story | ID | Surface | Test |
|---|---|---|---|
| Milestone push (default) | TS-545 | Comm | Council run → comm channel receives milestone messages only |
| Firehose push | TS-546 | Comm | `council.comm_firehose: true` → comm receives per-persona previews |

#### Channel bridge dynamic proxy

| Story | ID | Surface | Test |
|---|---|---|---|
| MCP tools manifest | TS-547 | REST | `GET /api/mcp/tools` returns array of tool descriptors |
| MCP call dispatch | TS-548 | REST | `POST /api/mcp/call` with `{name:"get_version",args:{}}` returns version |
| Bridge auto-install check | TS-549 | CLI | Smoke §7ab: Go bridge running (not JS fallback) |

#### Session status board

| Story | ID | Surface | Test |
|---|---|---|---|
| Hook event POST | TS-550 | REST | `POST /api/sessions/{id}/hook-event` with sprint event payload → 200 |
| Status GET | TS-551 | REST | `GET /api/sessions/{id}/status` returns sprint/git/test panels |
| CLI session status | TS-552 | CLI | `datawatch session status <id>` prints status panels |
| MCP session timeline | TS-553 | MCP | `session_timeline` with session_id returns event history |
| PWA Status sub-tab | TS-554 | PWA | Session detail → Status tab shows sprint/git/test panels |
| Android status tab issue | TS-555 | Mobile | datawatch-app issue filed for Session Status sub-tab |

### Rules audit (T36)

| Rule | Status | Evidence |
|---|---|---|
| DIP | ✓ | No unresolved design decisions; all tests for existing shipped features |
| 7-surface parity | TS-520–555 | All 7 surfaces covered per feature |
| Error-Filing Rule | ✓ | Bugs found during testing → GH issue |
| Android Sync Rule | TS-555 | Status tab issue filed; vault/scopes already have issues |
| Localization Rule | ✓ | No new locale strings; existing verified in prior sprints |
| Smoke | ✓ | release-smoke.sh passes |
| No internal refs | ✓ | check-no-internal-refs.sh passes |
| Cookbook | ✓ | T36 entries added to master-cookbook.md |

---

## Sprint T37 — Full suite validation + final audit (TS-556–TS-563)

### Scope

This is the close-out sprint. No new features; no howto updates. Purpose:
- Execute the full master-cookbook.md suite against the live test daemon (T1–T36)
- Fix any failures found (with Error-Filing Rule for each)
- Verify all 7-surface parity assertions for every feature touched in this plan
- Confirm all datawatch-app issues are filed
- Run the linting suite (check-no-internal-refs, check-curated-howtos, check-howto-coverage, check-plugin-manifests)
- Confirm smoke §42 howto-existence guard passes for all new howto files
- Final rules audit
- Commit + tag

**E2E test stories — TS-556–TS-563:**

| Story | ID | Surface | Test |
|---|---|---|---|
| Full suite run | TS-556 | ALL | Run T1–T36 against live daemon; 0 failures |
| Lint suite clean | TS-557 | CI | check-no-internal-refs + check-curated-howtos + check-howto-coverage pass |
| Smoke §42 howto guard | TS-558 | CI | Smoke §42 passes (all howto files exist on disk) |
| OpenAPI coverage check | TS-559 | REST | All endpoints in master-cookbook.md present in openapi.yaml |
| Definitions feature matrix | TS-560 | Docs | Feature matrix in definitions.md has entry for every howto file |
| Docs README complete | TS-561 | Docs | docs/howto/README.md indexes every file in docs/howto/ |
| Android issues complete | TS-562 | Mobile | Every sprint's Android story has a filed datawatch-app issue |
| No stale internal refs | TS-563 | Docs | All howtos pass BL/F/B ref grep clean |

### Rules audit (T37)

| Rule | Status | Evidence |
|---|---|---|
| DIP | ✓ | No unresolved design decisions across entire plan |
| 7-surface parity | T31–T36 | Each sprint table confirms all 7 surfaces |
| Error-Filing Rule | ✓ | All bugs filed as GH issues |
| Android Sync Rule | TS-562 | All Android issues confirmed filed |
| Localization Rule | ✓ | All locale keys verified across plan |
| Per-release smoke | ✓ | release-smoke.sh passes (final run) |
| No internal refs | TS-563 | check-no-internal-refs.sh PASS on all howtos |
| Howto-Coverage | ✓ | All 6 remaining howtos updated |
| Cookbook | ✓ | T31–T37 (TS-436–TS-563) added to master-cookbook.md |
| Plans folder hygiene | ✓ | This plan archived per tidy-plans.sh |
| Mobile-Parity | ✓ | Per-sprint mobile stories all filed |
| Configuration Accessibility | ✓ | All new config fields reachable on all 7 surfaces |
| Backlog-is-spec | ✓ | All stories trace to CHANGELOG items or backlog entries |
| DIP | ✓ | Any decisions during plan → DIP applied + recorded |
| Docs-as-MCP Currency | ✓ | All updated howtos have `docs: {index: true}` frontmatter |
| Reuse-and-Expand | ✓ | No new cards/tools when existing ones could be extended |

---

## Tracking summary

| Sprint | Howto target | TS range | Stories | Status |
|---|---|---|---|---|
| T31 | sessions-deep-dive.md | TS-436–449 | 14 | ⬜ |
| T32 | federated-observer.md | TS-450–463 | 14 | ⬜ |
| T33 | autonomous-planning.md + review-approve.md | TS-464–477 | 14 | ⬜ |
| T34 | llm-registry.md | TS-478–499 | 22 | ⬜ |
| T35 | compute-nodes.md + push-notifications.md | TS-500–519 | 20 | ⬜ |
| T36 | E2E comprehensive (all prev-audited features) | TS-520–555 | 36 | ⬜ |
| T37 | Full suite validation + final audit | TS-556–563 | 8 | ⬜ |
| **Total** | | **TS-436–563** | **128** | |

---

## Rules index (this plan)

| Rule | Where defined | Triggers |
|---|---|---|
| Decision Interview Protocol (DIP) | AGENT.md | Unresolved design decision |
| Error-Filing Rule | AGENT.md | Bug/defect found during work |
| Android Sync Rule | AGENT.md | PWA UI feature not in datawatch-app |
| 7-surface parity | AGENT.md (Configuration Accessibility) | Every new feature |
| Smoke before tag | AGENT.md (Release-discipline) | Every sprint end |
| No internal refs | AGENT.md (Docs rules) | Every howto edit |
| Howto-Coverage | AGENT.md (BL274 Docs-as-MCP) | Every feature landing |
| Cookbook | AGENT.md (Live Project Cookbook) | Every sprint |
| Localization | AGENT.md (Localization Rule) | Every new UI string |
| Mobile-Parity | AGENT.md (Mobile-Parity Rule) | Every new PWA feature |

---

## Automata PRD

This plan is tracked as an Automata PRD in the dashboard. Sprint tasks map to PRD tasks.
The PRD title: **"Docs + E2E Completion — v6.13→v7.2 audit close"**.
LLM: claude-sonnet (high effort).
Type: operational.

Stories are ordered: T31 → T37. Each sprint is one PRD task group.
The dashboard Gantt card shows sprint-by-sprint progress.
The task tree card shows individual TS-### stories as sub-tasks.
