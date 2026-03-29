# bugs (open — ordered by priority)
- claude rate-limit was identified; however it should have accepted the "Stop and wait for limit to reset" the default and the set up a scheduled request for the timeout to send continue prompt.
- if there is a scheduled request for a session figure out how to show it in the active session that is non-obtrusive and able to list an unknown size list of queued requests
- since there are scheduled requests there should be a section in settings showing a paginated list of editable/cancelable scheduled events section should also be collapasable
- the communication channel messages say the session ID but should include the session name in the message

# planned (recommended order — plans in docs/plans/)
1. **Config restructuring** — `docs/plans/2026-03-29-config-restructure.md` (1 week)
   — Do first: foundational. All subsequent config changes (filters, stats, LLM options) land cleanly on a well-structured config.
2. **Flexible detection filters** — `docs/plans/2026-03-29-flexible-filters.md` (1-2 weeks)
   — Depends on config restructure. Fixes multiple open bugs (prompt detection, hardcoded patterns). Unblocks per-LLM customization.
3. **ANSI console** — `docs/plans/2026-03-29-ansi-console.md` (2-3 weeks)
   — Highest user-visible impact. No dependencies on other plans. Makes TUI backends (claude, opencode) fully usable in web UI.
4. **System statistics** — `docs/plans/2026-03-29-system-statistics.md` (2-3 weeks)
   — Independent. Can be developed in parallel with ANSI console. Adds operational visibility.
5. **Scheduled prompts** — `docs/plans/2026-03-29-scheduled-prompts.md` (1-2 weeks)
   — Natural language scheduling ("in 2 hours", "next wed at 9am"). Uses internal timer, no LLM tokens until session starts. Extends existing ScheduleStore.
6. **libsignal integration** — `docs/plans/2026-03-29-libsignal.md` (3-6 months)
   — Long-term. Start research phase (Phase 1) early, but full implementation is lowest priority since signal-cli works. Can run in parallel as background research.

# backlog
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see docs/covert-channels.md)
- **IPv6 listener support** — add `[::]` bind support for all listeners (HTTP, MCP SSE, DNS, webhooks); investigate dual-stack vs IPv6-only; update config defaults and documentation
