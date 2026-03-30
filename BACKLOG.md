# bugs (open — ordered by priority)
- openwebui backend uses curl/script approach — investigate if interactive API session is possible for better UX

# browser validation needed (code deployed)
- terminal scroll constrained within session detail
- claude/opencode state badges updating during active work
- detection filter add/remove managed list UI
- interface checkbox mutual exclusion visual behavior
- real-time stats streaming in settings dashboard

# planned (in docs/plans/)
- **eBPF per-session stats** — `docs/plans/2026-03-30-ebpf-stats.md` (3-4 weeks, deferred)
- **RTK integration** — `docs/plans/2026-03-30-rtk-integration.md` (2.5 weeks, deferred)
- **libsignal** — `docs/plans/2026-03-29-libsignal.md` (3-6 months, deferred)

# user-requested (pending implementation)
- settings page needs sub-tabs (too long for single page)
- multi-machine README section needs correction about shared vs unique channels
- per-session network statistics (currently system-wide only)

# backlog
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling
- **IPv6 listener support** — add `[::]` bind support for all listeners
- **Browser debugging tools** — document recommended extensions: eruda (mobile console), React DevTools (if React ever used), Chrome DevTools remote debugging for Android. Built-in: triple-tap status dot for datawatch debug panel.
