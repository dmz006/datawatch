// General opt-out across every comm channel for the outbound
// session-lifecycle notification broadcast (config: notify_exclude).
// Found 2026-10-10 — an operator was surprised by imap_mcp emails that
// had, in fact, always been sent to every configured channel; they'd
// just always bounced before B110's outbound-recipient fix. Extracted
// as pure functions so the matching logic is unit-testable without
// constructing a real *router.Router.

package main

// notifyExcludeSet returns a lookup set for names, or nil when names
// is empty — nil (not an empty map) is the signal callers check to
// skip filtering entirely when nothing is excluded, so the common
// case (no exclusions configured) does zero extra work.
func notifyExcludeSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// isNotifyExcluded reports whether backendName (a comm backend's own
// Name(), e.g. "imap_mcp", "signal", "ntfy", "email") is in set. A nil
// set (nothing configured) never excludes anything.
func isNotifyExcluded(set map[string]bool, backendName string) bool {
	return set != nil && set[backendName]
}
