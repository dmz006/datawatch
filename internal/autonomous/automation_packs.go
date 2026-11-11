// Automaton pack selection: the template-pack names an Automaton draws from.
// The helper here keeps that list in canonical form before it is persisted.
package autonomous

import "strings"

// normalizePacks trims each pack name, drops empty ones, and returns the rest
// sorted and unique. It returns nil when no name remains so omitempty drops
// the field.
func normalizePacks(in []string) []string {
	trimmed := make([]string, 0, len(in))
	for _, p := range in {
		if p = strings.TrimSpace(p); p != "" {
			trimmed = append(trimmed, p)
		}
	}
	return sortedUniqueStrings(trimmed)
}
