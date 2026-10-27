package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newTagMCPServer fronts a real autonomous API with a minimal REST stand-in
// for the tag routes (409 on refusals, count in the body) and returns an MCP
// Server pointed at it.
func newTagMCPServer(t *testing.T) (*Server, *autonomous.Manager) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	api := newTestAPI(mgr)
	reply := func(w http.ResponseWriter, v any, err error) {
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			code := http.StatusBadRequest
			body := map[string]any{"error": err.Error()}
			var inUse *autonomous.TagInUseError
			switch {
			case errors.As(err, &inUse):
				code, body["count"] = http.StatusConflict, inUse.Count
			case errors.Is(err, autonomous.ErrTagHasChildren), errors.Is(err, autonomous.ErrTagCycle):
				code = http.StatusConflict
			case errors.Is(err, autonomous.ErrTagNotFound):
				code = http.StatusNotFound
			}
			w.WriteHeader(code)
			v = body
		}
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/tags", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			reply(w, map[string]any{"tags": api.ListTags(r.URL.Query().Get("include_sub") == "true")}, nil)
			return
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		tg, err := api.CreateTag(in["name"], in["parent_id"], in["color"], in["description"])
		reply(w, tg, err)
	})
	mux.HandleFunc("/api/autonomous/tags/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/autonomous/tags/")
		switch r.Method {
		case http.MethodPatch:
			var in map[string]*string
			_ = json.NewDecoder(r.Body).Decode(&in)
			tg, err := api.UpdateTag(id, in["name"], in["parent_id"], in["color"], in["description"])
			reply(w, tg, err)
		case http.MethodDelete:
			reply(w, map[string]any{"deleted": id}, api.DeleteTag(id))
		}
	})
	mux.HandleFunc("/api/autonomous/prds/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/autonomous/prds/"), "/set_tags")
		var in struct {
			TagIDs []string `json:"tag_ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		prd, err := api.SetPRDTags(id, in.TagIDs)
		reply(w, prd, err)
	})
	rest := httptest.NewServer(mux)
	t.Cleanup(rest.Close)
	_, portStr, _ := net.SplitHostPort(rest.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return &Server{webPort: port}, mgr
}

func tagReq(args map[string]any) mcpsdk.CallToolRequest {
	var req mcpsdk.CallToolRequest
	req.Params.Arguments = args
	return req
}

func tagResultText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("empty tool result")
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want TextContent", res.Content[0])
	}
	return tc.Text
}

type mcpTag struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
	Color    string `json:"color"`
	Count    int    `json:"count"`
}

func mcpListTags(t *testing.T, s *Server) map[string]mcpTag {
	t.Helper()
	res, err := s.handleAutonomousTagList(context.Background(), tagReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("tag_list: %v %v", err, res)
	}
	var out struct {
		Tags []mcpTag `json:"tags"`
	}
	if err := json.Unmarshal([]byte(tagResultText(t, res)), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	m := map[string]mcpTag{}
	for _, tg := range out.Tags {
		m[tg.ID] = tg
	}
	return m
}

func mcpCreateTag(t *testing.T, s *Server, args map[string]any) mcpTag {
	t.Helper()
	res, err := s.handleAutonomousTagCreate(context.Background(), tagReq(args))
	if err != nil || res.IsError {
		t.Fatalf("tag_create: %v %v", err, res)
	}
	var tg mcpTag
	if err := json.Unmarshal([]byte(tagResultText(t, res)), &tg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return tg
}

func TestTagToolsRoundTrip(t *testing.T) {
	s, _ := newTagMCPServer(t)
	ctx := context.Background()

	parent := mcpCreateTag(t, s, map[string]any{"name": "ops"})
	child := mcpCreateTag(t, s, map[string]any{"name": "deploy", "parent_id": parent.ID, "color": "#00ff00"})
	if got := mcpListTags(t, s); got[child.ID].ParentID != parent.ID || got[child.ID].Color != "#00ff00" {
		t.Errorf("after create: %+v", got[child.ID])
	}

	if res, _ := s.handleAutonomousTagRename(ctx, tagReq(map[string]any{"id": child.ID, "name": "release"})); res.IsError {
		t.Fatalf("rename: %s", tagResultText(t, res))
	}
	if got := mcpListTags(t, s); got[child.ID].Name != "release" {
		t.Errorf("after rename: %+v", got[child.ID])
	}
	if res, _ := s.handleAutonomousTagSetColor(ctx, tagReq(map[string]any{"id": child.ID, "color": "#0000ff"})); res.IsError {
		t.Fatalf("set_color: %s", tagResultText(t, res))
	}
	if got := mcpListTags(t, s); got[child.ID].Color != "#0000ff" {
		t.Errorf("after set_color: %+v", got[child.ID])
	}
	if res, _ := s.handleAutonomousTagSetParent(ctx, tagReq(map[string]any{"id": child.ID, "parent_id": ""})); res.IsError {
		t.Fatalf("set_parent root: %s", tagResultText(t, res))
	}
	if got := mcpListTags(t, s); got[child.ID].ParentID != "" {
		t.Errorf("after set_parent: %+v", got[child.ID])
	}
	if res, _ := s.handleAutonomousTagDelete(ctx, tagReq(map[string]any{"id": child.ID})); res.IsError {
		t.Fatalf("delete: %s", tagResultText(t, res))
	}
	if got := mcpListTags(t, s); len(got) != 1 {
		t.Errorf("tags after delete = %d, want 1", len(got))
	}
}

func TestTagToolsRefusals(t *testing.T) {
	s, mgr := newTagMCPServer(t)
	ctx := context.Background()

	parent := mcpCreateTag(t, s, map[string]any{"name": "parent"})
	child := mcpCreateTag(t, s, map[string]any{"name": "child", "parent_id": parent.ID})

	// 409: parent has a child.
	res, _ := s.handleAutonomousTagDelete(ctx, tagReq(map[string]any{"id": parent.ID}))
	if !res.IsError || !strings.Contains(tagResultText(t, res), "HTTP 409") {
		t.Fatalf("delete with children = %q, want HTTP 409 error", tagResultText(t, res))
	}
	// 409: cycle.
	res, _ = s.handleAutonomousTagSetParent(ctx, tagReq(map[string]any{"id": parent.ID, "parent_id": child.ID}))
	if !res.IsError || !strings.Contains(tagResultText(t, res), "HTTP 409") {
		t.Fatalf("cycle reparent = %q, want HTTP 409 error", tagResultText(t, res))
	}
	if got := mcpListTags(t, s); got[parent.ID].ParentID != "" || len(got) != 2 {
		t.Errorf("refusals changed state: %+v", got)
	}

	// 409 with count: leaf assigned to an Automaton.
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	res, _ = s.handleAutonomousPRDSetTags(ctx, tagReq(map[string]any{"id": prd.ID, "tag_ids_json": `["` + child.ID + `"]`}))
	if res.IsError {
		t.Fatalf("set_tags: %s", tagResultText(t, res))
	}
	if got := mcpListTags(t, s); got[child.ID].Count != 1 {
		t.Errorf("count after assign = %d, want 1", got[child.ID].Count)
	}
	res, _ = s.handleAutonomousTagDelete(ctx, tagReq(map[string]any{"id": child.ID}))
	txt := tagResultText(t, res)
	if !res.IsError || !strings.Contains(txt, "HTTP 409") || !strings.Contains(txt, `"count":1`) {
		t.Fatalf("delete assigned = %q, want HTTP 409 with count 1", txt)
	}

	// Clear the assignment; delete now succeeds.
	if res, _ = s.handleAutonomousPRDSetTags(ctx, tagReq(map[string]any{"id": prd.ID, "tag_ids_json": `[]`})); res.IsError {
		t.Fatalf("clear tags: %s", tagResultText(t, res))
	}
	if res, _ = s.handleAutonomousTagDelete(ctx, tagReq(map[string]any{"id": child.ID})); res.IsError {
		t.Fatalf("delete after unassign: %s", tagResultText(t, res))
	}
	if got := mcpListTags(t, s); len(got) != 1 {
		t.Errorf("tags = %d, want 1", len(got))
	}
}

func TestTagToolDefinitions(t *testing.T) {
	s := &Server{}
	for name, tool := range map[string]mcpsdk.Tool{
		"autonomous_tag_list":       s.toolAutonomousTagList(),
		"autonomous_tag_create":     s.toolAutonomousTagCreate(),
		"autonomous_tag_rename":     s.toolAutonomousTagRename(),
		"autonomous_tag_set_parent": s.toolAutonomousTagSetParent(),
		"autonomous_tag_set_color":  s.toolAutonomousTagSetColor(),
		"autonomous_tag_delete":     s.toolAutonomousTagDelete(),
		"autonomous_prd_set_tags":   s.toolAutonomousPRDSetTags(),
	} {
		if tool.Name != name {
			t.Errorf("tool name = %q, want %q", tool.Name, name)
		}
	}
}
