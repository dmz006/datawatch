package main

// Tests of the release plan commands of `datawatch plan`. A fake daemon answers
// canned replies by "METHOD /path" and records every request with its body, so
// the tests can check the route, the body and, above all, that a write which
// lacks the preview id never reaches the daemon as a write. Nothing sleeps.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

type releaseReq struct {
	method, path, query string
	body                map[string]any
}

func (r releaseReq) String() string { return r.method + " " + r.path }

type releaseReply struct {
	code int
	body string
}

type releaseFake struct {
	mu     sync.Mutex
	routes map[string]releaseReply
	seen   []releaseReq
}

func (f *releaseFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.seen = append(f.seen, releaseReq{r.Method, r.URL.Path, r.URL.RawQuery, body})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	rep, ok := f.routes[r.Method+" "+r.URL.Path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no such route in the fake"}`))
		return
	}
	if rep.code != 0 {
		w.WriteHeader(rep.code)
	}
	_, _ = w.Write([]byte(rep.body))
}

func (f *releaseFake) last() releaseReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) == 0 {
		return releaseReq{}
	}
	return f.seen[len(f.seen)-1]
}

func (f *releaseFake) writes() []releaseReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []releaseReq
	for _, r := range f.seen {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

// runPlanRelease runs `datawatch plan <args>` through a root command, so the
// "plan" alias is exercised too.
func runPlanRelease(t *testing.T, routes map[string]releaseReply, args ...string) (*releaseFake, string, error) {
	t.Helper()
	f := &releaseFake{routes: routes}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	t.Setenv("DATAWATCH_TOKEN", "test-token")
	root := &cobra.Command{Use: "datawatch"}
	root.AddCommand(newPlansCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"plan"}, args...))
	root.SilenceUsage, root.SilenceErrors = true, true
	err := root.Execute()
	return f, out.String(), err
}

const releaseThemesReply = `{"plan":{"title":"Release 2","id":"rel-2","plan_kind":"release","release":"2.0"},
"themes":[{"id":"t1","name":"Plans","order":1,"items":[{"kind":"automaton","id":"a1","title":"Plans browser","state":"running"}]},
{"id":"t2","name":"Docs","order":2,"items":[]}],
"unplaced":[{"kind":"automaton","id":"a2","title":"Loose end","state":"draft"}]}`

func TestPlanRelease_ListsThemes(t *testing.T) {
	f, out, err := runPlanRelease(t, map[string]releaseReply{
		"GET /api/plans/Release 2/themes": {0, releaseThemesReply},
	}, "theme", "list", "Release 2")
	if err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.method != "GET" || r.path != "/api/plans/Release 2/themes" {
		t.Fatalf("sent %v", r)
	}
	for _, want := range []string{"Plan Release 2  [rel-2]  release 2.0", "1. Plans  [t1]  1 item(s)",
		"Plans browser  [a1]  automaton  running", "2. Docs  [t2]  0 item(s)", "Unplaced: 1 item(s)", "Loose end  [a2]"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// --json prints the daemon's object.
	_, out, err = runPlanRelease(t, map[string]releaseReply{
		"GET /api/plans/rel-2/themes": {0, releaseThemesReply},
	}, "theme", "list", "rel-2", "--json")
	if err != nil || !strings.Contains(out, `"unplaced"`) {
		t.Fatalf("--json: %v %q", err, out)
	}
	// The older name still works.
	root := &cobra.Command{Use: "datawatch"}
	root.AddCommand(newPlansCmd())
	for _, name := range []string{"plans", "plan"} {
		if c, _, err := root.Find([]string{name}); err != nil || c.Name() != "plans" {
			t.Errorf("%q must find the plans command: %v %v", name, c, err)
		}
	}
}

func TestPlanRelease_ThemeWritesSendTheRightBodies(t *testing.T) {
	routes := map[string]releaseReply{"POST /api/plans/p/themes": {0, releaseThemesReply}}
	cases := []struct {
		name string
		args []string
		want map[string]any
	}{
		{"add", []string{"theme", "add", "p", "Plans"}, map[string]any{"action": "create", "name": "Plans"}},
		{"rename", []string{"theme", "rename", "p", "t1", "Pages"}, map[string]any{"action": "rename", "theme": "t1", "name": "Pages"}},
		{"remove", []string{"theme", "remove", "p", "t1"}, map[string]any{"action": "delete", "theme": "t1"}},
		{"place", []string{"theme", "place", "p", "t1", "--automaton", "a1"}, map[string]any{"action": "place", "theme": "t1", "kind": "automaton", "id": "a1"}},
		{"place automation", []string{"theme", "place", "p", "t1", "--automation", "au1"}, map[string]any{"action": "place", "theme": "t1", "kind": "automation", "id": "au1"}},
		{"unplace", []string{"theme", "unplace", "p", "--automaton", "a1"}, map[string]any{"action": "unplace", "kind": "automaton", "id": "a1"}},
	}
	for _, c := range cases {
		f, out, err := runPlanRelease(t, routes, c.args...)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		r := f.last()
		if r.method != "POST" || r.path != "/api/plans/p/themes" {
			t.Errorf("%s: sent %v", c.name, r)
		}
		for k, v := range c.want {
			if r.body[k] != v {
				t.Errorf("%s: body[%s] = %v, want %v (%v)", c.name, k, r.body[k], v, r.body)
			}
		}
		if len(r.body) != len(c.want) {
			t.Errorf("%s: body = %v, want %v", c.name, r.body, c.want)
		}
		// The themes after the change print, so the operator sees the result.
		if !strings.Contains(out, "Plan Release 2") {
			t.Errorf("%s: the themes after the change must print:\n%s", c.name, out)
		}
	}
	f, _, err := runPlanRelease(t, routes, "theme", "reorder", "p", "t2", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.last().body["themes"].([]any); len(got) != 2 || got[0] != "t2" || f.last().body["action"] != "reorder" {
		t.Fatalf("reorder body %v", f.last().body)
	}
	f, _, err = runPlanRelease(t, routes, "theme", "reorder-items", "p", "t1", "--kind", "automaton", "a2", "a1")
	if err != nil {
		t.Fatal(err)
	}
	if b := f.last().body; b["action"] != "reorder_items" || b["theme"] != "t1" || b["kind"] != "automaton" || len(b["items"].([]any)) != 2 {
		t.Fatalf("reorder-items body %v", b)
	}
}

func TestPlanRelease_ThemeRefusesBadArgumentsBeforeTheDaemon(t *testing.T) {
	routes := map[string]releaseReply{"POST /api/plans/p/themes": {0, `{}`}}
	for name, args := range map[string][]string{
		"place with no kind":          {"theme", "place", "p", "t1"},
		"place with both kinds":       {"theme", "place", "p", "t1", "--automaton", "a", "--automation", "b"},
		"unplace with no kind":        {"theme", "unplace", "p"},
		"reorder-items without kind":  {"theme", "reorder-items", "p", "t1", "a1"},
		"reorder-items with bad kind": {"theme", "reorder-items", "p", "t1", "--kind", "finding", "a1"},
		"add with no name":            {"theme", "add", "p"},
	} {
		f, _, err := runPlanRelease(t, routes, args...)
		if err == nil {
			t.Errorf("%s: want an error", name)
		}
		if len(f.seen) != 0 {
			t.Errorf("%s: the refused command reached the daemon: %v", name, f.seen)
		}
	}
}

func TestPlanRelease_ArchiveAndRestoreAPlan(t *testing.T) {
	f, out, err := runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/Old plan/archive": {0, `{"plan":{"title":"Old plan","id":"old","archived":true},"automata_held":2,"changed":true}`},
	}, "archive", "Old plan", "--reason", "shipped", "--protect", "rel-2")
	if err != nil {
		t.Fatal(err)
	}
	r := f.last()
	if r.method != "POST" || r.path != "/api/plans/Old plan/archive" || r.body["reason"] != "shipped" || r.body["protected_plan"] != "rel-2" {
		t.Fatalf("archive sent %v %v", r, r.body)
	}
	if !strings.Contains(out, "Archived plan Old plan  [old]") || !strings.Contains(out, "nothing was deleted") {
		t.Errorf("archive output %q", out)
	}

	_, out, err = runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/old/archive": {0, `{"plan":{"title":"Old plan","id":"old"},"automata_held":2,"changed":false}`},
	}, "archive", "old")
	if err != nil || !strings.Contains(out, "already archived; nothing changed") {
		t.Fatalf("a second archive: %v %q", err, out)
	}

	f, out, err = runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/old/restore": {0, `{"plan":{"title":"Old plan","id":"old"},"changed":true}`},
	}, "restore", "old")
	if err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.method != "POST" || r.path != "/api/plans/old/restore" {
		t.Fatalf("restore sent %v", r)
	}
	if !strings.Contains(out, "Restored plan Old plan  [old]") {
		t.Errorf("restore output %q", out)
	}

	_, out, err = runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/old/restore": {0, `{"plan":{"title":"Old plan","id":"old"},"changed":false}`},
	}, "restore", "old")
	if err != nil || !strings.Contains(out, "was not archived; nothing changed") {
		t.Fatalf("a restore of a plan that is not archived: %v %q", err, out)
	}
}

func TestPlanRelease_ArchiveOfAProtectedPlanIsRefused(t *testing.T) {
	_, _, err := runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/rel-2/archive": {409, `{"error":"plan rel-2 holds 3 Automata and is protected","plan_id":"rel-2","automata_held":3}`},
	}, "archive", "rel-2", "--protect", "rel-2")
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("the refusal must surface: %v", err)
	}
}

func TestPlanRelease_UnattributedShowsAnAutomatonHeldElsewhere(t *testing.T) {
	f, out, err := runPlanRelease(t, map[string]releaseReply{
		"GET /api/plans/unattributed": {0, `{"automata":[
{"title":"Free one","id":"a1","state":"draft","project":"datawatch","suggested_release":"2.0","suggested_theme":"Plans"},
{"title":"Held one","id":"a2","state":"running","project":"datawatch","holding_plan":"Old plan","holding_plan_id":"old"}],
"page":{"total":5,"limit":2,"offset":0}}`},
	}, "unattributed", "--project", "datawatch", "--current-release", "2.0", "--limit", "2")
	if err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.method != "GET" || !strings.Contains(r.query, "project=datawatch") || !strings.Contains(r.query, "current_release=2.0") || !strings.Contains(r.query, "limit=2") {
		t.Fatalf("sent %v", r)
	}
	for _, want := range []string{"Free one  [a1]  draft  datawatch  → release 2.0, theme Plans",
		"Held one  [a2]", "held by plan Old plan [old]; detach it first", "(2 of 5 shown"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	_, out, err = runPlanRelease(t, map[string]releaseReply{
		"GET /api/plans/unattributed": {0, `{"automata":[],"page":{"total":0}}`},
	}, "unattributed")
	if err != nil || !strings.Contains(out, "Every Automaton is held by a release plan") {
		t.Fatalf("empty list: %v %q", err, out)
	}
}

const bulkAttachReply = `{"attached":"automaton","plan":{"title":"Release 2","id":"rel-2"},"theme":"t1",
"results":[{"id":"a1","title":"One","status":"attached","theme":"t1"},
{"id":"a2","title":"Two","status":"refused","holder":"old","error":"automaton \"a2\" is already linked to old; detach it first"},
{"id":"a3","status":"not_found","error":"no automaton matches \"a3\""}]}`

func TestPlanRelease_AttachSeveralReportsARefusedAutomaton(t *testing.T) {
	f, out, err := runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/rel-2/attach": {0, bulkAttachReply},
	}, "attach", "rel-2", "--automaton", "a1", "--automaton", "a2,a3", "--theme", "Plans")
	r := f.last()
	if r.method != "POST" || r.path != "/api/plans/rel-2/attach" || r.body["kind"] != "automaton" || r.body["theme"] != "Plans" {
		t.Fatalf("sent %v %v", r, r.body)
	}
	ids, _ := r.body["ids"].([]any)
	if len(ids) != 3 || ids[0] != "a1" || ids[2] != "a3" {
		t.Fatalf("ids = %v", r.body["ids"])
	}
	if _, has := r.body["id"]; has {
		t.Fatalf("a list must not send id: %v", r.body)
	}
	for _, want := range []string{"attached: One  [a1]  (theme t1)", "refused: Two  [a2]", "attached to plan old; detach it first", "not found: a3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if err == nil || !strings.Contains(err.Error(), "2 of 3") {
		t.Fatalf("a refused id must make the command fail so a script sees it: %v", err)
	}
}

func TestPlanRelease_AttachOneIdKeepsTheOlderRequest(t *testing.T) {
	f, out, err := runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/p1/attach": {0, `{"attached":"automaton"}`},
	}, "attach", "p1", "--automaton", "a1")
	if err != nil {
		t.Fatal(err)
	}
	if b := f.last().body; b["kind"] != "automaton" || b["id"] != "a1" || len(b) != 2 {
		t.Fatalf("one id must send kind and id only: %v", b)
	}
	if !strings.Contains(out, "Attached automaton a1 to plan p1") {
		t.Errorf("output %q", out)
	}
	// A theme turns even one id into the list form.
	f, _, err = runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/p1/attach": {0, `{"plan":{"title":"P","id":"p1"},"results":[{"id":"a1","status":"attached","theme":"t1"}]}`},
	}, "attach", "p1", "--automation", "au1", "--theme", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if b := f.last().body; b["kind"] != "automation" || b["theme"] != "t1" || len(b["ids"].([]any)) != 1 {
		t.Fatalf("themed attach body %v", b)
	}
	// A conflict on the single form still surfaces.
	_, _, err = runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/p2/attach": {409, `{"error":"automaton \"a1\" is already linked to p1; detach it first"}`},
	}, "attach", "p2", "--automaton", "a1")
	if err == nil || !strings.Contains(err.Error(), "detach it first") {
		t.Fatalf("a conflict must be surfaced: %v", err)
	}
}

func TestPlanRelease_AttachRefusesBadFlagCombinationsBeforeTheDaemon(t *testing.T) {
	for name, args := range map[string][]string{
		"no item":           {"attach", "p"},
		"two kinds":         {"attach", "p", "--automaton", "a", "--automation", "b"},
		"finding and theme": {"attach", "p", "--finding", "f", "--theme", "t"},
		"finding and kind":  {"attach", "p", "--finding", "f", "--automaton", "a"},
	} {
		f, _, err := runPlanRelease(t, map[string]releaseReply{}, args...)
		if err == nil || len(f.seen) != 0 {
			t.Errorf("%s: err=%v, requests=%v", name, err, f.seen)
		}
	}
}

const releasePreviewReply = `{"mode":"release","applied":false,"preview_id":"pv-123","current_release":"2.0","next_release":"2.1",
"created":[{"kind":"plan","title":"Release 2.0","release":"2.0"}],
"attached":[{"kind":"automaton","id":"a1","title":"Plans browser","release":"2.0","theme":"Plans"}],
"unplaced":[],"conflicts":[{"kind":"automaton","id":"a2","title":"Held one","holding_plan":"Old plan","holding_plan_id":"old","reason":"attached to another plan"}],
"unchanged":[],"skipped":[]}`

func TestPlanRelease_MigratePreviewsAndRefusesToWriteWithoutThePreviewID(t *testing.T) {
	routes := map[string]releaseReply{"POST /api/plans/migrate": {0, releasePreviewReply}}

	// A plain --release is a preview and says how to write exactly it.
	f, out, err := runPlanRelease(t, routes, "migrate", "--release", "--project", "datawatch", "--current-release", "2.0", "--next-release", "2.1")
	if err != nil {
		t.Fatal(err)
	}
	b := f.last().body
	if b["mode"] != "release" || b["apply"] != false || b["project"] != "datawatch" || b["current_release"] != "2.0" || b["next_release"] != "2.1" {
		t.Fatalf("preview body %v", b)
	}
	for _, want := range []string{"Preview (release 2.0, next 2.1): would create 1, would attach 1",
		"would attach: Plans browser [a1] (automaton, release 2.0, theme Plans)",
		"refused: Held one [a2] (automaton) — held by plan Old plan [old] — attached to another plan",
		"--apply --preview-id pv-123"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview output lacks %q:\n%s", want, out)
		}
	}

	// --apply with no --preview-id: the preview prints, the write is refused,
	// and the only request the daemon saw was a preview (apply false).
	f, out, err = runPlanRelease(t, routes, "migrate", "--release", "--project", "datawatch", "--apply")
	if err == nil || !strings.Contains(err.Error(), "--preview-id") {
		t.Fatalf("a write without the preview id must be refused: %v", err)
	}
	if !strings.Contains(out, "Preview (release") || !strings.Contains(out, "Nothing was written") || !strings.Contains(out, "--preview-id pv-123") {
		t.Errorf("the refusal must show the preview and the id to use:\n%s", out)
	}
	for _, r := range f.writes() {
		if r.body["apply"] != false {
			t.Errorf("a refused write sent apply true: %v", r.body)
		}
	}

	// With the id it writes exactly that.
	f, _, err = runPlanRelease(t, routes, "migrate", "--release", "--project", "datawatch", "--apply", "--preview-id", "pv-123")
	if err != nil {
		t.Fatal(err)
	}
	if b := f.last().body; b["apply"] != true || b["preview_id"] != "pv-123" || b["mode"] != "release" {
		t.Fatalf("apply body %v", b)
	}
	if n := len(f.seen); n != 1 {
		t.Errorf("the write must be one request, got %d", n)
	}
}

func TestPlanRelease_MigrateStaleIDAndBadFlags(t *testing.T) {
	_, _, err := runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/migrate": {409, `{"error":"the preview is stale: the plans changed since you read it; preview again"}`},
	}, "migrate", "--release", "--project", "p", "--apply", "--preview-id", "old")
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("a stale id must surface: %v", err)
	}
	for name, args := range map[string][]string{
		"release without project":          {"migrate", "--release"},
		"preview id without apply":         {"migrate", "--release", "--project", "p", "--preview-id", "x"},
		"project without release":          {"migrate", "--project", "p"},
		"preview id without release":       {"migrate", "--preview-id", "x", "--apply"},
		"marks that are not a JSON object": {"migrate", "--release", "--project", "p", "--marks", "[1]"},
	} {
		f, _, err := runPlanRelease(t, map[string]releaseReply{"POST /api/plans/migrate": {0, releasePreviewReply}}, args...)
		if err == nil || len(f.seen) != 0 {
			t.Errorf("%s: err=%v requests=%v", name, err, f.seen)
		}
	}
}

func TestPlanRelease_MigrateWithoutReleaseIsTheArcsMigration(t *testing.T) {
	f, _, err := runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/migrate": {0, `{"note":"Preview only.","result":{"applied":false}}`},
	}, "migrate")
	if err != nil {
		t.Fatal(err)
	}
	if b := f.last().body; len(b) != 1 || b["apply"] != false {
		t.Fatalf("the arcs migration body = %v, want only apply false", b)
	}
	f, _, err = runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/migrate": {0, `{"result":{"applied":true}}`},
	}, "migrate", "--apply")
	if err != nil || f.last().body["apply"] != true {
		t.Fatalf("arcs --apply: %v %v", err, f.last().body)
	}
}

const rowPlanListReply = `{"row_plans":[
{"plan_id":"rp1","title":"Plan: Move plans","project":"datawatch","state":"planned","row_found":true,"automata":["a1"],"release_plan_title":"Release 2"},
{"plan_id":"rp2","title":"Plan: Gone row","project":"datawatch","state":"planned","row_found":false,"archived":true,"automata":[]}],"total":2}`

const rowPlanPreviewReply = `{"applied":false,"preview_id":"rp-77","current_release":"2.0",
"link_rows":[{"plan_id":"rp1","title":"Plan: Move plans","release_plan_title":"Release 2"}],
"backlog_items":[],"waiting":[{"plan_id":"rp3","title":"Plan: Waits","reason":"the release migration has not filed everything it holds"}],
"conflicts":[{"plan_id":"rp2","title":"Plan: Gone row","reason":"its row is gone"}],"unchanged":[]}`

func TestPlanRelease_RowPlansListAndConvert(t *testing.T) {
	f, out, err := runPlanRelease(t, map[string]releaseReply{
		"GET /api/plans/migrate/row-plans": {0, rowPlanListReply},
	}, "row-plans", "list", "--project", "datawatch")
	if err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.method != "GET" || r.query != "project=datawatch" {
		t.Fatalf("sent %v", r)
	}
	for _, want := range []string{"Plan: Move plans  [rp1]  datawatch  planned  holds 1 Automata  on release plan Release 2",
		"Plan: Gone row  [rp2]", "archived", "(its row is gone)"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}

	routes := map[string]releaseReply{"POST /api/plans/migrate/row-plans": {0, rowPlanPreviewReply}}
	f, out, err = runPlanRelease(t, routes, "row-plans", "convert", "--project", "datawatch", "--current-release", "2.0")
	if err != nil {
		t.Fatal(err)
	}
	if b := f.last().body; b["apply"] != false || b["project"] != "datawatch" || b["current_release"] != "2.0" {
		t.Fatalf("preview body %v", b)
	}
	for _, want := range []string{"Preview: would convert 1 to registry-row links, 0 to backlog items; waiting 1, conflicts 1",
		"would convert: Plan: Move plans [rp1] → release plan Release 2", "waiting: Plan: Waits", "conflict: Plan: Gone row [rp2] — its row is gone",
		"--apply --preview-id rp-77"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}

	// A write without the id: the preview prints, the write is refused.
	f, out, err = runPlanRelease(t, routes, "row-plans", "convert", "--apply")
	if err == nil || !strings.Contains(err.Error(), "--preview-id") {
		t.Fatalf("a write without the preview id must be refused: %v", err)
	}
	if !strings.Contains(out, "Nothing was written") || !strings.Contains(out, "--preview-id rp-77") {
		t.Errorf("the refusal must show the id to use:\n%s", out)
	}
	for _, r := range f.writes() {
		if r.body["apply"] != false {
			t.Errorf("a refused write sent apply true: %v", r.body)
		}
	}

	f, _, err = runPlanRelease(t, routes, "row-plans", "convert", "--apply", "--preview-id", "rp-77")
	if err != nil {
		t.Fatal(err)
	}
	if b := f.last().body; b["apply"] != true || b["preview_id"] != "rp-77" || len(f.seen) != 1 {
		t.Fatalf("apply body %v (%d requests)", b, len(f.seen))
	}

	_, _, err = runPlanRelease(t, map[string]releaseReply{
		"POST /api/plans/migrate/row-plans": {409, `{"error":"the row-plans changed since the preview; preview again"}`},
	}, "row-plans", "convert", "--apply", "--preview-id", "old")
	if err == nil || !strings.Contains(err.Error(), "preview again") {
		t.Fatalf("a stale id must surface: %v", err)
	}
	if f, _, err := runPlanRelease(t, routes, "row-plans", "convert", "--preview-id", "x"); err == nil || len(f.seen) != 0 {
		t.Errorf("--preview-id without --apply must be refused before the daemon: %v %v", err, f.seen)
	}
}

// Every release plan command has help text that links the how-to.
func TestPlanRelease_HelpLinksTheHowTo(t *testing.T) {
	root := &cobra.Command{Use: "datawatch"}
	root.AddCommand(newPlansCmd())
	for _, path := range [][]string{
		{"plan", "theme"}, {"plan", "archive"}, {"plan", "restore"}, {"plan", "unattributed"},
		{"plan", "attach"}, {"plan", "migrate"}, {"plan", "row-plans"},
	} {
		c, _, err := root.Find(path)
		if err != nil || c == nil || c.Name() != path[len(path)-1] {
			t.Errorf("%v: command not found (%v)", path, err)
			continue
		}
		if !strings.Contains(c.Long, "docs/howto/plan-records.md") {
			t.Errorf("%v: the help must link docs/howto/plan-records.md", path)
		}
		for _, banned := range []string{"PRD", "BL4", "GH#"} {
			if strings.Contains(c.Long+c.Short, banned) {
				t.Errorf("%v: help mentions %q", path, banned)
			}
		}
	}
	// Every theme and row-plans subcommand has a short description.
	for _, parent := range [][]string{{"plan", "theme"}, {"plan", "row-plans"}} {
		c, _, _ := root.Find(parent)
		if len(c.Commands()) == 0 {
			t.Errorf("%v has no subcommands", parent)
		}
		for _, sub := range c.Commands() {
			if sub.Short == "" {
				t.Errorf("%v %s has no short help", parent, sub.Name())
			}
		}
	}
}
