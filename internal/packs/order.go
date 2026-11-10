package packs

// builtinPackList is the pack list used when packs are disabled, and the
// global default when no level defines a list.
var builtinPackList = []string{"builtin"}

// ActivePacks returns the ordered pack list that applies to an Automaton.
//
// When packsEnabled is false it returns ["builtin"]. Otherwise it returns the
// first non-empty list in the order automaton, project, global. The lists are
// not merged: the first defined list wins. When all three are empty it returns
// the global default ["builtin"].
func ActivePacks(automaton, project, global []string, packsEnabled bool) []string {
	if !packsEnabled {
		return append([]string(nil), builtinPackList...)
	}
	for _, level := range [][]string{automaton, project, global} {
		if len(level) > 0 {
			return append([]string(nil), level...)
		}
	}
	return append([]string(nil), builtinPackList...)
}
