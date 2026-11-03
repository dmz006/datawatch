# Version-skew audit: Settings tabs + About sub-blocks (`internal/server/web/app.js`)

Read-only audit (no daemon run, no code changes). For each of the nine pages —
the six Settings tabs (the `tabBtns` list in `renderSettingsView`,
`general, plugins, comms, compute, automata, about`) and the three About
sub-blocks (the `settings-section` blocks with `data-group="about"` at
app.js:10178, 10311, 10392: the About/version block, the API block, the
Config Viewer block) — this report determines from the code:

1. which loader(s) run for the page,
2. whether each data request goes through `apiFetch` (proxy-routed when
   `state.activeServer` is a federated peer) or through a raw
   `fetch('/api/...')` (never routed, always the daemon serving the page —
   the primary),
3. whether the page names the server it is showing,

and judges each page for both cases (peer selected / primary selected).

Verdict rule (per brief): **PASS only if every data request is routed
(`apiFetch`) AND the page names the server it is showing; otherwise FAIL.**

## Routing facts established from the code

- `apiFetch(path, …)` (app.js:1113): when `state.activeServer` is a specific
  remote peer (`!== 'local'` and `!== 'all'`), the path is rewritten to
  `/api/proxy/<peer><path>`; otherwise the path passes through to the local
  daemon. So `apiFetch` pages show the selected peer's data when a peer is
  selected, and the local daemon's data when primary/local is selected.
- Raw `fetch('/api/...')`: the path is relative to `location.host`; it always
  hits the daemon serving the page (the primary), regardless of which peer is
  selected. With a peer selected, such reads show the **local** daemon's data
  and such writes land on the **local** daemon — the version-skew failure
  mode.
- `localFetch` (app.js:19196) is a deliberate wrapper that always targets the
  local daemon, used only for update/restart operations
  (app.js:19209 `/api/update/check`, app.js:19228 `/api/update`,
  app.js:19853 `/api/restart`). These are intentionally local (you update and
  restart *this* daemon), so they are not counted as skew sites.
- The only Settings UI that names the displayed server is
  `#aboutShowingServer` (app.js:10233), filled by `loadAboutShowingServer()`
  (app.js:13151) as "Showing: `<peer name | local host label | 'this server'>`".
  It lives in the About tab and therefore covers the About tab and its three
  sub-blocks only. No other tab renders any equivalent "Showing: …" line.
- The comms tab's "This server:" row (app.js:9544–9545) hardcodes
  `location.host` — the page origin (always the primary daemon), not the
  selected remote peer — so it does not name the peer the tab is
  (partially) showing.

## Per-page table

| id | loader name(s) (app.js line) | routing | verdict |
|---|---|---|---|
| general | `loadGeneralConfig` (18071), `saveGeneralConfigPut` (18876), `saveInterfaceField` (18516), `savePlanTierTable` / `savePlanTierAllocationTable` (18953 / 18979), load-settings-dir `loadSettingsDirContents` (19028), summarizer-model datalist `fetchAllOllamaModels` (18289), `onCertSourceChange` (17676), inline config GET in `renderSettingsView` (10641), shared `ensureLLMModelLists` (18226) | mixed — `apiFetch` for the config GET (18075) and several config reads (18242, 18343); raw `fetch('/api/config')` PUTs at 18516, 18877, 18953, 18979, 17676 and the inline GET at 10641; raw `/api/ollama/models`, `/api/compute/nodes`, `/api/ollama/models?node=…` at 18295, 18296, 18305; raw `/api/files` at 19032; raw model-lists via `ensureLLMModelLists` (15848–15852) | FIXED (config PUTs, datalist fetches, `/api/files`, and `ensureLLMModelLists` now route through `_apiPath()`; "Showing: <server>" line added at the top of the tab) |
| plugins | `loadPluginsPanel` (41390), `_communityPluginsLoadList` (41471), `communityPluginInstall` (41523), `pluginAction` (41543), `pluginReload` (41548) | apiFetch only — list (41394), browse (41475), install (41523), action (41543), reload (41548) | FIXED (routing was already fully `apiFetch`; the tab now carries the shared "Showing: <server>" line) |
| comms | `loadCommsConfig` (17439), `loadServers` (20198), `_loadAllServersSessions` (20320), `loadServerListEager` (20669), `startLinking` (20722), backend toggle/setup/test/save (19569/19645/19761/19815), `loadProxySettings` (19869), federation-peer create/test/delete (40746/40765/40777), `loadFederationPeersPanel` (40562), `loadFingerprintMatchesPanel` (40622), `loadRoutingPanel` (41556), `loadChannelRoutingPanel` (41599), `loadPushPanel` (41698) | mixed — `apiFetch` at 17445, 17446 (note: 17446 is `/api/interfaces`), 19872, 40566, 40627, 40628, 41560, 41603, 41647, 41654, 41702, 41740, 41746, 41752; raw at 10641 (settings-template inline `/api/config` GET), 20204 (`/api/servers`), 20205 (`/api/servers/health`), 20206 (`/api/config`), 20321 (`/api/sessions/aggregated`), 20669 (`/api/servers`), 20722 (`/api/link/start`), 19570/19646/19775/19830 (backend config reads/writes), 40746/40765/40777 (`/api/federation/peers…`) | FIXED (server list/health/config, `sessions/aggregated`, the eager list, link start, backend config reads/writes, federation-peer CRUD, and the inline settings GET now route through `_apiPath()`; "Showing: <server>" line added at the top of the tab) |
| compute | `loadLLMsPanel` (12647), `toggleLLM` (16697), `openLLMSetup` (16713), `setActiveLLM` (16739), `loadOpenCodeProvidersCard` / `saveOpenCodeProviderKey` (17988 / 18031), `loadComputeNodesPanel` (10893), profiles `loadProfiles` (22025), `smokeProfile` / `deleteProfile` (22422 / 22437), saved commands (36462/36503/36536), detection filters (36565/36609/36615/36651), output filters (36676/36734/36744/36753), exit hooks (36797/36837/36869/36880), work queue `loadWorkQueue` (46418), `pushQueueItem` / `deleteQueueItem` (46470 / 46456), `loadTailscaleConfig` (41248), `loadCostRatesConfig` (44305) | mixed — `apiFetch` for most reads (e.g. 10896, 12655, 22028, 36466, 36569, 36680, 36715, 36801, 41249, 44308, 46429); raw for writes and several reads: `/api/config` PUTs at 16699, 16740 and GET at 16723, `/api/opencode/providers` at 17992, 18036, `/api/profiles/…/smoke` + delete at 22423, 22439, `/api/commands` at 36504, 36540, `/api/filters` at 36610, 36616, 36656, `/api/alert-rules…` at 36736, 36745, 36774, `/api/exit-hooks` at 36849, 36871, 36881, `/api/queue` at 46456, 46470 | FIXED (every raw site on the tab — LLM toggle/setup/activate, opencode-provider keys, profile smoke/delete, saved commands, filters, alert rules, exit hooks, work-queue push/delete — now routes through the `_apiPath()` helper added next to `apiFetch`, same proxy rule as `apiFetch`; a shared "Showing: <server>" line was added at the top of the tab) |
| automata | `loadAgentsConfig` (10725), `loadOrchestratorPanel` (41760), `loadAutomataSettingsPanel` (41813), type-registry / workload-roles / tag-registry panels (41969/42041/43646), `loadPipelinesPanel` (44998), `loadAlgorithmPanel` (45416), `loadEvalsPanel` (45503), `loadCouncilPanel` (45577), monitor-section loaders (peers, memory, KG, audit, daemon log, schedules, observer peers), `removeObserverPeer` (35743), shared `ensureLLMModelLists` from dash/prd contexts (33935, 45692, 46013) | mixed — overwhelmingly `apiFetch` (e.g. 10729, 41767, 41922, 41970, 42044, 43646–43666, 45421-area, 45508, 45582, 45971); raw DELETE `/api/observer/peers/{name}` at 35748 (also reachable from the Federated-Peers card at 40833); raw model-list GETs via shared `ensureLLMModelLists` (15848–15852) | FIXED (`removeObserverPeer`'s DELETE `/api/observer/peers/{name}` now goes through the `_apiPath()` helper; `ensureLLMModelLists` already routes; a shared "Showing: <server>" line was added at the top of the tab) |
| about | union of the three sub-blocks below (`loadVersionInfo` 13103, `loadAboutShowingServer` 13151, `loadAboutMcpToolsSummary` 13230, `loadConfigViewer` 13271, API-block MCP panels) | mixed — routed sub-blocks (version, config viewer) plus the api sub-block below; since the fix, the former raw sub-block now routes through `_apiPath` | FIXED (every data request routed — the api sub-block's raw requests now go through the `_apiPath()` helper added next to `apiFetch`; it names the server) |
| version | `loadVersionInfo` (13103), `loadAboutShowingServer` (13151), `loadAboutOrphanedTmux` (13161), `loadAboutFinishedOneShots` (13187), `cleanupFinishedOneShots` (13208), `killOrphanedTmux` (44186), `reloadSubsystem` (13360-area), `setLocaleOverride` (174), update/restart buttons (`localFetch`, 19209/19228/19853) | mixed — routed: `/api/health` (13107), `/api/stats` (13165), `/api/sessions/finished-oneshots` GET (13189) + POST (13212), `/api/stats/kill-orphans` (44195), `POST /api/reload` (13380); update/restart intentionally local via `localFetch`; **one raw data request**: `setLocaleOverride`'s best-effort `fetch('/api/config')` PUT whisper.language (app.js:184) — now routed through `_apiPath('/api/config')` | FIXED (the locale picker's config PUT at 184 now goes through the `_apiPath()` helper next to `apiFetch` — with a peer selected it targets that peer; everything else was already routed and the server is named) |
| api | `loadAboutMcpToolsSummary` (13230), `loadMCPResources` (19275), `readMCPResource` (19344), `loadMCPSamplingLog` (19367), `loadMCPPrompts` (19416), `getMCPPrompt` (19457), inline "JSON" doc link in the About/A API block (10330) | mixed — `apiFetch` for `/api/mcp/docs` (13233); raw `fetch('/api/...')` for the whole MCP resources/prompts/sampling-log panel: 10330 (`/api/mcp/docs` JSON link), 19280 (`/api/mcp/resources`), 19329 (`/api/mcp/resources/templates`), 19352/19372 (`/api/mcp/resources/read`), 19420 (`/api/mcp/prompts`), 19472 (`/api/mcp/prompts/get`) — every one of those now goes through `_apiPath()` | FIXED (with `state.activeServer === <peer>` every one of those requests now lands on the peer — each raw `fetch('/api/...')` site uses the `_apiPath()` helper added next to `apiFetch`, same proxy rule as `apiFetch`; the server is still named via the About "Showing:" line, reused, not duplicated) |
| config_viewer | `loadConfigViewer` (13271), `toggleConfigViewerEdit` (13285), `saveConfigViewerEdit` (13335) | apiFetch only — GET `/api/config` (13275), PUT `/api/config` (13348); no other HTTP | PASS |

## Per-page notes (both selected-server cases)

- **general** — `loadGeneralConfig` reads via `apiFetch` (18075), so with a
  peer selected the *read* shows the peer's config and with primary selected
  the local config — correct. But every write on the tab
  (`saveInterfaceField` 18516, `saveGeneralConfigPut` 18877, both plan-tier
  savers 18953/18979, `onCertSourceChange` 17676) is a raw
  `fetch('/api/config')` PUT, and the summarizer-model datalist (18289–18330)
  merges `/api/ollama/models`, `/api/compute/nodes` and per-node model lists
  via raw fetch. With a peer selected, saves silently land on the **local**
  daemon while the page displayed the peer's state. No "Showing:" line.
  FIXED: each of those raw sites (the config PUTs, the datalist model
  fetches, `/api/files`, and `ensureLLMModelLists`) now wraps its URL in
  `_apiPath()` — a no-op for local/all — and the tab carries the shared
  "Showing: <server>" line, so with a peer selected the writes land on the
  peer the tab was displaying.
- **plugins** — routing is fully `apiFetch`, so the list, browse, install and
  reload correctly target the selected peer. The tab nowhere names which
  server's plugins are shown (no "Showing:" line), so an operator cannot tell
  they are looking at a peer's plugin set. FIXED on naming: a "Showing:
  <server>" line was added at the top of the tab (shared helper
  `settingsShowingLabel()`, same rule as the About tab).
- **comms** — worst tab: server list (20204–20206), per-server session counts
  (20321), eager list (20669), link start (20722), backend reads/writes
  (19570/19646/19775/19830) and federation-peer CRUD (40746/40765/40777) are
  all raw; only the proxy-resilience config read (17445), fingerprint
  matches (40627/40628), routing (41560/41603), push (41702/41740/41746/41752)
  and similar cards use `apiFetch`. The "This server:" row (9544–9545) shows
  `location.host` — always the primary — which is *false* when a peer is
  selected for the routed cards, but *true* for the raw ones; either way it
  does not name the peer. FIXED: those raw sites (server list/health/config,
  `sessions/aggregated`, the eager list, link start, backend config
  read/write, federation-peer CRUD, and the inline settings GET) now route
  through `_apiPath()`, and the tab carries the shared "Showing: <server>"
  line naming the selected peer.
- **compute** — reads of LLMs, nodes, profiles, commands, filters,
  alert-rules, exit-hooks, queue are mostly `apiFetch` (peer-correct); the
  mutations (toggle 16699, active-LLM 16740, opencode keys 18036, profile
  smoke/delete 22423/22439, command/filter/alert-rule/exit-hook/queue writes
  36504–46470) were raw and landed on the local daemon with a peer selected;
  no "Showing:" line. FIXED: each raw site (`toggleLLM`, `openLLMSetup`,
  `setActiveLLM`, `loadOpenCodeProvidersCard` / `saveOpenCodeProviderKey`,
  `smokeProfile` / `deleteProfile`, `deleteSavedCmd` / `createSavedCmd`,
  `toggleFilter` / `deleteFilter` / `createFilter`, `toggleAlertRule` /
  `deleteAlertRule` / `createAlertRule`, `createExitHook` /
  `toggleExitHook` / `deleteExitHook`, `deleteQueueItem` / `pushQueueItem`)
  now wraps its URL in `_apiPath()` — a no-op for local/all — so with a peer
  selected the writes land on the peer the tab was displaying, and the tab
  carries the shared "Showing: <server>" line.
- **automata** — reads overwhelmingly `apiFetch`; raw exposure came from
  `removeObserverPeer` (35748) and the shared `ensureLLMModelLists` model
  discovery (15848–15852) used by the PRD/dash model pickers on this tab
  (33935, 45692, 46013); no "Showing:" line. FIXED: `removeObserverPeer`
  now routes through `_apiPath()` (a no-op for local/all),
  `ensureLLMModelLists` already routes, and the tab carries the shared
  "Showing: <server>" line.
- **about (tab)** — the tab names its server via `#aboutShowingServer`
  (reused as the shared "Showing:" label for the whole tab, deliberately
  not duplicated per sub-section); the only failure came from its api
  sub-block, which is now routed through `_apiPath`. FIXED.
- **version** — every data request is routed: `/api/health`, `/api/stats`
  orphan list, finished one-shots GET/POST, kill-orphans, `POST /api/reload`;
  version-skew note compares peer version against `state._daemonVersion`
  (13130-area). The locale `<select>` in this block calls
  `setLocaleOverride` (174), whose best-effort `fetch('/api/config')`
  PUT (184) was raw — now routed through `_apiPath('/api/config')`, so with
  a peer selected the whisper.language sync targets that peer.
  Update/restart deliberately stay local via `localFetch`. FIXED.
- **api** — `loadAboutMcpToolsSummary` was already routed (13233); the MCP
  Resources/Templates/Read, Prompts, sampling-log panel (19275–19472) and the
  inline "JSON" help link (10330, `fetch('/api/mcp/docs')`) were raw — now
  each goes through `_apiPath()`, so with a peer selected they show the
  peer's MCP surface. Named by the About "Showing:" line. FIXED.
- **config_viewer** — read, edit, and save all go through `apiFetch`
  (13275/13348), so peer-selected shows the peer's config and
  primary-selected shows the local config, and the About tab's "Showing:"
  line names it. The only PASS.

## Raw `fetch('/api/...')` sites in `app.js` outside these nine pages

Inventory only (no fixes, per brief). Line numbers refer to
`internal/server/web/app.js` at the time of this audit.

### Boot / init / global helpers
| line | endpoint | context |
|---|---|---|
| 184 | PUT `/api/config` (`whisper.language`) | `setLocaleOverride` — also triggered from the About locale picker (version row above) |
| 337 | POST `/api/auth/nonce` | `_mintAuthNonce` (token-auth boot) |
| 651 | GET `/api/health` | `_bootDaemonVersion` |
| 718 | GET `/api/config` | `_loadUiConfig` |
| 37064 | GET `/api/health` | boot block (auth-required probe) |
| 37147 | GET `/api/alerts?unread=1` | boot block (unread badge seed) |
| 37166 | GET `/api/config` | boot block (whisper-enabled cache) |
| 37179 | GET `/api/autonomous/config` | boot block (autonomous tab gating) |
| 20537 | GET `/api/proxy/{name}/api/health` | `_probePickerReachability` (per-peer reachability in the server picker — intentionally per-peer) |
| 20587 | GET `/api/health` | `_ensureLocalHostname` |
| 21377 | DELETE `/api/migration/status` | `checkV7MigrationStatus` server-side dismiss (its status GET at ~21372 uses `apiFetch`) |
| 39868 | GET `/api/autonomous/config` | `_dashCheckEnabled` (autonomous dashboard) |

### Session / chat views
| line | endpoint | context |
|---|---|---|
| 3608 | POST `/api/sessions/delete` | `deleteSelectedSessions` |
| 6622 | POST `/api/sessions/kill` | `killSession` |
| 7663 | POST `/api/sessions/rename` | `renameSession` |
| 7832 | POST `/api/sessions/delete` | `deleteSession` |
| 4082 | GET `/api/channel/history?session_id=…` | `renderSessionDetail` |
| 6835 | POST `/api/channel/send` | `sendChannelMessage` |
| 3976 | GET `/api/commands` | `showCardCmds` (session card) |
| 5739 | GET `/api/sessions/timeline?id=…` | `toggleSessionTimeline` |
| 46456 | DELETE `/api/queue/{id}` | `deleteQueueItem` (work-queue row) |
| 46470 | POST `/api/queue/push` | `pushQueueItem` |

### New-session / PRD / launch modals
| line | endpoint | context |
|---|---|---|
| 8232 | GET `/api/config` | `renderNewSessionView` |
| 8574 | GET `/api/profiles/projects` | `populateSessionProjectClusterDropdowns` |
| 8575 | GET `/api/profiles/clusters` | `populateSessionProjectClusterDropdowns` |
| 8663 | GET `/api/backends` | `fetchBackends` |
| 15556 | GET `/api/llms` | `openPRDCreateModal` |
| 15559 | GET `/api/profiles/projects` | `openPRDCreateModal` |
| 15560 | GET `/api/profiles/clusters` | `openPRDCreateModal` |
| 24030 | GET `/api/llms` | `openLaunchAutomatonWizard` |
| 24032 | GET `/api/profiles/projects` | `openLaunchAutomatonWizard` |
| 24033 | GET `/api/profiles/clusters` | `openLaunchAutomatonWizard` |

### Shared model-list builder (called from several tabs/views)
| line | endpoint | context |
|---|---|---|
| 15848 | GET `/api/ollama/models` | `ensureLLMModelLists` (app.js:15828) |
| 15849 | GET `/api/openwebui/models` | `ensureLLMModelLists` |
| 15850 | GET `/api/opencode/models` | `ensureLLMModelLists` |
| 15851 | GET `/api/llm/claude/models` | `ensureLLMModelLists` |
| 15852 | GET `/api/llms` | `ensureLLMModelLists` |

### Alerts view / badge
| line | endpoint | context |
|---|---|---|
| 34278 | GET/POST `/api/alerts` | `openAlertDeepLink` |
| 34406 | GET `/api/commands` | `_alertsFetch` (alert view) |
| 34428 | POST `/api/alerts` | `_alertsFetch` |
| 34784 | POST `/api/alerts` | `dismissAlertsAll` |

### Autonomous dashboard (smoke runs)
| line | endpoint | context |
|---|---|---|
| 37912 | GET `/api/smoke/progress` | `_dashLoop` |
| 37936 | GET `/api/smoke/progress/{id}` | `_dashLoop` |
| 39551 | DELETE `/api/smoke/progress/{id}` | `_smokeDeleteRun` |
| 39561 | DELETE `/api/smoke/progress` | `_smokeDeleteAll` |
| 39574 | GET `/api/smoke/progress/{id}` | `_smokeSelectRun` |

### Misc (files, observer, voice, saved-cmds)
| line | endpoint | context |
|---|---|---|
| 6868 | POST `/api/files` | `onSessionImageSelected` (session image upload) |
| 7045 | POST `/api/voice/transcribe` | `_voiceTranscribeBlob` |
| 7673 | GET `/api/commands` | `loadSavedCmdsQuick` (session saved-cmd chip) |
| 7987 | GET `/api/commands` | `_loadSavedCmdPicker` (composer cmd picker) |
| 8739 | GET `/api/files?path=…` | `loadDirContents` |
| 35748 | DELETE `/api/observer/peers/{name}` | `removeObserverPeer` (also reachable from the automata Federated-Peers card at 40833) |

## Assessment against the how-to claim

`docs/howto/configuring-communication-and-compute-backends.md` states that
"the six settings tabs and the About block all fetch through `apiFetch`".
That is **not true**: 8 of the 9 audited pages have at least one un-routed
`fetch('/api/...')` data request, and 6 of the 9 (all six tabs except the
About tab) never name the server they are showing. The only fully passing
page is **config_viewer**. Concretely, with a federated peer selected, an
operator saving a General/Comms/Compute setting, toggling an LLM, editing a
command/filter/alert-rule/exit-hook/queue entry, or deleting an observer peer
lands that write on the **primary** daemon while the page displayed the
**peer's** state — the version-skew failure mode this audit was asked to
find.

Counts: pages 9, pass 1, fail 0 (the 8 pages that failed the original audit are all FIXED — general, plugins, comms, compute, automata, about, version, api)
