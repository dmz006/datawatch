package autonomous

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"
)

// laneFillProposalFake serves proposal heads from a map and records every
// path it was asked for.
type laneFillProposalFake struct {
	files map[string]string
	errs  map[string]error
	asked []string
}

func (f *laneFillProposalFake) ReadHead(rel string) ([]byte, error) {
	f.asked = append(f.asked, rel)
	if err, ok := f.errs[rel]; ok {
		return nil, err
	}
	if s, ok := f.files[rel]; ok {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("open %s: %w", rel, fs.ErrNotExist)
}

func laneFillProposalCheck(spec string, fake *laneFillProposalFake) (string, string) {
	return CheckLaneFillProposal("", false, false, spec, fake, nil)
}

func TestLaneFillProposalConfirmedLinePasses(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/a.md": "# Title\n\nConfirmed: yes\n",
	}}
	state, ref := laneFillProposalCheck("Implement the design proposal docs/plans/a.md", fake)
	if state != LaneFillProposalConfirmed || ref != "docs/plans/a.md" {
		t.Fatalf("state = %q ref = %q, want confirmed", state, ref)
	}
}

// A docs/plans path that is only mentioned, without a proposal field or line
// naming one, is not an Automaton's design proposal: the spec names no
// proposal and is refused, not confirmed off the file that was merely cited.
func TestLaneFillProposalMentionedDocsPathIsNotAProposal(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/a.md": "Confirmed: yes\n",
	}}
	state, _ := laneFillProposalCheck("Implement docs/plans/a.md as described", fake)
	if state != LaneFillProposalMissing {
		t.Fatalf("state = %q, want missing (a merely mentioned docs path is not the proposal)", state)
	}
	if len(fake.asked) != 0 {
		t.Fatalf("the merely mentioned file must not even be read, asked %v", fake.asked)
	}
}

// The plan folder's README is never an Automaton's design proposal, whether a
// proposal line names it on its own or names it alongside a real one.
func TestLaneFillProposalREADMEIsNeverAProposal(t *testing.T) {
	// Named alone: no proposal at all.
	{
		fake := &laneFillProposalFake{files: map[string]string{"docs/plans/README.md": "Confirmed: yes\n"}}
		state, _ := laneFillProposalCheck("see the proposal docs/plans/README.md", fake)
		if state != LaneFillProposalMissing {
			t.Fatalf("state = %q, want missing (the README is not a proposal)", state)
		}
		if len(fake.asked) != 0 {
			t.Fatalf("the README must not even be read, asked %v", fake.asked)
		}
	}
	// Named alongside a real one: the real one decides, the README is ignored.
	{
		fake := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": "Confirmed: yes\n"}}
		state, ref := laneFillProposalCheck("see the proposal docs/plans/README.md or docs/plans/a.md", fake)
		if state != LaneFillProposalConfirmed || ref != "docs/plans/a.md" {
			t.Fatalf("state = %q ref = %q, want confirmed by the real proposal", state, ref)
		}
	}
}

func TestLaneFillProposalLineAbsentRefuses(t *testing.T) {
	cfg := laneFillTestConfig()
	fake := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": "# Title\n\nNo marker here\n"}}
	state, ref := laneFillProposalCheck("see the proposal docs/plans/a.md", fake)
	if state != LaneFillProposalNotConfirmed {
		t.Fatalf("state = %q, want not_confirmed", state)
	}
	c := LaneFillCandidate{ID: "aaaa0001", Status: PRDApproved, ProposalState: state, ProposalPath: ref}
	code, text := EvaluateLaneFillEligibility(cfg, c, time.Now())
	if code != "proposal_not_confirmed" {
		t.Fatalf("code = %q", code)
	}
	if want := "Automaton aaaa0001: proposal docs/plans/a.md does not carry a Confirmed: yes line"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

func TestLaneFillProposalNoFileNamedRefuses(t *testing.T) {
	cfg := laneFillTestConfig()
	fake := &laneFillProposalFake{}
	state, _ := laneFillProposalCheck("no proposal file in this spec", fake)
	if state != LaneFillProposalMissing {
		t.Fatalf("state = %q, want missing", state)
	}
	c := LaneFillCandidate{ID: "aaaa0002", Status: PRDDraft, ProposalState: state}
	code, text := EvaluateLaneFillEligibility(cfg, c, time.Now())
	if code != "proposal_missing" {
		t.Fatalf("code = %q", code)
	}
	if want := "Automaton aaaa0002: the spec names no readable proposal file"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

func TestLaneFillProposalFileNamedButMissingOnDiskRefuses(t *testing.T) {
	fake := &laneFillProposalFake{}
	state, _ := laneFillProposalCheck("see the proposal docs/plans/gone.md", fake)
	if state != LaneFillProposalMissing {
		t.Fatalf("state = %q, want missing", state)
	}
}

func TestLaneFillProposalTwoFilesOnlySecondConfirmedPasses(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/one.md": "Confirmed: no\n",
		"docs/plans/two.md": "Confirmed: yes\n",
	}}
	state, ref := laneFillProposalCheck("the proposal is docs/plans/one.md or docs/plans/two.md", fake)
	if state != LaneFillProposalConfirmed || ref != "docs/plans/two.md" {
		t.Fatalf("state = %q ref = %q, want confirmed by the second file", state, ref)
	}
}

func TestLaneFillProposalDuplicateReferencesReadOnce(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": "x\n"}}
	laneFillProposalCheck("see the proposal docs/plans/a.md, and again docs/plans/a.md", fake)
	if len(fake.asked) != 1 {
		t.Fatalf("asked = %v, want one read", fake.asked)
	}
}

// An Automaton whose job is to author the design proposal is never gated on
// the Confirmed line of the proposal it is producing: even a spec that names a
// not-yet-present proposal file is exempt rather than refused.
func TestLaneFillProposalWritingAutomatonIsExempt(t *testing.T) {
	fake := &laneFillProposalFake{} // the proposal file the writer would create does not exist yet
	for _, spec := range []string{
		"Write the design proposal to docs/plans/foo.md",
		"Draft the proposal docs/plans/foo.md",
		"Author the docs/plans/foo.md proposal",
	} {
		state, _ := laneFillProposalCheck(spec, fake)
		if state != LaneFillProposalExempt {
			t.Errorf("spec %q: state = %q, want exempt (a writer is not gated on its own proposal)", spec, state)
		}
	}
}

func TestLaneFillProposalNoAndUnconfirmedDoNotPass(t *testing.T) {
	for _, body := range []string{
		"Confirmed: no\n",
		"Unconfirmed: yes\n",
		"Unconfirmed\n",
		"Confirmed: yesterday\n",
		"note: Confirmed: yes\n",
		"Confirmed yes\n",
	} {
		fake := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": body}}
		state, _ := laneFillProposalCheck("the proposal docs/plans/a.md", fake)
		if state != LaneFillProposalNotConfirmed {
			t.Errorf("body %q: state = %q, want not_confirmed", body, state)
		}
	}
}

func TestLaneFillProposalDecoratedLinePasses(t *testing.T) {
	for _, body := range []string{
		"**Confirmed:** yes\n",
		"**Confirmed: yes**\n",
		"_Confirmed: Yes_\n",
		"> Confirmed: yes\n",
		"- confirmed: YES\r\n",
		"\"Confirmed: yes\"\n",
	} {
		fake := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": "# T\n\n" + body}}
		state, _ := laneFillProposalCheck("the proposal docs/plans/a.md", fake)
		if state != LaneFillProposalConfirmed {
			t.Errorf("body %q: state = %q, want confirmed", body, state)
		}
	}
}

func TestLaneFillProposalLineAfter200DoesNotCount(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 199; i++ {
		b.WriteString("filler\n")
	}
	b.WriteString("Confirmed: yes\n") // line 200: counts
	on200 := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": b.String()}}
	if state, _ := laneFillProposalCheck("the proposal docs/plans/a.md", on200); state != LaneFillProposalConfirmed {
		t.Fatalf("line 200 must count, state = %q", state)
	}
	b.Reset()
	for i := 0; i < 200; i++ {
		b.WriteString("filler\n")
	}
	b.WriteString("Confirmed: yes\n") // line 201: does not
	on201 := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": b.String()}}
	if state, _ := laneFillProposalCheck("the proposal docs/plans/a.md", on201); state != LaneFillProposalNotConfirmed {
		t.Fatalf("line 201 must not count, state = %q", state)
	}
}

func TestLaneFillProposalNonImplementationTypesAreExempt(t *testing.T) {
	fake := &laneFillProposalFake{}
	for _, typ := range []string{"research", "operational", "personal", "Research"} {
		state, _ := CheckLaneFillProposal(typ, false, false, "no proposal file", fake, nil)
		if state != LaneFillProposalExempt {
			t.Errorf("type %q: state = %q, want exempt", typ, state)
		}
		c := LaneFillCandidate{ID: "aaaa0003", Status: PRDApproved, ProposalState: state}
		if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now()); code != "" {
			t.Errorf("type %q: refused with %q", typ, code)
		}
	}
	if state, _ := CheckLaneFillProposal("software", false, false, "no proposal file", fake, nil); state != LaneFillProposalMissing {
		t.Errorf("software must be checked, state = %q", state)
	}
	if state, _ := CheckLaneFillProposal("", true, false, "no proposal file", fake, nil); state != LaneFillProposalExempt {
		t.Errorf("a template is exempt, state = %q", state)
	}
}

// A defect Automaton is not a design implementation and has no design proposal
// to confirm, so it is exempt even when its spec names no proposal file.
func TestLaneFillProposalDefectIsExempt(t *testing.T) {
	fake := &laneFillProposalFake{}
	if state, _ := CheckLaneFillProposal("software", false, true, "no proposal file", fake, nil); state != LaneFillProposalExempt {
		t.Fatalf("state = %q, want exempt (a defect is not gated on a proposal)", state)
	}
	c := LaneFillCandidate{ID: "aaaa0006", Status: PRDApproved, ProposalState: LaneFillProposalExempt}
	if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now()); code != "" {
		t.Errorf("a defect must not be refused, got %q", code)
	}
}

func TestLaneFillProposalConfigKeyFalseDisablesRule(t *testing.T) {
	off := false
	cfg := laneFillTestConfig()
	cfg.RequireConfirmedProposal = &off
	for _, st := range []string{LaneFillProposalMissing, LaneFillProposalNotConfirmed} {
		c := LaneFillCandidate{ID: "aaaa0004", Status: PRDApproved, ProposalState: st, ProposalPath: "docs/plans/a.md"}
		if code, _ := EvaluateLaneFillEligibility(cfg, c, time.Now()); code != "" {
			t.Errorf("state %s with the key false: refused with %q", st, code)
		}
		if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now()); code == "" {
			t.Errorf("state %s with the default: not refused", st)
		}
	}
}

func TestLaneFillProposalUnsafePathsAreNeverReadAndCountAsUnreadable(t *testing.T) {
	spec := "the proposal is /etc/docs/plans/abs.md and not ../docs/plans/up.md"
	fake := &laneFillProposalFake{files: map[string]string{
		"/etc/docs/plans/abs.md": "Confirmed: yes\n",
		"../docs/plans/up.md":    "Confirmed: yes\n",
	}}
	state, _ := laneFillProposalCheck(spec, fake)
	if state != LaneFillProposalMissing {
		t.Fatalf("state = %q, want missing", state)
	}
	if len(fake.asked) != 0 {
		t.Fatalf("unsafe references must not reach the reader, asked %v", fake.asked)
	}
}

func TestLaneFillProposalOtherReadErrorFailsOpen(t *testing.T) {
	boom := errors.New("permission denied")
	fake := &laneFillProposalFake{errs: map[string]error{"docs/plans/a.md": boom}}
	var warned []string
	state, _ := CheckLaneFillProposal("", false, false, "the proposal docs/plans/a.md", fake, func(ref string, err error) { warned = append(warned, ref) })
	if state != LaneFillProposalUnknown {
		t.Fatalf("state = %q, want unknown", state)
	}
	if len(warned) != 1 {
		t.Errorf("warned = %v, want one", warned)
	}
	c := LaneFillCandidate{ID: "aaaa0005", Status: PRDApproved, ProposalState: state}
	if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now()); code != "" {
		t.Errorf("unknown must not refuse, got %q", code)
	}
	// An unreadable second file keeps the whole check open even when the first
	// is readable without the line.
	fake2 := &laneFillProposalFake{
		files: map[string]string{"docs/plans/one.md": "nothing\n"},
		errs:  map[string]error{"docs/plans/two.md": boom},
	}
	if state, _ := CheckLaneFillProposal("", false, false, "the proposal docs/plans/one.md or docs/plans/two.md", fake2, nil); state != LaneFillProposalUnknown {
		t.Errorf("state = %q, want unknown", state)
	}
}

func TestLaneFillProposalNoReaderIsUnknown(t *testing.T) {
	state, _ := CheckLaneFillProposal("", false, false, "the proposal docs/plans/a.md", nil, nil)
	if state != LaneFillProposalUnknown {
		t.Fatalf("state = %q, want unknown", state)
	}
}

func TestLaneFillProposalSnapshotRefusesAndNeverStartsInOnMode(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	h.c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: h.now}})
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/ok.md":    "Confirmed: yes\n",
		"docs/plans/other.md": "draft only\n",
	}}
	h.c.SetLaneFillProposalReader(fake)

	okPRD := h.addPRD("confirmed one", PRDApproved, "", "pi-prompt")
	okPRD.Spec = "build it, per the proposal docs/plans/ok.md"
	badPRD := h.addPRD("unconfirmed one", PRDApproved, "", "pi-prompt")
	badPRD.Spec = "build it, per the proposal docs/plans/other.md"
	nonePRD := h.addPRD("no proposal", PRDApproved, "", "pi-prompt")
	nonePRD.Spec = "build it"
	resPRD := h.addPRD("research one", PRDApproved, "", "pi-prompt")
	resPRD.Spec = "look into it"
	resPRD.Type = "research"
	for _, p := range []*PRD{okPRD, badPRD, nonePRD, resPRD} {
		if err := h.m.store.SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}

	h.poll()

	reasons := map[string]string{}
	for _, d := range h.status().Decisions {
		if d.EligibilityReason != "" {
			reasons[d.PRDID] = d.EligibilityReason
		}
	}
	if reasons[badPRD.ID] != LaneFillReasonProposalNotConfirmed {
		t.Errorf("unconfirmed: reasons = %v", reasons)
	}
	if reasons[nonePRD.ID] != LaneFillReasonProposalMissing {
		t.Errorf("no proposal: reasons = %v", reasons)
	}
	if _, bad := reasons[okPRD.ID]; bad {
		t.Errorf("confirmed one was refused: %v", reasons)
	}
	if _, bad := reasons[resPRD.ID]; bad {
		t.Errorf("research one was refused: %v", reasons)
	}
	for _, call := range h.fake.all() {
		if strings.Contains(call, badPRD.ID) || strings.Contains(call, nonePRD.ID) {
			t.Errorf("a refused Automaton was started: %v", h.fake.all())
		}
	}
	if q := h.status().QueuedEligible; q.GPU != 2 {
		t.Errorf("queued eligible = %+v, want the confirmed and the research Automaton counted", q)
	}
}

// A defect (bug) Automaton with no design proposal is eligible: it is not
// refused with proposal_missing and takes its lane in on mode.
func TestLaneFillProposalDefectSnapshotIsEligible(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	h.c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: h.now}})
	h.c.SetLaneFillProposalReader(&laneFillProposalFake{}) // no proposal files at all

	defect := h.addPRD("fix the defect", PRDApproved, "", "pi-prompt")
	defect.Spec = "fix the defect"
	defect.Kind = PRDKindBug
	if err := h.m.store.SavePRD(defect); err != nil {
		t.Fatal(err)
	}

	h.poll()

	if reasons := laneFillEligibilityReasons(h); reasons[defect.ID] != "" {
		t.Fatalf("a defect with no proposal was refused: %v", reasons)
	}
}

// An unchanged proposal refusal folds into one record across polls (a growing
// repeat count), not a new record every poll.
func TestLaneFillProposalRefusalFoldsAcrossPolls(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	h.c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: h.now}})
	h.c.SetLaneFillProposalReader(&laneFillProposalFake{files: map[string]string{
		"docs/plans/a.md": "still a draft\n",
	}})

	bad := h.addPRD("unconfirmed one", PRDApproved, "gpu", "pi-prompt")
	bad.Spec = "build it, per the proposal docs/plans/a.md"
	if err := h.m.store.SavePRD(bad); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		h.poll()
		h.now = h.now.Add(time.Minute)
	}

	st := h.status()
	var found *LaneFillDecision
	for i := range st.Decisions {
		d := &st.Decisions[i]
		if d.PRDID == bad.ID && d.EligibilityReason == LaneFillReasonProposalNotConfirmed {
			found = d
		}
	}
	if found == nil {
		t.Fatalf("expected a proposal_not_confirmed decision for %s: %+v", bad.ID, st.Decisions)
	}
	if found.RepeatCount != 3 {
		t.Fatalf("RepeatCount = %d, want 3 (one folded record, not a new one per poll)", found.RepeatCount)
	}
}

func laneFillEligibilityReasons(h *laneFillHarness) map[string]string {
	out := map[string]string{}
	for _, d := range h.status().Decisions {
		if d.EligibilityReason != "" {
			out[d.PRDID] = d.EligibilityReason
		}
	}
	return out
}
