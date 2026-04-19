// BL98 — KG contradiction detection (mempalace fact_checker port).
//
// Mempalace's fact_checker scans the temporal KG for triples that
// contradict each other (overlapping validity windows for the same
// subject+predicate but different object). BL98 ports the simplest
// useful slice:
//
//   * "Functional" predicates — those where one subject can have AT
//     MOST ONE active object value at a time (e.g. owns,
//     current_status, lives_in). A second active triple with a
//     different object is a contradiction.
//   * Active = valid_to == "" (the existing schema's "still true"
//     marker).
//
// Operators register predicates as functional via
// SetFunctionalPredicates. The default set is empty (every predicate
// is treated as multi-valued — same as before BL98) so existing
// callers see no behavioural change until they opt in.

package memory

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Contradiction describes one (subject, predicate) for which two or
// more active triples exist with different objects.
type Contradiction struct {
	Subject   string
	Predicate string
	Triples   []KGTriple // all the active rows that conflict
	DetectedAt time.Time
}

var (
	functionalMu         sync.RWMutex
	functionalPredicates = map[string]bool{}
)

// SetFunctionalPredicates declares which predicates may have only
// one active object per subject. Replaces the previous set; pass
// nil/empty to disable detection (callers can pre-screen entries
// before insert as an alternative). Operator-tunable so a project
// can declare its own functional predicates without code changes.
func SetFunctionalPredicates(preds []string) {
	functionalMu.Lock()
	defer functionalMu.Unlock()
	functionalPredicates = make(map[string]bool, len(preds))
	for _, p := range preds {
		functionalPredicates[p] = true
	}
}

// FunctionalPredicates returns the registered set as a slice.
func FunctionalPredicates() []string {
	functionalMu.RLock()
	defer functionalMu.RUnlock()
	out := make([]string, 0, len(functionalPredicates))
	for p := range functionalPredicates {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// FindContradictions scans every active triple and reports
// contradictions on the registered functional predicates. Returns
// an empty slice when none exist (or no functional predicates were
// declared).
func (kg *KnowledgeGraph) FindContradictions() ([]Contradiction, error) {
	functionalMu.RLock()
	preds := make([]string, 0, len(functionalPredicates))
	for p := range functionalPredicates {
		preds = append(preds, p)
	}
	functionalMu.RUnlock()
	if len(preds) == 0 {
		return nil, nil
	}

	rows, err := kg.db.Query(`
		SELECT id, subject, predicate, object, valid_from, valid_to, source, created_at
		FROM kg_triples
		WHERE valid_to = ''
		ORDER BY subject, predicate, created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("scan active triples: %w", err)
	}
	defer rows.Close()

	bucket := map[string][]KGTriple{} // key = subject|predicate
	for rows.Next() {
		var t KGTriple
		if err := rows.Scan(&t.ID, &t.Subject, &t.Predicate, &t.Object,
			&t.ValidFrom, &t.ValidTo, &t.Source, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		if !isFunctional(preds, t.Predicate) {
			continue
		}
		key := t.Subject + "|" + t.Predicate
		bucket[key] = append(bucket[key], t)
	}

	var out []Contradiction
	now := time.Now().UTC()
	for _, ts := range bucket {
		objects := map[string]bool{}
		for _, t := range ts {
			objects[t.Object] = true
		}
		if len(objects) < 2 {
			continue
		}
		out = append(out, Contradiction{
			Subject:    ts[0].Subject,
			Predicate:  ts[0].Predicate,
			Triples:    ts,
			DetectedAt: now,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Predicate < out[j].Predicate
	})
	return out, nil
}

// ResolveContradictionLatestWins keeps the most-recently-created
// triple in the contradiction and invalidates every other active
// triple in the conflict set. Operator-driven; the detector itself
// never mutates state.
func (kg *KnowledgeGraph) ResolveContradictionLatestWins(c Contradiction) error {
	if len(c.Triples) <= 1 {
		return nil
	}
	sort.Slice(c.Triples, func(i, j int) bool {
		return c.Triples[i].CreatedAt > c.Triples[j].CreatedAt
	})
	winner := c.Triples[0]
	for _, t := range c.Triples[1:] {
		if t.Object == winner.Object {
			continue // shouldn't happen — bucket dedupes objects
		}
		if err := kg.Invalidate(t.Subject, t.Predicate, t.Object, ""); err != nil {
			return fmt.Errorf("invalidate %d: %w", t.ID, err)
		}
	}
	return nil
}

func isFunctional(preds []string, p string) bool {
	for _, x := range preds {
		if x == p {
			return true
		}
	}
	return false
}
