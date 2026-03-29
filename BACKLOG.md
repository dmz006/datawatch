# bugs (open)
- if restarting a session and select openwebui it does not show the prompt field. Validate for opencode-prompt also. Prompt should be below LLM backend selector, not replace description.
- Claude MCP: if mcp is connected in backend, validate before retrying /mcp. See session ee8b as example.
- Claude MCP timeout should not kill session — dismiss banner, remove channel tab, let tmux work
- During a recent claude run there were prompts asking for feedback — review log history and create additional prompt filters
- claude-code has no enabled flag (always returns true) — should be configurable like other LLMs
- opencode-acp startup timeout (30s), health check interval (5s), message timeout (30s) not configurable
- per-LLM auto_git_commit/auto_git_init overrides not yet in LLM config structs
- alerts tab in web UI: menus need better architecture, events should have cards, collapseable like inactive

# updates
- review the go modules and code created in ../signal-go/ — test and validate for datawatch integration
- create a git project for signal-go, integrate into datawatch, remove signal-cli and Java dependencies

# toplan
- ANSI console: plan for changing tmux web UI to a fully supported ANSI console (xterm.js or similar) so TUI tools like claude and opencode display properly. Mobile-friendly font sizing, scroll support.
- Flexible detection filters: plan to move hardcoded prompt patterns to per-LLM/per-channel config. Make all detection configurable via config file and web UI.
- System statistics: plan for capturing top/CPU/GPU/disk/session details. Settings tab sub-menu with tabs (settings + statistics). Real-time on web UI, queryable via channels and MCP.

# config
- restructure config.yaml to group related fields by function with YAML comments
- ensure saved config includes all fields with defaults and inline documentation
- web UI General Configuration should mirror config file grouping

# backlog
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see docs/covert-channels.md)
