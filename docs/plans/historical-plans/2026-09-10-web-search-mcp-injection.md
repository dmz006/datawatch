# BL372 — Web Search MCP Injection for Agent Backends

**Date:** 2026-09-10 · **Version:** v8.22.0 · **Status:** Completed · **Priority:** Medium

## Problem

Autonomous opencode (and goose) sessions running research tasks time out or produce
empty results because they have no web search tool. The operator must manually edit
the opencode config file to add a SearXNG MCP entry — this is fragile, not
version-controlled, invisible to the operator UI, and doesn't work for goose at all.

Root cause observed: a PRD research session hit a 5-minute Ollama first-token timeout
because the model had no web search tool and couldn't make progress on an
internet-research task.

## Goal

Make web search a first-class, operator-configurable option in datawatch that is
automatically injected into opencode and goose sessions, with a companion skill that
tells the model how to use the tool effectively.

## Scope

Files/packages affected:

- `internal/config/config.go` — new `WebSearchConfig` struct
- `internal/server/api.go` — `handleGetConfig` / `applyConfigPatch` for new fields
- `internal/server/web/app.js` — Settings card (LLM_CONFIG_FIELDS or new Web Search section)
- `internal/server/web/locales/en.json` (+ de/es/fr/ja) — new locale keys
- `internal/session/manager.go` — `hookEnv` extension for opencode; `GooseEnv` extension
- `cmd/datawatch/main.go` — register new `mcp-search` sub-command
- `internal/mcp/search/` — new package: stdio MCP server + SearXNG proxy
- `internal/stats/collector.go` — `WebSearchStats` fields
- `docs/config-reference.yaml` — new `web_search.*` fields
- `docs/testing-tracker.md` — new section for `datawatch mcp-search` interface
- `docs/skills/web-search.md` (or via skills registry) — built-in guidance skill

## Design

### Config additions

```yaml
web_search:
  enabled: true
  provider: searxng          # only provider for now; extend later (Tavily, Brave API)
  url: http://searxng.example.com:3001   # operator-configured SearXNG instance
  engine: bing               # comma-separated engine list passed to SearXNG
  num_results: 10            # default results per query (1–20)
```

**All six access channels** (Configuration Accessibility Rule):
| Channel | How |
|---------|-----|
| YAML | `web_search:` block in `~/.datawatch/config.yaml` |
| REST API | `PUT /api/config {"key":"web_search.enabled","value":true}` etc. |
| Web UI | Settings → LLM (or General) → **Web Search** card |
| Comm channel | `configure web_search.enabled=true` |
| MCP | `config_set` tool with `web_search.*` keys |
| CLI | `datawatch config set web_search.enabled true` |

### opencode injection

Datawatch already injects `extra_mcp_servers` into opencode sessions via `hookEnv`
in `manager.go`. When `web_search.enabled` is true and the backend is
opencode/opencode-acp:

1. Append a `datawatch mcp-search` MCP server entry to the injected config
   (same mechanism as the existing datawatch MCP channel injection).
2. Also inject the built-in web-search skill (see Skills section below).

### goose injection

Goose accepts `GOOSE_MCP__<NAME>__*` env vars (BL363 T3). Extend the goose env-var
block in `manager.go` to add the web search MCP entry when `web_search.enabled`.

### `datawatch mcp-search` sub-command (new)

A CLI sub-command that runs as a stdio MCP server exposing the `web_search` tool.
Reads `web_search.*` config from the running daemon (or env vars as fallback) and
proxies queries to the configured SearXNG URL.

**Why a native sub-command instead of an external script:**
- Ships with the datawatch binary — no Node.js or other runtime dependency
- Uses Go `net/http` — consistent with the rest of the codebase
- Config comes from the daemon — operator changes propagate without session restart
- Testable as standard Go unit tests

### Skills hook (Skills-Awareness Rule, B10)

Two levels:

1. **Injection level**: N/A — this is platform plumbing (MCP server wiring).
2. **Usage level**: **Yes — built-in guidance skill required.** A skill doc
   (`web-search-guidance`) should be auto-injected into sessions alongside the MCP
   server. It tells the model: which engine is configured, result count limits,
   how to formulate effective queries, and that fallback engines (ddg, brave,
   startpage) may return zero results in deployments with rate-limited SearXNG
   instances. Without this, a capable model may still produce empty results by
   querying an unavailable engine or misreading the result format.

### Monitoring (Monitoring & Observability Rule)

- `SystemStats.WebSearch` struct: `Enabled bool`, `QueriesTotal int64`,
  `QueriesError int64`, `LastQueryAt time.Time`, `Provider string`
- `GET /api/web_search/stats` endpoint
- `web_search_stats` MCP tool
- Monitor tab card: enabled/disabled, query count, last query time
- Comm channel: `web search stats` (or via `stats` aggregation)
- Prometheus: `datawatch_web_search_queries_total`, `datawatch_web_search_errors_total`

## Phases

| # | Phase | Status |
|---|-------|--------|
| T1 | `WebSearchConfig` struct + `config_set` wiring + API GET/SET (all 6 channels) | Planned |
| T2 | `internal/mcp/search/` package: stdio MCP server + SearXNG proxy, unit tests | Planned |
| T3 | opencode injection: append `web_search` MCP entry in `hookEnv` | Planned |
| T4 | goose injection: extend `GOOSE_MCP__*` env block in manager.go | Planned |
| T5 | Web UI: Web Search settings card + locale keys (en/de/es/fr/ja) | Planned |
| T6 | Built-in guidance skill auto-injected with the MCP server | Planned |
| T7 | Monitoring: `WebSearchStats` + `/api/web_search/stats` + MCP tool + Monitor card | Planned |
| T8 | Docs: `web_search.*` in config-reference.yaml + testing-tracker.md section | Planned |

## Notes

- **Engine availability**: at writing, `bing` is the only engine returning results in
  the reference SearXNG deployment. brave, duckduckgo, and startpage are rate-limited
  or CAPTCHA-blocked. The default `engine` config value should be `bing`.
- **Stopgap**: a Node.js MCP script is already wired into the opencode config as a
  temporary measure. Remove it from docs/plan references and operator guidance once
  T2–T3 ship.
- **Provider roadmap**: extend later to Tavily, Brave Search API, or additional
  SearXNG engines once rate-limiting resolves.
- **Secrets-Store Rule**: `web_search.url` is a URL (no credentials). If a future
  provider (Tavily, Brave API) requires an API key, apply the `${secret:...}` pattern
  at that time.
