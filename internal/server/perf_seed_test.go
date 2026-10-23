// Deterministic heavy-data seed and endpoint measurement for the web UI load
// path (docs/plans/2026-10-23-web-ui-load-performance.md).
//
// SeedPerfServer builds a Server over a throwaway data dir holding 3000
// sessions (some with a 128 KB task, logs and transcripts) and 300 Automata
// (a few carrying 7-13 MB of story data), independent of any live daemon.
// The TestEndpointMeasure* tests then call every endpoint the web UI hits on
// load and record raw bytes, gzipped bytes and server time.
//
// PERF_REPORT_OUT=<path> makes the measurement tests also write their rows as
// JSON to <path>.

package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

const (
	perfSeedSessions        = 3000
	perfSeedBigTaskEvery    = 20 // every Nth session carries a 128 KB task
	perfSeedBigTaskBytes    = 128 * 1024
	perfSeedLogEvery        = 25 // every Nth session has an output log + transcript on disk
	perfSeedLogBytes        = 256 * 1024
	perfSeedAutomata        = 300
	perfSeedAutomations     = 20
	perfSeedAlerts          = 500
	perfSeedLightAutomatonB = 60 * 1024
)

// perfSeedHeavyBytes are the story-data sizes of the heavy Automata (7-13 MB).
var perfSeedHeavyBytes = []int{7 << 20, 8 << 20, 9 << 20, 10 << 20, 11 << 20, 13 << 20}

// PerfSeed is what SeedPerfServer returns: the wired Server plus the counts
// that were actually seeded.
type PerfSeed struct {
	Server      *Server
	DataDir     string
	Sessions    int
	BigTasks    int
	WithLogs    int
	Automata    int
	HeavyBytes  []int
	Automations int
	Alerts      int
}

// perfText produces deterministic, realistically compressible prose/log text.
type perfText struct {
	rng  *rand.Rand
	pool []byte
}

var perfWords = strings.Fields(`the task session output checkpoint verifier result error warning retry
build compile test passed failed running waiting capacity backend model prompt
transcript diff commit branch worktree story automaton guardrail scan config
func return struct interface context handler request response latency payload
internal server manager store autonomous executor decompose approve reject`)

func newPerfText(seed int64) *perfText {
	r := rand.New(rand.NewSource(seed))
	var b bytes.Buffer
	for b.Len() < 4<<20 {
		fmt.Fprintf(&b, "[%06d] %s %s=%x %s %s %s\n", r.Intn(1000000),
			perfWords[r.Intn(len(perfWords))], perfWords[r.Intn(len(perfWords))], r.Uint32(),
			perfWords[r.Intn(len(perfWords))], perfWords[r.Intn(len(perfWords))], perfWords[r.Intn(len(perfWords))])
	}
	return &perfText{rng: r, pool: b.Bytes()}
}

// take returns n deterministic bytes of text (a rotating window over the pool).
func (p *perfText) take(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n)
	off := p.rng.Intn(len(p.pool))
	for len(out) < n {
		chunk := p.pool[off%len(p.pool):]
		if len(chunk) > n-len(out) {
			chunk = chunk[:n-len(out)]
		}
		out = append(out, chunk...)
		off += len(chunk)
	}
	return string(out)
}

// perfStubObserver serves the two observer reads the UI makes on load; the
// embedded nil interface panics if anything else is called.
type perfStubObserver struct {
	ObserverAPI
	sessions int
}

func (o perfStubObserver) Stats() any {
	return map[string]any{"host": map[string]any{"cpu_pct": 12.5, "mem_pct": 41.0}, "sessions": o.sessions}
}

func (o perfStubObserver) Envelopes() any {
	out := make([]map[string]any, 0, o.sessions)
	for i := 0; i < o.sessions; i++ {
		out = append(out, map[string]any{"id": fmt.Sprintf("env-%04d", i), "cpu_pct": float64(i%100) / 10, "rss_bytes": 1 << 20})
	}
	return out
}

// SeedPerfServer seeds dataDir deterministically and returns a Server wired
// to it. Sessions and Automata are written straight to their store files
// before the managers open them, because both stores rewrite their whole
// file on every save (seeding 3000 sessions through SaveSession is quadratic).
func SeedPerfServer(t testing.TB, dataDir string) *PerfSeed {
	t.Helper()
	txt := newPerfText(20261023)
	seed := &PerfSeed{DataDir: dataDir}
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	const host = "perfhost"

	// Sessions: sessions.json is a JSON array of Session.
	states := []session.State{session.StateRunning, session.StateWaitingInput, session.StateComplete, session.StateComplete, session.StateFailed, session.StateKilled}
	sessions := make([]*session.Session, 0, perfSeedSessions)
	for i := 0; i < perfSeedSessions; i++ {
		id := fmt.Sprintf("%04x", i+1)
		task := fmt.Sprintf("seeded task %d: %s", i, txt.take(200))
		if i%perfSeedBigTaskEvery == 0 {
			task = txt.take(perfSeedBigTaskBytes)
			seed.BigTasks++
		}
		sess := &session.Session{
			ID: id, FullID: host + "-" + id, Name: fmt.Sprintf("perf-session-%04d", i),
			Task: task, ProjectDir: "/srv/perf/project", State: states[i%len(states)],
			CreatedAt: base.Add(time.Duration(i) * time.Minute), UpdatedAt: base.Add(time.Duration(i)*time.Minute + time.Hour),
			Hostname: host, BackendFamily: "claude-code", OutputMode: "log", InputMode: "none",
			LastResponse: txt.take(1500), LastSummaryLong: txt.take(3000),
		}
		if i%perfSeedLogEvery == 0 {
			dir := filepath.Join(dataDir, "sessions", sess.FullID)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
			sess.TrackingDir = dir
			sess.LogFile = filepath.Join(dir, "output.log")
			for name, size := range map[string]int{"output.log": perfSeedLogBytes, "transcript.jsonl": perfSeedLogBytes / 2} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(txt.take(size)), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			seed.WithLogs++
		}
		sessions = append(sessions, sess)
	}
	data, err := json.Marshal(sessions)
	if err != nil {
		t.Fatalf("marshal sessions: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dataDir, err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "sessions.json"), data, 0o644); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	seed.Sessions = len(sessions)

	// Automata: prds.jsonl, one PRD (with nested stories/tasks) per line.
	if err := os.MkdirAll(filepath.Join(dataDir, "autonomous"), 0o755); err != nil {
		t.Fatalf("mkdir autonomous: %v", err)
	}
	f, err := os.Create(filepath.Join(dataDir, "autonomous", "prds.jsonl"))
	if err != nil {
		t.Fatalf("create prds.jsonl: %v", err)
	}
	enc := json.NewEncoder(f)
	prdStatuses := []autonomous.PRDStatus{autonomous.PRDDraft, autonomous.PRDApproved, autonomous.PRDRunning, autonomous.PRDCompleted, autonomous.PRDFailed, autonomous.PRDBlocked}
	for i := 0; i < perfSeedAutomata; i++ {
		budget := perfSeedLightAutomatonB
		if i < len(perfSeedHeavyBytes) {
			budget = perfSeedHeavyBytes[i]
			seed.HeavyBytes = append(seed.HeavyBytes, budget)
		}
		prd := perfBuildAutomaton(txt, i, budget, prdStatuses[i%len(prdStatuses)], base)
		if err := enc.Encode(prd); err != nil {
			t.Fatalf("encode prd %d: %v", i, err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close prds.jsonl: %v", err)
	}
	seed.Automata = perfSeedAutomata

	mgr, err := session.NewManager(host, dataDir, "claude", time.Hour)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	amgr, err := autonomous.NewManager(dataDir, autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}

	// Automations: pair up unowned Automata, in ID order.
	made := 0
	ids := perfSortedPRDIDs(amgr)
	for i := 0; i+1 < len(ids) && made < perfSeedAutomations; i += 2 {
		_, err := amgr.CreateAutomation(autonomous.AutomationInput{
			Name:    fmt.Sprintf("perf-automation-%02d", made),
			Members: []autonomous.AutomationMember{{PRDID: ids[i]}, {PRDID: ids[i+1]}},
		}, "perf-seed")
		if err != nil {
			continue
		}
		made++
	}
	seed.Automations = made

	// Alerts: the store caps itself at 500.
	as, err := alerts.NewStore(filepath.Join(dataDir, "alerts.json"))
	if err != nil {
		t.Fatalf("alerts.NewStore: %v", err)
	}
	for i := 0; i < perfSeedAlerts; i++ {
		as.Add(alerts.Level("info"), fmt.Sprintf("perf alert %d", i), txt.take(300), "")
	}
	seed.Alerts = perfSeedAlerts

	cfg := config.DefaultConfig()
	srv := &Server{
		manager: mgr, hostname: host, cfg: cfg,
		autonomousMgr: autonomous.NewAPI(amgr),
		alertStore:    as,
		inferenceReg:  inference.NewRegistry(),
		observerAPI:   perfStubObserver{sessions: 200},
	}
	seed.Server = srv
	return seed
}

func perfSortedPRDIDs(m *autonomous.Manager) []string {
	var ids []string
	for _, p := range m.Store().ListPRDs() {
		ids = append(ids, p.ID)
	}
	// ListPRDs order is not guaranteed; sort for determinism.
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}

// perfBuildAutomaton builds one Automaton whose stories carry about
// storyBytes of task spec/error/verifier/checkpoint text.
func perfBuildAutomaton(txt *perfText, i, storyBytes int, status autonomous.PRDStatus, base time.Time) *autonomous.PRD {
	id := fmt.Sprintf("%08x", 0x10000000+i)
	created := base.Add(time.Duration(i) * time.Hour)
	nStories := 3
	nTasks := 4
	if storyBytes > 1<<20 {
		nStories, nTasks = 8, 12
	}
	perTask := storyBytes / (nStories * nTasks)
	prd := &autonomous.PRD{
		ID: id, Spec: txt.take(2000), Title: fmt.Sprintf("perf automaton %03d", i),
		ProjectDir: "/srv/perf/project", Status: status, CreatedAt: created, UpdatedAt: created.Add(time.Hour),
	}
	for s := 0; s < nStories; s++ {
		sid := fmt.Sprintf("%s-s%02x", id, s)
		story := autonomous.Story{
			ID: sid, PRDID: id, Title: fmt.Sprintf("story %d", s), Description: txt.take(400),
			Status: autonomous.StoryPending, CreatedAt: created, UpdatedAt: created,
		}
		for k := 0; k < nTasks; k++ {
			tid := fmt.Sprintf("%s-s%02x-t%02x", id, s, k)
			// Split the per-task budget across the fields a finished task fills.
			spec := perTask / 5
			task := autonomous.Task{
				ID: tid, StoryID: sid, PRDID: id, Title: fmt.Sprintf("task %d.%d", s, k),
				Spec: txt.take(spec), Status: autonomous.TaskCompleted, CreatedAt: created, UpdatedAt: created,
				Error:            txt.take(perTask / 5),
				AttemptRefs:      []string{txt.take(perTask / 5)}, // checkpoint-style payload
				StartFailureTail: []string{txt.take(perTask / 5)}, // task output tail
				Verification: &autonomous.VerificationResult{
					OK: true, Summary: "verified", VerifiedAt: created,
					RawJudge: txt.take(perTask - 4*(perTask/5)), // verifier text
				},
			}
			story.Tasks = append(story.Tasks, task)
		}
		prd.Story = append(prd.Story, story)
	}
	return prd
}

func TestPerfSeedCounts(t *testing.T) {
	seed := SeedPerfServer(t, t.TempDir())
	if seed.Sessions != perfSeedSessions {
		t.Fatalf("sessions=%d want %d", seed.Sessions, perfSeedSessions)
	}
	if got := len(seed.Server.manager.ListSessionsFiltered(session.SessionFilter{})); got != perfSeedSessions {
		t.Fatalf("manager lists %d sessions, want %d", got, perfSeedSessions)
	}
	if want := (perfSeedSessions + perfSeedBigTaskEvery - 1) / perfSeedBigTaskEvery; seed.BigTasks != want {
		t.Fatalf("big tasks=%d want %d", seed.BigTasks, want)
	}
	if seed.WithLogs == 0 {
		t.Fatal("no sessions with logs seeded")
	}
	if got := len(seed.Server.autonomousMgr.ListPRDs()); got != perfSeedAutomata {
		t.Fatalf("automata=%d want %d", got, perfSeedAutomata)
	}
	if seed.Automations == 0 {
		t.Fatal("no automations seeded")
	}
}

func TestPerfSeedHeavyAutomataSize(t *testing.T) {
	seed := SeedPerfServer(t, t.TempDir())
	rr := perfGet(seed.Server.handleAutonomousPRDs, "/api/autonomous/prds?full=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("list prds: %d", rr.Code)
	}
	var resp struct {
		PRDs []struct {
			ID    string            `json:"id"`
			Story []json.RawMessage `json:"stories"`
		} `json:"prds"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	list := resp.PRDs
	if len(list) != perfSeedAutomata {
		t.Fatalf("listed %d automata, want %d", len(list), perfSeedAutomata)
	}
	heavy := 0
	for _, p := range list {
		n := 0
		for _, s := range p.Story {
			n += len(s)
		}
		if n >= 6<<20 {
			heavy++
		}
	}
	if heavy != len(perfSeedHeavyBytes) {
		t.Fatalf("heavy automata=%d want %d (the list endpoint should expose their story data)", heavy, len(perfSeedHeavyBytes))
	}
}

// perfEndpoint is one request the web UI makes on load.
type perfEndpoint struct {
	Name    string
	Path    string
	Handler func(http.ResponseWriter, *http.Request)
}

func perfGet(h func(http.ResponseWriter, *http.Request), path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr
}

// PerfRow is one measured endpoint.
type PerfRow struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Status      int     `json:"status"`
	RawBytes    int     `json:"raw_bytes"`
	GzipBytes   int     `json:"gzip_bytes"`
	ServerMs    float64 `json:"server_ms"`
	GzipRatioPc float64 `json:"gzip_ratio_pct"`
}

// MeasureLoadEndpoints calls every load-time endpoint once and returns the
// rows. Reusable by later tests that compare before/after a change.
func MeasureLoadEndpoints(t testing.TB, s *Server) []PerfRow {
	t.Helper()
	eps := []perfEndpoint{
		{"sessions", "/api/sessions", s.handleSessions},
		{"autonomous prds", "/api/autonomous/prds", s.handleAutonomousPRDs},
		{"automations", "/api/automations", s.handleAutomations},
		{"capacity", "/api/capacity", s.handleCapacity},
		{"alerts", "/api/alerts", s.handleAlerts},
		{"observer stats", "/api/observer/stats", s.handleObserverStats},
		{"observer envelopes", "/api/observer/envelopes", s.handleObserverEnvelopes},
		{"config", "/api/config", s.handleConfig},
		{"llms", "/api/llms", s.handleLLMs},
	}
	var rows []PerfRow
	for _, ep := range eps {
		start := time.Now()
		rr := perfGet(ep.Handler, ep.Path)
		elapsed := time.Since(start)
		body := rr.Body.Bytes()
		gzLen := perfGzipLen(body)
		row := PerfRow{Name: ep.Name, Path: ep.Path, Status: rr.Code, RawBytes: len(body), GzipBytes: gzLen, ServerMs: float64(elapsed.Microseconds()) / 1000}
		if len(body) > 0 {
			row.GzipRatioPc = 100 * float64(gzLen) / float64(len(body))
		}
		rows = append(rows, row)
	}
	return rows
}

// perfGzipLen is the size of body gzipped at Go's default level.
func perfGzipLen(body []byte) int {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(body)
	_ = zw.Close()
	return gz.Len()
}

func perfLogTable(t testing.TB, rows []PerfRow) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "\n%-20s %-28s %6s %12s %12s %7s %10s\n", "endpoint", "path", "status", "raw_bytes", "gzip_bytes", "gz%", "server_ms")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-20s %-28s %6d %12d %12d %6.1f%% %10.1f\n", r.Name, r.Path, r.Status, r.RawBytes, r.GzipBytes, r.GzipRatioPc, r.ServerMs)
	}
	t.Log(b.String())
}

func perfWriteReport(t testing.TB, seed *PerfSeed, rows []PerfRow) {
	t.Helper()
	out := os.Getenv("PERF_REPORT_OUT")
	if out == "" {
		return
	}
	report := map[string]any{
		"seed": map[string]any{
			"sessions": seed.Sessions, "big_tasks": seed.BigTasks, "with_logs": seed.WithLogs,
			"automata": seed.Automata, "heavy_bytes": seed.HeavyBytes, "automations": seed.Automations, "alerts": seed.Alerts,
		},
		"endpoints": rows,
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatalf("write PERF_REPORT_OUT %s: %v", out, err)
	}
}

func TestEndpointMeasureLoadPath(t *testing.T) {
	seed := SeedPerfServer(t, t.TempDir())
	rows := MeasureLoadEndpoints(t, seed.Server)
	perfLogTable(t, rows)
	perfWriteReport(t, seed, rows)
	for _, r := range rows {
		if r.Status != http.StatusOK {
			t.Errorf("%s %s: status %d, want 200", r.Name, r.Path, r.Status)
		}
		if r.RawBytes == 0 {
			t.Errorf("%s: empty body", r.Name)
		}
		if r.RawBytes > 1024 && r.GzipBytes > r.RawBytes {
			t.Errorf("%s: gzip %d larger than raw %d", r.Name, r.GzipBytes, r.RawBytes)
		}
	}
}

func TestEndpointMeasureDeterministic(t *testing.T) {
	a := SeedPerfServer(t, t.TempDir())
	b := SeedPerfServer(t, t.TempDir())
	ra := perfGet(a.Server.handleAutonomousPRDs, "/api/autonomous/prds")
	rb := perfGet(b.Server.handleAutonomousPRDs, "/api/autonomous/prds")
	// A few bytes of slack: load-time stamps (time.Now) vary in encoded width.
	if d := ra.Body.Len() - rb.Body.Len(); d > 64 || d < -64 {
		t.Fatalf("automata list sizes differ between seeds: %d vs %d", ra.Body.Len(), rb.Body.Len())
	}
}
