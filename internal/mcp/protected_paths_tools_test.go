package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// protectedPathsMCPSample is the protected-paths status body for the read test:
// one task held on two protected paths and one Automaton that authorises a path.
const protectedPathsMCPSample = `{"action":"hold","rules_source":"base_commit","paths":["AGENT.md","docs/plans/"],` +
	`"held":[{"automaton_id":"abcd1234","automaton_title":"Docs refresh","task_id":"t7","task_title":"Rewrite the intro",` +
	`"paths":["AGENT.md","docs/plans/a.md"],"action":"hold","reason":"the commit changed protected paths",` +
	`"question_id":"q42","since":"2026-11-01T09:00:00Z","awaiting_spec_change":false}],` +
	`"authorized":[{"automaton_id":"ef567890","automaton_title":"Release notes","paths":["CHANGELOG.md"]}]}`

// protectedPathsRecorder is a loopback daemon double that records every request
// and answers with one fixed status and body.
type protectedPathsRecorder struct {
	mu                  sync.Mutex
	calls               int
	method, path, query string
	body                string
}

func (p *protectedPathsRecorder) snapshot() (calls int, method, path, query, body string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.method, p.path, p.query, p.body
}

func protectedPathsServer(t *testing.T, status int, reply string) (*Server, *protectedPathsRecorder) {
	t.Helper()
	rec := &protectedPathsRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.calls++
		rec.method, rec.path, rec.query, rec.body = r.Method, r.URL.Path, r.URL.RawQuery, string(raw)
		rec.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port}, rec
}

func TestProtectedPathsMCP(t *testing.T) {
	t.Run("schema", func(t *testing.T) {
		have := map[string]bool{}
		for _, n := range mergeTestToolNames(t) {
			have[n] = true
		}
		for _, want := range ProtectedPathsToolNames() {
			if !have[want] {
				t.Errorf("tool %q is not registered", want)
			}
		}

		read := (&Server{}).toolAutonomousProtectedPaths()
		if read.Name != "autonomous_protected_paths" {
			t.Errorf("read tool name = %q", read.Name)
		}
		if hint := read.Annotations.ReadOnlyHint; hint == nil || !*hint {
			t.Error("autonomous_protected_paths must be marked read-only")
		}
		if _, ok := read.InputSchema.Properties["automaton"]; !ok {
			t.Error("autonomous_protected_paths does not declare the automaton argument")
		}
		if len(read.InputSchema.Required) != 0 {
			t.Errorf("autonomous_protected_paths requires %v, want no required argument", read.InputSchema.Required)
		}
		for _, field := range []string{"action", "rules_source", "paths", "held", "authorized", "automaton_id",
			"task_id", "question_id", "since", "awaiting_spec_change"} {
			if !strings.Contains(read.Description, `"`+field+`"`) {
				t.Errorf("read description does not name %q", field)
			}
		}

		set := (&Server{}).toolAutonomousPRDSetAuthorizedPaths()
		if set.Name != "autonomous_prd_set_authorized_paths" {
			t.Errorf("set tool name = %q", set.Name)
		}
		if hint := set.Annotations.ReadOnlyHint; hint != nil && *hint {
			t.Error("autonomous_prd_set_authorized_paths writes, it must not be marked read-only")
		}
		for _, arg := range []string{"id", "paths"} {
			if _, ok := set.InputSchema.Properties[arg]; !ok {
				t.Errorf("set tool does not declare %q", arg)
			}
		}
		required := append([]string(nil), set.InputSchema.Required...)
		sort.Strings(required)
		if !reflect.DeepEqual(required, []string{"id", "paths"}) {
			t.Errorf("set tool requires %v, want id and paths", required)
		}
		if paths, _ := set.InputSchema.Properties["paths"].(map[string]any); paths["type"] != "array" {
			t.Errorf("paths schema = %v, want an array", paths)
		}
		// Every sentence an operator reads says Automaton, never PRD.
		for name, text := range map[string]string{"read": read.Description, "set": set.Description} {
			if !strings.Contains(text, "Automaton") {
				t.Errorf("%s description does not say Automaton", name)
			}
			if strings.Contains(text, "PRD") {
				t.Errorf("%s description says PRD: %s", name, text)
			}
		}
	})

	t.Run("read result", func(t *testing.T) {
		s, rec := protectedPathsServer(t, http.StatusOK, protectedPathsMCPSample)
		res, err := s.handleAutonomousProtectedPaths(context.Background(), callReq(nil))
		if err != nil || res == nil || res.IsError {
			t.Fatalf("err=%v res=%+v", err, res)
		}
		calls, method, path, query, _ := rec.snapshot()
		if calls != 1 || method != http.MethodGet || path != "/api/autonomous/protected-paths" || query != "" {
			t.Errorf("proxied %d x %s %s?%s; want one GET of the route with no filter", calls, method, path, query)
		}
		text := automationText(t, res)
		if text != protectedPathsMCPSample {
			t.Errorf("the result must be the daemon body untouched\ngot:  %s\nwant: %s", text, protectedPathsMCPSample)
		}
		for _, want := range []string{`"question_id":"q42"`, `"rules_source":"base_commit"`, `"authorized":[`} {
			if !strings.Contains(text, want) {
				t.Errorf("result lost %s: %s", want, text)
			}
		}

		// An Automaton ID narrows the read; a blank one is no filter.
		if _, err := s.handleAutonomousProtectedPaths(context.Background(), callReq(map[string]any{"automaton": "abcd1234"})); err != nil {
			t.Fatal(err)
		}
		if _, _, _, query, _ = rec.snapshot(); query != "automaton=abcd1234" {
			t.Errorf("query = %q, want automaton=abcd1234", query)
		}
		if _, err := s.handleAutonomousProtectedPaths(context.Background(), callReq(map[string]any{"automaton": "  "})); err != nil {
			t.Fatal(err)
		}
		if _, _, _, query, _ = rec.snapshot(); query != "" {
			t.Errorf("query = %q, want no filter for a blank automaton", query)
		}
	})

	t.Run("read error", func(t *testing.T) {
		s, _ := protectedPathsServer(t, http.StatusServiceUnavailable, "autonomous disabled")
		res, err := s.handleAutonomousProtectedPaths(context.Background(), callReq(nil))
		if err != nil {
			t.Fatalf("a refusal must come back as a tool result, not a protocol error: %v", err)
		}
		if res == nil || !res.IsError {
			t.Fatalf("a 503 from the daemon must come back as a tool error: %+v", res)
		}
		if text := automationText(t, res); !strings.Contains(text, "autonomous disabled") {
			t.Errorf("refusal text lost: %s", text)
		}
	})

	t.Run("set", func(t *testing.T) {
		const reply = `{"id":"abcd1234","title":"Docs refresh","authorized_paths":["AGENT.md","docs/plans/"]}`
		s, rec := protectedPathsServer(t, http.StatusOK, reply)
		res, err := s.handleAutonomousPRDSetAuthorizedPaths(context.Background(), callReq(map[string]any{
			"id":    "abcd1234",
			"paths": []any{"docs/plans/", "AGENT.md"},
		}))
		if err != nil || res == nil || res.IsError {
			t.Fatalf("err=%v res=%+v", err, res)
		}
		calls, method, path, _, body := rec.snapshot()
		if calls != 1 || method != http.MethodPost || path != "/api/autonomous/prds/abcd1234/set_authorized_paths" {
			t.Errorf("proxied %d x %s %s; want one POST of set_authorized_paths", calls, method, path)
		}
		var sent struct {
			Paths []string `json:"paths"`
		}
		if err := json.Unmarshal([]byte(body), &sent); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if !reflect.DeepEqual(sent.Paths, []string{"docs/plans/", "AGENT.md"}) {
			t.Errorf("sent paths = %v, want the list as given, in order", sent.Paths)
		}
		if text := automationText(t, res); text != reply {
			t.Errorf("result = %s, want the updated Automaton from the daemon", text)
		}

		// A typed []string argument is accepted too.
		if _, err := s.handleAutonomousPRDSetAuthorizedPaths(context.Background(), callReq(map[string]any{
			"id": "abcd1234", "paths": []string{"x/"},
		})); err != nil {
			t.Fatal(err)
		}
		if _, _, _, _, body = rec.snapshot(); body != `{"paths":["x/"]}` {
			t.Errorf("body = %s, want the typed list", body)
		}
	})

	t.Run("clear", func(t *testing.T) {
		s, rec := protectedPathsServer(t, http.StatusOK, `{"id":"abcd1234","title":"Docs refresh"}`)
		for name, paths := range map[string]any{"empty any list": []any{}, "empty string list": []string{}} {
			res, err := s.handleAutonomousPRDSetAuthorizedPaths(context.Background(), callReq(map[string]any{
				"id": "abcd1234", "paths": paths,
			}))
			if err != nil || res == nil || res.IsError {
				t.Fatalf("%s: err=%v res=%+v", name, err, res)
			}
			// An empty array clears the list, and goes on the wire as [] not null.
			if _, _, _, _, body := rec.snapshot(); body != `{"paths":[]}` {
				t.Errorf("%s: body = %s, want {\"paths\":[]}", name, body)
			}
		}
	})

	t.Run("bad input", func(t *testing.T) {
		cases := []struct {
			name string
			args map[string]any
			want string
		}{
			{"missing id", map[string]any{"paths": []any{"a/"}}, "id is required"},
			{"blank id", map[string]any{"id": "  ", "paths": []any{"a/"}}, "id is required"},
			{"id with a slash", map[string]any{"id": "ab/cd", "paths": []any{"a/"}}, "is not an Automaton ID"},
			{"id with a query", map[string]any{"id": "abcd?x=1", "paths": []any{"a/"}}, "is not an Automaton ID"},
			{"missing paths", map[string]any{"id": "abcd1234"}, "paths is required"},
			{"null paths", map[string]any{"id": "abcd1234", "paths": nil}, "paths is required"},
			{"paths is a string", map[string]any{"id": "abcd1234", "paths": "docs/plans/"}, "array of strings"},
			{"paths holds a number", map[string]any{"id": "abcd1234", "paths": []any{"a/", float64(3)}}, "array of strings"},
			{"paths holds an object", map[string]any{"id": "abcd1234", "paths": []any{map[string]any{"p": "a/"}}}, "array of strings"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				s, rec := protectedPathsServer(t, http.StatusOK, `{}`)
				res, err := s.handleAutonomousPRDSetAuthorizedPaths(context.Background(), callReq(tc.args))
				if err != nil {
					t.Fatalf("bad input must come back as a tool result, not a protocol error: %v", err)
				}
				if res == nil || !res.IsError {
					t.Fatalf("want a tool error, got %+v", res)
				}
				if text := automationText(t, res); !strings.Contains(text, tc.want) {
					t.Errorf("error = %q, want it to contain %q", text, tc.want)
				}
				if calls, _, _, _, _ := rec.snapshot(); calls != 0 {
					t.Errorf("bad input must not reach the daemon, got %d call(s)", calls)
				}
			})
		}

		// A refusal from the daemon (an absolute path) and an unknown Automaton
		// both come back as tool errors carrying the daemon's text.
		for _, tc := range []struct {
			status int
			reply  string
			want   string
		}{
			{http.StatusBadRequest, "autonomous.automation.protected_paths: entry \"/etc\" is absolute; use a repository-relative path", "is absolute"},
			{http.StatusNotFound, "not found", "HTTP 404"},
		} {
			s, _ := protectedPathsServer(t, tc.status, tc.reply)
			res, err := s.handleAutonomousPRDSetAuthorizedPaths(context.Background(), callReq(map[string]any{
				"id": "nosuchid", "paths": []any{"/etc"},
			}))
			if err != nil || res == nil || !res.IsError {
				t.Fatalf("status %d: err=%v res=%+v, want a tool error", tc.status, err, res)
			}
			if text := automationText(t, res); !strings.Contains(text, tc.want) {
				t.Errorf("status %d: error = %q, want it to contain %q", tc.status, text, tc.want)
			}
		}
	})

	t.Run("federation capability", func(t *testing.T) {
		for name, want := range map[string]string{
			"autonomous_protected_paths":          federation.CapAutonomousRead,
			"autonomous_prd_set_authorized_paths": federation.CapAutonomousWrite,
		} {
			got, ok := federation.MCPToolCap[name]
			if !ok {
				t.Errorf("%s has no federation capability", name)
				continue
			}
			if got != want {
				t.Errorf("MCPToolCap[%q] = %q, want %q", name, got, want)
			}
		}
	})
}

// The three protected-paths settings are written through the generic
// config_set tool: the key and value pass to PUT /api/config unchanged, so no
// parallel config tool is needed. The double refuses a body that is not valid
// JSON the way the daemon does, which makes config_set retry with a quoted
// string.
func TestProtectedPathsMCPConfigSetIsGeneric(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		want       any
	}{
		{"autonomous.automation.protected_paths", "docs/plans/,AGENT.md", "docs/plans/,AGENT.md"},
		{"autonomous.automation.protected_paths", `["docs/plans/","AGENT.md"]`, []any{"docs/plans/", "AGENT.md"}},
		{"autonomous.automation.protected_paths_action", "block", "block"},
		{"autonomous.automation.rules_review_source", "worktree", "worktree"},
	} {
		var mu sync.Mutex
		var last map[string]any
		var method, path string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			if json.Unmarshal(raw, &body) != nil {
				http.Error(w, "bad JSON", http.StatusBadRequest)
				return
			}
			mu.Lock()
			last, method, path = body, r.Method, r.URL.Path
			mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
		}))
		port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
		s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
		res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{"key": tc.key, "value": tc.value}))
		srv.Close()
		if err != nil || res == nil || res.IsError {
			t.Fatalf("%s: err=%v res=%+v", tc.key, err, res)
		}
		mu.Lock()
		if method != http.MethodPut || path != "/api/config" {
			t.Errorf("%s: proxied %s %s, want PUT /api/config", tc.key, method, path)
		}
		if len(last) != 1 || !reflect.DeepEqual(last[tc.key], tc.want) {
			t.Errorf("%s: body = %v, want the one key with %v", tc.key, last, tc.want)
		}
		mu.Unlock()
		if text := automationText(t, res); !strings.Contains(text, "Set "+tc.key) {
			t.Errorf("%s: reply does not confirm the write: %s", tc.key, text)
		}
	}
}
