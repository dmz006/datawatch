// Tests for the project roll-up read (GET /api/projects/{name}/rollup).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// rollupNow is the fixed clock the pure-function tests read against.
var rollupNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func rollupAutomationWith(id string, prdIDs ...string) *autonomous.Automation {
	a := &autonomous.Automation{ID: id}
	for _, p := range prdIDs {
		a.Members = append(a.Members, autonomous.AutomationMember{PRDID: p})
	}
	return a
}

func rollupIDs(list []rollupAutomaton) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.ID)
	}
	return out
}

func TestParseRollupWindow(t *testing.T) {
	cases := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{"", rollupDefaultWindowDays, false},
		{"1", 1, false},
		{"30", 30, false},
		{"365", 365, false},
		{"0", 0, true},
		{"366", 0, true},
		{"-3", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := parseRollupWindow(c.raw)
		if (err != nil) != c.wantErr {
			t.Fatalf("parseRollupWindow(%q) err=%v, wantErr=%v", c.raw, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("parseRollupWindow(%q) = %d, want %d", c.raw, got, c.want)
		}
	}
}

func TestBuildProjectRollup_GroupsAutomataByArc(t *testing.T) {
	dir := "/srv/acme"
	recent := rollupNow.Add(-2 * time.Hour)
	old := rollupNow.Add(-10 * 24 * time.Hour)

	// p1: held by arc auto-a via AutomationID; completed in window; 2 tasks, 1 done in window.
	p1 := &autonomous.PRD{
		ID: "p1", Title: "first", ProjectDir: dir, AutomationID: "auto-a",
		Status: autonomous.PRDCompleted, UpdatedAt: recent,
		Story: []autonomous.Story{{ID: "s1", Tasks: []autonomous.Task{
			{ID: "t1", Status: autonomous.TaskCompleted, CompletedAt: &recent},
			{ID: "t2", Status: autonomous.TaskPending},
		}}},
	}
	// p2: held by arc auto-b only through an Automation member (no AutomationID); running.
	p2 := &autonomous.PRD{
		ID: "p2", ProjectDir: dir, Status: autonomous.PRDRunning, UpdatedAt: recent,
		Story: []autonomous.Story{{ID: "s2", Tasks: []autonomous.Task{
			{ID: "t3", Status: autonomous.TaskCompleted, CompletedAt: &old},
		}}},
	}
	// p3: in no arc, in this project's dir → unassigned.
	p3 := &autonomous.PRD{ID: "p3", ProjectDir: dir, Status: autonomous.PRDDraft, UpdatedAt: recent}
	// p4: names an Automation that is not an arc → unassigned for this project.
	p4 := &autonomous.PRD{ID: "p4", ProjectDir: dir, AutomationID: "auto-z", Status: autonomous.PRDDraft, UpdatedAt: recent}
	// p5: another project's Automaton, in no arc → excluded.
	p5 := &autonomous.PRD{ID: "p5", ProjectDir: "/srv/other", Status: autonomous.PRDCompleted, UpdatedAt: recent}
	// p6: a template → excluded even though it sits in the project dir.
	p6 := &autonomous.PRD{ID: "p6", ProjectDir: dir, IsTemplate: true, Status: autonomous.PRDDraft}

	in := rollupInput{
		Project: "acme", Dir: dir + "/",
		Arcs: []config.ProjectArc{
			{AutomationID: "auto-a", Priority: 1, State: config.ArcStateActive},
			{AutomationID: "auto-b", Priority: 2},
			{AutomationID: "auto-a", Priority: 9}, // duplicate arc, ignored
		},
		PRDs:        []*autonomous.PRD{p1, p2, p3, p4, p5, p6},
		Automations: []*autonomous.Automation{rollupAutomationWith("auto-b", "p2")},
		SessionIDs: func(id string) []string {
			if id == "p1" {
				return []string{"sess-1", "sess-2"}
			}
			return nil
		},
		Points:     map[string]float64{"p1": 2.5},
		WindowDays: 7,
		Now:        rollupNow,
	}
	got := buildProjectRollup(in)

	if len(got.Arcs) != 2 {
		t.Fatalf("arcs = %d, want 2 (duplicate arc ignored)", len(got.Arcs))
	}
	a, b := got.Arcs[0], got.Arcs[1]
	if a.AutomationID != "auto-a" || a.State != config.ArcStateActive || a.Priority != 1 {
		t.Errorf("arc a = %+v", a)
	}
	if b.AutomationID != "auto-b" || b.State != config.ArcStatePlanned {
		t.Errorf("arc b = %+v (empty state must read as planned)", b)
	}
	if ids := rollupIDs(a.Automata); len(ids) != 1 || ids[0] != "p1" {
		t.Errorf("arc a automata = %v, want [p1]", ids)
	}
	if ids := rollupIDs(b.Automata); len(ids) != 1 || ids[0] != "p2" {
		t.Errorf("arc b automata = %v, want [p2]", ids)
	}
	if ids := rollupIDs(got.Unassigned.Automata); len(ids) != 2 || ids[0] != "p3" || ids[1] != "p4" {
		t.Errorf("unassigned = %v, want [p3 p4]", ids)
	}
	if n := len(got.Total.Automata); n != 4 {
		t.Errorf("total automata = %d, want 4 (p5 and template excluded)", n)
	}

	// Session ids, finished flag and points on the Automaton line.
	if sess := a.Automata[0].SessionIDs; len(sess) != 2 || sess[0] != "sess-1" {
		t.Errorf("p1 session ids = %v", sess)
	}
	if !a.Automata[0].Finished || a.Automata[0].Points != 2.5 {
		t.Errorf("p1 line = %+v", a.Automata[0])
	}
	if b.Automata[0].SessionIDs == nil {
		t.Errorf("p2 session ids must be an empty list, not null")
	}

	// Task progress and throughput per arc.
	if a.Tasks != (rollupTasks{Done: 1, Total: 2}) {
		t.Errorf("arc a tasks = %+v, want 1/2", a.Tasks)
	}
	if b.Tasks != (rollupTasks{Done: 1, Total: 1}) {
		t.Errorf("arc b tasks = %+v, want 1/1", b.Tasks)
	}
	if a.Throughput.AutomataFinished != 1 || a.Throughput.TasksFinished != 1 {
		t.Errorf("arc a throughput = %+v", a.Throughput)
	}
	if b.Throughput.TasksFinished != 0 {
		t.Errorf("arc b task finished 10 days ago must be outside the window: %+v", b.Throughput)
	}
	if want := 1.0 / 7.0; a.Throughput.AutomataPerDay != want {
		t.Errorf("arc a automata/day = %v, want %v", a.Throughput.AutomataPerDay, want)
	}

	// Points and totals.
	if a.Points != 2.5 || b.Points != 0 {
		t.Errorf("points a=%v b=%v, want 2.5 and 0", a.Points, b.Points)
	}
	if !got.PointsPriced {
		t.Errorf("points_priced = false, want true")
	}
	if got.Total.Points != 2.5 {
		t.Errorf("total points = %v, want 2.5", got.Total.Points)
	}
	if got.Total.Tasks != (rollupTasks{Done: 2, Total: 3}) {
		t.Errorf("total tasks = %+v, want 2/3", got.Total.Tasks)
	}
	if got.Total.Throughput.AutomataFinished != 1 || got.Total.Throughput.TasksFinished != 1 {
		t.Errorf("total throughput = %+v", got.Total.Throughput)
	}
	if got.WindowDays != 7 || !got.GeneratedAt.Equal(rollupNow) {
		t.Errorf("window/generated_at = %d / %v", got.WindowDays, got.GeneratedAt)
	}
}

func TestBuildProjectRollup_NoPricesNoArcs(t *testing.T) {
	got := buildProjectRollup(rollupInput{
		Project: "bare", Dir: "/srv/bare", WindowDays: 1, Now: rollupNow,
	})
	if got.PointsPriced {
		t.Errorf("points_priced = true with no points")
	}
	if len(got.Arcs) != 0 || len(got.Unassigned.Automata) != 0 || len(got.Total.Automata) != 0 {
		t.Errorf("empty project should have no arcs or automata: %+v", got)
	}
	if got.Total.Points != 0 || got.Total.Throughput.AutomataPerDay != 0 {
		t.Errorf("empty totals should be zero: %+v", got.Total)
	}
}

func TestBuildProjectRollup_ArcWithoutDirStillGroups(t *testing.T) {
	// A PRD in an arc is counted even when its ProjectDir is empty.
	p := &autonomous.PRD{ID: "p1", AutomationID: "auto-a", Status: autonomous.PRDRunning}
	got := buildProjectRollup(rollupInput{
		Dir: "/srv/acme", Arcs: []config.ProjectArc{{AutomationID: "auto-a"}},
		PRDs: []*autonomous.PRD{p}, WindowDays: 7, Now: rollupNow,
	})
	if ids := rollupIDs(got.Arcs[0].Automata); len(ids) != 1 {
		t.Errorf("arc automata = %v, want [p1]", ids)
	}
	if len(got.Unassigned.Automata) != 0 {
		t.Errorf("unassigned = %v, want none", rollupIDs(got.Unassigned.Automata))
	}
}

func TestProjectRollup_HandlerErrors(t *testing.T) {
	s, _ := newAutomationRESTServer(t, t.TempDir())
	s.cfg.Projects = map[string]config.ProjectConfigEntry{"acme": {Dir: "/srv/acme"}}

	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		rr := httptest.NewRecorder()
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/projects/{name}/rollup", s.handleProjectRollup)
		mux.ServeHTTP(rr, req)
		return rr
	}

	if rr := call(http.MethodPost, "/api/projects/acme/rollup"); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rr.Code)
	}
	if rr := call(http.MethodGet, "/api/projects/missing/rollup"); rr.Code != http.StatusNotFound {
		t.Errorf("unknown project status = %d, want 404", rr.Code)
	}
	if rr := call(http.MethodGet, "/api/projects/acme/rollup?window=0"); rr.Code != http.StatusBadRequest {
		t.Errorf("bad window status = %d, want 400", rr.Code)
	}

	noMgr := &Server{cfg: s.cfg}
	req := httptest.NewRequest(http.MethodGet, "/api/projects/acme/rollup", nil)
	rr := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects/{name}/rollup", noMgr.handleProjectRollup)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("no automations manager status = %d, want 503", rr.Code)
	}
}

func TestProjectRollup_HandlerReturnsUnassignedAutomata(t *testing.T) {
	dir := t.TempDir()
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	s.cfg.Projects = map[string]config.ProjectConfigEntry{"acme": {Dir: dir}}

	prd, err := mgr.CreatePRD("roll-up spec", dir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	done := time.Now()
	stored, ok := mgr.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("PRD %s not in store", prd.ID)
	}
	stored.Status = autonomous.PRDCompleted
	stored.UpdatedAt = done
	stored.Story = []autonomous.Story{{ID: "s1", Tasks: []autonomous.Task{
		{ID: "t1", Status: autonomous.TaskCompleted, CompletedAt: &done},
		{ID: "t2", Status: autonomous.TaskPending},
	}}}
	if err := mgr.Store().SavePRD(stored); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/projects/acme/rollup?window=7", nil)
	req.SetPathValue("name", "acme")
	rr := httptest.NewRecorder()
	s.handleProjectRollup(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got projectRollup
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Project != "acme" || got.WindowDays != 7 {
		t.Errorf("project/window = %q/%d", got.Project, got.WindowDays)
	}
	if len(got.Unassigned.Automata) != 1 || got.Unassigned.Automata[0].ID != prd.ID {
		t.Fatalf("unassigned = %+v, want the one PRD", got.Unassigned.Automata)
	}
	if got.Unassigned.Tasks != (rollupTasks{Done: 1, Total: 2}) {
		t.Errorf("unassigned tasks = %+v, want 1/2", got.Unassigned.Tasks)
	}
	if got.Total.Throughput.AutomataFinished != 1 || got.Total.Throughput.TasksFinished != 1 {
		t.Errorf("total throughput = %+v", got.Total.Throughput)
	}
	if got.Unassigned.Automata[0].SessionIDs == nil {
		t.Errorf("session_ids must be [] when there are none")
	}
}

// rollupTask builds a completed task finished at when.
func rollupTask(id string, when *time.Time) autonomous.Task {
	return autonomous.Task{ID: id, Status: autonomous.TaskCompleted, CompletedAt: when}
}

func TestBuildProjectRollup_FinishedCounting(t *testing.T) {
	dir := "/srv/acme"
	in2h := rollupNow.Add(-2 * time.Hour)
	in3h := rollupNow.Add(-3 * time.Hour)
	old := rollupNow.Add(-20 * 24 * time.Hour)

	prds := []*autonomous.PRD{
		// completed in the window
		{ID: "done", ProjectDir: dir, Status: autonomous.PRDCompleted, UpdatedAt: in2h},
		// cancelled after its work merged: counted, at the merge time
		{ID: "merged", ProjectDir: dir, Status: autonomous.PRDCancelled, UpdatedAt: old,
			Git:   autonomous.GitWorkflow{Branch: "auto/merged"},
			Story: []autonomous.Story{{ID: "s", Tasks: []autonomous.Task{rollupTask("t1", &in3h), rollupTask("t1b", &old)}}}},
		// cancelled, branch not merged: not counted, its task still is
		{ID: "unmerged", ProjectDir: dir, Status: autonomous.PRDCancelled, UpdatedAt: in2h,
			Git:   autonomous.GitWorkflow{Branch: "auto/unmerged"},
			Story: []autonomous.Story{{ID: "s", Tasks: []autonomous.Task{rollupTask("t2", &in2h)}}}},
		// archived with a shipped record
		{ID: "shipped", ProjectDir: dir, Status: autonomous.PRDArchived, UpdatedAt: old,
			MergeCommit: "abc123", MergedAt: &in3h},
		// completed long ago: outside the window
		{ID: "ancient", ProjectDir: dir, Status: autonomous.PRDCompleted, UpdatedAt: old},
		// the same Automaton listed twice: counted once, its task once
		{ID: "dup", ProjectDir: dir, Status: autonomous.PRDRunning, UpdatedAt: in2h,
			Story: []autonomous.Story{{ID: "s", Tasks: []autonomous.Task{rollupTask("t3", &in2h)}}}},
	}
	prds = append(prds, prds[len(prds)-1])

	got := buildProjectRollup(rollupInput{
		Project: "acme", Dir: dir, PRDs: prds, WindowDays: 7, Now: rollupNow,
		ShippedEnabled: true,
		Merged: func(p *autonomous.PRD) (bool, time.Time) {
			if p.ID == "merged" {
				return true, in3h
			}
			return false, time.Time{}
		},
	})
	th := got.Total.Throughput
	// done, merged, shipped
	if th.AutomataFinished != 3 {
		t.Errorf("automata finished = %d, want 3", th.AutomataFinished)
	}
	// t1 (in window), t2, t3 once; t1b is outside the window
	if th.TasksFinished != 3 {
		t.Errorf("tasks finished = %d, want 3", th.TasksFinished)
	}
	finished := map[string]bool{}
	for _, a := range got.Total.Automata {
		finished[a.ID] = a.Finished
	}
	for id, want := range map[string]bool{"done": true, "merged": true, "unmerged": false, "shipped": true, "ancient": true, "dup": false} {
		if finished[id] != want {
			t.Errorf("automaton %s finished = %v, want %v", id, finished[id], want)
		}
	}
	if len(got.Total.Automata) != 6 {
		t.Errorf("duplicate Automaton listed twice: %d lines, want 6", len(got.Total.Automata))
	}
}

func TestBuildProjectRollup_MergeTimeFallsBackToLastUpdate(t *testing.T) {
	dir := "/srv/acme"
	recent := rollupNow.Add(-1 * time.Hour)
	p := &autonomous.PRD{ID: "m", ProjectDir: dir, Status: autonomous.PRDArchived, UpdatedAt: recent}
	merged := func(*autonomous.PRD) (bool, time.Time) { return true, time.Time{} }
	got := buildProjectRollup(rollupInput{Project: "acme", Dir: dir, PRDs: []*autonomous.PRD{p},
		WindowDays: 7, Now: rollupNow, Merged: merged})
	if got.Total.Throughput.AutomataFinished != 1 {
		t.Fatalf("merge time unknown should use the last update, got %d", got.Total.Throughput.AutomataFinished)
	}
	p.UpdatedAt = rollupNow.Add(-30 * 24 * time.Hour)
	got = buildProjectRollup(rollupInput{Project: "acme", Dir: dir, PRDs: []*autonomous.PRD{p},
		WindowDays: 7, Now: rollupNow, Merged: merged})
	if got.Total.Throughput.AutomataFinished != 0 {
		t.Fatalf("old last update is outside the window, got %d", got.Total.Throughput.AutomataFinished)
	}
}

func TestBuildProjectRollup_WorktreeAutomatonWithLink(t *testing.T) {
	recent := rollupNow.Add(-1 * time.Hour)
	wt := &autonomous.PRD{
		ID: "wt", ProjectDir: "/srv/work/.datawatch/prd-worktrees/wt1", Status: autonomous.PRDCancelled,
		UpdatedAt: recent, Git: autonomous.GitWorkflow{Branch: "auto/wt"},
		Story: []autonomous.Story{{ID: "s", Tasks: []autonomous.Task{rollupTask("t", &recent)}}},
	}
	in := rollupInput{
		Project: "acme", Dir: "/srv/acme", PRDs: []*autonomous.PRD{wt}, WindowDays: 7, Now: rollupNow,
		Merged: func(*autonomous.PRD) (bool, time.Time) { return true, recent },
	}
	// no link and a worktree directory: not this project's
	if got := buildProjectRollup(in); got.Total.Throughput.AutomataFinished != 0 || len(got.Total.Automata) != 0 {
		t.Fatalf("unlinked worktree Automaton must not count: %+v", got.Total)
	}
	wt.Project = "acme"
	got := buildProjectRollup(in)
	if got.Total.Throughput.AutomataFinished != 1 || got.Total.Throughput.TasksFinished != 1 {
		t.Fatalf("linked worktree Automaton: %+v", got.Total.Throughput)
	}
}

func TestBuildProjectRollup_ResolverOrderAndWithoutProject(t *testing.T) {
	projects := map[string]string{"acme": "/srv/acme", "other": "/srv/other"}
	linkedElsewhere := &autonomous.PRD{ID: "x1", ProjectDir: "/srv/acme", Project: "other", Status: autonomous.PRDDraft}
	byDir := &autonomous.PRD{ID: "x2", ProjectDir: "/srv/acme", Status: autonomous.PRDDraft}
	orphan := &autonomous.PRD{ID: "x3", ProjectDir: "/nowhere", Status: autonomous.PRDDraft}
	in := rollupInput{Project: "acme", Dir: "/srv/acme", Projects: projects,
		PRDs: []*autonomous.PRD{linkedElsewhere, byDir, orphan}, WindowDays: 7, Now: rollupNow}
	got := buildProjectRollup(in)
	if len(got.Unassigned.Automata) != 1 || got.Unassigned.Automata[0].ID != "x2" {
		t.Fatalf("unassigned: %+v", got.Unassigned.Automata)
	}
	if len(got.WithoutProject.Automata) != 1 || got.WithoutProject.Automata[0].ID != "x3" {
		t.Fatalf("without_project: %+v", got.WithoutProject.Automata)
	}
	if len(got.Total.Automata) != 1 {
		t.Fatalf("without_project must not be in total: %+v", got.Total.Automata)
	}
	in.Project, in.Dir = "other", "/srv/other"
	got = buildProjectRollup(in)
	if len(got.WithoutProject.Automata) != 1 || len(got.Unassigned.Automata) != 1 || got.Unassigned.Automata[0].ID != "x1" {
		t.Fatalf("other project: %+v %+v", got.Unassigned.Automata, got.WithoutProject.Automata)
	}
}

func TestCompletedDisplayRollupThroughput(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-24 * time.Hour)
	mk := func(id string, st autonomous.PRDStatus, commit string) *autonomous.PRD {
		return &autonomous.PRD{ID: id, Status: st, MergeCommit: commit, UpdatedAt: recent, AutomationID: "arc"}
	}
	in := rollupInput{
		Project: "p", Dir: "/x",
		Arcs: []config.ProjectArc{{AutomationID: "arc"}},
		PRDs: []*autonomous.PRD{
			mk("done", autonomous.PRDCompleted, ""),
			mk("merged-cancelled", autonomous.PRDCancelled, "abc"),
			mk("plain-cancelled", autonomous.PRDCancelled, ""),
		},
		WindowDays: 7, Now: now, ShippedEnabled: true,
	}
	got := buildProjectRollup(in)
	if n := got.Total.Throughput.AutomataFinished; n != 2 {
		t.Errorf("finished = %d, want 2 (completed + merged-cancelled)", n)
	}
	for _, a := range got.Arcs[0].Automata {
		if a.ID == "merged-cancelled" && (a.Status != "completed" || !a.Finished) {
			t.Errorf("merged-cancelled line = %+v", a)
		}
		if a.ID == "plain-cancelled" && a.Finished {
			t.Errorf("plain cancelled counted as finished")
		}
	}
	in.ShippedEnabled = false
	if n := buildProjectRollup(in).Total.Throughput.AutomataFinished; n != 1 {
		t.Errorf("surface off finished = %d, want 1", n)
	}
}
