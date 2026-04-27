# Plans, Bugs & Backlog

Single source of truth for all datawatch project tracking.

---

# Rules

**All operating rules live in [`/AGENT.md`](../../AGENT.md).** This file
holds the project-tracking data only — bugs, plans, backlog items.
Cross-reference highlights:

- Pre-execution + scope — [`AGENT.md` § Pre-Execution Rule, § Scope Constraints](../../AGENT.md)
- Code quality + security — [`AGENT.md` § Code Quality Rules, § Security Rules](../../AGENT.md)
- Testing + audit + memory — [`AGENT.md` § Testing Tracker Rules, § Audit Logging Rule, § Memory Use Rule](../../AGENT.md)
- Git + versioning + release cadence — [`AGENT.md` § Git Discipline, § Versioning, § Release vs Patch Discipline](../../AGENT.md) (incl. binary-build cadence: only minor/major releases ship binaries)
- Documentation + tracker discipline — [`AGENT.md` § Documentation Rules, § Project Tracking](../../AGENT.md) (includes "no internal IDs in user-facing UI", "README marquee must reflect current release", "backlog refactor each release", "feature completeness audit", "container maintenance audit")
- Plan attribution — [`docs/plan-attribution.md`](../plan-attribution.md)

If you find a rule that applies to operating behavior duplicated in this file,
move it to AGENT.md and replace it with a cross-reference. AGENT.md is the
single source of truth.

## Unclassified

_(empty — drop new operator-filed items here; the backlog refactor each release pulls them into BL### entries below.)_

_Historical Unclassified items shipped + tracked elsewhere:_ Directory-selector "create folder" (v4.0.1), Aperant integration review (skipped — see [`docs/plan-attribution.md`](../plan-attribution.md) "Researched and skipped"), datawatch-observer / BL171–BL173 (✅ all three shapes shipped — see Recently closed).

---

## Open Bugs

_(empty — all numbered items are tracked in the Open backlog tables below; recently closed in **Recently closed**.)_

> Historical: B22 fixed in v2.4.3 · B23/24 in v2.4.4 · B25 in v2.4.5 · B31 in v3.0.1 · B30 in v3.1.0 — see Completed section.

## Open Features

_(All numbered now — see Open backlog table below for BL189, BL190, plus BL197 autonomous surface-parity audit.)_

## Pending backlog

_(empty — every item that was here is now ✅ closed. See **Recently closed** below for: BL170 (v4.5.0), BL174 (v4.6.0), BL171/172/173 datawatch-observer all three shapes (v4.1.0 / v4.4.0 / v4.5.0), S13 agent observer peers (v4.7.0).)_

## Open backlog (deferred / awaiting operator action)

**Quick map:** items where I can keep working sit in **Active work** below. Items where I'm blocked on an operator decision sit in **Awaiting operator action** with a structured "what's needed + recommendation" per item. Items shipped recently sit in **Recently closed** for one release cycle; long-term / external items sit in **Frozen / External**.

### Active work (no decision needed — keep iterating)

| ID | Item | Status |
|----|------|--------|
| BL180 Phase 2 (kprobes + cross-host) | **✅ Closed v5.13.0** — procfs cut v5.1.0; cross-host shipped v5.12.0; eBPF kprobes shipped v5.13.0. `Callers []CallerAttribution` envelope shape + procfs userspace correlator + `CorrelateAcrossPeers` federation aggregator + new `tcp_connect` + `inet_csk_accept` kprobes feeding `conn_attribution` BPF_MAP_TYPE_LRU_HASH + userspace `ReadConnAttribution()` / `PruneConnAttribution(olderThanNs)` per BL292 spec. The eBPF probes attempt to attach but failures are non-fatal (existing partial-mode fallback). Real attach is gated by CAP_BPF + a kernel that exposes the symbols; the Thor smoke-test on a CAP_BPF host stays as the operator-side validation. | ✅ Closed v5.13.0. |
| BL191 (PWA + Q4 + Q6) | **✅ Closed v5.10.0** — Q1 review/approve gate + Q2 templates + Q3 decisions log shipped v5.2.0; Q4 recursive child-PRDs shipped v5.9.0 (`Task.SpawnPRD` + genealogy + depth limit + auto-approve config + REST/MCP/CLI/chat/YAML parity); Q6 guardrails-at-all-levels shipped v5.10.0 (per-story + per-task guardrails via new `Story.Verdicts` + `Task.Verdicts` + `Config.PerTaskGuardrails` / `Config.PerStoryGuardrails` + `GuardrailFn` indirection wired to `/api/ask` loopback in main.go; block outcome halts the PRD with status=blocked). PWA full CRUD landed BL202 v5.5.0; PWA child-PRD tree + verdict drill-down stays as an iterative cosmetic follow-up. | ✅ Closed v5.10.0. |
| BL190 follow-up | **Density expansion shipped v5.15.0** — recipe map grew from 11 to 19; 22 PNGs in `docs/howto/screenshots/`. New recipes: `sessions-mobile`, `autonomous-mobile`, `autonomous-prd-expanded`, `settings-general-autonomous`, `settings-comms-signal`, `settings-llm-ollama`, `settings-monitor-mobile`, `diagrams-flow`, `autonomous-prd-decisions`, `session-detail-mobile`, `settings-general-auto-update`, `settings-llm-memory`, `header-search`. Seed-fixtures script enriched with a `fixrich` PRD carrying 1 story + 3 tasks + 3 decisions + 1 verdict so the expanded screenshot has substance. Inline coverage extended across 8 howtos with multi-shot sequences (chat-and-llm-quickstart now 6, daemon-operations 6, autonomous-planning 3, autonomous-review-approve 3, comm-channels 2, federated-observer 2, prd-dag-orchestrator 2, cross-agent-memory 2). | ✅ Density first cut shipped v5.15.0; further per-howto density (failure-path popups, verdict drill-down panels, mid-run progress bars) remains an iterative cosmetic follow-up. |
| BL200 howto coverage expansion | **✅ Closed v5.7.0** — 13 walkthroughs total (original 6 refreshed + 7 new: setup-and-install, chat-and-llm-quickstart, autonomous-review-approve, voice-input, comm-channels, federated-observer, mcp-tools). Each walkthrough keeps the per-channel reachability matrix at the bottom. PWA screenshot rebuild for the now-13-doc suite stays under BL190 follow-up. | ✅ Closed v5.7.0. |
| BL201 voice/whisper backend inheritance | **✅ Closed v5.8.0** — daemon-side resolution: `inheritWhisperEndpoint` in `cmd/datawatch/main.go` fills `whisper.endpoint` + `whisper.api_key` from `cfg.OpenWebUI.URL/APIKey` (for backend=openwebui) or `cfg.Ollama.Host` + `/v1` (for backend=ollama) when blank; explicit values still win. New `ollama` case added to `internal/transcribe/factory.go` (routes through OpenAI-compat client). PWA already hides whisper.endpoint/api_key (Settings → General → Voice Input shows backend / model / language / venv only — endpoint+key never surfaced). Test button (BL289 v5.4.0) already exists. 8 new unit tests cover the inheritance matrix. | ✅ Closed v5.8.0. |
| BL202 BL191 PWA full CRUD | **First cut shipped v5.3.0** — promoted to a new top-level **Autonomous** tab in the bottom nav (operator directive 2026-04-26: not buried in Settings). PRD list + status pill + click-to-expand stories + inline task-spec editor + per-status action buttons (Decompose / Approve / Reject / Request-revision / Run / Cancel) + template Instantiate + New-PRD modal + decisions log viewer. **Still open**: PWA backend + effort + model dropdowns wired to the new BL203 set-llm / set-task-llm endpoints; decisions log richer surfacing; recursive child-PRD view (Q4); guardrails-at-all-levels (Q6). | Open (panel shipped v5.3.0; PWA LLM dropdowns + Q4/Q6 remain). |
| BL203 flexible LLM selection (per-task / per-PRD) | **Backend + REST + CLI + chat + MCP shipped v5.4.0; PWA dropdowns shipped v5.5.0** — operator directive: "i want to be able to use the right llm for autonomous operations so it needs to be flexible". Most-specific wins: per-task → per-PRD → per-stage (decomp/verify) → global session.llm_backend. PWA Autonomous tab New-PRD + edit-task + per-PRD "LLM" button all wire backend / effort / model dropdowns through to `set_llm` / `set_task_llm`. | ✅ Closed v5.5.0. |
| BL173-followup | **Live cluster→parent push** is operator-side: dev workstation parent isn't reachable from the testing-cluster pod overlay. Production deploys don't have this gap. | No code action; verify on a production cluster when convenient. |

### Awaiting operator action

_(empty — every item that was here as of v5.0.5 is now answered or shipped. New operator-decision items land here with **What's needed / Options / Recommendation**.)_

### Recently closed (sticky for one release cycle, then archived)

**Audit (v5.0.5, 2026-04-26):** spot-checked 17 entries by grepping current source for the specific files / functions / config keys each entry claims. All 17 verified present. Two prior misses (BL187, BL198) were already re-opened earlier this cycle and re-fixed in v5.0.4 / v5.0.5 with corrected entries below — those entries now describe the *actual* root cause rather than the source-level audit.

| ID | Closed in | What |
|----|-----------|------|
| Diagrams page restructure + retention refinement | v5.25.0 | Operator-reported: diagrams.html sidebar dropped Plans group (operator-internal; already gitignored from embedded viewer since v5.3.0) + added top-level How-tos group with all 13 walkthroughs + extended Subsystems with mcp.md/cursor-mcp.md + extended API with observer.md/memory.md/sessions.md/devices.md/voice.md. Asset retention rule refined: keep-set = every major + latest minor + latest patch on latest minor (was just majors). `scripts/delete-past-minor-assets.sh` rewritten with the new logic. AGENT.md § Release-discipline rules updated. |
| Autonomous tab WS auto-refresh + dropdown narrowing | v5.24.0 | Operator v5.22.0 carry-over: PWA Autonomous tab required manual Refresh after every CLI/chat/REST mutation. New `MsgPRDUpdate` WS message + `Manager.SetOnPRDUpdate(PRDUpdateFn)` indirection + `Manager.EmitPRDUpdate(id)` trampoline. Every `*API` mutating method (Create/Decompose/Run/Cancel/Approve/Reject/RequestRevision/EditTaskSpec/InstantiateTemplate/SetTaskLLM/SetPRDLLM/DeletePRD/EditPRDFields) emits after save; trailing emit fires inside the Run goroutine when the executor walk finishes (terminal states reach the PWA). main.go binds the callback to `HTTPServer.BroadcastPRDUpdate`. PWA debounces 250 ms so a Run that flips many tasks per second reloads the panel once at the end. 4 new unit tests. Plus operator-reported tmux-bar fit: saved-commands dropdown `max-width: 200px → 130px` so the [📄] [Commands ▾] [arrows] row fits on one line on a 480px PWA card. |
| Asset retention + 4 operator-reported PWA bugs | v5.23.0 | (1) Settings → Comms bind interface fields were rendering empty (objects-as-strings bug) and were single-select; now multi-select with connected-interface auto-protect (prevents self-disconnect). (2) Session-detail channel/acp mode-badge dropped — the output-tab system conveys it. tmux mode-badge stays (no tab in tmux-only mode). (3) Response button icon-only — 📄 alone, no text, fits v5.22.0 right-justified arrow row. (4) Two new AGENT.md release-discipline rules: embedded docs must be current at binary build time (always go through make cross / make build, never `go build` directly); asset retention — only major releases keep binaries indefinitely, past minor/patch get assets pruned on next release. New `scripts/delete-past-minor-assets.sh` helper. Ran the script against 105 past-minor releases — deleted 477 binary attachments. Operator's config-save question answered in release notes (already efficient: PUT /api/config + applyConfigPatch updates in-memory + YAML, no restart unless key is in RESTART_FIELDS set). |
| Observability fill-in + arrow-buttons layout (audit #3) | v5.22.0 | LoopStatus surfaces BL191 Q4 + Q6 counters: `ChildPRDsTotal`, `MaxDepthSeen`, `BlockedPRDs`, `VerdictCounts` (pass/warn/block rollup across every Story.Verdicts + Task.Verdicts). Operators on `/api/autonomous/status` polling loops get the new fields automatically; PWA / MCP / chat all forward the same JSON. 4 new unit tests. Operator-reported: arrow buttons (Up/Down/Left/Right) now right-justified next to the saved-commands dropdown via `margin-left:auto` in the flex container — v5.19.0 had restored them but placed them BEFORE the dropdown which let flex-wrap put them on the next line. README marquee → v5.22.0. |
| Observer + whisper config-parity sweep (audit #2) | v5.21.0 | Same pattern as v5.17.0 (autonomous) but for observer + the missing whisper HTTP-shape keys. `internal/config.ObserverConfig` gained `ConnCorrelator` (BL293) + `Peers` (BL172) — pre-v5.21.0 these lived only on `observer.Config` so YAML/REST couldn't reach them. New `ObserverPeersConfig` struct. main.go bridge updated. `applyConfigPatch` gained 20 new cases (observer scalars × 5, pointer-bools × 6, federation × 5, peers × 4, ollama_tap × 1, whisper HTTP × 3). 6 new unit tests covering the round-trip matrix. README marquee → v5.21.0. |
| Documentation alignment sweep (audit #1) | v5.20.0 | Pure-docs release closing the audit's documentation drift findings. `docs/mcp.md` bumped from "41 tools" to "100+ tools" with family breakdown + live-list pointer. `docs/cursor-mcp.md` tools table extended beyond the v3-era 5 entries. `docs/api/autonomous.md` documents every endpoint added since v5.2.0 (approve/reject/request_revision/edit_task/instantiate/set_llm/set_task_llm/children/PATCH/DELETE?hard=true). `docs/api/observer.md` documents the cross-host endpoint + every observer MCP tool + CLI subcommand. `openapi.yaml` updated with the four newly-shipped paths. README marquee → v5.20.0. No code changes. |
| Operator-blocking CRUD + UX cleanup + audit gap | v5.19.0 | (1) Autonomous full CRUD finally lands: `Store.DeletePRD` (recursion-aware — descendants spawned via SpawnPRD removed too) + `Store.UpdatePRDFields` (title + spec edit on non-running PRDs) + Manager wrappers + `DELETE /api/autonomous/prds/{id}?hard=true` + `PATCH /api/autonomous/prds/{id}` + CLI `datawatch autonomous prd-delete` / `prd-edit` + PWA Edit + Delete buttons on every PRD card with confirm dialog. (2) PWA whisper test-button rendered as empty `<input type="button">` because `loadGeneralConfig` fell through to the generic input path; mirrored the comms render path's button branch. (3) PWA `loadSavedCmdsQuick` was overwriting `#savedCmdsQuick.innerHTML`, blowing away the Response button + tmux arrow group (regression of v5.2.0 BL191 work); restored. (4) Session-detail had a duplicate "Input Required" label inline above the tmux input box (operator: top-of-page badge already conveys it); removed. (5) RTK config section was duplicated in Settings → General + LLM (operator: should only be in LLM); removed from General. (6) README.md marquee bumped from v5.7.0 → v5.19.0 (was 12 releases stale across this session and the previous one). 8 new unit tests in `internal/autonomous/crud_test.go` (delete-from-map, recursive-descendant-cleanup, not-found-error, refuses-running, update-title, update-spec, update-refuses-running, edit-appends-decision). |
| MCP channel one-way bug | v5.18.0 | Operator report: MCP channel "not working in Claude" — investigated and traced to the HTTP→HTTPS redirect blocking the bridge's `notifyReady()` POST. The bridge follows the 307 redirect, hits the daemon's self-signed TLS cert, fails verify, and the daemon never learns the bridge's listening port. Result: `claude mcp list` shows ✓ Connected (stdio handshake works) but the daemon's push path has nowhere to send → reply tool works one-way only. Fix: redirect handler bypasses for loopback requests to `/api/channel/*`. New `isLoopbackRemote` helper + 1 unit test. Verified end-to-end: daemon log now shows `[channel] ready for session <id>` after every bridge spawn. |
| Operator-surface bridge for BL191 Q4 + Q6 config knobs | v5.17.0 | Polish-pass finding: v5.9.0 (`max_recursion_depth`, `auto_approve_children`) + v5.10.0 (`per_task_guardrails`, `per_story_guardrails`) shipped the runtime feature but the operator-facing surface was incomplete — YAML load dropped them, `PUT /api/config` silently no-op'd, PWA Settings → Autonomous didn't expose them, main.go translation didn't copy them. v5.17.0 closes the bridge: `internal/config/AutonomousConfig` gained the four fields; `cmd/datawatch/main.go` copies them with fallback to package defaults; `applyConfigPatch` handles them with both JSON-array and CSV-string accepting paths (new `splitCSV` helper); PWA gained four field entries. 2 new unit tests (1357 total). |
| PWA viz for shipped data-model work | v5.16.0 | Three contained PWA additions (`internal/server/web/app.js` only) that surface data the daemon was already producing through `/api/autonomous/prds/...` and `/api/observer/envelopes/all-peers` but the PWA wasn't rendering. (1) BL191 Q4 — PRD cards show `↗ parent <id>` + `depth N` badges; new **Children (lazy)** disclosure renders the genealogy tree; per-task `↳ spawn` + `→ child <id>` affordances. (2) BL191 Q6 — color-coded inline verdict badges on every story + task with hover tooltips for severity/summary/issues. (3) BL180 cross-host — "↔ Cross-host view" button on Federated peers, opens a modal that walks `/api/observer/envelopes/all-peers` and tags cross-peer caller rows with `🔗 cross`. New `cross-host-modal` recipe added to `scripts/howto-shoot.mjs` + screenshot inlined into federated-observer howto. |
| BL190 density expansion | v5.15.0 | Recipe map 11 → 19; 22 PNGs total. New recipes cover mobile viewports (sessions/autonomous/settings-monitor/session-detail), Settings deep-scrolls (general → autonomous block, general → auto-update, comms → Signal, LLM → Ollama, LLM → Episodic Memory), the autonomous-prd-expanded toggle (with seeded fixrich PRD carrying 1 story + 3 tasks + 3 decisions + 1 verdict), the diagrams-flow content view, and the header-search filter chips. Inline coverage extended across 8 howtos with multi-shot sequences. Seed-fixtures script enriched with the `fixrich` PRD so the expanded screenshot shows real story+task+decisions content rather than "no stories yet". |
| BL190 expand-and-fill | v5.14.0 | Recipe map grew from 6 to 11 (`settings-monitor`, `settings-about`, `alerts-tab`, `autonomous-new-prd-modal`, `session-detail` added). 11 screenshots committed under `docs/howto/screenshots/`. Inline coverage extended from 4 to 13 howtos — every walkthrough now has at least one PNG: setup-and-install + container-workers + cross-agent-memory + federated-observer + daemon-operations get `settings-monitor`; daemon-operations + setup-and-install get `settings-about`; daemon-operations gets `alerts-tab`; autonomous-review-approve gets `autonomous-landing` + `autonomous-new-prd-modal`; chat-and-llm-quickstart + pipeline-chaining get `session-detail`; comm-channels gets `settings-comms`; prd-dag-orchestrator gets `autonomous-landing`. Per-howto density (1-3 PNGs each) is below the original 15-20-per-howto target; the pipeline is in place for further iterative expansion. |
| BL180 Phase 2 eBPF kprobes (resume) | v5.13.0 | Per the BL292 commit roadmap: new `tcp_connect` (outbound) + `inet_csk_accept` (inbound) kprobes feeding a `conn_attribution` BPF_MAP_TYPE_LRU_HASH (key = sock pointer, value = {pid, ts_ns}). LRU eviction bounds memory; new userspace `realLinuxKprobeProbe.ReadConnAttribution()` iterates the map and `PruneConnAttribution(olderThanNs)` walks + deletes stale entries for freshness. Loader attempts attach on the new probes; failure is non-fatal (existing partial-mode fallback keeps byte counters live). bpf2go regenerated cleanly under clang 20.1.8 with both committed `vmlinux_amd64` + `vmlinux_arm64` headers; new `.o` artifacts updated in tree. 3 new unit tests cover the nil-safe iterator + post-Close idempotence + ConnAttribution row shape; real attach is gated by CAP_BPF and validated via the operator's Thor smoke-test (BL180 design Q6). |
| BL180 Phase 2 cross-host federation correlation | v5.12.0 | New `Envelope.ListenAddrs []ListenAddr` + `Envelope.OutboundEdges []OutboundEdge` fields. The local correlator now records LISTEN-state addrs on backend envelopes + ESTABLISHED outbound conns that miss a local listener (cross-host candidates). New `observer.CorrelateAcrossPeers(byPeer map[string][]Envelope, localPeerName string)` joins outbound edges from one peer to listen addrs on another, producing CallerAttribution rows with `<peer>:<envelope-id>` prefix on the matched server envelope. Reachable as `GET /api/observer/envelopes/all-peers` + `observer_envelopes_all_peers` MCP tool + `datawatch observer envelopes-all-peers` CLI. 7 new unit tests cover happy path, wildcard listener, same-peer-not-matched, single-peer no-op, sort-order, unmatched-edge, local-peer-prefix-suppression. Operator's Q5c "don't close until cross-host works" satisfied. |
| BL190 howto screenshot capture pipeline (first cut) | v5.11.0 | Operator removed the chrome MCP plugin (memory issue); new capture path goes through puppeteer-core in `/tmp/puppet` driving `/usr/bin/google-chrome` headless. New `scripts/howto-shoot.mjs` (recipe-driven; 6 recipes ship: sessions-landing, autonomous-landing, settings-llm, settings-comms, settings-voice, diagrams-landing) + `scripts/howto-seed-fixtures.sh` (idempotent; wipes `fixture: true` JSONL rows + re-seeds PRDs across every status pill, one orchestrator graph + guardrail node, one pipeline with before/after gates). 6 screenshots committed under `docs/howto/screenshots/` (excluded from embedded daemon binary via `_embed_skip.txt`); inlined into chat-and-llm-quickstart, autonomous-planning, voice-input, mcp-tools. Recipe map intentionally minimal — extend to ~15-20 shots × 13 howtos in iterative cuts. |
| BL191 Q6 — guardrails-at-all-levels | v5.10.0 | Per-story + per-task guardrails. New `Story.Verdicts` + `Task.Verdicts` slices, `Config.PerTaskGuardrails` + `Config.PerStoryGuardrails` (defaults empty = opt-in). New `GuardrailFn` indirection on `Manager` wired in `cmd/datawatch/main.go` to a `/api/ask` loopback (same path the BL25 verifier uses; `verification_backend` / `verification_effort` apply). After every task verifies green, per-task guardrails fire; a `block` verdict marks the task blocked and halts the PRD with status=blocked. Per-story guardrails fire after all tasks in a story complete. Permissive parse — unparseable guardrail output becomes a `warn` so best-effort runs still progress. 6 new unit tests (no-config no-op, all-pass, task-block-halts-PRD with second-task-untouched assertion, story-fire-after-all-tasks-done, story-block-halts, no-fn-wired silent no-op). |
| BL191 Q4 — recursive child-PRDs | v5.9.0 | Option (a) shortcut from the design doc: `Task.SpawnPRD` flag turns a parent task spec into a child PRD spec; `recurseChildPRD` in `internal/autonomous/executor.go` walks Decompose → (auto-)Approve → Run inline; child outcome rolls up onto the parent task. New `PRD.ParentPRDID/ParentTaskID/Depth` fields + `Store.CreatePRDWithParent` + `Store.ListChildPRDs`. New `Config.MaxRecursionDepth` (default 5; 0 disables) + `Config.AutoApproveChildren` (default true — false leaves the parent task `blocked` waiting for operator approval). Full parity: REST `GET /api/autonomous/prds/{id}/children` + MCP `autonomous_prd_children` + CLI `datawatch autonomous prd-children` + chat verb `autonomous children <id>` + YAML `autonomous.{max_recursion_depth,auto_approve_children}`. 5 new unit tests cover the recursion matrix. |
| BL201 voice/whisper backend inheritance | v5.8.0 | Daemon-side: new `inheritWhisperEndpoint` helper in `cmd/datawatch/main.go` fills `whisper.endpoint`/`whisper.api_key` from `cfg.OpenWebUI.URL/APIKey` when backend=openwebui, or from `cfg.Ollama.Host + /v1` when backend=ollama. Explicit values always win. New `ollama` case in `internal/transcribe/factory.go` routes through the OpenAI-compat client. PWA Settings already hides endpoint/api_key fields so the resolution is the single source of truth. 8 new unit tests cover the inheritance matrix (openwebui inherit, explicit override, ollama with/without trailing slash, whisper no-op, openai pass-through, case-insensitive backend trim). |
| BL200 howto coverage expansion | v5.7.0 | 13 walkthroughs total — original 6 refreshed + 7 new (`setup-and-install`, `chat-and-llm-quickstart`, `autonomous-review-approve`, `voice-input`, `comm-channels`, `federated-observer`, `mcp-tools`). MCP-tool surface table verified against `internal/mcp/*.go` `NewTool(...)` registry; federated-observer.md uses real `peer register` + `peer delete` + `--token-file` flags. PWA screenshot rebuild stays under BL190 follow-up. |
| `datawatch reload` CLI parity | v5.7.0 | Added the missing CLI subcommand for hot config reload — BL17 already had SIGHUP + `POST /api/reload` + the MCP `reload` tool. Closes a configuration-parity gap; lets every howto recommend `datawatch reload` after `datawatch config set` instead of the SIGHUP/curl dance. |
| Two-place version sync (api.go ↔ main.go) | v5.7.0 | `internal/server/api.go` was stuck at `Version = "5.0.3"` while `cmd/datawatch/main.go` marched through 5.0.x → 5.6.1 (LDFLAGS injection masked the runtime impact, but the AGENT.md "must be updated together" rule was being violated). Both files re-synced to 5.7.0; pre-commit version-check note in AGENT.md § Versioning called out as a recurring failure mode. |
| BL180 Phase 2 (procfs cut) | v5.1.0 | Per-caller envelope attribution: new `Callers []CallerAttribution` field + `internal/observer/conn_correlator.go` (procfs-based join of `/proc/<pid>/net/tcp` connections with the listen-port → envelope map). 9 unit tests cover the parser, scope filter, end-to-end join, and Phase 1 caller preservation. Existing `Caller`/`CallerKind` derived as the loudest entry for back-compat. eBPF kprobe layer + cross-host correlation remain open per operator answers (see Active work). |
| Session toolbar toggle | v5.1.0 | Removed the `toggleTermToolbar` affordance + state + `term-toolbar-hidden` CSS rules; the term-toolbar now always renders (operator confirmed the layout reads cleanly at every viewport). Filed [datawatch-app#8](https://github.com/dmz006/datawatch-app/issues/8) so the mobile shell drops the matching toggle. |
| BL178 reopen | v5.1.0 | Operator on v5.0.5: the session-list response icon was opening to text from "weeks ago". Daemon-side `GetLastResponse` only returned the stored `Session.LastResponse`, which is captured on running→waiting_input transitions. For long-lived running sessions, the stored value can stay stale until the next transition. Fix: when the session is `running` or `waiting_input`, `GetLastResponse` re-captures from the live tmux pane on every read and persists if changed; terminated sessions keep their last-word stored value. |
| Session-list history button | v5.1.0 | Renamed the "Show / Hide history (N)" toggle to just "History (N)" per operator — keeps the count, drops the verb churn. |
| Session-list FAB position | v5.1.0 | Two operator-reported bugs: (a) on Chrome desktop the FAB sat outside the centered 480px PWA card because it was anchored to the viewport's right edge; fix scoped a `right: calc(50vw - 240px + 16px)` override into the `@media (min-width:600px)` block so the FAB tracks the card. (b) On phone the FAB sat on top of the bottom nav because `bottom` was `64px` while `--nav-h` is `60px` (4px gap → visual overlap on Chrome mobile); fix uses `calc(var(--nav-h) + 16px + safe-area)` for a proper 16px clearance. |
| BL191 Q1 + Q2 + Q3 | v5.2.0 | Autonomous PRD lifecycle first cut: Q1 review/approve gate (status machine: draft → decomposing → needs_review → approved → running → complete + revisions_asked / rejected / cancelled) wired across REST / CLI / chat / MCP; Q3 per-PRD `Decisions []Decision` audit timeline appended on every transition; Q2 templates via `IsTemplate` flag + `InstantiateTemplate` with `{{var}}` substitution. 9 new lifecycle tests cover each transition + the gate on Run. PWA full CRUD (Q5) + recursion (Q4) + guardrails-at-all-levels (Q6) deferred to v5.3.0. |
| Settings → About — datawatch-app link | v5.2.0 | Added a GitHub link to the mobile-app repo + a placeholder note for the Play Store link once the app publishes. |
| Settings → About — orphaned-tmux clear | v5.2.0 | Moved the orphaned-tmux count + "Kill all orphaned" affordance from Settings → Monitor → System Statistics to Settings → About since it's an operator/maintenance affordance, not a live metric. Auto-refresh after kill. |
| Settings → General — Voice Input backend dropdown | v5.2.0 | Backend select extended to expose `whisper / openai / openai_compat / openwebui / ollama` (operator wants existing-LLM-backend reuse). Endpoint+key-from-LLM-config inheritance is queued as task #282. |
| PWA generic select + button field renderers | v5.2.0 | `internal/server/web/app.js` — added `select` + `button` field types so future config blocks can wire dropdowns + action buttons declaratively. |
| Session detail — tmux arrow buttons | v5.2.0 | Operator note 2026-04-26: four buttons (↑↓←→) added next to the saved-commands quick row, sending the corresponding tmux escape sequence (`\x1b[A` etc.) via the existing `send_input` WS event. Mobile alignment in [datawatch-app#9](https://github.com/dmz006/datawatch-app/issues/9). |
| datawatch-app catch-up issue (#9) | v5.2.0 | Operator directive 2026-04-26: every PWA-visible change in v5.1.0 + v5.2.0 batched into [datawatch-app#9](https://github.com/dmz006/datawatch-app/issues/9) so the mobile shell can stay aligned: toolbar removal, history rename, FAB position (desktop + phone), BL178 reopen, BL198 drawer fix, About-tab additions, voice-input dropdown, BL191 PRD lifecycle surfaces, arrow keys. |
| Embedded docs — drop plans/ + add back button | v5.3.0 | Operator note 2026-04-26: `docs/plans/` should not ship inside the daemon binary (operator-internal). Added `plans` to `docs/_embed_skip.txt` so `make sync-docs` skips it. Also added a "← PWA" back button to the diagrams.html header so operators can return without browser back. |
| BL203 flexible LLM selection (backend + parity surfaces) | v5.4.0 | Operator directive: per-task and per-PRD worker LLM overrides with most-specific-wins fallthrough to stage default and global. Backend (`SetPRDLLM` / `SetTaskLLM` on Manager + executor uses resolved values) + REST (`set_llm`, `set_task_llm`) + CLI (`prd-set-llm`, `prd-set-task-llm`) + chat verbs + MCP tools all shipped. PWA dropdowns follow in the next cut. |
| BL288 stale node+channel.js MCP cleanup | v5.4.0 | Operator on v5.3.0 saw `/usr/bin/node ~/.datawatch/channel/channel.js` spawn for new sessions even though `[channel] using native Go bridge` was logged. Root cause: leftover `datawatch` (unsuffixed) entry in `claude mcp list` from before the Go-bridge migration (project-scope `.mcp.json`). Added `channel.CleanupStaleJSRegistrations()` that scans all scopes on daemon start and removes any `datawatch*` entry pointing at `node + channel.js`. |
| BL289 internal-ID leak scrub + voice test button + voice howto | v5.4.0 | Operator note: v5.3.0 voice-input label leaked `[task #282]` into operator UI. Removed; the only such leak in the PWA today. Plus a working **Test transcription endpoint** button wired to `POST /api/voice/test` (1 KB silent WAV through the configured backend), which force-disables `whisper.enabled` on failure so a broken backend doesn't keep firing. New `docs/howto/voice-input.md` covers all five backends (whisper local, openai, openai_compat, openwebui, ollama) with the inheritance rules. |
| BL291 since-v4 memory-leak audit | v5.5.0 | Operator on v5.4.0 hitting OOM. Daemon RSS itself was small (60 MB) but every behavior added since v4.0 was audited for unbounded growth / leaked descriptors / per-tick churn. Four fixes: (1) `session.GetLastResponse` 2-second TTL cache + bounded eviction (BL178 reopen v5.1.0 was re-capturing entire encrypted logs on every read); (2) `autonomous.PRD.Decisions` capped at 200 most-recent via `trimDecisions()` in `Store.SavePRD` (BL191 v5.2.0 appended without bound); (3) `observer.CorrelateCallers` short-circuits when no `Kind=Backend` envelope is present (BL180 Phase 2 v5.1.0 was opening /proc per tracked PID per tick regardless of scope); (4) PWA `state.lastResponse` map bounded to 128 entries with FIFO drop. |
| BL202 PWA full CRUD second cut | v5.5.0 | Replaced the v5.3.0 `prompt()` chains with proper modals carrying backend / effort / model dropdowns (live from `/api/backends`). PRD-create + per-task edit + new per-PRD "LLM" action all wired to v5.4.0 `set_llm` / `set_task_llm`. Per-PRD + per-task LLM badges render the current override inline. |
| BL292 leak audit pass 2 | v5.6.0 | Two real leaks found beyond BL291: (1) `session.Manager.promptOscillation` slice grew on every running↔waiting flip with no cap AND the map entry was never deleted on session removal — capped at 100 per session + cleanup on removal; (2) `autonomous.Store.AddLearning` was append-only with the rewrite-everything persist pattern — capped at 1000 most-recent (older learnings already mirrored into episodic memory + KG). Also dropped `promptFirstSeen` / `promptLastNotify` / `lastResponseCache` entries on session removal (same lifecycle gap). BL180 Phase 2 eBPF kprobe work backed out cleanly mid-edit (never compiled successfully); will resume in a separate cycle with `BPF_MAP_TYPE_LRU_HASH` + userspace TTL pruner. |
| BL187 | v5.0.4 (real fix) | First closed v4.8.12 as "no code change needed" — HTML was clean. Operator reopened on v5.0.3: still seeing the old "New" tab, no FAB. Root cause was `internal/server/web/sw.js`: app-shell was cache-first with a static `CACHE_NAME='datawatch-v2'`, so installed PWAs kept serving the pre-BL187 cached `index.html` / `app.js` even after every daemon upgrade. Fix: app-shell switched to network-first w/ cache fallback (offline still works) + `CACHE_NAME` bumped to `datawatch-v5` so existing installs invalidate cleanly on next activate. |
| BL194 | v4.8.11 | "MCP tools" link added to `/diagrams.html` header alongside the existing "API spec" link. |
| BL178 | v4.8.10 | `showResponseViewer` always fetches the live response; cached value shown first as "(updating…)" then patched in place. |
| BL190 | v4.9.3 | How-to suite complete: 6 docs (autonomous-planning, cross-agent-memory, prd-dag-orchestrator, container-workers, pipeline-chaining, daemon-operations) with per-channel reachability matrix on every walkthrough. PWA screenshots deferred to operator. |
| BL197 partial | v4.9.2 | Chat-channel autonomous PRD parity: `autonomous {status, list, get, decompose, run, cancel, learnings, create}` + `prd` alias. PWA PRD-lifecycle UI deferred to BL191 (naturally part of the same design conversation). |
| BL180 Phase 1 | v4.9.1 | Observer ollama runtime tap: per-loaded-model envelopes from `/api/ps` with Caller + CallerKind + GPUMemBytes. New `Envelope.Caller`/`CallerKind` fields for the Phase 2 eBPF correlation. |
| BL189 | v4.9.0 | Whisper backend factory: `whisper.backend = whisper | openai | openai_compat`. Local Python venv default (unchanged); operators can route to OpenWebUI / faster-whisper-server / cloud OpenAI / etc. via the new HTTP backend. Tests cover multipart shape, HTTP errors, anon auth, factory routing. |
| BL185 | v4.8.23 | Rate-limit parser extended to accept `"resets <time>"` (no "at") — the newer claude format. The auto-detect + auto-select-1 + schedule-resume pipeline was already wired since BL30; only the parser needed the new marker. |
| BL177 | v4.8.22 | eBPF arm64 artifacts: per-arch vmlinux.h tree + dual `//go:generate`; both arch `.{go,o}` committed; cross-build verified. |
| BL195 | v4.8.22 | Public container distribution: `.github/workflows/containers.yaml` matrix-pushes 8 images to GHCR on every tag; stats-cluster also `docker save`'d as a release asset. `make containers-push` / `containers-tarball` for local mirror. |
| BL177 longer-term | v5.0.2 | CI drift-check `.github/workflows/ebpf-gen-drift.yaml` — fails when committed eBPF artifacts drift from `netprobe.bpf.c`. |
| BL173 task 1 | v5.0.1 | eBPF kprobe attach loader wired (`loader_linux.go`): `loadNetprobeObjects` + four kprobes; partial attach non-fatal; BTF preloaded so no CAP_SYS_PTRACE. Unblocks BL180 Phase 2 structurally. |
| BL184 secondary | v5.0.1 | opencode-acp `Thinking... (reason)` renders as a visible italic line instead of an empty `<details>` wrapper. |
| BL184 | v4.8.20 | opencode-acp recognition lag: `markChannelReadyIfDetected` runs unconditionally on every output + chat_message WS event. (Thinking-message UX deferred.) |
| BL181 | v4.8.21 | eBPF BTF discovery via `/sys/kernel/btf/vmlinux` (no more CAP_SYS_PTRACE / /proc/self/mem requirement). Test verifies the path. |
| BL192 | v4.8.19 | Doc-coverage audit: docs/api/{voice,devices,sessions}.md added; architecture-overview rows point at the new operator references. |
| BL175 | v4.8.18 | docs duplication strategy: `docs/_embed_skip.txt` + `scripts/check-docs-sync.sh` + `hooks/pre-commit-docs-sync` + `.github/workflows/docs-sync.yaml` CI guard. Hybrid of (a) keep-rsync + (c) skip-manifest. |
| BL199 | v4.8.18 | `/diagrams.html` header — dropped "back to web UI" link; API spec + MCP tools now open in the current browser tab. |
| BL198 | v5.0.5 (real fix) | First closed v4.8.18 with `transform: translateX(-100%)` + `visibility:hidden` + `pointer-events:none` on the mobile aside-collapsed state. Operator reopened on v5.0.4: still saw a 1px strip on desktop collapse, and the docs/diagram pane went blank when collapsed on mobile. Two distinct bugs: (a) **desktop** — the 1px `border-right` on the aside leaked at x=-1 because `box-sizing:border-box` + grid col 0 didn't suppress it; fix added `border-right:none; width:0; visibility:hidden; overflow:hidden` on `.body.aside-collapsed aside`. (b) **mobile** — the desktop rule `.body.aside-collapsed { grid-template-columns: 0px 1fr }` won by specificity even inside the mobile media query. With aside `position:fixed` and out of grid flow, auto-placement put `main` into the 0px first cell so it rendered at ~28 px (just its padding) — the "blank screen" the operator reported. Fix added `.body.aside-collapsed { grid-template-columns: 1fr }` inside the mobile media query so the layout stays single-column when collapsed. Both verified via puppeteer at desktop-open / desktop-collapsed / mobile-default / mobile-open. |
| BL196 | v4.8.17 | Binary size: HTTP gzip middleware + `make cross` rebuilt with `-trimpath -s -w` and opt-in UPX pack. |
| BL193 | v4.8.15 | Full doc-comparison audit (llm-backends, api-mcp-mapping, messaging-backends, architecture-overview, data-flow) — internal IDs swept, tables cross-checked against code. |
| BL176 | v4.8.9 | RTK update string sweep: PWA chip, OpenAPI description, chat help all show the install.sh one-liner. |
| BL188 | v4.8.9 | Attribution guide refreshed — nightwire credit expanded, Aperant noted under "Researched and skipped", operator-action note for BL117/BL33/F10/BL173 follow-ups. |
| BL182 | v4.8.8 | "Input Required" yellow popup now patches in place from the WebSocket state-change event — no more back-out/re-enter. |
| BL183 | v4.8.8 | Orphan-cleanup affordance always visible in Settings → Monitor → System Statistics (was hidden when count was zero). |
| BL186 | v4.8.8 | CLI long-help + setup epilogue swept of internal IDs (Shape A/B/C → operator-language). |
| Release-discipline rules | v4.8.8 | Two new rules: README marquee must reflect current release; backlog refactor each release. |
| Settings → Show inline doc links | v4.8.7 | Per-browser localStorage toggle in Settings → General; inline `docs` chip next to high-value section headers (Proxy Resilience, LLM Configuration, Communication Configuration, System Statistics) deep-links into `/diagrams.html#docs/...`. Honors the toggle. |
| Proxy-flow recursive variant | v4.8.7 | New mermaid flow + loop-prevention notes added to `docs/flow/proxy-flow.md`. |
| BL179 | v4.8.6 | Search-icon to header bar (left of daemon-status light); in-card duplicate removed. |
| `/diagrams.html` UX | v4.8.5 | Collapsible sidebar, mobile responsive, marked.js renders prose for files without mermaid blocks. |
| Diagram + flow refactor | v4.8.3 / v4.8.4 | Renamed flow files (orchestrator-flow / observer-flow / agent-spawn-flow); cleaned BL/F/Sprint/Shape from titles + body + diagrams; added Mermaid flowcharts so they render in `/diagrams.html`. |
| PWA internal-ID sweep | v4.8.1 / v4.8.2 | eBPF noop msg, federated peers card empty-state, profile placeholder, Cluster nodes subscript. |
| S14a foundation | v4.8.0 | `observer.federation.parent_url` + push-with-chain loop prevention + `Envelope.Source` attribution. **Remaining (v4.8.x):** root-side envelope rewriter, PWA Cluster filter pill, `observer_primary_list` MCP alias. |
| S13 follow — orchestrator integration | v4.7.2 | Per-node `observer_summary` join across local + peers in `GET /api/orchestrator/graphs/{id}`. |
| B44 | v4.7.1 | PWA sessions search-icon toggle (mobile parity). |
| BL173-followup verification | 2026-04-25 | Shape C image build + push + dry-run + harbor push validated. |
| BL174 verification | 2026-04-25 | Image-size deltas captured. agent-opencode -50 MB; agent-claude +6 MB; stats-cluster 11 MB. |
| Plugins list shows datawatch-observer | Verified live v4.8.2 | `/api/plugins` returns `native[]` correctly; bug was a v4.7.x snapshot. |

## Frozen Features

| # | Description | Status | Notes |
|---|-------------|--------|-------|
| F7  | libsignal — replace signal-cli with native Go | 🧊 frozen 2026-04-20 | Signal-cli is working and stable; 3–6 mo rewrite deferred until there's a concrete operational need. Plan kept at [2026-03-29-libsignal.md](2026-03-29-libsignal.md). |


## Frozen / external

| ID | Item | Notes |
|----|------|-------|
| BL174 stretch | Distroless / alpine spike for agent-base — would shrink ~250 MB further. Defer until image-size telemetry shows headroom worth chasing. | Defer. |
| S14b | Per-pod alert rules + observer-driven autoscaling. Depends on S14a so federated envelopes can be alert subjects. | Target v4.9.0. |
| S14c | ROCm + Intel level_zero scrapers in Shape C. Needs hardware to validate. | Target v5.0.0. |
| Mobile parity | datawatch-app Compose Multiplatform follow-ups [#2](https://github.com/dmz006/datawatch-app/issues/2) federated peers · [#3](https://github.com/dmz006/datawatch-app/issues/3) cluster nodes · [#4](https://github.com/dmz006/datawatch-app/issues/4) eBPF status · [#5](https://github.com/dmz006/datawatch-app/issues/5) native plugins · [#6](https://github.com/dmz006/datawatch-app/issues/6) Agents filter pill · [#7](https://github.com/dmz006/datawatch-app/issues/7) per-node observer_summary badge. | External repo. |
| Future sprint S14+ | Cross-cluster federation tree, per-pod alert routing, observer-driven autoscaling, ROCm / Intel level_zero. | Not yet specced. |

---

## Backlog — Sprint Plan

**v4.0.0 shipped 2026-04-20. Every planned S1–S8 backlog item is now landed.** The backlog remaining is operational tail (BL85 RTK auto-update, BL166 helm-tool re-add), long-running / frozen items, and v4.0.x follow-ups (web UI cards, BL103-per-guardrail wiring, etc.).

### Shipped timeline (v3.0.0 → v4.0.0)

| Release | Sprint | Items | Release notes |
|---------|--------|-------|---------------|
| v3.0.0 | F10 landing | 25 items (F10 platform, validator, peer broker, orchestrator bridge, mobile surface, memory federation) | [notes](RELEASE-NOTES-v3.0.0.md) |
| v3.1.0 | Test infra + B30 | 3 items (BL89/90/91, B30 fix) | [notes](RELEASE-NOTES-v3.1.0.md) |
| v3.2.0 | Intelligence core | 2 items (BL28 gates, BL39 cycle detection) | [notes](RELEASE-NOTES-v3.2.0.md) |
| v3.3.0 | Observability | 3 items (BL10/11/12) | [notes](RELEASE-NOTES-v3.3.0.md) |
| v3.4.0 / v3.4.1 | Operations + Windows fix | 4 items (BL17/22/37/87) + windows cross-build | [notes](RELEASE-NOTES-v3.4.0.md) |
| v3.5.0 | S1 — quick wins + UI | 5 items (BL1/34/35/41 + F14) | [notes](RELEASE-NOTES-v3.5.0.md) |
| v3.6.0 | S2 — sessions productivity | 6 items (BL5/26/27/29/30/40) | [notes](RELEASE-NOTES-v3.6.0.md) |
| v3.7.0 / v3.7.1 | S3 — cost + audit | 3 items (BL6/86/9) + cost-rates hotfix | [notes](RELEASE-NOTES-v3.7.0.md) |
| v3.7.2 | Sx — parity backfill | 20 MCP tools + 9 CLI commands (v3.5–v3.7 endpoints) | CHANGELOG |
| v3.7.3 | Sx2 — comm + mobile parity | comm router + mobile surface doc | CHANGELOG |
| v3.8.0 | S4 — messaging + UI | 4 items (BL15/31/42/69) | CHANGELOG |
| v3.9.0 | S5 — backends + chat UI | 4 items (BL20/78/79/72) | CHANGELOG |
| v3.10.0 | S6 — autonomous | 2 items (BL24/BL25) | [design](2026-04-20-bl24-autonomous-decomposition.md) · [usage](../api/autonomous.md) |
| v3.11.0 | S7 — plugin framework | 1 item (BL33) | [design](2026-04-20-bl33-plugin-framework.md) · [usage](../api/plugins.md) |
| **v4.0.0** | **S8 — PRD-DAG orchestrator** | **1 item (BL117) + cumulative release notes** | [design](2026-04-20-bl117-prd-dag-orchestrator.md) · [usage](../api/orchestrator.md) · [v3.0→v4.0 cumulative](RELEASE-NOTES-v4.0.0.md) |

Frozen / dropped: F13/BL19 (dropped), BL38 (dropped), BL45 (frozen), BL7 + BL8 (multi-user — frozen). F7 (libsignal) stays open as long-running.

### v4.0.1 — shipped 2026-04-20 (follow-up patch)

Every item flagged as a v4.0.x follow-up in v4.0.0 landed in v4.0.1, plus BL85, BL166, the directory-picker ergonomic, Aperant review, and the F7 freeze:

| Item | Status |
|---|---|
| Web UI Settings cards for autonomous / plugins / orchestrator | ✅ shipped — 14 new fields under General tab (7 autonomous, 3 plugins, 4 orchestrator) |
| BL117 real GuardrailFn (per-guardrail system prompt via `/api/ask`) | ✅ shipped — replaces the v1 stub; unparseable/unreachable → `warn`, doesn't halt the graph |
| Autonomous executor → `session.Manager.Start` wiring | ✅ shipped — `SpawnFn` loopback to `/api/sessions/start`, `VerifyFn` via `/api/ask`, fires async from `POST .../run` |
| Plugin hot-reload via fsnotify | ✅ shipped — `Registry.Watch(ctx)`, 500 ms debounce, wired at startup when `plugins.enabled` |
| `internal/server/web/openapi.yaml` resync | ✅ shipped — regenerated from `docs/api/openapi.yaml` |
| **BL85** — RTK auto-update REST surface | ✅ shipped — `GET /api/rtk/version`, `POST /api/rtk/check`, `POST /api/rtk/update`; background checker was already wired |
| **BL166** — tools-ops helm re-add | ✅ shipped — get.helm.sh reachable; installed from tarball with TARGETARCH |
| Directory-picker "create folder" | ✅ shipped — `POST /api/files` with `{path, name}`; root-path clamp enforced |
| Aperant integration review | ✅ skipped — AGPL-3.0 + Electron desktop app; sits on same claude-code layer; no headless API. Skip per 2026-04-20 research. |
| F7 libsignal | 🧊 frozen — deferred until a concrete need surfaces |

---

### Sprint S1 — Quick wins + UI diff → v3.5.0 — **shipped**

Five low-to-medium-risk items shipped in v3.5.0.

| ID | Item | Status |
|----|------|--------|
| BL1  | IPv6 listener support               | ✅ shipped — IPv6-safe `joinHostPort` at every bind site; `[::]:port` enables dual-stack |
| BL34 | Read-only ask mode                  | ✅ shipped — `POST /api/ask` (Ollama + OpenWebUI backends, no session, no tmux) |
| BL35 | Project summary command             | ✅ shipped — `GET /api/project/summary?dir=` (git status + commits + per-project session stats) |
| BL41 | Effort levels per task              | ✅ shipped — `Session.Effort` (quick/normal/thorough); REST + config + reload + UI parity |
| F14  | Live cell DOM diffing               | ✅ shipped — `tryUpdateSessionsInPlace()` per-card diff before falling back to full render |

### Sprint S2 — Sessions productivity → v3.6.0 — **shipped**

Six items shipped in v3.6.0.

| ID | Item | Status |
|----|------|--------|
| BL5  | Session templates                   | ✅ shipped — `/api/templates` CRUD + `template:` start field |
| BL26 | Recurring schedules                 | ✅ shipped — `recur_every_seconds` + `recur_until` on ScheduledCommand |
| BL27 | Project management                  | ✅ shipped — `/api/projects` CRUD + `project:` start field |
| BL29 | Git checkpoints + rollback          | ✅ shipped — `datawatch-pre/post-{id}` tags + `POST /api/sessions/{id}/rollback` |
| BL30 | Rate-limit cooldown                 | ✅ shipped — `/api/cooldown` (G/P/D) + `session.rate_limit_global_pause` opt-in |
| BL40 | Stale task recovery                 | ✅ shipped — `/api/sessions/stale` + `session.stale_timeout_seconds` |

### Sprint S3 — Cost + observability tail → v3.7.0 — **shipped**

Three items shipped in v3.7.0.

| ID | Item | Status |
|----|------|--------|
| BL6  | Cost tracking                       | ✅ shipped — `Session.tokens_in/out/est_cost_usd` + `/api/cost` + `/api/cost/usage` + per-backend rate table |
| BL86 | Remote GPU/system stats agent       | ✅ shipped — `cmd/datawatch-agent/` (linux-amd64/arm64) — `GET /stats` returns GPU+CPU+memory+disk JSON |
| BL9  | Audit log                           | ✅ shipped — append-only JSONL at `<data_dir>/audit.log` + `GET /api/audit` with filters |

### Sprint Sx — Parity backfill → v3.7.2 — **shipped**

**Audit finding 2026-04-20.** Endpoints shipped in v3.5.0–v3.7.0
had REST + YAML surfaces but were missing MCP / CLI parity, plus
end-to-end functional testing through a running daemon. v3.7.2
addresses the gap:

- **20 MCP tools** in `internal/mcp/sx_parity.go` (REST loopback proxies)
- **9 CLI subcommands** in `cmd/datawatch/cli_sx_parity.go`
- **Functional smoke** verified against a live daemon on port 18080;
  every endpoint returns valid JSON, POST/DELETE round-trips persist,
  cost-rate override applied to live `Manager` correctly.

**Sx2 → v3.7.3 (shipped 2026-04-20):**
- Comm router commands `cost`, `stale`, `audit`,
  `cooldown` (status/set/clear), and a generic `rest <METHOD> <PATH>
  [json]` passthrough that reaches every other Sx endpoint from chat.
- Mobile API surface documented at `docs/api/mobile-surface.md` —
  inventory of every v3.5–v3.7 endpoint plus use-case mapping for
  the `datawatch-app` paired client.

Full parity (REST + YAML + MCP + CLI + comm + mobile + web) for
v3.5–v3.7 is now achieved. S4 can start clean.

| Endpoint | Sprint shipped | Gaps |
|---|---|---|
| `/api/ask` (BL34) | S1 | MCP, comm, CLI |
| `/api/project/summary` (BL35) | S1 | MCP, comm, CLI |
| `/api/templates` (BL5) | S2 | MCP, comm, CLI, UI |
| `/api/projects` (BL27) | S2 | MCP, comm, CLI, UI |
| Recurring schedule fields (BL26) | S2 | MCP/comm/CLI for setting `recur_every_seconds` |
| `/api/sessions/{id}/rollback` (BL29) | S2 | MCP, comm, CLI |
| `/api/cooldown` (BL30) | S2 | MCP, comm, CLI |
| `/api/sessions/stale` (BL40) | S2 | MCP, comm, CLI |
| `/api/cost`, `/api/cost/usage`, `/api/cost/rates` (BL6) | S3 / v3.7.1 | MCP, comm, CLI |
| `/api/audit` (BL9) | S3 | MCP, comm, CLI |
| `datawatch-agent` (BL86) | S3 | parent integration (config, polling adapter, dashboard surface) |

Plus: **functional testing** for each — start a daemon, exercise the
endpoint via every channel, confirm round-trip works, then teardown.

This sprint MUST complete before S4 starts so we don't compound the
gap. Estimate ~2-3 days.

### Sprint S4 — Messaging + UI polish → v3.8.0 — **shipped**

| ID | Item | Status |
|----|------|--------|
| BL15 | Rich previews in alerts             | ✅ shipped — `messaging.FormatAlert` (Telegram MD escaping, Signal mono, Slack/Discord passthrough) + opt-in `session.alerts_rich_format` |
| BL31 | Device targeting (`@device` routing) | ✅ shipped — `session.device_aliases` config + `/api/device-aliases` CRUD |
| BL69 | Splash screen — custom logo         | ✅ shipped — `session.splash_logo_path/tagline` + `GET /api/splash/{logo,info}` |
| BL42 | Quick-response assistant            | ✅ shipped — `POST /api/assist` with dedicated assistant_* config |

Full parity for each: REST + YAML + MCP tool + CLI subcommand + comm + mobile (REST is mobile-friendly).

### Sprint S5 — Backends + chat UI → v3.9.0 — **shipped**

| ID | Item | Status |
|----|------|--------|
| BL20 | Backend auto-selection (routing rules) | ✅ shipped — `session.routing_rules` + `/api/routing-rules` + `/api/routing-rules/test` + MCP/CLI parity |
| BL78 | Chat UI: Gemini chat mode           | ✅ documented (config recipe at `docs/api/chat-mode-backends.md`) — `gemini.output_mode: chat` |
| BL79 | Chat UI: Aider/Goose chat mode      | ✅ documented — same `output_mode: chat` recipe for Aider + Goose |
| BL72 | OpenCode memory hooks               | ✅ documented — opencode chat-mode reuses BL65 memory hook path |

### Sprint S6 — Intelligence → v3.10.0 ✅ SHIPPED 2026-04-20

Design doc: [`2026-04-20-bl24-autonomous-decomposition.md`](2026-04-20-bl24-autonomous-decomposition.md) — maps every nightwire component to a datawatch primitive. Operator doc: [`../api/autonomous.md`](../api/autonomous.md).

| ID | Item | Status |
|----|------|--------|
| BL24 | Autonomous task decomposition       | ✅ shipped — `internal/autonomous/` package (models, JSONL store, decompose prompt+parser, security scanner, manager, executor with topo-sort + auto-fix retry), REST `/api/autonomous/*` + 10 MCP tools + `datawatch autonomous` CLI + comm via `rest` passthrough + `autonomous.*` YAML |
| BL25 | Independent verification            | ✅ shipped — `VerifyFn` indirection in executor; BL103 validator agent wiring deferred to v3.10.x patch |

### Sprint S7 — Extensibility → v3.11.0 ✅ SHIPPED 2026-04-20

Design doc: [`2026-04-20-bl33-plugin-framework.md`](2026-04-20-bl33-plugin-framework.md) — rejects `.so` / Lua; selects subprocess + JSON-RPC over stdio. Operator doc: [`../api/plugins.md`](../api/plugins.md).

| ID | Item | Status |
|----|------|--------|
| BL33 | Plugin framework                    | ✅ shipped — `internal/plugins/` subprocess driver, manifest discovery, 4 hooks, fan-out chaining, timeout/error stats; REST `/api/plugins/*` + 6 MCP tools + `datawatch plugins` CLI + comm via `rest` + `plugins.*` YAML. Disabled by default. |

### Sprint S8 — PRD-DAG orchestrator → **v4.0.0** ✅ SHIPPED 2026-04-20

Design doc: [`2026-04-20-bl117-prd-dag-orchestrator.md`](2026-04-20-bl117-prd-dag-orchestrator.md). Operator doc: [`../api/orchestrator.md`](../api/orchestrator.md). **Cumulative release notes** covering every shipped item since v3.0.0: [`RELEASE-NOTES-v4.0.0.md`](RELEASE-NOTES-v4.0.0.md).

| ID | Item | Status |
|----|------|--------|
| BL117 | PRD-driven DAG orchestrator + guardrail sub-agents | ✅ shipped — `internal/orchestrator/` package (Graph/Node/Verdict, JSONL store, Runner with Kahn topo-sort and verdict aggregation). 4 guardrail types (rules/security/release-readiness/docs-diagrams-architecture) with v1 stub GuardrailFn; plugin `on_guardrail` hook available for real guardrails. REST `/api/orchestrator/*` + 9 MCP tools + `datawatch orchestrator` CLI + comm via `rest` + `orchestrator.*` YAML. |

---

### Sprint summary

| Sprint | Items | Releases | Effort | Status |
|--------|-------|----------|--------|--------|
| S1 | 5 (4 quick wins + F14 DOM diff) | v3.5.0  | 1 day    | ✅ shipped |
| S2 | 6 sessions/productivity         | v3.6.0  | 1 week   | ✅ shipped |
| S3 | 3 cost + obs tail (+ new binary)| v3.7.0  | 1 week   | ✅ shipped (REST/YAML only — Sx gates full parity) |
| Sx | Parity backfill for v3.5–v3.7   | v3.7.2  | 2-3 days | ✅ shipped — MCP (20 tools) + CLI (9 commands) + functional smoke verified |
| Sx2| Comm + mobile parity            | v3.7.3  | 0.5 day  | ✅ shipped — router commands + mobile API surface doc |
| S4 | 4 messaging + UI polish         | v3.8.0  | 3 days   | ✅ shipped |
| S5 | 4 backends + chat UI            | v3.9.0  | 3 days   | ✅ shipped |
| S6 | 2 intelligence (BL24 + BL25)    | v3.10.0 | 2 weeks  | ✅ shipped — [design](2026-04-20-bl24-autonomous-decomposition.md) · [usage](../api/autonomous.md) |
| S7 | 1 plugin framework (BL33)       | v3.11.0 | 3 days   | ✅ shipped — [design](2026-04-20-bl33-plugin-framework.md) · [usage](../api/plugins.md) |
| S8 | 1 PRD-DAG orchestrator (BL117)  | **v4.0.0** | 2-3 weeks | ✅ shipped — [design](2026-04-20-bl117-prd-dag-orchestrator.md) · [usage](../api/orchestrator.md) · [v3.0→v4.0 release notes](RELEASE-NOTES-v4.0.0.md) |

---

### Per-category snapshot (cross-reference)

Quick reference. The sprint plan above is the source of truth — these tables only group items by domain so plans are easy to find.

| Category | Active items | Sprint(s) |
|---|---|---|
| **Sessions** | BL117 future (all S2/S3 sessions items shipped) | S8 |
| **Intelligence** | _(complete — BL24, BL25 shipped in v3.10.0)_ | — |
| **Observability** | _(complete — all shipped)_ | — |
| **Collaboration** | _(BL9 shipped; BL7 + BL8 frozen)_ | — |
| **Messaging** | _(complete — BL15, BL31 shipped)_ | — |
| **Backends & UI** | _(complete — BL20 shipped, BL78/BL79 documented)_ | — |
| **Memory & Security** | _(complete — BL72 documented)_ | — |
| **Extensibility** | _(complete — BL33 shipped in v3.11.0)_ | — |

Per-item plans live in [`2026-04-11-backlog-plans.md`](2026-04-11-backlog-plans.md). Quick-effort items are flagged with ⚡ in the sprint tables above.

> **Already shipped:** Operations (v3.4.0: BL17/22/37/87), Observability core (v3.3.0: BL10/11/12), Intelligence core (v3.2.0: BL28/39), Testing infrastructure (v3.1.0: BL89/90/91), and 25 items in v3.0.0 (BL92–BL116). See per-version release notes for the full shipped list.

---

## Completed

### Bugs Fixed

| # | Description | Fixed |
|---|-------------|-------|
| B1 | xterm.js crashes and slow load (20s → 32ms) | v2.3.0 |
| B2 | Claude Code prompt detection false positives | v2.3.1 |
| B3 | LLM session reconnect on daemon restart | v2.2.9 |
| B4 | Input bar sometimes disappears in session detail | v2.3.8 |
| B5 | Session history controls off-screen on mobile | v2.3.8 |
| B6 | Function parity gaps across API/MCP/CLI/comm | v2.4.1 |
| B7 | Code test coverage 11.2% → 14.5% (318 tests, pure-logic ceiling reached) | v2.4.1 |
| B20 | RTK update available not showing in Monitor page stats card | v2.4.1 |
| B21 | Monitor Infrastructure card shows wrong protocol and bad formatting | v2.4.1 |
| B22 | Daemon crashes from unrecovered panics in background goroutines | v2.4.3 |
| B23 | Silent daemon death — remaining goroutine recovery, BPF map purge, crash log | v2.4.4 |
| B24 | Update check shows downgrade as "update available" (semver compare in UI/router/auto-updater) | v2.4.4 |
| B25 | Trust prompt invisible — MCP spinner hides what user needs to do (full prompt context in card + Input Required banner with key tip) | v2.4.5 |
| B31 | In-app upgrade reports success but doesn't replace binary — asset name mismatch between updater and release (pre-existing since v2.x; surfaced on v3.0.0 upgrade) | v3.0.1 |
| B30 | Scheduled command lands in prompt but requires a 2nd Enter to activate (claude-code TUI phase-4 race) | v3.1.0 |
| B32 | Tmux/scheduled command executes with a blank line, operator has to press Enter again to submit — trailing `\n` in the payload was interpreted by TUIs as multi-line input so the explicit Enter just added another blank | v4.0.2 |
| B33 | PWA "Input Required" yellow card stays visible after sending a reply; only disappears on session reconnect — added auto-dismiss on send + manual X button; re-appears on next distinct prompt | v4.0.2 |
| B34 | Most tmux commands still required a second Enter to submit even after the B32 trim — bracketed-paste TUIs fold a single-call `Enter` into the paste event; `SendKeys` now always uses the two-step push-then-Enter pattern with a 120 ms settle | v4.0.4 |
| B35 | Diagram viewer on the Settings → About tab showed "Failed to load docs/architecture.md: Failed to fetch" on first open — the service worker was serving a stale v1 cache that hadn't seen the new `/docs/` path. Bumped cache name to `datawatch-v2` and made `/docs/*` + `/diagrams.html` network-first. | v4.0.7 |
| B36 | PWA user-facing strings listed internal ticket IDs (e.g. "Autonomous PRD decomposition (BL24+BL25)", "Plugin framework (BL33)", "Default effort (BL41)"). Stripped the parenthetical ticket refs; added a project rule that forbids BL/F/B/S numbers in any operator-facing surface (web, mobile, comm, CLI user output). | v4.0.7 |
| B37 | Auto-install RTK manual-install suggestion pointed at the old release-asset URL; the operator-preferred upstream path is `curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh`. Updated the CLI fallback message and `docs/rtk-integration.md`. | v4.0.7 |
| B38 | PWA + mobile Settings saves for the Autonomous / Plugins / Orchestrator sections silently no-op'd — `applyConfigPatch` had no case branches for `autonomous.*`, `plugins.*`, `orchestrator.*`, so unknown keys fell through the switch while the handler still returned 200. Added case-branches for all 17 keys plus a `default:` that logs unknown keys to stderr so future schema drift surfaces instead of silently dropping. Closes [issue #19](https://github.com/dmz006/datawatch/issues/19). | v4.0.8 |

### Features & Backlog Completed

| ID | Item | Version |
|----|------|---------|
| BL23 | Episodic memory (SQLite + embeddings) | v1.3.0 |
| BL32 | Semantic search across sessions | v1.3.0 |
| BL36 | Task learnings capture | v1.3.0 |
| BL44 | Memory: auto-retrieve on session start | v1.4.0 |
| BL46 | Memory: export/import | v1.4.0 |
| BL48 | Memory: browser enhancements | v1.4.0 |
| BL50 | Memory: embedding cache | v1.4.0 |
| BL52 | Memory: session output auto-index | v1.4.0 |
| BL62 | Memory: write-ahead log | v1.4.0 |
| BL63 | Memory: deduplication | v1.4.0 |
| BL55 | Memory: spatial organization (wings/rooms/halls) | v1.5.0 |
| BL56 | Memory: 4-layer wake-up stack | v1.5.0 |
| BL57 | Memory: temporal knowledge graph | v1.5.0 |
| BL58 | Memory: verbatim storage mode | v1.5.0 |
| BL60 | Memory: entity detection | v1.5.0 |
| BL68 | Memory: hybrid content encryption | v1.5.1 |
| BL70 | Memory: key rotation and management | v1.5.1 |
| BL54 | Memory: REST API enhancements | v1.6.0 |
| BL61 | Memory: MCP KG tools | v1.6.0 |
| BL47 | Memory: retention policies | v2.0.0 |
| BL49 | Memory: cross-project search | v2.0.0 |
| BL51 | Memory: batch reindexing | v2.0.0 |
| BL53 | Memory: learning quality scoring | v2.0.0 |
| BL59 | Memory: conversation mining | v2.0.0 |
| BL64 | Memory: cross-project tunnels | v2.0.0 |
| BL65 | Memory: Claude Code auto-save hook | v2.0.0 |
| BL66 | Memory: pre-compact hook | v2.0.0 |
| BL67 | Memory: mempalace import | v2.0.0 |
| BL43 | Memory: PostgreSQL+pgvector backend | v2.0.2 |
| BL73 | Rich chat UI (bubbles, avatars, markdown) | v2.1.3 |
| BL77 | Chat UI: Ollama native chat mode | v2.2.0 |
| BL80 | Chat UI: image/diagram rendering | v2.2.0 |
| BL81 | Chat UI: thinking/reasoning overlay | v2.2.0 |
| BL82 | Chat UI: conversation threads | v2.2.0 |
| BL83 | OpenCode-ACP rich chat interface | v2.3.1 |
| BL84 | Tmux history scrolling | v2.3.4 |
| BL85 | RTK auto-update check | v2.3.5 |
| BL88 | `POST /api/memory/save` endpoint | v2.3.8 |
| F4  | Channel parity (threaded conversations)         | v1.0.2 |
| F8  | Health check endpoint                           | v1.0.2 |
| F9  | Fallback chains                                 | v1.0.2 |
| F11 | Voice input (Whisper)                           | v1.1.0 |
| F12 | Prometheus metrics                              | v1.0.2 |
| F15 | Session chaining — pipeline DAG executor        | v2.4.0 |
| F10 | Ephemeral container-spawned agents              | v3.0.0 |
| F17 | Mobile device registry (`POST /api/devices/register`) — closes GH [#1](https://github.com/dmz006/datawatch/issues/1) | v3.0.0 |
| F18 | Voice transcription (`POST /api/voice/transcribe`) — closes GH [#2](https://github.com/dmz006/datawatch/issues/2) | v3.0.0 |
| F19 | Federation fan-out (`GET /api/federation/sessions`) — closes GH [#3](https://github.com/dmz006/datawatch/issues/3) | v3.0.0 |
| BL89 | Mock session manager for unit tests (TmuxAPI interface + FakeTmux) | v3.1.0 |
| BL90 | httptest server for API endpoint tests | v3.1.0 |
| BL91 | MCP tool handler tests (direct handler invocation) | v3.1.0 |
| BL28 | Quality gates (test baseline + regression detection wired into Executor) | v3.2.0 |
| BL39 | Circular dependency detection (NewPipeline rejects cycles, DFS path output) | v3.2.0 |
| BL10 | Session diffing — git shortstat captured into Session.DiffSummary on completion | v3.3.0 |
| BL11 | Anomaly detection — stuck-loop / long-input-wait / duration-outlier helpers | v3.3.0 |
| BL12 | Historical analytics — `GET /api/analytics?range=Nd` day-bucket aggregation | v3.3.0 |
| BL17 | Hot config reload — SIGHUP + `POST /api/reload` re-applies hot-reloadable subset | v3.4.0 |
| BL22 | RTK auto-install — `datawatch setup rtk` downloads platform binary into ~/.local/bin | v3.4.0 |
| BL37 | System diagnostics — `GET /api/diagnose` health checks (tmux, sessions, disk, goroutines) | v3.4.0 |
| BL87 | `datawatch config edit` — visudo-style safe editor with validate-on-save loop | v3.4.0 |
| BL1  | IPv6 listener support — every bind via `net.JoinHostPort`; `[::]:port` dual-stack | v3.5.0 |
| BL34 | Read-only ask mode — `POST /api/ask` (Ollama + OpenWebUI), no session/tmux | v3.5.0 |
| BL35 | Project summary — `GET /api/project/summary?dir=` git + per-project session stats | v3.5.0 |
| BL41 | Effort levels per task — `Session.Effort` (quick/normal/thorough), full config parity | v3.5.0 |
| F14  | Live cell DOM diffing — `tryUpdateSessionsInPlace()` per-card diff path | v3.5.0 |
| BL5  | Session templates — `/api/templates` CRUD + `template:` start field | v3.6.0 |
| BL26 | Recurring schedules — `recur_every_seconds` + `recur_until` on ScheduledCommand | v3.6.0 |
| BL27 | Project management — `/api/projects` CRUD + `project:` start field | v3.6.0 |
| BL29 | Git checkpoints + rollback — pre/post tags + `POST /api/sessions/{id}/rollback` | v3.6.0 |
| BL30 | Rate-limit cooldown — `/api/cooldown` + opt-in `rate_limit_global_pause` | v3.6.0 |
| BL40 | Stale task recovery — `/api/sessions/stale` + configurable threshold | v3.6.0 |
| BL6  | Cost tracking — Session.tokens_in/out/est_cost_usd + `/api/cost` + per-backend rates | v3.7.0 |
| BL86 | Remote GPU/system stats agent — `cmd/datawatch-agent/` standalone binary | v3.7.0 |
| BL9  | Operator audit log — append-only JSONL + `/api/audit` filtered query | v3.7.0 |

### Promoted to Features

Per the no-reuse rule, the original BL numbers stay reserved. Status reflects the current state of the parent F-feature.

| BL  | Promoted to | Status |
|-----|-------------|--------|
| BL2 | F14 (Live cell DOM diffing) | Open (F14 still in Open Features) |
| BL3 | F10 (Ephemeral container-spawned agents) | Shipped in v3.0.0 |
| BL4 | F15 (Session chaining — pipeline DAG executor) | Shipped in v2.4.0 |

### Dropped / Frozen

Numbers stay reserved (per the rule above) and are never reused.

| ID | Decision | Date | Reason |
|----|----------|------|--------|
| F13 | Dropped | 2026-04-19 | Copilot/Cline/Windsurf backends — operator decided not to support |
| BL19 | Dropped (with F13) | 2026-04-19 | Original BL that was promoted to F13 |
| BL38 | Dropped | 2026-04-19 | Message content privacy — operator decided not to pursue |
| BL45 | Frozen | 2026-04-19 | ChromaDB/Pinecone/Weaviate backends — operator unsure if needed; revisit if pgvector hits a limit |
| BL7  | Frozen | 2026-04-19 | Multi-user access control — single-operator use stays the supported model for now; no work scheduled |
| BL8  | Frozen | 2026-04-19 | Session sharing (time-limited links) — depends on BL7's auth model; frozen with BL7 |


See [testing.md](../testing.md) for test results and pre-release checklists.
