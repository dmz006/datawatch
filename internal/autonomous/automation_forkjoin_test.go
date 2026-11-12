package autonomous

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// forkJoinAutomation returns a valid active Automation: entry fork f1 over
// branches b1 and b2, both paired with join j1 (which has no successor).
func forkJoinAutomation(t *testing.T) *Automation {
	t.Helper()
	return &Automation{
		ID:            "a0000001",
		Name:          "fj",
		Status:        AutomationActive,
		EntryMemberID: "f1",
		Members: []AutomationMember{
			{ID: "f1", Kind: MemberKindFork, ForkMembers: []string{"b1", "b2"}},
			{ID: "b1", PRDID: "p1"},
			{ID: "b2", PRDID: "p2"},
			{ID: "j1", Kind: MemberKindJoin, JoinMembers: []string{"b1", "b2"}},
		},
	}
}

// memberAt returns a pointer to the member with id inside a, failing the test
// when it is absent.
func memberAt(t *testing.T, a *Automation, id string) *AutomationMember {
	t.Helper()
	for i := range a.Members {
		if a.Members[i].ID == id {
			return &a.Members[i]
		}
	}
	t.Fatalf("member %s not found", id)
	return nil
}

func TestAutomationMemberKindsRoundTrip(t *testing.T) {
	if got := (AutomationMember{}).kind(); got != MemberKindPRD {
		t.Fatalf("empty kind = %q, want %q", got, MemberKindPRD)
	}
	if got := (AutomationMember{Kind: MemberKindPRD}).kind(); got != MemberKindPRD {
		t.Fatalf("prd kind = %q, want %q", got, MemberKindPRD)
	}

	a := forkJoinAutomation(t)
	a.Members = append(a.Members, AutomationMember{ID: "c1", Kind: MemberKindAutomation, AutomationID: "c0000001"})
	memberAt(t, a, "j1").OnBranchFailure = BranchAnyOneIsEnough
	a.ActiveCursors = []string{"b1", "b2"}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Automation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fork := memberAt(t, &got, "f1")
	if fork.Kind != MemberKindFork || !reflect.DeepEqual(fork.ForkMembers, []string{"b1", "b2"}) {
		t.Fatalf("fork member after round trip: %+v", fork)
	}
	join := memberAt(t, &got, "j1")
	if join.Kind != MemberKindJoin || !reflect.DeepEqual(join.JoinMembers, []string{"b1", "b2"}) || join.OnBranchFailure != BranchAnyOneIsEnough {
		t.Fatalf("join member after round trip: %+v", join)
	}
	nested := memberAt(t, &got, "c1")
	if nested.Kind != MemberKindAutomation || nested.AutomationID != "c0000001" {
		t.Fatalf("automation member after round trip: %+v", nested)
	}
	if !reflect.DeepEqual(got.ActiveCursors, []string{"b1", "b2"}) {
		t.Fatalf("active cursors after round trip: %v", got.ActiveCursors)
	}

	// A linear Automation in canonical form round-trips byte-identically and
	// carries none of the Phase 3a fields.
	lin := automaton(t)
	normalizeActiveCursors(lin)
	first, err := json.Marshal(lin)
	if err != nil {
		t.Fatalf("marshal linear: %v", err)
	}
	var back Automation
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatalf("unmarshal linear: %v", err)
	}
	second, err := json.Marshal(&back)
	if err != nil {
		t.Fatalf("re-marshal linear: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("linear round trip changed bytes:\n first: %s\nsecond: %s", first, second)
	}
	for _, key := range []string{`"kind"`, `"active_cursors"`, `"automation_id"`, `"fork_members"`, `"join_members"`, `"on_branch_failure"`} {
		if bytes.Contains(first, []byte(key)) {
			t.Fatalf("linear record carries %s: %s", key, first)
		}
	}
}

func TestAutomationValidateForkJoinPairing(t *testing.T) {
	if err := validateAutomation(forkJoinAutomation(t)); err != nil {
		t.Fatalf("valid fork/join rejected: %v", err)
	}

	cases := []struct {
		name      string
		mutate    func(t *testing.T, a *Automation)
		errSubstr string
	}{
		{"fork with one branch", func(t *testing.T, a *Automation) {
			memberAt(t, a, "f1").ForkMembers = []string{"b1"}
		}, "fork f1 needs at least 2 branch members"},
		{"fork carries an outcome edge", func(t *testing.T, a *Automation) {
			memberAt(t, a, "f1").OnComplete = "b1"
		}, "fork f1 must not carry outcome edges"},
		{"branch carries an outcome edge", func(t *testing.T, a *Automation) {
			memberAt(t, a, "b1").OnFailed = "b2"
		}, "branch member b1 of fork f1 must not carry outcome edges"},
		{"branch is a fork", func(t *testing.T, a *Automation) {
			memberAt(t, a, "f1").ForkMembers = []string{"b1", "f1"}
		}, "branches must be prd or automation members"},
		{"join carries on_failed", func(t *testing.T, a *Automation) {
			memberAt(t, a, "j1").OnFailed = "b1"
		}, "join j1 may carry only on_complete"},
		{"edge names a join", func(t *testing.T, a *Automation) {
			memberAt(t, a, "b1").OnComplete = "j1"
		}, "names join j1"},
		{"join as entry member", func(t *testing.T, a *Automation) {
			a.EntryMemberID = "j1"
		}, "entry member j1 is a join"},
		{"join with one branch", func(t *testing.T, a *Automation) {
			memberAt(t, a, "j1").JoinMembers = []string{"b1"}
		}, "join j1 needs at least 2 branch members"},
		{"join with no paired fork", func(t *testing.T, a *Automation) {
			a.Members = append(a.Members, AutomationMember{ID: "b3", PRDID: "p3"})
			memberAt(t, a, "j1").JoinMembers = []string{"b1", "b2", "b3"}
		}, "fork f1 has 0 paired joins"},
		{"branch shared by two joins", func(t *testing.T, a *Automation) {
			a.Members = append(a.Members, AutomationMember{ID: "j2", Kind: MemberKindJoin, JoinMembers: []string{"b1", "b2"}})
		}, "branch member b1 is already in join j1"},
		{"prd member carries fork_members", func(t *testing.T, a *Automation) {
			memberAt(t, a, "b1").ForkMembers = []string{"b2", "b1"}
		}, "fork_members is only valid on kind fork"},
		{"fork member carries prd_id", func(t *testing.T, a *Automation) {
			memberAt(t, a, "f1").PRDID = "p9"
		}, "must not carry prd_id"},
		{"unknown kind", func(t *testing.T, a *Automation) {
			memberAt(t, a, "b1").Kind = "loop"
		}, "has unknown kind"},
		{"unknown on_branch_failure", func(t *testing.T, a *Automation) {
			memberAt(t, a, "j1").OnBranchFailure = "sometimes"
		}, "unknown on_branch_failure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := forkJoinAutomation(t)
			tc.mutate(t, a)
			err := validateAutomation(a)
			if err == nil || !strings.Contains(err.Error(), tc.errSubstr) {
				t.Fatalf("got %v, want error containing %q", err, tc.errSubstr)
			}
		})
	}

	// Every join rule is satisfied by a valid any_one_is_enough join.
	ok := forkJoinAutomation(t)
	memberAt(t, ok, "j1").OnBranchFailure = BranchAnyOneIsEnough
	if err := validateAutomation(ok); err != nil {
		t.Fatalf("any_one_is_enough join rejected: %v", err)
	}
}

func TestAutomationValidateBranchEdgesPinnedToPairingJoin(t *testing.T) {
	// A join's on_complete leads on to a tail member; the branches reach the
	// join only through the pairing, never through an outcome edge.
	a := forkJoinAutomation(t)
	a.Members = append(a.Members, AutomationMember{ID: "t1", PRDID: "p3"})
	memberAt(t, a, "j1").OnComplete = "t1"
	if err := validateAutomation(a); err != nil {
		t.Fatalf("join forward path rejected: %v", err)
	}
	succ := automationSuccessors(a)
	if !sameMemberSet(succ["f1"], []string{"b1", "b2"}) {
		t.Fatalf("fork successors = %v, want its branches", succ["f1"])
	}
	for _, b := range []string{"b1", "b2"} {
		if !sameMemberSet(succ[b], []string{"j1"}) {
			t.Fatalf("branch %s successors = %v, want its paired join only", b, succ[b])
		}
	}
	if !sameMemberSet(succ["j1"], []string{"t1"}) {
		t.Fatalf("join successors = %v, want its on_complete only", succ["j1"])
	}

	// A branch with an outcome edge to the tail is rejected even with a valid join.
	edged := forkJoinAutomation(t)
	edged.Members = append(edged.Members, AutomationMember{ID: "t1", PRDID: "p3"})
	memberAt(t, edged, "b1").OnComplete = "t1"
	if err := validateAutomation(edged); err == nil || !strings.Contains(err.Error(), "b1") {
		t.Fatalf("branch edge to tail: got %v, want error naming b1", err)
	}

	// A cycle that runs back through the fork is rejected with the path named.
	cyc := forkJoinAutomation(t)
	cyc.Members = append(cyc.Members, AutomationMember{ID: "t1", PRDID: "p3"})
	memberAt(t, cyc, "j1").OnComplete = "t1"
	memberAt(t, cyc, "t1").OnComplete = "f1"
	if err := validateAutomation(cyc); err == nil || !strings.Contains(err.Error(), "cycle detected") || !strings.Contains(err.Error(), "f1") {
		t.Fatalf("cycle through fork: got %v, want cycle error naming f1", err)
	}
}

func TestAutomationValidateNestedOwnershipUniqueness(t *testing.T) {
	stored := map[string]*Automation{
		"c0000001": {ID: "c0000001", Name: "child", Status: AutomationActive},
	}
	lookup := func(id string) (*Automation, bool) {
		c, ok := stored[id]
		return c, ok
	}
	noOwner := func(string) string { return "" }
	parent := func() *Automation {
		return &Automation{
			ID:            "a0000001",
			Name:          "parent",
			Status:        AutomationActive,
			EntryMemberID: "n1",
			Members:       []AutomationMember{{ID: "n1", Kind: MemberKindAutomation, AutomationID: "c0000001"}},
		}
	}

	if err := validateAutomationNesting(parent(), lookup, noOwner); err != nil {
		t.Fatalf("unowned child rejected: %v", err)
	}
	selfOwned := func(string) string { return "a0000001" }
	if err := validateAutomationNesting(parent(), lookup, selfOwned); err != nil {
		t.Fatalf("child owned by this Automation rejected: %v", err)
	}
	other := func(string) string { return "a0000009" }
	if err := validateAutomationNesting(parent(), lookup, other); err == nil || err.Error() != "automation invalid: member n1: child automation c0000001 already belongs to automation a0000009" {
		t.Fatalf("child owned elsewhere: got %v", err)
	}
	done := parent()
	done.Status = AutomationCompleted
	if err := validateAutomationNesting(done, lookup, other); err != nil {
		t.Fatalf("terminal parent owns nothing, got %v", err)
	}
	missing := parent()
	missing.Members[0].AutomationID = "cdeadbee"
	if err := validateAutomationNesting(missing, lookup, noOwner); err == nil || !strings.Contains(err.Error(), "child automation cdeadbee not found") {
		t.Fatalf("missing child: got %v", err)
	}

	// Through the store: a child nested by one draft Automation cannot be
	// nested by a second one.
	s, err := NewAutomationStore(t.TempDir(), func(id string) (*PRD, bool) { return &PRD{ID: id}, true })
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	child, err := s.Create(&Automation{Name: "child"})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	first := &Automation{Name: "first", Members: []AutomationMember{{Kind: MemberKindAutomation, AutomationID: child.ID}}}
	if _, err := s.Create(first); err != nil {
		t.Fatalf("create first parent: %v", err)
	}
	second := &Automation{Name: "second", Members: []AutomationMember{{Kind: MemberKindAutomation, AutomationID: child.ID}}}
	if _, err := s.Create(second); err == nil || !strings.Contains(err.Error(), "already belongs to automation") {
		t.Fatalf("second parent nesting the same child: got %v", err)
	}
}

func TestAutomationValidateNestedCycleRejection(t *testing.T) {
	// A member naming its own Automation is rejected by validateAutomation.
	self := &Automation{
		ID:            "a0000001",
		Name:          "self",
		Status:        AutomationActive,
		EntryMemberID: "n1",
		Members:       []AutomationMember{{ID: "n1", Kind: MemberKindAutomation, AutomationID: "a0000001"}},
	}
	if err := validateAutomation(self); err == nil || !strings.Contains(err.Error(), "nests its own automation") {
		t.Fatalf("self reference: got %v", err)
	}

	nest := func(id, childID string) *Automation {
		return &Automation{
			ID:            id,
			Name:          id,
			Status:        AutomationActive,
			EntryMemberID: "n1",
			Members:       []AutomationMember{{ID: "n1", Kind: MemberKindAutomation, AutomationID: childID}},
		}
	}
	noOwner := func(string) string { return "" }
	stored := map[string]*Automation{
		"a0000002": nest("a0000002", "a0000003"), // B nests C
		"a0000003": nest("a0000003", "a0000001"), // C nests A, the parent under validation
	}
	lookup := func(id string) (*Automation, bool) {
		c, ok := stored[id]
		return c, ok
	}

	// A nests B, B nests C, C nests A: a three-level cycle through the parent.
	parent := nest("a0000001", "a0000002")
	if err := validateAutomationNesting(parent, lookup, noOwner); err == nil || !strings.Contains(err.Error(), "nesting cycle via automations [a0000002 a0000003 a0000001]") {
		t.Fatalf("transitive cycle: got %v", err)
	}

	// Breaking the chain at C makes the same parent acceptable.
	stored["a0000003"] = nest("a0000003", "c0000009")
	stored["c0000009"] = &Automation{ID: "c0000009", Name: "leaf", Status: AutomationActive}
	if err := validateAutomationNesting(parent, lookup, noOwner); err != nil {
		t.Fatalf("acyclic nesting rejected: %v", err)
	}
}

func TestAutomationLoadSeedsActiveCursorsFromLegacyCursor(t *testing.T) {
	dir := t.TempDir()
	legacy := Automation{
		ID:              "a0000001",
		Name:            "legacy",
		Status:          AutomationActive,
		EntryMemberID:   "m1",
		CurrentMemberID: "m2",
		Members:         []AutomationMember{{ID: "m1", PRDID: "p1", OnComplete: "m2"}, {ID: "m2", PRDID: "p2"}},
	}
	cursored := Automation{
		ID:            "a0000002",
		Name:          "cursored",
		Status:        AutomationActive,
		EntryMemberID: "m1",
		ActiveCursors: []string{"m1"},
		Members:       []AutomationMember{{ID: "m1", PRDID: "p3"}},
	}
	var buf bytes.Buffer
	for _, a := range []Automation{legacy, cursored} {
		line, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "automations.jsonl"), buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	lookup := func(id string) (*PRD, bool) { return &PRD{ID: id}, true }
	s, err := NewAutomationStore(dir, lookup)
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	got, ok := s.Get("a0000001")
	if !ok || !reflect.DeepEqual(got.ActiveCursors, []string{"m2"}) {
		t.Fatalf("legacy record cursors = %v, want [m2]", got)
	}
	got, ok = s.Get("a0000002")
	if !ok || !reflect.DeepEqual(got.ActiveCursors, []string{"m1"}) {
		t.Fatalf("record with active_cursors = %+v, want [m1]", got)
	}

	// A linear Automation created with a cursor keeps the cursor set in step
	// with CurrentMemberID, and clearing the cursor clears the set.
	created, err := s.Create(&Automation{
		Name:            "mirror",
		CurrentMemberID: "m9",
		Members:         []AutomationMember{{ID: "m9", PRDID: "p9"}},
	})
	if err != nil {
		t.Fatalf("create linear: %v", err)
	}
	if !reflect.DeepEqual(created.ActiveCursors, []string{"m9"}) {
		t.Fatalf("created cursors = %v, want [m9]", created.ActiveCursors)
	}
	updated, err := s.Update(created.ID, 0, func(a *Automation) error {
		a.CurrentMemberID = ""
		return nil
	})
	if err != nil {
		t.Fatalf("update linear: %v", err)
	}
	if updated.ActiveCursors != nil {
		t.Fatalf("cleared cursor left set %v", updated.ActiveCursors)
	}
}
