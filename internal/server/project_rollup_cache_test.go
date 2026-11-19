// Tests for the project roll-up cache, its memos and the index-only read path.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestRollupCache_HitDoesNotCallBuilder(t *testing.T) {
	c := newRollupCache(8)
	key := rollupCacheKey{project: "acme", windowDays: 7, revision: 3}
	var builds int
	build := func() []byte { builds++; return []byte("body") }
	if b, hit := c.get(key, build); hit || string(b) != "body" {
		t.Fatalf("first read hit=%v body=%q", hit, b)
	}
	if b, hit := c.get(key, build); !hit || string(b) != "body" {
		t.Fatalf("second read hit=%v body=%q", hit, b)
	}
	if builds != 1 {
		t.Fatalf("builder ran %d times, want 1", builds)
	}
	key.revision = 4
	c.get(key, build)
	if builds != 2 {
		t.Fatalf("a new revision must rebuild, builds=%d", builds)
	}
	if c.len() != 1 {
		t.Fatalf("the older revision of the same project and window must be dropped, len=%d", c.len())
	}
}

func TestRollupCache_ConcurrentColdReadsBuildOnce(t *testing.T) {
	c := newRollupCache(8)
	key := rollupCacheKey{project: "acme", windowDays: 7, revision: 1}
	var builds atomic.Int32
	release := make(chan struct{})
	build := func() []byte {
		builds.Add(1)
		<-release
		return []byte("once")
	}
	const readers = 16
	var wg sync.WaitGroup
	results := make([]string, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, _ := c.get(key, build)
			results[i] = string(b)
		}(i)
	}
	time.Sleep(50 * time.Millisecond) // let every reader reach the guard
	close(release)
	wg.Wait()
	if n := builds.Load(); n != 1 {
		t.Fatalf("concurrent cold reads built %d times, want 1", n)
	}
	for i, r := range results {
		if r != "once" {
			t.Fatalf("reader %d got %q", i, r)
		}
	}
}

func TestRollupCache_BoundedAndEvictsProject(t *testing.T) {
	c := newRollupCache(3)
	for i := 0; i < 10; i++ {
		c.get(rollupCacheKey{project: "p", windowDays: i + 1, revision: 1}, func() []byte { return []byte("x") })
	}
	if c.len() != 3 {
		t.Fatalf("len = %d, want the bound 3", c.len())
	}
	c.get(rollupCacheKey{project: "other", windowDays: 7, revision: 1}, func() []byte { return []byte("y") })
	c.evictProject("p")
	if c.len() != 1 {
		t.Fatalf("after evicting p, len = %d, want 1 (other)", c.len())
	}
	var rebuilt bool
	c.get(rollupCacheKey{project: "p", windowDays: 10, revision: 1}, func() []byte { rebuilt = true; return nil })
	if !rebuilt {
		t.Fatal("an evicted project must rebuild")
	}
}

func TestRollupProofCache_KnownProofRunsNoGit(t *testing.T) {
	c := newRollupProofCache(8)
	now := time.Now()
	runs := 0
	prove := func(merged bool) func() (bool, time.Time) {
		return func() (bool, time.Time) { runs++; return merged, now }
	}
	e := autonomous.SummaryEntry{ID: "a", GitBranch: "auto/a", UpdatedAt: now}
	k := rollupProofKey("/repo", e)
	for i := 0; i < 3; i++ {
		if m, _ := c.resolve(k, now.Add(time.Duration(i)*time.Hour), prove(true)); !m {
			t.Fatal("merged proof lost")
		}
	}
	if runs != 1 {
		t.Fatalf("merged proof ran git %d times, want 1 (a merged branch stays merged)", runs)
	}

	neg := rollupProofKey("/repo", autonomous.SummaryEntry{ID: "b", GitBranch: "auto/b", UpdatedAt: now})
	runs = 0
	c.resolve(neg, now, prove(false))
	c.resolve(neg, now.Add(rollupProofNegativeTTL/2), prove(false))
	if runs != 1 {
		t.Fatalf("not-merged proof ran %d times inside the negative window, want 1", runs)
	}
	c.resolve(neg, now.Add(rollupProofNegativeTTL+time.Second), prove(false))
	if runs != 2 {
		t.Fatalf("not-merged proof must be asked again after the bounded window, runs=%d", runs)
	}

	e.UpdatedAt = now.Add(time.Minute) // the record changed: a new proof key
	if rollupProofKey("/repo", e) == k {
		t.Fatal("a changed record must change the proof key")
	}
}

func TestRollupProofCache_Bounded(t *testing.T) {
	c := newRollupProofCache(4)
	now := time.Now()
	for i := 0; i < 20; i++ {
		c.resolve(string(rune('a'+i)), now, func() (bool, time.Time) { return true, now })
	}
	c.mu.Lock()
	n := len(c.m)
	c.mu.Unlock()
	if n > 4 {
		t.Fatalf("proof cache holds %d, bound is 4", n)
	}
}

func TestRollupPointsMemo_ByRevisionAndAge(t *testing.T) {
	m := &rollupPointsMemo{byWindow: map[int]rollupPointsRec{}}
	now := time.Now()
	if _, ok := m.get(7, 1, now); ok {
		t.Fatal("empty memo must miss")
	}
	m.put(7, 1, now, map[string]float64{"a": 1})
	if p, ok := m.get(7, 1, now.Add(time.Second)); !ok || p["a"] != 1 {
		t.Fatal("same revision must hit")
	}
	if _, ok := m.get(7, 2, now); ok {
		t.Fatal("a new revision must miss")
	}
	if _, ok := m.get(30, 1, now); ok {
		t.Fatal("another window must miss")
	}
	if _, ok := m.get(7, 1, now.Add(rollupPointsMaxAge+time.Second)); ok {
		t.Fatal("memo must expire when spend may have moved without a write")
	}
}

// rollupHTTP calls the roll-up handler and returns the recorder.
func rollupHTTP(t *testing.T, s *Server, project string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+project+"/rollup?window=7", nil)
	req.SetPathValue("name", project)
	rr := httptest.NewRecorder()
	s.handleProjectRollup(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	return rr
}

func rollupStoredPRD(t *testing.T, mgr *autonomous.Manager, dir string, status autonomous.PRDStatus, tasks ...autonomous.Task) *autonomous.PRD {
	t.Helper()
	prd, err := mgr.CreatePRD("spec", dir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	stored, _ := mgr.Store().GetPRD(prd.ID)
	stored.Status = status
	stored.Story = []autonomous.Story{{ID: "s-" + prd.ID, Tasks: tasks}}
	if err := mgr.Store().SavePRD(stored); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return stored
}

func TestProjectRollup_HandlerCachesAndInvalidates(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	s.cfg.Projects = map[string]config.ProjectConfigEntry{"acme": {Dir: dirA}, "other": {Dir: dirB}}

	mine := rollupStoredPRD(t, mgr, dirA, autonomous.PRDRunning,
		autonomous.Task{ID: "t1", Status: autonomous.TaskPending})
	theirs := rollupStoredPRD(t, mgr, dirB, autonomous.PRDRunning,
		autonomous.Task{ID: "o1", Status: autonomous.TaskPending})

	first := rollupHTTP(t, s, "acme").Body.String()
	// Same key: the identical body, with the same generated_at, is served.
	time.Sleep(5 * time.Millisecond)
	if again := rollupHTTP(t, s, "acme").Body.String(); again != first {
		t.Fatalf("a repeat read with an unchanged key must serve the cached body")
	}

	// A write to an Automaton of another project may rebuild, but must not
	// change what this project reports.
	other, _ := mgr.Store().GetPRD(theirs.ID)
	other.Story[0].Tasks[0].Status = autonomous.TaskCompleted
	if err := mgr.Store().SavePRD(other); err != nil {
		t.Fatal(err)
	}
	var before, after projectRollup
	mustDecodeRollup(t, first, &before)
	mustDecodeRollup(t, rollupHTTP(t, s, "acme").Body.String(), &after)
	if before.Total.Tasks != after.Total.Tasks || len(before.Total.Automata) != len(after.Total.Automata) {
		t.Fatalf("another project's write changed this project's result: %+v vs %+v", before.Total, after.Total)
	}

	// A task status change on a member invalidates.
	m, _ := mgr.Store().GetPRD(mine.ID)
	done := time.Now()
	m.Story[0].Tasks[0].Status = autonomous.TaskCompleted
	m.Story[0].Tasks[0].CompletedAt = &done
	if err := mgr.Store().SavePRD(m); err != nil {
		t.Fatal(err)
	}
	var changed projectRollup
	mustDecodeRollup(t, rollupHTTP(t, s, "acme").Body.String(), &changed)
	if changed.Total.Tasks != (rollupTasks{Done: 1, Total: 1}) {
		t.Fatalf("a member task change was not picked up: %+v", changed.Total.Tasks)
	}
	if changed.Total.Throughput.TasksFinished != 1 {
		t.Fatalf("tasks finished = %d, want 1", changed.Total.Throughput.TasksFinished)
	}
	if !changed.GeneratedAt.After(before.GeneratedAt) {
		t.Fatalf("generated_at must be the time of the rebuild: %v then %v", before.GeneratedAt, changed.GeneratedAt)
	}

	// Project config change: arcs are part of the key and evict the project.
	s.cfg.Projects["acme"] = config.ProjectConfigEntry{Dir: dirA, Arcs: []config.ProjectArc{{AutomationID: "arc-x", Priority: 1}}}
	var withArc projectRollup
	mustDecodeRollup(t, rollupHTTP(t, s, "acme").Body.String(), &withArc)
	if len(withArc.Arcs) != 1 || withArc.Arcs[0].AutomationID != "arc-x" {
		t.Fatalf("a project config change must rebuild: %+v", withArc.Arcs)
	}
	s.evictRollupProject("acme")
	if n := s.rollup.cache.len(); n != 0 {
		t.Fatalf("evicting the only cached project left %d entries", n)
	}
}

func TestProjectRollup_HandlersNeverListFullRecords(t *testing.T) {
	dir := t.TempDir()
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	s.cfg.Projects = map[string]config.ProjectConfigEntry{"acme": {Dir: dir}}
	done := time.Now()
	rollupStoredPRD(t, mgr, dir, autonomous.PRDCompleted,
		autonomous.Task{ID: "t1", Status: autonomous.TaskCompleted, CompletedAt: &done})

	calls := mgr.Store().ListPRDCalls()
	rollupHTTP(t, s, "acme")

	for _, c := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/api/projects/acme/timeline", s.handleProjectTimeline},
		{"/api/projects/acme/timeline?level=automata", s.handleProjectTimeline},
		{"/api/projects/acme/history", s.handleProjectHistory},
	} {
		path := c.path
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.SetPathValue("name", "acme")
		rr := httptest.NewRecorder()
		c.handler(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rr.Code, rr.Body.String())
		}
	}
	preq := httptest.NewRequest(http.MethodGet, "/api/autonomous/progress", nil)
	prr := httptest.NewRecorder()
	s.handleAutonomousProgress(prr, preq)
	if prr.Code != http.StatusOK {
		t.Fatalf("progress status = %d body=%s", prr.Code, prr.Body.String())
	}

	if got := mgr.Store().ListPRDCalls(); got != calls {
		t.Fatalf("the roll-up, timeline, history and progress reads called ListPRDs %d times; the index answers them", got-calls)
	}
}

func mustDecodeRollup(t *testing.T, body string, into *projectRollup) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), into); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
