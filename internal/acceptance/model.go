package acceptance

import (
	"strings"
	"time"
)

// Settings of acceptance.mode.
const (
	ModeOff    = "off"
	ModeReport = "report"
)

// Kind says which default list a request starts from. Code at the request's
// source sets it (see KindForSource); a caller never chooses it.
type Kind string

// The four kinds.
const (
	KindDeliverable Kind = "deliverable" // a written deliverable
	KindChange      Kind = "change"      // a change to something live
	KindDelivery    Kind = "delivery"    // delivery only
	KindOther       Kind = "other"
)

var sourceKinds = map[string]Kind{
	"message": KindDelivery, "intake": KindDelivery, "handoff": KindDelivery, "question": KindDelivery,
	"report": KindDeliverable, "research": KindDeliverable, "design": KindDeliverable, "docs": KindDeliverable,
	"automaton": KindChange, "bug": KindChange, "code": KindChange, "operation": KindChange, "rule": KindChange,
}

// KindForSource maps a request source to its kind. An unknown source is
// KindOther, never a receipt-only kind.
func KindForSource(source string) Kind {
	if k, ok := sourceKinds[strings.ToLower(strings.TrimSpace(source))]; ok {
		return k
	}
	return KindOther
}

// Check methods. Any other name (apart from the "project:<name>" form) reads
// "cannot tell".
const (
	MethodCommand = "command"
	MethodState   = "state"
	MethodReceipt = "receipt"
	MethodConfirm = "confirm"
	MethodReview  = "review"

	projectMethodPrefix = "project:"
	statePrefix         = "state:"
)

// Values of Item.Before.
const (
	BeforeNone      = ""
	BeforeFailFirst = "fail_first" // the check must fail at a named line before the work
	BeforeDiffer    = "differ"     // the stored value must differ from the start value
)

// Item sources.
const (
	SourceDefault   = "default"
	SourceAsk       = "ask" // added by datawatch for an ask sentence no item covered
	SourceUser      = "user"
	SourcePlanner   = "planner"
	SourceTemplate  = "template"
	SourceInherited = "inherited" // an older record's own "done when" text
)

// ResultValue is the outcome of one item.
type ResultValue string

// The four results. Anything but ResultMet is not met.
const (
	ResultUnchecked  ResultValue = "unchecked"
	ResultMet        ResultValue = "met"
	ResultNotMet     ResultValue = "not_met"
	ResultCannotTell ResultValue = "cannot_tell"
)

// Evidence origins. Only datawatch, project-check and reviewer evidence is
// datawatch's own observation; holder evidence is a claim.
const (
	OriginDatawatch    = "datawatch"
	OriginProjectCheck = "project_check"
	OriginReviewer     = "reviewer"
	OriginPerson       = "person"
	OriginHolder       = "holder"
)

// Expect names what a command check must show. Pass is a line that must appear
// in a passing run (it proves the named thing ran). Fail is the line the start
// run must show when the item needs a failing baseline.
type Expect struct {
	Pass string `json:"pass" yaml:"pass"`
	Fail string `json:"fail,omitempty" yaml:"fail,omitempty"`
}

// CheckSpec is a command check. Command and Script are copies datawatch takes
// when the list is frozen or that its own plan approval path supplies; no
// operator surface accepts them (see ItemInput).
type CheckSpec struct {
	ProjectCheck string   `json:"project_check,omitempty"`
	ScriptPath   string   `json:"script_path,omitempty"`
	Script       string   `json:"script,omitempty"`
	Command      string   `json:"command,omitempty"`
	Inputs       []string `json:"inputs,omitempty"`
	Expect       []Expect `json:"expect,omitempty"`
	TimeoutSec   int      `json:"timeout_seconds,omitempty"`
}

// StateSpec is a check of a stored or observed fact.
type StateSpec struct {
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`         // state:file, repository-relative
	Contains string `json:"contains,omitempty" yaml:"contains,omitempty"` // state:file
	Want     string `json:"want,omitempty" yaml:"want,omitempty"`         // the value a probe must report
	Target   string `json:"target,omitempty" yaml:"target,omitempty"`     // what the probe reads
}

// WaitSpec makes a command or state item an "observe after" item.
type WaitSpec struct {
	AfterSec  int `json:"after_seconds,omitempty" yaml:"after_seconds,omitempty"`
	WindowSec int `json:"window_seconds,omitempty" yaml:"window_seconds,omitempty"`
	MaxGapSec int `json:"max_gap_seconds,omitempty" yaml:"max_gap_seconds,omitempty"`
}

// Item is one "done when" statement and how it is checked.
type Item struct {
	ID        string     `json:"id" yaml:"id"`
	Statement string     `json:"statement" yaml:"statement"`
	From      string     `json:"from,omitempty" yaml:"from,omitempty"` // the words of the ask it covers
	Source    string     `json:"source" yaml:"source,omitempty"`
	Method    string     `json:"method" yaml:"method"`
	Check     *CheckSpec `json:"check,omitempty" yaml:"-"`
	State     *StateSpec `json:"state,omitempty" yaml:"state,omitempty"`
	Wait      *WaitSpec  `json:"wait,omitempty" yaml:"wait,omitempty"`
	Before    string     `json:"before,omitempty" yaml:"before,omitempty"`
	// Change marks an item that must show work was done.
	Change bool `json:"change,omitempty" yaml:"change,omitempty"`
	// Negative marks an item that is met by something not happening.
	Negative bool `json:"negative,omitempty" yaml:"negative,omitempty"`
	// ReportOnly marks an item that only says a failure is reported. It never
	// closes a change.
	ReportOnly bool   `json:"report_only,omitempty" yaml:"report_only,omitempty"`
	Checker    string `json:"checker,omitempty" yaml:"checker,omitempty"`
	// Effect is where the statement has to be true. "work" is the work product
	// itself and "ref:<name>" is a named branch; any other text is a place
	// datawatch cannot read from a file in the work.
	Effect string `json:"effect,omitempty" yaml:"effect,omitempty"`
	// Revision counts the times this item was changed after freeze.
	Revision int `json:"revision,omitempty" yaml:"-"`
}

// methodBase returns the method family and, for "state:x" and "project:x",
// the name after the colon.
func (it Item) methodBase() (string, string) {
	switch {
	case strings.HasPrefix(it.Method, statePrefix):
		return MethodState, strings.TrimPrefix(it.Method, statePrefix)
	case strings.HasPrefix(it.Method, projectMethodPrefix):
		return MethodCommand, strings.TrimPrefix(it.Method, projectMethodPrefix)
	}
	return it.Method, ""
}

// deterministic reports whether datawatch can evaluate the item without a
// person or a reviewer.
func (it Item) deterministic() bool {
	base, _ := it.methodBase()
	return base == MethodCommand || base == MethodState || base == MethodReceipt
}

// Caller is who made a change: the name the request claimed beside the caller
// datawatch observed from the credential.
type Caller struct {
	Claimed  string `json:"claimed,omitempty"`
	Observed string `json:"observed,omitempty"`
	// SessionID is set when the call came from a known session.
	SessionID string `json:"session_id,omitempty"`
	// ViaTool is set when the call came through a model tool, whose own
	// identity may be unknown.
	ViaTool bool `json:"via_tool,omitempty"`
}

// Evidence is what was run or read for a result.
type Evidence struct {
	Origin   string    `json:"origin"`
	What     string    `json:"what"`
	Output   string    `json:"output,omitempty"`
	ExitCode *int      `json:"exit_code,omitempty"`
	Hash     string    `json:"hash,omitempty"`
	Subject  string    `json:"subject,omitempty"`
	At       time.Time `json:"at"`
}

// Waiting says a result is not due yet or is held for usage. It spends no
// retry and is not the operator's action until it is overdue.
type Waiting struct {
	Reason string    `json:"reason"`
	Due    time.Time `json:"due,omitempty"`
}

// Result is the current outcome of one item.
type Result struct {
	Value   ResultValue `json:"value"`
	Reason  string      `json:"reason,omitempty"`
	Waiting *Waiting    `json:"waiting,omitempty"`
	// Subject is the version the result belongs to.
	Subject      string     `json:"subject,omitempty"`
	RunKey       string     `json:"run_key,omitempty"`
	At           time.Time  `json:"at,omitempty"`
	Evidence     []Evidence `json:"evidence,omitempty"`
	AlreadyTrue  bool       `json:"already_true,omitempty"`
	Infra        bool       `json:"infra,omitempty"`
	InputChanged []string   `json:"input_changed,omitempty"`
}

// Confirmation is a person's answer on an item.
type Confirmation struct {
	Yes        bool      `json:"yes"`
	Words      string    `json:"words"`
	RelayedFor string    `json:"relayed_for,omitempty"` // an outside person whose answer was passed on
	By         Caller    `json:"by"`
	At         time.Time `json:"at"`
	Subject    string    `json:"subject,omitempty"`
	// Counts is false when datawatch cannot show the caller is not the doer;
	// the answer is then shown as a claim.
	Counts bool `json:"counts"`
}

// Acceptance is an item accepted without proof. It is never "met".
type Acceptance struct {
	Reason string    `json:"reason"`
	Words  string    `json:"words"`
	By     Caller    `json:"by"`
	At     time.Time `json:"at"`
	Counts bool      `json:"counts"`
}

// Baseline is the start run of a command check.
type Baseline struct {
	Finished bool     `json:"finished"`
	ExitCode int      `json:"exit_code"`
	Reason   string   `json:"reason,omitempty"`
	FailSeen []string `json:"fail_seen,omitempty"`
	// Passed means the check already passed before the work.
	Passed  bool      `json:"passed,omitempty"`
	Subject string    `json:"subject,omitempty"`
	At      time.Time `json:"at"`
}

// StartValue is a state item's value when the list was frozen.
type StartValue struct {
	Recorded bool      `json:"recorded"`
	Value    string    `json:"value,omitempty"`
	True     bool      `json:"already_true,omitempty"`
	Subject  string    `json:"subject,omitempty"`
	At       time.Time `json:"at"`
}

// Sample is one observation of an "observe after" item.
type Sample struct {
	At       time.Time `json:"at"`
	Answered bool      `json:"answered"`
	SourceAt time.Time `json:"source_at,omitempty"`
	Holds    bool      `json:"holds"`
	Restart  bool      `json:"restart,omitempty"`
}

// ItemState is everything datawatch recorded about one item.
type ItemState struct {
	Result         Result            `json:"result"`
	CurrentSubject string            `json:"current_subject,omitempty"`
	InputHashes    map[string]string `json:"input_hashes,omitempty"`
	InputsFrozen   bool              `json:"inputs_frozen,omitempty"`
	Baseline       *Baseline         `json:"baseline,omitempty"`
	Start          *StartValue       `json:"start,omitempty"`
	WaitAnchor     time.Time         `json:"wait_anchor,omitempty"`
	Samples        []Sample          `json:"samples,omitempty"`
	Runs           map[string]Result `json:"runs,omitempty"`
	InfraTries     map[string]int    `json:"infra_tries,omitempty"`
	Confirm        *Confirmation     `json:"confirm,omitempty"`
	Accept         *Acceptance       `json:"accept,omitempty"`
}

// Ask is the request, word for word, with everything added to it later.
type Ask struct {
	Text      string        `json:"text"`
	Additions []AskAddition `json:"additions,omitempty"`
	Hash      string        `json:"hash"`
}

// AskAddition is later words linked to the ask.
type AskAddition struct {
	Text string    `json:"text"`
	By   Caller    `json:"by"`
	At   time.Time `json:"at"`
}

// Revision is the list as it stood after a freeze or a later change. The
// first one is the list as first frozen.
type Revision struct {
	N        int       `json:"n"`
	At       time.Time `json:"at"`
	By       Caller    `json:"by"`
	Words    string    `json:"words,omitempty"`
	Items    []Item    `json:"items"`
	AskHash  string    `json:"ask_hash"`
	Reviewer string    `json:"reviewer_settings,omitempty"`
	Hash     string    `json:"hash"`
}

// Event actions.
const (
	ActionDraft    = "draft"
	ActionFreeze   = "freeze"
	ActionRevise   = "revise"
	ActionConfirm  = "confirm"
	ActionAccept   = "accept"
	ActionAskWords = "ask_words"
	ActionReplaced = "unit_replaced"
	ActionRework   = "rework"
)

// Event is one recorded change to a list.
type Event struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Item   string    `json:"item,omitempty"`
	By     Caller    `json:"by"`
	Words  string    `json:"words,omitempty"`
	// AfterFreeze marks a change made after work could start.
	AfterFreeze bool `json:"after_freeze,omitempty"`
}

// List is the "done when" list of one ask.
type List struct {
	RequestID string `json:"request_id"`
	// Unit is the unit of work now carrying the ask. The list belongs to the
	// ask and survives the unit being reset, removed, split or cloned.
	Unit      string `json:"unit,omitempty"`
	Source    string `json:"source"`
	Kind      Kind   `json:"kind"`
	Scheduled bool   `json:"scheduled,omitempty"`
	Ask       Ask    `json:"ask"`
	// RevisedAskHash is the ask hash the last revision was made against.
	RevisedAskHash string                `json:"revised_ask_hash,omitempty"`
	Items          []Item                `json:"items"`
	State          map[string]*ItemState `json:"state"`
	Revisions      []Revision            `json:"revisions,omitempty"`
	History        []Event               `json:"history,omitempty"`
	// StartSubjects are the versions at freeze, by effect.
	StartSubjects   map[string]Subject `json:"start_subjects,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	FrozenAt        time.Time          `json:"frozen_at,omitempty"`
	NeededBy        time.Time          `json:"needed_by,omitempty"`
	Parent          string             `json:"parent,omitempty"`
	Occurrence      string             `json:"occurrence,omitempty"`
	OccurrenceDue   time.Time          `json:"occurrence_due,omitempty"`
	Holders         []string           `json:"holders,omitempty"`
	DefaultsVersion string             `json:"defaults_version,omitempty"`
	Reviewer        string             `json:"reviewer_settings,omitempty"`
	// Coverage is a person's confirmation that the list covers the whole ask.
	Coverage *Confirmation `json:"coverage,omitempty"`
	// ReviewedHash is the list hash an independent reviewer found complete.
	ReviewedHash      string `json:"reviewed_hash,omitempty"`
	ReworkRounds      int    `json:"rework_rounds,omitempty"`
	LastReworkSubject string `json:"last_rework_subject,omitempty"`
	// Yours holds why the list is now the operator's to act on.
	Yours string `json:"yours,omitempty"`
	// Serial counts stored changes; Generation counts replacements of the
	// unit. Both stop a slow edit or run from overwriting a newer change.
	Serial     int `json:"serial"`
	Generation int `json:"generation,omitempty"`
	// ClosedAt is when the list first read done. It closes with its versions:
	// later unrelated changes do not reopen it.
	ClosedAt time.Time `json:"closed_at,omitempty"`
	// ClaimedDoneAt is when something else reported the request done.
	ClaimedDoneAt time.Time `json:"claimed_done_at,omitempty"`
}

// Frozen reports whether the list was fixed.
func (l *List) Frozen() bool { return !l.FrozenAt.IsZero() }

func (l *List) item(id string) (int, *Item) {
	for i := range l.Items {
		if l.Items[i].ID == id {
			return i, &l.Items[i]
		}
	}
	return -1, nil
}

func (l *List) state(id string) *ItemState {
	if l.State == nil {
		l.State = map[string]*ItemState{}
	}
	st := l.State[id]
	if st == nil {
		st = &ItemState{Result: Result{Value: ResultUnchecked}}
		l.State[id] = st
	}
	return st
}

func (l *List) isHolder(sessionID string) bool {
	for _, h := range l.Holders {
		if h == sessionID {
			return true
		}
	}
	return false
}

// independent reports whether datawatch can show that a caller is not the one
// who did the work. A session counts only when the holders are known and it is
// not one of them. A model tool with no session identity never counts.
func (l *List) independent(c Caller) bool {
	if c.SessionID != "" {
		return len(l.Holders) > 0 && !l.isHolder(c.SessionID)
	}
	return !c.ViaTool
}
