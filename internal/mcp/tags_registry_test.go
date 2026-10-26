package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// newTagRegistryReplyServer returns an MCP Server whose loopback REST port
// answers every request with the given body and records the requests.
func newTagRegistryReplyServer(t *testing.T, body string) (*Server, *[]tagSurfaceCall) {
	t.Helper()
	var calls []tagSurfaceCall
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := tagSurfaceCall{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &c.body)
		}
		calls = append(calls, c)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(rest.Close)
	_, portStr, _ := net.SplitHostPort(rest.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return &Server{webPort: port}, &calls
}

func TestTagRegistryMCPStatusReturnsRegistryObject(t *testing.T) {
	reply := `{"tags":[{"id":"aa11","name":"ops"}],"untagged":{"automata":0},` +
		`"registry":{"role":"peer","keeper":"hub","stale":false,"last_synced_at":null,` +
		`"revision":7,"pending_sync_count":2,` +
		`"notes":[{"id":"n-1","kind":"merged","name":"deploy","kept_id":"aa11","replaced_id":"bb22"}],` +
		`"last_error":""}}`
	s, calls := newTagRegistryReplyServer(t, reply)

	res, err := s.handleAutonomousTagRegistryStatus(context.Background(), tagReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("registry_status: %v %v", err, res)
	}
	if len(*calls) != 1 || (*calls)[0].method != http.MethodGet || (*calls)[0].path != "/api/autonomous/tags" || (*calls)[0].query != "" {
		t.Fatalf("calls = %+v, want one plain GET /api/autonomous/tags", *calls)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(tagResultText(t, res)), &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if _, ok := got["tags"]; ok {
		t.Errorf("result carries the tag list; want the registry object only: %v", got)
	}
	if got["role"] != "peer" || got["keeper"] != "hub" || got["revision"] != float64(7) || got["pending_sync_count"] != float64(2) {
		t.Errorf("registry fields = %v", got)
	}
	notes, _ := got["notes"].([]any)
	if len(notes) != 1 {
		t.Errorf("notes = %v, want one note", got["notes"])
	}
}

func TestTagRegistryMCPStatusRefusesReplyWithoutRegistry(t *testing.T) {
	s, _ := newTagRegistryReplyServer(t, `{"tags":[]}`)
	res, err := s.handleAutonomousTagRegistryStatus(context.Background(), tagReq(nil))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("result is not an error: %v", tagResultText(t, res))
	}
}

func TestTagRegistryMCPDismissNoteCallsRoute(t *testing.T) {
	s, calls := newTagSurfaceServer(t)
	res, err := s.handleAutonomousTagRegistryDismissNote(context.Background(), tagReq(map[string]any{"id": "note-7f"}))
	if err != nil || res.IsError {
		t.Fatalf("dismiss_note: %v %v", err, res)
	}
	want := tagSurfaceCall{method: http.MethodPost, path: "/api/autonomous/tags/registry/notes/note-7f/dismiss"}
	if len(*calls) != 1 || (*calls)[0].method != want.method || (*calls)[0].path != want.path || (*calls)[0].query != "" {
		t.Fatalf("calls = %+v, want one %+v", *calls, want)
	}
	if len((*calls)[0].body) != 0 {
		t.Errorf("dismiss sent a body: %v", (*calls)[0].body)
	}
}

func TestTagRegistryMCPToolDefinitions(t *testing.T) {
	s := &Server{}
	status := s.toolAutonomousTagRegistryStatus()
	if status.Name != "autonomous_tag_registry_status" {
		t.Errorf("status tool name = %q", status.Name)
	}
	dismiss := s.toolAutonomousTagRegistryDismissNote()
	if dismiss.Name != "autonomous_tag_registry_dismiss_note" {
		t.Errorf("dismiss tool name = %q", dismiss.Name)
	}
	if _, ok := dismiss.InputSchema.Properties["id"]; !ok {
		t.Errorf("dismiss tool has no id property")
	}
}
