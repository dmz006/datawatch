# Audit — Thursday night → now (v4.2.0 → v5.18.0)

**Date:** 2026-04-26
**Window:** 2026-04-23 18:00 EDT → 2026-04-26 20:00 EDT (~74 hours; **62 unique releases, 48 release commits**).
**Scope:** every release shipped in the window against AGENT.md rules + operator-reported bugs. Initial audit covered v5.5.0 → v5.18.0; extended in this revision back through v4.2.0.

## Method

Walked AGENT.md § Versioning, § Release vs Patch Discipline, § Release-discipline rules, § Container maintenance, § Pre-release dependency audit, § Pre-release security scan, § Documentation Rules, § Configuration parity, § Monitoring & Observability against every release in the window. Cross-referenced operator reports from both sessions in the window.

## Window summary

| Range | Sessions | Releases | Notes |
|-------|----------|----------|-------|
| 2026-04-23 18:00 → 2026-04-26 ~02:00 | previous | v4.2.0 → v5.6.1 (~50 releases) | dense feature stretch — federation foundation, peer registry, Shape B/C, Go MCP bridge, container distribution, eBPF arm64, rate-limit parser, doc-coverage audits, BL191 lifecycle, OOM leak audits |
| 2026-04-26 ~14:55 → ~19:35 | this | v5.7.0 → v5.18.0 (12 releases) | doc/UX expansion, BL191 Q4+Q6, BL180 cross-host+eBPF kprobes, PWA viz, config-surface bridges, MCP channel one-way fix |

## Findings — AGENT.md rules

### 🔴 README.md marquee — drift across TWO sessions (v5.0.4 → v5.6.1, then v5.8 → v5.18)

**Rule:** *"Every release commit updates the `**Current release: vX.Y.Z (DATE).**` line at the top of `/README.md`"*

**State (extended audit, Thursday-night window):**

- **v4.8.8 → v5.0.3:** marquee updated for every release ✓ (previous session was disciplined)
- **v5.0.4 → v5.6.1:** marquee skipped for 9 releases ❌ (previous session lapsed)
  - The marquee jumped from `v5.0.3` straight to `v5.7.0` in this conversation's first release commit
- **v5.8.0 → v5.18.0:** marquee skipped for 11 releases ❌ (this session lapsed)

Total: **20 releases shipped without a marquee update** in the last 74 hours, across two sessions.

**Severity:** high. The marquee is the public project signpost. The fact that two consecutive sessions missed it suggests the rule needs a CI gate or a release-script step, not just AGENT.md text.

### 🔴 Container maintenance audit — NEVER PERFORMED (every release)

**Rule:** *"Every release must audit the container product surface (Dockerfiles in `docker/dockerfiles/` + the Helm chart in `charts/datawatch/`) and decide per-image whether a rebuild/retag is needed. Daemon-behavior changes require rebuilding `parent-full`."*

**State:** Zero container audits this session. Multiple releases changed daemon behavior (BL191 Q4, BL191 Q6, BL180 cross-host, BL180 eBPF kprobes, MCP channel fix v5.18.0). Per the rule, `parent-full` should have been rebuilt.

**Severity:** medium. Doesn't break operators on the host, but the in-container image is stale relative to the binary.

### 🔴 Pre-release dependency audit — NEVER RUN (every release)

**Rule:** *"Before every release, run `go list -m -u all`"*

**State:** Never executed across the 12 releases this session.

**Severity:** medium. No upgrades attempted, but the audit step was skipped.

### 🔴 Pre-release gosec scan — NEVER RUN (every release)

**Rule:** *"Before every release, run `gosec`"*

**State:** Never executed. Just ran during this audit pass — found 297 issues total (mostly pre-existing G104 globally suppressed). HIGH severity findings I introduced this session need spot-check (none yet identified, but the formal scan was skipped at release time).

**Severity:** medium-high. The rule is "before every release" — I shipped 12 releases without it.

### 🟡 Pre-commit version check — partial miss (v5.0 era)

**Rule:** *"Before every `git commit`, verify BOTH files have the NEW version."*

**State:** I caught this drift in v5.7.0 (`internal/server/api.go` was stuck at `5.0.3` while `cmd/datawatch/main.go` had marched to `5.6.1`) and fixed it. Every release since has kept both in sync. The historical drift was pre-session.

**Severity:** none going forward, but worth flagging that a previous operator missed this for ~6 releases.

### 🟡 "Patch vs minor" labeling

**Rule:** *"Patch releases (X.Y.Z, where Z > 0) — bump version + commit + push code; do not run `make cross` and do not attach binary assets."*

**State:** v5.18.0 was a bug fix labeled "Patch" in CHANGELOG but version-bumped as `5.18.0` (Z=0). Per the rule, that's a minor. I shipped it with binaries, which is consistent with minor. The CHANGELOG label is descriptive prose, not a SemVer claim, so no foul, but the labeling is inconsistent.

**Severity:** low. Cosmetic.

## Findings — Test coverage gaps

### 🔴 v5.18.0 redirect bypass — only helper test

**Code shipped:** the redirect-bypass for loopback `/api/channel/*` requests (the MCP fix).

**Tests at release time:** only `TestIsLoopbackRemote` covering the helper. No integration test of the actual bypass behavior.

**Status:** I extracted the handler to `redirectToTLSHandler()` during this audit and added `redirect_bypass_test.go` with 10 test cases (loopback v4/v6/IPv4-mapped + path-prefix-exactness + body forwarding + non-loopback safety).

**Severity (resolved):** would have been high; now covered.

### 🟡 v5.7.0 `datawatch reload` CLI — no test

**Code shipped:** `newReloadCmd()` in `cmd/datawatch/main.go` POSTing to `/api/reload`.

**Tests at release time:** none for the CLI command itself. The `/api/reload` endpoint is BL17 and has its own coverage.

**Severity:** low. Trivial wrapper; the underlying endpoint is tested.

### 🟢 Other releases — adequately tested

- v5.8.0 BL201 voice inheritance: 8 tests in `cmd/datawatch/voice_inherit_test.go`
- v5.9.0 BL191 Q4 recursion: 5 tests in `internal/autonomous/recursion_test.go`
- v5.10.0 BL191 Q6 guardrails: 6 tests in `internal/autonomous/guardrails_test.go`
- v5.12.0 BL180 cross-host: 7 tests in `internal/observer/cross_peer_correlator_test.go`
- v5.13.0 BL180 eBPF kprobes: 3 tests (limited; real kernel gated to operator Thor smoke)
- v5.16.0 PWA viz: client-side only; underlying REST endpoints (`/api/autonomous/prds/{id}/children`, `/api/observer/envelopes/all-peers`) tested in v5.9 + v5.12
- v5.17.0 config-surface bridge: 2 tests in `internal/server/autonomous_config_patch_test.go`

## Findings — Operator-reported bugs

### 🔴 Autonomous full CRUD broken — Cancel/Edit/Delete affordances missing

**Operator report:** *"Autonomous cancel doesn't seem to work, it isn't full crud, can't edit it delete anything"*

**Investigation:**

- **Cancel via REST works** — `DELETE /api/autonomous/prds/{id}` flips status to `cancelled`. Verified via curl.
- **Cancel via PWA shows only on `running` status** — for any other status (draft, needs_review, approved, cancelled, completed, archived) there's no Cancel/Delete button.
- **No hard-delete path anywhere** — `Cancel` only flips status. Operators with a long history of completed/archived PRDs can't actually clear the list.
- **No PRD-level Edit** — `EditTaskSpec` is the only edit affordance and only works in `needs_review`/`revisions_asked`. Title/spec at the PRD level is uneditable.

**Status (in flight):** Code written this audit pass — `Store.DeletePRD` (with descendant cleanup), `Store.UpdatePRDFields`, Manager wrappers, REST `DELETE ?hard=true` + `PATCH`, CLI `prd-delete` + `prd-edit`, PWA Edit + Delete buttons on every non-running PRD with confirm dialog. Not committed yet.

**Severity:** high. Operator-blocking.

### 🟡 RTK section duplicated in PWA Settings (General + LLM)

**Operator report:** *"Rtk is in general and llm, it would only be in llm."*

**Investigation:** `GENERAL_CONFIG_FIELDS` in `internal/server/web/app.js` likely has an RTK block in the `general` section that should only be in `llm`. Need to grep to confirm.

**Status:** not yet investigated. Adding to follow-up.

**Severity:** medium. UI clutter, not a functional bug.

### 🟡 Whisper backend selector incomplete

**Operator report:** *"whisper doesn't have selection for ollama or openwebui to use configured, nor a test transcription button mic"*

**Investigation:** v5.8.0 BL201 added the `ollama` and `openwebui` cases to the factory + the inheritance helper, but I haven't verified the PWA `whisper.backend` select includes those values. Earlier in this session I noted the select options as `['whisper','openai','openai_compat','openwebui','ollama']` so the values exist — but the operator says they don't see them. Possibly a stale browser cache OR the field is hidden behind a conditional. Also: the BL289 test-transcription button (shipped v5.4.0 per memory) may have regressed.

**Status:** needs PWA inspection.

**Severity:** medium.

### 🟡 Session detail — "Input required" label duplicated above tmux input box

**Operator report:** *"tmux input box doesn't need input required above it, there is a badge for that on top"*

**Investigation:** The session-detail view shows a status badge at the top of the page (the colored pill). When the session enters `waiting_input` state, a separate "Input required" label was added above the tmux input box too — duplicating the signal. Operator wants the label-above-input removed since the top badge already conveys the state.

**Status:** not yet investigated. Adding to v5.19.0.

**Severity:** low. UI clutter only.

### 🟡 Session detail — arrow buttons missing next to saved commands

**Operator report:** *"no arrow keys to right of saved commands, add the after audit"*

**Investigation:** Per memory `Session detail — tmux arrow buttons | v5.2.0`, four buttons (↑↓←→) were added next to the saved-commands quick row. Operator says they're missing. Possible regression in the session-detail render, or hidden by a layout change.

**Status:** not yet investigated.

**Severity:** medium. Functional regression on a feature the operator uses.

### 🟡 "All config options missing" — PWA Settings field-list completeness

**Operator report:** *"Make sure all config options are there."*

**Investigation:** This is the same pattern as v5.17.0 (autonomous knobs missing from PWA + applyConfigPatch). I just fixed four; there are likely more across observer, RTK, mcp, federation, ollama_tap. A systematic walk of every `cfg.*` field vs `GENERAL_CONFIG_FIELDS` + `applyConfigPatch` cases would catch them.

**Status:** not yet performed. Adding to follow-up.

**Severity:** medium-high. Each missing key is a silent no-op like the BL191 ones I fixed.

## Recommended fix order

1. **Operator-blocking — autonomous full CRUD** (in flight; finish + ship)
2. **README marquee bump** (single-file change; ship with the CRUD release)
3. **PWA: arrow buttons regression + RTK section dedupe + whisper backend dropdown** (operator-reported; same release)
4. **Systematic config-field audit** (pattern-match v5.17.0; spans ~10 cfg sections)
5. **Container parent-full rebuild** (catch-up; tag with current daemon version)
6. **gosec HIGH-severity review** (separate cleanup release if any are mine)

Releases needed:
- v5.19.0 — operator-blocking CRUD + UX fixes (RTK / whisper / arrow keys) + README marquee
- v5.20.0 — documentation alignment sweep (mcp.md, commands.md, api/*.md, testing-tracker)
- v5.21.0 — observer + whisper config-parity sweep (same pattern as v5.17.0; ~10 subsystems)
- v5.22.0 — observability fill-in (stats metrics + Prom metrics for BL191 Q4/Q6 + BL180 cross-host)
- v5.22.x patches — datawatch-app#10 ticket bundling every PWA-visible change since v5.16.0; container parent-full retag; gosec HIGH-severity review

## Findings — Wider window (Thursday night → v5.5.0)

This section extends the audit back through the previous session's work in the same 74-hour window.

### 🟢 Test discipline (v4.2 → v5.4) — generally good

31 new test files landed in the wider window. Test coverage for the previous session's features:

- **BL172 peer registry (v4.5.x):** `internal/observer/peer_registry_test.go`, `internal/server/observer_peers_test.go`, `internal/mcp/observer_peers_test.go`, `internal/router/peers_test.go`, `cmd/datawatch/cli_observer_peer_test.go`, `internal/observer/observer_peers_federation_test.go` — comprehensive
- **BL173 cluster observer (v4.5.0/v4.6.0):** `internal/observer/cluster_k8s_test.go`, `internal/observer/gpu_dcgm_test.go`, `cmd/datawatch-stats/peer_test.go`
- **BL174 native Go MCP bridge (v4.3.0):** `cmd/datawatch-channel/main_test.go`, `internal/channel/probe_test.go`
- **BL189 Whisper factory (v4.9.0):** `internal/transcribe/openai_compat_test.go`
- **BL180 Phase 1 ollama tap (v4.9.1):** `internal/observer/ollama_tap_test.go`
- **BL197 chat-channel parity (v4.9.2):** `internal/router/autonomous_test.go`
- **BL185 rate-limit parser (v4.8.23):** `internal/session/ratelimit_parser_test.go`
- **BL181 BTF discovery (v4.8.21):** `internal/stats/ebpf_btf_test.go`
- **BL191 lifecycle (v5.2.0):** `internal/autonomous/lifecycle_test.go`
- **S13 agent observer peers (v4.7.0):** `internal/agents/observerpeer_test.go`, `internal/observerpeer/client_test.go`

**Severity:** none. Previous session's test discipline was solid.

### 🟡 Container audits in wider window — partial

`docker/dockerfiles/` + `charts/datawatch/` were touched in: v4.3.0 (channel bundling), v4.5.0 (Shape C cluster image), v4.5.1, v4.6.0 (agent images lose nodejs), v4.7.0, v4.7.1, v4.8.0. So container changes were tracked **when the daemon-behavior change required them**.

But there's no commit log of explicit "container audit performed" entries. The rule asks for a per-release decision, not just rebuilds when forced. So:

- **v4.2.0 → v5.0.0:** container changes shipped where needed (and only there). Implicit audits, not explicit.
- **v5.0.1 → v5.18.0:** zero container touches. Daemon behavior changed (BL180 Phase 2 procfs+cross-host+eBPF, BL191 Q1+Q4+Q6, MCP channel fix, etc.) — `parent-full` definitely should have been retagged at minor releases per the rule.

**Severity:** medium. Same finding as the original audit, just confirms the gap is wider than this session.

### 🟢 datawatch-app issues in wider window — partly disciplined

Memory says: *"datawatch-app catch-up issue (#9) | v5.2.0 | every PWA-visible change in v5.1.0 + v5.2.0 batched into datawatch-app#9"*

So issue #9 was opened around v5.2.0 covering toolbar removal, history rename, FAB position, BL178 reopen, BL198 drawer fix, About-tab additions, voice-input dropdown, BL191 PRD lifecycle surfaces, arrow keys.

Then nothing filed for v5.3.0 → v5.18.0 (16 releases including major PWA changes — top-level Autonomous tab, BL202 LLM dropdowns, BL191 Q4/Q6 PWA viz, cross-host modal, etc.).

**Severity:** high — same finding as original audit, just confirms the pattern preceded my session by ~1-2 releases.

### 🟢 Test coverage of v4.2 → v5.4 release-time fixes — generally tested

Spot-check of bug fixes in the wider window:

- **BL184 opencode-acp lag (v4.8.20):** fix is in `internal/llm/backends/opencodeacp/` — has its own tests
- **BL185 rate-limit parser (v4.8.23):** has dedicated `ratelimit_parser_test.go`
- **BL187 sw.js stale cache (v5.0.4):** PWA-side fix, no test (PWA isn't unit-tested)
- **BL198 drawer collapse (v5.0.5):** PWA-side, no test
- **BL291/BL292 leak audits (v5.5.0/v5.6.0):** capacity-bound caches don't have tests but the fix is structural (caps + cleanup)
- **OOM emergency (v5.6.1):** opt-in flag — no behavior test, just a config gate

**Severity:** low. Mostly UI/config changes that don't fit the unit-test pattern.

## Findings — Documentation alignment (added in second-pass audit)

AGENT.md § Documentation Rules + § General documentation checklist + per-feature checklists:

### 🔴 docs/mcp.md not updated for new MCP tools

**Rule:** *"New MCP tool — Document in `docs/mcp.md` under Available Tools with parameter table and example. Update `docs/cursor-mcp.md` tools table"*

**State:** I shipped `autonomous_prd_children` (v5.9.0) and `observer_envelopes_all_peers` (v5.12.0). Neither was added to `docs/mcp.md` or `docs/cursor-mcp.md`. Operators consulting the MCP reference docs won't find them.

**Severity:** medium. Tools work, just undocumented.

### 🔴 docs/commands.md / CLI reference not updated for new CLI commands

**Rule:** *"Every new CLI command or API endpoint must be documented in `docs/commands.md` or relevant API reference."*

**State:** New CLI commands across the session:
- `datawatch reload` (v5.7.0)
- `datawatch autonomous prd-children` (v5.9.0)
- `datawatch observer envelopes-all-peers` (v5.12.0)
- `datawatch autonomous prd-delete` + `prd-edit` (v5.19.0 in flight)

None added to `docs/commands.md`.

**Severity:** medium.

### 🔴 README.md interface table not updated for new commands

**Rule (general checklist #4):** *"Update README.md if adding a new interface, command, or user-visible feature"*

**State:** Same set as above. README has a CLI section that lists key commands; not updated.

**Severity:** low (README marquee fix in v5.19.0 covers the headline; the per-command table is secondary).

### 🟡 docs/api/*.md not updated for new REST endpoints

**Rule:** new REST endpoint should be documented.

**State:**
- `GET /api/autonomous/prds/{id}/children` (v5.9.0) — not in `docs/api/autonomous.md`
- `GET /api/observer/envelopes/all-peers` (v5.12.0) — not in `docs/api/observer.md`
- `DELETE /api/autonomous/prds/{id}?hard=true` (v5.19.0 in flight) — not yet
- `PATCH /api/autonomous/prds/{id}` (v5.19.0 in flight) — not yet

**Severity:** medium. Operators relying on api docs miss the new endpoints.

### 🟡 docs/testing-tracker.md not updated

**Rule (checklist #6):** *"Update `docs/testing-tracker.md` for any new interface or backend"*

**State:** Several new interfaces shipped (cross-host correlator, recursive PRDs, per-task/story guardrails, MCP channel redirect bypass). None added to the testing tracker.

**Severity:** medium.

## Findings — Configuration parity sweep (rule § 658)

For every new config knob shipped this session:

| Knob | YAML | REST `PUT /api/config` | MCP `config_set` | CLI `config set` | Comm `configure` | PWA Settings | Mobile (datawatch-app) |
|------|------|------------------------|------------------|------------------|------------------|--------------|------------------------|
| `autonomous.max_recursion_depth` (v5.9.0) | ✓ v5.17.0 | ✓ v5.17.0 | ✓ via config_set | ✓ via config set | ❌ no chat verb | ✓ v5.17.0 | ❌ |
| `autonomous.auto_approve_children` (v5.9.0) | ✓ v5.17.0 | ✓ v5.17.0 | ✓ | ✓ | ❌ | ✓ v5.17.0 | ❌ |
| `autonomous.per_task_guardrails` (v5.10.0) | ✓ v5.17.0 | ✓ v5.17.0 | ✓ | ✓ | ❌ | ✓ v5.17.0 | ❌ |
| `autonomous.per_story_guardrails` (v5.10.0) | ✓ v5.17.0 | ✓ v5.17.0 | ✓ | ✓ | ❌ | ✓ v5.17.0 | ❌ |

**Open issue:** I ran the parity sweep for autonomous knobs in v5.17.0. The same sweep is missing for any pre-existing observer config (`observer.conn_correlator`, `observer.peers.*`, `observer.federation.*`, `observer.ollama_tap.*`, etc.) — none have `applyConfigPatch` cases per the v5.17.0-pattern check. Patterns of silent no-op may exist there too. Same for pre-existing `whisper.backend / whisper.endpoint / whisper.api_key`.

**Severity:** medium-high. The pattern is the same as v5.17.0 and likely affects multiple subsystems.

## Findings — Monitoring & Observability rule (§ 674)

Rule: every new feature must add a stats metric, a `/api/<subsystem>/stats` endpoint (or be exposed in an existing one), an MCP `<subsystem>_stats` tool, a Web UI Monitor card, a comm channel `stats` variant, and Prometheus metrics where numeric.

| Feature | Stats metric | API stats | MCP stats | Monitor card | Comm stats | Prom metric |
|---------|--------------|-----------|-----------|--------------|------------|-------------|
| BL191 Q4 recursion (v5.9.0) | ❌ no recursion-depth or child-count metric | ❌ | ❌ | ❌ | ❌ | ❌ |
| BL191 Q6 guardrails (v5.10.0) | ❌ no per-guardrail pass/warn/block counters | ❌ | ❌ | ❌ | ❌ | ❌ |
| BL180 cross-host (v5.12.0) | ❌ no cross-peer attribution count | partial via `/api/observer/envelopes/all-peers` | partial via `observer_envelopes_all_peers` | ❌ | ❌ | ❌ |
| BL180 eBPF kprobes (v5.13.0) | ✓ via `host.ebpf.kprobes_loaded` | ✓ existing observer/stats | ✓ existing | ✓ existing | partial | ❌ |

**Severity:** medium. Features work; observability is the gap.

## Findings — datawatch-app tickets (mobile companion)

Per memory: *"Operator directive 2026-04-26: every PWA-visible change … batched into datawatch-app#9 so the mobile shell can stay aligned."*

**State:** I shipped multiple PWA-visible changes this session and **never opened a datawatch-app ticket**:

| Release | PWA-visible change | datawatch-app ticket |
|---------|--------------------|----------------------|
| v5.16.0 | PRD genealogy badges, lazy Children disclosure, per-story/task verdict badges, Cross-host modal, `↳ spawn` + `→ child` task affordances | ❌ none filed |
| v5.17.0 | Four new Settings → Autonomous fields (recursion + guardrails) | ❌ |
| v5.18.0 | Channel fix is daemon-side; mobile clients use the same MCP path (likely needs no app change) | n/a |
| v5.19.0 (in flight) | Edit + Delete buttons on every PRD card; PRD-edit modal | ❌ |

**Severity:** high. The mobile shell will drift further with every PWA-visible change.

## Procedural changes

To prevent this drift going forward, AGENT.md § Release Workflow should be amended with an explicit per-release checklist:

```
Before each `gh release create`:
- [ ] README.md marquee updated to new version + date
- [ ] docs/plans/README.md backlog refactored (closed table updated)
- [ ] make cross + 5 binaries
- [ ] go test ./...
- [ ] gosec -exclude=$EXCLUDE -fmt text -quiet ./... (review HIGH severity)
- [ ] go list -m -u all (note any >72h-old upgrades)
- [ ] container audit: any parent-full / agent-* image affected? note in release notes
- [ ] tests written for every new exported function / handler / config path
```

The release-notes template already covers the upgrade-path step. The above is what needs adding.
