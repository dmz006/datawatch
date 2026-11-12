// `session list --group-by` — the grouped text view over the REST
// grouping read (GET /api/sessions?group_by=...&tag_prefix=...).

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const sessionGroupTestBody = `{
"group_by":"tag","tag_prefix":"automation:","total":5,
"groups":[
 {"key":"(none)","count":2,"state_mix":{"running":1,"complete":1},"sessions":[
  {"id":"a1","full_id":"h1-a1","name":"untagged","state":"running"},
  {"id":"b1","full_id":"h1-b1","name":"done","state":"complete","task_id":"T42"}],
  "stories":null,"progress":null},
 {"key":"automation:repo-ci-watch","count":3,"state_mix":{"running":1,"failed":2},"sessions":[
  {"id":"c1","full_id":"h1-c1","name":"watch","state":"running"},
  {"id":"d1","full_id":"h1-d1","name":"watch2","state":"failed"},
  {"id":"e1","full_id":"h1-e1","name":"watch3","state":"failed"}],
  "stories":null,"progress":null}],
"state_totals":{"running":2,"complete":1,"failed":2}}`

func TestSessionGroupQuery_MapsFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("group-by", "", "")
	cmd.Flags().String("tag-prefix", "", "")
	_ = cmd.Flags().Set("group-by", "tag")
	_ = cmd.Flags().Set("tag-prefix", "automation:")
	q := sessionGroupsQuery(cmd)
	want := url.Values{"group_by": {"tag"}, "tag_prefix": {"automation:"}}
	if q.Encode() != want.Encode() {
		t.Errorf("query = %q, want %q", q.Encode(), want.Encode())
	}

	// tag-alone axis without prefix
	_ = cmd.Flags().Set("group-by", "state")
	_ = cmd.Flags().Set("tag-prefix", "")
	if got := sessionGroupsQuery(cmd); got.Encode() != "group_by=state" {
		t.Errorf("query = %q", got.Encode())
	}
}

func TestSessionGroupCmd_HitsAPI(t *testing.T) {
	seen := recordingDaemon(t, sessionGroupTestBody)
	if err := runCmd(t, newSessionCmd(), "list", "--group-by", "tag", "--tag-prefix", "automation:"); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("want 1 request, got %d", len(*seen))
	}
	u := (*seen)[0]
	if u.Path != "/api/sessions" || u.Query().Get("group_by") != "tag" || u.Query().Get("tag_prefix") != "automation:" {
		t.Errorf("request = %s", u)
	}
}

func TestSessionGroupCmd_RendersGroupedView(t *testing.T) {
	recordingDaemon(t, sessionGroupTestBody)
	out := captureStdout(t, func() {
		if err := runCmd(t, newSessionCmd(), "list", "--group-by", "tag", "--tag-prefix", "automation:"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Sessions grouped by tag (tag prefix \"automation:\"), 5 total:") {
		t.Errorf("header missing:\n%s", out)
	}
	// Group header: key, count, state mix.
	if !strings.Contains(out, "automation:repo-ci-watch  3 (failed×2, running×1)") {
		t.Errorf("group header missing:\n%s", out)
	}
	// Sessions indented under the group, with state.
	if !strings.Contains(out, "[c1] watch | running") || !strings.Contains(out, "[d1] watch2 | failed") {
		t.Errorf("sessions not indented under groups:\n%s", out)
	}
	// task id shown on tagged tasks.
	if !strings.Contains(out, "· task T42") {
		t.Errorf("task id missing:\n%s", out)
	}
	// Per-state totals.
	if !strings.Contains(out, "Per-state totals: complete×1, failed×2, running×2") {
		t.Errorf("totals missing:\n%s", out)
	}
}

func TestSessionGroupCmd_AutomatonAxisShowsStoriesAndProgress(t *testing.T) {
	body := `{
"group_by":"automaton","tag_prefix":"","total":3,
"groups":[
 {"key":"A1","count":2,"state_mix":{"running":1,"complete":1},"sessions":[
  {"id":"a1","full_id":"h1-a1","name":"root","state":"running","story_id":""},
  {"id":"b1","full_id":"h1-b1","name":"leaf","state":"complete","story_id":"A1-3"}],
  "stories":[{"key":"A1-3","count":1,"state_mix":{"complete":1},"sessions":[{"id":"b1","full_id":"h1-b1","name":"leaf","state":"complete","story_id":"A1-3"}]}],
  "progress":{"done":1,"total":2}},
 {"key":"(unassigned)","count":1,"state_mix":{"failed":1},"sessions":[{"id":"c1","full_id":"h1-c1","name":"lost","state":"failed"}],
  "stories":null,"progress":null}],
"state_totals":{"running":1,"complete":1,"failed":1}}`
	recordingDaemon(t, body)
	out := captureStdout(t, func() {
		if err := runCmd(t, newSessionCmd(), "list", "--group-by", "automaton"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "A1  2 (complete×1, running×1) — done 1/2") {
		t.Errorf("automaton header + progress missing:\n%s", out)
	}
	if !strings.Contains(out, "story A1-3:  1 (complete×1)") {
		t.Errorf("story block missing:\n%s", out)
	}
	if !strings.Contains(out, "(unassigned)  1 (failed×1)") {
		t.Errorf("ungrouped group missing:\n%s", out)
	}
}

func TestSessionGroupCmd_ReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `unknown axis "colour" in group_by`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	err := runCmd(t, newSessionCmd(), "list", "--group-by", "colour")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v, want HTTP 400", err)
	}
}
