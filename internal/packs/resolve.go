package packs

import "fmt"

// ResolvedTask is one standard task contributed to an Automaton's task list,
// together with the pack it came from. PackName and PackVersion record the
// provenance so a resolved list can always be traced back to its sources.
type ResolvedTask struct {
	Task
	PackName    string
	PackVersion string
}

// ResolveTaskList builds the ordered task list for an Automaton of the given
// kind from the active packs. Packs are walked in the order given; for each
// pack whose Kinds include kind, its tasks are appended after the tasks
// already collected. Two tasks with the same title are refused, and the error
// names both packs that contributed them.
func ResolveTaskList(kind string, active []*Pack) ([]ResolvedTask, error) {
	var out []ResolvedTask
	seen := make(map[string]string) // task title -> pack name that contributed it
	for _, p := range active {
		if p == nil || !packHasKind(p, kind) {
			continue
		}
		for _, t := range p.Tasks {
			if first, dup := seen[t.Title]; dup {
				return nil, fmt.Errorf("duplicate task title %q: contributed by pack %q and pack %q", t.Title, first, p.Name)
			}
			seen[t.Title] = p.Name
			out = append(out, ResolvedTask{Task: t, PackName: p.Name, PackVersion: p.Version})
		}
	}
	return out, nil
}

// packHasKind reports whether the pack lists kind among its Kinds.
func packHasKind(p *Pack, kind string) bool {
	for _, k := range p.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}
