package autonomous

import (
	"sync"
	"testing"
	"time"
)

// dupFake is a session table whose killer really ends the session, so the next
// listing shows it gone (confirmed exit). stubborn ids survive a kill.
type dupFake struct {
	mu       sync.Mutex
	infos    []SessionInfo
	kills    []string
	stubborn map[string]bool
	audits   []string
}

func (f *dupFake) list() []SessionInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SessionInfo(nil), f.infos...)
}

func (f *dupFake) kill(id, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kills = append(f.kills, id)
	if f.stubborn[id] {
		return nil
	}
	for i := range f.infos {
		if f.infos[i].ID == id {
			f.infos[i].Alive = false
		}
	}
	return nil
}

// dies ends a session on its own, with no kill.
func (f *dupFake) dies(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.infos {
		if f.infos[i].ID == id {
			f.infos[i].Alive = false
		}
	}
}

func (f *dupFake) killed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.kills...)
}

// dupSetup builds a manager with two live sessions for one task: "old" created
// first, "new" created after.
func dupSetup(t *testing.T, keep string) (*Manager, *PRD, *Task, *dupFake) {
	t.Helper()
	endSessionsWait, endSessionsPoll = 50*time.Millisecond, 5*time.Millisecond
	m := capFixtureManager(t)
	cur, dir := pendingTaskWithDir(t, m)
	tk := &cur.Story[0].Tasks[0]
	base := time.Now().Add(-time.Hour)
	f := &dupFake{stubborn: map[string]bool{}, infos: []SessionInfo{
		{ID: "old", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir, Alive: true, CreatedAt: base},
		{ID: "new", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir, Alive: true, CreatedAt: base.Add(10 * time.Minute)},
	}}
	m.SetSessionListerFn(f.list)
	m.SetSessionKillerFn(f.kill)
	m.SetAuditFn(func(action string, _ map[string]any) { f.audits = append(f.audits, action) })
	m.mu.Lock()
	m.cfg.DuplicateSessionKeep = keep
	m.mu.Unlock()
	return m, cur, tk, f
}

func TestDecideDuplicateSession_Pure(t *testing.T) {
	base := time.Now()
	live := []SessionInfo{
		{ID: "b", Alive: true, CreatedAt: base.Add(time.Minute)},
		{ID: "a", Alive: true, CreatedAt: base},
		{ID: "dead", Alive: false, CreatedAt: base.Add(-time.Hour)},
	}
	for keep, want := range map[string]string{"": "a", "older": "a", "bogus": "a", "newer": "b", "NEWER": "b"} {
		kept, ended := DecideDuplicateSession(live, keep)
		if kept == nil || kept.ID != want || len(ended) != 1 {
			t.Fatalf("keep=%q: kept=%v ended=%v, want %s", keep, kept, ended, want)
		}
	}
	if k, e := DecideDuplicateSession(nil, "older"); k != nil || e != nil {
		t.Fatalf("empty input: %v %v", k, e)
	}
}

func TestDuplicateSession_OlderKeptByDefault(t *testing.T) {
	m, prd, tk, f := dupSetup(t, "")
	got, _ := findAdoptableSessionKeep(prd, tk, f.list(), m.duplicateSessionKeep())
	if got == nil || got.ID != "old" {
		t.Fatalf("default adopts %v, want old", got)
	}
	ended := m.endDuplicateSessions(prd, tk, "old", "test", "dup")
	if len(ended) != 1 || ended[0] != "new" || len(f.killed()) != 1 || f.killed()[0] != "new" {
		t.Fatalf("ended=%v kills=%v, want only new", ended, f.killed())
	}
	if len(tk.SessionDecisions) != 1 || tk.SessionDecisions[0].Kept != "old" || tk.SessionDecisions[0].Ended[0] != "new" {
		t.Fatalf("decisions = %+v", tk.SessionDecisions)
	}
	if len(f.audits) != 1 || f.audits[0] != auditActionDuplicateSession {
		t.Fatalf("audits = %v", f.audits)
	}
}

func TestDuplicateSession_OverrideToNewer(t *testing.T) {
	m, prd, tk, f := dupSetup(t, "newer")
	got, _ := findAdoptableSessionKeep(prd, tk, f.list(), m.duplicateSessionKeep())
	if got == nil || got.ID != "new" {
		t.Fatalf("newer override adopts %v, want new", got)
	}
	ended := m.endDuplicateSessions(prd, tk, "new", "test", "dup")
	if len(ended) != 1 || ended[0] != "old" || tk.SessionDecisions[0].Kept != "new" {
		t.Fatalf("ended=%v decisions=%+v", ended, tk.SessionDecisions)
	}
}

func TestDuplicateSession_SessionEndsOnItsOwn(t *testing.T) {
	// The loser ends by itself before the end request: it is not killed, and the
	// decision says so.
	m, prd, tk, f := dupSetup(t, "")
	f.dies("new")
	if ended := m.endDuplicateSessions(prd, tk, "old", "test", "dup"); len(ended) != 0 {
		t.Fatalf("ended = %v, want none", ended)
	}
	// Nothing live to end: no kill and no decision (nothing was decided).
	if len(f.killed()) != 0 || len(tk.SessionDecisions) != 0 {
		t.Fatalf("kills=%v decisions=%+v", f.killed(), tk.SessionDecisions)
	}

	// The kept session ends by itself: nothing is ended, the survivor is left.
	m, prd, tk, f = dupSetup(t, "")
	f.dies("old")
	if ended := m.endDuplicateSessions(prd, tk, "old", "test", "dup"); len(ended) != 0 || len(f.killed()) != 0 {
		t.Fatalf("kept gone: ended=%v kills=%v", ended, f.killed())
	}
}

func TestDuplicateSession_ExitMustBeConfirmed(t *testing.T) {
	m, prd, tk, f := dupSetup(t, "")
	f.stubborn["new"] = true
	if ended := m.endDuplicateSessions(prd, tk, "old", "test", "dup"); len(ended) != 0 {
		t.Fatalf("ended = %v; an unconfirmed exit must not count", ended)
	}
	if len(tk.SessionDecisions) != 0 {
		t.Fatalf("decision recorded without confirmed exit: %+v", tk.SessionDecisions)
	}
}

func TestDuplicateSession_DecisionRecordedOnce(t *testing.T) {
	m, prd, tk, f := dupSetup(t, "")
	m.endDuplicateSessions(prd, tk, "old", "test", "dup")
	// A repeat lookup finds the same pair again (the table is restored) and
	// ends it again; the decision and audit entry are not duplicated.
	f.mu.Lock()
	f.infos[1].Alive = true
	f.mu.Unlock()
	m.endDuplicateSessions(prd, tk, "old", "test", "dup")
	if len(tk.SessionDecisions) != 1 || len(f.audits) != 1 {
		t.Fatalf("decisions=%d audits=%d, want 1 each", len(tk.SessionDecisions), len(f.audits))
	}
}

func TestDuplicateSession_PreSpawnKeepsOlder(t *testing.T) {
	m, prd, tk, f := dupSetup(t, "")
	_, dir := prd, m.resolveTaskProjectDir(prd, tk)
	f.mu.Lock()
	for i := range f.infos {
		f.infos[i].ProjectDir, f.infos[i].TmuxSession = dir, "tmux-"+f.infos[i].ID
	}
	f.mu.Unlock()
	if id := m.adoptLiveWorkerBeforeSpawn(prd, tk, 0, dir); id != "old" {
		t.Fatalf("pre-spawn adopted %q, want old", id)
	}
	if k := f.killed(); len(k) != 1 || k[0] != "new" {
		t.Fatalf("kills = %v, want [new]", k)
	}
	if len(tk.SessionDecisions) != 1 {
		t.Fatalf("decisions = %+v", tk.SessionDecisions)
	}
}
