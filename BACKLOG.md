# bugs (open — ordered by priority)

## Critical
<!-- No critical bugs — fixed in v0.7.4 (reconciler, MCP retry validation, session safety) -->

## High — incorrect UI behavior
<!-- Fix second: users see wrong info or missing controls -->
4. Missing prompt filters: recent claude run had undetected feedback prompts — review log history and add patterns. — *Affects: prompt detection completeness*

## Medium — config gaps (deferred to Config Restructure plan)
<!-- These are config completeness issues best addressed during the config restructure -->
5. claude-code has no enabled flag — should be configurable like other LLMs. — *Affects: config consistency*
6. per-LLM auto_git_commit/auto_git_init overrides. — *Affects: per-session git control*
7. opencode-acp timeouts not configurable. — *Affects: config completeness*

## Low — UI polish
8. Alerts tab: menus need better architecture, events should have cards, collapseable. — *Affects: alerts readability*

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
