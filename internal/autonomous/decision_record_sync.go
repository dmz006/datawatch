package autonomous

// The decision record service: it keeps a decision record and its queue
// question in step. The record is the source of truth; the question is built
// from it (BuildQuestionFromDecision), never from caller prose, and exactly
// one question exists per record.
//
//   - Create validates the record, builds the question, checks its clarity
//     (refuse mode stores nothing, warn mode stores and reports), stores the
//     record, then raises the question through the record-id dedupe so a
//     crash or a repeat can never leave two.
//   - Update changes an open record and applies the rebuilt question to the
//     same open question.
//   - Withdraw withdraws the open question and marks the record withdrawn.
//   - Reconcile runs on every daemon poll and repairs drift: an open record
//     without a question, an open question whose text differs from the
//     record, a question withdrawn outside the record.
//
// Both switches (autonomous.decisions.enabled and autonomous.questions.enabled)
// gate Create, Update and Reconcile; they are read live on every call, so a
// config change applies without a restart.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// decisionSyncActor is the actor recorded when the service itself changes a
// question or a record.
const decisionSyncActor = "datawatch"

// The two switches, named as the operator sees them.
const (
	decisionsSwitchName = "autonomous.decisions.enabled"
	questionsSwitchName = "autonomous.questions.enabled"
)

// ErrDecisionRecordsUnavailable is returned when a switch the decision
// records need is off. It names every switch that is off.
type ErrDecisionRecordsUnavailable struct {
	// Switches lists the config keys that are off.
	Switches []string
}

// Error names which switches are off.
func (e *ErrDecisionRecordsUnavailable) Error() string {
	return "decision records are unavailable: " + strings.Join(e.Switches, " and ") + " is off"
}

// ErrDecisionClarityRefused is returned in refuse mode when the clarity
// check has findings. Nothing was stored.
type ErrDecisionClarityRefused struct {
	Findings []QualityFinding
}

// Error lists the findings.
func (e *ErrDecisionClarityRefused) Error() string {
	parts := make([]string, 0, len(e.Findings))
	for _, f := range e.Findings {
		parts = append(parts, f.Message)
	}
	return "decision record refused as unclear: " + strings.Join(parts, " ")
}

// DecisionRecordView joins a record with the state of its queue question and,
// when the question is answered, who answered (actor and surface), the quote
// and the answered time, all read from the question store.
type DecisionRecordView struct {
	DecisionRecord
	QuestionState   QuestionState `json:"question_state,omitempty"`
	AnsweredBy      string        `json:"answered_by,omitempty"`
	AnsweredSurface string        `json:"answered_surface,omitempty"`
	AnsweredQuote   string        `json:"answered_quote,omitempty"`
	AnsweredAt      *time.Time    `json:"answered_at,omitempty"`
}

// DecisionReconcileReport says what one Reconcile pass did.
type DecisionReconcileReport struct {
	At time.Time `json:"at"`
	// Raised counts questions raised for open records that had none.
	Raised int `json:"raised"`
	// Linked counts records linked to a question that already existed (a
	// crash between the raise and the link).
	Linked int `json:"linked"`
	// Updated counts open questions whose text was brought back to the record.
	Updated int `json:"updated"`
	// Withdrawn counts records marked withdrawn because their question was.
	Withdrawn int `json:"withdrawn"`
	// Answered counts records that got the answer of their question applied
	// because the prompt trigger missed it.
	Answered int `json:"answered"`
	// Errors holds one line per record that could not be repaired; the next
	// pass tries again.
	Errors []string `json:"errors,omitempty"`
}

// Changed reports whether the pass repaired anything.
func (r DecisionReconcileReport) Changed() bool {
	return r.Raised+r.Linked+r.Updated+r.Withdrawn+r.Answered > 0
}

// DecisionRecords is the service. All methods are safe for concurrent use.
type DecisionRecords struct {
	mu sync.Mutex // one lock around every create/raise/sync sequence

	records   *DecisionRecordStore
	questions *QuestionStore

	decisionsCfg func() config.AutonomousDecisionsConfig
	questionsCfg func() config.AutonomousQuestionsConfig
	now          func() time.Time

	// effectsMu guards the answers waiting for their release and effects
	// (decision_record_answer.go). It is never held while they run.
	effectsMu   sync.Mutex
	pending     []decisionAnswerApplied
	release     func(QuestionChange)
	effectsSeen func(DecisionRecord, Question, Answer) // test seam

	// followMu guards the follow-up targets (decision_record_followup.go);
	// followRunMu serialises the follow-up runs so one record's follow-up is
	// checked, run and recorded as one step.
	followMu    sync.Mutex
	followStart DecisionTemplateStarter
	followOpen  DecisionAutomatonOpener
	followRunMu sync.Mutex

	// folder is the state of the decision folder watch
	// (decision_record_folder.go).
	folder decisionFolderState
}

// NewDecisionRecords builds the service. The config functions are read on
// every call (a nil one reads the defaults, which leaves the question channel
// off). A nil clock reads the wall clock.
func NewDecisionRecords(records *DecisionRecordStore, questions *QuestionStore,
	decisionsCfg func() config.AutonomousDecisionsConfig,
	questionsCfg func() config.AutonomousQuestionsConfig,
	now func() time.Time) *DecisionRecords {
	if now == nil {
		now = time.Now
	}
	return &DecisionRecords{
		records:      records,
		questions:    questions,
		decisionsCfg: decisionsCfg,
		questionsCfg: questionsCfg,
		now:          now,
	}
}

// RecordStore returns the decision record store this service persists to.
// Exposed so a Manager attaching the service can wire the object system
// tag derivation into its save paths (ObjectTagDeriver);
// NewDecisionRecords itself stays store-agnostic.
func (s *DecisionRecords) RecordStore() *DecisionRecordStore {
	return s.records
}

// QuestionStore returns the question store this service raises into. The
// same exposure as RecordStore: the Manager wires its object system tag
// derivation into the store's Raise and Update paths.
func (s *DecisionRecords) QuestionStore() *QuestionStore {
	return s.questions
}

func (s *DecisionRecords) decisionsConfig() config.AutonomousDecisionsConfig {
	if s.decisionsCfg == nil {
		return config.AutonomousDecisionsConfig{}
	}
	return s.decisionsCfg()
}

func (s *DecisionRecords) questionsConfig() config.AutonomousQuestionsConfig {
	if s.questionsCfg == nil {
		return config.AutonomousQuestionsConfig{}
	}
	return s.questionsCfg()
}

// available returns nil when both switches are on, else the typed error that
// names the ones that are off.
func (s *DecisionRecords) available() error {
	var off []string
	if !s.decisionsConfig().EffectiveEnabled() {
		off = append(off, decisionsSwitchName)
	}
	if !s.questionsConfig().Enabled {
		off = append(off, questionsSwitchName)
	}
	if len(off) > 0 {
		return &ErrDecisionRecordsUnavailable{Switches: off}
	}
	return nil
}

// checkContent validates the content of rec and runs the clarity check on
// the question built from it. Findings are returned in warn mode; in refuse
// mode any finding becomes an ErrDecisionClarityRefused.
func (s *DecisionRecords) checkContent(rec DecisionRecord) ([]QualityFinding, error) {
	c := contentOf(rec)
	if err := ValidateNewDecisionRecord(&c); err != nil {
		return nil, err
	}
	policy := DecisionClarityPolicyFromConfig(s.decisionsConfig())
	findings, err := CheckDecisionClarity(c, BuildQuestionFromDecision(c), policy)
	if err != nil {
		return nil, err
	}
	if policy.Mode == QualityModeRefuse && len(findings) > 0 {
		return nil, &ErrDecisionClarityRefused{Findings: findings}
	}
	return findings, nil
}

// Create stores a new record and raises its question. In refuse mode a record
// with clarity findings is refused with ErrDecisionClarityRefused and nothing
// is stored; in warn mode the record is stored and the findings come back
// with the result. If the record is stored but its question cannot be raised,
// the view and an error come back together and the next Reconcile retries.
func (s *DecisionRecords) Create(rec DecisionRecord, source, sourceRef string) (DecisionRecordView, []QualityFinding, error) {
	if err := s.available(); err != nil {
		return DecisionRecordView{}, nil, err
	}
	rec.Source, rec.SourceRef = source, sourceRef

	defer s.drainAnswerEffects()
	s.mu.Lock()
	defer s.mu.Unlock()
	findings, err := s.checkContent(rec)
	if err != nil {
		return DecisionRecordView{}, nil, err
	}
	stored, err := s.records.Create(rec)
	if err != nil {
		return DecisionRecordView{}, nil, err
	}
	var rep DecisionReconcileReport
	if err := s.syncLocked(stored, &rep); err != nil {
		cur, _ := s.records.Get(stored.ID)
		return s.view(cur), findings, fmt.Errorf("decision record %s is stored but its question is not in step (the next poll retries): %w", stored.ID, err)
	}
	cur, _ := s.records.Get(stored.ID)
	return s.view(cur), findings, nil
}

// Update replaces the content of an open record and applies the rebuilt
// question to the same open question. An answered or withdrawn record, or a
// record whose question was already decided, is refused with
// ErrDecisionRecordClosed. Clarity is re-checked as in Create.
func (s *DecisionRecords) Update(id string, content DecisionRecord) (DecisionRecordView, []QualityFinding, error) {
	if err := s.available(); err != nil {
		return DecisionRecordView{}, nil, err
	}
	defer s.drainAnswerEffects()
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.records.Get(id)
	if !ok {
		return DecisionRecordView{}, nil, fmt.Errorf("decision record %s: %w", id, ErrDecisionRecordNotFound)
	}
	if cur.State != DecisionRecordOpen {
		return DecisionRecordView{}, nil, fmt.Errorf("decision record %s is %s: %w", id, cur.State, ErrDecisionRecordClosed)
	}
	if cur.QuestionID != "" {
		if q, ok := s.questions.Get(cur.QuestionID); ok && q.State != QuestionOpen {
			if q.State != QuestionAnswered {
				// Withdrawn outside the record: bring the record in step.
				_, _ = s.records.Withdraw(id, decisionSyncActor, "the question was "+string(q.State))
			}
			return DecisionRecordView{}, nil, fmt.Errorf("decision record %s: its question is %s: %w", id, q.State, ErrDecisionRecordClosed)
		}
	}
	findings, err := s.checkContent(content)
	if err != nil {
		return DecisionRecordView{}, nil, err
	}
	updated, err := s.records.Update(id, content)
	if err != nil {
		return DecisionRecordView{}, nil, err
	}
	var rep DecisionReconcileReport
	if err := s.syncLocked(updated, &rep); err != nil {
		return s.view(updated), findings, fmt.Errorf("decision record %s is updated but its question is not in step (the next poll retries): %w", id, err)
	}
	now, _ := s.records.Get(id)
	return s.view(now), findings, nil
}

// Withdraw withdraws the record's open question and marks the record
// withdrawn. Withdrawing a withdrawn record changes nothing; an answered
// record, or one whose question is answered, is refused with
// ErrDecisionRecordClosed.
func (s *DecisionRecords) Withdraw(id, actor, reason string) (DecisionRecordView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.records.Get(id)
	if !ok {
		return DecisionRecordView{}, fmt.Errorf("decision record %s: %w", id, ErrDecisionRecordNotFound)
	}
	switch cur.State {
	case DecisionRecordAnswered:
		return DecisionRecordView{}, fmt.Errorf("decision record %s is %s: %w", id, cur.State, ErrDecisionRecordClosed)
	case DecisionRecordOpen:
		// The question goes first: if the daemon stops between the two
		// steps, the record is still open and Reconcile marks it withdrawn.
		q, found := s.questionForLocked(cur)
		if found {
			switch q.State {
			case QuestionOpen:
				if _, err := s.questions.Withdraw(q.ID, actor, reason); err != nil && !errors.Is(err, ErrQuestionNotOpen) {
					return DecisionRecordView{}, err
				}
			case QuestionAnswered:
				return DecisionRecordView{}, fmt.Errorf("decision record %s: its question is answered: %w", id, ErrDecisionRecordClosed)
			}
		}
	}
	out, err := s.records.Withdraw(id, actor, reason)
	if err != nil {
		return DecisionRecordView{}, err
	}
	return s.view(out), nil
}

// Get returns the record joined with its question state and answer, the same
// as View.
func (s *DecisionRecords) Get(id string) (DecisionRecordView, bool) {
	return s.View(id)
}

// View returns the record joined with its question state and answer.
func (s *DecisionRecords) View(id string) (DecisionRecordView, bool) {
	rec, ok := s.records.Get(id)
	if !ok {
		return DecisionRecordView{}, false
	}
	return s.view(rec), true
}

// List returns the matching records as views, newest first.
func (s *DecisionRecords) List(f DecisionRecordFilter) []DecisionRecordView {
	recs := s.records.List(f)
	out := make([]DecisionRecordView, 0, len(recs))
	for _, r := range recs {
		out = append(out, s.view(r))
	}
	return out
}

// view joins rec with its question.
func (s *DecisionRecords) view(rec DecisionRecord) DecisionRecordView {
	v := DecisionRecordView{DecisionRecord: rec}
	if rec.QuestionID == "" {
		return v
	}
	q, ok := s.questions.Get(rec.QuestionID)
	if !ok {
		return v
	}
	v.QuestionState = q.State
	if a, ok := s.questions.CurrentAnswer(q.ID); ok {
		v.AnsweredBy, v.AnsweredSurface, v.AnsweredQuote = a.Actor, a.Surface, a.Quote
		v.AnsweredAt = cloneTime(a.CreatedAt)
	}
	return v
}

// questionForLocked finds the question of a record: the linked one, or, for a
// record that was never linked (a crash after the raise), an open or answered
// question that carries the record id.
func (s *DecisionRecords) questionForLocked(rec DecisionRecord) (Question, bool) {
	if rec.QuestionID != "" {
		return s.questions.Get(rec.QuestionID)
	}
	for _, st := range []QuestionState{QuestionOpen, QuestionAnswered} {
		for _, q := range s.questions.List(QuestionFilter{State: st, Limit: maxQuestionListLimit}) {
			if q.DecisionRecordID == rec.ID {
				return q, true
			}
		}
	}
	return Question{}, false
}

// Reconcile repairs drift between the open records and their questions. It
// runs on every daemon poll and does nothing while either switch is off.
func (s *DecisionRecords) Reconcile() DecisionReconcileReport {
	rep := DecisionReconcileReport{At: s.now()}
	if s.available() != nil {
		return rep
	}
	defer s.drainAnswerEffects()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.records.List(DecisionRecordFilter{State: DecisionRecordOpen, Limit: maxDecisionListLimit}) {
		if err := s.syncLocked(rec, &rep); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", rec.ID, err))
		}
	}
	return rep
}

// syncLocked brings the question of one open record in step with it: raises
// it when missing (through the record-id dedupe), links it, applies the
// record text to an open question, and marks the record withdrawn when its
// question was withdrawn or expired, and applies the answer of an answered
// question that the record does not hold yet.
func (s *DecisionRecords) syncLocked(rec DecisionRecord, rep *DecisionReconcileReport) error {
	if rec.QuestionID == "" {
		got, raised, err := s.questions.RaiseUnlessDecisionRecord(BuildQuestionFromDecision(rec))
		if err != nil {
			return fmt.Errorf("raise question: %w", err)
		}
		if _, err := s.records.SetQuestionID(rec.ID, got.ID); err != nil {
			return fmt.Errorf("link question %s: %w", got.ID, err)
		}
		if raised {
			rep.Raised++
		} else {
			rep.Linked++
		}
		rec.QuestionID = got.ID
	}
	q, ok := s.questions.Get(rec.QuestionID)
	if !ok {
		return fmt.Errorf("question %s is missing from the question store", rec.QuestionID)
	}
	switch q.State {
	case QuestionOpen:
		out, err := s.questions.Update(q.ID, questionPatchFromBuilt(BuildQuestionFromDecision(rec)), decisionSyncActor)
		if err != nil {
			return fmt.Errorf("update question %s: %w", q.ID, err)
		}
		if len(changedQuestionFields(q, out)) > 0 {
			rep.Updated++
		}
	case QuestionWithdrawn, QuestionExpired:
		if _, err := s.records.Withdraw(rec.ID, decisionSyncActor, "the question was "+string(q.State)); err != nil {
			return fmt.Errorf("mark withdrawn: %w", err)
		}
		rep.Withdrawn++
	case QuestionAnswered:
		// The prompt trigger missed this answer (a restart, a hook that was
		// not wired yet): apply it now. An answer already on the record
		// changes nothing.
		a, ok := s.questions.CurrentAnswer(q.ID)
		if !ok {
			return nil
		}
		applied, err := s.applyAnswerLocked(rec, q, a)
		if err != nil {
			return fmt.Errorf("apply answer: %w", err)
		}
		if applied {
			rep.Answered++
		}
	}
	return nil
}

// questionPatchFromBuilt turns a question built from a record into a patch
// that sets every field the record owns, so a link that was removed from the
// record clears on the question too.
func questionPatchFromBuilt(b Question) QuestionPatch {
	rec := QuestionRecommendation{}
	if b.Recommendation != nil {
		rec = *b.Recommendation
	}
	return QuestionPatch{
		Label:          &b.Label,
		Text:           &b.Text,
		Options:        b.Options,
		Recommendation: &rec,
		Default:        &b.Default,
		PRDID:          &b.PRDID,
		ProposalPath:   &b.ProposalPath,
		RegistryRowKey: &b.RegistryRowKey,
		Context:        &b.Context,
	}
}

// RequestFollowUp records that the operator wants the named follow-up to run
// once the question is answered. The name must be one of the entries of
// autonomous.decisions.follow_ups, read live; an unknown name, or any name
// while the list is empty, is refused with *ErrUnknownFollowUp, which says
// which names are allowed (or that none are configured). The question must
// belong to an open decision record, otherwise ErrNoDecisionRecord or
// ErrDecisionRecordClosed comes back. Nothing runs here: the follow-up runs
// when the answer is applied, and asking again before then replaces the
// earlier request.
func (s *DecisionRecords) RequestFollowUp(questionID, name string) error {
	if err := s.available(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records.GetByQuestionID(questionID)
	if !ok {
		return fmt.Errorf("question %s: %w", questionID, ErrNoDecisionRecord)
	}
	if rec.State != DecisionRecordOpen {
		return fmt.Errorf("decision record %s is %s: %w", rec.ID, rec.State, ErrDecisionRecordClosed)
	}
	if _, err := lookupFollowUp(s.decisionsConfig().EffectiveFollowUps(), name); err != nil {
		return err
	}
	_, err := s.records.RecordFollowUp(rec.ID, DecisionFollowUp{Requested: true, Name: name})
	return err
}
