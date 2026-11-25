// Hold migration. A command the operator runs on purpose: it lists the
// Automata that are blocked or failed for reasons that look like a hold (an
// evaluation run whose own measurement failed, design work parked behind an
// operator decision, work the operator already noted as waiting) and puts the
// ones the operator chooses on hold with a reason the operator supplies.
//
// Matching uses regular expressions on stored data and runs nothing. The
// operator decides: chosen ids need not be candidates, and the reason is never
// generated or defaulted. No loop, pass, housekeeping step or startup path
// calls MigrateToHold; a test in this package keeps it that way.
package autonomous

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Codes of the rules a hold migration candidate can match.
const (
	holdMigrationRuleEval   = "eval_measurement"
	holdMigrationRuleDesign = "design_parked"
	holdMigrationRuleNote   = "operator_hold_note"
)

// ErrHoldMigrationBusy is returned by a real MigrateToHold run while a final
// gate is running: moving statuses under a running gate could race it.
var ErrHoldMigrationBusy = errors.New("hold migration: a final gate is running; try again when it has finished")

var (
	holdMigrationEvalRe   = regexp.MustCompile(`(?i)\b(?:eval|evaluation|tiering|matrix|benchmark)s?\b`)
	holdMigrationDesignRe = regexp.MustCompile(`(?i)\b(?:design|proposal|mockup)s?\b`)
	holdMigrationLineRe   = regexp.MustCompile(`(?m)^\s*HOLD\b`)
	holdMigrationNoteRe   = regexp.MustCompile(`(?i)\b(?:hold|parked|until after|after the release)\b`)
)

// HoldMigrationCandidate is one blocked or failed Automaton that looks like it
// should be on hold. Why holds one entry per matched rule: the rule code, a
// colon, and a plain sentence.
type HoldMigrationCandidate struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Project       string   `json:"project"`
	Status        string   `json:"status"`
	BlockedReason string   `json:"blocked_reason"`
	Why           []string `json:"why"`
}

// HoldMigrationOpts configures one MigrateToHold run. Reason is required for a
// real run and is never generated or defaulted. Condition and Until are
// optional and apply to every chosen id.
type HoldMigrationOpts struct {
	// DryRun returns the candidates and writes nothing.
	DryRun bool
	// IDs are the Automata the operator chose. Empty means list only.
	IDs       []string
	Reason    string
	Condition string
	Until     *time.Time
	Actor     string
}

// HoldMigrationHeld is one Automaton a real run put on hold.
type HoldMigrationHeld struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	From  string `json:"from"`
}

// HoldMigrationSkip is one chosen id a real run left alone, and why.
type HoldMigrationSkip struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// HoldMigrationReport is the accounting of one MigrateToHold run.
type HoldMigrationReport struct {
	DryRun bool `json:"dry_run"`
	// Busy is true when a final gate is running. A real run then fails with
	// ErrHoldMigrationBusy; a dry run still reports.
	Busy bool `json:"busy"`
	// Considered is the number of blocked or failed, not held, Automata looked at.
	Considered int `json:"considered"`
	// Candidates are those that match at least one rule.
	Candidates []HoldMigrationCandidate `json:"candidates"`
	// Held lists the chosen Automata a real run put on hold.
	Held []HoldMigrationHeld `json:"held,omitempty"`
	// Skipped lists the chosen ids a real run did not hold, each with a reason.
	Skipped []HoldMigrationSkip `json:"skipped,omitempty"`
}

// holdMigrationSnapshot is the data the rules read, copied under the store lock.
type holdMigrationSnapshot struct {
	id, title, project, status, blockedReason, spec string
	tagText                                         string
	failedTask                                      bool
	decisionKinds                                   map[string]bool
	newestNote                                      string
}

// holdMigrationTagNames maps each tag id to its name.
func (m *Manager) holdMigrationTagNames() map[string]string {
	names := map[string]string{}
	for _, t := range m.store.ListTags() {
		if t != nil {
			names[t.ID] = t.Name
		}
	}
	return names
}

func holdMigrationSnap(p *PRD, tagNames map[string]string) holdMigrationSnapshot {
	s := holdMigrationSnapshot{
		id: p.ID, title: p.Title, project: p.Project, status: string(p.Status),
		blockedReason: p.BlockedReason, spec: p.Spec,
		decisionKinds: map[string]bool{},
	}
	var tags []string
	for _, id := range p.TagIDs {
		tags = append(tags, id)
		if n := tagNames[id]; n != "" {
			tags = append(tags, n)
		}
	}
	tags = append(tags, p.SystemTags...)
	s.tagText = strings.Join(tags, "\n")
	for i := range p.Story {
		for j := range p.Story[i].Tasks {
			if p.Story[i].Tasks[j].Status == TaskFailed {
				s.failedTask = true
			}
		}
	}
	var newest time.Time
	for _, d := range p.Decisions {
		s.decisionKinds[d.Kind] = true
		if strings.TrimSpace(d.Note) != "" && !d.At.Before(newest) {
			newest, s.newestNote = d.At, d.Note
		}
	}
	return s
}

// holdMigrationWhy returns one entry per rule the snapshot matches.
func holdMigrationWhy(s holdMigrationSnapshot) []string {
	var why []string
	blocked := s.status == string(PRDBlocked)
	failedOrBlocked := blocked || s.status == string(PRDFailed)

	text := s.title + "\n" + s.spec + "\n" + s.tagText
	if failedOrBlocked && s.failedTask && holdMigrationEvalRe.MatchString(text) {
		why = append(why, holdMigrationRuleEval+": the title, spec or tags describe an evaluation, tiering, matrix or benchmark run and one of its tasks failed, so the run's own measurement came out failed")
	}

	if blocked && holdMigrationDesignRe.MatchString(s.title+"\n"+s.spec) {
		byDecision := s.decisionKinds[operatorDecisionKind] || s.decisionKinds[operatorAnswerKind] || s.decisionKinds["decision_gate_hold"]
		if byDecision || holdMigrationLineRe.MatchString(s.spec) {
			why = append(why, holdMigrationRuleDesign+": the title or spec describe design, proposal or mockup work and the Automaton is blocked by an operator decision or a HOLD line in its spec")
		}
	}

	if holdMigrationNoteRe.MatchString(s.blockedReason) || holdMigrationNoteRe.MatchString(s.newestNote) {
		why = append(why, holdMigrationRuleNote+": the blocked reason or the newest decision note says hold, parked, until after, or after the release")
	}
	return why
}

// holdMigrationScan looks at every blocked or failed Automaton that is not
// held and returns how many it considered and the ones that match a rule, in
// creation order.
func (m *Manager) holdMigrationScan() (int, []HoldMigrationCandidate) {
	tagNames := m.holdMigrationTagNames()
	prds := m.store.ListPRDs()
	type entry struct {
		created time.Time
		cand    HoldMigrationCandidate
	}
	var found []entry
	considered := 0
	for _, p := range prds {
		if p == nil {
			continue
		}
		var snap holdMigrationSnapshot
		var created time.Time
		m.readLocked(func() {
			created = p.CreatedAt
			if (p.Status == PRDBlocked || p.Status == PRDFailed) && closedRecordReason(p) == "" {
				snap = holdMigrationSnap(p, tagNames)
			}
		})
		if snap.id == "" {
			continue
		}
		considered++
		why := holdMigrationWhy(snap)
		if len(why) == 0 {
			continue
		}
		found = append(found, entry{created, HoldMigrationCandidate{
			ID: snap.id, Title: snap.title, Project: snap.project,
			Status: snap.status, BlockedReason: snap.blockedReason, Why: why,
		}})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if !found[i].created.Equal(found[j].created) {
			return found[i].created.Before(found[j].created)
		}
		return found[i].cand.ID < found[j].cand.ID
	})
	out := make([]HoldMigrationCandidate, len(found))
	for i, e := range found {
		out[i] = e.cand
	}
	return considered, out
}

// HoldMigrationCandidates lists every blocked or failed Automaton that is not
// held and matches at least one hold rule. It reads data and writes nothing.
// Ordinary failed implementation work matches no rule and is never listed.
func (m *Manager) HoldMigrationCandidates() []HoldMigrationCandidate {
	_, cands := m.holdMigrationScan()
	if cands == nil {
		cands = []HoldMigrationCandidate{}
	}
	return cands
}

// MigrateToHold puts the Automata the operator chose on hold. A dry run, or an
// empty IDs list, only returns the candidates. A real run needs a non-empty
// Reason from the operator and refuses while a final gate is running. Each
// chosen id goes through Manager.Hold with the same reason, condition, until
// and actor; chosen ids need not be candidates. An id that is missing, not
// blocked or failed, or already held is skipped with a reason.
func (m *Manager) MigrateToHold(opts HoldMigrationOpts) (HoldMigrationReport, error) {
	rep := HoldMigrationReport{DryRun: opts.DryRun}
	rep.Busy = m.finalGateRunning()
	rep.Considered, rep.Candidates = m.holdMigrationScan()
	if rep.Candidates == nil {
		rep.Candidates = []HoldMigrationCandidate{}
	}
	if opts.DryRun || len(opts.IDs) == 0 {
		return rep, nil
	}
	reason := strings.TrimSpace(opts.Reason)
	if reason == "" {
		return rep, errors.New("hold migration: the operator must supply a reason; it is never generated or defaulted")
	}
	reason, err := checkHoldReason(reason)
	if err != nil {
		return rep, fmt.Errorf("hold migration: %w", err)
	}
	if err := checkHoldUntil(opts.Until, m.holdNow()); err != nil {
		return rep, fmt.Errorf("hold migration: %w", err)
	}
	if rep.Busy {
		return rep, ErrHoldMigrationBusy
	}
	actor := strings.TrimSpace(opts.Actor)
	if actor == "" {
		actor = "operator"
	}
	seen := map[string]bool{}
	for _, raw := range opts.IDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		p, ok := m.store.GetPRD(id)
		if !ok {
			rep.Skipped = append(rep.Skipped, HoldMigrationSkip{ID: id, Reason: "not found"})
			continue
		}
		var status PRDStatus
		var title string
		m.readLocked(func() { status, title = p.Status, p.Title })
		switch status {
		case PRDHeld:
			rep.Skipped = append(rep.Skipped, HoldMigrationSkip{ID: id, Reason: "already held"})
			continue
		case PRDBlocked, PRDFailed:
		default:
			rep.Skipped = append(rep.Skipped, HoldMigrationSkip{ID: id, Reason: fmt.Sprintf("not blocked or failed (status %s)", status)})
			continue
		}
		if _, err := m.Hold(id, HoldRequest{Reason: reason, Condition: opts.Condition, Until: opts.Until}, actor); err != nil {
			rep.Skipped = append(rep.Skipped, HoldMigrationSkip{ID: id, Reason: err.Error()})
			continue
		}
		rep.Held = append(rep.Held, HoldMigrationHeld{ID: id, Title: title, From: string(status)})
	}
	return rep, nil
}
