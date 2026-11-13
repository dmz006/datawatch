package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// acmeRecord is a stored project record with every field the CLI reads.
const acmeRecord = `{"name":"acme","dir":"/srv/acme","default_backend":"claude-code",` +
	`"description":"Acme work","policy":{"plan_review_mode":"block"},"owner":"dana",` +
	`"repository":"acme/app","default_branch":"main",` +
	`"arcs":[{"automation_id":"a1","priority":1,"state":"active"}],` +
	`"target":{"name":"v1","definition_of_done":"shipped"},` +
	`"weekly_share":30,"branch_retention_days":14,"major_trigger":"manual"}`

const acmeRollup = `{"project":"acme","window_days":7,"arcs":[],"total":{}}`

// fakeProjectsDaemon answers the project REST routes the record verbs use and
// records what the CLI sent.
type fakeProjectsDaemon struct {
	mu            sync.Mutex
	posted        []map[string]any
	rollupWindow  string
	historyQuery  string
	timelineQuery string
	paths         []string
}

func (f *fakeProjectsDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/projects":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.posted = append(f.posted, body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/projects/acme":
		_, _ = w.Write([]byte(acmeRecord))
	case r.Method == http.MethodGet && r.URL.Path == "/api/projects/acme/plans":
		_, _ = w.Write([]byte(`{"project":"acme","plans":[],"backlog":{"plans":[]}}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/projects/acme/rollup":
		f.rollupWindow = r.URL.Query().Get("window")
		_, _ = w.Write([]byte(acmeRollup))
	case r.Method == http.MethodGet && r.URL.Path == "/api/projects/acme/history":
		f.historyQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"project":"acme","items":[],"gantt":[]}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/projects/acme/timeline":
		f.timelineQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(timelineNewFieldsBody))
	default:
		http.Error(w, "project not found", http.StatusNotFound)
	}
}

// runProjectsCmd points the CLI at a fake daemon and runs `projects <args>`.
func runProjectsCmd(t *testing.T, f *fakeProjectsDaemon, args ...string) error {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	oldURL := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = oldURL })
	t.Setenv("DATAWATCH_TOKEN", "test-token")
	cmd := newProjectsCmd()
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func (f *fakeProjectsDaemon) lastPost(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posted) == 0 {
		t.Fatal("no POST /api/projects was sent")
	}
	return f.posted[len(f.posted)-1]
}

func (f *fakeProjectsDaemon) postCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posted)
}

func TestProjectsRecordSubcommandsRegistered(t *testing.T) {
	cmd := newProjectsCmd()
	want := map[string]bool{"show": false, "set": false, "arcs": false, "history": false, "timeline": false}
	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for verb, found := range want {
		if !found {
			t.Errorf("`projects %s` is not registered", verb)
		}
	}
	setCmd, _, err := cmd.Find([]string{"set", "acme"})
	if err != nil || setCmd.Name() != "set" {
		t.Fatalf("Find(set) = %v, %v", setCmd, err)
	}
	for _, flag := range []string{"owner", "repository", "default-branch", "target",
		"weekly-share", "branch-retention-days", "major-trigger"} {
		if setCmd.Flags().Lookup(flag) == nil {
			t.Errorf("set is missing the --%s flag", flag)
		}
	}
	arcsCmd, _, _ := cmd.Find([]string{"arcs", "acme"})
	if arcsCmd.Flags().Lookup("set") == nil {
		t.Error("arcs is missing the --set flag")
	}
	showCmd, _, _ := cmd.Find([]string{"show", "acme"})
	if showCmd.Flags().Lookup("window") == nil {
		t.Error("show is missing the --window flag")
	}
	historyCmd, _, _ := cmd.Find([]string{"history", "acme"})
	for _, flag := range []string{"limit", "cursor"} {
		if historyCmd.Flags().Lookup(flag) == nil {
			t.Errorf("history is missing the --%s flag", flag)
		}
	}
}

func TestParseArcSpecs(t *testing.T) {
	got, err := parseArcSpecs([]string{"a1:1:active", "b2:2:done", "c3:0:"})
	if err != nil {
		t.Fatalf("valid specs rejected: %v", err)
	}
	want := []projectArcArg{
		{AutomationID: "a1", Priority: 1, State: "active"},
		{AutomationID: "b2", Priority: 2, State: "done"},
		{AutomationID: "c3", Priority: 0, State: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseArcSpecs = %+v, want %+v", got, want)
	}

	empty, err := parseArcSpecs(nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("empty list = %#v, %v; want non-nil empty slice and no error", empty, err)
	}

	bad := map[string][]string{
		"too few parts":    {"a1:1"},
		"too many parts":   {"a1:1:done:x"},
		"empty id":         {":1:done"},
		"non-integer prio": {"a1:high:done"},
		"unknown state":    {"a1:1:paused"},
		"duplicate id":     {"a1:1:done", "a1:2:active"},
	}
	for name, specs := range bad {
		if _, err := parseArcSpecs(specs); err == nil {
			t.Errorf("%s: parseArcSpecs(%v) accepted bad input", name, specs)
		}
	}
}

func TestProjectsSetSendsOnlyChangedFieldsAndEchoesStoredFields(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "set", "acme", "--owner", "ops", "--weekly-share", "40", "--target", "v2"); err != nil {
		t.Fatalf("set: %v", err)
	}
	body := f.lastPost(t)

	// Echoed so the daemon does not reset them.
	if body["name"] != "acme" || body["dir"] != "/srv/acme" {
		t.Errorf("name/dir = %v/%v, want acme//srv/acme", body["name"], body["dir"])
	}
	if body["default_backend"] != "claude-code" || body["description"] != "Acme work" {
		t.Errorf("default_backend/description not echoed: %v", body)
	}
	policy, ok := body["policy"].(map[string]any)
	if !ok || policy["plan_review_mode"] != "block" {
		t.Errorf("policy not echoed: %v", body["policy"])
	}

	// Changed.
	if body["owner"] != "ops" {
		t.Errorf("owner = %v, want ops", body["owner"])
	}
	if body["weekly_share"] != float64(40) {
		t.Errorf("weekly_share = %v, want 40", body["weekly_share"])
	}
	target, _ := body["target"].(map[string]any)
	if target["name"] != "v2" || target["definition_of_done"] != "shipped" {
		t.Errorf("target = %v, want name v2 keeping definition_of_done shipped", target)
	}

	// Not passed, so not sent.
	for _, key := range []string{"repository", "default_branch", "branch_retention_days", "major_trigger", "arcs"} {
		if _, present := body[key]; present {
			t.Errorf("%s sent though its flag was not given: %v", key, body[key])
		}
	}
}

func TestProjectsSetAllFlagsAndRefusesEmpty(t *testing.T) {
	f := &fakeProjectsDaemon{}
	err := runProjectsCmd(t, f, "set", "acme",
		"--repository", "acme/core", "--default-branch", "trunk",
		"--branch-retention-days", "30", "--major-trigger", "quarterly")
	if err != nil {
		t.Fatalf("set all flags: %v", err)
	}
	body := f.lastPost(t)
	if body["repository"] != "acme/core" || body["default_branch"] != "trunk" ||
		body["branch_retention_days"] != float64(30) || body["major_trigger"] != "quarterly" {
		t.Errorf("flags not sent: %v", body)
	}

	before := f.postCount()
	if err := runProjectsCmd(t, f, "set", "acme"); err == nil {
		t.Error("set with no fields should fail")
	}
	if f.postCount() != before {
		t.Error("set with no fields must not POST")
	}
}

func TestProjectsSetUnknownProjectFails(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "set", "missing", "--owner", "ops"); err == nil {
		t.Error("set on an unknown project should return the daemon's 404")
	}
	if f.postCount() != 0 {
		t.Error("set on an unknown project must not POST")
	}
}

func TestProjectsArcsShowsWithoutPosting(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "arcs", "acme"); err != nil {
		t.Fatalf("arcs show: %v", err)
	}
	if f.postCount() != 0 {
		t.Error("arcs without --set must not POST")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.paths) != 1 || f.paths[0] != "GET /api/projects/acme/plans" {
		t.Errorf("arcs without --set must run the plans list, got %v", f.paths)
	}
}

func TestProjectsArcsSetReplacesList(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "arcs", "acme", "--set", "a1:2:done,b2:1:planned"); err != nil {
		t.Fatalf("arcs --set: %v", err)
	}
	body := f.lastPost(t)
	raw, err := json.Marshal(body["arcs"])
	if err != nil {
		t.Fatal(err)
	}
	var got []projectArcArg
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := []projectArcArg{
		{AutomationID: "a1", Priority: 2, State: "done"},
		{AutomationID: "b2", Priority: 1, State: "planned"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("arcs sent = %+v, want %+v", got, want)
	}
	if body["owner"] != nil {
		t.Errorf("arcs set must not send record fields: owner=%v", body["owner"])
	}
	if body["dir"] != "/srv/acme" {
		t.Errorf("dir not echoed: %v", body["dir"])
	}
}

func TestProjectsArcsSetRejectsBadSpecWithoutPosting(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "arcs", "acme", "--set", "a1:x:done"); err == nil {
		t.Error("bad arc spec should fail")
	}
	if f.postCount() != 0 {
		t.Error("bad arc spec must not POST")
	}
}

func TestProjectsArcsSetEmptyClears(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "arcs", "acme", "--set="); err != nil {
		t.Fatalf("arcs --set= : %v", err)
	}
	body := f.lastPost(t)
	arcs, ok := body["arcs"].([]any)
	if !ok || len(arcs) != 0 {
		t.Errorf("arcs = %v, want an explicit empty list to clear them", body["arcs"])
	}
}

func TestProjectsHistoryPassesLimitAndCursor(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "history", "acme", "--limit", "5", "--cursor", "abc"); err != nil {
		t.Fatalf("history: %v", err)
	}
	if f.historyQuery != "cursor=abc&limit=5" {
		t.Errorf("history query = %q, want cursor=abc&limit=5", f.historyQuery)
	}
	f2 := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f2, "history", "acme"); err != nil {
		t.Fatalf("history: %v", err)
	}
	if f2.historyQuery != "" {
		t.Errorf("history with no flags sent query %q", f2.historyQuery)
	}
}

func TestProjectsShowFetchesRecordAndRollup(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "show", "acme", "--window", "30"); err != nil {
		t.Fatalf("show: %v", err)
	}
	if f.rollupWindow != "30" {
		t.Errorf("rollup window = %q, want 30", f.rollupWindow)
	}
	joined := strings.Join(f.paths, " ")
	if !strings.Contains(joined, "GET /api/projects/acme") || !strings.Contains(joined, "GET /api/projects/acme/rollup") {
		t.Errorf("show did not read both record and rollup: %v", f.paths)
	}
}

func TestRenderProjectShowCombinesRecordAndRollup(t *testing.T) {
	out, err := renderProjectShow([]byte(acmeRecord), []byte(acmeRollup))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Record map[string]any `json:"record"`
		Rollup map[string]any `json:"rollup"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got.Record["owner"] != "dana" {
		t.Errorf("record.owner = %v, want dana", got.Record["owner"])
	}
	if got.Rollup["project"] != "acme" {
		t.Errorf("rollup.project = %v, want acme", got.Rollup["project"])
	}
}

func TestProjectsShowWithoutWindowOmitsQuery(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "show", "acme"); err != nil {
		t.Fatalf("show: %v", err)
	}
	if f.rollupWindow != "" {
		t.Errorf("rollup window = %q, want none so the daemon default applies", f.rollupWindow)
	}
}

func TestProjectsTimelinePassesFlags(t *testing.T) {
	f := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f, "timeline", "acme", "--level", "tasks", "--parent", "none",
		"--automaton", "p1", "--focus", "a1", "--limit", "5", "--cursor", "abc"); err != nil {
		t.Fatalf("timeline: %v", err)
	}
	want := "automaton=p1&cursor=abc&focus=a1&level=tasks&limit=5&parent=none"
	if f.timelineQuery != want {
		t.Errorf("timeline query = %q, want %q", f.timelineQuery, want)
	}
	f2 := &fakeProjectsDaemon{}
	if err := runProjectsCmd(t, f2, "timeline", "acme"); err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if f2.timelineQuery != "" {
		t.Errorf("timeline with no flags sent query %q", f2.timelineQuery)
	}
	if err := runProjectsCmd(t, &fakeProjectsDaemon{}, "timeline", "ghost"); err == nil {
		t.Error("an unknown project must be an error")
	}
	cmd := newProjectsCmd()
	tl, _, _ := cmd.Find([]string{"timeline", "acme"})
	for _, flag := range []string{"level", "parent", "automaton", "focus", "limit", "cursor"} {
		if tl.Flags().Lookup(flag) == nil {
			t.Errorf("timeline is missing the --%s flag", flag)
		}
	}
}

const timelineNewFieldsBody = `{"project":"acme","level":"automata","rows":[{"kind":"automaton","id":"p1","title":"Alpha","first":"2026-11-01T10:00:00Z","last":null,"start":"2026-11-01T10:00:00Z","end":null,"status":"running","release":"v1.2.0","depends_on":["t0"]}],"edges":[{"from":"p0","to":"p1","kind":"chain"}]}`

func TestProjectsTimelinePrintsNewFields(t *testing.T) {
	var runErr error
	out := captureStdout(t, func() {
		runErr = runProjectsCmd(t, &fakeProjectsDaemon{}, "timeline", "acme", "--level", "automata")
	})
	if runErr != nil {
		t.Fatalf("timeline: %v", runErr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	rows, _ := got["rows"].([]any)
	row, _ := rows[0].(map[string]any)
	if row["status"] != "running" || row["start"] != "2026-11-01T10:00:00Z" || row["end"] != nil || row["release"] != "v1.2.0" {
		t.Errorf("row fields changed: %v", row)
	}
	if deps, _ := row["depends_on"].([]any); len(deps) != 1 || deps[0] != "t0" {
		t.Errorf("depends_on = %v", row["depends_on"])
	}
	if edges, _ := got["edges"].([]any); len(edges) != 1 {
		t.Errorf("edges = %v", got["edges"])
	}
	tl, _, _ := newProjectsCmd().Find([]string{"timeline", "acme"})
	for _, want := range []string{"status", "dependencies", "chain edges"} {
		if !strings.Contains(tl.Short, want) {
			t.Errorf("help lacks %q: %s", want, tl.Short)
		}
	}
}
