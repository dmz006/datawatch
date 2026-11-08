// Tag filter and tags listing on the session CLI: the axis shortcut flags
// (--project, --automaton, --automation, --stage, --kind, --role) map to
// <axis>:<value> tags under the same --tag-match rule as --tag, and
// `session tags` (no session id) lists the tags sessions carry with counts
// via GET /api/sessions/tags.

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSessionListQuery_AxisShortcuts(t *testing.T) {
	c := sessionListSubcmd(t)
	if err := c.ParseFlags([]string{
		"--project", "web", "--stage", "task", "--kind", "feature",
		"--role", "gpu", "--automaton", "prd1", "--automation", "auto1",
	}); err != nil {
		t.Fatal(err)
	}
	q := sessionListQuery(c)
	want := url.Values{"tag": {
		"project:web", "automaton:prd1", "automation:auto1", "stage:task", "kind:feature", "role:gpu",
	}}
	if q.Encode() != want.Encode() {
		t.Errorf("query = %q, want %q", q.Encode(), want.Encode())
	}
}

// Axis shortcuts combine with --tag and the --tag-match rule.
func TestSessionListQuery_AxisShortcutsCombineWithTagAndMatch(t *testing.T) {
	c := sessionListSubcmd(t)
	if err := c.ParseFlags([]string{"--tag", "lane:cpu", "--project", " web ", "--tag-match", "all"}); err != nil {
		t.Fatal(err)
	}
	q := sessionListQuery(c)
	want := url.Values{"tag": {"lane:cpu", "project:web"}, "tag_match": {"all"}}
	if q.Encode() != want.Encode() {
		t.Errorf("query = %q, want %q", q.Encode(), want.Encode())
	}
}

func TestSessionListCmd_AxisShortcutsHitAPI(t *testing.T) {
	seen := recordingDaemon(t, `[]`)
	if err := runCmd(t, newSessionCmd(), "list", "--project", "web", "--stage", "task"); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("want 1 request, got %d", len(*seen))
	}
	u := (*seen)[0]
	if u.Path != "/api/sessions" {
		t.Fatalf("path = %q, want /api/sessions", u.Path)
	}
	got := u.Query()["tag"]
	want := []string{"project:web", "stage:task"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("tag = %q, want %q", got, want)
	}
}

// `session tags` with no args lists the tag inventory through the REST
// endpoint and prints the tag names with their counts.
func TestSessionTagsCmd_ListsTagsWithCounts(t *testing.T) {
	seen := recordingDaemon(t, `{"tags":[{"tag":"lane:cpu","axis":"lane","value":"cpu","count":3},{"tag":"kind:feature","axis":"kind","value":"feature","count":1}]}`)
	out := captureStdout(t, func() {
		if err := runCmd(t, newSessionCmd(), "tags"); err != nil {
			t.Fatal(err)
		}
	})
	if len(*seen) != 1 {
		t.Fatalf("want 1 request, got %d", len(*seen))
	}
	if u := (*seen)[0]; u.Path != "/api/sessions/tags" {
		t.Errorf("path = %q, want /api/sessions/tags", u.Path)
	}
	for _, want := range []string{"lane:cpu", "3", "kind:feature", "1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSessionTagsCmd_EmptyInventory(t *testing.T) {
	seen := recordingDaemon(t, `{"tags":[]}`)
	out := captureStdout(t, func() {
		if err := runCmd(t, newSessionCmd(), "tags"); err != nil {
			t.Fatal(err)
		}
	})
	if u := (*seen)[0]; u.Path != "/api/sessions/tags" {
		t.Errorf("path = %q, want /api/sessions/tags", u.Path)
	}
	if !strings.Contains(out, "no tags") {
		t.Errorf("output = %q, want a no-tags note", out)
	}
}

func TestSessionTagsCmd_JSONPrintsRawReply(t *testing.T) {
	recordingDaemon(t, `{"tags":[{"tag":"role:gpu","axis":"role","value":"gpu","count":2}]}`)
	out := captureStdout(t, func() {
		if err := runCmd(t, newSessionCmd(), "tags", "--json"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, `"role:gpu"`) || !strings.Contains(out, `"count":2`) {
		t.Errorf("output = %q, want the raw JSON reply", out)
	}
}

// `session tags <session-id>` stays the mutation: without --tags it refuses
// instead of silently clearing, and with --tags it posts the assignment with
// the parsed id list.
func TestSessionTagsCmd_StillReplacesSessionTags(t *testing.T) {
	seen := recordingDaemon(t, `{"ok":true}`)
	if err := runCmd(t, newSessionCmd(), "tags", "sess1"); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 0 {
		t.Errorf("session tags <id> without --tags sent %d request(s), want none", len(*seen))
	}

	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })

	if err := runCmd(t, newSessionCmd(), "tags", "sess1", "--tags", "t1,t2"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"tag_ids":["t1","t2"]`) {
		t.Errorf("body = %s", body)
	}
}
