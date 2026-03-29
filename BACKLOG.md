# bugs (open — ordered by priority)

## Critical
<!-- No critical bugs — fixed in v0.7.4 (reconciler, MCP retry validation, session safety) -->

## High — incorrect UI behavior
<!-- Fix second: users see wrong info or missing controls -->
4. Prompt field not shown when restarting session with openwebui/opencode-prompt selected. Prompt should be below LLM backend selector, not replace description. — *Affects: session creation UX*
5. Session 3324 (bash): waiting_input shows stale prompt (java command) instead of latest `$` prompt. — *Affects: prompt detection accuracy*
6. Missing prompt filters: recent claude run had undetected feedback prompts — review log history and add patterns. — *Affects: prompt detection completeness*

## Medium — config gaps
<!-- Fix third: missing configurability, no functional impact -->
7. claude-code has no enabled flag (always returns true) — should be configurable like other LLMs. — *Affects: config consistency*
8. per-LLM auto_git_commit/auto_git_init overrides not yet in LLM config structs. — *Affects: per-session git control*
9. opencode-acp timeouts not configurable (startup 30s, health 5s, message 30s). — *Affects: config completeness*

## Low — UI polish
<!-- Fix last: cosmetic, no functional impact -->
10. Alerts tab: menus need better architecture, events should have cards, collapseable like inactive. — *Affects: alerts readability*

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
- add a "schedule" option to a prompt so user can say "in 2 hours" or "at midnight" or "next wed at X time" run this prompt. schedule should use internal datawatch timer and not LLM since that would burn tokens
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see docs/covert-channels.md)
