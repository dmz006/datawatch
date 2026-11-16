package autonomous

import "sort"

// ProgressKeyNone is the group key for a record with no Automation (arc).
// Records with no project use ProjectKeyNone, which has the same spelling.
const ProgressKeyNone = "(none)"

// ProgressGrouped is the overall totals plus the same counts grouped by
// project, by Automation (arc) and by Automaton. Every grouping covers the
// same records, so the sum over any one grouping equals Totals.
type ProgressGrouped struct {
	Totals       ProgressGroup            `json:"totals"`
	ByProject    map[string]ProgressGroup `json:"by_project"`
	ByAutomation map[string]ProgressGroup `json:"by_automation"`
	ByAutomaton  map[string]ProgressGroup `json:"by_automaton"`
	// NoProject reports the records that had no project and were counted
	// under (none), so they are shown and never dropped.
	NoProject ProgressNoProject `json:"no_project"`
}

// ProgressNoProject lists the Automata that carry no project.
type ProgressNoProject struct {
	Count int      `json:"count"`
	IDs   []string `json:"ids"`
}

// ProgressGroupTotals counts the records chosen by selectProgressRecords overall
// and by project, Automation and Automaton, using the same counting rules as
// ComputeProgress. Empty input gives zero totals and empty (non-nil) maps.
func ProgressGroupTotals(recs []progressRec) ProgressGrouped {
	total := &progressAcc{}
	byProject := map[string]*progressAcc{}
	byAutomation := map[string]*progressAcc{}
	byAutomaton := map[string]*progressAcc{}
	noProject := ProgressNoProject{IDs: []string{}}

	pick := func(m map[string]*progressAcc, key string) *progressAcc {
		a := m[key]
		if a == nil {
			a = &progressAcc{}
			m[key] = a
		}
		return a
	}

	for _, r := range recs {
		p := r.prd
		accumulateRecord(total, r)
		pk := r.project
		if pk == "" {
			pk = ProjectKey(p)
		}
		if pk == ProjectKeyNone {
			noProject.Count++
			noProject.IDs = append(noProject.IDs, p.ID)
		}
		ak := p.AutomationID
		if ak == "" {
			ak = ProgressKeyNone
		}
		accumulateRecord(pick(byProject, pk), r)
		accumulateRecord(pick(byAutomation, ak), r)
		accumulateRecord(pick(byAutomaton, p.ID), r)
	}
	sort.Strings(noProject.IDs)

	finish := func(m map[string]*progressAcc) map[string]ProgressGroup {
		out := make(map[string]ProgressGroup, len(m))
		for k, a := range m {
			out[k] = a.finish()
		}
		return out
	}
	return ProgressGrouped{
		Totals:       total.finish(),
		ByProject:    finish(byProject),
		ByAutomation: finish(byAutomation),
		ByAutomaton:  finish(byAutomaton),
		NoProject:    noProject,
	}
}
