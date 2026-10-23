package autonomous

import (
	"errors"
	"sort"
	"testing"
)

func newTagTestManager(t *testing.T) (*Manager, *Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{store: st}, st, dir
}

func mustTag(t *testing.T, m *Manager, name, parent string) *Tag {
	t.Helper()
	tg, err := m.CreateTag(name, parent, "", "")
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return tg
}

func mustPRD(t *testing.T, st *Store, id string) {
	t.Helper()
	if err := st.SavePRD(&PRD{ID: id}); err != nil {
		t.Fatal(err)
	}
}

func TestTagCreateValidation(t *testing.T) {
	m, _, _ := newTagTestManager(t)
	if _, err := m.CreateTag("  ", "", "", ""); !errors.Is(err, ErrTagInvalid) {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := m.CreateTag("x", "nope", "", ""); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("unknown parent: %v", err)
	}
}

func TestTagPersistRoundTrip(t *testing.T) {
	m, st, dir := newTagTestManager(t)
	parent, err := m.CreateTag("parent", "", "#ff0000", "top level")
	if err != nil {
		t.Fatal(err)
	}
	child := mustTag(t, m, "child", parent.ID)
	mustPRD(t, st, "p1")
	if err := m.SetPRDTags("p1", []string{child.ID, parent.ID}); err != nil {
		t.Fatal(err)
	}

	// Reopen the store from the same directory (simulated restart).
	st2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(st2.ListTags()); n != 2 {
		t.Fatalf("tags after reopen: %d", n)
	}
	gp, ok := st2.GetTag(parent.ID)
	if !ok || gp.Name != "parent" || gp.Color != "#ff0000" || gp.Description != "top level" {
		t.Fatalf("parent after reopen: %+v", gp)
	}
	gc, ok := st2.GetTag(child.ID)
	if !ok || gc.ParentID != parent.ID {
		t.Fatalf("child after reopen: %+v", gc)
	}
	if gp.CreatedAt.IsZero() || gp.UpdatedAt.IsZero() {
		t.Fatalf("timestamps lost: %+v", gp)
	}
	p, ok := st2.GetPRD("p1")
	if !ok {
		t.Fatal("prd missing after reopen")
	}
	got := append([]string(nil), p.TagIDs...)
	sort.Strings(got)
	want := []string{child.ID, parent.ID}
	sort.Strings(want)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("TagIDs after reopen: %v want %v", got, want)
	}
}

func TestTagDeletePersists(t *testing.T) {
	m, _, dir := newTagTestManager(t)
	a := mustTag(t, m, "a", "")
	mustTag(t, m, "b", "")
	if err := m.DeleteTag(a.ID); err != nil {
		t.Fatal(err)
	}
	st2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st2.GetTag(a.ID); ok || len(st2.ListTags()) != 1 {
		t.Fatalf("deleted tag resurrected: %d tags", len(st2.ListTags()))
	}
}

func TestTagSetParentCycleGuard(t *testing.T) {
	m, _, _ := newTagTestManager(t)
	a := mustTag(t, m, "a", "")
	b := mustTag(t, m, "b", a.ID)
	c := mustTag(t, m, "c", b.ID)
	d := mustTag(t, m, "d", "")

	if _, err := m.SetTagParent(a.ID, a.ID); !errors.Is(err, ErrTagCycle) {
		t.Fatalf("self parent: %v", err)
	}
	if _, err := m.SetTagParent(a.ID, b.ID); !errors.Is(err, ErrTagCycle) {
		t.Fatalf("direct loop: %v", err)
	}
	if _, err := m.SetTagParent(a.ID, c.ID); !errors.Is(err, ErrTagCycle) {
		t.Fatalf("deep loop: %v", err)
	}
	if got, _ := m.store.GetTag(a.ID); got.ParentID != "" {
		t.Fatalf("refused move mutated tag: %q", got.ParentID)
	}
	if _, err := m.SetTagParent(c.ID, d.ID); err != nil {
		t.Fatalf("valid reparent: %v", err)
	}
	if got, _ := m.store.GetTag(c.ID); got.ParentID != d.ID {
		t.Fatalf("reparent not applied: %q", got.ParentID)
	}
	if _, err := m.SetTagParent(c.ID, ""); err != nil {
		t.Fatalf("move to root: %v", err)
	}
	if got, _ := m.store.GetTag(c.ID); got.ParentID != "" {
		t.Fatalf("root move not applied: %q", got.ParentID)
	}
	if _, err := m.SetTagParent(c.ID, "missing"); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("unknown parent: %v", err)
	}
}

func TestTagDeleteRefusedWithChildren(t *testing.T) {
	m, _, _ := newTagTestManager(t)
	a := mustTag(t, m, "a", "")
	b := mustTag(t, m, "b", a.ID)
	if err := m.DeleteTag(a.ID); !errors.Is(err, ErrTagHasChildren) {
		t.Fatalf("want ErrTagHasChildren, got %v", err)
	}
	if _, ok := m.store.GetTag(a.ID); !ok {
		t.Fatal("tag removed despite refusal")
	}
	if _, err := m.SetTagParent(b.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteTag(a.ID); err != nil {
		t.Fatalf("delete after children moved: %v", err)
	}
}

func TestTagDeleteRefusedWhenAssigned(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	a := mustTag(t, m, "a", "")
	mustPRD(t, st, "p1")
	mustPRD(t, st, "p2")
	mustPRD(t, st, "p3")
	for _, id := range []string{"p1", "p2"} {
		if err := m.SetPRDTags(id, []string{a.ID}); err != nil {
			t.Fatal(err)
		}
	}
	err := m.DeleteTag(a.ID)
	var in *TagInUseError
	if !errors.Is(err, ErrTagInUse) || !errors.As(err, &in) || in.Count != 2 {
		t.Fatalf("want in-use error with count 2, got %v", err)
	}
	if _, ok := st.GetTag(a.ID); !ok {
		t.Fatal("tag removed despite refusal")
	}
	for _, id := range []string{"p1", "p2"} {
		if err := m.SetPRDTags(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.DeleteTag(a.ID); err != nil {
		t.Fatalf("delete after assignments cleared: %v", err)
	}
	if err := m.DeleteTag(a.ID); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestTagSetPRDTagsValidatesAndDedupes(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	a := mustTag(t, m, "a", "")
	b := mustTag(t, m, "b", "")
	mustPRD(t, st, "p1")

	if err := m.SetPRDTags("p1", []string{a.ID, "bogus"}); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if p, _ := st.GetPRD("p1"); len(p.TagIDs) != 0 {
		t.Fatalf("rejected call mutated PRD: %v", p.TagIDs)
	}
	if err := m.SetPRDTags("p1", []string{a.ID, b.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	p, _ := st.GetPRD("p1")
	if len(p.TagIDs) != 2 || p.TagIDs[0] != a.ID || p.TagIDs[1] != b.ID {
		t.Fatalf("dedupe: %v", p.TagIDs)
	}
	if err := m.SetPRDTags("missing", nil); err == nil {
		t.Fatal("unknown PRD accepted")
	}
}

func TestTagDescendantFilter(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	root := mustTag(t, m, "root", "")
	mid := mustTag(t, m, "mid", root.ID)
	leaf := mustTag(t, m, "leaf", mid.ID)
	other := mustTag(t, m, "other", "")
	for _, id := range []string{"pr", "pm", "pl", "po", "pml"} {
		mustPRD(t, st, id)
	}
	set := func(id string, tags ...string) {
		t.Helper()
		if err := m.SetPRDTags(id, tags); err != nil {
			t.Fatal(err)
		}
	}
	set("pr", root.ID)
	set("pm", mid.ID)
	set("pl", leaf.ID)
	set("po", other.ID)
	set("pml", mid.ID, leaf.ID) // two tags in one subtree counts once

	ids := func(ps []*PRD) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.ID)
		}
		sort.Strings(out)
		return out
	}
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	if got := ids(m.PRDsWithTag(root.ID, true)); !eq(got, []string{"pl", "pm", "pml", "pr"}) {
		t.Fatalf("root includeSub: %v", got)
	}
	if got := ids(m.PRDsWithTag(root.ID, false)); !eq(got, []string{"pr"}) {
		t.Fatalf("root exact: %v", got)
	}
	if got := ids(m.PRDsWithTag(mid.ID, true)); !eq(got, []string{"pl", "pm", "pml"}) {
		t.Fatalf("mid includeSub: %v", got)
	}
	if got := ids(m.PRDsWithTag(leaf.ID, true)); !eq(got, []string{"pl", "pml"}) {
		t.Fatalf("leaf includeSub: %v", got)
	}

	roll := m.TagCounts(true)
	if roll[root.ID] != 4 || roll[mid.ID] != 3 || roll[leaf.ID] != 2 || roll[other.ID] != 1 {
		t.Fatalf("rolled-up counts: %v", roll)
	}
	exact := m.TagCounts(false)
	if exact[root.ID] != 1 || exact[mid.ID] != 2 || exact[leaf.ID] != 2 || exact[other.ID] != 1 {
		t.Fatalf("exact counts: %v", exact)
	}
}
