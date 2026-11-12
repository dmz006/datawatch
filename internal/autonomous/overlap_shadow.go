package autonomous

// overlap_shadow.go records what the search-before-create check would have
// warned about while the create mode is shadow. It writes one audit entry
// through the sink it is given and nothing else: the Automaton, the registry
// and the branches are left alone, and the create reply is not touched.

import "github.com/dmz006/datawatch/internal/audit"

// AuditActionCreateSearch is the action name of the shadow-mode audit entry.
const AuditActionCreateSearch = "automaton_create_search"

// RecordCreateSearchShadow writes one audit entry for a create whose search
// ran in shadow mode. The entry names the created Automaton as its subject,
// and its details carry the mode and the warnings the search would have
// returned, an empty list when there were none. A nil sink drops the entry.
func RecordCreateSearchShadow(sink func(audit.Entry), actor, automatonID, mode string, warnings []OverlapWarning) {
	if sink == nil {
		return
	}
	if warnings == nil {
		warnings = []OverlapWarning{}
	}
	sink(audit.Entry{
		Actor:   actor,
		Action:  AuditActionCreateSearch,
		Subject: automatonID,
		Details: map[string]any{
			"mode":     mode,
			"warnings": warnings,
		},
	})
}
