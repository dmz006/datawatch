package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The filing spec that derives kind bug (DeriveKind whole-word keywords)
// and one that does not.
const (
	defectIntakeBugSpec     = "fix the login crash: users report the session handler flak and no longer completes"
	defectIntakeFeatureSpec = "write an operator howto for the new billing page layout"
)

// fakeDefectIntake is the test stand-in for DefectIntake (the daemon
// installs defectIntakeAdapter over the Automation member insert).
type fakeDefectIntake struct {
	calls []Defect
	err   error
}

func (f *fakeDefectIntake) Offer(_ context.Context, d Defect) error {
	f.calls = append(f.calls, d)
	return f.err
}

// freshDefectIntakeManager returns a Manager with the defect-intake knobs
// left at their zero defaults (no Automation named, no intake installed).
func freshDefectIntakeManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// filedDefectIntake creates one Automaton under the filing path and
// returns the record the surfaces would read.
func filedDefectIntake(t *testing.T, m *Manager, spec string) *PRD {
	t.Helper()
	prd, err := m.CreatePRD(spec, t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("store dropped the filed Automaton %q", prd.ID)
	}
	return got
}

func recordedDefectIntake(t *testing.T, p *PRD) *DefectIntakeRecord {
	t.Helper()
	if p.DefectIntake == nil {
		t.Fatalf("the filed Automaton has no DefectIntake record; outcome was never recorded")
	}
	return p.DefectIntake
}

// A kind-bug filing must be offered to the Automation named by
// autonomous.defect_intake_automation, and the Automaton must carry the
// offered outcome (name included) for the surfaces to show it.
func TestDefectIntake_BugFilingOfferedWhenAutomatonNamed(t *testing.T) {
	m := freshDefectIntakeManager(t)
	fi := &fakeDefectIntake{}
	m.SetDefectIntake(fi)
	m.SetDefectIntakeAutomation("Bugs")

	got := filedDefectIntake(t, m, defectIntakeBugSpec)

	if EffectiveKind(got, nil) != PRDKindBug {
		t.Fatalf("EffectiveKind = %q; the test filing must derive kind bug", EffectiveKind(got, nil))
	}
	if len(fi.calls) != 1 {
		t.Fatalf("fake intake Offer calls = %d; want exactly 1", len(fi.calls))
	}
	if fi.calls[0].ID != got.ID {
		t.Fatalf("Offer defect ID = %q; want the filed Automaton ID %q", fi.calls[0].ID, got.ID)
	}
	if fi.calls[0].Kind != PRDKindBug {
		t.Fatalf("Offer defect Kind = %q; want %q", fi.calls[0].Kind, PRDKindBug)
	}
	rec := recordedDefectIntake(t, got)
	if rec.Outcome != DefectIntakeOffered {
		t.Fatalf("recorded outcome = %q; want %q", rec.Outcome, DefectIntakeOffered)
	}
	if rec.Automation != "Bugs" {
		t.Fatalf("recorded automation = %q; want the named Automation %q", rec.Automation, "Bugs")
	}
	if rec.Message != "" {
		t.Fatalf("recorded message = %q; want empty on an offered outcome", rec.Message)
	}
}

// With the setting empty the filing must not be offered anywhere — the
// manager never guesses an Automation — and the Automaton must carry the
// not_configured outcome.
func TestDefectIntake_BugFilingNotConfiguredWhenNameEmpty(t *testing.T) {
	m := freshDefectIntakeManager(t)
	fi := &fakeDefectIntake{}
	m.SetDefectIntake(fi)
	// No SetDefectIntakeAutomation call: autonomous.defect_intake_automation is empty.

	got := filedDefectIntake(t, m, defectIntakeBugSpec)

	if len(fi.calls) != 0 {
		t.Fatalf("fake intake Offer calls = %d; want 0 (nothing may be offered when the setting is empty)", len(fi.calls))
	}
	rec := recordedDefectIntake(t, got)
	if rec.Outcome != DefectIntakeNotConfigured {
		t.Fatalf("recorded outcome = %q; want %q", rec.Outcome, DefectIntakeNotConfigured)
	}
	if rec.Automation != "" {
		t.Fatalf("recorded automation = %q; want empty (there is no named Automation)", rec.Automation)
	}

	// A whitespace-only name is empty too: still not_configured, no call.
	m.SetDefectIntakeAutomation("   ")
	got = filedDefectIntake(t, m, defectIntakeBugSpec)
	if len(fi.calls) != 0 {
		t.Fatalf("fake intake Offer calls = %d; want 0 for a whitespace-only name", len(fi.calls))
	}
	if rec := recordedDefectIntake(t, got); rec.Outcome != DefectIntakeNotConfigured {
		t.Fatalf("recorded outcome = %q for a whitespace-only name; want %q", rec.Outcome, DefectIntakeNotConfigured)
	}
}

// Intake handles bug filings only: a non-bug Automaton is recorded
// skipped and must not reach the intake, even with an Automation named.
func TestDefectIntake_NonBugFilingSkipped(t *testing.T) {
	m := freshDefectIntakeManager(t)
	fi := &fakeDefectIntake{}
	m.SetDefectIntake(fi)
	m.SetDefectIntakeAutomation("Bugs")

	got := filedDefectIntake(t, m, defectIntakeFeatureSpec)

	if EffectiveKind(got, nil) == PRDKindBug {
		t.Fatalf("the feature test spec derived kind bug; the test premise is broken")
	}
	if len(fi.calls) != 0 {
		t.Fatalf("fake intake Offer calls = %d; want 0 for a non-bug filing", len(fi.calls))
	}
	rec := recordedDefectIntake(t, got)
	if rec.Outcome != DefectIntakeSkipped {
		t.Fatalf("recorded outcome = %q; want %q", rec.Outcome, DefectIntakeSkipped)
	}
}

// A failing offer must record the error outcome — with the message naming
// the cause — without failing the filing itself.
func TestDefectIntake_BugFilingErrorOutcomeWhenOfferFails(t *testing.T) {
	m := freshDefectIntakeManager(t)
	cause := errors.New("automation store refused the insert: duplicate member")
	fi := &fakeDefectIntake{err: cause}
	m.SetDefectIntake(fi)
	m.SetDefectIntakeAutomation("Bugs")

	// The filing itself must still succeed.
	got := filedDefectIntake(t, m, defectIntakeBugSpec)

	if len(fi.calls) != 1 {
		t.Fatalf("fake intake Offer calls = %d; want 1", len(fi.calls))
	}
	rec := recordedDefectIntake(t, got)
	if rec.Outcome != DefectIntakeError {
		t.Fatalf("recorded outcome = %q; want %q", rec.Outcome, DefectIntakeError)
	}
	if rec.Automation != "Bugs" {
		t.Fatalf("recorded automation = %q; want the named Automation %q", rec.Automation, "Bugs")
	}
	if !strings.Contains(rec.Message, cause.Error()) {
		t.Fatalf("recorded message %q does not name the cause %q", rec.Message, cause.Error())
	}

	// An error is not terminal: once the offer side is fixed, the next
	// intake pass for the same defect ID may offer again (the Automation
	// store's own duplicate check is the backstop against a true repeat).
	fi.err = nil
	m.intakeDefect(got)
	if len(fi.calls) != 2 {
		t.Fatalf("fake intake Offer calls after the fix = %d; want 2 (error retries until offered)", len(fi.calls))
	}
	if rec := recordedDefectIntake(t, got); rec.Outcome != DefectIntakeOffered {
		t.Fatalf("recorded outcome after the retry = %q; want %q", rec.Outcome, DefectIntakeOffered)
	}
}

// Once the bug filing has been offered, repeated intake passes for the
// same defect ID must not offer it again — the offer is idempotent per
// Automaton ID, and an earlier failed attempt must never clobber the
// offered record.
func TestDefectIntake_OfferIdempotentPerDefectID(t *testing.T) {
	m := freshDefectIntakeManager(t)
	fi := &fakeDefectIntake{}
	m.SetDefectIntake(fi)
	m.SetDefectIntakeAutomation("Bugs")
	_ = filedDefectIntake(t, m, defectIntakeBugSpec)
	got, ok := m.Store().GetPRD(fi.calls[0].ID)
	if !ok {
		t.Fatalf("store dropped the filed Automaton")
	}

	// Re-enter the intake path twice more for the very same defect ID: the
	// offered short-circuit must keep the fake out.
	for pass := 0; pass < 2; pass++ {
		m.intakeDefect(got)
	}

	if len(fi.calls) != 1 {
		t.Fatalf("fake intake Offer calls = %d after 3 intake passes; want exactly 1 (idempotent per defect ID)", len(fi.calls))
	}
	rec := recordedDefectIntake(t, got)
	if rec.Outcome != DefectIntakeOffered || rec.Automation != "Bugs" {
		t.Fatalf("recorded outcome = %q (automation %q) after the repeats; want %q on %q", rec.Outcome, rec.Automation, DefectIntakeOffered, "Bugs")
	}
}

// No intake installed with the Automation named: an error outcome naming
// the missing implementation, and no panic.
func TestDefectIntake_BugFilingErrorWhenNoIntakeInstalled(t *testing.T) {
	m := freshDefectIntakeManager(t)
	// No SetDefectIntake call at all.
	m.SetDefectIntakeAutomation("Bugs")

	got := filedDefectIntake(t, m, defectIntakeBugSpec)

	rec := recordedDefectIntake(t, got)
	if rec.Outcome != DefectIntakeError {
		t.Fatalf("recorded outcome = %q; want %q", rec.Outcome, DefectIntakeError)
	}
	if !strings.Contains(rec.Message, "no defect intake is installed") {
		t.Fatalf("recorded message = %q; want the missing-intake cause", rec.Message)
	}
}
