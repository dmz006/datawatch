// Tests for the comm verbs "autonomous tag-registry" and
// "autonomous tag-registry-dismiss <note-id>": the request each verb builds,
// the usage refusals, the registry-only reply and the dispatch through the
// router to a fake REST server.

package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestTagRegistryCommBuildsStatusRequest(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"status", []string{"tag-registry"}},
		{"status-underscore", []string{"tag_registry"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, usage := buildCommTagRequest(tc.args[0], tc.args)
			if usage != "" {
				t.Fatalf("unexpected usage: %s", usage)
			}
			if req.method != http.MethodGet || req.path != "/api/autonomous/tags" {
				t.Errorf("route = %s %s, want GET /api/autonomous/tags", req.method, req.path)
			}
			if req.body != nil {
				t.Errorf("body = %v, want none", req.body)
			}
			if !req.registryOnly {
				t.Errorf("registryOnly = false, want the reply trimmed to the registry object")
			}
		})
	}
}

func TestTagRegistryCommDismissRequest(t *testing.T) {
	cases := []struct {
		name string
		args []string
		path string
	}{
		{"dismiss", []string{"tag-registry-dismiss", "note-7f"}, "/api/autonomous/tags/registry/notes/note-7f/dismiss"},
		{"dismiss-underscore", []string{"tag_registry_dismiss", "n1"}, "/api/autonomous/tags/registry/notes/n1/dismiss"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, usage := buildCommTagRequest(tc.args[0], tc.args)
			if usage != "" {
				t.Fatalf("unexpected usage: %s", usage)
			}
			if req.method != http.MethodPost || req.path != tc.path {
				t.Errorf("route = %s %s, want POST %s", req.method, req.path, tc.path)
			}
			if req.body != nil || req.registryOnly {
				t.Errorf("body = %v, registryOnly = %v, want a bare POST", req.body, req.registryOnly)
			}
		})
	}
}

func TestTagRegistryCommUsageRefusals(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"tag-registry", "extra"}, "usage: autonomous tag-registry"},
		{[]string{"tag-registry-dismiss"}, "usage: autonomous tag-registry-dismiss <note-id>"},
		{[]string{"tag-registry-dismiss", "a", "b"}, "usage: autonomous tag-registry-dismiss <note-id>"},
	}
	for _, tc := range cases {
		if _, usage := buildCommTagRequest(tc.args[0], tc.args); usage != tc.want {
			t.Errorf("%v: usage = %q, want %q", tc.args, usage, tc.want)
		}
	}
}

func TestTagRegistryCommRegistryObjectFromReply(t *testing.T) {
	reply := `{"tags":[{"id":"aa11"}],"registry":{"role":"peer","revision":4,"notes":[]}}`
	got, err := registryObjectFromTagsReply([]byte(reply))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if want := `{"role":"peer","revision":4,"notes":[]}`; string(got) != want {
		t.Errorf("registry = %s, want %s", got, want)
	}
	if _, err := registryObjectFromTagsReply([]byte(`{"tags":[]}`)); err == nil {
		t.Errorf("reply without a registry object was accepted")
	}
}

func TestTagRegistryCommDispatch(t *testing.T) {
	type seen struct{ method, path, body string }
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method, r.URL.Path, string(b)})
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"tags":[{"id":"aa11"}],"registry":{"role":"keeper","revision":9}}`))
			return
		}
		_, _ = w.Write([]byte(`{"dismissed":"note-7f"}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	r := bareSubsRouter()
	r.SetWebPort(port)

	got = nil
	out := joinReplies(r.HandleTestMessage("autonomous tag-registry"))
	if len(got) != 1 || got[0] != (seen{"GET", "/api/autonomous/tags", ""}) {
		t.Fatalf("status request = %+v", got)
	}
	if !strings.Contains(out, "revision") || strings.Contains(out, `"tags"`) {
		t.Errorf("status reply = %q, want the registry object only", out)
	}

	got = nil
	out = joinReplies(r.HandleTestMessage("autonomous tag-registry-dismiss note-7f"))
	if len(got) != 1 || got[0] != (seen{"POST", "/api/autonomous/tags/registry/notes/note-7f/dismiss", ""}) {
		t.Fatalf("dismiss request = %+v", got)
	}
	if !strings.Contains(out, "dismissed") {
		t.Errorf("dismiss reply = %q", out)
	}

	if out = joinReplies(r.HandleTestMessage("autonomous tag-registry-dismiss")); !strings.Contains(out, "usage") {
		t.Errorf("expected usage, got %q", out)
	}
}

func TestTagRegistryCommHelpMentionsVerbs(t *testing.T) {
	help := joinReplies((&Router{}).HandleTestMessage("autonomous"))
	for _, v := range []string{"tag-registry", "tag-registry-dismiss <note-id>"} {
		if !strings.Contains(help, v) {
			t.Errorf("help misses %q", v)
		}
	}
}
