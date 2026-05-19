---
docs:
  index: true
  topics: [mcp, sampling, elicitation, claude-code, council, triggers, v7]
exec_params:
  - {name: trigger, required: false, description: "Trigger name (e.g. alert_triage, morning_briefing, council_deliberation)"}
exec_steps:
  - tool: mcp__datawatch__get_version
    description: Check daemon version (sampling requires v7.1.0+)
    args: {}
    read_only: true
  - tool: mcp__datawatch__get_stats
    description: Check if a claude-code session is connected for sampling
    args: {}
    read_only: true
---
# How-to: MCP Sampling (BL302 S3)

Datawatch can request LLM completions from whichever Claude Code session is
currently connected — without spawning a new session. This is **MCP Sampling**:
the daemon sends a `sampling/createMessage` request through the channel bridge
to the MCP host (Claude Code / Claude Desktop) and gets a completion back.

## What it is

| Component | Description |
|-----------|-------------|
| `SamplingDispatcher` | Holds the last 50 sampling results; gracefully degrades when no MCP host is connected |
| `TriggerRegistry` | 5 built-in trigger names with pre-built prompt templates |
| REST `POST /api/mcp/sample` | Fire a sampling trigger or custom prompt |
| REST `GET /api/mcp/sampling-log` | Browse the last 50 sampling results |
| Config `mcp.sampling.*` | Enable/disable, max tokens, timeout |

## The 5 built-in triggers

| Trigger | Prompt purpose |
|---------|---------------|
| `alert_triage` | Analyse an alert payload and suggest next action |
| `anomaly_analysis` | Explain an observed anomaly in session or process stats |
| `morning_briefing` | Summarise last 24h activity, open sessions, pending alerts |
| `council_deliberation` | Draft a council question from a running Automaton's current decision |
| `automaton_decision` | Recommend approve / reject / revise for an Automaton story |

## Base requirements

- `datawatch start` — daemon up.
- An active claude-code or claude-desktop MCP session. Sampling is triggered by the MCP client (Claude), not called directly.
- An LLM backend configured for the session that will receive the sampling request. See [llm-registry.md](llm-registry.md) and [chat-and-llm-quickstart.md](chat-and-llm-quickstart.md).

> **Pre-conditions**: MCP sampling requires an LLM backend configured and reachable. See [llm-registry.md](llm-registry.md).

## Configuration

```yaml
mcp:
  sampling:
    enabled: true          # default true
    max_tokens: 1024
    timeout_seconds: 30
    include_system_prompt: true
```

## Send a sampling request

### REST

```bash
# Built-in trigger (daemon fills template + live data)
curl -X POST https://localhost:8443/api/mcp/sample \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"trigger":"alert_triage","context":{"alert_id":"abc123"}}'

# Custom prompt
curl -X POST https://localhost:8443/api/mcp/sample \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"What is the best backend to use for long-running code tasks?","max_tokens":512}'

# View last 50 results
curl https://localhost:8443/api/mcp/sampling-log \
  -H "Authorization: Bearer $TOKEN"
```

### MCP (from Claude Code)

Sampling is invoked by the daemon calling back through the MCP bridge — it
is not a tool you call explicitly. However you can trigger it via REST or
comm to test the integration.

### CLI

```bash
datawatch mcp sample --trigger alert_triage --context '{"alert_id":"abc"}'
datawatch mcp sample --prompt "Summarise my open sessions"
```

### Comm

```bash
!mcp sample trigger=morning_briefing
```

## View the sampling log (PWA)

Settings → MCP → **Sampling log** tab shows the last 50 results with:
- Trigger name or `custom`
- Prompt excerpt
- Response excerpt
- Timestamp
- Status (ok / error / unsupported)

The tab auto-refreshes every 30 s while open.

## Graceful degradation

When no Claude Code session with a connected MCP host is active,
`ErrSamplingNotSupported` is returned:

```json
{"error": "sampling not supported: no MCP host connected"}
```

The ring buffer still records the attempt. Callers should handle this
gracefully — it is not a daemon error, just a state condition.

## Plugin integration

Plugins can declare `sampling_triggers` in their manifest to register
additional trigger names:

```yaml
sampling_triggers:
  - name: plugin_review
    prompt_template: "Review the following plugin output: {{.Output}}"
```

## See also

- [`howto/mcp-elicitation.md`](mcp-elicitation.md) — structured input collection via MCP
- [`howto/mcp-prompts.md`](mcp-prompts.md) — 10 built-in slash commands
- [`howto/mcp-tools.md`](mcp-tools.md) — full MCP tool catalogue
- [`howto/council-mode.md`](council-mode.md) — multi-persona debate using sampling
