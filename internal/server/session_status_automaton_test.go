package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// automatonStatusAPI is an AutonomousAPI fake serving one PRD and,
// optionally, a session→task mapping for FindTaskBySessionID.
type automatonStatusAPI struct {
	fakeOrchAutonomous
	prd       any
	bySession map[string][2]string
}

func (a *automatonStatusAPI) GetPRD(id string) (any, bool) {
	if a.prd == nil {
		return nil, false
	}
	return a.prd, true
}

func (a *automatonStatusAPI) FindTaskBySessionID(sid string) (string, string) {
	v := a.bySession[sid]
	return v[0], v[1]
}

func automatonStatusPRD(spec string) *autonomous.PRD {
	started := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	decisions := make([]autonomous.Decision, 0, 12)
	for i := 0; i < 12; i++ {
		decisions = append(decisions, autonomous.Decision{
			At: started.Add(time.Duration(i) * time.Minute), Kind: "run",
			Actor: "autonomous", Note: "n" + string(rune('a'+i)),
		})
	}
	return &autonomous.PRD{
		ID: "prd1", Title: "Status block", Status: autonomous.PRDStatus("running"),
		Decisions: decisions,
		Story: []autonomous.Story{{
			ID: "s1", Title: "Story one", FilesPlanned: []string{"story.go"},
			Tasks: []autonomous.Task{{
				ID: "t1", StoryID: "s1", Title: "Task one", Spec: spec,
				Status: autonomous.TaskStatus("in_progress"), SessionID: "h-aa",
				WaitReason: "capacity", StartedAt: &started, RetryCount: 2, Error: "boom",
				Verification: &autonomous.VerificationResult{
					OK: false, Severity: "high", Summary: "missing test",
					Issues: []string{"no test"}, VerifierBackend: "claude-code",
					VerifierModel: "opus", VerifierSource: "task",
				},
				PreTaskSHA:       "abc123",
				FilesPlanned:     []string{"a.go"},
				FilesTouched:     []string{"a.go", "b.go"},
				ForeignFileDrift: []string{"x.go"},
			}, {
				ID: "t2", StoryID: "s1", Title: "Task two", Status: autonomous.TaskStatus("pending"),
			}},
		}},
	}
}

func automatonStatusServer(t *testing.T, api AutonomousAPI, sess *session.Session) *Server {
	t.Helper()
	sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	allowTestTemp(sm)
	// In-memory tmux fake: these tests never need a live multiplexer.
	sm.WithFakeTmux()
	t.Cleanup(sm.WaitBackground)
	if sess != nil {
		if err := sm.SaveSession(sess); err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
	}
	return &Server{manager: sm, autonomousMgr: api}
}

func getSessionStatus(t *testing.T, s *Server, sid string) map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleSessionStatus(rr, httptest.NewRequest(http.MethodGet, "/api/sessions/"+sid+"/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestSessionStatusAutomaton_PopulatedFromTask(t *testing.T) {
	api := &automatonStatusAPI{prd: automatonStatusPRD("do the thing")}
	s := automatonStatusServer(t, api, &session.Session{
		ID: "aa", FullID: "h-aa", PRDID: "prd1", TaskID: "t1",
		TokensIn: 100, TokensOut: 50, EstCostUSD: 0.25,
	})
	body := getSessionStatus(t, s, "h-aa")
	a, ok := body["automaton"].(map[string]any)
	if !ok {
		t.Fatalf("automaton block missing: %v", body)
	}
	want := map[string]any{
		"prd_id": "prd1", "prd_title": "Status block", "prd_status": "running",
		"story_id": "s1", "story_title": "Story one", "task_id": "t1",
		"task_title": "Task one", "task_spec": "do the thing", "task_status": "in_progress",
		"wait_reason": "capacity", "retry_count": float64(2), "error": "boom",
	}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("%s=%v want %v", k, a[k], v)
		}
	}
	if _, ok := a["started_at"]; !ok {
		t.Error("started_at missing")
	}
	if _, ok := a["task_spec_truncated"]; ok {
		t.Error("task_spec_truncated set for short spec")
	}
	v := a["verification"].(map[string]any)
	if v["ok"] != false || v["severity"] != "high" || v["summary"] != "missing test" ||
		v["verifier_backend"] != "claude-code" || v["verifier_model"] != "opus" || v["verifier_source"] != "task" {
		t.Errorf("verification=%v", v)
	}
	g := a["git"].(map[string]any)
	if g["pre_task_sha"] != "abc123" || len(g["files_planned"].([]any)) != 1 ||
		len(g["files_touched"].([]any)) != 2 || len(g["foreign_file_drift"].([]any)) != 1 {
		t.Errorf("git=%v", g)
	}
	decs := a["decisions"].([]any)
	if len(decs) != 10 {
		t.Fatalf("decisions len=%d want 10", len(decs))
	}
	if last := decs[9].(map[string]any); last["note"] != "nl" || last["kind"] != "run" || last["actor"] != "autonomous" {
		t.Errorf("last decision=%v", last)
	}
	c := a["cost"].(map[string]any)
	if c["tokens_in"] != float64(100) || c["tokens_out"] != float64(50) || c["est_cost_usd"] != 0.25 {
		t.Errorf("cost=%v", c)
	}
	sp := body["sprint"].(map[string]any)
	for k, v := range map[string]any{
		"sprint_id": "prd1", "title": "Status block", "status": "running", "automata": "Status block",
		"task_id": "t1", "task_title": "Task one", "story_title": "Story one",
	} {
		if sp[k] != v {
			t.Errorf("sprint.%s=%v want %v", k, sp[k], v)
		}
	}
}

func TestSessionStatusAutomaton_FindTaskBySessionIDFallback(t *testing.T) {
	prd := automatonStatusPRD("spec")
	prd.Story[0].Tasks[1].SessionID = "h-bb"
	api := &automatonStatusAPI{prd: prd, bySession: map[string][2]string{"h-bb": {"prd1", "t2"}}}
	s := automatonStatusServer(t, api, &session.Session{ID: "bb", FullID: "h-bb"})
	a, ok := getSessionStatus(t, s, "bb")["automaton"].(map[string]any)
	if !ok {
		t.Fatal("automaton block missing")
	}
	if a["prd_id"] != "prd1" || a["task_id"] != "t2" || a["task_title"] != "Task two" {
		t.Errorf("automaton=%v", a)
	}
	// Task has no files of its own: story's planned files are used.
	g := a["git"].(map[string]any)
	if fp := g["files_planned"].([]any); len(fp) != 1 || fp[0] != "story.go" {
		t.Errorf("files_planned=%v", fp)
	}
}

func TestSessionStatusAutomaton_OmittedCases(t *testing.T) {
	cases := []struct {
		name string
		api  *automatonStatusAPI
		sess *session.Session
	}{
		{"non-autonomous session", &automatonStatusAPI{prd: automatonStatusPRD("x")},
			&session.Session{ID: "cc", FullID: "h-cc"}},
		{"PRD missing", &automatonStatusAPI{},
			&session.Session{ID: "cc", FullID: "h-cc", PRDID: "gone", TaskID: "t1"}},
		{"unknown session", &automatonStatusAPI{prd: automatonStatusPRD("x")}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := automatonStatusServer(t, tc.api, tc.sess)
			body := getSessionStatus(t, s, "h-cc")
			if _, ok := body["automaton"]; ok {
				t.Errorf("automaton key present: %v", body["automaton"])
			}
			if _, ok := body["sprint"]; ok {
				t.Errorf("sprint key present: %v", body["sprint"])
			}
		})
	}
}

// automatonReadableAPI is an AutonomousAPI fake that also serves task
// wait-visibility, and resolves wait reasons to readable sentences.
type automatonReadableAPI struct {
	fakeOrchAutonomous
	prd      any
	waiting  []autonomous.TaskWaitView
	readable string
}

func (a *automatonReadableAPI) GetPRD(id string) (any, bool) {
	if a.prd == nil {
		return nil, false
	}
	return a.prd, true
}

func (a *automatonReadableAPI) TaskVisibility() ([]autonomous.TaskWaitView, []autonomous.TaskHoldView) {
	return a.waiting, nil
}

func (a *automatonReadableAPI) ReadableWaitSentence(reason string) string { return a.readable }

// The raw wait_reason stays untouched; wait_reason_readable sits beside it,
// resolved through the manager when the manager can resolve holder names.
func TestSessionStatusAutomaton_WaitReasonReadableBesideRaw(t *testing.T) {
	raw := autonomous.NodeBusyWaitText("n1", 1, 1, []string{"t3"})
	want := "waiting for node n1: 1/1 engines in use, held by Fix crash / T3 (c3e70011)"
	api := &automatonReadableAPI{
		prd:      automatonStatusPRD("do the thing"),
		waiting:  []autonomous.TaskWaitView{{TaskID: "t2", PRDID: "prd1", Reason: raw, Cause: autonomous.WaitCauseNodeBusy}},
		readable: want,
	}
	s := automatonStatusServer(t, api, &session.Session{ID: "dd", FullID: "h-dd", PRDID: "prd1", TaskID: "t2"})
	a := getSessionStatus(t, s, "h-dd")["automaton"].(map[string]any)
	if a["wait_reason"] != raw {
		t.Errorf("wait_reason=%v, want %q", a["wait_reason"], raw)
	}
	if a["wait_reason_readable"] != want {
		t.Errorf("wait_reason_readable=%v, want %q", a["wait_reason_readable"], want)
	}
	if a["wait_cause"] != "node_busy" {
		t.Errorf("wait_cause=%v, want node_busy", a["wait_cause"])
	}
}

// A manager without the ReadableWaitSentence capability still gets a
// readable field — raw holder ids in place of names (the fallback path).
func TestReadableWaitSentence_FallbackWithoutResolver(t *testing.T) {
	reason := "pool llm:pi-prompt full (2/2, held by task-a,task-b)"
	manager := &automatonStatusAPI{}
	if got, want := readableWaitSentence(manager, reason),
		"pool llm:pi-prompt full (2/2), held by task-a and task-b"; got != want {
		t.Errorf("readable = %q, want %q", got, want)
	}
	if got := readableWaitSentence(manager, ""); got != "" {
		t.Errorf("empty reason readable=%q, want \"\"", got)
	}
	// With a resolver wired, its sentence wins.
	api := &automatonReadableAPI{readable: "pool llm:pi-prompt full (2/2), held by A and B"}
	if got := readableWaitSentence(api, reason); got != api.readable {
		t.Errorf("readable=%q, want the resolved sentence", got)
	}
}

func TestSessionStatusAutomaton_SpecTruncation(t *testing.T) {
	cases := []struct {
		name      string
		spec      string
		truncated bool
	}{
		{"at limit", strings.Repeat("é", automatonTaskSpecMaxRunes), false},
		{"over limit", strings.Repeat("é", automatonTaskSpecMaxRunes+50), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &automatonStatusAPI{prd: automatonStatusPRD(tc.spec)}
			s := automatonStatusServer(t, api, &session.Session{ID: "aa", FullID: "h-aa", PRDID: "prd1", TaskID: "t1"})
			a := getSessionStatus(t, s, "h-aa")["automaton"].(map[string]any)
			got, _ := a["task_spec"].(string)
			trunc, _ := a["task_spec_truncated"].(bool)
			if trunc != tc.truncated {
				t.Errorf("task_spec_truncated=%v want %v", trunc, tc.truncated)
			}
			if n := len([]rune(strings.TrimSuffix(got, "…"))); n > automatonTaskSpecMaxRunes {
				t.Errorf("task_spec runes=%d > %d", n, automatonTaskSpecMaxRunes)
			}
		})
	}
}
