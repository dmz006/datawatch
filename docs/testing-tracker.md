# Interface Testing Tracker

This document tracks the validation status of all datawatch interfaces and communication channels.

**Testing levels:**
- **Tested=Yes**: Go unit/integration tests exist and pass (`go test`)
- **Validated=Yes**: Live end-to-end connection confirmed (real client, real session activity observed)

Last updated: 2026-03-29 (v0.7.0)

## Messaging Backends

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| Signal                | Yes    | Yes       | signal-cli v0.14.1 jsonRpc, linked device, group messaging on real phone | Live: sent "list" from phone Signal app, received session list in group. Sent "help", received help text. State change notifications delivered to Signal group. Async send fix (30s timeout). Reconnect on signal-cli death tested by killing process. |
| Telegram              | No     | No        | —               | Not validated yet       |
| Discord               | No     | No        | —               | Not validated yet       |
| Slack                 | No     | No        | —               | Not validated yet       |
| Matrix                | No     | No        | —               | Not validated yet       |
| Twilio SMS            | No     | No        | —               | Not validated yet       |
| ntfy                  | No     | No        | —               | Not validated yet       |
| Email (SMTP)          | No     | No        | —               | Not validated yet       |
| GitHub Webhook        | No     | No        | —               | Not validated yet       |
| Generic Webhook       | Yes    | Yes       | HTTP POST to :9002/task via curl | Live: `curl -X POST localhost:9002/task -d '{"task":"list --active"}'` → `{"ok":true}`. Daemon log confirms: `[webhook] Received: "list --active"`, `"version"`, `"help"` all processed by router. Webhook is inbound-only (Send is no-op), responses go to other backends. |
| DNS Channel           | Yes    | Yes       | miekg/dns v1.1.72, server on 127.0.0.1:15353, external dig client | Live: `dig @127.0.0.1 -p 15353 TXT <encoded-query>` returned 3 fragmented TXT records. Decoded response: full session list with 60 sessions, HR separators, backend names. HMAC auth verified. Unit: 15 tests (86% coverage). |

## Web and API Interfaces

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| Web UI                | Yes    | Yes       | Chrome/Firefox, PWA mode, localhost:8080 | Live: session list, detail, create, kill, restart observed in browser. Alerts tabs, settings toggles, saved commands, quick input buttons (y/n/Enter/Up/Down/Esc), channel tab, connection banner all interacted with manually. Output streaming confirmed live via fsnotify. |
| REST API              | Yes    | Yes       | curl from CLI to localhost:8080 | Live: /api/sessions/start creates sessions, /api/config GET/PUT saves and reads, /api/backends returns version cache, /api/command routes commands. All confirmed with curl during development. |
| WebSocket             | Yes    | Yes       | Browser WS connection to ws://localhost:8080/ws | Live: session subscribe delivers output lines in real-time. State change events update UI. Alert broadcasts trigger badge. channel_ready events dismiss banners. needs_input triggers quick buttons. All observed in browser DevTools. |

## MCP Interfaces

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| MCP stdio             | Yes    | Yes       | Per-session MCP channel servers (datawatch-{sessionID}) | Live: claude mcp list shows Connected status. Channel ready callback fires, channel tab appears. Messages sent via channel tab reach claude. Per-session random ports confirmed. npm deps auto-installed via corepack shim. |
| MCP SSE               | No     | No        | —               | Not validated yet       |

## LLM Backends

| Backend               | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| claude-code           | Yes    | Yes       | Claude Code v2.1.87, channel mode, skip_permissions | Live: session created via web UI, trust prompt detected in ~4s, Enter sent via quick button, channel connected ("Listening for channel messages" in tmux), messages exchanged via channel tab. MCP auto-retry observed after "MCP server failed". |
| opencode              | Yes    | Yes       | opencode 1.3.5, interactive TUI mode | Live: session started, TUI displayed in tmux capture-pane. Binary auto-detected from ~/.opencode/bin/. "Ask anything..." prompt visible. Alternate screen mode limits web UI output display. |
| opencode-acp          | Yes    | Yes       | opencode 1.3.5 serve mode, HTTP/SSE, remote ollama | Live: session started, SSE stream connected, sent "what is 5+5?" via web UI, received "10" response in output log. Status messages: thinking/processing/ready/done/awaiting input all observed. Delta accumulation confirmed for multi-token responses. |
| shell                 | Yes    | Yes       | bash interactive, empty task, /home/dmz/Desktop | Live: session started, bash prompt appeared in tmux. $ prompt detected → waiting_input state → quick buttons visible. Typed commands executed in shell. |
| aider                 | No     | No        | —               | Not validated yet       |
| goose                 | No     | No        | —               | Not validated yet       |
| gemini                | No     | No        | —               | Not validated yet       |
| opencode-prompt       | Yes    | Yes       | opencode 1.3.5, `opencode run '<task>'` | Live: session created with task "what is 1+1?", opencode ran, DATAWATCH_COMPLETE detected, session marked complete. --print-logs flag added for status output. prompt_required enforced in web UI dropdown. |
| ollama                | Yes    | Yes       | Remote ollama (Gemma3:12b) at datawatch:11434 | Live: session started, `>>> Send a message` prompt detected → waiting_input. Sent "what is 5+5?" via web UI, received "10" in tmux output. OLLAMA_HOST env correctly set for remote. Response streamed char-by-char. |
| openwebui             | Yes    | Yes       | OpenWebUI at datawatch:3000, qwen3-coder-next:q4_K_M via Ollama | Live: session created with task "what is 2+2?", curl streamed API response, DATAWATCH_COMPLETE detected, session marked complete. Direct curl test confirmed streaming works (data: chunks with delta.content). prompt_required enforced. Model dropdown populated from /api/openwebui/models. |

## Session Management

| Feature               | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| PID lock              | Yes    | Yes       | Live: attempted `datawatch start` while running → "already running" error. Stale PID cleaned up after kill. |
| fsnotify monitoring   | Yes    | Yes       | Live: output appeared in web UI within milliseconds of tmux activity. Confirmed inotify active via strace. |
| Prompt detection      | Yes    | Yes       | Live: claude trust prompt detected in ~4s (was 10+s before fix). Shell $ prompt, ollama >>>, opencode-acp ready all trigger waiting_input. |
| MCP auto-retry        | Yes    | Yes       | Live: "MCP server failed" appeared in tmux, /mcp + Enter sent automatically, MCP reconnected. Configurable limit (mcp_max_retries=5). |
| Session guardrails    | Yes    | Yes       | Live: AGENT.md created for opencode session, CLAUDE.md for claude-code. Verified CLAUDE.md skipped when AGENT.md exists in project dir. |
| Alerts                | Yes    | Yes       | Live: alerts appeared in web UI for session start, state changes, waiting_input. Active/Inactive tabs, per-session sub-tabs, quick-reply buttons all interacted with manually. |
| Session reconciler    | Yes    | Yes       | Live: killed daemon, restarted, sessions with live tmux resumed to running. Sessions with dead tmux marked complete. Reconciler runs every 30s. |
