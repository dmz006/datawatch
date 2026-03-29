# Interface Testing Tracker

This document tracks the validation status of all datawatch interfaces and communication channels.

Last updated: 2026-03-29 (v0.6.18)

## Messaging Backends

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| Signal                | Yes    | Yes       | signal-cli jsonRpc, linked device, group messaging | Send/receive commands, list, help, alerts. Async send fix (30s timeout). Reconnect on signal-cli death. |
| Telegram              | No     | No        | —               | Not validated yet       |
| Discord               | No     | No        | —               | Not validated yet       |
| Slack                 | No     | No        | —               | Not validated yet       |
| Matrix                | No     | No        | —               | Not validated yet       |
| Twilio SMS            | No     | No        | —               | Not validated yet       |
| ntfy                  | No     | No        | —               | Not validated yet       |
| Email (SMTP)          | No     | No        | —               | Not validated yet       |
| GitHub Webhook        | No     | No        | —               | Not validated yet       |
| Generic Webhook       | Yes    | Yes       | HTTP POST to :9002 | Webhook router receives and processes commands. |
| DNS Channel           | No     | No        | —               | Not implemented — research/planning only (see docs/covert-channels.md) |

## Web and API Interfaces

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| Web UI                | Yes    | Yes       | Chrome/Firefox, PWA mode, localhost:8080 | Session list, detail, create, kill, restart, alerts tabs, settings, saved commands, quick input buttons, channel tab, connection banner, fsnotify live output. |
| REST API              | Yes    | Yes       | curl, /api/sessions, /api/config, /api/backends, /api/command | Session CRUD, config GET/PUT, backend list with version cache, command routing. |
| WebSocket             | Yes    | Yes       | Browser WS, session subscribe, output streaming | Live output, session state changes, alert broadcast, channel_ready events, needs_input. |

## MCP Interfaces

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| MCP stdio             | Yes    | Yes       | Per-session MCP channel servers (datawatch-{sessionID}) | Random port per session, channel_ready callback, multi-session support. npm deps auto-installed. |
| MCP SSE               | No     | No        | —               | Not validated yet       |

## LLM Backends

| Backend               | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| claude-code           | Yes    | Yes       | Claude Code v2.1.87, channel mode, skip_permissions | Per-session MCP, consent prompt detection (1s fast path), channel tab, MCP auto-retry. Trust/channels prompts require manual Enter via tmux input. |
| opencode              | Yes    | Yes       | opencode 1.3.5, interactive TUI mode | TUI uses alternate screen mode — web UI output shows minimal text. Binary auto-detected from ~/.opencode/bin/. Empty task starts TUI (no -p ''). |
| opencode-acp          | Yes    | Yes       | opencode 1.3.5 serve mode, HTTP/SSE | Fixed parts[] message format. SSE delta accumulation for responses. Status: thinking/processing/ready/done. "awaiting input" triggers waiting_input state. |
| shell                 | Yes    | Yes       | bash interactive, empty task | Starts $SHELL in project dir. $ prompt detected for waiting_input. Empty task no longer passes '' as argument. |
| aider                 | No     | No        | —               | Not validated yet       |
| goose                 | No     | No        | —               | Not validated yet       |
| gemini                | No     | No        | —               | Not validated yet       |
| opencode-prompt       | Yes    | Yes       | opencode 1.3.5, `opencode run '<task>'` | Single-prompt mode: runs task and exits. prompt_required enforced in web UI. Uses DATAWATCH_COMPLETE detection. |
| ollama                | Yes    | Yes       | Remote ollama (Gemma3:12b) at datawatch:11434 | Interactive mode with `>>> ` prompt detection. Empty task starts chat. OLLAMA_HOST env for remote. Response streamed char-by-char with ANSI. |
| openwebui             | Yes    | Yes       | OpenWebUI at datawatch:3000, OpenAI-compatible API | prompt_required enforced. Model dropdown from /api/openwebui/models. Test & Load Models button. Empty task blocked. |

## Session Management

| Feature               | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| PID lock              | Yes    | Yes       | Multiple start attempts | Prevents duplicate daemons. Stale PIDs cleaned up. |
| fsnotify monitoring   | Yes    | Yes       | Linux inotify | Interrupt-driven output processing. Falls back to 50ms polling. |
| Prompt detection      | Yes    | Yes       | Claude consent, shell $, opencode-acp ready | 1s fast path for prompt patterns, 10s full idle timeout. |
| MCP auto-retry        | Yes    | Yes       | "MCP server failed" detection | Sends /mcp + Enter, configurable limit (mcp_max_retries=5). |
| Session guardrails    | Yes    | Yes       | CLAUDE.md for claude-code, AGENT.md for others | Skips CLAUDE.md when AGENT.md exists in project dir. |
| Alerts                | Yes    | Yes       | All state transitions + session start | Active/Inactive tabs, per-session sub-tabs, quick-reply buttons. |
