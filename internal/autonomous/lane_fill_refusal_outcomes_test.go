package autonomous

import (
	"testing"
	"time"
)

// Tests in this file pin each refusal and pass outcome of the confirmed-proposal
// lane fill rule against the shared fakes (laneFillProposalFake and the
// lane fill harness), with no daemon required. They exercise both halves of the
// rule: CheckLaneFillProposal deciding the candidate's proposal state, and
// EvaluateLaneFillEligibility mapping that state to a refusal code.

// An Automaton whose job is to author the proposal is not refused for its own
// not-yet-confirmed proposal: even with the file present and unconfirmed, the
// writer is exempt and eligible.
func TestRefusalWriterOwnUnconfirmedProposalNotRefused(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/foo.md": "still a draft, no confirmation yet\n",
	}}
	state, _ := laneFillProposalCheck("Write the design proposal to docs/plans/foo.md", fake)
	if state != LaneFillProposalExempt {
		t.Fatalf("state = %q, want exempt (a writer is not gated on its own proposal)", state)
	}
	c := LaneFillCandidate{ID: "bbbb0001", Status: PRDApproved, ProposalState: state}
	if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now()); code != "" {
		t.Fatalf("the proposal writer was refused with %q", code)
	}
}

// A spec that merely mentions docs/plans/README.md, without a line naming a
// proposal, yields no proposal path at all: the file is not even read and the
// candidate is missing, which evaluates to proposal_missing.
func TestRefusalREADMEMentionWithoutProposalLineNamesNoPath(t *testing.T) {
	spec := "Update the plan index docs/plans/README.md before the release"
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/README.md": "Confirmed: yes\n",
	}}
	if got := LaneFillProposalFiles(spec); len(got) != 0 {
		t.Fatalf("LaneFillProposalFiles = %v, want no proposal path for a bare README mention", got)
	}
	state, ref := CheckLaneFillProposal("", false, false, spec, fake, nil)
	if state != LaneFillProposalMissing {
		t.Fatalf("state = %q, want missing (no explicit proposal line)", state)
	}
	if ref != "" {
		t.Fatalf("ref = %q, want empty (no proposal path was named)", ref)
	}
	if len(fake.asked) != 0 {
		t.Fatalf("the mentioned README must not even be read, asked %v", fake.asked)
	}
	c := LaneFillCandidate{ID: "bbbb0002", Status: PRDApproved, ProposalState: state}
	code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now())
	if code != "proposal_missing" {
		t.Fatalf("code = %q, want proposal_missing", code)
	}
}

// An explicit proposal field (field-style naming on a line) and an explicit
// proposal line are each honoured: each one points the check at the named file.
func TestRefusalExplicitFieldAndExplicitLineEachHonoured(t *testing.T) {
	// Field style: "Proposal: docs/plans/a.md" names the file on a proposal word.
	{
		fake := &laneFillProposalFake{files: map[string]string{
			"docs/plans/field.md": "no marker\n",
		}}
		state, ref := laneFillProposalCheck("Proposal: docs/plans/field.md", fake)
		if state != LaneFillProposalNotConfirmed || ref != "docs/plans/field.md" {
			t.Fatalf("state = %q ref = %q, want not_confirmed for the named field file", state, ref)
		}
		if len(fake.asked) != 1 || fake.asked[0] != "docs/plans/field.md" {
			t.Fatalf("the field-named file must be read, asked %v", fake.asked)
		}
	}
	// Sentence style: "the proposal docs/plans/line.md" honours the line too.
	{
		fake := &laneFillProposalFake{files: map[string]string{
			"docs/plans/line.md": "draft only\n",
		}}
		state, ref := laneFillProposalCheck("build it, per the proposal docs/plans/line.md", fake)
		if state != LaneFillProposalNotConfirmed || ref != "docs/plans/line.md" {
			t.Fatalf("state = %q ref = %q, want not_confirmed for the line-named file", state, ref)
		}
	}
}

// A confirmed proposal passes the eligibility rules: no refusal at all.
func TestRefusalConfirmedProposalPassesEligibility(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/ok.md": "# Design\n\nConfirmed: yes\n",
	}}
	state, ref := laneFillProposalCheck("build it, per the proposal docs/plans/ok.md", fake)
	if state != LaneFillProposalConfirmed || ref != "docs/plans/ok.md" {
		t.Fatalf("state = %q ref = %q, want confirmed", state, ref)
	}
	c := LaneFillCandidate{ID: "bbbb0003", Status: PRDApproved, ProposalState: state, ProposalPath: ref}
	if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now()); code != "" {
		t.Fatalf("a confirmed proposal was refused with %q", code)
	}
}

// An unconfirmed proposal is refused with the stable code proposal_not_confirmed.
func TestRefusalUnconfirmedProposalYieldsNotConfirmed(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/draft.md": "# Design\n\nstill open questions\n",
	}}
	state, ref := laneFillProposalCheck("build it, per the proposal docs/plans/draft.md", fake)
	if state != LaneFillProposalNotConfirmed {
		t.Fatalf("state = %q, want not_confirmed", state)
	}
	c := LaneFillCandidate{ID: "bbbb0004", Status: PRDApproved, ProposalState: state, ProposalPath: ref}
	code, text := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now())
	if code != "proposal_not_confirmed" {
		t.Fatalf("code = %q, want proposal_not_confirmed", code)
	}
	if want := "Automaton bbbb0004: proposal docs/plans/draft.md does not carry a Confirmed: yes line"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

// A non-defect implementation Automaton that names no readable proposal is
// refused with the stable code proposal_missing.
func TestRefusalNoReadableProposalYieldsMissing(t *testing.T) {
	fake := &laneFillProposalFake{} // nothing on disk
	state, _ := CheckLaneFillProposal("", false, false, "build it with no proposal file anywhere", fake, nil)
	if state != LaneFillProposalMissing {
		t.Fatalf("state = %q, want missing", state)
	}
	c := LaneFillCandidate{ID: "bbbb0005", Status: PRDApproved, ProposalState: state}
	code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now())
	if code != "proposal_missing" {
		t.Fatalf("code = %q, want proposal_missing", code)
	}
	// The same spec naming a file that is not there is missing too.
	state, _ = CheckLaneFillProposal("", false, false, "build it, per the proposal docs/plans/absent.md", fake, nil)
	if state != LaneFillProposalMissing {
		t.Fatalf("state = %q, want missing for a named but absent file", state)
	}
}

// A defect Automaton with no proposal at all is eligible: it is not refused for
// a missing proposal and takes its lane in on mode.
func TestRefusalDefectWithoutProposalEligible(t *testing.T) {
	fake := &laneFillProposalFake{} // no proposal files exist
	state, _ := CheckLaneFillProposal("", false, true, "fix the crash, no design", fake, nil)
	if state != LaneFillProposalExempt {
		t.Fatalf("state = %q, want exempt for a defect", state)
	}
	if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), LaneFillCandidate{
		ID: "bbbb0006", Status: PRDApproved, ProposalState: state,
	}, time.Now()); code != "" {
		t.Fatalf("a defect was refused with %q", code)
	}

	// Snapshot level: a defect with no proposal starts in on mode while an
	// identical non-defect without a proposal is refused with proposal_missing.
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	h.c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: h.now}})
	h.c.SetLaneFillProposalReader(&laneFillProposalFake{})

	defect := h.addPRD("fix the crash", PRDApproved, "gpu", "pi-prompt")
	defect.Kind = PRDKindBug
	impl := h.addPRD("rebuild without a design", PRDApproved, "gpu", "pi-prompt")
	if err := h.m.store.SavePRD(defect); err != nil {
		t.Fatal(err)
	}
	if err := h.m.store.SavePRD(impl); err != nil {
		t.Fatal(err)
	}

	h.poll()

	reasons := laneFillEligibilityReasons(h)
	if reasons[defect.ID] != "" {
		t.Fatalf("a defect with no proposal was refused: %v", reasons)
	}
	if reasons[impl.ID] != "proposal_missing" {
		t.Fatalf("the non-defect without a proposal = %v, want proposal_missing", reasons)
	}
}

// An unchanged refusal is not repeated across polls: consecutive polls fold into
// one decision record with a growing repeat count, not one new record per poll.
func TestRefusalUnchangedNotRepeatedAcrossPolls(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	h.c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: h.now}})
	h.c.SetLaneFillProposalReader(&laneFillProposalFake{files: map[string]string{
		"docs/plans/static.md": "never confirmed\n",
	}})

	bad := h.addPRD("an unconfirmed proposal", PRDApproved, "gpu", "pi-prompt")
	bad.Spec = "build it, per the proposal docs/plans/static.md"
	if err := h.m.store.SavePRD(bad); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		h.poll()
		h.now = h.now.Add(time.Minute)
	}

	records := 0
	var repeat int
	for _, d := range h.status().Decisions {
		if d.PRDID != bad.ID {
			continue
		}
		if d.EligibilityReason != LaneFillReasonProposalNotConfirmed {
			continue
		}
		records++
		repeat = d.RepeatCount
	}
	if records != 1 {
		t.Fatalf("decision records = %d, want exactly one unchanged refusal folded across polls", records)
	}
	if repeat != 2 {
		t.Fatalf("RepeatCount = %d, want 2 (the second poll folded in, not a new record)", repeat)
	}
}
