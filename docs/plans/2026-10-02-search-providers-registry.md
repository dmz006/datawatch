# Search providers registry — multi-provider web_search, usage tracking, caching

## 1. Context

### Decisions made with the operator (not inferred)

- Operator SSH-investigated (with the assistant) the `datawatch` compute node's
  SearXNG instance (actually bundled inside its `perplexica` container) for a
  PRD note: "SearXNG was degraded/broken" (2026-09-17). Found and fixed a real
  misconfiguration (`bing` engine was `disabled: true`) but also found a
  second, non-fixable issue: Bing's own scraped results are intermittently
  degraded for compound queries — consistent with Bing-side anti-scraping
  throttling, not a config problem. See GH#165.
- Operator explicitly rejected proxying search through their own logged-in
  Google account (account-suspension risk, confirmed via live research) and
  asked for a comparison of: keep tuning SearXNG vs. a real search API vs.
  build a custom scraping gateway.
- Operator chose a real paid API (Brave Search API) as the way to get "a
  controlled point I can enable LLM to search... that I can in the future
  control" — and provided a Brave Search API key, now stored in the secrets
  vault as `brave_search_api_key` (never written to config.yaml in plaintext).
- Operator explicitly asked for: multi-provider support (not a single
  provider swap — "each search engine we have" on the dashboard implies
  SearXNG stays available alongside Brave), usage tracking (total + daily/
  weekly/monthly per provider + a graph + a history of where each search was
  used), a decision on internal caching to reduce paid-API usage, full
  config-accessibility + security (secrets vault, not plaintext), and a
  minor-release documentation pass with a mandatory CI-watch-to-completion
  step added as a standing release rule.

## 2. Scope

Replace the single-provider `web_search.{provider,url,engine,num_results}`
config shape with a **list of named, independently enabled/disabled search
providers** (`web_search.providers: []`), each one `searxng` or `brave`
typed. A router tries enabled providers in priority order (first success
wins) so SearXNG can stay as a free fallback behind Brave as the primary.
Add usage tracking (SQLite-backed event log + rollups), an in-memory/
TTL-persisted result cache to cut paid-API spend, and full CRUD across all
Configuration Accessibility Rule channels.

Legacy single-provider config (`web_search.provider/url/engine/num_results`)
auto-migrates on load into a single `Providers[0]` entry named `"default"` —
existing installs keep working with zero manual action.

## 3. Phases

### Phase 1 — Backend: registry, Brave provider, caching, usage store (this release)

- `internal/config/config.go`: `SearchProvider` struct, `WebSearchConfig.Providers
  []SearchProvider`, legacy-field migration on load.
- `internal/websearch/` (new package): `Provider` interface, `SearxngProvider`
  (existing logic moved+kept), `BraveProvider` (new — Brave Web Search API,
  `X-Subscription-Token` header, official JSON response), `Registry`
  (priority-ordered fallback + records usage + applies cache).
- `internal/websearch/cache.go`: TTL in-memory cache keyed by
  `provider_name+query+num_results`, configurable TTL, default on.
- `internal/websearch/store.go`: SQLite-backed usage store (new
  `~/.datawatch/websearch.db`) — one row per search (ts, provider name/type,
  query text, session/PRD id if known, cache hit, success, latency, error) +
  rollup queries (today/week/month/all-time per provider and total) + a
  daily-bucketed time series for the dashboard graph.
- `cmd/datawatch/main.go` `mcp-search` subcommand + `internal/mcp/search/server.go`:
  route through the new `Registry` instead of calling SearXNG directly.
- Secrets: Brave API key stored via `secret_set`, provider's `api_key` field
  holds a `${secret:name}` ref (`internal/secrets/refs.go` convention,
  already used by the LLM registry) — resolved at use time, never logged,
  never round-tripped in plaintext through REST/PWA (masked on read).

### Phase 2 — All config-accessibility channels (this release)

- REST: `GET/POST /api/websearch/providers`, `PATCH/DELETE
  /api/websearch/providers/{name}`, `POST .../enable|disable|test`,
  `GET /api/websearch/stats`, `GET /api/websearch/history`. Legacy
  `GET /api/web_search/stats` kept as a deprecated alias.
- CLI: `datawatch websearch provider add|list|get|update|delete|enable|
  disable|test`, `datawatch websearch stats`, `datawatch websearch history`.
- MCP: `websearch_providers_list/get/add/update/delete/enable/disable/test`,
  `websearch_stats`, `websearch_history`. `web_search_stats` kept as a
  deprecated alias.
- Comm channel: read-only `websearch stats` + `enable/disable websearch
  provider <name>` (full nested CRUD over chat is out of scope — matches
  the existing precedent for other multi-entry/array config, e.g. council's
  `backends` array special-case in `internal/router/council.go`).
- YAML: `web_search.providers[]` + per-entry fields, documented in
  `docs/config-reference.yaml`.

### Phase 3 — PWA (this release)

- Settings: replace the single web_search field section with a provider
  list card (name/type/enabled/priority, Test button, Edit, Delete, Add)
  modeled on the Council persona list's existing CRUD-modal pattern
  (cascading type-specific fields: searxng → url+engine; brave → API key
  input [masked, write-only] + optional cache-TTL override).
- Dashboard: new "Search Usage" card — total/today/week/month (all-
  providers and per-provider), a daily bar/sparkline graph (hand-rolled SVG,
  no new chart library dependency), and a recent-searches history list
  (timestamp, provider, truncated query, session link when known).

### Phase 4 — Mobile parity (tracked, not implemented here)

File a `datawatch-app` issue for Android/iOS parity on the new Settings
provider-list UI and Dashboard card, per the established "file, don't
implement cross-repo" pattern. Capability parity required, not
implementation parity (Mobile-Parity Rule).

## 4. Parity surface

| Surface | Status |
|---|---|
| YAML | ✅ `web_search.providers[]`, `cache_enabled`, `cache_ttl_seconds` |
| REST | ✅ full CRUD + stats + history (see Phase 2) |
| CLI | ✅ `datawatch websearch ...` |
| MCP | ✅ `websearch_*` tools |
| Comm channel | ✅ read-only stats + enable/disable (array CRUD excluded — chat-unfriendly, matches council `backends` precedent) |
| PWA | ✅ Settings provider-list card + Dashboard usage card |
| Android | ⏳ tracked via datawatch-app issue (capability parity, not this release) |
| iPhone/iOS | ⏳ tracked via datawatch-app issue (capability parity, not this release) |

## 5. Verification

- Go unit tests: `BraveProvider` (httptest fake), `Registry` fallback
  ordering + cache hit/miss, `store.go` rollup math, REST handlers.
- End-to-end: real PRD run on the `opencode` backend exercising the
  `web_search` MCP tool through the Brave provider; confirm a usage row is
  recorded, dashboard stats reflect it, cache prevents a second identical
  paid call within the TTL window.
- `go build ./...`, `go test ./...` clean.
- Config round-trip per the Configuration Accessibility Rule (`PUT`/`GET
  /api/config`, `configure` comm-channel command).

## 6. Release sequencing

Minor release (new config shape, new endpoints, new DB file): next version
after current `8.38.1` is **`8.39.0`**. Standard release-checklist flow
applies; this plan also adds a new mandatory release-checklist item (§6):
after pushing a tag, arm a `Monitor` watching the triggered release run to
completion (not just a one-off `gh run watch`) before reporting the release
done — operator-directed 2026-10-02, codified as a standing rule so it
survives past this one release.

## 7. Status (updated 2026-10-02)

Shipped as **v8.39.0**. All Phase 1–3 scope delivered:

- `internal/websearch` (providers, cache, SQLite store, registry) — complete, 29 unit tests passing.
- `internal/mcp/search` (standalone `mcp-search` subprocess) — complete, rewritten for the registry, 12 unit tests passing.
- Daemon-side wiring (`cmd/datawatch/main.go`): registry construction after `ResolveConfig`, wired into `httpServer.SetWebSearchRegistry`, the stats-collector closure, and session-injection gating.
- REST (`internal/server/websearch.go`), including secret-leak regression tests (`internal/server/websearch_test.go`).
- CLI (`cmd/datawatch/cli_websearch.go`), MCP admin tools (`internal/mcp/websearch.go`), comm channel (`internal/router/websearch.go`) — all proxy-to-REST, matching the LLM registry's established pattern.
- PWA: Settings → Compute "Web Search Providers" card (list+form CRUD, mirrors the Remote Servers card pattern), Dashboard "Search Usage" card, and a rewritten Monitor-tab stat tile + history modal pulling from the live multi-provider stats endpoint.
- Locale keys added to all 5 bundles (en/de/es/fr/ja) per the Localization Rule; `TestLocales_CommonNavKeysPresent` extended with the new settings-card and dashboard-card titles.
- Docs: `config-reference.yaml`, `datawatch-definitions.md`, `mcp.md`, `testing-tracker.md`, `README.md`, `docs/plans/README.md` backlog (BL391 entry).
- Phase 4 (Android/iPhone parity) tracked via `dmz006/datawatch-app#205`, not implemented in this repo.

Outstanding before tag: end-to-end PRD-on-opencode verification through the Brave provider, `go build`/`go test` clean (confirmed), git commit + tag + CI monitor.
