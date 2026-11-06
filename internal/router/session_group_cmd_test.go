// `sessions groups` — the comm-channel count summary over the grouped
// session-list read.

package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sessionGroupsServer answers /api/sessions with the grouped body and
// records the last raw query.
func sessionGroupsServer(t *testing.T, query *string, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*query = r.URL.RawQuery
		switch r.URL.Path {
		case "/api/sessions":
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const sessionGroupsTagBody = `{
"group_by":"tag","tag_prefix":"automation:","total":4,
"groups":[
 {"key":"(none)","count":2,"state_mix":{"running":1,"complete":1},"sessions":[],"stories":null,"progress":null},
 {"key":"automation:repo-ci-watch","count":2,"state_mix":{"running":1,"failed":1},"sessions":[],"stories":null,"progress":null}],
"state_totals":{"running":2,"complete":1,"failed":1}}`

func TestSessionGroupParse(t *testing.T) {
	for in, want := range map[string]string{
		"sessions groups":                 "",
		"Sessions groups":                 "",
		"sessions groups tag":             "tag",
		"sessions groups automaton":       "automaton",
		"sessions groups tag automation:": "tag automation:",
	} {
		cmd := Parse(in)
		if cmd.Type != CmdSessionGroups || cmd.Text != want {
			t.Errorf("Parse(%q) = {%s %q}, want {session-groups %q}", in, cmd.Type, cmd.Text, want)
		}
	}
	if Parse("sessions group").Type == CmdSessionGroups {
		t.Error("a different verb must not parse as sessions groups")
	}
}

func TestSessionGroupHelpListsVerb(t *testing.T) {
	help := HelpText("h")
	if !strings.Contains(help, "sessions groups [axis] [tag-prefix]") {
		t.Errorf("help lacks the sessions groups verb\nhelp:\n%s", help)
	}
}

func TestSessionGroupQuery_DefaultsToTag(t *testing.T) {
	q, err := parseSessionGroupsArgs("")
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("group_by") != "tag" || q.Has("tag_prefix") {
		t.Errorf("query = %q", q.Encode())
	}
}

func TestSessionGroupQuery_TagPrefix(t *testing.T) {
	q, err := parseSessionGroupsArgs("tag automation:")
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("group_by") != "tag" || q.Get("tag_prefix") != "automation:" {
		t.Errorf("query = %q", q.Encode())
	}
	if _, err := parseSessionGroupsArgs("colour"); err == nil {
		t.Error("unknown axis must be rejected")
	}
	if _, err := parseSessionGroupsArgs("state a b"); err == nil {
		t.Error("too many arguments must be rejected")
	}
}

func TestSessionGroupCommReplyPerGroupCounts(t *testing.T) {
	var query string
	srv := sessionGroupsServer(t, &query, sessionGroupsTagBody)
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleSessionGroups("tag automation:") })

	if query != "group_by=tag&tag_prefix=automation%3A" {
		t.Errorf("query = %q", query)
	}
	for _, want := range []string{
		"automation:repo-ci-watch  2 (failed×1, running×1)",
		"(none)  2 (complete×1, running×1)",
		"totals: complete×1, failed×1, running×2",
		"4 session(s)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q\nreply:\n%s", want, got)
		}
	}
}

func TestSessionGroupCommReplyAutomatonHasProgress(t *testing.T) {
	body := `{
"group_by":"automaton","tag_prefix":"","total":3,
"groups":[
 {"key":"A1","count":2,"state_mix":{"running":1,"complete":1},"sessions":[],"stories":null,"progress":{"done":1,"total":2}},
 {"key":"(unassigned)","count":1,"state_mix":{"failed":1},"sessions":[],"stories":null,"progress":null}],
"state_totals":{"running":1,"complete":1,"failed":1}}`
	var query string
	srv := sessionGroupsServer(t, &query, body)
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleSessionGroups("automaton") })
	if query != "group_by=automaton" {
		t.Errorf("query = %q", query)
	}
	if !strings.Contains(got, "A1  2 (complete×1, running×1)  [done 1/2]") {
		t.Errorf("reply lacks the progress count\nreply:\n%s", got)
	}
	if !strings.Contains(got, "(unassigned)  1 (failed×1)") {
		t.Errorf("reply lacks the ungrouped group\nreply:\n%s", got)
	}
}

func TestSessionGroupCommBadAxisRepliesUsage(t *testing.T) {
	var query string
	srv := sessionGroupsServer(t, &query, sessionGroupsTagBody)
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleSessionGroups("colour") })
	if !strings.Contains(got, `unknown axis "colour"`) || !strings.Contains(got, "usage: sessions groups") {
		t.Errorf("reply = %q", got)
	}
	if query != "" {
		t.Errorf("no HTTP call should be made for a bad axis, query = %q", query)
	}
}

func TestSessionGroupCommHTTPErrorSurfaces(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleSessionGroups("tag") })
	if !strings.Contains(got, "sessions groups failed") {
		t.Errorf("reply = %q", got)
	}
}
