---
docs:
  index: true
  topics: [mcp, elicitation, forms, approval, v7]
exec_params:
  - {name: schema, required: false, description: "Schema name: approval | text_input | choice"}
exec_steps:
  - tool: mcp__datawatch__get_version
    description: Check daemon version (elicitation requires v7.1.0+)
    args: {}
    read_only: true
---
# How-to: MCP Elicitation (BL302 S3)

**MCP Elicitation** lets the daemon prompt the operator for structured input
through whatever MCP-aware client is connected (Claude Code, Claude Desktop,
etc.) — returning a typed response without the operator leaving their AI tool.

Use it when an Automaton needs a decision, a plugin needs a parameter, or the
daemon needs operator confirmation before an irreversible action.

## What it is

| Component | Description |
|-----------|-------------|
| `ElicitationDispatcher` | Sends `elicitation/create` through the MCP channel bridge |
| 3 built-in schemas | `approval`, `text_input`, `choice` |
| REST `POST /api/mcp/elicit` | Trigger an elicitation and block until response |
| Config `mcp.elicitation.*` | Enable/disable, timeout |

## The 3 built-in schemas

### `approval`

A yes / no confirmation dialog.

```json
{
  "schema": "approval",
  "prompt": "Approve deletion of 12 stale sessions?",
  "context": {"count": 12, "oldest_id": "abc1"}
}
```

Response: `{"approved": true|false, "reason": "..."}`

### `text_input`

Free-text entry from the operator.

```json
{
  "schema": "text_input",
  "prompt": "Enter a name for this new Automaton:",
  "placeholder": "e.g. refactor-auth-module"
}
```

Response: `{"value": "operator-supplied-text"}`

### `choice`

Pick one from a list.

```json
{
  "schema": "choice",
  "prompt": "Which LLM should handle this decomposition?",
  "options": ["ollama (fast)", "claude-sonnet-4-6 (thorough)", "skip"]
}
```

Response: `{"choice": "ollama (fast)", "index": 0}`

## Configuration

```yaml
mcp:
  elicitation:
    enabled: true          # default true
    timeout_seconds: 120   # operator has 2 minutes to respond
```

## Send an elicitation request

### REST

```bash
# Approval
curl -X POST https://localhost:8443/api/mcp/elicit \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"schema":"approval","prompt":"Allow this Automaton to push to main?","context":{"automaton_id":"abc"}}'

# Choice
curl -X POST https://localhost:8443/api/mcp/elicit \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"schema":"choice","prompt":"Select effort level","options":["quick","normal","thorough"]}'
```

The call blocks until the operator responds or the timeout expires.
On timeout: `{"error": "elicitation timeout"}`

### CLI

```bash
datawatch mcp elicit --schema approval --prompt "Proceed with deploy?"
datawatch mcp elicit --schema choice --prompt "Pick backend" --options "ollama,claude-code"
```

### Comm

```bash
!mcp elicit schema=approval prompt="Approve PRD decompose?"
```

## How it appears to the operator

When an elicitation fires, Claude Code (or the connected MCP host) surfaces
it as a native form — the form type depends on the schema:

- `approval` → a yes/no prompt with optional reason field
- `text_input` → an editable text box
- `choice` → a radio button or dropdown

The operator responds inside their AI tool; the response is sent back through
the bridge to the daemon, which unblocks the waiting handler.

## Plugin integration

Plugins can trigger elicitations from their subprocess by calling
`POST /api/mcp/elicit` with the daemon's API token. The plugin can then
act on the response before returning its own result.

## Graceful degradation

When no MCP host is connected:
```json
{"error": "elicitation not supported: no MCP host connected"}
```

Autonomous workflows that use elicitation should handle this and fall back
to a default choice (e.g. approve automatically in unattended mode) or
pause for manual intervention.

## See also

- [`howto/mcp-sampling.md`](mcp-sampling.md) — LLM completions routed through MCP host
- [`howto/mcp-prompts.md`](mcp-prompts.md) — 10 built-in slash commands with live context
- [`howto/autonomous-review-approve.md`](autonomous-review-approve.md) — Automaton approval gates
