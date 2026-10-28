package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/pilog"
	"github.com/dmz006/datawatch/internal/session"
)

// progressSampleLog is a synthetic pi session transcript with one user prompt
// and two assistant messages (carrying thinking/text/toolCall blocks and
// usage). The last line is intentionally truncated mid-stream: a live pi log
// ends exactly like this, and the endpoint must read it without error.
const progressSampleLog = `{"type":"message","timestamp":"2026-02-11T10:00:20Z","message":{"role":"user","content":"do the thing"}}
{"type":"message","timestamp":"2026-02-11T10:05:10Z","message":{"role":"assistant","usage":{"input":100,"output":25,"totalTokens":125},"content":[{"type":"thinking","thinking":"plan it first"},{"type":"text","text":"Writing hello.txt"},{"type":"toolCall","name":"write","arguments":{"path":"hello.txt","content":"hi"}}]}}
{"type":"message","timestamp":"2026-02-11T10:06:41Z","message":{"role":"assistant","usage":{"input":90,"output":10,"totalTokens":100},"content":[{"type":"toolCall","name":"bash","arguments":{"command":"ls -la"}}]}}
{"type":"message","timestamp":"2026-02-11T1`

const (
	progressLogStarted = "2026-02-11T09:59:30Z" // session create time (before log start)
	progressLogAt      = "2026-02-11T10:06:41Z" // expected last_event_at
	progressWarning    = "suspected looping: same call x5"
)

// newProgressTestServer builds a server whose autonomous manager holds one
// PRD with a running task (pt1) and a pending task (pt2).
func newProgressTestServer(t *testing.T) (*Server, *autonomous.Manager, string) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{{
		ID: "sp1", Title: "Story",
		Tasks: []autonomous.Task{
			{ID: "pt1", Title: "running", Status: autonomous.TaskInProgress},
			{ID: "pt2", Title: "waiting", Status: autonomous.TaskPending},
		},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	return &Server{autonomousMgr: autonomous.NewAPI(mgr)}, mgr, prd.ID
}

// seedPT1FromSampleLog feeds pt1 the exact snapshot a real sample log
// produces — the path the executor's periodic poll takes when it refreshes.
func seedPT1FromSampleLog(t *testing.T, mgr *autonomous.Manager) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "2026-02-11T10-00-00-000Z_progtest.jsonl")
	if err := os.WriteFile(path, []byte(progressSampleLog), 0o644); err != nil {
		t.Fatalf("write sample log: %v", err)
	}
	prog, err := pilog.Read(path)
	if err != nil {
		t.Fatalf("pilog.Read: %v", err)
	}
	snap := autonomous.FromPilog(prog)
	if !mgr.RefreshTaskProgress("pt1", snap, progressWarning) {
		t.Fatalf("RefreshTaskProgress(pt1) = false, want true")
	}
}

// TestPRDGetFullIncludesProgressOnlyForRunningTask: the prd-get "full" detail
// carries the live `progress` snapshot (built from the sample log) plus
// looping_warning on the in-progress task, and omits both keys entirely on
// the pending task.
func TestPRDGetFullIncludesProgressOnlyForRunningTask(t *testing.T) {
	s, mgr, id := newProgressTestServer(t)
	seedPT1FromSampleLog(t, mgr)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id+"?detail=full", nil)
	s.handleAutonomousPRDs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var full struct {
		Stories []struct {
			Tasks []map[string]any `json:"tasks"`
		} `json:"stories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var t1, t2 map[string]any
	for _, st := range full.Stories {
		for _, task := range st.Tasks {
			switch task["id"] {
			case "pt1":
				t1 = task
			case "pt2":
				t2 = task
			}
		}
	}
	if t1 == nil || t2 == nil {
		t.Fatalf("tasks pt1/pt2 not found in full PRD: %s", rec.Body.String())
	}
	p, ok := t1["progress"].(map[string]any)
	if !ok {
		t.Fatalf("pt1.running must carry a progress object: %v", t1)
	}
	if p["last_event_at"] != progressLogAt {
		t.Errorf(`pt1.progress.last_event_at = %v, want %s`, p["last_event_at"], progressLogAt)
	}
	if p["assistant_turns"] != float64(2) {
		t.Errorf(`pt1.progress.assistant_turns = %v, want 2`, p["assistant_turns"])
	}
	tcs, ok := p["tool_calls"].(map[string]any)
	if !ok {
		t.Fatalf(`pt1.progress.tool_calls missing: %v`, p)
	}
	if tcs["bash"] != float64(1) || tcs["write"] != float64(1) {
		t.Errorf(`pt1.progress.tool_calls = %v, want {"bash":1,"write":1}`, tcs)
	}
	if p["last_tool_name"] != "bash" {
		t.Errorf(`pt1.progress.last_tool_name = %v, want "bash"`, p["last_tool_name"])
	}
	if p["last_summary"] != "Writing hello.txt" {
		t.Errorf(`pt1.progress.last_summary = %v, want "Writing hello.txt"`, p["last_summary"])
	}
	if p["tokens"] != float64(225) {
		t.Errorf(`pt1.progress.tokens = %v, want 225`, p["tokens"])
	}
	if t1["looping_warning"] != progressWarning {
		t.Errorf(`pt1.looping_warning = %v, want %q`, t1["looping_warning"], progressWarning)
	}

	if _, present := t2["progress"]; present {
		t.Errorf("pt2 (pending) must not carry progress: %v", t2)
	}
	if _, present := t2["looping_warning"]; present {
		t.Errorf("pt2 (pending) must not carry looping_warning: %v", t2)
	}
}

// TestPRDGetTasksDetailCarriesProgress: the lightweight "tasks" detail (what
// MCP autonomous_prd_get and compact pollers use) lists the running task's
// progress, and none on the pending one.
func TestPRDGetTasksDetailCarriesProgress(t *testing.T) {
	s, mgr, id := newProgressTestServer(t)
	seedPT1FromSampleLog(t, mgr)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id+"?detail=tasks", nil)
	s.handleAutonomousPRDs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var view struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, task := range view.Tasks {
		switch task["id"] {
		case "pt1":
			p, ok := task["progress"].(map[string]any)
			if !ok {
				t.Fatalf("tasks detail: pt1 must carry progress: %v", task)
			}
			if p["last_event_at"] != progressLogAt || p["assistant_turns"] != float64(2) {
				t.Errorf("tasks detail pt1.progress = %v, want last_event_at %s + 2 turns", p, progressLogAt)
			}
			if task["looping_warning"] != progressWarning {
				t.Errorf("tasks detail pt1.looping_warning = %v, want %q", task["looping_warning"], progressWarning)
			}
		case "pt2":
			if _, present := task["progress"]; present {
				t.Errorf("tasks detail: pt2 (pending) must not carry progress: %v", task)
			}
		default:
			t.Errorf("unexpected task %v in tasks detail", task["id"])
		}
	}
}

// plantSampleSession plants a pi session in s.manager's store with an
// on-disk log LocateLog can find for (ProjectDir, CreatedAt), and returns the
// session ID.
func plantSampleSession(t *testing.T, s *Server, workDir string, withLog bool) string {
	t.Helper()
	started, err := time.Parse(time.RFC3339, progressLogStarted)
	if err != nil {
		t.Fatalf("parse started: %v", err)
	}
	if withLog {
		dir := filepath.Join(piSessionsRootServer(), pilog.ProjectDirName(workDir))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "2026-02-11T10-00-00-000Z_progtest.jsonl"),
			[]byte(progressSampleLog), 0o644); err != nil {
			t.Fatalf("write log: %v", err)
		}
	}
	sess := &session.Session{
		ID:            "sessp",
		FullID:        "testhost-sessp",
		ProjectDir:    workDir,
		CreatedAt:     started,
		UpdatedAt:     started,
		Hostname:      "testhost",
		State:         session.StateRunning,
		BackendFamily: "pi-prompt",
	}
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return sess.ID
}

// TestSessionProgressEndpoint: GET /api/sessions/{id}/progress parses the
// session's own log and returns the same counts the per-task snapshot does;
// 404 for an unknown session or a session with no log; 405 for non-GET.
func TestSessionProgressEndpoint(t *testing.T) {
	piRoot := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", piRoot)

	s := newTestServer(t, nil, nil)
	workDir := t.TempDir()
	id := plantSampleSession(t, s, workDir, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+id+"/progress", nil)
	s.handleSessionProgress(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var body struct {
		SessionID      string         `json:"session_id"`
		FullID         string         `json:"full_id"`
		State          string         `json:"state"`
		LogPath        string         `json:"log_path"`
		LastEventAt    string         `json:"last_event_at"`
		AssistantTurns int            `json:"assistant_turns"`
		ToolCalls      map[string]int `json:"tool_calls"`
		LastToolName   string         `json:"last_tool_name"`
		LastSummary    string         `json:"last_summary"`
		Tokens         int            `json:"tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if body.SessionID != "sessp" || body.FullID != "testhost-sessp" {
		t.Errorf("ids = %q/%q, want sessp/testhost-sessp", body.SessionID, body.FullID)
	}
	if body.State != "running" {
		t.Errorf("state = %q, want running", body.State)
	}
	if body.LastEventAt != progressLogAt {
		t.Errorf("last_event_at = %q, want %s", body.LastEventAt, progressLogAt)
	}
	if body.AssistantTurns != 2 {
		t.Errorf("assistant_turns = %d, want 2", body.AssistantTurns)
	}
	if body.ToolCalls["bash"] != 1 || body.ToolCalls["write"] != 1 {
		t.Errorf("tool_calls = %v, want bash/write each 1", body.ToolCalls)
	}
	if body.LastToolName != "bash" || body.LastSummary != "Writing hello.txt" {
		t.Errorf("last = %q/%q, want bash/%q", body.LastToolName, body.LastSummary, "Writing hello.txt")
	}
	if body.Tokens != 225 {
		t.Errorf("tokens = %d, want 225", body.Tokens)
	}
	if body.LogPath == "" || !strings.HasSuffix(body.LogPath, ".jsonl") {
		t.Errorf("log_path = %q, want a .jsonl path", body.LogPath)
	}

	// A stored session whose log does not (yet) exist → 404, not 500.
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	noneID := plantSampleSession2(t, s, t.TempDir())
	rec404 := httptest.NewRecorder()
	req404 := httptest.NewRequest(http.MethodGet, "/api/sessions/"+noneID+"/progress", nil)
	s.handleSessionProgress(rec404, req404)
	if rec404.Code != http.StatusNotFound {
		t.Fatalf("session without log: status = %d, want 404", rec404.Code)
	}

	// Unknown session → 404.
	recNF := httptest.NewRecorder()
	reqNF := httptest.NewRequest(http.MethodGet, "/api/sessions/doesnotexist/progress", nil)
	s.handleSessionProgress(recNF, reqNF)
	if recNF.Code != http.StatusNotFound {
		t.Fatalf("unknown session: status = %d, want 404", recNF.Code)
	}

	// Non-GET → 405.
	rec405 := httptest.NewRecorder()
	req405 := httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/progress", nil)
	s.handleSessionProgress(rec405, req405)
	if rec405.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status = %d, want 405", rec405.Code)
	}
}

// plantSampleSession2 saves a second session (SaveSession upserts by ID, so
// this gets its own ID).
func plantSampleSession2(t *testing.T, s *Server, workDir string) string {
	t.Helper()
	started, err := time.Parse(time.RFC3339, progressLogStarted)
	if err != nil {
		t.Fatalf("parse started: %v", err)
	}
	sess := &session.Session{
		ID:            "sessn",
		FullID:        "testhost-sessn",
		ProjectDir:    workDir,
		CreatedAt:     started,
		UpdatedAt:     started,
		Hostname:      "testhost",
		State:         session.StateRunning,
		BackendFamily: "pi-prompt",
	}
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return sess.ID
}
