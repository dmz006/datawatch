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

// ParseStateList splits a comma-separated state value. It trims the
// surrounding whitespace of each state and de-duplicates them, keeping the
// first occurrence in place. An unknown state is refused with a plain
// message naming the valid states in the order given. A value without
// states (empty, or only commas and whitespace) returns nil: no state
// filter. The single-state form is unchanged — one token must itself be a
// valid state, and the same value that used to be accepted still is.
func ParseStateList(raw, name string, valid []string) ([]string, error) {
	var out []string
	seen := make(map[string]bool, len(valid))
	for _, part := range strings.Split(raw, ",") {
		tok := strings.TrimSpace(part)
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
