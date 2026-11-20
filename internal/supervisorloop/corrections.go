package supervisorloop

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Correction classes. A correction the operator gave is exactly one of these.
const (
	// ClassState: the correction holds only while the situation it
	// describes holds. It expires when that situation is done.
	ClassState = "state"
	// ClassStandingRule: the correction is a durable rule. It is promoted
	// to the project rules pack as a reviewed commit, and a fresh session
	// reads it from the pack.
	ClassStandingRule = "standing_rule"
	// ClassProductDefect: the correction reveals a product defect. It is
	// filed as an Automaton on the project plan, and its workaround rides
	// the handover until the Automaton lands.
	ClassProductDefect = "product_defect"
)

var correctionClasses = map[string]bool{
	ClassState: true, ClassStandingRule: true, ClassProductDefect: true,
}

// Correction statuses.
const (
	StatusActive   = "active"   // recorded, held in the state record
	StatusExpired  = "expired"  // state correction, its situation is done
	StatusPromoted = "promoted" // standing rule, committed to the rules pack
	StatusFiled    = "filed"    // product defect, filed as an Automaton
)

// Errors returned by Corrections.
var (
	ErrCorrectionNotFound = errors.New("correction not found")
	ErrCorrectionClass    = errors.New("correction class does not allow this action")
	ErrCorrection         = errors.New("invalid correction")
)

// Correction is one correction the operator gave.
type Correction struct {
	ID         string    `json:"id"`
	Text       string    `json:"text"`
	Evidence   string    `json:"evidence,omitempty"`
	Class      string    `json:"class"`
	Status     string    `json:"status"`
	Workaround string    `json:"workaround,omitempty"`
	Link       string    `json:"link,omitempty"` // promotion commit, or the Automaton it was filed as
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// RulesPackWriter commits reviewed rules to the project rules pack source.
type RulesPackWriter interface {
	// CommitRule appends ruleText to the project's rules pack as a
	// reviewed commit and returns a link to the commit.
	CommitRule(project, ruleText string) (link string, err error)
}

// PlanFiler files product defects as Automata on the project plan.
type PlanFiler interface {
	// FileAutomaton files the defect as an Automaton on the project plan
	// and returns a link to the Automaton.
	FileAutomaton(project, title, body string) (link string, err error)
}

// Corrections manages the corrections the operator gave. Corrections live in
// the state record's correction section. A standing rule that is promoted
// leaves the record — a fresh session reads it from the rules pack. A state
// correction that expires leaves the record too. A product defect stays in
// the record with its link and workaround until its Automaton lands.
type Corrections struct {
	recs  *StateRecords
	rules RulesPackWriter
	plans PlanFiler
	clock Clock
}

// NewCorrections builds the accessor over the state records.
func NewCorrections(recs *StateRecords, rules RulesPackWriter, plans PlanFiler, clock Clock) *Corrections {
	return &Corrections{recs: recs, rules: rules, plans: plans, clock: clock}
}

// entryKey namespaces correction ids inside the state record, whose entry
// ids are shared across all sections.
func entryKey(id string) string { return "corr/" + id }

func (c *Corrections) ready() error {
	if c.recs == nil {
		return errors.New("corrections: state records are required")
	}
	if c.clock == nil {
		return errors.New("corrections: clock is required")
	}
	return nil
}

// Record stores a correction as active in the state record.
func (c *Corrections) Record(project, role string, corr Correction, who, why string) error {
	if err := c.ready(); err != nil {
		return err
	}
	if who == "" || why == "" {
		return fmt.Errorf("%w: who and why are required", ErrCorrection)
	}
	if corr.ID == "" || strings.TrimSpace(corr.Text) == "" || !correctionClasses[corr.Class] {
		return fmt.Errorf("%w: id, text and a valid class are required", ErrCorrection)
	}
	if corr.Status != "" && corr.Status != StatusActive {
		return fmt.Errorf("%w: a recorded correction is %s", ErrCorrection, StatusActive)
	}
	now := c.clock.Now()
	corr.Status = StatusActive
	if corr.CreatedAt.IsZero() {
		corr.CreatedAt = now
	}
	corr.UpdatedAt = now
	data, err := json.Marshal(corr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrection, err)
	}
	entry := StateEntry{ID: entryKey(corr.ID), Section: SectionCorrection, Text: string(data)}
	return c.recs.SetEntry(project, role, entry, who, why)
}

// Get returns the correction under id from the state record.
func (c *Corrections) Get(project, role, id string) (Correction, bool, error) {
	if err := c.ready(); err != nil {
		return Correction{}, false, err
	}
	rec, err := c.recs.Get(project, role)
	if err != nil {
		return Correction{}, false, err
	}
	for _, e := range rec.Entries {
		if e.Section != SectionCorrection || e.ID != entryKey(id) {
			continue
		}
		var corr Correction
		if err := json.Unmarshal([]byte(e.Text), &corr); err != nil {
			return Correction{}, false, fmt.Errorf("decode correction %s: %w", id, err)
		}
		return corr, true, nil
	}
	return Correction{}, false, nil
}

// List returns every correction held in the state record, ordered by id.
func (c *Corrections) List(project, role string) ([]Correction, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	rec, err := c.recs.Get(project, role)
	if err != nil {
		return nil, err
	}
	var out []Correction
	for _, e := range rec.Entries {
		if e.Section != SectionCorrection {
			continue
		}
		var corr Correction
		if err := json.Unmarshal([]byte(e.Text), &corr); err != nil {
			return nil, fmt.Errorf("decode correction %s: %w", strings.TrimPrefix(e.ID, "corr/"), err)
		}
		out = append(out, corr)
	}
	return out, nil
}

// Expire retires a state correction once its situation is done: it is
// marked expired and removed from the state record, so a fresh session
// never inherits it.
func (c *Corrections) Expire(project, role, id, who, why string) (Correction, error) {
	if err := c.ready(); err != nil {
		return Correction{}, err
	}
	if who == "" || why == "" {
		return Correction{}, fmt.Errorf("%w: who and why are required", ErrCorrection)
	}
	corr, ok, err := c.Get(project, role, id)
	if err != nil {
		return Correction{}, err
	}
	if !ok {
		return Correction{}, fmt.Errorf("%w: %s", ErrCorrectionNotFound, id)
	}
	if corr.Class != ClassState {
		return Correction{}, fmt.Errorf("%w: only %s corrections expire (this one is %s)", ErrCorrectionClass, ClassState, corr.Class)
	}
	if corr.Status != StatusActive {
		return Correction{}, fmt.Errorf("%w: only active corrections expire (this one is %s)", ErrCorrectionClass, corr.Status)
	}
	corr.Status = StatusExpired
	corr.UpdatedAt = c.clock.Now()
	return corr, c.recs.RemoveEntry(project, role, entryKey(id), who, why)
}

// Promote commits a standing-rule correction to the rules pack as a
// reviewed commit, links the correction to the commit, and removes it from
// the state record — a fresh session reads the rule from the pack.
func (c *Corrections) Promote(project, role, id, who, why string) (Correction, error) {
	if err := c.ready(); err != nil {
		return Correction{}, err
	}
	if who == "" || why == "" {
		return Correction{}, fmt.Errorf("%w: who and why are required", ErrCorrection)
	}
	if c.rules == nil {
		return Correction{}, errors.New("corrections: no rules pack writer configured")
	}
	corr, ok, err := c.Get(project, role, id)
	if err != nil {
		return Correction{}, err
	}
	if !ok {
		return Correction{}, fmt.Errorf("%w: %s", ErrCorrectionNotFound, id)
	}
	if corr.Class != ClassStandingRule {
		return Correction{}, fmt.Errorf("%w: only %s corrections promote (this one is %s)", ErrCorrectionClass, ClassStandingRule, corr.Class)
	}
	if corr.Status != StatusActive {
		return Correction{}, fmt.Errorf("%w: only active corrections promote (this one is %s)", ErrCorrectionClass, corr.Status)
	}
	link, err := c.rules.CommitRule(project, promotionText(corr))
	if err != nil {
		return Correction{}, fmt.Errorf("commit rule: %w", err)
	}
	corr.Link = link
	corr.Status = StatusPromoted
	corr.UpdatedAt = c.clock.Now()
	return corr, c.recs.RemoveEntry(project, role, entryKey(id), who, why)
}

// File files a product-defect correction as an Automaton on the project
// plan. The correction keeps the link to the Automaton and its workaround
// in the state record; the handover carries the workaround text.
func (c *Corrections) File(project, role, id, who, why string) (Correction, error) {
	if err := c.ready(); err != nil {
		return Correction{}, err
	}
	if who == "" || why == "" {
		return Correction{}, fmt.Errorf("%w: who and why are required", ErrCorrection)
	}
	if c.plans == nil {
		return Correction{}, errors.New("corrections: no plan filer configured")
	}
	corr, ok, err := c.Get(project, role, id)
	if err != nil {
		return Correction{}, err
	}
	if !ok {
		return Correction{}, fmt.Errorf("%w: %s", ErrCorrectionNotFound, id)
	}
	if corr.Class != ClassProductDefect {
		return Correction{}, fmt.Errorf("%w: only %s corrections file (this one is %s)", ErrCorrectionClass, ClassProductDefect, corr.Class)
	}
	if corr.Status != StatusActive {
		return Correction{}, fmt.Errorf("%w: only active corrections file (this one is %s)", ErrCorrectionClass, corr.Status)
	}
	link, err := c.plans.FileAutomaton(project, strings.TrimSpace(corr.Text), filingBody(corr))
	if err != nil {
		return Correction{}, fmt.Errorf("file automaton: %w", err)
	}
	corr.Link = link
	corr.Status = StatusFiled
	corr.UpdatedAt = c.clock.Now()
	data, err := json.Marshal(corr)
	if err != nil {
		return Correction{}, fmt.Errorf("%w: %v", ErrCorrection, err)
	}
	entry := StateEntry{ID: entryKey(id), Section: SectionCorrection, Text: string(data)}
	return corr, c.recs.SetEntry(project, role, entry, who, why)
}

// HandoverLines renders the corrections a fresh session starts from: the
// unpromoted corrections held in the state record. Promoted standing rules
// are not repeated here — the fresh session reads them from the rules pack.
// A filed defect's workaround rides its line.
func (c *Corrections) HandoverLines(project, role string) ([]string, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	corrs, err := c.List(project, role)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(corrs))
	for _, corr := range corrs {
		line := corr.ID + ": " + corr.Text
		if corr.Evidence != "" {
			line += " (evidence: " + corr.Evidence + ")"
		}
		if corr.Workaround != "" {
			line += " workaround: " + corr.Workaround
		}
		if corr.Link != "" {
			line += " [link: " + corr.Link + "]"
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// promotionText is the rule text committed to the pack.
func promotionText(corr Correction) string {
	if strings.TrimSpace(corr.Evidence) == "" {
		return strings.TrimSpace(corr.Text)
	}
	return strings.TrimSpace(corr.Text) + "\n\nEvidence: " + strings.TrimSpace(corr.Evidence)
}

// filingBody is the Automaton body a filed defect carries.
func filingBody(corr Correction) string {
	if strings.TrimSpace(corr.Evidence) != "" {
		return strings.TrimSpace(corr.Evidence)
	}
	return strings.TrimSpace(corr.Text)
}
