package autonomous

import (
	"errors"
	"testing"
)

func newTagTestManager(t *testing.T) (*Manager, *Store) {
	t.Helper()
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{store: st}, st
}

func TestTagLifecycle(t *testing.T) {
	m, st := newTagTestManager(t)
	if _, err := m.CreateTag(" ", "", "", ""); !errors.Is(err, ErrTagInvalid) {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := m.CreateTag("x", "nope", "", ""); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("bad parent: %v", err)
	}
	a, _ := m.CreateTag("a", "", "", "")
	b, _ := m.CreateTag("b", a.ID, "", "")
	c, _ := m.CreateTag("c", b.ID, "", "")

	if _, err := m.SetTagParent(a.ID, c.ID); !errors.Is(err, ErrTagCycle) {
		t.Fatalf("cycle: %v", err)
	}
	if _, err := m.SetTagParent(a.ID, a.ID); !errors.Is(err, ErrTagCycle) {
		t.Fatalf("self: %v", err)
	}
	if err := m.DeleteTag(a.ID); !errors.Is(err, ErrTagHasChildren) {
		t.Fatalf("children: %v", err)
	}
	if got := m.TagDescendants(a.ID); len(got) != 2 {
		t.Fatalf("descendants: %v", got)
	}

	p1 := &PRD{ID: "p1"}
	p2 := &PRD{ID: "p2"}
	_ = st.SavePRD(p1)
	_ = st.SavePRD(p2)
	if err := m.SetPRDTags("p1", []string{c.ID, c.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPRDTags("p2", []string{"bogus"}); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("bogus: %v", err)
	}
	if got, _ := st.GetPRD("p1"); len(got.TagIDs) != 2 {
		t.Fatalf("dedupe: %v", got.TagIDs)
	}
	if n := len(m.PRDsWithTag(a.ID, true)); n != 1 {
		t.Fatalf("sub: %d", n)
	}
	if n := len(m.PRDsWithTag(a.ID, false)); n != 0 {
		t.Fatalf("exact: %d", n)
	}
	if cnt := m.TagCounts(true); cnt[a.ID] != 1 || cnt[b.ID] != 1 || cnt[c.ID] != 1 {
		t.Fatalf("rollup: %v", cnt)
	}
	if cnt := m.TagCounts(false); cnt[a.ID] != 0 || cnt[c.ID] != 1 {
		t.Fatalf("exact counts: %v", cnt)
	}
	err := m.DeleteTag(c.ID)
	var in *TagInUseError
	if !errors.As(err, &in) || in.Count != 1 || !errors.Is(err, ErrTagInUse) {
		t.Fatalf("in use: %v", err)
	}
	_ = m.SetPRDTags("p1", nil)
	if err := m.DeleteTag(c.ID); err != nil {
		t.Fatal(err)
	}

	// persistence
	st2, err := NewStore(st.dir[:len(st.dir)-len("/autonomous")])
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.ListTags()) != 2 {
		t.Fatalf("reload: %d", len(st2.ListTags()))
	}
}
