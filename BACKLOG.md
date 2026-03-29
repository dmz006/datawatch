# bugs (open — ordered by priority)
- review history from session 0759, the acp status messages awating input, processing, thinking and any others created should update the status (running, rate limit, etc) in session and in session list.  if there are similar events in claude or other LLM also create the same change
- "some require a daemon restart. Restart now" on settings tab should only display if there have been changes that require it
- no documentation for what "suppress toasts for active session" so people will not know what it means.  All config options and changes should have been documented. that is a rule!
- claude rate-limit was identified; however it should have accepted the "Stop and wait for limit to reset" the default and the set up a scheduled request for the timeout to send continue prompt.
- double check from history that rate-limit was identified. use session ebc7 (this session) to debug; do not stop session.
- claude rate limit; after selecting "1. Stop and wait for limit to reset" will update screen and filter and monitor think it is active again. need to ignore the response after accepting that message until the scheduled job runs or time limit expires to reset rate-limit
- if there is a scheduled request for a session figure out how to show it in the active session that is non-obtrusive and able to list an unknown size list of queued requests
- also show there are scheduled requests with a dropdown to view all in the queue on the main sessions page
- since there are scheduled requests there should be a section in settings showing a paginated list of editable/cancelable scheduled events section should also be collapasable
- the communication channel messages say the session ID but should include the session name in the message
- in the channel tab there should be some info or link or details on what channel can do and what commands can be sent (by llm)
- there are tmux sessions running that are not displaying, debug. if there are past sessions in history that can be reactivated they should be and identify why they were not. if they are truely lost old sessoins then delete them (do not delete or stop ebc7 that is this claude session)
- auto-restart on config save doesn't give notice of "config saved" - check all settings give save notice.  Also I do not see it in the config file; all configurations should be in the config file. correct or explain. same for toasts. verify all configuration options are in the config file
- does seed and init honor --secure? if not fix it.
- tls cert details are not in the config file when enabled. it should have sent the user on a XX second redirect to https: on the port and the server should have auto-restarted and configured TLS
- with web tls enabled it should have the option (switch) for a 2nd interface for tls - if not enabled main port becomes tls otherwise main port stays non-tls and tls port is activated

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
