package packs

import (
	"strings"
	"testing"
)

func resolveFixture(name, version string, kinds []string, titles ...string) *Pack {
	p := &Pack{Name: name, Version: version, Kinds: kinds}
	for _, t := range titles {
		p.Tasks = append(p.Tasks, Task{Title: t})
	}
	return p
}

func titlesOf(ts []ResolvedTask) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Title)
	}
	return out
}

func TestResolveTaskListOrder(t *testing.T) {
	a := resolveFixture("alpha", "1.0.0", []string{KindFeature}, "a1", "a2")
	b := resolveFixture("beta", "2.1.0", []string{KindFeature}, "b1")
	c := resolveFixture("gamma", "0.3.0", []string{KindFeature}, "c1", "c2")

	got, err := ResolveTaskList(KindFeature, []*Pack{a, b, c})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"a1", "a2", "b1", "c1", "c2"}
	if strings.Join(titlesOf(got), ",") != strings.Join(want, ",") {
		t.Fatalf("order: got %v, want %v", titlesOf(got), want)
	}

	// Reversing the active packs reverses the pack order in the output.
	rev, err := ResolveTaskList(KindFeature, []*Pack{c, b, a})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantRev := []string{"c1", "c2", "b1", "a1", "a2"}
	if strings.Join(titlesOf(rev), ",") != strings.Join(wantRev, ",") {
		t.Fatalf("reversed order: got %v, want %v", titlesOf(rev), wantRev)
	}
}

func TestResolveTaskListRecordsProvenance(t *testing.T) {
	a := resolveFixture("alpha", "1.0.0", []string{KindBug}, "a1")
	b := resolveFixture("beta", "2.1.0", []string{KindBug}, "b1")

	got, err := ResolveTaskList(KindBug, []*Pack{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want 2", len(got))
	}
	if got[0].PackName != "alpha" || got[0].PackVersion != "1.0.0" {
		t.Errorf("task 0 provenance: got %q@%q, want alpha@1.0.0", got[0].PackName, got[0].PackVersion)
	}
	if got[1].PackName != "beta" || got[1].PackVersion != "2.1.0" {
		t.Errorf("task 1 provenance: got %q@%q, want beta@2.1.0", got[1].PackName, got[1].PackVersion)
	}
}

func TestResolveTaskListKindFiltering(t *testing.T) {
	feat := resolveFixture("feat-pack", "1.0.0", []string{KindFeature}, "f1")
	bug := resolveFixture("bug-pack", "1.0.0", []string{KindBug}, "g1")
	both := resolveFixture("both-pack", "1.0.0", []string{KindFeature, KindBug}, "h1")
	none := resolveFixture("other-pack", "1.0.0", []string{KindOther}, "o1")
	active := []*Pack{feat, bug, both, none}

	got, err := ResolveTaskList(KindBug, active)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"g1", "h1"}
	if strings.Join(titlesOf(got), ",") != strings.Join(want, ",") {
		t.Fatalf("bug filter: got %v, want %v", titlesOf(got), want)
	}

	got, err = ResolveTaskList(KindMaintenance, active)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("kind with no matching pack: got %v, want none", titlesOf(got))
	}
}

func TestResolveTaskListNilAndEmpty(t *testing.T) {
	got, err := ResolveTaskList(KindFeature, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("nil active: got %v, err %v; want empty, nil", got, err)
	}
	withNil, err := ResolveTaskList(KindFeature, []*Pack{nil, resolveFixture("p", "1.0.0", []string{KindFeature}, "x")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(titlesOf(withNil), ",") != "x" {
		t.Fatalf("nil pack entry: got %v, want [x]", titlesOf(withNil))
	}
}

func TestResolveTaskListRefusesDuplicateTitle(t *testing.T) {
	a := resolveFixture("alpha", "1.0.0", []string{KindFeature}, "shared", "a-only")
	b := resolveFixture("beta", "1.0.0", []string{KindFeature}, "b-only", "shared")

	got, err := ResolveTaskList(KindFeature, []*Pack{a, b})
	if err == nil {
		t.Fatalf("expected duplicate-title error, got tasks %v", titlesOf(got))
	}
	if got != nil {
		t.Errorf("expected no tasks on refusal, got %v", titlesOf(got))
	}
	msg := err.Error()
	if !strings.Contains(msg, "alpha") || !strings.Contains(msg, "beta") {
		t.Errorf("error must name both packs, got: %v", err)
	}
	if !strings.Contains(msg, "shared") {
		t.Errorf("error must name the duplicate title, got: %v", err)
	}
}

func TestResolveTaskListDuplicateOnlyCountsActiveKind(t *testing.T) {
	// The same title in packs that do not apply to the kind is not a conflict.
	a := resolveFixture("alpha", "1.0.0", []string{KindFeature}, "shared")
	b := resolveFixture("beta", "1.0.0", []string{KindBug}, "shared")

	got, err := ResolveTaskList(KindFeature, []*Pack{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(titlesOf(got), ",") != "shared" {
		t.Fatalf("got %v, want [shared]", titlesOf(got))
	}
}
