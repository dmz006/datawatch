# BL318 — Instance-Scoped Claude Config (no $HOME pollution)

**Filed:** 2026-05-20  
**Priority:** P1 — patch release  
**Status:** 📋 queued

---

## Problem

Every datawatch daemon instance — production or test — unconditionally writes MCP
registration into the user's `$HOME`-level files on every session spawn:

- `SweepUserScopeMCPConfig` (called from `main.go:1141`) rewrites `~/.mcp.json`
- `RegisterSessionMCP` (called from `main.go:1193`) runs `claude mcp add` which
  writes into `~/.claude.json`

Because all instances share the same `$HOME`, a test daemon overwrites the production
daemon's registration and vice versa. Discovered when an e2e test daemon left running
at port 18080 replaced the production daemon's entries in `~/.claude.json` and
`~/.mcp.json`, causing this Claude Code session to silently connect to the wrong
instance.

**Files corrupted in the incident (manually restored 2026-05-20):**
- `~/.claude.json` — 2 stale MCP server entries (`dw-e2e-test-46dc`, `dw-e2e-test-83f4`)
- `~/.mcp.json` — overwritten to `http://127.0.0.1:18080` + test token
- `~/workspace/datawatch/.mcp.json` — same

---

## Root Cause

`SweepUserScopeMCPConfig` and `RegisterSessionMCP` use `os.UserHomeDir()` as the
config target, not the daemon's own data directory. There is no guard that says "only
the primary instance may write to $HOME".

---

## Fix (per-server-instance scope)

The isolation boundary should be the daemon instance, not the session. All sessions
spawned by one instance share config (accumulated permissions, MCP trust) — correct
behaviour. Two instances never collide.

### 1. Replace `SweepUserScopeMCPConfig` with instance-scoped writer

Write to `$DATAWATCH_DATA_DIR/.mcp.json` instead of `~/.mcp.json`. The production
default (`~/.datawatch/.mcp.json`) is still under `$HOME` but is instance-owned, not
globally shared.

### 2. Scope `claude mcp add` calls to `$DATAWATCH_DATA_DIR/.claude/`

Pass `CLAUDE_CONFIG_DIR=$DATAWATCH_DATA_DIR/.claude` in the env when invoking the
`claude` binary for `RegisterSessionMCP`. All MCP registrations land in the instance's
own config dir, never in `~/.claude.json`.

### 3. Keep `WriteProjectMCPConfig` as-is

Project-dir `.mcp.json` (BL109) is intentionally written to the session's project
directory for non-claude-code backends. This is correct behaviour — leave it alone.

### 4. Guard: never write to `$HOME` files from a non-default instance

As defence-in-depth: if `$DATAWATCH_DATA_DIR` != `$HOME/.datawatch`, skip any
write that would target a `$HOME`-level file and emit a warning instead.

---

## Test coverage

- Unit: `SweepUserScopeMCPConfig` must not write to `$HOME` when data dir is
  a temp path (mock `os.UserHomeDir` or parameterise the path).
- E2e smoke: start two daemon instances; confirm only their own config dirs are
  modified, neither touches the other's files or `$HOME/.claude.json`.

---

## Backend coverage

| Backend | Affected file | Fix needed |
|---------|--------------|------------|
| claude-code | `~/.claude.json` (via `claude mcp add`) | Scope to `$DATAWATCH_DATA_DIR/.claude/` via `CLAUDE_CONFIG_DIR` |
| All | `~/.mcp.json` (via `SweepUserScopeMCPConfig`) | Write to `$DATAWATCH_DATA_DIR/.mcp.json` instead |
| opencode | `<projectDir>/.mcp.json` | Already project-scoped — no change needed |
| aider, gemini, goose | `<projectDir>/.mcp.json` | Already project-scoped — no change needed |

## Files to change

- `internal/channel/mcp_config.go` — add `WriteInstanceMCPConfig(dataDir, ...)`,
  deprecate `SweepUserScopeMCPConfig`
- `internal/channel/channel.go` — thread `CLAUDE_CONFIG_DIR` into `claude mcp add`
  subprocess env
- `cmd/datawatch/main.go` — replace `SweepUserScopeMCPConfig` call (line 1141)
  with `WriteInstanceMCPConfig`; pass `CLAUDE_CONFIG_DIR` at session spawn
