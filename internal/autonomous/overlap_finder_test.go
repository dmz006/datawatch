package autonomous

import (
	"errors"
	"strings"
	"testing"
)

// overlapFinderPlan is the new plan's title: ten content terms so
// partial candidates score strictly below 1.0.
const overlapFinderPlan = "alpha beta gamma delta epsilon zeta eta theta iota kappa"

// overlapFillers are neutral candidate-only terms.
var overlapFillers = []string{"omega1", "omega2", "omega3", "omega4", "omega5", "omega6", "omega7", "omega8", "omega9"}

// planHead is the first keep terms of the plan plus extra filler
// terms, so the score is keep / (keep + extra).
func planHead(keep, extra int) string {
	terms := strings.Fields(overlapFinderPlan)[:keep]
	for i := 0; i < extra; i++ {
		terms = append(terms, overlapFillers[i])
	}
	return strings.Join(terms, " ")
}

// fakeOverlapLister is the automata lister for finder tests.
type fakeOverlapLister struct {
	recs   []OverlapAutomaton
	panics bool
}

func (f fakeOverlapLister) ListAutomata() []OverlapAutomaton {
	if f.panics {
		panic("lister exploded")
	}
	return f.recs
}

// fakeOverlapRegistry is the registry source for finder tests.
type fakeOverlapRegistry struct {
	cands  []overlapCandidate
	err    error
	panics bool
	calls  int
}

func (f *fakeOverlapRegistry) registryCandidates(root string) ([]overlapCandidate, error) {
	f.calls++
	if f.panics {
		panic("registry exploded")
	}
	return f.cands, f.err
}

// fakeOverlapBranch is the branch source for finder tests.
type fakeOverlapBranch struct {
	cands  []overlapCandidate
	err    error
	panics bool
	calls  int
}

func (f *fakeOverlapBranch) branchCandidates(string) ([]overlapCandidate, error) {
	f.calls++
	if f.panics {
		panic("branch source exploded")
	}
	return f.cands, f.err
}

func fakeRootResolver(root string, ok bool) projectRootResolver {
	return func(string) (string, bool) { return root, ok }
}

func warningsOfKind(ws []OverlapWarning, kind string) []OverlapWarning {
	var out []OverlapWarning
	for _, w := range ws {
		if w.Kind == kind {
			out = append(out, w)
		}
	}
	return out
}

func TestOverlapFinderAutomataEveryStatus(t *testing.T) {
	lister := fakeOverlapLister{recs: []OverlapAutomaton{
		{ID: "aa11", Title: overlapFinderPlan, Status: "running"},
		{ID: "bb22", Title: overlapFinderPlan, Status: "failed"},
		{ID: "cc33", Title: overlapFinderPlan, Status: "completed"},
	}}
	f := NewOverlapFinder(&lister, &fakeOverlapRegistry{}, &fakeOverlapBranch{}, 0.5, fakeRootResolver("/root", true))
	ws, skipped := f.Find(overlapFinderPlan, "", "new-99", "")

	if len(skipped) != 0 {
		t.Fatalf("nothing was skipped, got %v", skipped)
	}
	got := warningsOfKind(ws, warningKindAutomaton)
	if len(got) != 3 {
		t.Fatalf("want 3 automaton warnings, got %d: %#v", len(got), ws)
	}
	byID := map[string]OverlapWarning{}
	for _, w := range got {
		byID[w.ID] = w
		if !strings.Contains(w.Reason, "Automaton") {
			t.Fatalf("reason must say Automaton: %q", w.Reason)
		}
		if strings.Contains(w.Reason, "PRD") {
			t.Fatalf("reason must never say PRD: %q", w.Reason)
		}
		if len(w.SharedTerms) > 5 {
			t.Fatalf("shared terms must be capped at five: %v", w.SharedTerms)
		}
	}
	for id, status := range map[string]string{"aa11": "running", "bb22": "failed", "cc33": "completed"} {
		w, ok := byID[id]
		if !ok {
			t.Fatalf("missing automaton warning for %s: %#v", id, ws)
		}
		if w.Status != status {
			t.Fatalf("status for %s: want %q, got %q", id, status, w.Status)
		}
		if w.ReadHint != "datawatch autonomous get "+id {
			t.Fatalf("read hint for %s: %q", id, w.ReadHint)
		}
		if w.Score != 1 {
			t.Fatalf("score for %s: want 1, got %v", id, w.Score)
		}
	}
}

func TestOverlapFinderExcludesNewAutomaton(t *testing.T) {
	lister := fakeOverlapLister{recs: []OverlapAutomaton{
		{ID: "new-99", Title: overlapFinderPlan, Status: "running"},
		{ID: "other-1", Title: overlapFinderPlan, Status: "completed"},
	}}
	f := NewOverlapFinder(&lister, &fakeOverlapRegistry{}, &fakeOverlapBranch{}, 0.5, fakeRootResolver("/root", true))
	ws, _ := f.Find(overlapFinderPlan, "", "new-99", "")

	found := map[string]bool{}
	for _, w := range ws {
		found[w.ID] = true
	}
	if found["new-99"] {
		t.Fatalf("the new Automaton must be excluded by id: %#v", ws)
	}
	if !found["other-1"] {
		t.Fatalf("other Automata must still be reported: %#v", ws)
	}
}

func TestOverlapFinderRegistryRow(t *testing.T) {
	reg := &fakeOverlapRegistry{cands: []overlapCandidate{
		{Source: "registry_row", Row: 12, Text: overlapFinderPlan},
	}}
	f := NewOverlapFinder(&fakeOverlapLister{}, reg, &fakeOverlapBranch{}, 0.5, fakeRootResolver("/root", true))
	ws, skipped := f.Find(overlapFinderPlan, "", "new-99", "")

	if len(skipped) != 0 {
		t.Fatalf("nothing was skipped, got %v", skipped)
	}
	got := warningsOfKind(ws, warningKindRegistry)
	if len(got) != 1 {
		t.Fatalf("want 1 registry warning, got %#v", ws)
	}
	w := got[0]
	if w.Row != 12 {
		t.Fatalf("row: want 12, got %d", w.Row)
	}
	if w.ReadHint != "docs/plans/README.md row 12" {
		t.Fatalf("read hint: %q", w.ReadHint)
	}
	if !strings.Contains(w.Reason, "Automaton") || strings.Contains(w.Reason, "PRD") {
		t.Fatalf("reason must say Automaton, never PRD: %q", w.Reason)
	}
}

func TestOverlapFinderUnmergedBranch(t *testing.T) {
	br := &fakeOverlapBranch{cands: []overlapCandidate{
		{Source: "unmerged_branch", Ref: "automaton/gone", Text: overlapFinderPlan},
	}}
	f := NewOverlapFinder(&fakeOverlapLister{}, &fakeOverlapRegistry{}, br, 0.5, fakeRootResolver("/root", true))
	ws, skipped := f.Find(overlapFinderPlan, "", "new-99", "")

	if len(skipped) != 0 {
		t.Fatalf("nothing was skipped, got %v", skipped)
	}
	got := warningsOfKind(ws, warningKindBranch)
	if len(got) != 1 {
		t.Fatalf("want 1 branch warning, got %#v", ws)
	}
	w := got[0]
	if w.Branch != "automaton/gone" {
		t.Fatalf("branch: want automaton/gone, got %q", w.Branch)
	}
	if w.ReadHint != "git log main..automaton/gone" {
		t.Fatalf("read hint: %q", w.ReadHint)
	}
}

func TestOverlapFinderBelowThreshold(t *testing.T) {
	// The candidate shares only 3 of the 10 plan terms, so its score
	// is 3/10 = 0.3, below the 0.5 threshold, while it still clears
	// the three-shared-terms floor.
	reg := &fakeOverlapRegistry{cands: []overlapCandidate{
		{Source: "registry_row", Row: 3, Text: planHead(3, 7)},
	}}
	f := NewOverlapFinder(&fakeOverlapLister{}, reg, &fakeOverlapBranch{}, 0.5, fakeRootResolver("/root", true))
	ws, skipped := f.Find(overlapFinderPlan, "", "new-99", "")

	if len(skipped) != 0 {
		t.Fatalf("nothing was skipped, got %v", skipped)
	}
	if len(ws) != 0 {
		t.Fatalf("a candidate below the threshold must not be reported: %#v", ws)
	}
	if reg.calls != 1 {
		t.Fatalf("the registry source must still run, got %d calls", reg.calls)
	}
}

func TestOverlapFinderSourceErrorOneUnavailable(t *testing.T) {
	lister := fakeOverlapLister{recs: []OverlapAutomaton{
		{ID: "aa11", Title: overlapFinderPlan, Status: "running"},
	}}
	reg := &fakeOverlapRegistry{err: errors.New("registry read failed")}
	br := &fakeOverlapBranch{cands: []overlapCandidate{
		{Source: "unmerged_branch", Ref: "automaton/gone", Text: overlapFinderPlan},
	}}
	f := NewOverlapFinder(&lister, reg, br, 0.5, fakeRootResolver("/root", true))
	ws, skipped := f.Find(overlapFinderPlan, "", "new-99", "")

	if len(skipped) != 0 {
		t.Fatalf("a failing source is warned, not skipped: %v", skipped)
	}
	unavailable := warningsOfKind(ws, warningKindUnavailable)
	if len(unavailable) != 1 {
		t.Fatalf("want exactly one unavailable warning, got %d: %#v", len(unavailable), ws)
	}
	if !strings.Contains(unavailable[0].Reason, "registry") {
		t.Fatalf("the unavailable warning must name the registry source: %q", unavailable[0].Reason)
	}
	if !strings.Contains(unavailable[0].Reason, "source") {
		t.Fatalf("the unavailable warning must be one operator sentence about the source: %q", unavailable[0].Reason)
	}
	if len(warningsOfKind(ws, warningKindAutomaton)) != 1 {
		t.Fatalf("other sources must still report: %#v", ws)
	}
	if len(warningsOfKind(ws, warningKindBranch)) != 1 {
		t.Fatalf("other sources must still report: %#v", ws)
	}
}

func TestOverlapFinderPanicRecovered(t *testing.T) {
	lister := fakeOverlapLister{recs: []OverlapAutomaton{
		{ID: "aa11", Title: overlapFinderPlan, Status: "failed"},
	}}
	reg := &fakeOverlapRegistry{cands: []overlapCandidate{
		{Source: "registry_row", Row: 5, Text: overlapFinderPlan},
	}}
	br := &fakeOverlapBranch{panics: true}
	f := NewOverlapFinder(&lister, reg, br, 0.5, fakeRootResolver("/root", true))

	var ws []OverlapWarning
	var skipped []string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("a panic must not escape Find: %v", r)
			}
		}()
		ws, skipped = f.Find(overlapFinderPlan, "", "new-99", "")
	}()

	if len(skipped) != 0 {
		t.Fatalf("a panicking source is warned, not skipped: %v", skipped)
	}
	unavailable := warningsOfKind(ws, warningKindUnavailable)
	if len(unavailable) != 1 {
		t.Fatalf("want exactly one unavailable warning, got %d: %#v", len(unavailable), ws)
	}
	if !strings.Contains(unavailable[0].Reason, "branch") {
		t.Fatalf("the unavailable warning must name the branch source: %q", unavailable[0].Reason)
	}
	if len(warningsOfKind(ws, warningKindAutomaton)) != 1 || len(warningsOfKind(ws, warningKindRegistry)) != 1 {
		t.Fatalf("the other sources must still report: %#v", ws)
	}
}

func TestOverlapFinderNoProjectRootSkips(t *testing.T) {
	lister := fakeOverlapLister{recs: []OverlapAutomaton{
		{ID: "aa11", Title: overlapFinderPlan, Status: "completed"},
	}}
	// Both file-based sources panic: if they were ever called without a
	// project root the panic would turn into an unavailable warning.
	reg := &fakeOverlapRegistry{panics: true}
	br := &fakeOverlapBranch{panics: true}
	f := NewOverlapFinder(&lister, reg, br, 0.5, fakeRootResolver("", false))
	ws, skipped := f.Find(overlapFinderPlan, "", "new-99", "")

	names := map[string]bool{}
	for _, n := range skipped {
		names[n] = true
	}
	if !names["registry"] || !names["branch"] {
		t.Fatalf("skipped must name registry and branch, got %v", skipped)
	}
	if reg.calls != 0 || br.calls != 0 {
		t.Fatalf("skipped sources must not run: registry=%d branch=%d", reg.calls, br.calls)
	}
	if len(warningsOfKind(ws, warningKindUnavailable)) != 0 {
		t.Fatalf("skipped sources must produce no warning: %#v", ws)
	}
	for _, bad := range []string{warningKindRegistry, warningKindBranch} {
		if len(warningsOfKind(ws, bad)) != 0 {
			t.Fatalf("skipped sources must produce no %s warning: %#v", bad, ws)
		}
	}
	if len(warningsOfKind(ws, warningKindAutomaton)) != 1 {
		t.Fatalf("the automaton source must still report: %#v", ws)
	}
}

func TestOverlapFinderSortOrder(t *testing.T) {
	// Scores: exact plan = 1.0, planHead(9,3) = 9/10 = 0.9,
	// planHead(5,5) = 5/10 = 0.5, planHead(3,7) = 3/10 = 0.3.
	// Kinds are interleaved so every sort tier is exercised.
	lister := fakeOverlapLister{recs: []OverlapAutomaton{
		{ID: "a-2", Title: overlapFinderPlan, Status: "running"},
		{ID: "a-0", Title: overlapFinderPlan, Status: "failed"},
		{ID: "far-1", Title: "completely different work unrelated", Status: "completed"},
	}}
	reg := &fakeOverlapRegistry{cands: []overlapCandidate{
		{Source: "registry_row", Row: 9, Text: overlapFinderPlan},
		{Source: "registry_row", Row: 2, Text: planHead(5, 5)},
		{Source: "registry_row", Row: 7, Text: overlapFinderPlan},
		{Source: "registry_row", Row: 8, Text: overlapFinderPlan},
	}}
	br := &fakeOverlapBranch{cands: []overlapCandidate{
		{Source: "unmerged_branch", Ref: "automaton/d", Text: planHead(3, 7)},
		{Source: "unmerged_branch", Ref: "automaton/b", Text: overlapFinderPlan},
		{Source: "unmerged_branch", Ref: "automaton/c", Text: planHead(5, 5)},
		{Source: "unmerged_branch", Ref: "automaton/e", Text: planHead(9, 3)},
	}}
	f := NewOverlapFinder(&lister, reg, br, 0.3, fakeRootResolver("/root", true))
	ws, skipped := f.Find(overlapFinderPlan, "", "new-99", "")

	if len(skipped) != 0 {
		t.Fatalf("nothing was skipped, got %v", skipped)
	}
	if len(ws) != 10 {
		t.Fatalf("want 10 warnings, got %d: %#v", len(ws), ws)
	}
	type wantCase struct {
		kind   string
		id     string
		row    int
		branch string
		score  float64
	}
	want := []wantCase{
		{warningKindAutomaton, "a-0", 0, "", 1},
		{warningKindAutomaton, "a-2", 0, "", 1},
		{warningKindBranch, "", 0, "automaton/b", 1},
		{warningKindRegistry, "", 7, "", 1},
		{warningKindRegistry, "", 8, "", 1},
		{warningKindRegistry, "", 9, "", 1},
		{warningKindBranch, "", 0, "automaton/e", 0.9},
		{warningKindBranch, "", 0, "automaton/c", 0.5},
		{warningKindRegistry, "", 2, "", 0.5},
		{warningKindBranch, "", 0, "automaton/d", 0.3},
	}
	for i, wc := range want {
		w := ws[i]
		if w.Kind != wc.kind {
			t.Fatalf("position %d: kind want %s, got %s: %#v", i, wc.kind, w.Kind, ws)
		}
		if w.ID != wc.id || w.Row != wc.row || w.Branch != wc.branch {
			t.Fatalf("position %d: want %+v, got %+v", i, wc, w)
		}
		if w.Score != wc.score {
			t.Fatalf("position %d: score want %v, got %v", i, wc.score, w.Score)
		}
	}
}
