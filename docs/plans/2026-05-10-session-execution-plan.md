# Session execution plan — 2026-05-10 overnight

**Directive:** Complete all open v7.0 items, howto review + screenshots, docs audit. No stopping.
**Core rule:** Do not break session 5ed0 (current Claude Code session).

---

## Status snapshot

### LLM Enabled Models overhaul (2026-05-10-llm-enabled-models-overhaul.md)

| Section | Status |
|---|---|
| A. Data model (Models[], AutoAddModels, back-compat) | ✅ Done |
| B. REST endpoints (in_use, refresh_models, reassign, force_delete) | ✅ Done |
| C. MCP tools (llm_in_use, llm_add_model, llm_remove_model, llm_list_models, llm_refresh_models) | ✅ Done |
| C. Comm verbs (llm models add/remove/list, llm in-use) | ✅ Done |
| C. CLI commands (models add/remove/list, in-use, refresh-models, reassign, force-delete) | ❌ Missing |
| D. PWA Edit/Add panel (per-node table, kind-aware) | ✅ Done |
| D. PWA In-use envelope | ✅ Done |
| D. PWA Block-on-delete modal + reassign flow | ✅ Done |
| E. Locale × 5 | ✅ Done |
| F. Mobile parity issue (datawatch-app) | ❌ Not filed |
| G. Smoke tests (6 new LLM checks) | ❌ Missing |
| H. CHANGELOG entry | ❌ Missing |

### v7.0 readiness items (2026-05-10-v7.0-readiness.md)

| Item | Status |
|---|---|
| Observer/monitoring error (wrong peer name in detail handler) | ❌ Fix ready, needs build |
| Automata "filter" subheader investigation | ❌ Could not reproduce; flag in GATE |
| automataCancel still uses browser confirm() | ❌ Fix pending |
| embedder_node settings type in app.js | ❌ Uncommitted |
| #183 CSS/UX items (#3-#7) | ❌ Need visual verify |
| #212 Howto screenshots | ❌ Sprint 4 |

### Howtos (27 files, 0 screenshots)

All 27 howtos likely need updates for v7.0 changes (Compute nodes, LLMs overhaul, observer, sessions, alerts, automata). Full review + screenshots in Sprint 4.

---

## Sprint 1: Bug fixes + build (alpha.38)

**Steps:**
1. [x] Fix `handleComputeNodeDetail` — use `ObserverPeer` for peer snapshot lookup
2. [ ] Fix `automataCancel` — use `showConfirmModal` instead of `confirm()`
3. [ ] Commit all uncommitted app.js changes (embedder_node type)
4. [ ] Bump version to alpha.38
5. [ ] Build binary (`rtk go build`)
6. [ ] Stop daemon, install, restart
7. [ ] Visual verify: compute node live detail no longer errors
8. [ ] Smoke test
9. [ ] Commit + tag + release

**Rule requirements:**
- 7-surface parity: observer fix is server-only (not new feature, bug fix — no surface audit needed)
- Smoke: run release-smoke.sh before tagging
- Locale: no new UI strings in this sprint
- Mobile parity: no new PWA features; no app issue needed
- CHANGELOG: add alpha.38 entry with bug fixes

---

## Sprint 2: LLM Overhaul — CLI + Smoke + Mobile parity (alpha.39)

**Steps:**
1. [ ] Add CLI commands to `cmd/datawatch/cli_llm.go`:
   - `llm models list <name>`
   - `llm models add <name> --node <cn> --model <m>`
   - `llm models remove <name> --node <cn> --model <m>`
   - `llm in-use <name> [--filter <text>] [--page N] [--size 5|10|50]`
   - `llm refresh-models <name>`
   - `llm reassign <name> --to-llm <other> [--to-model <m>]`
   - `llm force-delete <name> --confirm "yes I understand this terminates active work"`
2. [ ] Add smoke tests to `scripts/release-smoke.sh`:
   - `llm_enabled_models_basic`
   - `llm_in_use_pagination`
   - `llm_auto_add_models`
   - `llm_back_compat_load`
   - `llm_delete_409`
   - `llm_reassign_active_bindings`
   - `llm_force_delete_audit`
3. [ ] File datawatch-app mobile parity issue (via gh issue)
4. [ ] CHANGELOG entry (alpha.37 section — complete the overhaul)
5. [ ] Build, install, smoke, commit, tag

**Rule requirements:**
- 7-surface parity: ✓ (REST✅ + MCP✅ + CLI⬜ + comm✅ + PWA✅ + locale✅ + mobile⬜)
- Smoke: +7 new checks
- CHANGELOG: complete alpha.37 overhaul entry
- Plans audit: include rule audit block

---

## Sprint 3: v7.0 visual/UX items + #183 (alpha.40+)

**Steps:**
1. [ ] #183 #3: Alerts narrower 10% + right-justified (CSS)
2. [ ] #183 #4: Sessions search/filters state badges in dropdown
3. [ ] #183 #5: LLM badges/filters collapse into a button
4. [ ] #183 #6: Filter box 10% wider
5. [ ] #183 #7: New-automaton form spacing
6. [ ] Automata "filter" subheader — visual check and remove if visible
7. [ ] Build, smoke, commit, tag per change

---

## Sprint 4: Howto review + screenshots

**Files to review (27 total):**
```
algorithm-mode.md
autonomous-planning.md
autonomous-review-approve.md
channel-state-engine.md
chat-and-llm-quickstart.md
claude-hooks.md
comm-channels.md
container-workers.md
council-mode.md
cross-agent-memory.md
daemon-operations.md
docs-as-mcp.md
evals.md
federated-observer.md
identity-and-telos.md
mcp-tools.md
pipeline-chaining.md
prd-dag-orchestrator.md
profiles.md
secrets-manager.md
sessions-deep-dive.md
setup-and-install.md
skills-sync.md
tailscale-mesh.md
v7-compute-migration.md
voice-input.md
```

**For each howto:**
1. Read current content
2. Identify outdated sections (API paths, UI screenshots, version refs, feature names)
3. Update content
4. Take screenshots via browser (navigate → screenshot → save to docs/howto/screenshots/ → push to GitHub raw for embedding)
5. Embed screenshot URLs

**Known updates needed (pre-audit):**
- `setup-and-install.md` — v7.0 LLM + Compute Nodes setup changed
- `chat-and-llm-quickstart.md` — LLM enabled models overhaul changes UI
- `v7-compute-migration.md` — should show current compute node + LLM UI
- `council-mode.md` — Council personas wizard added (v6.22.3)
- `autonomous-planning.md` / `prd-dag-orchestrator.md` — Automata redesign
- `sessions-deep-dive.md` — sessions UI changed significantly
- `federated-observer.md` — observer + compute node integration

**Missing howtos (likely):**
- `compute-nodes.md` — new v7.0 compute node management
- `llm-registry.md` — new v7.0 LLM registry + enabled models
- `observer-and-monitoring.md` — observer peer + live detail
- `alerts-and-notifications.md` — alert dock + notification channels

**Screenshot capture plan:**
For each howto that needs a screenshot:
1. Navigate to relevant view in browser
2. Use mcp__claude-in-chrome__computer screenshot
3. Save to `/mnt/DMZ/dmz0/dmz/Private/src/workspace/datawatch/docs/howto/screenshots/<name>.png`
4. Push to GitHub and get raw URL
5. Embed `![description](https://raw.githubusercontent.com/...)` in howto

---

## Sprint 5: Full docs audit

**Scope:**
- `docs/` directory audit: all .md files
- Remove stale internal refs (BL###, F##, B##, version narrative) from user-facing docs
- Cross-reference all howtos against actual REST/MCP/CLI surface
- Verify all locale keys referenced in howtos exist
- Verify all screenshot URLs are accessible
- `README.md` audit
- `CHANGELOG.md` completeness check

---

## Rule audit (session-level)

Per `feedback_docs_plans_audit`: every CHANGELOG entry + per-sprint plan doc includes a compact rule audit block.

| Rule | Sprint coverage |
|---|---|
| 7-surface parity | Per sprint below |
| Per-release smoke | Every sprint runs release-smoke.sh before tag |
| Locale × 5 | No new strings except Sprint 2 (CLI help text not i18n) |
| Mobile parity issue | Sprint 2 (LLM overhaul) |
| Plans folder hygiene | This plan dated; tidy-plans archives >1 week old plans |
| Smoke cleanup tracking | All smoke tests use smoke-* + add_cleanup |
| Backlog-is-spec | Working from plan docs as spec |
| User-facing docs no internals | Sprint 4+5 verify this |
| Automata not PRD | All copy uses "Automata" / "Automaton" |
| Cookbook | End of each sprint |
| Deviations | None so far |
