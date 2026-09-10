# BL372 — Web Search MCP Injection for Agent Backends

**Date:** 2026-09-10 · **Status:** Draft · **Priority:** Medium

## Problem

Autonomous opencode (and goose) sessions running research tasks time out or produce
empty results because they have no web search tool. The operator must manually edit
`~/.config/opencode/opencode.jsonc` to add a SearXNG MCP entry — this is fragile,
not version-controlled, invisible to the operator UI, and doesn't work for goose at all.

Root cause observed: PRD `8b750b08` session `acc3` hit a 5-minute Ollama first-token
timeout on the examples-catalog research task because qwen3.8:27b had no web search
and couldn't make progress.

## Goal

Make web search a first-class, operator-configurable option in datawatch that is
automatically injected into opencode and goose sessions.

## Design

### Config additions (`config_set` / API)

```yaml
web_search:
  enabled: true
  provider: searxng          # only provider for now; extend later
  url: http://datawatch:3001
  engine: bing               # comma-separated; SearXNG only returns results for bing
                             # in this deployment (brave/ddg/startpage rate-limited)
  num_results: 10            # default per query
```

### opencode injection

Datawatch already injects `extra_mcp_servers` into opencode sessions via
`hookEnv` in `manager.go`. Extend this:

1. When `web_search.enabled` is true and the session backend is opencode/opencode-acp,
   generate a per-session inline MCP server entry that calls `datawatch mcp-search`
   (see below).
2. Inject it via `OPENCODE_MCP_*` env vars or by writing a temp config fragment
   (same pattern as `CLAUDE_CONFIG_DIR` channel injection).

### goose injection

Goose already accepts `GOOSE_MCP__<NAME>__*` env vars (BL363 T3). Extend the goose
env-var injection in `manager.go` to also write the web search MCP entry when
`web_search.enabled` is true.

### `datawatch mcp-search` sub-command (new)

A new CLI sub-command that runs a stdio MCP server exposing the `web_search` tool.
It reads `web_search.*` config from the running daemon (or from env vars as fallback)
and proxies queries to the configured SearXNG URL.

**Why a native sub-command instead of the current Node.js script:**
- Ships with the datawatch binary — no Node.js dependency
- Uses Go's `net/http` — same HTTP client patterns as the rest of the codebase
- Config comes from the daemon — operator changes propagate without restarting sessions
- Testable as a regular Go unit test

### MCP framing

Same Content-Length + `\r\n\r\n` protocol. The working Node.js prototype in
`/home/dmz/.config/opencode/searxng-mcp.js` is the reference for message shapes.

## Tasks

- [ ] T1: Add `web_search` config struct + `config_set` wiring + API GET/SET
- [ ] T2: Implement `datawatch mcp-search` stdio server (Go, unit-tested)
- [ ] T3: opencode injection — add `web_search` MCP to `extra_mcp_servers` env chain
- [ ] T4: goose injection — extend `GOOSE_MCP__*` env block in manager.go
- [ ] T5: Operator UI — show web search status in Settings alongside LLM backends
- [ ] T6: Docs — add `web_search` to config-reference.yaml

## Notes

- Current SearXNG deployment at `http://datawatch:3001`: only `bing` engine works.
  brave, duckduckgo, startpage all blocked by CAPTCHA / rate limiting.
- The working Node.js MCP script lives at `/home/dmz/.config/opencode/searxng-mcp.js`
  and is already wired in `~/.config/opencode/opencode.jsonc` as a stopgap.
- Extend `provider` list later: Tavily, Brave Search API, or a bare SearXNG with
  other engines once the rate-limiting situation changes.
