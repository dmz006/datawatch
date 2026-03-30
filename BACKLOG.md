# bugs (open — ordered by priority)

# planned (recommended order — plans in docs/plans/)
1. **ANSI console** — `docs/plans/2026-03-29-ansi-console.md` (2-3 weeks)
   — Highest user-visible impact. Makes TUI backends (claude, opencode) fully usable in web UI.

# backlog
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see docs/covert-channels.md)
- **IPv6 listener support** — add `[::]` bind support for all listeners (HTTP, MCP SSE, DNS, webhooks); investigate dual-stack vs IPv6-only; update config defaults and documentation
