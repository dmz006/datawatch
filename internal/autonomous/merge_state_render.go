// merge_state_render.go — plain-text renderings of the derived, read-only
// merge_state field for the CLI and the chat verbs, read from the REST body
// so every surface shows the same words. The field is computed on the
// daemon side (archive_merge_guard.go) and never persisted; this file only
// turns its presence into operator text. Pure text work; nothing here
// touches a session or a git repository.
package autonomous

import (
	"encoding/json"
)

// MergeStateLineText is the operator-facing text for one derived merge_state
// value. The only value the guard defines is finished_not_merged
// (archive_merge_guard.go); any other non-empty value is still named so a
// future value cannot render as blank.
func MergeStateLineText(state string) string {
	switch state {
	case "":
		return ""
	case MergeStateFinishedNotMerged:
		return "branch work not merged into the base branch (merge_state: finished_not_merged)"
	default:
		return "merge_state: " + state
	}
}

// mergeStateOfBody decodes the derived merge_state of an Automaton body (or
// of one list row), or "" when the body carries none.
func mergeStateOfBody(raw json.RawMessage) string {
	var v struct {
		State string `json:"merge_state"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v.State
}

// MergeStateGetLine is the one-line note for one Automaton get body, or ""
// when the body carries no derived merge_state.
func MergeStateGetLine(raw []byte) string {
	return MergeStateLineText(mergeStateOfBody(raw))
}

// MergeStateListLines is one line per Automaton of a list body that carries
// a derived merge_state: "<title or id>: <text>". Rows without the field
// are not marked.
func MergeStateListLines(raw []byte) []string {
	var v struct {
		PRDs []json.RawMessage `json:"prds"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []string
	for _, body := range v.PRDs {
		text := MergeStateLineText(mergeStateOfBody(body))
		if text == "" {
			continue
		}
		var h struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		_ = json.Unmarshal(body, &h)
		name := h.ID
		if h.Title != "" {
			name = h.Title + " (" + h.ID + ")"
		}
		out = append(out, name+": "+text)
	}
	return out
}
