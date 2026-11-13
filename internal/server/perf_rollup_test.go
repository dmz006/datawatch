// Performance guard for the project roll-up and the Automata list at 700
// Automata (the summary index, the revision-keyed roll-up cache, the
// index-backed list).
//
// perfRollupFixture builds a deterministic store of 700 Automata in a temp
// directory: 10 to 40 tasks each, long decision logs, session history and task
// outputs, so a full record is large. Most are finished, some running, some
// cancelled, some shipped; they spread over 3 projects and several Automations.
// There is no network and no git: the base repository is empty, so the merge
// proof answers "not merged" without running git, and no spend ledger is wired,
// so the cost engine is never asked. The data directory is a t.TempDir; the
// operator's data directory is never touched.
//
// Limits (default multiplier 1):
//
//	roll-up warm   (same revision)                   300 ms
//	roll-up cold   (first read after a write)        1.5 s
//	Automata list  (default response)                300 ms and 2 MB
//
// PERF_ROLLUP_MULTIPLIER=<number >= 1> scales every time limit for a slow CI
// host (for example 3 allows 900 ms warm and 4.5 s cold). The byte limit never
// scales.
//
// The tests also assert, through Store.RecordReadCalls, that neither the
// roll-up nor the list handed out a full record.

package server

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

const (
	perfRollupAutomata    = 700
	perfRollupAutomations = 24
	perfRollupMembersPer  = 10

	perfRollupWarmLimit = 300 * time.Millisecond
	perfRollupColdLimit = 1500 * time.Millisecond
	perfListLimit       = 300 * time.Millisecond
	perfListMaxBytes    = 2 << 20
)

// perfRollupMultiplier reads PERF_ROLLUP_MULTIPLIER (default 1, never below 1).
func perfRollupMultiplier(t testing.TB) float64 {
	t.Helper()
	raw := os.Getenv("PERF_ROLLUP_MULTIPLIER")
	if raw == "" {
		return 1
	}
	m, err := strconv.ParseFloat(raw, 64)
	if err != nil || m < 1 {
		t.Fatalf("PERF_ROLLUP_MULTIPLIER must be a number >= 1, got %q", raw)
	}
	return m
}

func perfScaled(t testing.TB, d time.Duration) time.Duration {
	return time.Duration(float64(d) * perfRollupMultiplier(t))
}

// perfRollupFixture is the wired Server plus what was seeded.
type perfRollupFixture struct {
	Server   *Server
	Manager  *autonomous.Manager
	Projects []string // project names, sorted
	Members  []string // ids of Automata that belong to an Automation
	Counts   map[autonomous.PRDStatus]int
	Shipped  int
	Tasks    int
	Bytes    int // total bytes of the encoded records
}

// perfRollupStatuses is the status mix: most finished, some running.
func perfRollupStatus(i int) autonomous.PRDStatus {
	switch i % 20 {
	case 0, 1, 2:
		return autonomous.PRDRunning
	case 3, 4:
		return autonomous.PRDCancelled
	case 5:
		return autonomous.PRDFailed
	case 6:
		return autonomous.PRDApproved
	case 7:
		return autonomous.PRDDraft
	default:
		return autonomous.PRDCompleted
	}
}

func newPerfRollupFixture(t testing.TB) *perfRollupFixture {
	t.Helper()
	dataDir := t.TempDir()
	txt := newPerfText(20261113)
	rng := rand.New(rand.NewSource(20261113))
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	fx := &perfRollupFixture{Counts: map[autonomous.PRDStatus]int{}}

	projectNames := []string{"alpha", "bravo", "charlie"}
	projects := map[string]config.ProjectConfigEntry{}
	dirs := make([]string, len(projectNames))
	for i, n := range projectNames {
		dirs[i] = filepath.Join(dataDir, "projects", n)
		if err := os.MkdirAll(dirs[i], 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dirs[i], err)
		}
		projects[n] = config.ProjectConfigEntry{Dir: dirs[i]}
	}
	fx.Projects = projectNames

	if err := os.MkdirAll(filepath.Join(dataDir, "autonomous"), 0o755); err != nil {
		t.Fatalf("mkdir autonomous: %v", err)
	}
	f, err := os.Create(filepath.Join(dataDir, "autonomous", "prds.jsonl"))
	if err != nil {
		t.Fatalf("create prds.jsonl: %v", err)
	}
	enc := json.NewEncoder(f)
	for i := 0; i < perfRollupAutomata; i++ {
		prd := perfRollupBuild(txt, rng, i, base, dirs[i%len(dirs)])
		fx.Counts[prd.Status]++
		if prd.MergeCommit != "" {
			fx.Shipped++
		}
		for _, st := range prd.Story {
			fx.Tasks += len(st.Tasks)
		}
		raw, _ := json.Marshal(prd)
		fx.Bytes += len(raw)
		if err := enc.Encode(prd); err != nil {
			t.Fatalf("encode prd %d: %v", i, err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close prds.jsonl: %v", err)
	}

	mgr, err := autonomous.NewManager(dataDir, autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	if repo := mgr.Config().WorktreeBaseRepo; repo != "" {
		t.Fatalf("fixture needs an empty WorktreeBaseRepo so the merge proof runs no git, got %q", repo)
	}
	fx.Manager = mgr

	// Automations: groups of finished Automata, in id order.
	var finished []string
	for _, p := range mgr.Store().SummaryEntries() {
		if p.Status == autonomous.PRDCompleted {
			finished = append(finished, p.ID)
		}
	}
	sortStrings(finished) // deterministic order
	for a := 0; a < perfRollupAutomations && (a+1)*perfRollupMembersPer <= len(finished); a++ {
		var members []autonomous.AutomationMember
		for _, id := range finished[a*perfRollupMembersPer : (a+1)*perfRollupMembersPer] {
			members = append(members, autonomous.AutomationMember{PRDID: id})
			fx.Members = append(fx.Members, id)
		}
		if _, err := mgr.CreateAutomation(autonomous.AutomationInput{
			Name: fmt.Sprintf("perf-rollup-automation-%02d", a), Members: members,
		}, "perf-rollup"); err != nil {
			t.Fatalf("CreateAutomation %d: %v", a, err)
		}
	}

	cfg := config.DefaultConfig()
	cfg.Projects = projects
	fx.Server = &Server{cfg: cfg, autonomousMgr: newTestAPI(mgr)}
	return fx
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// perfRollupBuild builds one Automaton: 10 to 40 tasks over several stories,
// a long decision log, session history and task outputs.
func perfRollupBuild(txt *perfText, rng *rand.Rand, i int, base time.Time, dir string) *autonomous.PRD {
	id := fmt.Sprintf("%08x", 0x20000000+i)
	status := perfRollupStatus(i)
	created := base.Add(time.Duration(i) * 40 * time.Minute)
	prd := &autonomous.PRD{
		ID: id, Title: fmt.Sprintf("perf rollup automaton %03d", i), Spec: txt.take(3000),
		ProjectDir: dir, Status: status, CreatedAt: created, UpdatedAt: created.Add(2 * time.Hour),
		PlannerSessionID: fmt.Sprintf("plan-%04d", i),
	}
	if i%9 == 0 {
		prd.Kind = autonomous.PRDKindBug
	}
	nTasks := 10 + rng.Intn(31)
	nStories := 2 + rng.Intn(4)
	for s := 0; s < nStories; s++ {
		sid := fmt.Sprintf("%s-s%02x", id, s)
		prd.Story = append(prd.Story, autonomous.Story{
			ID: sid, PRDID: id, Title: fmt.Sprintf("story %d", s), Description: txt.take(600),
			Status: autonomous.StoryPending, CreatedAt: created, UpdatedAt: created,
		})
	}
	for k := 0; k < nTasks; k++ {
		si := k % nStories
		sid := prd.Story[si].ID
		tid := fmt.Sprintf("%s-t%03x", id, k)
		started := created.Add(time.Duration(k) * 10 * time.Minute)
		done := started.Add(8 * time.Minute)
		task := autonomous.Task{
			ID: tid, StoryID: sid, PRDID: id, Title: fmt.Sprintf("task %d", k),
			Spec: txt.take(1200), CreatedAt: created, UpdatedAt: done,
			SessionID: fmt.Sprintf("work-%04d-%03d", i, k), VerifierSessionID: fmt.Sprintf("ver-%04d-%03d", i, k),
			Error:            txt.take(800),
			AttemptRefs:      []string{txt.take(800)},
			StartFailureTail: []string{txt.take(1500)}, // task output tail
			Verification: &autonomous.VerificationResult{
				OK: true, Summary: "verified", VerifiedAt: done, RawJudge: txt.take(1500),
			},
		}
		switch status {
		case autonomous.PRDCompleted:
			task.Status = autonomous.TaskCompleted
			task.StartedAt, task.CompletedAt = &started, &done
		case autonomous.PRDRunning:
			if k < nTasks/2 {
				task.Status = autonomous.TaskCompleted
				task.StartedAt, task.CompletedAt = &started, &done
			} else if k == nTasks/2 {
				task.Status = autonomous.TaskInProgress
				task.StartedAt = &started
			} else {
				task.Status = autonomous.TaskPending
			}
		case autonomous.PRDFailed:
			task.Status = autonomous.TaskCompleted
			if k == nTasks-1 {
				task.Status = autonomous.TaskFailed
			}
			task.StartedAt, task.CompletedAt = &started, &done
		case autonomous.PRDCancelled:
			task.Status = autonomous.TaskCancelled
		default:
			task.Status = autonomous.TaskPending
		}
		prd.Story[si].Tasks = append(prd.Story[si].Tasks, task)
	}
	// A long decision log (the store trims it when it saves, so it stays at the cap).
	for d := 0; d < 120; d++ {
		prd.Decisions = append(prd.Decisions, autonomous.Decision{
			At: created.Add(time.Duration(d) * time.Minute), Kind: "run", Backend: "claude-code",
			Actor: "autonomous", Note: txt.take(400), JudgeSessionID: fmt.Sprintf("judge-%04d-%03d", i, d%12),
		})
	}
	// Some shipped: finished work that carries a merge record.
	if status == autonomous.PRDCompleted && i%7 == 0 {
		merged := created.Add(5 * time.Hour)
		prd.MergeCommit = fmt.Sprintf("%040x", i+1)
		prd.MergedInRelease = "v9.0." + strconv.Itoa(i%40)
		prd.MergedAt = &merged
	}
	return prd
}

func perfRollupGet(t testing.TB, s *Server, project string) (*httptest.ResponseRecorder, time.Duration) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+project+"/rollup?window=7", nil)
	req.SetPathValue("name", project)
	rr := httptest.NewRecorder()
	start := time.Now()
	s.handleProjectRollup(rr, req)
	d := time.Since(start)
	if rr.Code != http.StatusOK {
		t.Fatalf("rollup %s: status %d body=%s", project, rr.Code, rr.Body.String())
	}
	return rr, d
}

// perfRollupWrite changes one member Automaton, which bumps the store revision.
func perfRollupWrite(t testing.TB, fx *perfRollupFixture, id string) {
	t.Helper()
	st := fx.Manager.Store()
	p, ok := st.GetPRD(id)
	if !ok {
		t.Fatalf("no Automaton %s", id)
	}
	p.Title += "."
	if err := st.SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
}

func TestPerfRollupFixtureShape(t *testing.T) {
	fx := newPerfRollupFixture(t)
	n := len(fx.Manager.Store().SummaryEntries())
	if n != perfRollupAutomata {
		t.Fatalf("fixture holds %d Automata, want %d", n, perfRollupAutomata)
	}
	if len(fx.Manager.ListAutomations()) < 3 {
		t.Fatalf("fixture holds %d Automations, want several", len(fx.Manager.ListAutomations()))
	}
	for _, st := range []autonomous.PRDStatus{autonomous.PRDCompleted, autonomous.PRDRunning, autonomous.PRDCancelled} {
		if fx.Counts[st] == 0 {
			t.Errorf("fixture has no %s Automaton", st)
		}
	}
	if fx.Counts[autonomous.PRDCompleted] < perfRollupAutomata/2 {
		t.Errorf("most Automata must be finished, got %d of %d", fx.Counts[autonomous.PRDCompleted], perfRollupAutomata)
	}
	if fx.Shipped == 0 {
		t.Error("fixture has no shipped Automaton")
	}
	if avg := fx.Bytes / perfRollupAutomata; avg < 50<<10 {
		t.Errorf("average full record is only %d bytes; the fixture must make records large", avg)
	}
	t.Logf("fixture: %d Automata, %d tasks, %d MB of records, %d shipped, counts %v",
		n, fx.Tasks, fx.Bytes>>20, fx.Shipped, fx.Counts)
}

func TestPerfRollupWarmRead(t *testing.T) {
	fx := newPerfRollupFixture(t)
	limit := perfScaled(t, perfRollupWarmLimit)
	reads := fx.Manager.Store().RecordReadCalls()
	for _, p := range fx.Projects {
		cold, _ := perfRollupGet(t, fx.Server, p)
		warm, d := perfRollupGet(t, fx.Server, p)
		if d > limit {
			t.Errorf("warm roll-up of %s took %v, limit %v", p, d, limit)
		}
		if cold.Body.String() != warm.Body.String() {
			t.Errorf("warm roll-up of %s was rebuilt: the body changed with no write", p)
		}
		t.Logf("%s warm %v (%d bytes)", p, d, warm.Body.Len())
	}
	if got := fx.Manager.Store().RecordReadCalls(); got != reads {
		t.Errorf("roll-up read %d full records, want 0", got-reads)
	}
}

func TestPerfRollupColdRead(t *testing.T) {
	fx := newPerfRollupFixture(t)
	limit := perfScaled(t, perfRollupColdLimit)
	// A write first: the cache is empty and the index is built.
	perfRollupWrite(t, fx, fx.Members[0])
	reads := fx.Manager.Store().RecordReadCalls()
	for _, p := range fx.Projects {
		rr, d := perfRollupGet(t, fx.Server, p)
		if d > limit {
			t.Errorf("cold roll-up of %s took %v, limit %v", p, d, limit)
		}
		t.Logf("%s cold %v (%d bytes)", p, d, rr.Body.Len())
	}
	if got := fx.Manager.Store().RecordReadCalls(); got != reads {
		t.Errorf("roll-up read %d full records, want 0", got-reads)
	}
}

func TestPerfRollupWriteInvalidates(t *testing.T) {
	fx := newPerfRollupFixture(t)
	limit := perfScaled(t, perfRollupColdLimit)
	project := fx.Projects[0]
	first, _ := perfRollupGet(t, fx.Server, project)
	again, _ := perfRollupGet(t, fx.Server, project)
	if first.Body.String() != again.Body.String() {
		t.Fatal("a repeat read with no write must be served from the cache")
	}

	rev := fx.Manager.Store().Revision()
	perfRollupWrite(t, fx, fx.Members[len(fx.Members)/2])
	if fx.Manager.Store().Revision() == rev {
		t.Fatal("a write must bump the store revision")
	}
	reads := fx.Manager.Store().RecordReadCalls()
	time.Sleep(2 * time.Millisecond) // generated_at is the build time; make it differ
	after, d := perfRollupGet(t, fx.Server, project)
	if d > limit {
		t.Errorf("read after a write took %v, limit %v", d, limit)
	}
	if after.Body.String() == first.Body.String() {
		t.Error("the read after a write was served from the stale cache")
	}
	if got := fx.Manager.Store().RecordReadCalls(); got != reads {
		t.Errorf("rebuild read %d full records, want 0", got-reads)
	}
	// And the rebuilt roll-up is warm again.
	next, d2 := perfRollupGet(t, fx.Server, project)
	if next.Body.String() != after.Body.String() {
		t.Error("the rebuilt roll-up was not cached")
	}
	if d2 > perfScaled(t, perfRollupWarmLimit) {
		t.Errorf("read after the rebuild took %v, warm limit %v", d2, perfScaled(t, perfRollupWarmLimit))
	}
}

func TestPerfAutomataListDefault(t *testing.T) {
	fx := newPerfRollupFixture(t)
	limit := perfScaled(t, perfListLimit)
	reads := fx.Manager.Store().RecordReadCalls()
	lists := fx.Manager.Store().ListPRDCalls()
	for pass := 0; pass < 2; pass++ { // first read, then a repeat
		start := time.Now()
		rr := perfGetWith(fx.Server.handleAutonomousPRDs, "/api/autonomous/prds", "")
		d := time.Since(start)
		if rr.Code != http.StatusOK {
			t.Fatalf("list: status %d", rr.Code)
		}
		if rr.Body.Len() > perfListMaxBytes {
			t.Errorf("list body %d bytes exceeds %d", rr.Body.Len(), perfListMaxBytes)
		}
		if d > limit {
			t.Errorf("list pass %d took %v, limit %v", pass, d, limit)
		}
		t.Logf("list pass %d: %v, %d bytes", pass, d, rr.Body.Len())
	}
	if got := fx.Manager.Store().RecordReadCalls(); got != reads {
		t.Errorf("list read %d full records, want 0", got-reads)
	}
	if got := fx.Manager.Store().ListPRDCalls(); got != lists {
		t.Errorf("list called ListPRDs %d times, want 0", got-lists)
	}
}

func TestPerfAutomataListAfterWrite(t *testing.T) {
	fx := newPerfRollupFixture(t)
	limit := perfScaled(t, perfListLimit)
	first := perfGetWith(fx.Server.handleAutonomousPRDs, "/api/autonomous/prds", "")
	etag := first.Header().Get("ETag")
	perfRollupWrite(t, fx, fx.Members[0])
	reads := fx.Manager.Store().RecordReadCalls()
	start := time.Now()
	rr := perfGetWith(fx.Server.handleAutonomousPRDs, "/api/autonomous/prds", etag)
	d := time.Since(start)
	if rr.Code != http.StatusOK {
		t.Fatalf("list after a write with the old ETag: status %d, want 200", rr.Code)
	}
	if d > limit || rr.Body.Len() > perfListMaxBytes {
		t.Errorf("list after a write took %v with %d bytes (limits %v, %d)", d, rr.Body.Len(), limit, perfListMaxBytes)
	}
	if got := fx.Manager.Store().RecordReadCalls(); got != reads {
		t.Errorf("list read %d full records, want 0", got-reads)
	}
}

// BenchmarkProjectRollupWarm measures a roll-up read served from the cache.
func BenchmarkProjectRollupWarm(b *testing.B) {
	fx := newPerfRollupFixture(b)
	perfRollupGet(b, fx.Server, fx.Projects[0]) // fill the cache
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		perfRollupGet(b, fx.Server, fx.Projects[0])
	}
}

// BenchmarkProjectRollupCold measures a roll-up read right after a write, so
// the cache misses and the roll-up is built from the summary index.
func BenchmarkProjectRollupCold(b *testing.B) {
	fx := newPerfRollupFixture(b)
	perfRollupGet(b, fx.Server, fx.Projects[0])
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		perfRollupWrite(b, fx, fx.Members[i%len(fx.Members)])
		b.StartTimer()
		perfRollupGet(b, fx.Server, fx.Projects[0])
	}
}
