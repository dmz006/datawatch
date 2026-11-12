package mcp

// Test for the system and axis filters of the autonomous_tag_list tool.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newPlacementTagMCPServer fronts a real autonomous API with a minimal REST
// stand-in that honours the list filters, as the daemon's listing does:
// ?system=true|false keeps the registry-created tags or the operator tags,
// and ?axis=NAME keeps the axis' "<NAME>:<value>" tags plus the bare NAME tag
// when one is held.
func newPlacementTagMCPServer(t *testing.T) (*Server, *autonomous.Manager) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	reply := func(w http.ResponseWriter, v any, err error) {
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			code := http.StatusBadRequest
			if errors.Is(err, autonomous.ErrTagNotFound) {
				code = http.StatusNotFound
			}
			w.WriteHeader(code)
			v = map[string]any{"error": err.Error()}
		}
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/tags", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			reply(w, nil, http.ErrBodyNotAllowed)
			return
		}
		q := r.URL.Query()
		system, axis := q.Get("system"), q.Get("axis")
		list := []any{}
		for _, tag := range mgr.ListTags() {
			if system != "" && (system == "true") != tag.System {
				continue
			}
			if axis != "" && tag.Name != axis && !strings.HasPrefix(tag.Name, axis+":") {
				continue
			}
			list = append(list, tag)
		}
		reply(w, map[string]any{"tags": list}, nil)
	})
	rest := httptest.NewServer(mux)
	t.Cleanup(rest.Close)
	_, portStr, _ := net.SplitHostPort(rest.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return &Server{webPort: port}, mgr
}

// mcpTagNames calls autonomous_tag_list with args and returns the tag names
// in the order the listing gives them.
func mcpTagNames(t *testing.T, s *Server, args map[string]any) []string {
	t.Helper()
	res, err := s.handleAutonomousTagList(context.Background(), tagReq(args))
	if err != nil || res.IsError {
		t.Fatalf("autonomous_tag_list %v: %v %v", args, err, res)
	}
	var out struct {
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := json.Unmarshal([]byte(tagResultText(t, res)), &out); err != nil {
		t.Fatalf("decode tag list: %v", err)
	}
	names := []string{}
	for _, tg := range out.Tags {
		names = append(names, tg.Name)
	}
	sort.Strings(names)
	return names
}

func TestPlacementTags_MCPFilter(t *testing.T) {
	s, mgr := newPlacementTagMCPServer(t)
	if _, err := mgr.EnsureSystemTag("lane:gpu"); err != nil {
		t.Fatalf("EnsureSystemTag: %v", err)
	}
	if _, err := mgr.CreateTag("infra", "", "", ""); err != nil {
		t.Fatalf("CreateTag infra: %v", err)
	}
	if _, err := mgr.CreateTag("lane:cpu", "", "", ""); err != nil {
		t.Fatalf("CreateTag lane:cpu: %v", err)
	}
	ctx := context.Background()

	// system is only passed when the caller gives it, so an empty call keeps
	// every tag.
	cases := []struct {
		name string
		args map[string]any
		want []string
	}{
		{"no filter", map[string]any{}, []string{"infra", "lane:cpu", "lane:gpu"}},
		{"include_sub", map[string]any{"include_sub": true}, []string{"infra", "lane:cpu", "lane:gpu"}},
		{"only the registry-created tags", map[string]any{"system": true}, []string{"lane:gpu"}},
		{"only the operator tags", map[string]any{"system": false}, []string{"infra", "lane:cpu"}},
		{"one axis, both kinds", map[string]any{"axis": "lane"}, []string{"lane:cpu", "lane:gpu"}},
		{"axis + system", map[string]any{"axis": "lane", "system": true}, []string{"lane:gpu"}},
		{"axis + operator only", map[string]any{"axis": "lane", "system": false}, []string{"lane:cpu"}},
		{"axis with no tag", map[string]any{"axis": "compute"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpTagNames(t, s, tc.args); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s: tags %v, want %v", tc.name, got, tc.want)
			}
		})
	}

	// A non-bool system argument is a tool error before any request is sent.
	res, err := s.handleAutonomousTagList(ctx, tagReq(map[string]any{"system": "yes"}))
	if err != nil || !res.IsError {
		t.Fatalf("system=yes: want a tool error, got %v %v", err, res)
	}
}
