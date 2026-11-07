package session

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

// Tests for the one-pass system tag index (system_tag_index.go) and for
// SystemTagsByPRD, which reads from it.

// oldSystemTagsScan is the per-Automaton scan SystemTagsByPRD used before the
// index existed. It is kept here as the expected value: the index must return
// the same multiset of tags for every Automaton.
func oldSystemTagsScan(t *testing.T, m *Manager, prdID string) []string {
	t.Helper()
	out := []string{}
	for _, sess := range m.store.List() {
		if sess.PRDID == prdID {
			out = append(out, sess.SystemTags...)
		}
	}
	for _, sess := range m.ListArchivedSessions(SessionFilter{}, func(s *Session) bool { return s.PRDID == prdID }, "", time.Time{}, 0) {
		out = append(out, sess.SystemTags...)
	}
	return out
}

// sortedCopy returns a sorted copy so two tag lists can be compared as multisets.
func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

// tagFixture is one session of the index fixture. Archived sessions are
// terminal and old enough to be moved by ArchiveOnce.
type tagFixture struct {
	id       string
	prdID    string
	tags     []string
	archived bool
}

func seedTagFixture(t *testing.T, m *Manager, fixtures []tagFixture) {
	t.Helper()
	for _, f := range fixtures {
		sess := &Session{ID: f.id, FullID: "h-" + f.id, Name: "name-" + f.id, Task: "task " + f.id,
			PRDID: f.prdID, SystemTags: f.tags}
		if f.archived {
			sess.State = StateComplete
			sess.CreatedAt = time.Now().Add(-40 * day)
			sess.UpdatedAt = time.Now().Add(-30 * day)
		} else {
			sess.State = StateRunning
			sess.CreatedAt = time.Now()
			sess.UpdatedAt = time.Now()
		}
		if err := m.store.Save(sess); err != nil {
			t.Fatalf("save %s: %v", f.id, err)
		}
	}
	// Move the archived ones into the archive file.
	n, err := m.store.ArchiveOnce(time.Now())
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	want := 0
	for _, f := range fixtures {
		if f.archived {
			want++
		}
	}
	if n != want {
		t.Fatalf("archived %d sessions, want %d", n, want)
	}
}

func tagFixtureSet() []tagFixture {
	return []tagFixture{
		// Automaton "prd-a": active and archived sessions, with a repeated tag.
		{id: "a1", prdID: "prd-a", tags: []string{"ci", "ci", "gpu"}},
		{id: "a2", prdID: "prd-a", tags: []string{"gpu"}, archived: true},
		{id: "a3", prdID: "prd-a", tags: []string{"docs", "ci"}, archived: true},
		// Automaton "prd-b": archived only.
		{id: "b1", prdID: "prd-b", tags: []string{"net"}, archived: true},
		{id: "b2", prdID: "prd-b", tags: nil, archived: true},
		// Automaton "prd-c": active only, no system tags.
		{id: "c1", prdID: "prd-c", tags: nil},
		// Sessions outside any Automaton must not leak into any entry.
		{id: "o1", prdID: "", tags: []string{"loose"}},
		{id: "o2", prdID: "", tags: []string{"loose"}, archived: true},
		// Automaton "prd-d": one archived session, a single system tag.
		{id: "d1", prdID: "prd-d", tags: []string{"sys-d"}, archived: true},
	}
}

func TestSystemTagIndexMatchesOldScan(t *testing.T) {
	m := newTestManager(t)
	seedTagFixture(t, m, tagFixtureSet())

	for _, prd := range []string{"prd-a", "prd-b", "prd-c", "prd-d", "prd-missing"} {
		got := sortedCopy(m.SystemTagsByPRD(prd))
		want := sortedCopy(oldSystemTagsScan(t, m, prd))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SystemTagsByPRD(%q) = %v, want (old scan) %v", prd, got, want)
		}
	}

	// Spot-check the expected contents, so a broken old scan cannot hide a bug.
	if got, want := sortedCopy(m.SystemTagsByPRD("prd-a")), []string{"ci", "ci", "ci", "docs", "gpu", "gpu"}; !reflect.DeepEqual(got, want) {
		t.Errorf("prd-a tags = %v, want %v", got, want)
	}
}

func TestSystemTagIndexKeepsRepeatsAndSkipsEmptyPRDID(t *testing.T) {
	m := newTestManager(t)
	seedTagFixture(t, m, tagFixtureSet())

	idx := m.systemTagIndex()
	if got, want := sortedCopy(idx["prd-a"]), []string{"ci", "ci", "ci", "docs", "gpu", "gpu"}; !reflect.DeepEqual(got, want) {
		t.Errorf("index[prd-a] = %v, want %v (repeats kept)", got, want)
	}
	if _, ok := idx[""]; ok {
		t.Errorf("index has an entry for the empty PRD ID: %v", idx[""])
	}
	for k := range idx {
		if k == "" {
			t.Errorf("empty PRD ID key present")
		}
	}
}

func TestSystemTagsByPRDUnknownAutomatonEmptySlice(t *testing.T) {
	m := newTestManager(t)
	seedTagFixture(t, m, tagFixtureSet())

	for _, prd := range []string{"prd-nobody", ""} {
		got := m.SystemTagsByPRD(prd)
		if got == nil {
			t.Errorf("SystemTagsByPRD(%q) = nil, want empty non-nil slice", prd)
		}
		if len(got) != 0 {
			t.Errorf("SystemTagsByPRD(%q) = %v, want empty", prd, got)
		}
	}
}

func TestSystemTagsByPRDReturnsCopy(t *testing.T) {
	m := newTestManager(t)
	seedTagFixture(t, m, tagFixtureSet())

	got := m.SystemTagsByPRD("prd-b")
	if len(got) == 0 {
		t.Fatal("prd-b has no tags in the fixture")
	}
	got[0] = "mutated"
	again := m.SystemTagsByPRD("prd-b")
	if again[0] == "mutated" {
		t.Errorf("SystemTagsByPRD returned a slice that aliases the index")
	}
}

func TestSystemTagIndexDecodesEachArchivedRecordOnce(t *testing.T) {
	m := newTestManager(t)
	fixtures := tagFixtureSet()
	seedTagFixture(t, m, fixtures)

	archived := 0
	for _, f := range fixtures {
		if f.archived {
			archived++
		}
	}

	m.store.resetArchiveDecodeCount()
	_ = m.systemTagIndex()
	if got := m.store.archiveDecodeCount(); got != int64(archived) {
		t.Fatalf("decode count after building the index = %d, want %d (each archived record once)", got, archived)
	}

	// SystemTagsByPRD builds the index on each call, so one call decodes
	// each archived record once as well.
	m.store.resetArchiveDecodeCount()
	_ = m.SystemTagsByPRD("prd-a")
	if got := m.store.archiveDecodeCount(); got != int64(archived) {
		t.Fatalf("decode count after SystemTagsByPRD = %d, want %d", got, archived)
	}
}
