# Settings-tab federation (item 3 of 3)

**Date:** 2026-10-08
**Version at planning time:** v8.73.14

## Scope

Operator decision: "do all three" (GH#194, Dashboard/Observer denial
coverage, full Settings-tab federation), this is item 3 — the original
directive's literal subject. GH#194 shipped as v8.73.10
(`docs/plans/2026-10-08-gh194-never-say-local.md`); the Dashboard/
Observer extension shipped as v8.73.11
(`docs/plans/2026-10-08-pwa-federated-error-visibility.md`, Phase 4).

Files:
- `internal/server/web/app.js`
- `internal/server/web/app-settings-fed.test.js` (new)
- `CHANGELOG.md`, `docs/testing-tracker.md`, version bump

No Go changes. No new locale strings needed — every error message
surfaced is either the server's own real text (via `_fedFetchError`)
or an existing `t()`-backed fallback already in the bundles.

## Inventory

`renderSettingsView()` fires ~45 independent `load*()` calls
unconditionally on render (sections are pre-built and hidden/shown via
CSS per tab, not lazy-loaded per tab-switch). Audited every one found
by name-pattern search (`load*Config`, `load*Panel`, `load*List`,
`load*Status`) for two things: (a) raw `fetch()` vs. `apiFetch()` —
whether a selected remote server's data is fetched AT ALL, and (b)
whether a real error message reaches the user on failure, vs. a
generic string or silent swallow.

### Migrated raw fetch → apiFetch (14 functions)

Previously never reflected a selected remote peer's data at all, and
most had no real error handling either (several had NO `.catch()` at
all — an unhandled rejection left the view stuck on its loading
placeholder forever):

`loadLinkStatus`, `loadConfigStatus`, `loadCommsConfig`,
`loadGeneralConfig`, `loadAgentsConfig`, `loadCommBackendsStatus`,
`loadSavedCommands`, `loadAlertRules`, `loadAlertRuleFirings`,
`loadExitHooks`, `loadWorkQueue`, `loadFilters`, `loadVersionInfo`,
`loadProfiles` (backs both `loadProjectProfiles`/`loadClusterProfiles`).

`loadCommsConfig`/`loadGeneralConfig`/`loadLLMTabConfig` each populate
*multiple* independent DOM sections from one fetch (`COMMS_CONFIG_FIELDS`/
`GENERAL_CONFIG_FIELDS`/`LLM_CONFIG_FIELDS`) — on a real (non-local)
failure, every section now gets the error message; a local failure
keeps its pre-existing silent-swallow behavior (unchanged scope: this
pass is about federation, not fixing unrelated local-failure UX).

### Error-visibility fixed on already-apiFetch loaders (10 functions)

Already proxy-aware (so already got the real 401/403/502 text from
`_fedFetchError` at the fetch layer) but each `.catch()` replaced it
with a generic string or empty swallow:

`loadGuardrailProfilesPanel`, `loadProxySettings`, `loadCostRatesConfig`,
`loadDetectionFilters`, `loadBrandingPanel`, `loadAboutMcpToolsSummary`,
`loadLLMTabConfig` (also multi-section, see above), `loadAutomataSettingsPanel`,
`loadDocsTrustPanel`, `loadFileServicePanel`.

Shared helper: `_fedMsg(e, fallback)` (renamed from `_obsFedMsg` —
originally written for Observer's sub-cards in the prior round, reused
here for the identical reason).

### Already correct — confirmed, not assumed (partial list)

`loadComputeNodesPanel`, `loadLLMsPanel`, `loadSecretsPanel`,
`loadTailscaleStatus`, `loadWebSearchProvidersList`,
`loadSyncedSkillsList`, `loadSchedulesList`, `loadCooldownStatus`,
`loadAcmeStatus`, `loadTemplatesPanel`, `loadDeviceAliasesPanel`,
`loadPipelinesPanel`, `loadConfigViewer`, `loadPluginsPanel`,
`loadCommunityPluginsPanel`, `loadRoutingPanel`, `loadChannelRoutingPanel`,
`loadAutomataTypeRegistryPanel`, `loadOrchestratorPanel`,
`loadToolingPanel`, `loadDiscussionPanel`, `loadSkillsPanel`,
`loadIdentityPanel`, `loadAlgorithmPanel`, `loadEvalsPanel`,
`loadCouncilPanel` — each individually spot-checked (apiFetch +
`.catch(e => ...e.message...)` already present). No change made.

### Deliberately left alone — judgment calls, flagged for operator sign-off

- **`loadServers`/`loadServersList`** (the Remote Servers card itself,
  and the picker's own backing data) — NOT migrated to apiFetch. Their
  semantics are genuinely ambiguous under federation: should viewing a
  remote peer's Settings show *that peer's own* list of *its* federated
  peers (true multi-hop federation), or should "what other servers
  exist" always mean "from this browser's own local vantage point,
  regardless of which peer's data you're currently viewing"? Changing
  this carries real behavioral risk without a clear answer, so left as
  local-only pending explicit direction.
- **`loadTailscaleConfig`** — populates form-field placeholders only
  (no dedicated status-display element to put an error message into
  without restructuring markup); left silently degrading to blank
  fields on failure, same as before this pass. `loadTailscaleStatus`
  (the adjacent status *panel*, which does have a display element) was
  already correct and untouched.
- **`fileServiceUpload`** — its own raw (non-`apiFetch`) `fetch()`
  call uses a `FormData` body for binary file upload; left alone
  (different risk profile and design question than a JSON `GET`
  migration — proxying a potentially-large binary upload through
  `/api/proxy/<name>/...` wasn't evaluated in this pass).

### Not audited at all

This pass's name-pattern search (`load*Config`/`*Panel`/`*List`/`*Status`)
may not be fully exhaustive of every Settings sub-card — e.g.
`loadPushPanel`, `loadFederationPeersPanel` were seen in passing (called
from `switchSettingsTab`/`renderSettingsView`) but not individually
verified. A follow-up pass should re-run the same two-question audit
(proxy-aware? real error?) against the complete call list in
`renderSettingsView`'s own body as the definitive source of truth,
rather than a name-pattern grep.

## Server picker

Added to `renderSettingsView()` via `_injectServerPickerBar(view,
renderSettingsView, { hideAll: true })`, right after the view's
template renders and before any `load*()` call fires. `hideAll: true`
matches the Observer precedent (`docs/plans/2026-10-08-pwa-federated-error-visibility.md`
Phase 2 comment) — an aggregated "All servers" view of ~45 independent
config sections has no coherent meaning the way Dashboard's single
PRD/cost rollup does.

## Capability-aware denial UX

Not separately built — already satisfied by the existing
`_fedFetchError` classifier (shipped in the original federation-
error-visibility round): every 403 this pass's fixes expose shows the
server's own literal `federation peer lacks capability: <cap>` text,
naming the SPECIFIC capability, not a generic "forbidden." No
client-side capability-name mapping was needed or built — the server
is authoritative and already verbose.

## Parity surface

- **REST**: no new endpoints or fields.
- **MCP**: unaffected.
- **CLI**: unaffected — PWA-only.
- **comm channel**: unaffected.
- **YAML/config**: no new config keys.
- **PWA**: this round's entire surface.
- **Android / iPhone**: excluded — PWA-specific client code.

## Verification

- `node --test internal/server/web/app-settings-fed.test.js` — new
  file, 6 tests: picker injection (+ `hideAll:true` assertion), the
  simple-pattern migration (`loadConfigStatus`, both the real-error and
  the proxy-URL-routing regression guard), the multi-section pattern
  (`loadCommsConfig`), the already-apiFetch-just-needed-the-message
  pattern (`loadGuardrailProfilesPanel`), and a confirmation test for
  two of the "already correct" functions (`loadComputeNodesPanel`,
  `loadSecretsPanel`) so that claim is verified, not just asserted.
- `node --test internal/server/web/*.test.js` — full suite green.
- `go build` — clean (version string only, no Go logic touched).
- Not every one of the ~24 touched functions has an individual test —
  most share one of the handful of mechanical patterns above, each
  covered once. Consistent with this session's established practice
  for Observer's sub-cards (Phase 2/4).

## Status

**Partial.** Picker added, 14 functions migrated to proxy-awareness, 10
more given real error-visibility, ~26 confirmed already-correct. Three
explicit judgment calls deferred (`loadServers`/`loadServersList`,
`loadTailscaleConfig`, `fileServiceUpload`). The audit itself may not
be 100% exhaustive — flagged above as a follow-up starting point.
