---
docs:
  index: true
  topics: [mcp, prompts, slash-commands, channel-bridge, cli, comm, pwa]
exec_params:
  - {name: name, required: false, description: "Prompt name to render (e.g. diagnose-system)"}
exec_steps:
  - tool: docs_list_howtos
    description: List available howtos
    args: {}
    read_only: true
  - tool: mcp__datawatch__get_version
    description: Check daemon version (also available via plan-sprint prompt)
    args: {}
    read_only: true
---
# How-to: MCP Prompts (BL302 S4)

Datawatch registers 10 MCP prompts as slash commands. Each prompt injects
live resource data (sessions, alerts, stats, memory, knowledge graph, etc.)
so Claude has full context when invoked from Claude Code, Claude Desktop,
or any MCP-aware client.

## What it is

Ten focused prompts — each targeting a common operator workflow — accessible
via five surfaces:

| Surface | Access pattern |
|---------|---------------|
| MCP protocol | `prompts/list`, `prompts/get` via stdio/SSE |
| REST | `GET /api/mcp/prompts`, `POST /api/mcp/prompts/get` |
| CLI | `datawatch mcp prompts list`, `datawatch mcp prompts get <name> [key=val...]` |
| Comm | `!mcp prompts`, `!mcp prompt <name> [key=val...]` |
| PWA | Settings → MCP → Prompts panel |

## The 10 prompts

| Name | Args | Live data injected |
|------|------|-------------------|
| `analyze-session` | `session_id` (opt) | sessions/{id} + sessions/{id}/history |
| `review-automaton` | `automaton_id` (req) | automata/{id} + automata/{id}/status |
| `triage-alert` | `alert_id` (req) | alerts/{id} + stats |
| `morning-briefing` | `since` (opt) | sessions + alerts + memory/recent + stats |
| `research-topic` | `topic` (req) | memory/recent + kg/entities |
| `council-brief` | `council_id` (req) | council/{id} + council/personas |
| `session-summary` | `session_id` (req) | sessions/{id}/history |
| `diagnose-system` | — | stats + alerts + config |
| `explore-kg` | `entity` (opt) | kg/entities + kg/triples |
| `plan-sprint` | `context` (opt) | memory/recent + version |

## Usage examples

### CLI: list all prompts

```
datawatch mcp prompts list
```

Output shows name, description, and required/optional arguments.

### CLI: render a prompt

```
datawatch mcp prompts get diagnose-system
```

```
datawatch mcp prompts get analyze-session session_id=abc123
```

```
datawatch mcp prompts get morning-briefing since=2026-05-17
```

### REST: list prompts

```
GET /api/mcp/prompts
→ {"prompts":[{"name":"analyze-session","description":"...","arguments":[...]}, ...]}
```

### REST: get a rendered prompt

```
POST /api/mcp/prompts/get
Content-Type: application/json

{"name":"diagnose-system","arguments":{}}
→ {"name":"diagnose-system","description":"...","messages":[{"role":"user","content":"..."}]}
```

### Comm: list prompts

```
!mcp prompts
```

### Comm: render a prompt

```
!mcp prompt diagnose-system
!mcp prompt analyze-session session_id=abc123
```

### PWA

Open Settings → MCP → Prompts. Click **Load Prompts** to see the list.
Click **Get** on any prompt, fill in arguments, and the rendered text
appears below. Use **Copy** to copy the text to your clipboard.

## Graceful degradation

If a resource read fails (daemon unavailable, subsystem disabled, unknown
ID), the prompt replaces the data with `(data unavailable)` rather than
returning an error. You get a partial but useful prompt rather than nothing.

## Configuration

```yaml
mcp:
  prompts:
    enabled: true   # default; set false to disable all MCP prompts
```

## Stats tracking

Each `POST /api/mcp/prompts/get` call increments:
- `mcp_stats.prompt_calls_total`
- `mcp_stats.prompt_calls_by_name.<name>`

Visible in `GET /api/stats` → `mcp_stats` and the `datawatch://stats/mcp`
MCP resource.
