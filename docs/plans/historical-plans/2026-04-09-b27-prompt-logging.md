# B27: Alert Prompt Logging

**Date:** 2026-04-09
**Priority:** medium
**Effort:** 3-4 hours

---

## Problem

When a session transitions running → waiting_input, alerts and comm channel
messages show the LLM's response (what it said) but NOT the user's prompt
(what was asked). Users see "waiting for input" with the response context but
can't tell what prompt triggered that response.

Additionally, when users type directly in the tmux terminal or use the
interactive web UI terminal, the prompt may not be captured at all.

## Input Path Analysis

There are 3 input paths. Each captures LastInput differently:

| Path | How input arrives | LastInput set? | Where |
|------|------------------|----------------|-------|
| **Web UI / Comm channel** | `SendInput()` called | Yes, truncated 100 chars | manager.go:1074 |
| **MCP channel** | `SendInput()` via MCP tool | Yes | manager.go:1074 |
| **Direct tmux typing** | `SendRawKeys()` char-by-char | Yes (on Enter) via rawInputBuf | manager.go:996 |

All three paths DO set `sess.LastInput`. The problem is that the **alert body
construction** (main.go:1900-1947) and **remote alert bundler** (main.go:1695-1810)
use `LastResponse`/`PromptContext` for the body, not `LastInput`.

## What Currently Shows Where

| Event | Alert body | Comm channel thread | Missing |
|-------|-----------|---------------------|---------|
| running → waiting_input | LastResponse (AI output) | LastResponse (AI output) | User's prompt |
| waiting_input (needs_input) | PromptContext (screen) | PromptContext + response | User's prompt |
| State change event | "input: {LastInput}" | "input: {LastInput}" | Nothing — this works |

The state-change event text includes `LastInput` at main.go:1881, but this is
just the event description (e.g., "input: yes"), not the alert body.

## Solution

### Phase 1: Include prompt in alert body

Modify the alert body construction to prepend `LastInput` when available:

```
Prompt: <what user asked>
---
<LLM response / context>
```

**Files:**
- `cmd/datawatch/main.go` — NeedsInputHandler (line ~1900): prepend `sess.LastInput`
  to the alert body
- `cmd/datawatch/main.go` — bundleRemoteAlert (line ~1754): include `LastInput` in
  the bundled message header

### Phase 2: Add `prompt` command (like `copy` for prompts)

Add a `prompt` or `last-prompt` command that returns the last user input for a
session, mirroring the `copy` command for responses:

- Router: `CmdPrompt` — returns `sess.LastInput`
- API: `GET /api/sessions/prompt?id=<id>`
- MCP: `get_prompt` tool
- Web: show in response viewer alongside copy

### Phase 3: Capture prompts via MCP channel

For claude-code sessions using the MCP channel, Claude could expose the last
user prompt through the channel. This would require:

- A file similar to `/tmp/claude/response.md` for the prompt
- OR: capture from the Claude Code transcript JSONL (if available)
- OR: capture from the MCP channel's `send_input` tool call (already sets LastInput)

The MCP channel path already works — `send_input` calls `SendInput()` which
sets `LastInput`. The issue is only with direct tmux typing.

### Phase 4: Copy via MCP channel consideration

Currently `copy` reads `/tmp/claude/response.md` or falls back to tmux tail.
Should copy go through the MCP channel instead?

**Assessment:** No — the current approach is better because:
1. `/tmp/claude/response.md` gives the actual structured response
2. MCP channel doesn't have a "get last response" tool in Claude Code
3. The tmux fallback handles non-claude backends
4. Adding an MCP-channel-based copy would require Claude Code changes

## Testing

- Unit: verify alert body includes "Prompt: ..." when LastInput is set
- API: send input via /api/test/message, check alert body format
- Comm: verify Signal/Telegram thread shows prompt + response
- Web: verify alert card shows prompt text
- MCP: verify `get_prompt` tool returns LastInput
- Direct tmux: type in tmux, verify rawInputBuf captures and LastInput is set
