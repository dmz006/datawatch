package autonomous

import (
	"encoding/json"
	"log"
)

// AttemptCategoryBackfillReport is the result of one attempt-category
// backfill pass.
type AttemptCategoryBackfillReport struct {
	// AttemptsExamined counts failed, stalled or cancelled attempts that had
	// a blank FailureCategory when the pass looked at them.
	AttemptsExamined int `json:"attempts_examined"`
	// AttemptsUpdated counts attempts that were given a category.
	AttemptsUpdated int `json:"attempts_updated"`
	// LeftUncategorized counts attempts that carry no end reason, summary or
	// verifier issue, so no category could be derived. They stay blank.
	LeftUncategorized int `json:"left_uncategorized"`
	// AutomatonsSaved counts the Automatons that were persisted.
	AutomatonsSaved int `json:"automatons_saved"`
}

// BackfillAttemptCategories walks every stored Automaton's task attempts and
// sets FailureCategory on failed, stalled or cancelled attempts that have a
// blank one, using attemptCategoryFor over the stored EndReason,
// FailureSummary, VerifierSummary and VerifierIssues. Only Automatons with a
// changed attempt are persisted. It is idempotent: a second run finds nothing
// to update (attempts left blank are re-counted as uncategorized only).
func (m *Manager) BackfillAttemptCategories() AttemptCategoryBackfillReport {
	var rep AttemptCategoryBackfillReport
	for _, listed := range m.store.ListPRDs() {
		var changed bool
		m.store.WithLock(func() {
			for si := range listed.Story {
				for ti := range listed.Story[si].Tasks {
					atts := listed.Story[si].Tasks[ti].Attempts
					for ai := range atts {
						a := &atts[ai]
						switch a.Outcome {
						case attemptOutcomeFailed, attemptOutcomeStalled, attemptOutcomeCancelled:
						default:
							continue
						}
						if a.FailureCategory != "" {
							continue
						}
						rep.AttemptsExamined++
						cat := attemptCategoryFor(a.EndReason, a.Outcome, "", a.FailureSummary, a.VerifierSummary, a.VerifierIssues)
						if cat == "" {
							rep.LeftUncategorized++
							continue
						}
						a.FailureCategory = cat
						rep.AttemptsUpdated++
						changed = true
					}
				}
			}
		})
		if !changed {
			continue
		}
		if err := m.store.SavePRD(listed); err != nil {
			log.Printf("[autonomous] %s attempt category backfill: save: %v", listed.ID, err)
			continue
		}
		rep.AutomatonsSaved++
	}
	return rep
}

// StartAttemptCategoryBackfill runs one attempt-category backfill pass in a
// goroutine after the stores have loaded. It never blocks or fails startup.
func (m *Manager) StartAttemptCategoryBackfill() {
	go func() {
		rep := m.BackfillAttemptCategories()
		b, _ := json.Marshal(rep)
		log.Printf("[autonomous] attempt_category_backfill: report %s", b)
	}()
}
