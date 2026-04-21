# RTK Integration Guide

[RTK (Rust Token Killer)](https://github.com/rtk-ai/rtk) is a Rust CLI proxy that
compresses AI coding agent output before it reaches the LLM's context window, reducing
token consumption by 60-90%.

## Quick Setup

```bash
# Install RTK — upstream installer (recommended)
curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh

# Initialize hooks for Claude Code
rtk init -g

# Enable in datawatch
datawatch setup rtk
# Or edit config directly:
# rtk:
#   enabled: true
#   auto_init: true
```

## Configuration

| Method | How |
|--------|-----|
| **YAML** | Edit `~/.datawatch/config.yaml` → `rtk:` section (see below) |
| **CLI** | `datawatch setup rtk` (interactive wizard) |
| **Web UI** | Settings tab → General → **RTK (Token Savings)** card |
| **REST API** | `PUT /api/config` with `{"rtk.enabled": true, "rtk.auto_init": true}` |
| **Comm channel** | `configure rtk.enabled=true`, `configure rtk.binary=/path/to/rtk` |

```yaml
rtk:
  enabled: true            # Enable RTK integration
  binary: rtk              # Path to RTK binary
  show_savings: true       # Display savings in stats dashboard
  auto_init: true          # Auto-run 'rtk init -g' on startup if hooks missing
  discover_interval: 0     # Seconds between discover checks (0 = disabled)
```

### Where to see RTK stats

| Location | What you see |
|----------|-------------|
| **Web UI** | Settings → Monitor tab → **RTK Token Savings** card (version, hooks, tokens saved, avg %, commands) |
| **REST API** | `GET /api/stats` → `rtk_installed`, `rtk_version`, `rtk_hooks_active`, `rtk_total_saved`, `rtk_avg_savings_pct`, `rtk_total_commands` |
| **Comm channel** | `stats` command includes RTK summary when enabled |
| **Prometheus** | `GET /metrics` includes RTK gauge metrics |

## Supported Backends

RTK only activates for LLM backends that support the PreToolUse hook:

| Backend | RTK Support | Notes |
|---------|-------------|-------|
| claude-code | Full | PreToolUse hook intercepts shell commands |
| gemini | Full | Same hook mechanism |
| aider | Partial | Some commands supported |
| opencode | Unknown | Not tested |
| openwebui | N/A | API-only, no shell commands |
| ollama | N/A | API-only, no shell commands |

When `auto_init: true`, datawatch only runs `rtk init -g` if the active
`session.llm_backend` is in the supported list.

## Stats Dashboard

When RTK is enabled and installed, the following metrics appear in the stats API
(`GET /api/stats`) and the web UI System Statistics dashboard:

| Metric | Description |
|--------|-------------|
| `rtk_installed` | Whether RTK binary is found |
| `rtk_version` | RTK version string |
| `rtk_hooks_active` | Whether Claude Code hooks are installed |
| `rtk_total_saved` | Total tokens saved across all sessions |
| `rtk_avg_savings_pct` | Average compression percentage |
| `rtk_total_commands` | Total commands processed by RTK |

Per-session stats (in `session_stats[]`):
| Metric | Description |
|--------|-------------|
| `rtk_saved_tokens` | Tokens saved for this session's project |
| `rtk_savings_pct` | Average savings % for this project |
| `rtk_commands` | RTK-compressed commands in this project |

## Discover API

`GET /api/rtk/discover?since=7` returns optimization suggestions — commands that
could benefit from RTK compression but aren't currently using it.

## Hook Coexistence

Both datawatch's MCP channel and RTK use Claude Code's hook system:
- **RTK**: PreToolUse hook rewrites Bash commands for compressed output
- **datawatch**: MCP channel server for structured communication

These hooks coexist without conflict. RTK's hook fires first (command rewrite), then
Claude executes the rewritten command. The MCP channel operates at a different level
(MCP protocol, not shell hooks).

**Correct setup order:**
1. `rtk init -g` — installs the PreToolUse hook
2. `datawatch start` — registers the MCP channel server

Both are stored in Claude's settings and load on every session start. No manual
hook ordering is needed — they operate independently.

## Tee Output Recovery

When RTK-proxied commands fail, full unfiltered output is saved to
`~/.local/share/rtk/tee/`. If a session has issues with compressed output,
check this directory for the raw version.
