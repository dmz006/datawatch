package autonomous

import "strings"

// ObjectTagSelection is the parsed tag selection an Automata object list takes
// (an Automaton, an Automation or a task): the tag strings the object must
// carry — on its system tags (SystemTags) or its assigned managed TagIDs —
// and the match rule. With any-match (the default), an object matches when it
// carries at least one requested tag; with all-match, it must carry every
// requested tag. The rule vocabulary is the same one the sessions list and
// the progress backlog validate (TagMatchAny / TagMatchAll, ValidTagMatch).
type ObjectTagSelection struct {
	// Tags is the de-duplicated, first-seen set of tag strings the object
	// must carry. Empty means the selection matches every object.
	Tags []string
	// AllMatch is true for the "all" rule (every tag present) and false for
	// the "any" rule (the default; at least one tag present).
	AllMatch bool
}

// MatchesObjectTags reports whether the tags an object carries satisfy the
// selection. systemTags is the object's derived system-tag set (its
// PlacementTags, e.g. "project:datawatch") and tagIDs the managed Tag IDs
// assigned to it; either may be nil. A selection with no tags matches every
// object. With the default any-match rule an object matches when any one
// requested tag is present; with all-match every requested tag must be
// present.
func (sel ObjectTagSelection) MatchesObjectTags(systemTags, tagIDs []string) bool {
	if len(sel.Tags) == 0 {
		return true
	}
	set := make(map[string]struct{})
	for _, t := range systemTags {
		set[t] = struct{}{}
	}
	for _, t := range tagIDs {
		set[t] = struct{}{}
	}
	if sel.AllMatch {
		for _, want := range sel.Tags {
			if _, ok := set[want]; !ok {
				return false
			}
		}
		return true
	}
	for _, want := range sel.Tags {
		if _, ok := set[want]; ok {
			return true
		}
	}
	return false
}

// ObjectTagShortcut expands one placement-axis shortcut an operator types into
// the tag string it names: "project" names the project axis, "automation"
// the Automation axis, "automaton" the Automaton axis. These map to the same
// axis templates the object's SystemTags derive under (see derivation.go),
// so a shortcut resolves to exactly the tag the object itself would carry.
// An unknown axis or a blank value returns "" — a shortcut is a sugar-free
// tag: it names an axis, nothing else.
func ObjectTagShortcut(axis, value string) string {
	switch axis {
	case "project", "automation", "automaton":
		if value = strings.TrimSpace(value); value != "" {
			return axis + ":" + value
		}
		return ""
	default:
		return ""
	}
}

// FilterTasksView returns a copy of view whose task list is narrowed to the
// entries matching sel. Each entry's own system tags (its Task.SystemTags)
// are the tags an object carries; tasks carry no managed TagIDs of their
// own. The view's summary fields and the order of the surviving entries are
// preserved. A selection with no tags returns the view unchanged.
func FilterTasksView(view TasksView, sel ObjectTagSelection) TasksView {
	if len(sel.Tags) == 0 {
		return view
	}
	kept := make([]TaskEntry, 0, len(view.Tasks))
	for _, t := range view.Tasks {
		if sel.MatchesObjectTags(t.SystemTags, nil) {
			kept = append(kept, t)
		}
	}
	view.Tasks = kept
	return view
}
