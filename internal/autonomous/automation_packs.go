// Automaton pack selection: the template-pack names an Automaton draws from.
// The helper here keeps that list in canonical form before it is persisted.
package autonomous

import "strings"

// normalizePacks trims each pack name, drops empty ones and later duplicates,
// and returns the rest in the order given: the list is a priority order, so it
// is never sorted. It returns nil when no name remains so omitempty drops the
// field. The input slice is not modified.
func normalizePacks(in []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// SetPacks stores the given pack names on the Automaton in canonical form (see
// normalizePacks). It is the one way to assign Packs, so the stored list is
// always trimmed, unique and in priority order.
func (a *Automation) SetPacks(packs []string) {
	a.Packs = normalizePacks(packs)
}
