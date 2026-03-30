# bugs (open — ordered by priority)
- openwebui backend uses curl/script approach — investigate if interactive API session is possible for better UX

# browser validation needed (code fixes deployed in v0.15.0)
- terminal scroll constrained within session detail — CSS min-height:0 + overflow:hidden fix
- claude/opencode state badges updating during active work via capture-pane detection
- detection filter add/remove managed list UI in settings
- interface checkbox mutual exclusion visual behavior

# Fixed (v0.15.0) — test results in docs/bug-testing.md
- ~~bash terminal scrolls past window~~ — CSS: min-height:0, overflow:hidden on session-detail
- ~~ollama terminal wrapping~~ — default console_cols=120 for ollama, opencode, openwebui
- ~~shell script_path=/usr/bin/bash~~ — isShellBinary() detects shell binaries, treats as interactive
- ~~claude state badges not updating~~ — capture-pane state detection in StartScreenCapture
- ~~configure command missing from messaging~~ — new configure/config/set command in router
- ~~per-LLM detection editing~~ — global managed list UI (per-LLM via config section prefix)

# Fixed (v0.14.5-0.14.6) — test results in docs/bug-testing.md
- ~~interface binding not working~~ — mutual exclusion, config save, restart tested
- ~~TLS redirect URL double-port~~ — stripped port from r.Host
- ~~TLS dual-port not working~~ — tested enable/disable cycle
- ~~confirm modal Yes not auto-focused~~ — yesBtn.focus()
- ~~JS syntax error breaking web UI~~ — stray } removed
- ~~splash screen too short~~ — 3 second minimum
- ~~detection filters empty/broken~~ — managed list UI

# planned

# backlog
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see docs/covert-channels.md)
- **IPv6 listener support** — add `[::]` bind support for all listeners
