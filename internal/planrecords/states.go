// states.go — the shared comma-separated state query parser.
//
// Every REST list endpoint that takes a state/status/kind query uses
// ParseStateList so all of them split, trim, de-duplicate and refuse
// unknown values the same way.

package planrecords

import (
	"fmt"
	"strings"
)

// ParseStateList splits a comma-separated state value. The list form trims
// the surrounding whitespace of each part, skips parts that vanish and
// de-duplicates in order, keeping the first occurrence. An unknown state is
// refused with a plain message naming the parameter, the refused state and
// the valid states in the order given: %s %q is not a valid %s; use one of …
// A value without states (empty, or only commas and whitespace) returns
// nil: no state filter.
//
// The single form is kept exact the way the route had it before a list
// existed: without a comma the token must itself be a valid state, so a
// value padded with spaces is refused rather than trimmed — and so is an
// unknown state.
func ParseStateList(raw, name string, valid []string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	list := strings.ContainsRune(raw, ',')
	out := make([]string, 0, 2)
	seen := make(map[string]bool, len(valid))
	for _, part := range strings.Split(raw, ",") {
		tok := part
		if list {
			tok = strings.TrimSpace(tok)
		}
		if tok == "" || seen[tok] {
			continue
		}
		ok := false
		for _, v := range valid {
			if v == tok {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("%s %q is not a valid %s; use one of %s", name, tok, name, strings.Join(valid, ", "))
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out, nil
}

// StatesForKinds is the union of the valid states of the given kinds,
// de-duplicated and in first-occurrence order; an empty argument admits no
// state.
func StatesForKinds(kinds ...Kind) []State {
	out := []State{}
	seen := map[State]bool{}
	for _, k := range kinds {
		for _, st := range States(k) {
			if !seen[st] {
				seen[st] = true
				out = append(out, st)
			}
		}
	}
	return out
}
