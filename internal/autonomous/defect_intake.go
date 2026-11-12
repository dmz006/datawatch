// Defect intake (autonomous.defect_intake_automation): when an Automaton is
// filed with kind bug and the setting names an Automation, the filing is
// offered to that Automation's bugs stage. The offer goes through the
// DefectIntake interface so a fake can stand in for the daemon adapter;
// cmd/datawatch/main.go installs the real one over the Automation's
// member insert (Manager.InsertMember, automation_insert.go), where it
// lands as a held PendingInsert on a non-draft Automation (operator
// approves) or as a member at once on a draft.
//
// The outcome of the intake — offered, not_configured, skipped or error —
// is recorded on the Automaton itself (PRD.DefectIntake) so the surfaces
// can show it. The setting names an Automation explicitly; when it is
// empty the filing is recorded not_configured and nothing is offered.
// Manager never guesses an Automation.

package autonomous

import (
	"context"
	"strings"
	"time"
)

// Defect intake outcomes recorded on the filed Automaton (PRD.DefectIntake).
const (
	// DefectIntakeOffered — the defect was offered to the named Automation.
	DefectIntakeOffered = "offered"
	// DefectIntakeNotConfigured — autonomous.defect_intake_automation is
	// empty: nothing is offered (Manager never guesses an Automation).
	DefectIntakeNotConfigured = "not_configured"
	// DefectIntakeSkipped — filed Automaton is not a kind bug: intake only
	// handles bug filings.
	DefectIntakeSkipped = "skipped"
	// DefectIntakeError — the offer was attempted and failed; the message
	// carries the cause.
	DefectIntakeError = "error"
)

// defectIntakeOfferTimeout bounds one Offer call so a slow or wedged
// intake implementation can never hold the filing path hostage.
const defectIntakeOfferTimeout = 30 * time.Second

// DefectIntakeRecord is the recorded outcome of the defect intake for one
// filed Automaton. Surfaces read it off the Automaton (PRD.DefectIntake);
// a nil record means the intake path never ran for it.
type DefectIntakeRecord struct {
	// Outcome is one of DefectIntakeOffered, DefectIntakeNotConfigured,
	// DefectIntakeSkipped or DefectIntakeError.
	Outcome string `json:"outcome"`
	// Automation is the name of the Automation the setting named (empty for
	// a not_configured record).
	Automation string `json:"automation,omitempty"`
	// Message carries the offer failure for an error record.
	Message string    `json:"message,omitempty"`
	At      time.Time `json:"at"`
}

// Defect is what the intake sees of the filed Automaton: enough to place
// it, nothing it must not. Id is the Automaton's ID — the offer is
// idempotent per it.
type Defect struct {
	ID         string // the Automaton (PRD) ID
	Title      string
	Kind       string // PRDKindBug when filed as a defect
	Summary    string // the filed spec
	ProjectDir string
}

// DefectIntake places a filed defect with the Automation that takes defect
// intake. Implementations must be safe to call more than once per defect
// ID — Manager already de-duplicates the call (see intakeDefect), and the
// daemon adapter's duplicate insert is additionally refused by the
// Automation store itself.
type DefectIntake interface {
	Offer(ctx context.Context, d Defect) error
}

// SetDefectIntake installs the defect intake implementation; nil disables
// offers (a kind-bug filing with the Automation named then records the
// outcome as an error naming the missing intake). Guarded by m.mu.
func (m *Manager) SetDefectIntake(fi DefectIntake) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defectIntake = fi
}

// DefectIntake returns the installed intake implementation (nil = none).
func (m *Manager) DefectIntake() DefectIntake {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.defectIntake
}

// SetDefectIntakeAutomation names the Automation this installation takes
// defect intake on (autonomous.defect_intake_automation), in the live
// Manager config mirror like PriorityByKind. Empty clears it: filings are
// then recorded not_configured and nothing is offered — never guess an
// Automation. Guarded by m.mu.
func (m *Manager) SetDefectIntakeAutomation(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.DefectIntakeAutomation = strings.TrimSpace(name)
}

// DefectIntakeAutomation returns the named Automation (already trimmed)
// from the live config mirror.
func (m *Manager) DefectIntakeAutomation() string {
	cfg := m.Config()
	return strings.TrimSpace(cfg.DefectIntakeAutomation)
}

// intakeDefect is the defect-filing hook: it decides the outcome for the
// filed Automaton, offers it when due and records the outcome on the
// record (PRD.DefectIntake) with a save either way, so the surfaces can
// show what happened. It never fails the filing: the outcome — including
// an offer error and its message — lands on the record instead.
func (m *Manager) intakeDefect(prd *PRD) {
	if prd == nil {
		return
	}
	// Intake only handles bug filings; anything else is skipped whatever
	// the setting says.
	if EffectiveKind(prd, nil) != PRDKindBug {
		m.recordDefectIntake(prd, DefectIntakeSkipped, m.DefectIntakeAutomation(), "")
		return
	}
	name := m.DefectIntakeAutomation()
	if name == "" {
		m.recordDefectIntake(prd, DefectIntakeNotConfigured, "", "")
		return
	}
	// Idempotent per defect ID: once this filing was offered, a repeated
	// intake pass (re-entry, a caller retrying the filing path) must not
	// offer it again — the Automation store would refuse the duplicate
	// insert anyway, and the error must not clobber the offered record.
	if rec := prd.DefectIntake; rec != nil && rec.Outcome == DefectIntakeOffered {
		return
	}
	fi := m.DefectIntake()
	if fi == nil {
		m.recordDefectIntake(prd, DefectIntakeError, name, "no defect intake is installed")
		return
	}
	defect := Defect{
		ID:         prd.ID,
		Title:      prd.Title,
		Kind:       EffectiveKind(prd, nil),
		Summary:    prd.Spec,
		ProjectDir: prd.ProjectDir,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defectIntakeOfferTimeout)
	defer cancel()
	if err := fi.Offer(ctx, defect); err != nil {
		m.recordDefectIntake(prd, DefectIntakeError, name, err.Error())
		return
	}
	m.recordDefectIntake(prd, DefectIntakeOffered, name, "")
}

// recordDefectIntake writes the outcome onto the Automaton record and
// persists it (a persistence failure is logged the same way the other
// best-effort CreatePRD updates are; the outcome is already set on the
// in-memory record the caller holds).
func (m *Manager) recordDefectIntake(prd *PRD, outcome, automation, message string) {
	rec := &DefectIntakeRecord{Outcome: outcome, Automation: automation, Message: message, At: time.Now()}
	m.store.WithLock(func() {
		prd.DefectIntake = rec
		prd.UpdatedAt = rec.At
	})
	_ = m.store.SavePRD(prd)
}
