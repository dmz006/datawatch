# bugs (open — ordered by priority)
- claude session state badges don't update during active work (thinking/processing) — MCP/ACP backends skip terminal detection; claude MCP channel events may not trigger state changes in web UI session list
- bash terminal scrolls to bottom of window instead of staying constrained within the defined terminal area; verify capture-pane display for shell backends
- ollama terminal wraps — screen size mismatch between tmux pane and xterm.js; verify console_cols/rows enforcement for ollama config
- opencode-acp not starting/connecting — was working previously; debug ACP health check and session startup flow
- shell backend script_path config set to /usr/bin/bash causes task passed as positional arg — should be empty for interactive; document correct config
- per-LLM prompt detection editing needs to be accessible from each LLM config popup (not just global detection section)
- openwebui backend uses curl/script approach — investigate if interactive API session is possible for better UX
- all config editing options should be available from communication channels (messaging commands)

# Fixed (v0.14.5-0.14.6) — test results in docs/bug-testing.md
- ~~interface binding not working~~ — FIXED: mutual exclusion logic, config save, restart tested
- ~~TLS redirect URL double-port~~ — FIXED: stripped port from r.Host
- ~~TLS dual-port not working~~ — FIXED: tested enable/disable cycle
- ~~confirm modal Yes not auto-focused~~ — FIXED: yesBtn.focus()
- ~~JS syntax error breaking web UI~~ — FIXED: stray } removed
- ~~splash screen too short~~ — FIXED: 3 second minimum
- ~~detection filters empty/broken~~ — FIXED: managed list UI

# planned

# backlog
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see docs/covert-channels.md)
- **IPv6 listener support** — add `[::]` bind support for all listeners (HTTP, MCP SSE, DNS, webhooks); investigate dual-stack vs IPv6-only; update config defaults and documentation
