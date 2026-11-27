package server

// Work finished by hand reads done. A record with a shipped record, or one
// closed with outcome delivered, must read done in the progress numbers, the
// timeline and the list's completed filter. A held or open record never does.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type workDoneRecord struct {
	id        string
	status    autonomous.PRDStatus
	outcome   string
	commit    string
	tasks     []autonomous.TaskStatus
	forced    bool // the operator forced it to run again past its shipped record
	wantState string
	wantDone  bool
	// wantCount is the progress bucket: done, waiting, running, held, or ""
	// when progress leaves the record out.
	wantCount string
}

func workDoneRecords() []workDoneRecord {
	pending := []autonomous.TaskStatus{autonomous.TaskPending, autonomous.TaskPending}
	return []workDoneRecord{
		{id: "d0000001", status: autonomous.PRDDraft, wantState: "draft", wantCount: "waiting"},
		{id: "d0000002", status: autonomous.PRDDraft, commit: "c0ffee01", tasks: pending, wantState: "completed", wantDone: true, wantCount: "done"},
		{id: "d0000003", status: autonomous.PRDCancelled, outcome: autonomous.OutcomeDelivered, tasks: pending, wantState: "completed", wantDone: true, wantCount: "done"},
		{id: "d0000004", status: autonomous.PRDArchived, outcome: autonomous.OutcomeDelivered, wantState: "completed", wantDone: true, wantCount: "done"},
		{id: "d0000005", status: autonomous.PRDCancelled, outcome: autonomous.OutcomeDelivered, commit: "c0ffee02", wantState: "completed", wantDone: true, wantCount: "done"},
		{id: "d0000006", status: autonomous.PRDCancelled, outcome: autonomous.OutcomeDropped, tasks: pending, wantState: "cancelled"},
		{id: "d0000007", status: autonomous.PRDCancelled, outcome: autonomous.OutcomeSuperseded, tasks: pending, wantState: "cancelled"},
		{id: "d0000008", status: autonomous.PRDHeld, wantState: "held", wantCount: "held"},
		{id: "d0000009", status: autonomous.PRDHeld, commit: "c0ffee03", wantState: "held", wantCount: "held"},
		{id: "d000000a", status: autonomous.PRDDraft, outcome: autonomous.OutcomeDelivered, wantState: "draft", wantCount: "waiting"},
		{id: "d000000b", status: autonomous.PRDCancelled, outcome: autonomous.OutcomeHeld, tasks: []autonomous.TaskStatus{autonomous.TaskCompleted}, wantState: "cancelled"},
		{id: "d000000c", status: autonomous.PRDRunning, commit: "c0ffee04", forced: true, tasks: pending, wantState: "running", wantCount: "running"},
	}
}

func workDoneServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	m, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range workDoneRecords() {
		tag, err := m.CreateTag("own-"+r.id, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		p := &autonomous.PRD{ID: r.id, Title: "Add " + r.id, Status: r.status,
			ProjectDir: "/tmp/demo", ClosedOutcome: r.outcome, MergeCommit: r.commit, TagIDs: []string{tag.ID}}
		if r.commit != "" {
			p.MergedInRelease = "v0.0.1"
		}
		if r.forced {
			p.ClosedWorkOverride = &autonomous.ClosedWorkOverride{Commit: r.commit}
		}
		if len(r.tasks) > 0 {
			s := autonomous.Story{ID: r.id + "-s", PRDID: r.id, Title: "work"}
			for i, st := range r.tasks {
				s.Tasks = append(s.Tasks, autonomous.Task{ID: r.id + "-t" + string(rune('0'+i)), PRDID: r.id, Status: st})
			}
			p.Story = []autonomous.Story{s}
		}
		if err := m.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{autonomousMgr: autonomous.NewAPI(m), cfg: cfg}
}

func workDoneGet(t *testing.T, h http.HandlerFunc, path string, out any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
}

func workDoneTagOf(t *testing.T, s *Server, id string) string {
	t.Helper()
	raw, ok := s.autonomousMgr.GetPRD(id)
	if !ok {
		t.Fatalf("no record %s", id)
	}
	return raw.(*autonomous.PRD).TagIDs[0]
}

func workDoneProgress(t *testing.T, s *Server, id string) autonomous.ProgressGroup {
	t.Helper()
	var m autonomous.ProgressMetrics
	workDoneGet(t, s.handleAutonomousProgress, "/api/autonomous/progress?since=2000-01-01&tag="+workDoneTagOf(t, s, id), &m)
	return m.Totals
}

func workDoneTimeline(t *testing.T, s *Server) map[string]autonomous.TimelineAutomaton {
	t.Helper()
	var res autonomous.TimelineResult
	workDoneGet(t, s.handleAutonomousTimeline, "/api/autonomous/timeline", &res)
	out := map[string]autonomous.TimelineAutomaton{}
	for _, p := range res.Projects {
		for _, a := range p.Automata {
			out[a.ID] = a
		}
	}
	return out
}

func workDoneRows(t *testing.T, s *Server, query string) map[string]map[string]any {
	t.Helper()
	var body struct {
		PRDs []map[string]any `json:"prds"`
	}
	workDoneGet(t, s.handleAutonomousPRDs, "/api/autonomous/prds?limit=100"+query, &body)
	out := map[string]map[string]any{}
	for _, row := range body.PRDs {
		out[row["id"].(string)] = row
	}
	return out
}

func TestWorkFinishedByHandReadsDoneInProgress(t *testing.T) {
	s := workDoneServer(t, config.DefaultConfig())
	for _, r := range workDoneRecords() {
		g := workDoneProgress(t, s, r.id)
		got := map[string]int{"done": g.Done, "waiting": g.Waiting, "running": g.Running, "held": g.Held}
		for name, n := range got {
			want := 0
			if name == r.wantCount {
				want = 1
			}
			if n != want {
				t.Errorf("%s (%s outcome=%q commit=%q): progress %s = %d, want %d", r.id, r.status, r.outcome, r.commit, name, n, want)
			}
		}
		// Work finished by hand leaves no tasks left to do.
		if r.wantDone && g.TasksLeft != 0 {
			t.Errorf("%s: tasks left = %d on finished work, want 0", r.id, g.TasksLeft)
		}
		if r.wantDone && g.TasksDone != 0 {
			t.Errorf("%s: tasks done = %d, want 0: no worker completed one", r.id, g.TasksDone)
		}
	}
}

func TestWorkFinishedByHandReadsDoneOnTimeline(t *testing.T) {
	s := workDoneServer(t, config.DefaultConfig())
	bars := workDoneTimeline(t, s)
	for _, r := range workDoneRecords() {
		a, ok := bars[r.id]
		if !ok {
			t.Errorf("%s: not on the timeline", r.id)
			continue
		}
		if a.Status != r.wantState {
			t.Errorf("%s: timeline status = %q, want %q", r.id, a.Status, r.wantState)
		}
		if r.wantDone && (a.ProjectionReason != "finished" || a.ProjectedFinish != nil) {
			t.Errorf("%s: projection = %v %q, want finished with no projected date", r.id, a.ProjectedFinish, a.ProjectionReason)
		}
	}
}

func TestWorkFinishedByHandReadsDoneInListAndCounts(t *testing.T) {
	s := workDoneServer(t, config.DefaultConfig())
	all := workDoneRows(t, s, "")
	completed := workDoneRows(t, s, "&status=completed")
	done := 0
	for _, r := range workDoneRecords() {
		if r.wantDone {
			done++
		}
		if got := all[r.id]["effective_status"]; got != r.wantState {
			t.Errorf("%s: effective_status = %v, want %q", r.id, got, r.wantState)
		}
		if got := all[r.id]["status"]; got != string(r.status) {
			t.Errorf("%s: stored status = %v, want %q unchanged", r.id, got, r.status)
		}
		if _, in := completed[r.id]; in != r.wantDone {
			t.Errorf("%s: in the completed filter = %v, want %v", r.id, in, r.wantDone)
		}
	}
	var counts shippedCountsBody
	workDoneGet(t, s.handleAutonomousShippedCounts, "/api/autonomous/shipped/counts", &counts)
	if counts.PRDs["completed"] != done {
		t.Errorf("completed count = %d, want %d (%v)", counts.PRDs["completed"], done, counts.PRDs)
	}
}

// With autonomous.shipped.enabled off, a shipped record changes no view; a
// recorded delivered outcome still does.
func TestWorkFinishedByHandShippedSurfaceOff(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Autonomous.Shipped.Enabled = shipBool(false)
	s := workDoneServer(t, cfg)
	bars := workDoneTimeline(t, s)
	completed := workDoneRows(t, s, "&status=completed")
	for id, want := range map[string]bool{"d0000002": false, "d0000003": true} {
		if got := bars[id].Status == "completed"; got != want {
			t.Errorf("%s: timeline done = %v, want %v", id, got, want)
		}
		if _, got := completed[id]; got != want {
			t.Errorf("%s: completed filter = %v, want %v", id, got, want)
		}
		if got := workDoneProgress(t, s, id).Done == 1; got != want {
			t.Errorf("%s: progress done = %v, want %v", id, got, want)
		}
	}
}
