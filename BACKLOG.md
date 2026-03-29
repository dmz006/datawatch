# bugs (open — ordered by priority)
- on session start; llm should only offer a resume code if they support it; this needs to be a per llm config option and by default opencode, opencode-acp and claude are the only that support it. if not enabled it should be hidden onstart.
- new session auto git and auto git commit should be on the same line
- bash session is not showing shell prompt and is not notifying on prompt; debug and fix
- inside viewing a session the banner of LLM tmux channel running rate_limit stop timeline etc should all be the same "size". stop can continue having a border because it is actoinable. also if a run command comes up it should have a green highlighted border again so its easy to see it's actionable
- on sessions page, add between filter & show history badge icons for each (in the list) llm type for easy filter selection


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
