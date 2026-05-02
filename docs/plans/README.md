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

## Current state — 2026-05-02

Latest release: **v5.28.4** (2026-05-01, BL217 PUT /api/config parity fix).

| Bucket | Count | Notes |
|---|---|---|
| Open bugs | 5 | See Open Bugs section — filed 2026-05-02. Settings/LLM overlap; RTK upgrade UI; broken diagrams (×2); failures alert stream; PWA resize after session close. |
| Open features | 0 | All ranked features closed: BL214 v5.28.0; #26/#27/#28/#29/#30/#31 closed v5.27.7–v5.27.9. |
| Active backlog | 5 | BL218 channel session hygiene · BL219 LLM tooling lifecycle · BL220 Config Accessibility Rule audit · BL221 PRD rebuild design · BL190 cosmetic (iterative). |
| Awaiting operator action | 0 | |
| Recently closed (sticky) | see table below | v5.27.1 ↘ v5.26.0. |
| Frozen / external | 5 items | F7 libsignal · BL174 distroless spike · S14b/c · datawatch-app mobile parity. |

Per operator policy: only debugging and bug-fix work in this branch until the v6.0 release date. New planning items (BL218–BL221) are design/doc work queued for v6.0 implementation window.

## Unclassified

_(empty — drop new operator-filed items here; the backlog refactor each release pulls them into BL### entries below.)_

_Historical Unclassified items shipped + tracked elsewhere:_ Directory-selector "create folder" (v4.0.1), Aperant integration review (skipped — see [`docs/plan-attribution.md`](../plan-attribution.md) "Researched and skipped"), datawatch-observer / BL171–BL173 (✅ all three shapes shipped — see Recently closed).

_2026-05-02 operator-filed items promoted directly to BL218–BL221 (see Active backlog below)._

---

## Open Bugs

| ID | Summary | Filed |
|----|---------|-------|
| **BL222** | Settings → General contains Claude-specific fields that duplicate LLM → claude-code config | 2026-05-02 |
| **BL223** | RTK upgrade card renders raw JS/HTML — `onclick` and `title` setter code visible as text | 2026-05-02 |
| **BL224** | `orchestrator-flow.md` diagram does not render in `/diagrams.html` | 2026-05-02 |
| **BL225** | `prd-phase3-phase4-flow.md` diagram does not render in `/diagrams.html` | 2026-05-02 |
| **BL226** | Service-level failures (eBPF, memory, plugins, jobs) have no alert stream entry or dedicated UI tab | 2026-05-02 |
| **BL227** | PWA layout does not refit after dismissing the session-stopped popup | 2026-05-02 |

---

**BL222 — Settings → General / LLM claude-code config overlap**

PWA Settings → General reportedly contains 4 Claude-specific settings (likely: auto-accept disclaimer, permission mode, default effort, model override) that also appear in Settings → LLM → claude-code. Two edit surfaces for the same config keys creates confusion about which setting wins and may produce silent conflicts if the operator sets them to different values.

**Fix:** Audit `loadGeneralConfig()` and the `LLM_CONFIG_FIELDS` / general settings arrays in `internal/server/web/app.js`. Fields that are claude-code-specific (i.e. only meaningful when backend is claude-code) should live exclusively in LLM → claude-code. Fields that are genuinely session-level defaults (backend selection, console size, auto-git) stay in General. Confirm `applyConfigPatch` round-trips correctly for any moved fields and the PWA reflects the canonical location only.

**Acceptance criteria:** No config key appears as an editable field in both General and LLM → claude-code tabs. Operator can set any claude-specific option from a single canonical location. `go test ./internal/server/...` still green.

**Files:** `internal/server/web/app.js` (settings renderers)

---

**BL223 — RTK upgrade card renders raw JS as visible text**

The RTK upgrade section in the PWA shows the `onclick` handler source and `title` setter code as visible text rather than as an interactive button. The raw strings `this.title='Copied! Paste into a shell.'` and `this.textContent='copied!'` are appearing in the rendered card. Root cause: the upgrade snippet HTML is being inserted via `innerHTML` in a context where event-handler attributes are not being executed, or the template string is being double-escaped before insertion.

**Fix:** Locate the RTK upgrade card render in `app.js` (search for `rtk_check`, `install.sh`, or the RTK version display). Replace the inline `onclick=` attribute pattern with `createElement` + `addEventListener`, or fix the escaping so the button is emitted as live DOM rather than raw HTML text. Verify in the running PWA that clicking the upgrade button copies the install command to the clipboard.

**Acceptance criteria:** RTK card shows a clean upgrade button with version label. Clicking copies the install one-liner. No raw JS visible as text. Verify against running local instance (Settings → LLM → RTK section).

**Files:** `internal/server/web/app.js` (RTK settings card renderer)

---

**BL224 — `orchestrator-flow.md` diagram does not render**

The orchestrator flow diagram fails to render in `/diagrams.html`. Likely causes: Mermaid syntax error introduced during a recent edit, use of a Mermaid feature not supported by the pinned CDN version, or a file path mismatch in the diagrams sidebar index.

**Fix:** Locate the file (likely `docs/flow/orchestrator-flow.md` or `docs/plans/orchestrator-flow.md`). Validate the Mermaid block at [mermaid.live](https://mermaid.live). Fix the syntax. Confirm it renders in `/diagrams.html` after daemon restart.

**Acceptance criteria:** Opening `/diagrams.html` → orchestrator flow shows a rendered diagram with no error overlay.

**Files:** The relevant `orchestrator-flow.md` file in `docs/`

---

**BL225 — `prd-phase3-phase4-flow.md` diagram does not render**

Same category as BL224. The PRD phase 3–4 flow diagram fails to render in `/diagrams.html`.

**Fix:** Locate the file, validate Mermaid syntax, fix. Confirm in `/diagrams.html`.

**Acceptance criteria:** PRD phase 3–4 flow renders cleanly in `/diagrams.html`.

**Files:** The relevant `prd-phase3-phase4-flow.md` file in `docs/`

---

**BL226 — Service-level failures need alert stream + System tab**

eBPF probe load failures, memory backend errors, plugin invocation errors, and pipeline/job failures currently produce daemon log lines only. There is no path from these failures to the operator-visible Alerts tab, and the Alerts tab currently shows session-level alerts only (no service-level grouping). Operators monitoring the service must tail daemon logs to spot persistent infrastructure failures.

**Two-part fix:**

1. **Alert emission** — from each failure site, emit a service alert via `alerts.Store` using a `source: "system"` (or `category: "service"`) tag. Failure sites: eBPF loader attach failures (`internal/observer/`), memory backend operation errors (`internal/memory/`), plugin fanout errors (`internal/plugins/plugins.go`), pipeline step failures (`internal/pipeline/`), agent spawn/bootstrap failures (`internal/agents/`). Use a non-fatal severity (warn or error) that does not interrupt session flow.

2. **PWA System tab** — add a "System" (or "Datawatch") tab to the Alerts section that filters for `source: "system"` alerts. Separate unread badge counter from the session-alert counter. Tab shows timestamp, component, severity, and message. Include a "Clear all system alerts" action. Light-red highlight for error-severity rows (operator's original request).

**Acceptance criteria:** Deliberately breaking the eBPF config produces a system alert visible in the System tab without any session being affected. All alert emission paths have unit tests. PWA System tab renders and filters correctly. Configuration Accessibility Rule: system alert suppress/acknowledge reachable via REST + MCP + CLI.

**Files:** `internal/alerts/`, `internal/observer/`, `internal/memory/`, `internal/plugins/plugins.go`, `internal/pipeline/`, `internal/agents/`, `internal/server/web/app.js`

---

**BL227 — PWA layout does not refit after session-stopped popup close**

After a session completes and the "session stopped" notification overlay is dismissed, the xterm terminal panel retains its pre-popup dimensions — the layout does not reflow. Operator must navigate away and back to restore correct terminal size. Root cause: the popup dismiss path does not trigger `fitAddon.fit()` + a synthetic `resize` event, unlike the explicit Dismiss path fixed in BL211 / v5.27.1.

**Fix:** In the session-stopped popup close handler in `app.js`, after removing/hiding the popup element, add:

```js
if (fitAddon) {
  requestAnimationFrame(() => { fitAddon.fit(); window.dispatchEvent(new Event('resize')); });
}
```

Guard with a check that the terminal is still visible. Verify on the running PWA by starting a session, letting it complete, dismissing the popup, and confirming the terminal fills the panel without navigating away.

**Acceptance criteria:** After session-stopped popup dismissal, xterm fills its container with no manual navigation required. Regression check: existing Dismiss-path behaviour (BL211 fix) still works.

**Files:** `internal/server/web/app.js` (session-stopped popup dismiss handler)

---

> Historical: B22 fixed in v2.4.3 · B23/24 in v2.4.4 · B25 in v2.4.5 · B31 in v3.0.1 · B30 in v3.1.0 — see Completed section.

## Open Features

_(empty — every numbered feature has shipped. Mempalace alignment closed in v5.27.0; PRD-flow phases 1-6 + container F10 + memory federation are locked.)_

## Pending backlog

_(empty — BL173-followup closed v5.28.2. See **Active backlog** for items in flight: BL218–BL221 design/planning work + BL190 cosmetic iterative.)_

## Open backlog (deferred / awaiting operator action)

**Quick map:** items where I can keep working sit in **Active work** below. Items where I'm blocked on an operator decision sit in **Awaiting operator action** with a structured "what's needed + recommendation" per item. Items shipped recently sit in **Recently closed** for one release cycle; long-term / external items sit in **Frozen / External**.

### Active work (no decision needed — keep iterating)

> **2026-05-02 refactor:** BL208, BL209, BL211, BL212, BL213, BL215, BL217 all closed
> in v5.27.6–v5.28.4 (see Recently closed). Remaining open items: BL210 MCP gaps
> (deferred to v6.0) + BL218–BL221 new planning items + BL190 cosmetic iterative.
> Open bugs BL222–BL227 filed today and ready to fix.

---

#### BL218 — Channel session-start hygiene: Go-first enforcement + per-session `.mcp.json` cleanup (filed 2026-05-02)

**Context:** BL216 (v5.27.10) fixed `WriteProjectMCPConfig` to write the Go bridge path when `BridgePath()` is set, and added `CleanupStaleJSRegistrations()` at daemon start. BL212 (v5.27.7/v5.27.9) added memory tools to both the Go bridge binary and `channel.js` JS fallback. But several per-session gaps remain.

**Gap 1 — `channel.js` accuracy check uses size only.**  
`EnsureExtracted` (channel.go) compares `info.Size() != int64(len(channelJS))` as its staleness gate. If an older `channel.js` happens to be the same byte count as the embedded version (realistic on a minor content change), the stale file is never overwritten. Fix: compare SHA-256 hash of the on-disk file against `sha256.Sum256(channelJS)`, not size.

**Gap 2 — No pre-launch `.mcp.json` sweep for Claude sessions.**  
`WriteProjectMCPConfig` is idempotent and rewrites the `datawatch` entry on every spawn, but it only covers the *project* `.mcp.json`. It does not check:
- `~/.mcp.json` (user-scope global) — Claude Code loads this before the project file; a stale user-scope entry overrides the per-project fix.
- Session working directory if it differs from the registered project dir.
- Any Claude-scope `.mcp.json` written by a previous run of a *different* LLM backend that happened to create one.

Fix: on `onPreLaunch` for any Claude-based backend, scan user-scope + working-dir scope `.mcp.json` files and rewrite the `datawatch` entry to the current bridge (Go or JS) the same way `WriteProjectMCPConfig` does. Log a `[channel] rewrote stale .mcp.json at <path>` line for each.

**Gap 3 — JS fallback path not verified before use.**  
When `BridgePath()` returns empty (Go bridge not on hand), the fallback writes `node ~/.datawatch/channel/channel.js`. But if `node` is not on `$PATH` or `node_modules` hasn't been installed (e.g. npm was unavailable at daemon start), the session launches silently with a broken bridge. Fix: `WriteProjectMCPConfig` in JS-fallback mode should call `Probe()` and fail-fast with a descriptive error rather than writing an unusable config.

**Gap 4 — `channel.js` vs Go bridge preference not logged per session.**  
BL216 added a daemon log line `[channel] session <id> registered with <kind> bridge at <path>` on registration, but the pre-launch path (before the session is even started) doesn't log which bridge is being wired. Operators troubleshooting mid-flight can't tell which bridge a not-yet-connected session was configured to use. Fix: log `[channel] pre-launch: wiring <go|js> bridge for session <name> at <path>` from `onPreLaunch`.

**Acceptance criteria:**
- SHA-256-based `EnsureExtracted` staleness check with unit test (hash-same-size-different-content scenario).
- User-scope `~/.mcp.json` swept and updated on every Claude pre-launch; working-dir swept if different from project dir.
- JS fallback path calls `Probe()` and returns an error (surfaced as a pre-launch failure) if node or node_modules is absent.
- Pre-launch log line emitted for every bridge wiring, visible in `datawatch logs`.
- `GET /api/channel/info` `stale_mcp_json` field extended to check user-scope `~/.mcp.json` (currently only checks project scope).

**Related:** BL216 (closed v5.27.10) · BL212 (closed v5.27.7/v5.27.9) · `internal/channel/channel.go` · `internal/channel/mcp_config.go`

---

#### BL219 — LLM tooling lifecycle: per-backend setup/teardown, ignore-file hygiene, cross-backend cleanup (filed 2026-05-02)

**Context:** Each configured LLM backend leaves file-system side effects in the project directory. When a session starts with backend X, artifacts left by previous backend Y may confuse the new backend or clutter the repository. Datawatch knows all configured LLMs (8 backends) and should own the setup/teardown lifecycle for their file artifacts.

**Known per-backend file footprint:**

| Backend | Files created in project dir | Notes |
|---------|-----------------------------|-|
| `claude-code` | `.mcp.json` (project-scope MCP) | Managed by `WriteProjectMCPConfig`; handled by BL218 |
| `opencode` | `.mcp.json` (OpenCode auto-discovers this), `.opencode/` config dir | OpenCode shares the `.mcp.json` convention; writes its own under `.opencode/` |
| `aider` | `.aider.conf.yml`, `.aider.chat.history.md`, `.aider.tags.cache.v*/` | Cache dirs grow unbounded; history file leaks session content |
| `goose` | `.goose/` (session cache + config), `.goose/sessions/*.jsonl` | Session JSONL files; may contain secrets in tool-call outputs |
| `gemini` | `gemini_api_config.json` or env-only (CLI is config-file-light); may write `.gemini/` | Less certain; needs audit against current gemini CLI version |
| `ollama` / `openwebui` | No project-dir artifacts (HTTP backends, no local CLI) | — |
| `shell` | None (operator-defined) | — |

**Required behavior:**

1. **Pre-session setup (on `onPreLaunch`):** For the starting backend, ensure its required tooling is in place (e.g. `WriteProjectMCPConfig` for claude/opencode). Log what was set up.

2. **Cross-backend cleanup (on `onPreLaunch`):** For each *other* configured backend, remove or neutralize its project-dir artifacts that would conflict with the starting backend. Specifically:
   - If starting `claude-code`: remove any `.mcp.json` `datawatch` entry that points at another backend's MCP bridge (not "remove file," just remove the `datawatch` key if it's wrong — BL218 handles the rewrite).
   - If starting `opencode`: similar `.mcp.json` check; leave `.opencode/` alone (it's opencode's own state).
   - If starting `aider`: no MCP setup needed; but if a `.mcp.json` exists with a stale `datawatch` entry, remove the entry (aider doesn't use `.mcp.json` natively).

3. **Post-session teardown (on `onSessionEnd`):** For the backend that just finished, optionally remove ephemeral artifacts (configurable per backend: `session.cleanup_on_end`). Default: keep but ensure they're in `.gitignore` / `.cfignore`.

4. **Ignore file hygiene:** On first session start with a given backend in a project dir, append the backend's known artifact patterns to `.gitignore` (and `.cfignore` / `.dockerignore` if present). Idempotent — don't add duplicates. Patterns per backend:

   ```
   # claude-code / opencode (datawatch-managed)
   .mcp.json

   # aider
   .aider.conf.yml
   .aider.chat.history.md
   .aider.tags.cache.v*/

   # goose
   .goose/

   # opencode
   .opencode/
   ```

   Note: `.mcp.json` is debatable — some operators want to commit it for team sharing. Make this per-backend-per-project configurable via `session.gitignore_artifacts: [aider, goose]` (default: all except `claude-code` and `opencode`).

5. **Knowledge of all configured backends:** The ignore-file writer and the cross-backend cleanup runner should enumerate `cfg.LLMBackends()` (all enabled backends) so they cover any backend the operator has configured, not just a hardcoded list.

**New config fields:**
```yaml
session:
  cleanup_artifacts_on_end: false     # remove ephemeral backend files after session ends
  gitignore_artifacts: [aider, goose] # append to .gitignore on first use (default: all non-MCP backends)
  gitignore_check_on_start: true      # verify + update .gitignore on every session start
```

**New internal package:** `internal/tooling/` (or extend `internal/channel/`) — `BackendArtifacts` registry mapping backend name → known file patterns; `EnsureIgnored(projectDir, backend)`, `CleanupArtifacts(projectDir, backend)`.

**Acceptance criteria:**
- Pre-launch: for each configured backend, `EnsureIgnored` appends its patterns to `.gitignore` (and `.cfignore`/`.dockerignore` if present) idempotently; tested with a temp dir.
- Pre-launch: cross-backend `.mcp.json` `datawatch` entry is cleaned up when switching from one MCP-writing backend to another.
- Post-session: `CleanupArtifacts` removes aider/goose ephemeral files when `cleanup_artifacts_on_end: true`; tested.
- All new config fields reachable via YAML + REST + MCP + CLI + comm + PWA (Configuration Accessibility Rule).
- Unit tests: artifact registry shape, `EnsureIgnored` idempotence, cross-backend cleanup.

**Related:** BL218 above · BL288 (v5.4.0 stale-JS cleanup) · `internal/channel/mcp_config.go` · `internal/session/manager.go` `onPreLaunch` hook

---

#### BL220 — Configuration Accessibility Rule full alignment audit (filed 2026-05-02, expands BL210)

**Context:** BL210 (partially closed v5.27.8, remainder deferred) audited only the **MCP surface** — "does every REST endpoint have an MCP tool equivalent?" The operator directive extends this to the full **Configuration Accessibility Rule**: every feature must be reachable from all 6 surfaces:

```
YAML config → REST API → MCP tool → CLI subcommand → Comm channel command → PWA / Web UI
```

BL210's MCP gap closure (~85% → 100%) is a prerequisite but not sufficient. Gaps exist on other surfaces too.

**Scope of this audit:**

1. **YAML ↔ REST parity** — every `config.Config` field that `applyConfigPatch` handles should also be writable via `PUT /api/config`. Known gaps: some nested structs added in v5.x sprints may have been missed in `applyConfigPatch` switch statements (the pattern that caused BL217).

2. **REST ↔ CLI parity** — `datawatch config get/set` mirrors `PUT /api/config`. Verify `datawatch` CLI subcommands exist for every non-trivial REST endpoint family (sessions, memory, autonomous, observer, profiles, agents, orchestrator, plugins, skills [future], identity [future]).

3. **REST ↔ Comm channel parity** — the router's command parser (`internal/router/commands.go`) should cover every operator-useful action available via REST. Current gaps likely: observer peer management, orchestrator graph control, agent spawn from chat.

4. **REST ↔ PWA parity** — Settings tabs should surface every configurable field. Known gaps: some `internal/config/` fields added in v5.x never got PWA Settings form entries (spotted: some observer sub-fields, some autonomous tuning knobs).

5. **MCP remaining gaps** (BL210 deferred):
   - `filter_list` / `filter_upsert` / `filter_delete` — detection filter management from IDE
   - `backends_list` / `backends_active` — reachability + version info for all backends
   - `federation_sessions` — proxy-mode aggregated session list
   - `device_register` — mobile push token registry write
   - `files_list` / `files_browse` — directory browser
   - `session_aggregated` / `session_set_state` / `session_set_prompt` — three sub-endpoints lacking MCP

**Deliverable:** A matrix doc (`docs/config-accessibility-audit.md`) mapping every feature/config area to its 6-surface status (✅ / 🟡 partial / 🔴 missing). Each gap gets a BL sub-item. The audit itself is a 1-sprint pass; gap closures are bundled into the v6.0 release.

**Related:** BL210 (MCP-only audit, v5.27.8) · AGENT.md § Configuration Accessibility Rule · `internal/server/api.go` · `internal/router/commands.go` · `internal/mcp/` · `internal/server/web/app.js`

---

#### BL221 — PRD system complete rebuild design (filed 2026-05-02)

**Context:** The PRD (autonomous decomposition) system has been extended incrementally since v4.x — lifecycle states, per-story/task profiles, file association, recursive child PRDs, templates, guardrails, verifier, DAG orchestrator. Each addition was correct in isolation but the accumulated design has diverged from the intent of the unified platform described in [`docs/plans/2026-05-02-unified-ai-platform-design.md`](2026-05-02-unified-ai-platform-design.md).

**This is a design discussion item — implementation begins only after operator sign-off on the new design.** Work is deferred to the v6.0 implementation window.

**Intended scope of the rebuild discussion:**

1. **Align PRD with session type taxonomy** — the unified platform design introduces `coding | research | operational | personal | skill` session types. PRDs today are implicitly `coding`-typed. A research PRD (decompose a literature review into sub-queries), an operational PRD (decompose a runbook), or a personal PRD (decompose a life goal into milestones) require different decomposition prompts, different verifiers, and different memory namespaces. The PRD schema needs a `type` field that threads through decomposition, execution, and verification.

2. **ISA (Ideal State Artifact) generalization** — PAI's ISA concept (from `docs/plans/2026-05-02-pai-comparison-analysis.md`) describes a PRD-like document for any task type. The rebuild should generalize the PRD into an ISA: any operator goal (software, research, creative, operational) can be expressed as an ISA with a type-appropriate decomposition strategy.

3. **Algorithm mode integration** — the Algorithm mode session template (Observe → Orient → Decide → Act → Summarize) should be available as a PRD execution mode. An `algorithm_mode: true` PRD pauses at Decide phase for operator approval of the decomposition before execution begins. This replaces the existing `needs_review` approval gate with a more structured phase model.

4. **Council gate** — the unified platform design adds a Council guardrail (multi-agent debate before major decisions). A `pre_decompose_council: true` PRD flag should run a Council session on the proposed approach before decomposition, feeding the consensus recommendation into the decomposer's context.

5. **Evals integration** — the existing verifier is a binary yes/no. The new evals framework (BL221 depends on evals design; see unified platform doc Week 6-7) should replace it with a rubric-based scorer supporting multiple grader types (string_match, regex, llm_rubric, binary_test).

6. **Workflow** — the current PRD UI and API are functional but the UX is rough (operator must navigate 5 states, manually approve, track stories). The rebuild should produce a cleaner linear operator workflow:
   - Create PRD (type + goal description)
   - Decompose (automatic, or Algorithm mode with Observe/Orient phases presented to operator)
   - Review decomposition (single approval gate replacing the needs_review → approved two-step)
   - Run (with live progress, per-story status, council gates and eval checkpoints inline)
   - Complete (eval score shown, learnings extracted, memory saved)

7. **API stability** — the rebuild must preserve backward compatibility for all existing REST and MCP surfaces or provide a migration path. The `GET/POST /api/autonomous/prds` shape should stay stable; new fields are additive.

**Design inputs:**
- `docs/plans/2026-05-02-unified-ai-platform-design.md` — Part II (PRD types, Algorithm mode, Council, Evals)
- `docs/plans/2026-05-02-pai-comparison-analysis.md` — ISA concept, PAI vs datawatch PRD gap analysis
- `internal/autonomous/` — current implementation
- `docs/api/autonomous.md` — current API reference

**Next step:** Operator + Claude Code design session (2026-05-03 or later). Create `docs/plans/2026-05-02-prd-rebuild-design.md` as the output of that session.

---

| ID | Item | Status |
|----|------|--------|
| **BL210** | **Daemon MCP coverage parity audit** — remaining gaps after v5.27.8 partial close. Original audit: 126 REST surfaces vs 130 MCP tools; ~85% coverage. v5.27.8 closed: `memory_wal`, `memory_test_embedder`, `memory_wakeup`, `claude_models`, `claude_efforts`, `claude_permission_modes`, `rtk_version`, `rtk_check`, `rtk_update`, `rtk_discover`, `daemon_logs` (11 tools). **2026-05-02 scope expansion:** full Configuration Accessibility Rule audit (YAML + REST + MCP + CLI + Comm + PWA) tracked as BL220. BL210 closes when remaining MCP gaps (below) are closed. | Open — deferred to v6.0 window. Remaining gaps: filters CRUD, backends listing, federation sessions, device register, files browser, 3 session sub-endpoints. |
| **BL218** | **Channel session-start hygiene** — 4 gaps in Go-first/JS-fallback bridge wiring. See detail section above. | Open — v6.0 window. |
| **BL219** | **LLM tooling lifecycle** — per-backend artifact setup/teardown, ignore-file hygiene, cross-backend cleanup. See detail section above. | Open — v6.0 window. |
| **BL220** | **Configuration Accessibility Rule full alignment audit** — 6-surface matrix (YAML + REST + MCP + CLI + Comm + PWA). See detail section above. | Open — v6.0 window. Deliverable: `docs/config-accessibility-audit.md`. |
| **BL221** | **PRD system complete rebuild design** — design discussion item; refs unified platform design doc. See detail section above. | Open — design session 2026-05-03+. |
| BL190 | **Howto screenshot density** — 22 shots across 8 howtos; below the 15-20-per-howto target. | Iterative cosmetic; pick up only if an operator hits a recipe gap. |

#### BL210 — MCP coverage gaps (current status after v5.27.8 partial close)

Audit: **126 REST surfaces; 130 MCP tools** at time of filing. v5.27.8 closed 11 tools (memory ×3, LLM listing ×3, RTK ×4, daemon_logs ×1). Remaining gaps below.

**Closed in v5.27.8** ✅

| Tool added | Closes |
|---|---|
| `memory_wal` | `GET /api/memory/wal` |
| `memory_test_embedder` | `POST /api/memory/test` |
| `memory_wakeup` | `GET /api/memory/wakeup` |
| `claude_models` | `GET /api/llm/claude/models` |
| `claude_efforts` | `GET /api/llm/claude/efforts` |
| `claude_permission_modes` | `GET /api/llm/claude/permission_modes` |
| `rtk_version`, `rtk_check`, `rtk_update`, `rtk_discover` | RTK quartet |
| `daemon_logs` | `GET /api/daemon/logs` |

**Still open — deferred to v6.0 window** 🔴

| Area | Missing MCP | Priority |
|---|---|---|
| Filters | `filter_list` / `filter_upsert` / `filter_delete` — detection filter management from IDE | High |
| Backends | `backends_list` / `backends_active` — reachability + version info (get_config doesn't include this) | High |
| Sessions | `session_set_state` / `session_set_prompt` — two sub-endpoint operations lacking MCP | Medium |
| Federation | `federation_sessions` — proxy-mode aggregated session list | Medium |
| Files | `files_list` / `files_browse` — directory browser | Medium |
| Devices | `device_register` — mobile push token registry write | Low |
| Sessions | `session_aggregated` — cross-proxy aggregated view | Low |

**Full MCP coverage (no gaps):**

Sessions (start, list, get, output, timeline, send, kill, restart, rename, delete, bind, import, reconcile, rollback) ✅ · Autonomous ✅ · Observer ✅ · Orchestrator ✅ · Memory (all 16 tools) ✅ · KG ✅ · Pipeline ✅ · Profiles ✅ · Plugins ✅ · Templates ✅ · Cooldown ✅ · Cost ✅ · Audit / Analytics / Diagnose / Stats / Alerts ✅ · Config ✅ · Reload ✅ · Update ✅ · Schedule ✅ · Saved commands ✅ · RTK ✅ · LLM listing ✅

**Note:** BL220 (Configuration Accessibility Rule full audit) extends BL210's MCP scope to the full 6-surface matrix.

### Awaiting operator action

_(empty — every item that was here as of v5.0.5 is now answered or shipped. New operator-decision items land here with **What's needed / Options / Recommendation**.)_

### Recently closed (sticky for one release cycle, then archived)

**Audit (v5.27.0, 2026-04-28):** spot-checked the new entries by grepping current source for the specific files / functions / config keys each entry claims. All verified present. Pre-v5.0 entries audited in the v5.0.5 sweep are kept inline below for cross-reference but rolled-up; assume true unless flagged.

| ID | Closed in | What |
|----|-----------|------|
| BL214 UX fix — language picker promoted + whisper.language tracks PWA locale | v5.28.3 | Operator-asked UX fix on top of v5.28.0/.1 i18n foundation: (1) language picker moved to top of Settings → About (the datawatch identity card), Settings → General → Language kept synced for discoverability; (2) PWA UI language now the default app language — `setLocaleOverride()` syncs `whisper.language` via PUT /api/config when picking a concrete locale (Auto leaves whisper alone); (3) `whisper.language` form field removed from the PWA Whisper card and replaced with a read-only "tracks PWA language" indicator. New `readonly` config-form field type. Configuration parity preserved — `whisper.language` still settable via YAML / REST / MCP / CLI / chat for power-users who need a different transcription language than UI language. Mobile parity at datawatch-app#40 (language picker placement + whisper sync) + #41 (BL208 #30 PRD card style audit gap caught during the same UI-change → mobile-parity audit). |
| BL173-followup — cluster→parent push verified end-to-end in testing cluster | v5.28.2 | **BL173-followup CLOSED.** Verified end-to-end in the operator's testing cluster (`kubectl context: testing`, 3-node Ubuntu 22.04 cluster on 10.8.2.0/24). Deployed `ghcr.io/dmz006/datawatch-parent-full:latest` v5.28.1 as a Deployment in `bl173-verify` ns with seeded config (token + `observer.peers.allow_register=true`) via initContainer + ClusterIP Service. Ran a separate `curlimages/curl` peer Pod that hit `parent.bl173-verify.svc.cluster.local:8080`: `[1] register peer prod-pod-test → token Aqw-…`, `[2] push snapshot → status:ok`, `[3] /api/observer/envelopes/all-peers → by_peer includes prod-pod-test envelope`, `[4] DELETE → status:ok`. Real cluster pod-network topology: peer pod → ClusterIP Service → parent pod cross-node. The dev-workstation pod-overlay gap that originally blocked this is resolved by deploying parent in-cluster (which is the production topology anyway). Runbook in `docs/howto/federated-observer.md` carries forward as the operator-side prod-cluster check; the BL173-followup item itself is done. |
| BL214 wave-2 + BL173-followup runbook | v5.28.1 | **BL214 wave-2** — i18n string-coverage extension. Wired through `t()`: confirm-modal Yes/No buttons (`showConfirmModal`), session dialog titles (delete/stop-session via `dialog_*` Android keys), batch-delete count `%1$d` placeholder, alerts-tab loading + empty state, Autonomous-tab `templates` filter label + New-PRD FAB title. 4 new universal keys (`action_yes`/`action_no`/`common_loading`/`common_no_alerts`) added to all 5 locale bundles + filed at datawatch-app#39 per the v5.28.0 Localization Rule. `TestLocales_CommonNavKeysPresent` parity guard extended. **BL173-followup** — cluster→parent push handler verified end-to-end (peer `bl173-verify` round-tripped: register → push → aggregator includes peer → cleanup). New "Production-cluster reachability check (BL173-followup)" runbook in `docs/howto/federated-observer.md` with the exact pod-side curl + cleanup commands so the operator's production-cluster verification is one-shot when convenient. Failure-mode triage documented (connection error = network gap; 401/403 = auth/token plumbing). |
| BL214 — PWA i18n foundation (DE/ES/FR/JA) | v5.28.0 | **BL214** (datawatch#32) — PWA i18n foundation with translations sourced 1:1 from the datawatch-app Compose Multiplatform Android client (`composeApp/src/androidMain/res/values{,-de,-es,-fr,-ja}/strings.xml`). 5 locale bundles (~240 keys each) embedded in the binary at `internal/server/web/locales/`. Zero-dep `window._i18n` + `t(key, vars)` helper with Android-style `%1$s`/`%1$d` placeholders. `applyI18nDOM` sweeps `data-i18n="<key>"` (with `data-i18n-attr`/`data-i18n-html` variants). Auto-detection: `navigator.language` strip-to-base → fallback to `en`. Settings → General → Language picker (Auto / EN / DE / ES / FR / JA) persisted in localStorage; reload applies. Initial coverage: bottom nav (Sessions/Autonomous/Alerts/Settings) + Settings tabs (Monitor/General/Comms/LLM/About). 3 new tests in `internal/server/v5280_locales_test.go` (presence + ≥90% EN-parity + required-key guards). Iterative string-coverage expansion across the remaining ~9700 lines of `app.js` continues in v5.28.x patches. |
| BL216 — MCP channel bridge introspection (full parity) + BL109 stale-`.mcp.json` fix | v5.27.10 | **BL216** — operator question "ring-laptop has stale `~/.mcp.json` pointing at node + channel.js but daemon log says Go bridge; which is actually used?" answered through every parity surface. New `GET /api/channel/info` returns `{kind, path, ready, hint, node_path, node_modules, stale_mcp_json}`. New `channel_info` MCP tool. New `datawatch channel info` + `datawatch channel cleanup-stale-mcp-json` CLI subcommands (with `--dry-run`/`--json`). New chat `channel info` command. New PWA Monitor → MCP channel bridge panel with kind badge (Go ✓ / JS ⚠) + stale-mcp-json warnings. Per-session register-time daemon log line `[channel] session <id> registered with <kind> bridge at <path>`. **BL109 fix** — `WriteProjectMCPConfig` now writes `Command: <go-bridge>, Args: []` when `BridgePath()` is set, instead of hardcoding `node + channel.js` (which produced stale files on Go-bridge hosts since v5.4.0). 11 new tests across `internal/channel/v52710_bridge_test.go`, `internal/server/v52710_channel_info_test.go`, `internal/router/v52710_channel_info_test.go`. datawatch-app#38 tracks mobile mirror. |
| BL213 + BL212 follow-up — Signal device-linking API + JS channel memory parity | v5.27.9 | **BL213** (datawatch#31) — three Signal device-linking endpoints completed for the mobile companion. **(1)** `GET /api/link/qr` aliased to the existing SSE QR-pair stream (mobile expects the `qr` path name). **(2)** `GET /api/link/status` upgraded from placeholder to real impl: shells out to `signal-cli -a <account> listDevices` and returns the parsed device list `[{id, name, created, last_seen}, ...]` via new `parseListDevicesOutput` helper. **(3)** `DELETE /api/link/{deviceId}` invokes `signal-cli removeDevice -d <id>` with guardrails: rejects non-DELETE (405), missing/non-numeric id (400), device id 1 (primary, 400), and missing `signal.account_number` config (503). 7 new tests in `v5279_link_test.go`. **BL212 follow-up** (datawatch#29) — v5.27.7 added memory tools to the Go bridge but left the JS fallback at `reply`-only. Operator caught this: ring-laptop / storage testing instances still hit the JS path via `~/.mcp.json` pointing at `node ~/.datawatch/channel/channel.js`. v5.27.9 mirrors `memory_remember` / `memory_recall` / `memory_list` / `memory_forget` / `memory_stats` into `internal/channel/embed/channel.js` with a new `callParent` helper that returns the parent's response body (legacy `postToDatawatch` stays for fire-and-forget paths). HTTP errors surface as MCP errors (no silent empty results). 3 new Go snapshot tests in `internal/channel/v5279_channeljs_test.go`. |
| BL208 #30 + BL210 — PRD card style alignment + daemon-MCP gap closures | v5.27.8 | **BL208 #30** (datawatch#30) — PRD card harmonised with Sessions card style. New `.prd-card` CSS class shares the bg2 / system-radius / 12px-14px padding shape; status drives the 4px left-border colour via `.prd-card-status-{draft,decomposing,needs_review,…}` modifiers. Redundant "PRDs" sub-header on the Autonomous tab dropped (operator: tab label is enough). `renderPRDRow` switched off inline border/padding to the class. `.prd-row` alias kept for the v5.26.6 `scrollToPRD` selector. **BL210** — 11 new MCP tools close the daemon-MCP coverage gaps the operator audit flagged: `memory_wal` / `memory_test_embedder` / `memory_wakeup` (operator-priority memory gaps), `claude_models` / `claude_efforts` / `claude_permission_modes` (v5.27.5 LLM listing endpoints), `rtk_version` / `rtk_check` / `rtk_update` / `rtk_discover` (RTK quartet), `daemon_logs`. All forward to existing `/api/*` paths via `proxyJSON`. Bodies in `internal/mcp/v5278_gap_closures.go`; registration inlined into `mcp.New()` alongside the other memory tool block. Remaining BL210 gaps (filters CRUD, backends listing, federation aggregated, devices register, files browser, three sessions sub-endpoints) deferred to v5.28.x — none are operator-priority. |
| BL208 #26 + #27 + BL209 + BL212 — PWA UI parity + config-driven quick commands + channel.js memory tools | v5.27.7 | **BL208 #26** — Running badge pulse (CSS @keyframes 0.55→1.0 over 700ms) + 3-dot generating indicator below the terminal output (each dot fades over 600ms with 200ms stagger). Pure-CSS, prefers-reduced-motion respected. JS hook on state transitions injects/removes the indicator div. **BL208 #27** — scroll-mode button glyph swapped `↕` → `📜` to match Android's TerminalToolbar. **BL209** (datawatch#28) — new `GET /api/quick_commands` endpoint serving an operator-editable list (config: `session.quick_commands`); falls back to a 15-entry baseline (`yes`/`no`/`continue`/`skip`/`/exit` + `Esc`/`Tab`/`Enter`/arrows/`PgUp`/`PgDn`/`Ctrl-b`). Mobile + PWA migration off hardcoded lists tracked at datawatch-app#31. **BL212** (datawatch#29) — `cmd/datawatch-channel/main.go` (Go bridge spawned per claude-code session) gains `memory_remember`/`memory_recall`/`memory_list`/`memory_forget`/`memory_stats` MCP tools. Each forwards to the parent's `/api/memory/*` endpoints via a new `callParent` helper. New `urlQueryEscape` keeps the bridge stdlib-only (no `net/url` dep). 11 new Go tests. New smoke section §7x. **BL208 #30** (PRD card style alignment) deferred to v5.27.8 — bigger restyle of `renderPRDRow` + Sessions card harmonisation. |
| BL211 + BL215 — scrollback state-detection + rate-limit miss hotfix | v5.27.6 | **(BL211)** New `CapturePaneLiveTail()` method on TmuxAPI that always reads the live pane bottom regardless of tmux copy-mode. State detection at `manager.go:1489` switched off `CapturePaneVisible` (which captures scrolled view in copy-mode for PWA display) onto the live tail. Operator scenario fixed: scrolling up no longer pins state detection on stale content while claude finishes its turn. **(BL215)** Per-line rate-limit length gate raised 200 → 1024 chars at `manager.go:3791` because modern claude rate-limit dialogs are paragraph-length with context (operator hit one on 2026-04-30 that datawatch missed; line was ~600 chars containing "5-hour limit reached"). The +60min fallback for no-reset-time messages was already correct (line 3837-3840) — BL215 only had to fix the upstream miss. 8 new Go tests covering both fixes; full sweep 1508/1508. PWA `CapturePaneVisible` keeps the operator-friendly scroll behaviour for the display channel — surgical fix, no UX regression. |
| BL207 — claude permission_mode + model + effort surfaces (plan-mode for PRDs) | v5.27.5 | Three new claude-code per-session options surfaced through every parity surface. **(1)** New REST endpoints `GET /api/llm/claude/{models,efforts,permission_modes}` return hardcoded lists (Anthropic /v1/models query frozen as BL206 per operator decision). **(2)** New `session.permission_mode` config field (`plan` / `acceptEdits` / `auto` / `bypassPermissions` / `dontAsk` / `default`) — when set, claude-code launches with `--permission-mode <value>` and `--dangerously-skip-permissions` is suppressed (explicit operator mode wins). **(3)** Per-session overrides via `POST /api/sessions/start` body (`permission_mode`, `model`, `claude_effort`). **(4)** `PRD.PermissionMode` + `Task.PermissionMode` so PRDs can run a single design-only step (`plan`) inside an otherwise execute-the-plan PRD; executor resolves task → PRD → global. **(5)** PWA New Session modal gains a claude-only options block (Permission mode / Model / Effort dropdowns) populated from the new endpoints; visible only when backend=claude-code. **(6)** Settings → LLM → claude-code panel + Settings → General → Sessions both gain a `permission_mode` text field. New AGENT.md rule: every major release refreshes the hardcoded alias list against Anthropic's current set. 10 new Go tests. New smoke section §7w. datawatch-app sync issues to follow for the mobile companion. |
| BL205 — GET /api/update/check + modern rate-limit patterns | v5.27.4 | **(1) datawatch#25** — new read-only `GET /api/update/check` endpoint so mobile + PWA clients can implement "check → confirm → install" UX without firing the install on the first call. POST /api/update keeps its existing atomic check+install behaviour. PWA `checkForUpdate()` migrated off direct api.github.com calls onto the daemon endpoint (one source of truth, no CORS, goes through daemon auth). 6 new Go tests cover the happy path + method enforcement + side-effect guard. **(2) Operator-reported rate-limit regression** — `rateLimitPatterns` extended with the modern claude-code phrasings (`limit reached`, `weekly usage limit`, `hit weekly limit`, `5-hour limit`, `opus/sonnet limit reached`) that the legacy "You've hit your limit" pattern no longer catches. Both the auto-schedule resume + the alert filter pick up the new phrasings. 4 new Go tests in `internal/session/v5274_ratelimit_modern_test.go`. New smoke section §7v. |
| Hotfix — chat-channel reload wire-up + claudeDisclaimerResponse refactor | v5.27.3 | v5.27.2 wired `SetReloadFn` on the production comm router but missed the `testRouter` that backs `POST /api/test/message`. The smoke test surfaced "Reload not wired by this build" via the chat surface; fixed by wiring symmetrically. Plus `claudeDisclaimerResponse` extracted as a pure helper for unit-testability (4 new test cases). 9 new Go tests + 5 new smoke checks (§7u). Process note: the test+doc commit on top of v5.27.2 included a code change that should have been its own patch — recorded in `docs/plans/RELEASE-NOTES-v5.27.3.md` as a lesson for the next release window. |
| BL204 — subsystem hot-reload + claude auto-accept disclaimer | v5.27.2 | Two operator-asked items closed in one patch. **(1) Subsystem reload** — new `POST /api/reload?subsystem=<name>` endpoint + `Server.RegisterReloader` API + named reloaders for `config` / `filters` / `memory`. Replaces the all-or-nothing daemon restart for hot-reloadable subsystems. Full parity: CLI `datawatch reload [subsystem]`, MCP `reload` tool with `subsystem` arg, chat `reload [subsystem]` command, REST endpoint, PWA Settings → General → Auto-restart on config save (existing toggle now flagged in docs as "subsystem-reload aware"). docs/operations.md updated with the "Why most config changes don't trigger a restart" explainer. **(2) Claude auto-accept disclaimer** — new `session.claude_auto_accept_disclaimer` config flag (default false). When on + backend is `claude-code`, the existing FilterEngine `DetectPrompt` hook auto-sends `1\n` for "trust this folder" / "Quick safety check" and `\n` for "Loading development channels" after a 750ms debounce. Full parity: YAML `session.claude_auto_accept_disclaimer`, REST `PUT /api/config`, MCP `config_set`, CLI `datawatch config set`, comm `configure`, PWA Settings → LLM → claude-code → "Auto-accept startup disclaimer". 2 new chat-parser tests (1471 total). |
| Bug fix — xterm refit + input rebind on prompt cycle | v5.27.1 | Operator-reported: submitting a follow-up prompt resized xterm wrong + dropped the tmux input element's Enter handler, forcing exit/re-enter to recover. Cause: `refreshNeedsInputBanner` (the state-driven banner toggle) was patching slot innerHTML without the immediate `requestAnimationFrame → fitAddon.fit() → resize_term` sync that v5.26.44 added to the explicit Dismiss path. Fixed by comparing before/after banner HTML and running the same fit sequence on any change; rebinds the Enter handler when missing via a `_dwEnterBound` flag on the input element. |
| Mempalace alignment minor — full configuration parity | v5.27.0 | Bundled the v5.26.70 + v5.26.72 mempalace alignment work behind full configuration parity per the project rule: every feature reachable from REST + MCP + CLI + comm channels + PWA. New PWA **Memory Maintenance** section under Settings → Monitor → Memory mirrors the new tools (`sweep_stale`, `spellcheck`, `extract_facts`, `schema_version`) and links to `docs/memory.md` + `RELEASE-NOTES-v5.27.0.md`. Earlier v6.0.0 draft backed out before publish so the v6.0 cut moment stays under operator control. 1469 unit tests (+5 router parsing); smoke 72/0/4. [datawatch-app#21](https://github.com/dmz006/datawatch-app/issues/21) filed for mobile mirror. |
| stdio MCP probe + L4-L5 wake-up REST + GHCR cleanup | v5.26.71 | Three closed in one bundle: (1) `scripts/release-smoke-stdio-mcp.sh` spawns `datawatch mcp` as a subprocess, sends JSON-RPC initialize + tools/list + tools/call(memory_recall), validates each response — required fixing a nil-reader segfault in `ServeStdio` and registering memory tools always-on so they surface in `tools/list`. (2) New `GET /api/memory/wakeup` REST endpoint composes the L0+L1+L4+L5 bundle on demand; `release-smoke-wakeup.sh` probes 3 shapes. (3) `.github/workflows/ghcr-cleanup.yaml` runs weekly + workflow_dispatch, deletes versions from closed minor lines while keeping latest patch + `latest`; uses `GITHUB_TOKEN` with `packages: write` — no PAT. |
| Mempalace QW bundle + ZAP active scan + PRD spacing | v5.26.70 | Five mempalace quick-win Go-native ports: auto-tag on save (`room_detector.go`), memory pinning (column + REST `POST /api/memory/pin` + L1 boost), conversation-window stitching (`conversation_window.go`), query sanitizer (`query_sanitizer.go` — 10 OWASP-LLM01 patterns redacted before embedder), repair self-check (`repair.go`). ZAP workflow gets two new active-scan passes (PWA full + API full with `-t`). PWA `renderStory` / `renderTask` filter empty segments + fold `✎ files` button inline when no files planned. |
| docs/testing.md ↔ smoke coverage audit | v5.26.66-69 | docs/testing.md ↔ smoke coverage audit (#41) closed via §7n KG add+query / §7o spatial-filter / §7p entity detection / §7q per-backend send / §7r stdio MCP / §7s wake-up L4/L5 prerequisite check sections in `release-smoke.sh`. Six new smoke sections cover the gaps `docs/testing.md` flagged. |
| PRD-flow Phase 4 — file association | v5.26.64–67 | `FilesPlanned` decomposer prompt extension + per-task `FilesTouched` post-session diff hook (`ProjectGit.DiffNames` capped at 50 paths) + `RecordTaskFilesTouched` + PWA file-edit modal + ⚠ conflict markers when two pending stories plan the same file. JSON-tag rename `files_planned` → `files`. [datawatch-app#19](https://github.com/dmz006/datawatch-app/issues/19) filed for mobile mirror. |
| PRD-flow Phase 3 — per-story execution profile + approval gate | v5.26.60–63 | Per-story state machine (`pending → awaiting_approval → pending → in_progress → completed`), per-story `ExecutionProfile` override (most-specific wins), `Approve` / `Reject` per story with `RejectedReason` rendered inline. Unified Profile dropdown in the New Session modal. [datawatch-app#18](https://github.com/dmz006/datawatch-app/issues/18) + [#20](https://github.com/dmz006/datawatch-app/issues/20) filed. |
| Settings card-section docs chips | v5.26.0 | Operator-reported: every Settings card needs a docs chip; complex settings should link to howto. `settingsSectionHeader(key, title, docsPath)` already supported the docs arg but no caller passed one. v5.26.0 threads `sec.docs` through all three field arrays (COMMS / LLM / GENERAL). Complex sections (autonomous / orchestrator / voice / pipelines / memory / sessions / RTK) point at the relevant howto; simpler ones (web server / MCP server / plugins / datawatch / auto-update) point at architecture doc. Pure-PWA change; existing `Show inline doc links` toggle still hides all chips when off. README marquee → v5.26.0. |
| Settings card-section docs chips | v5.26.0 | Operator-reported: every Settings card needs a docs chip; complex settings should link to howto. `settingsSectionHeader(key, title, docsPath)` already supported the docs arg but no caller passed one. v5.26.0 threads `sec.docs` through all three field arrays (COMMS / LLM / GENERAL). Complex sections (autonomous / orchestrator / voice / pipelines / memory / sessions / RTK) point at the relevant howto; simpler ones (web server / MCP server / plugins / datawatch / auto-update) point at architecture doc. Pure-PWA change; existing `Show inline doc links` toggle still hides all chips when off. README marquee → v5.26.0. |
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
| BL206 (frozen 2026-04-29) | **Anthropic `/v1/models` query for live claude model list.** Operator decision 2026-04-29: don't query the API. v5.27.5 ships hardcoded alias list (`sonnet`/`opus`/`haiku` + recent full names). Revisit when Anthropic ships a new alias that operators want surfaced before the next datawatch release picks it up — that's the only forcing function for the API integration. | Defer; aliases stay in code-controlled list. |
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
