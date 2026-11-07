package session

import (
	"reflect"
	"sort"
	"sync"
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

	idx := m.buildSystemTagIndex()
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
	_ = m.buildSystemTagIndex()
	if got := m.store.archiveDecodeCount(); got != int64(archived) {
		t.Fatalf("decode count after building the index = %d, want %d (each archived record once)", got, archived)
	}

	// SystemTagsByPRD on an empty cache builds the index once, so it decodes
	// each archived record once as well.
	m.InvalidateSystemTagIndex()
	m.store.resetArchiveDecodeCount()
	_ = m.SystemTagsByPRD("prd-a")
	if got := m.store.archiveDecodeCount(); got != int64(archived) {
		t.Fatalf("decode count after SystemTagsByPRD = %d, want %d", got, archived)
	}
}

func fixtureArchivedCount() int64 {
	n := int64(0)
	for _, f := range tagFixtureSet() {
		if f.archived {
			n++
		}
	}
	return n
}

// fakeTagClock is an injectable clock for the cache tests.
type fakeTagClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeTagClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeTagClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newCachedTagManager(t *testing.T) (*Manager, *fakeTagClock) {
	t.Helper()
	m := newTestManager(t)
	seedTagFixture(t, m, tagFixtureSet())
	clk := &fakeTagClock{t: time.Now()}
	m.tagIdx.clock = clk.now
	m.store.resetArchiveDecodeCount()
	return m, clk
}

func TestSystemTagIndexCacheHitWithinLifetime(t *testing.T) {
	m, clk := newCachedTagManager(t)
	m.SetInheritedTagsCacheTTL(time.Minute)
	_ = m.SystemTagsByPRD("prd-a")
	clk.advance(30 * time.Second)
	_ = m.SystemTagsByPRD("prd-b")
	if got := m.store.archiveDecodeCount(); got != fixtureArchivedCount() {
		t.Fatalf("decode count = %d, want %d (one build)", got, fixtureArchivedCount())
	}
}

func TestSystemTagIndexDefaultTTL(t *testing.T) {
	m, clk := newCachedTagManager(t)
	_ = m.SystemTagsByPRD("prd-a")
	clk.advance(14 * time.Second)
	_ = m.SystemTagsByPRD("prd-a")
	if got := m.store.archiveDecodeCount(); got != fixtureArchivedCount() {
		t.Fatalf("within default TTL decode = %d, want one build", got)
	}
	clk.advance(2 * time.Second)
	_ = m.SystemTagsByPRD("prd-a")
	if got := m.store.archiveDecodeCount(); got != 2*fixtureArchivedCount() {
		t.Fatalf("after default TTL decode = %d, want two builds", got)
	}
}

func TestSystemTagIndexRebuildsAfterLifetime(t *testing.T) {
	m, clk := newCachedTagManager(t)
	m.SetInheritedTagsCacheTTL(time.Minute)
	_ = m.SystemTagsByPRD("prd-a")
	clk.advance(61 * time.Second)
	_ = m.SystemTagsByPRD("prd-a")
	if got := m.store.archiveDecodeCount(); got != 2*fixtureArchivedCount() {
		t.Fatalf("decode count = %d, want %d (two builds)", got, 2*fixtureArchivedCount())
	}
}

func TestSystemTagIndexInvalidateForcesRebuild(t *testing.T) {
	m, _ := newCachedTagManager(t)
	m.SetInheritedTagsCacheTTL(time.Hour)
	_ = m.SystemTagsByPRD("prd-a")
	m.InvalidateSystemTagIndex()
	_ = m.SystemTagsByPRD("prd-a")
	if got := m.store.archiveDecodeCount(); got != 2*fixtureArchivedCount() {
		t.Fatalf("decode count = %d, want two builds", got)
	}
}

func TestSystemTagIndexNegativeTTLRebuildsEveryCall(t *testing.T) {
	m, _ := newCachedTagManager(t)
	m.SetInheritedTagsCacheTTL(-1)
	for i := 0; i < 3; i++ {
		_ = m.SystemTagsByPRD("prd-a")
	}
	if got := m.store.archiveDecodeCount(); got != 3*fixtureArchivedCount() {
		t.Fatalf("decode count = %d, want three builds", got)
	}
}

func TestSystemTagIndexSingleFlight(t *testing.T) {
	m, _ := newCachedTagManager(t)
	m.SetInheritedTagsCacheTTL(time.Hour)
	const n = 50
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([][]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = m.SystemTagsByPRD("prd-a")
		}(i)
	}
	close(start)
	wg.Wait()
	// One build decodes each archived record once; a second build would double it.
	if got := m.store.archiveDecodeCount(); got != fixtureArchivedCount() {
		t.Fatalf("decode count = %d, want %d (exactly one build)", got, fixtureArchivedCount())
	}
	for i, r := range results {
		if len(r) != 6 {
			t.Fatalf("caller %d got %v", i, r)
		}
	}
}

func TestSystemTagIndexSnapshotIsCopyOnRead(t *testing.T) {
	m, _ := newCachedTagManager(t)
	snap := m.SystemTagIndexSnapshot()
	got := snap("prd-b")
	if len(got) == 0 {
		t.Fatal("prd-b empty")
	}
	got[0] = "mutated"
	if snap("prd-b")[0] == "mutated" || m.SystemTagsByPRD("prd-b")[0] == "mutated" {
		t.Fatal("snapshot aliases the cache")
	}
	if v := snap("nobody"); v == nil || len(v) != 0 {
		t.Fatalf("unknown prd = %v, want empty non-nil", v)
	}
}

// ---- invalidation by store generation ----

// tagsOf returns the sorted answer for a PRD.
func tagsOf(m *Manager, prd string) []string { return sortedCopy(m.SystemTagsByPRD(prd)) }

func newGenTagManager(t *testing.T) *Manager {
	t.Helper()
	m, _ := newCachedTagManager(t)
	m.SetInheritedTagsCacheTTL(time.Hour) // far longer than any test step
	_ = m.SystemTagsByPRD("prd-a")        // warm the cache
	m.store.resetArchiveDecodeCount()
	return m
}

func TestSystemTagIndexSetSystemTagsInvalidates(t *testing.T) {
	m := newGenTagManager(t)
	if err := m.SetSystemTags("a1", []string{"fresh"}); err != nil {
		t.Fatal(err)
	}
	if got, want := tagsOf(m, "prd-a"), []string{"ci", "docs", "fresh", "gpu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prd-a after SetSystemTags = %v, want %v", got, want)
	}
	if m.store.archiveDecodeCount() == 0 {
		t.Fatal("no rebuild after SetSystemTags")
	}
}

func TestSystemTagIndexArchiveInvalidates(t *testing.T) {
	m := newGenTagManager(t)
	sess, ok := m.GetSession("c1")
	if !ok {
		t.Fatal("c1 missing")
	}
	// Give c1 tags (an invalidating save), then age it into the archive.
	if err := m.SetSystemTags("c1", []string{"late"}); err != nil {
		t.Fatal(err)
	}
	if got := tagsOf(m, "prd-c"); !reflect.DeepEqual(got, []string{"late"}) {
		t.Fatalf("prd-c = %v", got)
	}
	m.store.Mutate(func() {
		sess.State = StateComplete
		sess.UpdatedAt = time.Now().Add(-30 * day)
	})
	m.store.resetArchiveDecodeCount()
	if n, err := m.ArchiveSessions(time.Now()); err != nil || n != 1 {
		t.Fatalf("archive moved %d, err %v", n, err)
	}
	_ = tagsOf(m, "prd-c")
	if m.store.archiveDecodeCount() == 0 {
		t.Fatal("no rebuild after ArchiveOnce")
	}
	if got := tagsOf(m, "prd-c"); !reflect.DeepEqual(got, []string{"late"}) {
		t.Fatalf("prd-c after archive = %v", got)
	}
}

func TestSystemTagIndexDeleteInvalidates(t *testing.T) {
	m := newGenTagManager(t)
	if err := m.Delete("h-a1", false); err != nil {
		t.Fatal(err)
	}
	if got, want := tagsOf(m, "prd-a"), []string{"ci", "docs", "gpu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prd-a after Delete = %v, want %v", got, want)
	}
}

func TestSystemTagIndexRestoreInvalidates(t *testing.T) {
	m := newGenTagManager(t)
	_ = tagsOf(m, "prd-d")
	if _, err := m.RestoreArchivedSession("h-d1"); err != nil {
		t.Fatal(err)
	}
	m.store.resetArchiveDecodeCount()
	if got := tagsOf(m, "prd-d"); !reflect.DeepEqual(got, []string{"sys-d"}) {
		t.Fatalf("prd-d after restore = %v (must not be doubled or lost)", got)
	}
	if m.store.archiveDecodeCount() == 0 {
		t.Fatal("no rebuild after restore")
	}
}

func TestSystemTagIndexPlainSaveDoesNotRebuild(t *testing.T) {
	m := newGenTagManager(t)
	before := m.store.tagGeneration()
	sess, _ := m.GetSession("a1")
	for i := 0; i < 5; i++ {
		sess.UpdatedAt = time.Now().Add(time.Duration(i) * time.Second)
		if err := m.store.Save(sess); err != nil {
			t.Fatal(err)
		}
		_ = m.SystemTagsByPRD("prd-a")
	}
	if m.store.tagGeneration() != before {
		t.Fatal("UpdatedAt-only save bumped the generation")
	}
	if got := m.store.archiveDecodeCount(); got != 0 {
		t.Fatalf("decode counter = %d after plain saves, want 0", got)
	}
}

func TestSystemTagIndexMoveSessionTagsViaSave(t *testing.T) {
	m := newGenTagManager(t)
	sess, _ := m.GetSession("a1")
	sess.Tags = []string{"old-tag"}
	if err := m.store.Save(sess); err != nil {
		t.Fatal(err)
	}
	gen := m.store.tagGeneration()
	n, err := m.MoveSessionTags("old-tag", "new-tag")
	if err != nil || n < 1 {
		t.Fatalf("MoveSessionTags = %d, %v", n, err)
	}
	got, _ := m.GetSession("a1")
	if len(got.Tags) != 1 || got.Tags[0] != "new-tag" {
		t.Fatalf("tags = %v, want [new-tag] (persisted through Save)", got.Tags)
	}
	// Own tags are not in the index, so the answer is unchanged and no rebuild happens.
	if m.store.tagGeneration() != gen {
		t.Fatal("MoveSessionTags bumped the generation although system tags did not change")
	}
	if want := []string{"ci", "ci", "ci", "docs", "gpu", "gpu"}; !reflect.DeepEqual(tagsOf(m, "prd-a"), want) {
		t.Fatalf("prd-a = %v, want %v", tagsOf(m, "prd-a"), want)
	}
}

func TestInheritedTagsCacheTTLGetter(t *testing.T) {
	m := newTestManager(t)
	if got := m.InheritedTagsCacheTTL(); got != 15*time.Second {
		t.Fatalf("unset TTL = %v, want the default 15s", got)
	}
	m.SetInheritedTagsCacheTTL(time.Hour)
	if got := m.InheritedTagsCacheTTL(); got != time.Hour {
		t.Fatalf("TTL after set = %v, want 1h", got)
	}
	m.SetInheritedTagsCacheTTL(-1)
	if got := m.InheritedTagsCacheTTL(); got != -1 {
		t.Fatalf("TTL after set off = %v, want -1 (caching off)", got)
	}
}
