# How-to: MCP tools — drive datawatch from Claude / Cursor / any MCP host

Datawatch exposes its operator surface as MCP tools — every REST
endpoint has a matching MCP tool name. Wire datawatch into your AI
client of choice and the LLM can call session_start, secrets_get,
council_run, observer_envelopes, etc. directly.

## What it is

The Model Context Protocol (MCP) is Anthropic's open spec for
LLM-to-tool wiring. Datawatch ships an MCP server that:

- Speaks stdio MCP (for claude-code MCP, Cursor, opencode-acp).
- Mirrors every REST endpoint as a typed tool.
- Audit-logs every invocation through the same path as REST + CLI.

Live tool catalogue at `https://localhost:8443/api/mcp/docs`.

## Base requirements

- `datawatch start` — daemon up.
- An MCP host (any of):
  - **claude-code MCP** — Claude Code CLI.
  - **Cursor** — IDE with MCP support.
  - **opencode-acp** — opencode's ACP protocol bridges into MCP.
  - **Custom** — anything that speaks stdio MCP.

## Setup

In `~/.datawatch/datawatch.yaml`:

```yaml
mcp:
  enabled: true                       # default: true
  per_tool_acl:                       # optional; default: every tool exposed
    secrets_get: [session:approved]
    council_run: [operator]
```

`datawatch reload` to apply.

For the MCP host, point at the daemon's stdio entry:

```json
{
  "mcpServers": {
    "datawatch": {
      "command": "datawatch",
      "args": ["mcp"],
      "env": {
        "DATAWATCH_TOKEN": "<your bearer>",
        "DATAWATCH_URL": "https://localhost:8443"
      }
    }
  }
}
```

(In claude-code: `claude mcp add datawatch -- datawatch mcp`.)

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Confirm the MCP server is reachable + lists tools.
datawatch mcp list
#  → 47 tools exposed:
#      session_start, session_get, session_list, ...
#      secrets_list, secrets_get, secrets_set, ...
#      council_personas, council_run, ...
#      observer_envelopes, observer_peers, ...

# 2. Inspect a tool's schema.
datawatch mcp docs session_start
#  → name: session_start
#    args: { backend: string, task: string, project_dir?: string,
#            profile?: string, model?: string, effort?: string }
#    returns: Session object

# 3. Invoke a tool one-shot from CLI (debugging / scripting).
datawatch mcp call session_list '{}'
#  → [ { full_id: "...", state: "running", ... }, ... ]

datawatch mcp call session_start \
  '{"backend":"ollama","task":"hello","project_dir":"/tmp"}'
```

### 4b. Happy path — PWA

The PWA isn't an MCP host (it's the operator's UI); it links to the
catalogue:

1. Settings → About → **MCP Tools** row → click `/api/mcp/docs`.
2. The catalogue page lists every tool with name + args schema +
   return shape + a "try" button.
3. Try button opens a JSON-args editor; submit invokes the tool
   against the daemon and shows the response inline. Audit-logged
   like any other call.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same `/api/mcp/docs` link in Settings → About. View-only — no
in-app MCP host.

### 5b. REST

```sh
# Catalogue.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/mcp/docs

# Invoke a tool one-shot.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"task":"hello","backend":"ollama","project_dir":"/tmp"}' \
  $BASE/api/mcp/session_start
```

REST `POST /api/mcp/<tool>` accepts the tool's args as JSON body and
returns the same response the MCP host would see. Useful as a
non-stdio path for clients that can't speak MCP directly.

### 5c. MCP — the operator-canonical surface

```jsonc
// Inside an MCP host like claude-code or Cursor:
// (operator types) "List my running datawatch sessions, then start one against ollama with task 'hello'"

// (LLM calls) session_list({})
//   → returns array of sessions
// (LLM calls) session_start({"backend":"ollama","task":"hello","project_dir":"/tmp"})
//   → returns the new session object
// (LLM responds with summary)
```

Tool names follow `<resource>_<verb>` (e.g. `session_start`,
`secrets_get`, `council_run`, `observer_envelopes`,
`autonomous_decompose`).

### 5d. Comm channel

```
You: mcp list
Bot: 47 tools available; full catalogue at https://your-host/api/mcp/docs

You: mcp call session_list {}
Bot: <returns formatted list>
```

`mcp call` is operator-gated to private chats (sensitive tools shouldn't
fire from group rooms).

### 5e. YAML

`mcp.*` block:

```yaml
mcp:
  enabled: true
  per_tool_acl:                       # default: empty = all tools open
    secrets_get:    [session:approved, cli:operator]
    secrets_set:    [cli:operator]
    council_run:    [operator]
    sessions_kill:  [cli:operator, plugin:scheduler]
  rate_limit_per_caller: 60           # invocations per minute per caller
```

ACL rules use the same scope syntax as the secrets manager
(`plugin:<name>`, `session:<state>`, `cli:operator`, `chat:<channel>`).

## Diagram

```
  ┌──────────────────────┐    ┌──────────────────────┐
  │ MCP host             │    │ MCP host             │
  │  (claude-code, Cursor│    │  (custom)            │
  │   opencode-acp)      │    │                      │
  └─────────┬────────────┘    └──────────┬───────────┘
            │ stdio MCP                  │ stdio MCP
            └──────────┬─────────────────┘
                       ▼
             ┌──────────────────────┐
             │ datawatch mcp server │
             │  (47+ tools)         │
             └──────────┬───────────┘
                        │ same audit-log + ACL as REST
                        ▼
             ┌──────────────────────┐
             │ Daemon core          │
             │ (sessions / secrets /│
             │  council / observer  │
             │  / automata / ...)   │
             └──────────────────────┘
```

## Common pitfalls

- **MCP host can't find datawatch.** Verify `command` resolves to the
  binary on the host's PATH. `which datawatch` from the same shell
  the MCP host is started in.
- **Tool calls return 401.** `DATAWATCH_TOKEN` env var missing or
  wrong. Reset via `datawatch token rotate` and update the host config.
- **ACL denies a call.** Daemon returns the denied scope set; check
  `mcp.per_tool_acl` and adjust.
- **Tool not in catalogue but in the docs.** A new tool was added;
  restart the MCP host so it re-fetches the catalogue.
- **Rate-limit hits.** Bump `rate_limit_per_caller` for known-safe
  callers (e.g. an autonomous executor that legitimately needs to
  fire many calls per minute).

## Linked references

- See also: [`secrets-manager.md`](secrets-manager.md) — secrets ACL syntax.
- See also: [`autonomous-planning.md`](autonomous-planning.md) — `autonomous_*` tools.
- See also: [`council-mode.md`](council-mode.md) — `council_*` tools.
- Architecture: `../architecture-overview.md` § MCP server.
- Live catalogue: `https://localhost:8443/api/mcp/docs`.

## Screenshots needed (operator weekend pass)

- [ ] `/api/mcp/docs` rendered tool catalogue (PWA browser view)
- [ ] claude-code with datawatch MCP host wired (`claude mcp list` output)
- [ ] Cursor with datawatch MCP host configured (settings JSON)
- [ ] An LLM-driven session_start round-trip (LLM ⇄ MCP ⇄ daemon log)
