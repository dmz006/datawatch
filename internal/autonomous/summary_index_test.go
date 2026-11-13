package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func newSummaryIndexStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func summaryMatches(t *testing.T, s *Store, id string) {
	t.Helper()
	s.mu.Lock()
	p := s.prds[id]
	var want SummaryEntry
	if p != nil {
		want = buildSummaryEntry(p)
	}
	s.mu.Unlock()
	got, ok := s.SummaryEntry(id)
	if p == nil {
		if ok {
			t.Fatalf("entry for deleted %s still present", id)
		}
		return
	}
	if !ok {
		t.Fatalf("no entry for %s", id)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entry drifted from record:\n got %+v\nwant %+v", got, want)
	}
}

func addTaskedStory(t *testing.T, s *Store, p *PRD) {
	t.Helper()
	s.mu.Lock()
	p.Story = append(p.Story, Story{ID: "st-" + p.ID, PRDID: p.ID, Title: "s", Tasks: []Task{
		{ID: "t1-" + p.ID, PRDID: p.ID, Title: "a", Spec: "SECRET-TASK-TEXT", Status: TaskPending, SessionID: "w1"},
		{ID: "t2-" + p.ID, PRDID: p.ID, Title: "b", Spec: "more", Status: TaskPending, SessionID: "w2"},
	}})
	s.mu.Unlock()
	if err := s.SavePRD(p); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryIndexTracksCreateTaskRelinkDeleteReload(t *testing.T) {
	s, dir := newSummaryIndexStore(t)
	p, err := s.CreatePRD("spec body", "/proj/a", "claude", "m", "")
	if err != nil {
		t.Fatal(err)
	}
	summaryMatches(t, s, p.ID)

	addTaskedStory(t, s, p)
	e, _ := s.SummaryEntry(p.ID)
	if e.TaskCount != 2 || e.TaskStatusCounts[string(TaskPending)] != 2 || len(e.WorkerSessionIDs) != 2 {
		t.Fatalf("bad counts after add: %+v", e)
	}

	// task status change
	s.mu.Lock()
	p.Story[0].Tasks[0].Status = TaskCompleted
	s.mu.Unlock()
	if err := s.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	e, _ = s.SummaryEntry(p.ID)
	if e.TaskStatusCounts[string(TaskCompleted)] != 1 || e.TaskStatusCounts[string(TaskPending)] != 1 {
		t.Fatalf("status change not reflected: %+v", e.TaskStatusCounts)
	}
	summaryMatches(t, s, p.ID)

	// project relink
	s.mu.Lock()
	p.ProjectDir = "/proj/b"
	p.ProjectProfile = "prof"
	s.mu.Unlock()
	if err := s.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	e, _ = s.SummaryEntry(p.ID)
	if e.ProjectDir != "/proj/b" || e.ProjectProfile != "prof" {
		t.Fatalf("relink not reflected: %+v", e)
	}
	if len(s.SummaryEntriesForProject("/proj/b")) != 1 || len(s.SummaryEntriesForProject("/proj/a")) != 0 {
		t.Fatal("project filter wrong")
	}

	// reload from disk
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	summaryMatches(t, s2, p.ID)
	if s2.Revision() == 0 {
		t.Fatal("revision not bumped by load")
	}

	// delete
	if err := s.DeletePRD(p.ID); err != nil {
		t.Fatal(err)
	}
	summaryMatches(t, s, p.ID)
	if len(s.SummaryEntries()) != 0 {
		t.Fatal("entries left after delete")
	}
}

func TestSummaryIndexRevisionBumpsOnWriteNotRead(t *testing.T) {
	s, _ := newSummaryIndexStore(t)
	r0 := s.Revision()
	p, _ := s.CreatePRD("x", "", "", "", "")
	r1 := s.Revision()
	if r1 <= r0 {
		t.Fatalf("create: %d -> %d", r0, r1)
	}
	_ = s.SummaryEntries()
	_, _ = s.SummaryEntry(p.ID)
	_ = s.SummaryEntriesForProject("")
	if s.Revision() != r1 {
		t.Fatal("read bumped revision")
	}
	if err := s.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	r2 := s.Revision()
	if r2 <= r1 {
		t.Fatalf("save: %d -> %d", r1, r2)
	}
	if err := s.DeletePRD(p.ID); err != nil {
		t.Fatal(err)
	}
	if s.Revision() <= r2 {
		t.Fatal("delete did not bump")
	}
}

func TestSummaryIndexNewestFirst(t *testing.T) {
	s, _ := newSummaryIndexStore(t)
	a, _ := s.CreatePRD("a", "", "", "", "")
	time.Sleep(2 * time.Millisecond)
	b, _ := s.CreatePRD("b", "", "", "", "")
	got := s.SummaryEntries()
	if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Fatalf("order wrong: %+v", got)
	}
}

func TestSummaryIndexConcurrentWritersReaders(t *testing.T) {
	s, _ := newSummaryIndexStore(t)
	p, _ := s.CreatePRD("x", "", "", "", "")
	addTaskedStory(t, s, p)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				_, _ = s.CreatePRD("n", "", "", "", "")
				_ = s.UpdatePRDFieldsForIndexTest(p.ID, "t")
			}
		}()
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = s.SummaryEntries()
				_, _ = s.SummaryEntry(p.ID)
				_ = s.Revision()
			}
		}()
	}
	wg.Wait()
	if n := len(s.SummaryEntries()); n != 101 {
		t.Fatalf("entries = %d, want 101", n)
	}
}

func TestSummaryIndexEntryCarriesNoTaskText(t *testing.T) {
	s, _ := newSummaryIndexStore(t)
	p, _ := s.CreatePRD("SECRET-SPEC-TEXT", "", "", "", "")
	addTaskedStory(t, s, p)
	e, _ := s.SummaryEntry(p.ID)
	raw, _ := json.Marshal(e)
	for _, banned := range []string{"SECRET-TASK-TEXT", "SECRET-SPEC-TEXT"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("entry carries record text %q: %s", banned, raw)
		}
	}
}

// UpdatePRDFieldsForIndexTest renames a PRD through the public store path.
func (s *Store) UpdatePRDFieldsForIndexTest(id, title string) error {
	_, err := s.UpdatePRDFields(id, title, "")
	return err
}

// The entry's session ids are the ids Manager.PRDSessionIDs returns, in the
// same order, once the live decompose lookup is added.
func TestSummaryIndexSessionIDsMatchPRDSessionIDs(t *testing.T) {
	f := newCostManager(t)
	prd := newCostPRD(t, f.m, "s-task", "s-verify", "s-judge")
	f.m.SetDecomposeSessionsFn(func(id string) []string { return []string{"s-decomp"} })
	e, ok := f.m.Store().SummaryEntry(prd.ID)
	if !ok {
		t.Fatal("no entry")
	}
	want := f.m.PRDSessionIDs(prd.ID)
	got := append(append([]string(nil), e.SessionIDs...), f.m.DecomposeSessionIDs(prd.ID)...)
	if len(got) != len(want) {
		t.Fatalf("entry+decompose = %v, PRDSessionIDs = %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order differs at %d: %v vs %v", i, got, want)
		}
	}
}
