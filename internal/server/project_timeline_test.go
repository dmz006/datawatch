// Tests for the project timeline read (GET /api/projects/{name}/timeline).

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

var timelineNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func tlTime(h int) *time.Time {
	t := timelineNow.Add(time.Duration(h) * time.Hour)
	return &t
}

// tlPRD makes an Automaton created at hour c, updated at hour u, in dir.
func tlPRD(id string, status autonomous.PRDStatus, c, u int, dir string, tasks ...autonomous.Task) *autonomous.PRD {
	p := &autonomous.PRD{ID: id, Title: "t " + id, Status: status, ProjectDir: dir,
		CreatedAt: *tlTime(c), UpdatedAt: *tlTime(u)}
	if len(tasks) > 0 {
		p.Story = []autonomous.Story{{ID: id + "-s", Tasks: tasks}}
	}
	return p
}

func tlTask(id string, status autonomous.TaskStatus, start, end *time.Time) autonomous.Task {
	return autonomous.Task{ID: id, Title: "task " + id, Status: status, StartedAt: start, CompletedAt: end}
}

func tlAutomation(id string, prdIDs ...string) *autonomous.Automation {
	a := &autonomous.Automation{ID: id, Name: "arc " + id}
	for _, p := range prdIDs {
		a.Members = append(a.Members, autonomous.AutomationMember{PRDID: p})
	}
	return a
}

func tlBuild(t *testing.T, in timelineInput) projectTimeline {
	t.Helper()
	if in.Project == "" {
		in.Project = "acme"
	}
	if in.Dir == "" {
		in.Dir = "/srv/acme"
	}
	in.Now = timelineNow
	out, err := buildProjectTimeline(in)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return out
}

func tlRow(t *testing.T, rows []timelineRow, id string) timelineRow {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no row %q in %+v", id, rows)
	return timelineRow{}
}

func TestProjectTimeline_ArcsSpanEarliestToLatest(t *testing.T) {
	prds := []*autonomous.PRD{
		tlPRD("a1", autonomous.PRDCompleted, -50, -20, "/x", tlTask("k1", autonomous.TaskCompleted, tlTime(-40), tlTime(-10))),
		tlPRD("a2", autonomous.PRDCompleted, -30, -15, "/x"),
	}
	out := tlBuild(t, timelineInput{
		Arcs:    []config.ProjectArc{{AutomationID: "arc1", State: "active"}},
		Entries: summaryOf(prds), Load: loaderOf(prds),
		Automations: []*autonomous.Automation{tlAutomation("arc1", "a1", "a2")},
	})
	if out.Level != "arcs" || out.Total != 1 || out.HasMore {
		t.Fatalf("out = %+v", out)
	}
	r := out.Rows[0]
	if r.Kind != "arc" || r.ID != "arc1" || r.State != "active" || r.Title != "arc arc1" {
		t.Errorf("row = %+v", r)
	}
	if r.First == nil || !r.First.Equal(*tlTime(-50)) {
		t.Errorf("first = %v, want -50h", r.First)
	}
	if r.Last == nil || !r.Last.Equal(*tlTime(-15)) {
		t.Errorf("last = %v, want -15h (latest member end; tasks never widen the arc)", r.Last)
	}
	if r.Running || !r.HasChildren || r.ChildrenCount != 2 {
		t.Errorf("row = %+v", r)
	}
}

func TestProjectTimeline_ArcWithNoAutomataIsPlannedWithNullTimes(t *testing.T) {
	out := tlBuild(t, timelineInput{Arcs: []config.ProjectArc{{AutomationID: "empty"}}})
	if len(out.Rows) != 1 {
		t.Fatalf("rows = %+v", out.Rows)
	}
	r := out.Rows[0]
	if r.State != "planned" || r.First != nil || r.Last != nil || r.HasChildren || r.ChildrenCount != 0 || r.Running {
		t.Errorf("row = %+v", r)
	}
	b, _ := json.Marshal(r)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if v, ok := m["first"]; !ok || v != nil {
		t.Errorf("first must serialize as null, got %v (present=%v)", v, ok)
	}
}

func TestProjectTimeline_RunningWorkHasNullLastAndRunning(t *testing.T) {
	prds := []*autonomous.PRD{
		tlPRD("r1", autonomous.PRDRunning, -10, -1, "/x",
			tlTask("done", autonomous.TaskCompleted, tlTime(-9), tlTime(-5)),
			tlTask("live", autonomous.TaskInProgress, tlTime(-4), nil)),
	}
	in := timelineInput{
		Arcs:    []config.ProjectArc{{AutomationID: "arc1"}},
		Entries: summaryOf(prds), Load: loaderOf(prds),
		Automations: []*autonomous.Automation{tlAutomation("arc1", "r1")},
	}
	arc := tlBuild(t, in).Rows[0]
	if arc.Last != nil || !arc.Running || arc.First == nil {
		t.Errorf("arc = %+v", arc)
	}
	in.Level, in.Parent = "automata", "arc1"
	au := tlBuild(t, in).Rows[0]
	if au.Last != nil || !au.Running || au.ChildrenCount != 2 {
		t.Errorf("automaton = %+v", au)
	}
	in.Level, in.Automaton = "tasks", "r1"
	rows := tlBuild(t, in).Rows
	live := tlRow(t, rows, "live")
	if live.Last != nil || !live.Running || live.First == nil {
		t.Errorf("live task = %+v", live)
	}
	done := tlRow(t, rows, "done")
	if done.Running || done.Last == nil {
		t.Errorf("done task = %+v", done)
	}
}

func TestProjectTimeline_ExplicitLinkBeatsDirectory(t *testing.T) {
	// The Automaton's directory is the project's, but an arc holds it: it is in the arc.
	p := tlPRD("e1", autonomous.PRDCompleted, -5, -1, "/srv/acme")
	in := timelineInput{
		Arcs:    []config.ProjectArc{{AutomationID: "arc1"}},
		Entries: summaryOf([]*autonomous.PRD{p}), Load: loaderOf([]*autonomous.PRD{p}),
		Automations: []*autonomous.Automation{tlAutomation("arc1", "e1")},
	}
	out := tlBuild(t, in)
	if len(out.Rows) != 1 || out.Rows[0].Kind != "arc" || out.Rows[0].ChildrenCount != 1 {
		t.Fatalf("rows = %+v", out.Rows)
	}
	// An explicit AutomationID on the PRD also wins over the directory.
	p.AutomationID = "arc1"
	in.Automations = nil
	in.Entries = summaryOf([]*autonomous.PRD{p})
	out = tlBuild(t, in)
	if len(out.Rows) != 1 || out.Rows[0].ChildrenCount != 1 {
		t.Fatalf("rows = %+v", out.Rows)
	}
}

func TestProjectTimeline_NoArcGroupUsesDirectoryOnlyAsFallback(t *testing.T) {
	prds := []*autonomous.PRD{
		tlPRD("dir1", autonomous.PRDCompleted, -5, -1, "/srv/acme"),
		tlPRD("other", autonomous.PRDCompleted, -5, -1, "/srv/other"),
		tlPRD("held", autonomous.PRDCompleted, -5, -1, "/srv/other"),
		tlPRD("tmpl", autonomous.PRDCompleted, -5, -1, "/srv/acme"),
	}
	prds[3].IsTemplate = true
	in := timelineInput{
		Arcs:    []config.ProjectArc{{AutomationID: "arc1"}},
		Entries: summaryOf(prds), Load: loaderOf(prds),
		Automations: []*autonomous.Automation{tlAutomation("arc1", "held"), tlAutomation("elsewhere", "other")},
	}
	out := tlBuild(t, in)
	if len(out.Rows) != 2 {
		t.Fatalf("rows = %+v", out.Rows)
	}
	arc, un := out.Rows[0], out.Rows[1]
	if arc.ID != "arc1" || arc.ChildrenCount != 1 {
		t.Errorf("arc = %+v", arc)
	}
	if un.Kind != "unassigned" || un.ChildrenCount != 1 {
		t.Errorf("unassigned = %+v (want only dir1: other project, template skipped)", un)
	}
	in.Level, in.Parent = "automata", "none"
	rows := tlBuild(t, in).Rows
	if len(rows) != 1 || rows[0].ID != "dir1" {
		t.Errorf("none rows = %+v", rows)
	}
	// With no unassigned Automata there is no unassigned row.
	out = tlBuild(t, timelineInput{Arcs: in.Arcs, Entries: summaryOf(prds[1:3]), Load: loaderOf(prds[1:3]), Automations: in.Automations})
	if len(out.Rows) != 1 || out.Rows[0].Kind != "arc" {
		t.Errorf("rows = %+v", out.Rows)
	}
}

func TestProjectTimeline_ChildrenPagingWithCursor(t *testing.T) {
	var prds []*autonomous.PRD
	for i := 0; i < 120; i++ {
		prds = append(prds, tlPRD(fmt.Sprintf("p%03d", i), autonomous.PRDCompleted, -200+i, -100+i, "/x"))
	}
	in := timelineInput{
		Level:   "automata",
		Parent:  "arc1",
		Arcs:    []config.ProjectArc{{AutomationID: "arc1"}},
		Entries: summaryOf(prds), Load: loaderOf(prds),
		Automations: []*autonomous.Automation{tlAutomation("arc1", func() []string {
			var ids []string
			for _, p := range prds {
				ids = append(ids, p.ID)
			}
			return ids
		}()...)},
		Limit: 50,
	}
	var got []string
	pages := 0
	for {
		out := tlBuild(t, in)
		pages++
		if out.Total != 120 {
			t.Fatalf("total = %d", out.Total)
		}
		for _, r := range out.Rows {
			got = append(got, r.ID)
		}
		if !out.HasMore {
			if out.NextCursor != "" {
				t.Errorf("last page has a cursor")
			}
			if len(out.Rows) != 20 {
				t.Errorf("last page = %d rows, want 20", len(out.Rows))
			}
			break
		}
		if len(out.Rows) != 50 || out.NextCursor == "" {
			t.Fatalf("page %d: rows=%d cursor=%q", pages, len(out.Rows), out.NextCursor)
		}
		c, err := decodeHistoryCursor(out.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		in.Cursor = c
	}
	if pages != 3 || len(got) != 120 {
		t.Fatalf("pages=%d rows=%d", pages, len(got))
	}
	for i, id := range got {
		if id != fmt.Sprintf("p%03d", i) {
			t.Fatalf("row %d = %s", i, id)
		}
	}
}

func TestProjectTimeline_TasksLevel(t *testing.T) {
	p := tlPRD("a1", autonomous.PRDCompleted, -9, -1, "/x",
		tlTask("k1", autonomous.TaskCompleted, tlTime(-8), tlTime(-6)),
		tlTask("k2", autonomous.TaskPending, nil, nil))
	in := timelineInput{
		Level: "tasks", Automaton: "a1",
		Arcs:    []config.ProjectArc{{AutomationID: "arc1"}},
		Entries: summaryOf([]*autonomous.PRD{p}), Load: loaderOf([]*autonomous.PRD{p}),
		Automations: []*autonomous.Automation{tlAutomation("arc1", "a1")},
	}
	out := tlBuild(t, in)
	if out.Level != "tasks" || out.Parent != "a1" || len(out.Rows) != 2 {
		t.Fatalf("out = %+v", out)
	}
	k1, k2 := out.Rows[0], out.Rows[1]
	if k1.Kind != "task" || k1.State != "completed" || k1.First == nil || k1.Last == nil || k1.HasChildren {
		t.Errorf("k1 = %+v", k1)
	}
	if k2.First != nil || k2.Last != nil || k2.Running {
		t.Errorf("k2 = %+v", k2)
	}
	in.Automaton = "missing"
	if _, err := buildProjectTimeline(in); err == nil || err.Status != http.StatusNotFound {
		t.Errorf("unknown automaton err = %v", err)
	}
	in.Automaton = ""
	if _, err := buildProjectTimeline(in); err == nil || err.Status != http.StatusBadRequest {
		t.Errorf("missing automaton err = %v", err)
	}
}

func TestProjectTimeline_FocusReturnsAutomataAndTasksInOneReply(t *testing.T) {
	prds := []*autonomous.PRD{
		tlPRD("a1", autonomous.PRDCompleted, -9, -1, "/x",
			tlTask("k1", autonomous.TaskCompleted, tlTime(-8), tlTime(-6)),
			tlTask("k2", autonomous.TaskCompleted, tlTime(-6), tlTime(-3))),
		tlPRD("a2", autonomous.PRDCompleted, -5, -2, "/x"),
	}
	in := timelineInput{
		Focus: "arc1", Limit: 1,
		Arcs:    []config.ProjectArc{{AutomationID: "arc1"}},
		Entries: summaryOf(prds), Load: loaderOf(prds),
		Automations: []*autonomous.Automation{tlAutomation("arc1", "a1", "a2")},
	}
	out := tlBuild(t, in)
	if out.Level != "focus" || out.Parent != "arc1" || out.HasMore || out.Total != 4 || len(out.Rows) != 4 {
		t.Fatalf("out = %+v", out)
	}
	kinds := []string{"automaton", "task", "task", "automaton"}
	for i, k := range kinds {
		if out.Rows[i].Kind != k {
			t.Errorf("row %d kind = %s, want %s", i, out.Rows[i].Kind, k)
		}
	}
	in.Focus = "nope"
	if _, err := buildProjectTimeline(in); err == nil || err.Status != http.StatusNotFound {
		t.Errorf("unknown focus err = %v", err)
	}
}

func TestProjectTimeline_HandlerErrors(t *testing.T) {
	s, _ := newAutomationRESTServer(t, t.TempDir())
	s.cfg.Projects = map[string]config.ProjectConfigEntry{"acme": {Dir: "/srv/acme"}}
	call := func(srv *Server, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		rr := httptest.NewRecorder()
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/projects/{name}/timeline", srv.handleProjectTimeline)
		mux.ServeHTTP(rr, req)
		return rr
	}
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/projects/acme/timeline", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/projects/missing/timeline", http.StatusNotFound},
		{http.MethodGet, "/api/projects/acme/timeline?limit=0", http.StatusBadRequest},
		{http.MethodGet, "/api/projects/acme/timeline?limit=abc", http.StatusBadRequest},
		{http.MethodGet, "/api/projects/acme/timeline?cursor=!!!", http.StatusBadRequest},
		{http.MethodGet, "/api/projects/acme/timeline?level=bogus", http.StatusBadRequest},
		{http.MethodGet, "/api/projects/acme/timeline?level=tasks", http.StatusBadRequest},
		{http.MethodGet, "/api/projects/acme/timeline?level=automata&parent=ghost", http.StatusNotFound},
		{http.MethodGet, "/api/projects/acme/timeline?cursor=bm90IGpzb24", http.StatusBadRequest},
		{http.MethodGet, "/api/projects/acme/timeline", http.StatusOK},
	}
	for _, c := range cases {
		if rr := call(s, c.method, c.path); rr.Code != c.want {
			t.Errorf("%s %s = %d, want %d (%s)", c.method, c.path, rr.Code, c.want, rr.Body.String())
		}
	}
	noMgr := &Server{cfg: s.cfg}
	if rr := call(noMgr, http.MethodGet, "/api/projects/acme/timeline"); rr.Code != http.StatusServiceUnavailable {
		t.Errorf("no manager status = %d, want 503", rr.Code)
	}
}

func TestProjectTimeline_UnknownCursorKeyIs400(t *testing.T) {
	c := &historyCursor{K: "arc:does-not-exist"}
	_, err := buildProjectTimeline(timelineInput{Project: "acme", Dir: "/srv/acme", Cursor: c,
		Arcs: []config.ProjectArc{{AutomationID: "arc1"}}, Now: timelineNow})
	if err == nil || err.Status != http.StatusBadRequest {
		t.Errorf("err = %v, want 400", err)
	}
}

// ---- Gantt accuracy: spans, ordering, edges, status wording ----

// Each arc spans its own members' earliest start to latest end; no row, and no
// reply field, carries a whole-window span covering unrelated arcs.
func TestProjectTimeline_GanttArcSpansAreMemberMinMaxNotWholeWindow(t *testing.T) {
	prds := []*autonomous.PRD{
		tlPRD("m1", autonomous.PRDCompleted, -90, -80, "/x"),
		tlPRD("m2", autonomous.PRDCompleted, -60, -30, "/x"),
		tlPRD("n1", autonomous.PRDCompleted, -10, -5, "/x"),
	}
	out := tlBuild(t, timelineInput{
		Arcs:    []config.ProjectArc{{AutomationID: "arcA"}, {AutomationID: "arcB"}},
		Entries: summaryOf(prds), Load: loaderOf(prds),
		Automations: []*autonomous.Automation{tlAutomation("arcA", "m1", "m2"), tlAutomation("arcB", "n1")},
	})
	a, b := tlRow(t, out.Rows, "arcA"), tlRow(t, out.Rows, "arcB")
	if a.Start == nil || !a.Start.Equal(*tlTime(-90)) || a.End == nil || !a.End.Equal(*tlTime(-30)) {
		t.Errorf("arcA span = %v..%v, want -90h..-30h", a.Start, a.End)
	}
	if b.Start == nil || !b.Start.Equal(*tlTime(-10)) || b.End == nil || !b.End.Equal(*tlTime(-5)) {
		t.Errorf("arcB span = %v..%v, want -10h..-5h", b.Start, b.End)
	}
	if !a.End.Before(*b.Start) {
		t.Errorf("arc spans must not overlap through a shared window: %v vs %v", a.End, b.Start)
	}
}

// Rows come back in completion order: earliest end first, an unfinished row
// after every finished one.
func TestProjectTimeline_GanttAutomataOrderedByCompletionTime(t *testing.T) {
	prds := []*autonomous.PRD{
		tlPRD("late", autonomous.PRDCompleted, -50, -5, "/srv/acme"),
		tlPRD("live", autonomous.PRDRunning, -40, -1, "/srv/acme", tlTask("k", autonomous.TaskInProgress, tlTime(-3), nil)),
		tlPRD("early", autonomous.PRDCompleted, -60, -20, "/srv/acme"),
	}
	// No Automation chains these, so only completion time orders them.
	out := tlBuild(t, timelineInput{Level: "automata", Parent: "none", Entries: summaryOf(prds), Load: loaderOf(prds)})
	var ids []string
	for _, r := range out.Rows {
		ids = append(ids, r.ID)
	}
	if fmt.Sprint(ids) != "[early late live]" {
		t.Errorf("order = %v, want [early late live]", ids)
	}
}

// A dependency sits above its dependent even when it finished later; rows with
// no dependency between them keep completion order (the tie break).
func TestProjectTimeline_GanttTopologicalOrderTieBrokenByCompletion(t *testing.T) {
	rows := []timelineRow{
		{ID: "c", End: tlTime(-10), Start: tlTime(-12)},
		{ID: "b", End: tlTime(-30), Start: tlTime(-32), DependsOn: []string{"d"}},
		{ID: "d", End: tlTime(-5), Start: tlTime(-8)},
		{ID: "a", End: tlTime(-20), Start: tlTime(-22)},
	}
	got := orderTimelineRows(rows, nil)
	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	// Completion order is b, a, c, d. b waits for d, so d is placed first and
	// then b is released; the free rows a and c keep completion order after it.
	if fmt.Sprint(ids) != "[a d b c]" && fmt.Sprint(ids) != "[a c d b]" {
		t.Fatalf("order = %v", ids)
	}
	pos := map[string]int{}
	for i, id := range ids {
		pos[id] = i
	}
	if pos["d"] > pos["b"] {
		t.Errorf("dependency d must precede b: %v", ids)
	}
	if pos["a"] > pos["c"] {
		t.Errorf("tie between a and c must break by completion (a first): %v", ids)
	}
	if rows[0].ID != "c" {
		t.Errorf("input was reordered")
	}
	// Edges count as dependencies too.
	got = orderTimelineRows(rows, []timelineEdge{{From: "c", To: "a"}})
	pos = map[string]int{}
	for i, r := range got {
		pos[r.ID] = i
	}
	if pos["c"] > pos["a"] {
		t.Errorf("edge c->a must put c above a")
	}
}

// A task's depends_on reaches the tasks reply, and the tasks are listed with
// the dependency above the dependent.
func TestProjectTimeline_GanttTaskDependencyIsReported(t *testing.T) {
	second := tlTask("second", autonomous.TaskCompleted, tlTime(-8), tlTime(-7))
	second.DependsOn = []string{"first"}
	first := tlTask("first", autonomous.TaskCompleted, tlTime(-6), tlTime(-5))
	p := tlPRD("a1", autonomous.PRDCompleted, -9, -1, "/x", second, first)
	out := tlBuild(t, timelineInput{
		Level: "tasks", Automaton: "a1",
		Arcs:    []config.ProjectArc{{AutomationID: "arc1"}},
		Entries: summaryOf([]*autonomous.PRD{p}), Load: loaderOf([]*autonomous.PRD{p}),
		Automations: []*autonomous.Automation{tlAutomation("arc1", "a1")},
	})
	s := tlRow(t, out.Rows, "second")
	if len(s.DependsOn) != 1 || s.DependsOn[0] != "first" {
		t.Errorf("depends_on = %v", s.DependsOn)
	}
	if out.Rows[0].ID != "first" || out.Rows[1].ID != "second" {
		t.Errorf("order = %s,%s; the dependency must come first", out.Rows[0].ID, out.Rows[1].ID)
	}
}

// A chain step between two Automata of one Automation is an edge in the
// automata reply and in the focus reply.
func TestProjectTimeline_GanttChainStepBetweenAutomataIsAnEdge(t *testing.T) {
	prds := []*autonomous.PRD{
		tlPRD("p1", autonomous.PRDCompleted, -50, -30, "/x"),
		tlPRD("p2", autonomous.PRDCompleted, -29, -10, "/x"),
	}
	auto := &autonomous.Automation{ID: "arc1", Name: "arc arc1", Members: []autonomous.AutomationMember{
		{ID: "m1", PRDID: "p1", OnComplete: "m2"},
		{ID: "m2", PRDID: "p2"},
	}}
	in := timelineInput{
		Level: "automata", Parent: "arc1",
		Arcs:    []config.ProjectArc{{AutomationID: "arc1"}},
		Entries: summaryOf(prds), Load: loaderOf(prds),
		Automations: []*autonomous.Automation{auto},
	}
	out := tlBuild(t, in)
	found := false
	for _, e := range out.Edges {
		if e.From == "p1" && e.To == "p2" {
			found = true
		}
	}
	if !found {
		t.Errorf("edges = %+v, want p1 -> p2", out.Edges)
	}
}

// A running row has no end and the Running status; a finished row reads
// Completed and only an Automaton merged into a release carries the release.
func TestProjectTimeline_GanttRunningStatusAndCompletedRelease(t *testing.T) {
	merged := tlPRD("shipped", autonomous.PRDCompleted, -20, -10, "/x")
	merged.MergedInRelease = "v9.9.0"
	plain := tlPRD("plain", autonomous.PRDCompleted, -20, -9, "/x")
	live := tlPRD("live", autonomous.PRDRunning, -8, -1, "/x", tlTask("k", autonomous.TaskInProgress, tlTime(-3), nil))
	out := tlBuild(t, timelineInput{
		Level: "automata", Parent: "arc1",
		Arcs:    []config.ProjectArc{{AutomationID: "arc1"}},
		Entries: summaryOf([]*autonomous.PRD{merged, plain, live}), Load: loaderOf([]*autonomous.PRD{merged, plain, live}),
		Automations: []*autonomous.Automation{tlAutomation("arc1", "shipped", "plain", "live")},
	})
	s, p, l := tlRow(t, out.Rows, "shipped"), tlRow(t, out.Rows, "plain"), tlRow(t, out.Rows, "live")
	if s.Status != "completed" || s.Release != "v9.9.0" {
		t.Errorf("shipped = %q release %q", s.Status, s.Release)
	}
	if p.Status != "completed" || p.Release != "" {
		t.Errorf("plain = %q release %q", p.Status, p.Release)
	}
	if l.Status != "running" || l.End != nil || !l.Running {
		t.Errorf("live = %+v", l)
	}
}
