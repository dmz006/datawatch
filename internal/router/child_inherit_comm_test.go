// `autonomous create ... --parent <id>` over the comm channel sends the same
// parent_prd_id body field the REST, MCP and CLI surfaces send.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func childInheritCommDaemon(t *testing.T, posts *[]map[string]any) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/autonomous/prds" {
			raw, _ := io.ReadAll(r.Body)
			var b map[string]any
			_ = json.Unmarshal(raw, &b)
			*posts = append(*posts, b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1"}`))
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestChildInheritParity_Comm_CreateForwardsParent(t *testing.T) {
	for _, msg := range []string{
		"autonomous create tidy the docs --parent p1",
		"autonomous create --parent p1 tidy the docs",
		"autonomous create tidy the docs --parent=p1",
	} {
		var posts []map[string]any
		port := childInheritCommDaemon(t, &posts)
		r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
		r.SetWebPort(port)
		r.HandleTestMessage(msg)
		if len(posts) != 1 {
			t.Fatalf("%q: want one POST, got %d", msg, len(posts))
		}
		if posts[0]["parent_prd_id"] != "p1" || posts[0]["spec"] != "tidy the docs" {
			t.Fatalf("%q: body = %v", msg, posts[0])
		}
	}
}

func TestChildInheritParity_Comm_NoParentOmitsField(t *testing.T) {
	var posts []map[string]any
	port := childInheritCommDaemon(t, &posts)
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	r.HandleTestMessage("autonomous create tidy the docs")
	if len(posts) != 1 {
		t.Fatalf("want one POST, got %d", len(posts))
	}
	if _, ok := posts[0]["parent_prd_id"]; ok {
		t.Fatalf("parent_prd_id must be omitted: %v", posts[0])
	}
}

func TestChildInheritParity_Comm_ParentWithoutValueIsUsageError(t *testing.T) {
	var posts []map[string]any
	port := childInheritCommDaemon(t, &posts)
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	replies := r.HandleTestMessage("autonomous create tidy the docs --parent")
	if len(posts) != 0 {
		t.Fatalf("nothing should be sent, got %v", posts)
	}
	if !strings.Contains(strings.Join(replies, "\n"), "--parent <automaton-id>") {
		t.Fatalf("reply should show the usage line: %q", replies)
	}
}

func TestChildInheritParity_Comm_HelpNamesParent(t *testing.T) {
	if !strings.Contains(autonomousCreateUsage, "--parent") {
		t.Fatal("usage line must name --parent")
	}
}
