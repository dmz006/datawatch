# bugs (open)
- if restarting a session and select openwebui it does not show the prompt field. Validate for opencode-prompt also. Prompt should be below LLM backend selector, not replace description.
- Claude MCP: if mcp is connected in backend, validate before retrying /mcp. See session ee8b as example.
- Claude MCP timeout should not kill session — dismiss banner, remove channel tab, let tmux work
- During a recent claude run there were prompts asking for feedback — review log history and create additional prompt filters
- claude-code has no enabled flag (always returns true) — should be configurable like other LLMs
- opencode-acp startup timeout (30s), health check interval (5s), message timeout (30s) not configurable
- per-LLM auto_git_commit/auto_git_init overrides not yet in LLM config structs
- alerts tab in web UI: menus need better architecture, events should have cards, collapseable like inactive
- session 3324 is a bash session. it should display the prompt, also the waiting for input is showing a java enable command or the initial command not the latest prompt waiting which should show ...somethign.. $
- session ee8b is still active, it may be caught in mcp connection issue but it is not showing in active session list. debug and fix

# planned (recommended order — plans in docs/plans/)
1. **Config restructuring** — `docs/plans/2026-03-29-config-restructure.md` (1 week)
   — Do first: foundational. All subsequent config changes (filters, stats, LLM options) land cleanly on a well-structured config.
2. **Flexible detection filters** — `docs/plans/2026-03-29-flexible-filters.md` (1-2 weeks)
   — Depends on config restructure. Fixes multiple open bugs (prompt detection, hardcoded patterns). Unblocks per-LLM customization.
3. **ANSI console** — `docs/plans/2026-03-29-ansi-console.md` (2-3 weeks)
   — Highest user-visible impact. No dependencies on other plans. Makes TUI backends (claude, opencode) fully usable in web UI.
4. **System statistics** — `docs/plans/2026-03-29-system-statistics.md` (2-3 weeks)
   — Independent. Can be developed in parallel with ANSI console. Adds operational visibility.
5. **libsignal integration** — `docs/plans/2026-03-29-libsignal.md` (3-6 months)
   — Long-term. Start research phase (Phase 1) early, but full implementation is lowest priority since signal-cli works. Can run in parallel as background research.

# backlog
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see docs/covert-channels.md)
