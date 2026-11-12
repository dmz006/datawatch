package autonomous

// The follow-up of an answered decision record.
//
// An operator can ask, when answering, for one named follow-up from the
// configured list autonomous.decisions.follow_ups (a list of {name, kind,
// target}). The default list is empty, so nothing runs unless the operator
// configured it. When the answer is applied the service looks the name up in
// the live list and runs it once:
//
//	start_template   instantiate the template named by target
//	open_automaton   open the held Automaton named by target: approve it when
//	                 it still waits for approval, then run it
//
// The outcome is stored on the record (DecisionRecordStore.RecordFollowUp)
// with a status of done, skipped or error and a one-line result. A record
// whose follow_up status is already set never runs it again. A follow-up that
// is no longer in the configured list when the answer arrives, or an
// Automaton that is not held or draft, is skipped with the reason, never
// failed.
//
// The two actions reach the Automaton system through small interfaces so the
// tests can fake them; *API satisfies DecisionTemplateStarter as it is, and
// NewDecisionAutomatonOpener adapts *API for the other.

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// The statuses a follow-up outcome is stored with.
const (
	DecisionFollowUpDone    = "done"
	DecisionFollowUpSkipped = "skipped"
	DecisionFollowUpError   = "error"
)

// decisionFollowUpResultMax caps the one-line result stored on the record.
const decisionFollowUpResultMax = 300

// ErrNoDecisionRecord is returned when a follow-up is requested for a
// question that no decision record owns.
var ErrNoDecisionRecord = errors.New("the question has no decision record")

// ErrUnknownFollowUp is returned when a requested follow-up is not in the
// configured list. Allowed holds the configured names; an empty Allowed
// means none are configured.
type ErrUnknownFollowUp struct {
	Name    string
	Allowed []string
}

// Error names the allowed follow-ups, or says none are configured.
func (e *ErrUnknownFollowUp) Error() string {
	if len(e.Allowed) == 0 {
		return fmt.Sprintf("unknown follow-up %q: no follow-ups are configured (autonomous.decisions.follow_ups is empty)", e.Name)
	}
	return fmt.Sprintf("unknown follow-up %q: the allowed follow-ups are %s", e.Name, strings.Join(e.Allowed, ", "))
}

// DecisionTemplateStarter instantiates a template into a new Automaton. It is
// the call the autonomous_template_instantiate MCP tool makes; *API has it.
// The result is the new Automaton (a *PRD) or nil.
type DecisionTemplateStarter interface {
	InstantiateTemplate(templateID string, vars map[string]string, actor string) (any, error)
}

// DecisionAutomatonOpener opens a held Automaton: it reads the status, then
// approves and runs it, the calls behind autonomous_prd_approve and
// autonomous_prd_run.
type DecisionAutomatonOpener interface {
	// AutomatonStatus returns the status of the Automaton and whether it exists.
	AutomatonStatus(id string) (status string, found bool)
	// Approve moves a reviewed Automaton to approved.
	Approve(id, actor, note string) (any, error)
	// Run starts the executor of an approved Automaton.
	Run(id string) error
}

// apiAutomatonOpener adapts *API to DecisionAutomatonOpener.
type apiAutomatonOpener struct{ a *API }

// NewDecisionAutomatonOpener returns the opener the daemon wires: the status
// read, approve and run of the Automaton API.
func NewDecisionAutomatonOpener(a *API) DecisionAutomatonOpener {
	return apiAutomatonOpener{a: a}
}

func (o apiAutomatonOpener) AutomatonStatus(id string) (string, bool) {
	st, ok := o.a.prdStatus(id)
	return string(st), ok
}

func (o apiAutomatonOpener) Approve(id, actor, note string) (any, error) {
	return o.a.Approve(id, actor, note)
}

func (o apiAutomatonOpener) Run(id string) error { return o.a.Run(id) }

// SetFollowUpTargets wires the two follow-up actions. Either may be nil; a
// follow-up whose action is not wired is recorded as an error, not run.
func (s *DecisionRecords) SetFollowUpTargets(start DecisionTemplateStarter, open DecisionAutomatonOpener) {
	s.followMu.Lock()
	s.followStart, s.followOpen = start, open
	s.followMu.Unlock()
}

// lookupFollowUp finds the entry named name in list, or returns the typed
// error that names the allowed names.
func lookupFollowUp(list []config.AutonomousDecisionFollowUp, name string) (config.AutonomousDecisionFollowUp, error) {
	for _, f := range list {
		if f.Name == name {
			return f, nil
		}
	}
	allowed := make([]string, 0, len(list))
	for _, f := range list {
		allowed = append(allowed, f.Name)
	}
	sort.Strings(allowed)
	return config.AutonomousDecisionFollowUp{}, &ErrUnknownFollowUp{Name: name, Allowed: allowed}
}

// runFollowUp runs the follow-up the record asked for, once, and stores the
// outcome. It does nothing when none was asked for or when an outcome is
// already stored.
func (s *DecisionRecords) runFollowUp(rec DecisionRecord, _ Question, _ Answer) {
	s.followRunMu.Lock()
	defer s.followRunMu.Unlock()

	cur, ok := s.records.Get(rec.ID)
	if !ok || cur.FollowUp == nil || !cur.FollowUp.Requested || cur.FollowUp.Name == "" {
		return
	}
	if cur.FollowUp.Status != "" {
		return // already ran, was skipped or failed: never again
	}
	name := cur.FollowUp.Name
	out := DecisionFollowUp{Requested: true, Name: name}

	entry, err := lookupFollowUp(s.decisionsConfig().EffectiveFollowUps(), name)
	if err != nil {
		out.Status = DecisionFollowUpSkipped
		out.Result = fmt.Sprintf("follow-up %q is no longer in autonomous.decisions.follow_ups", name)
	} else {
		out.Kind, out.Target = entry.Kind, entry.Target
		out.Status, out.Result = s.executeFollowUp(cur, entry)
	}
	out.Result = followUpResultLine(out.Result, decisionFollowUpResultMax)
	at := s.now().UTC()
	out.At = &at
	if _, err := s.records.RecordFollowUp(rec.ID, out); err != nil {
		log.Printf("[autonomous] decision record %s: store follow-up outcome: %v", rec.ID, err)
	}
}

// executeFollowUp performs one follow-up and returns its status and result.
// A panic in the action becomes an error outcome so one bad action cannot
// take the answer path down.
func (s *DecisionRecords) executeFollowUp(rec DecisionRecord, f config.AutonomousDecisionFollowUp) (status, result string) {
	defer func() {
		if r := recover(); r != nil {
			status, result = DecisionFollowUpError, fmt.Sprintf("follow-up %q panicked: %v", f.Name, r)
		}
	}()
	s.followMu.Lock()
	start, open := s.followStart, s.followOpen
	s.followMu.Unlock()

	switch f.Kind {
	case config.DecisionFollowUpStartTemplate:
		return startTemplateFollowUp(start, f)
	case config.DecisionFollowUpOpenAutomaton:
		return openAutomatonFollowUp(open, f, rec)
	default:
		return DecisionFollowUpError, fmt.Sprintf("follow-up %q has unknown kind %q", f.Name, f.Kind)
	}
}

func startTemplateFollowUp(start DecisionTemplateStarter, f config.AutonomousDecisionFollowUp) (string, string) {
	if start == nil {
		return DecisionFollowUpError, "no template starter is wired"
	}
	out, err := start.InstantiateTemplate(f.Target, nil, decisionSyncActor)
	if err != nil {
		return DecisionFollowUpError, fmt.Sprintf("start template %s: %v", f.Target, err)
	}
	if p, ok := out.(*PRD); ok && p != nil {
		return DecisionFollowUpDone, fmt.Sprintf("started template %s as Automaton %s", f.Target, p.ID)
	}
	return DecisionFollowUpDone, fmt.Sprintf("started template %s", f.Target)
}

// openAutomatonFollowUp approves (when it still waits for approval) and runs
// the Automaton. Only an Automaton that is held or a draft is opened; any
// other state is skipped with the state in the result.
func openAutomatonFollowUp(open DecisionAutomatonOpener, f config.AutonomousDecisionFollowUp, rec DecisionRecord) (string, string) {
	if open == nil {
		return DecisionFollowUpError, "no Automaton opener is wired"
	}
	status, found := open.AutomatonStatus(f.Target)
	if !found {
		return DecisionFollowUpError, fmt.Sprintf("Automaton %s does not exist", f.Target)
	}
	switch PRDStatus(status) {
	case PRDDraft, PRDNeedsReview, PRDRevisionsAsked:
		note := "opened by the answer to decision " + rec.ID
		if _, err := open.Approve(f.Target, decisionSyncActor, note); err != nil {
			return DecisionFollowUpError, fmt.Sprintf("approve Automaton %s (%s): %v", f.Target, status, err)
		}
	case PRDApproved:
		// Already approved and waiting: only the run is left.
	default:
		return DecisionFollowUpSkipped, fmt.Sprintf("Automaton %s is %s, not held or draft", f.Target, status)
	}
	if err := open.Run(f.Target); err != nil {
		return DecisionFollowUpError, fmt.Sprintf("run Automaton %s: %v", f.Target, err)
	}
	return DecisionFollowUpDone, fmt.Sprintf("opened Automaton %s (was %s)", f.Target, status)
}

// followUpResultLine folds s onto one line and cuts it to limit bytes on a rune boundary.
func followUpResultLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut]
}
