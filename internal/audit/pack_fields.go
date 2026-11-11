// Package audit records and queries operator and daemon audit entries.
//
// pack_fields.go: helpers for the pack attribution fields an audit entry
// may carry in its Details map. An entry written while a task runs from an
// installable pack records that pack's name under the "pack_name" key, so
// the audit query can filter on it.
package audit

import (
	"strings"
)

// PackNameField is the Details key holding the installable pack name that
// produced an audit entry.
const PackNameField = "pack_name"

// PackNameOf returns the pack name recorded on e, with surrounding white
// space trimmed. It returns "" when the entry has no Details, no pack_name
// key, or a pack_name value that is not a string.
func PackNameOf(e Entry) string {
	if e.Details == nil {
		return ""
	}
	s, ok := e.Details[PackNameField].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
