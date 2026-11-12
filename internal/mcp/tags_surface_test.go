package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// tagSurfaceCall is one request a tag tool sent to the fake REST server.
type tagSurfaceCall struct {
	method string
	path   string
	query  string
	body   map[string]any
}

// newTagSurfaceServer returns an MCP Server whose loopback REST port is a fake
// that records every request and answers 200 with {"ok":true}.
func newTagSurfaceServer(t *testing.T) (*Server, *[]tagSurfaceCall) {
	t.Helper()
	var calls []tagSurfaceCall
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := tagSurfaceCall{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &c.body)
		}
		calls = append(calls, c)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(rest.Close)
	_, portStr, _ := net.SplitHostPort(rest.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return &Server{webPort: port}, &calls
}

func TestTagListCountsSendsCountsQuery(t *testing.T) {
	cases := []struct {
		name  string
		args  map[string]any
		query string
	}{
		{"plain", nil, ""},
		{"counts", map[string]any{"counts": true}, "counts=true"},
		{"counts-and-sub", map[string]any{"counts": true, "include_sub": true}, "counts=true&include_sub=true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, calls := newTagSurfaceServer(t)
			res, err := s.handleAutonomousTagList(context.Background(), tagReq(tc.args))
			if err != nil || res.IsError {
				t.Fatalf("tag_list: %v %v", err, res)
			}
			want := tagSurfaceCall{method: http.MethodGet, path: "/api/autonomous/tags", query: tc.query}
			if len(*calls) != 1 || (*calls)[0].method != want.method || (*calls)[0].path != want.path || (*calls)[0].query != want.query {
				t.Fatalf("calls = %+v, want one %+v", *calls, want)
			}
		})
	}
}

func TestTagPRDSetTagsSendsTagsAndPrimary(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		body map[string]any
	}{
		{"tags-only", map[string]any{"id": "prd1", "tag_ids_json": `["aa11","bb22"]`},
			map[string]any{"tag_ids": []any{"aa11", "bb22"}}},
		{"tags-and-primary", map[string]any{"id": "prd1", "tag_ids_json": `["aa11"]`, "primary_tag_id": "aa11"},
			map[string]any{"tag_ids": []any{"aa11"}, "primary_tag_id": "aa11"}},
		{"primary-only", map[string]any{"id": "prd1", "primary_tag_id": "bb22"},
			map[string]any{"primary_tag_id": "bb22"}},
		{"primary-cleared", map[string]any{"id": "prd1", "tag_ids_json": `[]`, "primary_tag_id": ""},
			map[string]any{"tag_ids": []any{}, "primary_tag_id": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, calls := newTagSurfaceServer(t)
			res, err := s.handleAutonomousPRDSetTags(context.Background(), tagReq(tc.args))
			if err != nil || res.IsError {
				t.Fatalf("set_tags: %v %v", err, res)
			}
			if len(*calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(*calls))
			}
			c := (*calls)[0]
			if c.method != http.MethodPost || c.path != "/api/autonomous/prds/prd1/set_tags" {
				t.Errorf("route = %s %s", c.method, c.path)
			}
			if !reflect.DeepEqual(c.body, tc.body) {
				t.Errorf("body = %v, want %v", c.body, tc.body)
			}
		})
	}
}

func TestTagPRDSetTagsNeedsTagsOrPrimary(t *testing.T) {
	s, calls := newTagSurfaceServer(t)
	res, err := s.handleAutonomousPRDSetTags(context.Background(), tagReq(map[string]any{"id": "prd1"}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected an error result when neither tag_ids_json nor primary_tag_id is given")
	}
	if len(*calls) != 0 {
		t.Errorf("sent %d requests, want none", len(*calls))
	}
}

func TestTagSessionSetTagsSendsIDAndTags(t *testing.T) {
	cases := []struct {
		name string
		json string
		want map[string]any
	}{
		{"set", `["aa11","bb22"]`, map[string]any{"id": "sess1", "tag_ids": []any{"aa11", "bb22"}}},
		{"clear", `[]`, map[string]any{"id": "sess1", "tag_ids": []any{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, calls := newTagSurfaceServer(t)
			res, err := s.handleSessionSetTags(context.Background(),
				tagReq(map[string]any{"id": "sess1", "tag_ids_json": tc.json}))
			if err != nil || res.IsError {
				t.Fatalf("session_set_tags: %v %v", err, res)
			}
			if len(*calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(*calls))
			}
			c := (*calls)[0]
			if c.method != http.MethodPost || c.path != "/api/sessions/tags" {
				t.Errorf("route = %s %s", c.method, c.path)
			}
			if !reflect.DeepEqual(c.body, tc.want) {
				t.Errorf("body = %v, want %v", c.body, tc.want)
			}
		})
	}
}

func TestTagAttentionListSendsGET(t *testing.T) {
	s, calls := newTagSurfaceServer(t)
	res, err := s.handleAutonomousAttentionList(context.Background(), tagReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("attention_list: %v %v", err, res)
	}
	if len(*calls) != 1 || (*calls)[0].method != http.MethodGet || (*calls)[0].path != "/api/autonomous/attention" || (*calls)[0].query != "" {
		t.Fatalf("calls = %+v, want one GET /api/autonomous/attention", *calls)
	}
}

func TestTagSurfaceToolDefinitions(t *testing.T) {
	s := &Server{}
	for name, tool := range map[string]mcpsdk.Tool{
		"autonomous_attention_list": s.toolAutonomousAttentionList(),
		"session_set_tags":          s.toolSessionSetTags(),
	} {
		if tool.Name != name {
			t.Errorf("tool name = %q, want %q", tool.Name, name)
		}
	}
	if _, ok := s.toolAutonomousTagList().InputSchema.Properties["counts"]; !ok {
		t.Errorf("autonomous_tag_list has no counts property")
	}
	if _, ok := s.toolAutonomousPRDSetTags().InputSchema.Properties["primary_tag_id"]; !ok {
		t.Errorf("autonomous_prd_set_tags has no primary_tag_id property")
	}
}
