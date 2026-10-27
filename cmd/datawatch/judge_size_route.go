package main

import "fmt"

// judgeSizeRouteBackend is the backend a large diff is routed to first.
const judgeSizeRouteBackend = "claude-code"

// judgeSizeRoute orders a judge backend chain for a measured diff size.
// threshold 0 turns size routing off. At or below the threshold the chain
// keeps its configured order; above it claude-code moves to the front when
// it is in the chain and the rest keep their relative order. It returns a
// fresh slice with each backend once (first occurrence wins) and the
// sentence the decision log shows; chain is never mutated.
func judgeSizeRoute(chain []string, lines, threshold int) (ordered []string, rule string) {
	ordered = make([]string, 0, len(chain))
	seen := make(map[string]struct{}, len(chain))
	for _, b := range chain {
		if _, dup := seen[b]; dup {
			continue
		}
		seen[b] = struct{}{}
		ordered = append(ordered, b)
	}

	switch {
	case threshold == 0:
		return ordered, "default order: size routing is off (threshold 0)"
	case lines <= threshold:
		return ordered, fmt.Sprintf("default order: %d changed lines is at or below the threshold of %d", lines, threshold)
	}

	if _, ok := seen[judgeSizeRouteBackend]; !ok {
		return ordered, fmt.Sprintf("default order: %d changed lines is above the threshold of %d but %s is not in the chain",
			lines, threshold, judgeSizeRouteBackend)
	}
	front := make([]string, 0, len(ordered))
	front = append(front, judgeSizeRouteBackend)
	for _, b := range ordered {
		if b != judgeSizeRouteBackend {
			front = append(front, b)
		}
	}
	return front, fmt.Sprintf("size rule: %d changed lines is above the threshold of %d, %s tried first",
		lines, threshold, judgeSizeRouteBackend)
}
