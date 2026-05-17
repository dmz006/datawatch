# MCP Prompts

datawatch exposes 10 focused prompts as MCP slash commands. Each prompt injects live resource data so the connected AI has full context when invoked.

## Available prompts

| Name | Required args | Description |
|------|--------------|-------------|
| `analyze-session` | — | Summarize session activity and recommend next steps |
| `review-automaton` | `automaton_id` | Review Automaton config + execution status |
| `triage-alert` | `alert_id` | Assess severity, root cause, and action for an alert |
| `morning-briefing` | — | Daily overview: sessions, alerts, memory, stats |
| `research-topic` | `topic` | Research a topic using memory and knowledge graph |
| `council-brief` | `council_id` | Brief a council run with personas |
| `session-summary` | `session_id` | Summarize session channel history |
| `diagnose-system` | — | Full daemon health check (stats + alerts + config) |
| `explore-kg` | — | Explore knowledge graph entities and triples |
| `plan-sprint` | — | Plan next sprint from memory and version context |

## Surfaces

### MCP (Claude Code slash commands)

When Claude Code has the `datawatch` MCP server configured, prompts appear as `/mcp__datawatch__<prompt-name>`. Arguments are passed interactively.

### REST API

```bash
# List all prompts
curl https://localhost:8443/api/mcp/prompts

# Get rendered prompt
curl -X POST https://localhost:8443/api/mcp/prompts/get \
  -H 'Content-Type: application/json' \
  -d '{"name":"diagnose-system","arguments":{}}'
```

### Comm (Telegram / comm bridge)

```
!mcp prompts
!mcp prompt diagnose-system
!mcp prompt analyze-session session_id=abc123
!mcp prompt research-topic topic=memory+search
```

### CLI

```bash
datawatch mcp prompts list
datawatch mcp prompts get diagnose-system
datawatch mcp prompts get analyze-session --arg session_id=abc123
```

### PWA

Settings → MCP → Prompts panel: click **Load Prompts**, then **Get** next to any prompt.

## exec_steps

```yaml
exec_steps:
  - tool: mcp__datawatch__diagnose-system
    description: Check daemon health
  - tool: mcp__datawatch__morning-briefing
    description: Daily briefing
    args:
      since: "2026-05-17"
  - tool: mcp__datawatch__analyze-session
    description: Analyze a specific session
    args:
      session_id: "<session-id>"
```
