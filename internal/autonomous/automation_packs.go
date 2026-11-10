// Automaton pack selection: the template-pack names an Automaton draws from.
// The helpers here keep that list canonical and resolve the effective list
// across the Automaton, its project and the global default.
package autonomous

import "github.com/dmz006/datawatch/internal/config"

// normalizePacks trims each pack name, drops empty ones and repeats, and keeps
// the first-seen order because the list is a priority order. It returns nil
// when no name remains so omitempty drops the field.
func normalizePacks(in []string) []string {
	return config.NormalizePacks(in)
}

// SetPacks stores a canonical copy of names as the Automaton's pack list.
// Callers that write the field go through here so the persisted value is
// always trimmed, de-duplicated and non-empty-named.
func (a *Automation) SetPacks(names []string) {
	a.Packs = normalizePacks(names)
}

// builtinPacks is the global default when no level defines a list.
var builtinPacks = []string{"builtin"}

// EffectivePacks returns the pack list that applies to the Automaton: the
// first non-empty list in the order Automaton, project, global. Lists are not
// merged. When all three are empty it returns the default ["builtin"]. The
// result is always a fresh slice.
func (a *Automation) EffectivePacks(project, global []string) []string {
	for _, level := range [][]string{a.Packs, project, global} {
		if l := normalizePacks(level); len(l) > 0 {
			return l
		}
	}
	return append([]string(nil), builtinPacks...)
}
