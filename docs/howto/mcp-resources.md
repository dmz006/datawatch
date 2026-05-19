---
docs:
  index: true
  topics: [mcp, resources, resource-templates, channel-bridge, cli, comm]
exec_params:
  - {name: uri, required: false, description: "datawatch:// URI to read (e.g. datawatch://version)"}
exec_steps:
  - tool: docs_list_howtos
    description: List available howtos (uses datawatch://docs resource internally)
    args: {}
    read_only: true
  - tool: mcp__datawatch__get_version
    description: Check daemon version (also available at datawatch://version)
    args: {}
    read_only: true
---
# How-to: MCP Resources (BL302 S1+S2)

Datawatch exposes structured read-only data through MCP resources — a
standard protocol surface that MCP-aware AI clients (Claude Desktop,
Claude Code, etc.) can browse and read without calling explicit tools.

> **Pre-conditions**: Some MCP resources expose data from subsystems that must be configured first:
> - Council persona resources — requires personas to be set up. See [council-mode.md](council-mode.md).
> - Memory and KG resources — requires memory backend to be configured. See [cross-agent-memory.md](cross-agent-memory.md).
> - Automata resources — requires Automata to exist. See [autonomous-planning.md](autonomous-planning.md).

## What it is

Thirteen static resources, nine live resources, and eight URI-template
resources — 21 total — all accessible via three surfaces:

| Surface | Access pattern |
|---------|---------------|
| MCP protocol | `resources/list`, `resources/read` via stdio/SSE |
| REST loopback | `GET /api/mcp/resources`, `/api/mcp/resources/read?uri=…` |
| CLI | `datawatch mcp resources list|templates|read <uri>` |
| Comm | `!mcp resources`, `!mcp templates`, `!mcp read <uri>` |
| PWA | Settings → MCP → Resources tab (categorized view) |

## Static resources (S1)

| URI | Description |
|-----|-------------|
| `datawatch://version` | Daemon version and build metadata |
| `datawatch://config` | Current running configuration (secrets redacted) |
| `datawatch://channel/info` | Active MCP channel info |
| `datawatch://docs` | List of all available howto documents |

## Live resources (S2)

These resources return real-time daemon state. When a subsystem is
unavailable (e.g. memory not configured, KG not enabled) the resource
returns a graceful empty JSON object rather than an error.

### Sessions & Stats

| URI | Description |
|-----|-------------|
| `datawatch://sessions` | All active and recent sessions (array wrapped in `{sessions:[…]}`) |
| `datawatch://stats` | Full daemon stats snapshot |
| `datawatch://stats/mcp` | MCP-specific stats block (`mcp_stats` sub-object) |

### Alerts

| URI | Description |
|-----|-------------|
| `datawatch://alerts` | All active alerts (`{alerts:[…]}`) |

### Memory

| URI | Description |
|-----|-------------|
| `datawatch://memory/recent` | Last 20 memory entries (`{entries:[…]}`) |

### Automata

| URI | Description |
|-----|-------------|
| `datawatch://automata` | All automata/PRDs (`{automata:[…]}`) |

### Council

| URI | Description |
|-----|-------------|
| `datawatch://council/personas` | Registered council personas |

### Knowledge Graph

| URI | Description |
|-----|-------------|
| `datawatch://kg/entities` | KG entity statistics and overview |
| `datawatch://kg/triples` | Recent KG WAL entries (up to 50) |

## Resource templates (S1)

Parameterized URIs resolved at read time:

| URI template | Description |
|-------------|-------------|
| `datawatch://docs/{path}` | Read a specific howto by path |
| `datawatch://sessions/{id}` | Session detail |
| `datawatch://sessions/{id}/history` | Session message history |
| `datawatch://automata/{id}` | Automaton (PRD) detail |
| `datawatch://automata/{id}/status` | Automaton execution status |
| `datawatch://memory/{id}` | Single memory entry by ID |
| `datawatch://alerts/{id}` | Alert detail by ID |
| `datawatch://council/{id}` | Council run detail by ID |

## Reading a resource via CLI

```bash
# List all resources (static + live)
datawatch mcp resources list

# List URI templates
datawatch mcp resources templates

# Read static resources
datawatch mcp resources read datawatch://version
datawatch mcp resources read datawatch://docs/mcp-resources.md

# Read live resources
datawatch mcp resources read datawatch://sessions
datawatch mcp resources read datawatch://stats
datawatch mcp resources read datawatch://alerts
datawatch mcp resources read datawatch://memory/recent
datawatch mcp resources read datawatch://automata
datawatch mcp resources read datawatch://council/personas
datawatch mcp resources read datawatch://kg/entities
datawatch mcp resources read datawatch://kg/triples

# Read parameterized resources
datawatch mcp resources read datawatch://sessions/abc123
datawatch mcp resources read datawatch://sessions/abc123/history
datawatch mcp resources read datawatch://alerts/42
```

## Reading a resource via comm

```
!mcp resources
!mcp templates
!mcp read datawatch://version
!mcp read datawatch://sessions
!mcp read datawatch://stats
!mcp read datawatch://alerts
!mcp read datawatch://memory/recent
```

## Configuration

Resources are enabled by default. To disable:

```yaml
mcp:
  resources:
    enabled: false
```

## Channel bridge forwarding

When a downstream MCP client connects via `datawatch-channel`, it
automatically discovers all resources (static + live) from the daemon
and forwards them. The bridge calls `GET /api/mcp/resources` and
`GET /api/mcp/resources/templates` at startup, then registers
forwarders that proxy `resources/read` calls to
`GET /api/mcp/resources/read?uri=<resolved-uri>`.

On startup, the bridge logs:
```
[datawatch-channel] discovered 21 resources
[datawatch-channel] discovered 8 resource templates
```

## PWA — categorized resource panel

The Settings → MCP → Resources tab groups resources by category and
shows the count per category:

- **System** — version, config, channel/info
- **Docs** — docs, docs/{path} template
- **Sessions** — sessions, sessions/{id}, sessions/{id}/history
- **Stats & Alerts** — stats, stats/mcp, alerts, alerts/{id}
- **Memory** — memory/recent, memory/{id}
- **Automata** — automata, automata/{id}, automata/{id}/status
- **Council** — council/personas, council/{id}
- **Knowledge Graph** — kg/entities, kg/triples

## Tool annotations

MCP tools are annotated with read-only / destructive hints so
conforming clients can display appropriate UI cues:

- `memory_recall`, `kg_query`, `research_sessions`, `get_prompt`,
  `copy_response` — `readOnlyHint: true`
- `memory_remember`, `kg_add` — `readOnlyHint: false`,
  `destructiveHint: false`
