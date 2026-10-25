package autonomous

import (
	"testing"
)

// stubSessionTagCounter stands in for the session side of the tag counts: it
// returns fixed effective tag lists, one per session.
type stubSessionTagCounter struct{ sets [][]string }

func (s stubSessionTagCounter) CountSessionsWithTag(id string) int {
	n := 0
	for _, set := range s.sets {
		for _, t := range set {
			if t == id {
				n++
				break
			}
		}
	}
	return n
}

func (s stubSessionTagCounter) SessionEffectiveTagSets() [][]string { return s.sets }

// savePRDWithTags stores an Automaton carrying the given tag IDs.
func savePRDWithTags(t *testing.T, st *Store, id string, tags ...string) {
	t.Helper()
	if err := st.SavePRD(&PRD{ID: id, TagIDs: tags}); err != nil {
		t.Fatal(err)
	}
}

// countsByTag returns the counts view of every tag, keyed by tag ID.
func countsByTag(t *testing.T, m *Manager, includeSub bool) (map[string]TagCountView, UntaggedCounts) {
	t.Helper()
	tags, untagged := NewAPI(m).ListTagsWithCounts(includeSub)
	out := map[string]TagCountView{}
	for _, x := range tags {
		v, ok := x.(TagCountView)
		if !ok {
			t.Fatalf("counts item is %T, want TagCountView", x)
		}
		out[v.ID] = v
	}
	return out, untagged
}

func TestTagCountsSubtreeVersusDirect(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	parent := mustTag(t, m, "parent", "")
	child := mustTag(t, m, "child", parent.ID)
	savePRDWithTags(t, st, "p1", child.ID)
	savePRDWithTags(t, st, "p2", parent.ID)

	counts, _ := countsByTag(t, m, false)
	p := counts[parent.ID]
	if p.SubtreeCount != 2 || p.DirectCount != 1 {
		t.Errorf("parent subtree=%d direct=%d, want 2 and 1", p.SubtreeCount, p.DirectCount)
	}
	c := counts[child.ID]
	if c.SubtreeCount != 1 || c.DirectCount != 1 {
		t.Errorf("child subtree=%d direct=%d, want 1 and 1", c.SubtreeCount, c.DirectCount)
	}
	// Count keeps its include_sub meaning: direct when false, subtree when true.
	if p.Count != 1 {
		t.Errorf("parent count with include_sub=false = %d, want 1", p.Count)
	}
	withSub, _ := countsByTag(t, m, true)
	if withSub[parent.ID].Count != 2 {
		t.Errorf("parent count with include_sub=true = %d, want 2", withSub[parent.ID].Count)
	}
}

func TestTagCountsSiblingItemCountedOnceForParent(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	parent := mustTag(t, m, "parent", "")
	c1 := mustTag(t, m, "c1", parent.ID)
	c2 := mustTag(t, m, "c2", parent.ID)
	savePRDWithTags(t, st, "p1", c1.ID, c2.ID)

	counts, _ := countsByTag(t, m, false)
	if got := counts[parent.ID].SubtreeCount; got != 1 {
		t.Errorf("parent subtree = %d, want 1 (one item under two siblings)", got)
	}
	if got := counts[parent.ID].DirectCount; got != 0 {
		t.Errorf("parent direct = %d, want 0", got)
	}
	if got := counts[c1.ID].SubtreeCount; got != 1 {
		t.Errorf("c1 subtree = %d, want 1", got)
	}
}

func TestTagCountsUntaggedCounts(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	a := mustTag(t, m, "a", "")
	savePRDWithTags(t, st, "p1")
	savePRDWithTags(t, st, "p2", a.ID)
	savePRDWithTags(t, st, "p3")
	m.SetSessionTagCounter(stubSessionTagCounter{sets: [][]string{nil, {a.ID}, {}}})

	_, untagged := countsByTag(t, m, false)
	if untagged.Count != 2 {
		t.Errorf("untagged automata = %d, want 2", untagged.Count)
	}
	if untagged.SessionCount != 2 {
		t.Errorf("untagged sessions = %d, want 2", untagged.SessionCount)
	}
}

func TestTagCountsSessionsIncludeInheritedTags(t *testing.T) {
	m, _, _ := newTagTestManager(t)
	parent := mustTag(t, m, "parent", "")
	child := mustTag(t, m, "child", parent.ID)
	// The counter returns each session's effective tags: its own tags plus the
	// tags it inherits from its Automaton.
	m.SetSessionTagCounter(stubSessionTagCounter{sets: [][]string{
		{child.ID},            // own child tag, outside any Automaton
		{parent.ID},           // inherited parent tag only
		{child.ID, parent.ID}, // own child plus inherited parent
		{},                    // untagged
	}})

	counts, untagged := countsByTag(t, m, false)
	p := counts[parent.ID]
	if p.SessionSubtreeCount != 3 {
		t.Errorf("parent session subtree = %d, want 3 (a session with two subtree tags counts once)", p.SessionSubtreeCount)
	}
	if p.SessionDirectCount != 2 {
		t.Errorf("parent session direct = %d, want 2", p.SessionDirectCount)
	}
	c := counts[child.ID]
	if c.SessionSubtreeCount != 2 || c.SessionDirectCount != 2 {
		t.Errorf("child session subtree=%d direct=%d, want 2 and 2", c.SessionSubtreeCount, c.SessionDirectCount)
	}
	if untagged.SessionCount != 1 {
		t.Errorf("untagged sessions = %d, want 1", untagged.SessionCount)
	}
}

func TestTagCountsWithoutSessionCounterIsZero(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	a := mustTag(t, m, "a", "")
	savePRDWithTags(t, st, "p1", a.ID)

	counts, untagged := countsByTag(t, m, true)
	if got := counts[a.ID]; got.SessionSubtreeCount != 0 || got.SessionDirectCount != 0 {
		t.Errorf("session counts without a counter = %d/%d, want 0/0", got.SessionSubtreeCount, got.SessionDirectCount)
	}
	if untagged.SessionCount != 0 {
		t.Errorf("untagged sessions without a counter = %d, want 0", untagged.SessionCount)
	}
}
