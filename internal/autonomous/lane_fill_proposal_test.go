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
	return CheckLaneFillProposal("", false, spec, fake, nil)
}

func TestLaneFillProposalConfirmedLinePasses(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/a.md": "# Title\n\nConfirmed: yes\n",
	}}
	state, ref := laneFillProposalCheck("Implement docs/plans/a.md please", fake)
	if state != LaneFillProposalConfirmed || ref != "docs/plans/a.md" {
		t.Fatalf("state = %q ref = %q, want confirmed", state, ref)
	}
}

func TestLaneFillProposalLineAbsentRefuses(t *testing.T) {
	cfg := laneFillTestConfig()
	fake := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": "# Title\n\nNo marker here\n"}}
	state, ref := laneFillProposalCheck("see docs/plans/a.md", fake)
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
	state, _ := laneFillProposalCheck("no proposal anywhere in this spec", fake)
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
	state, _ := laneFillProposalCheck("see docs/plans/gone.md", fake)
	if state != LaneFillProposalMissing {
		t.Fatalf("state = %q, want missing", state)
	}
}

func TestLaneFillProposalTwoFilesOnlySecondConfirmedPasses(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{
		"docs/plans/one.md": "Confirmed: no\n",
		"docs/plans/two.md": "Confirmed: yes\n",
	}}
	state, ref := laneFillProposalCheck("docs/plans/one.md and docs/plans/two.md", fake)
	if state != LaneFillProposalConfirmed || ref != "docs/plans/two.md" {
		t.Fatalf("state = %q ref = %q, want confirmed by the second file", state, ref)
	}
}

func TestLaneFillProposalDuplicateReferencesReadOnce(t *testing.T) {
	fake := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": "x\n"}}
	laneFillProposalCheck("docs/plans/a.md, again docs/plans/a.md", fake)
	if len(fake.asked) != 1 {
		t.Fatalf("asked = %v, want one read", fake.asked)
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
		state, _ := laneFillProposalCheck("docs/plans/a.md", fake)
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
		state, _ := laneFillProposalCheck("docs/plans/a.md", fake)
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
	if state, _ := laneFillProposalCheck("docs/plans/a.md", on200); state != LaneFillProposalConfirmed {
		t.Fatalf("line 200 must count, state = %q", state)
	}
	b.Reset()
	for i := 0; i < 200; i++ {
		b.WriteString("filler\n")
	}
	b.WriteString("Confirmed: yes\n") // line 201: does not
	on201 := &laneFillProposalFake{files: map[string]string{"docs/plans/a.md": b.String()}}
	if state, _ := laneFillProposalCheck("docs/plans/a.md", on201); state != LaneFillProposalNotConfirmed {
		t.Fatalf("line 201 must not count, state = %q", state)
	}
}

func TestLaneFillProposalNonImplementationTypesAreExempt(t *testing.T) {
	fake := &laneFillProposalFake{}
	for _, typ := range []string{"research", "operational", "personal", "Research"} {
		state, _ := CheckLaneFillProposal(typ, false, "no proposal", fake, nil)
		if state != LaneFillProposalExempt {
			t.Errorf("type %q: state = %q, want exempt", typ, state)
		}
		c := LaneFillCandidate{ID: "aaaa0003", Status: PRDApproved, ProposalState: state}
		if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, time.Now()); code != "" {
			t.Errorf("type %q: refused with %q", typ, code)
		}
	}
	if state, _ := CheckLaneFillProposal("software", false, "no proposal", fake, nil); state != LaneFillProposalMissing {
		t.Errorf("software must be checked, state = %q", state)
	}
	if state, _ := CheckLaneFillProposal("", true, "no proposal", fake, nil); state != LaneFillProposalExempt {
		t.Errorf("a template is exempt, state = %q", state)
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
	spec := "see /etc/docs/plans/abs.md and ../docs/plans/up.md and docs/plans/../../x/docs/plans/a.md"
	fake := &laneFillProposalFake{files: map[string]string{
		"/etc/docs/plans/abs.md":             "Confirmed: yes\n",
		"../docs/plans/up.md":                "Confirmed: yes\n",
		"docs/plans/../../x/docs/plans/a.md": "Confirmed: yes\n",
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
	state, _ := CheckLaneFillProposal("", false, "docs/plans/a.md", fake, func(ref string, err error) { warned = append(warned, ref) })
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
	if state, _ := CheckLaneFillProposal("", false, "docs/plans/one.md docs/plans/two.md", fake2, nil); state != LaneFillProposalUnknown {
		t.Errorf("state = %q, want unknown", state)
	}
}

func TestLaneFillProposalNoReaderIsUnknown(t *testing.T) {
	state, _ := CheckLaneFillProposal("", false, "docs/plans/a.md", nil, nil)
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
	okPRD.Spec = "build it, see docs/plans/ok.md"
	badPRD := h.addPRD("unconfirmed one", PRDApproved, "", "pi-prompt")
	badPRD.Spec = "build it, see docs/plans/other.md"
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
