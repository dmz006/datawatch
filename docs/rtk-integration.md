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
  write_rules_files: false # Write the RTK text into the tracked AGENT.md/CLAUDE.md instead of the untracked file (legacy; default off)
```

### Where the RTK instructions go

The RTK instructions are a short block of text that tells a session to put `rtk` in front of
its shell commands. datawatch delivers it through an **untracked per-session file**, not
through the project's rules files:

- At session start a claude-code session gets the block in `CLAUDE.local.md` in the project
  directory — the same untracked file that carries the [platform
  context](datawatch-definitions.md#platform-context). The block sits between
  `<!-- rtk-instructions -->` and `<!-- /rtk-instructions -->` marker lines; any text you wrote
  in that file outside the markers is kept.
- In a git repository datawatch first adds `CLAUDE.local.md` to the repository's
  `info/exclude` list (a per-clone ignore list that is not itself tracked). A session start
  therefore leaves no change in `git status`, and the text cannot reach a commit, including
  the commit an Automaton task makes when it finishes.
- If the project already tracks its own `CLAUDE.local.md`, datawatch refuses to write into it
  and the session does not get the block that way.
- The session record keeps `rtk_instructions_delivered: true` once the text is in place.
- With RTK turned off, the block is removed from the untracked file at the next session
  start.

**`AGENT.md` and `CLAUDE.md` are never written by the daemon by default.** Rules files are
written by people only. This matters most for Automaton work: a block appended to a tracked
rules file shows up as an uncommitted change, and the protected-paths check treats a change to
the rules file as a change that needs a decision.

### The rules-file setting

`rtk.write_rules_files` controls whether the daemon writes the block into the tracked
rules files instead of the untracked file. **The default is `false` (off).** Turn it on only if you want the old behaviour,
where the block is appended to the project's own `AGENT.md` (or `CLAUDE.md`) at session start
and the untracked file is not used.

| Surface | How |
|---------|-----|
| **YAML** | `rtk.write_rules_files: true` in `~/.datawatch/config.yaml` |
| **Web UI** | Settings → General → **RTK (Token Savings)** → *Write RTK instructions into AGENT.md and CLAUDE.md* (the help text beside it says the default is off) |
| **REST API** | `PUT /api/config` with `{"rtk.write_rules_files": true}` |
| **CLI** | `datawatch config set rtk.write_rules_files true` |
| **MCP** | `config_set` with `rtk.write_rules_files` |
| **Comm channel** | `configure rtk.write_rules_files=true` |

### Startup cleanup of an old block

Older versions appended the block to the tracked rules file at every session start, so some
worktrees still carry it as an uncommitted change. At daemon start a cleanup pass looks at the
project directory of each stored session and checks `AGENT.md` and `CLAUDE.md` for an
`rtk-instructions` block. It removes the block **only when the block is the sole difference
from the committed file**. All of these must hold:

1. the file is tracked by git;
2. `git status` shows one plain unstaged modification for it (nothing staged);
3. the file without the block is byte-identical to the committed version
   (`git show HEAD:<file>`).

When they hold, the pass writes the stripped file back, which restores the committed version,
and logs the removal. In every other case — the file is untracked, the block is committed, the
file has other uncommitted changes, or git cannot answer — the file is **left exactly as it
is** and the pass raises **one warning alert per worktree** that names the file or files and
the reason, so you decide whether to remove the block by hand. The pass does nothing while
`rtk.write_rules_files` is on, because the block is then meant to be there.

### What to review after upgrading

- **Leftover blocks.** A worktree whose `AGENT.md` or `CLAUDE.md` raised the cleanup warning alert still carries the block. Remove it by hand, or commit it if you meant to keep it.
- **Committed blocks.** The cleanup never rewrites a block that is already committed; remove those in a normal commit.
- **Legacy setting.** If your `config.yaml` sets `rtk.write_rules_files: true`, the daemon keeps writing into the tracked rules files. Confirm that is what you want.
- **Tracked `CLAUDE.local.md`.** A project that tracks its own `CLAUDE.local.md` gets no RTK block through the untracked file.

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
