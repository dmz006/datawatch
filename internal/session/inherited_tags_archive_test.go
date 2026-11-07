package session

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Tests that a list call for inherited tags decodes each archived session file
// at most once: one SystemTagIndexSnapshot per list call plus the per-Automaton
// lookups (and SystemTagsByPRD on the same cache) must not re-decode the
// archive. The fixture is 300 archived sessions across 100 Automata (three per
// Automaton, each session carrying its own system tag) plus a few active
// sessions.

const (
	inheritedTagsArchiveAutomata     = 100
	inheritedTagsArchivePerAutomaton = 3
	inheritedTagsArchiveTotal        = inheritedTagsArchiveAutomata * inheritedTagsArchivePerAutomaton
)

// inheritedTagsArchiveFixture holds the fixture layout and, per Automaton, the
// sorted system tags a list handler must see for it (archived and active
// sessions combined).
type inheritedTagsArchiveFixture struct {
	automata []string
	expected map[string][]string
}

// seedInheritedTagsArchive creates the fixture in a fresh manager and moves the
// terminal sessions into the archive file.
func seedInheritedTagsArchive(t *testing.T, m *Manager) *inheritedTagsArchiveFixture {
	t.Helper()
	f := &inheritedTagsArchiveFixture{expected: map[string][]string{}}
	now := time.Now()
	save := func(id, prd, tag string, archived bool) {
		sess := &Session{ID: id, FullID: "h-" + id, Name: "name-" + id, Task: "task " + id,
			PRDID: prd, SystemTags: []string{tag}}
		if archived { // terminal and old enough for ArchiveOnce; see seedTagFixture
			sess.State = StateComplete
			sess.CreatedAt = now.Add(-40 * day)
			sess.UpdatedAt = now.Add(-30 * day)
		} else {
			sess.State = StateRunning
			sess.CreatedAt = now
			sess.UpdatedAt = now
		}
		if err := m.store.Save(sess); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	for a := 0; a < inheritedTagsArchiveAutomata; a++ {
		prd := fmt.Sprintf("prd-%03d", a)
		f.automata = append(f.automata, prd)
		tags := make([]string, 0, inheritedTagsArchivePerAutomaton)
		for n := 0; n < inheritedTagsArchivePerAutomaton; n++ {
			tag := fmt.Sprintf("istag-%03d-%d", a, n) // each session has its own tag
			save(fmt.Sprintf("ia-%03d-%d", a, n), prd, tag, true)
			tags = append(tags, tag)
		}
		// Already sorted: the numeric suffixes sort before "active".
		f.expected[prd] = tags
	}
	// A few active sessions: two inside an Automaton (their tags must appear
	// too) and one outside any Automaton.
	save("ia-active-000", "prd-000", "istag-active-000", false)
	f.expected["prd-000"] = append(f.expected["prd-000"], "istag-active-000")
	save("ia-active-001", "prd-001", "istag-active-001", false)
	f.expected["prd-001"] = append(f.expected["prd-001"], "istag-active-001")
	save("ia-loose-000", "", "istag-loose-000", false)

	n, err := m.store.ArchiveOnce(time.Now())
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if n != inheritedTagsArchiveTotal {
		t.Fatalf("archived %d sessions, want %d", n, inheritedTagsArchiveTotal)
	}
	if active := len(m.store.List()); active != 3 {
		t.Fatalf("active set has %d sessions, want 3", active)
	}
	return f
}

// TestInheritedTagsArchiveListDecodesEachFileOnce checks the list path:
// one index snapshot per list call, per-Automaton lookups against it, and
// SystemTagsByPRD within the cache lifetime. Together they must decode the
// 300 archived files exactly once.
func TestInheritedTagsArchiveListDecodesEachFileOnce(t *testing.T) {
	m := newTestManager(t)
	f := seedInheritedTagsArchive(t, m)
	m.SetInheritedTagsCacheTTL(time.Hour)

	t.Run("one snapshot plus per-Automaton lookups decodes each file once", func(t *testing.T) {
		m.store.resetArchiveDecodeCount()
		snap := m.SystemTagIndexSnapshot() // what one list call takes
		for _, prd := range f.automata {   // the 100 per-Automaton lookups
			if got, want := sortedCopy(snap(prd)), f.expected[prd]; !reflect.DeepEqual(got, want) {
				t.Errorf("snapshot(%q) tags = %v, want %v", prd, got, want)
			}
		}
		if got := m.store.archiveDecodeCount(); got != inheritedTagsArchiveTotal {
			t.Fatalf("decode count after first pass = %d, want %d (no archived file twice)", got, inheritedTagsArchiveTotal)
		}
	})

	t.Run("SystemTagsByPRD within the cache lifetime decodes nothing more", func(t *testing.T) {
		for _, prd := range f.automata { // 100 SystemTagsByPRD calls on the warm cache
			if got, want := sortedCopy(m.SystemTagsByPRD(prd)), f.expected[prd]; !reflect.DeepEqual(got, want) {
				t.Errorf("SystemTagsByPRD(%q) = %v, want %v", prd, got, want)
			}
		}
		if got := m.store.archiveDecodeCount(); got != inheritedTagsArchiveTotal {
			t.Fatalf("decode count after cached SystemTagsByPRD = %d, still want %d (no re-decode)", got, inheritedTagsArchiveTotal)
		}
	})
}

// TestInheritedTagsArchiveCachingOffSingleSnapshot checks that with caching
// turned off (negative lifetime) a single snapshot, taken for a whole list,
// still builds the index and decodes each archived file exactly once.
func TestInheritedTagsArchiveCachingOffSingleSnapshot(t *testing.T) {
	m := newTestManager(t)
	f := seedInheritedTagsArchive(t, m)
	m.SetInheritedTagsCacheTTL(-1) // caching off
	if got := m.InheritedTagsCacheTTL(); got != -1 {
		t.Fatalf("TTL = %v, want -1 (caching off)", got)
	}

	m.store.resetArchiveDecodeCount()
	snap := m.SystemTagIndexSnapshot() // one list call takes one snapshot
	for _, prd := range f.automata {
		if got, want := sortedCopy(snap(prd)), f.expected[prd]; !reflect.DeepEqual(got, want) {
			t.Errorf("snapshot(%q) tags = %v, want %v", prd, got, want)
		}
	}
	if got := m.store.archiveDecodeCount(); got != inheritedTagsArchiveTotal {
		t.Fatalf("caching off: decode count = %d, want %d (each archived file once)", got, inheritedTagsArchiveTotal)
	}
}
