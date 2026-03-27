# Interface Testing Tracker

This document tracks the validation status of all datawatch interfaces and communication channels.

Nothing has been validated yet.

## Messaging Backends

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| Signal                | No     | No        | —               | Not validated yet       |
| Telegram              | No     | No        | —               | Not validated yet       |
| Discord               | No     | No        | —               | Not validated yet       |
| Slack                 | No     | No        | —               | Not validated yet       |
| Matrix                | No     | No        | —               | Not validated yet       |
| Twilio SMS            | No     | No        | —               | Not validated yet       |
| ntfy                  | No     | No        | —               | Not validated yet       |
| Email (SMTP)          | No     | No        | —               | Not validated yet       |
| GitHub Webhook        | No     | No        | —               | Not validated yet       |
| Generic Webhook       | No     | No        | —               | Not validated yet       |
| DNS Channel           | No     | No        | —               | Not implemented — research/planning only (see docs/covert-channels.md) |

## Web and API Interfaces

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| Web UI                | No     | No        | —               | Not validated yet       |
| REST API              | No     | No        | —               | Not validated yet       |
| WebSocket             | No     | No        | —               | Not validated yet       |

## MCP Interfaces

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| MCP stdio             | No     | No        | —               | Not validated yet       |
| MCP SSE               | No     | No        | —               | Not validated yet       |

## LLM Backends

| Backend               | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| claude-code           | No     | No        | —               | Not validated yet       |
| aider                 | No     | No        | —               | Not validated yet       |
| goose                 | No     | No        | —               | Not validated yet       |
| gemini                | No     | No        | —               | Not validated yet       |
| opencode              | No     | No        | —               | Not validated yet       |
| ollama                | No     | No        | —               | Not validated yet       |
| openwebui             | No     | No        | —               | Not validated yet       |
| shell                 | No     | No        | —               | Not validated yet       |

## CLI

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| CLI                   | No     | No        | —               | Not validated yet       |

---

## Column Definitions

- **Tested**: The interface has been exercised in a real environment (not mocked)
- **Validated**: The interface behaves correctly end-to-end including error handling
- **Test Conditions**: OS/version, account type, network conditions, test scenario
- **Notes**: Observed issues, caveats, or special setup required

## Validation Checklist Per Interface

Use these checks when testing an interface:

### Messaging backend
- [ ] Can send a message to the backend and receive a response from datawatch
- [ ] `help` command returns the help text
- [ ] `new: <task>` starts a session and reports the session ID
- [ ] `list` returns current sessions
- [ ] `status <id>` returns recent output
- [ ] `alerts` returns alert history
- [ ] State changes (running → waiting_input) are delivered as messages
- [ ] Needs-input prompts are delivered as messages
- [ ] Alert broadcast is received when an alert fires
- [ ] Setup wizard (`setup <service>`) works from this backend

### LLM backend
- [ ] `datawatch backend list` shows the backend as registered
- [ ] `datawatch session new --backend <name> "<task>"` starts a session
- [ ] Session reaches `running` state and produces output in logs
- [ ] Session reaches `waiting_input` or `complete` state
- [ ] `setup llm <backend>` wizard configures the backend correctly

### Web UI / API
- [ ] Web UI loads in browser
- [ ] Session list updates in real time via WebSocket
- [ ] Session detail shows live output
- [ ] Quick-input buttons work for waiting sessions
- [ ] Alerts tab shows alert history
- [ ] Settings page shows backend status
- [ ] `/api/health` returns 200
- [ ] `/api/sessions` returns correct session list

### MCP
- [ ] stdio transport connects from Cursor or Claude Desktop
- [ ] `list_sessions` tool returns sessions
- [ ] `start_session` tool starts a new session
- [ ] `get_output` tool returns session log
- [ ] SSE transport connects from a remote AI client (if SSEEnabled)

## How to Update

When an interface is tested, update the row with:
- Tested: Yes
- Validated: Yes (if working correctly) or No (if issues found)
- Test Conditions: e.g. "Linux 6.17, Telegram bot in private group, 2026-03-27"
- Notes: any relevant observations

Alternatively, use `datawatch test <interface>` to auto-collect interface details
and open a GitHub PR updating this file.

Last updated: 2026-03-27
