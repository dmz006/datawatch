package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
)

const cliReqMarker = "MARKER-ab12-secret-words"

type requestCLIHit struct {
	method, path, surface, auth string
	body                        map[string]any
}

func requestCLIServer(t *testing.T, status int) *[]requestCLIHit {
	t.Helper()
	hits := &[]requestCLIHit{}
	view := delivery.RequestView{Request: delivery.Request{ID: "r-1a2b", Kind: delivery.ReqAsk, Role: delivery.RoleSupervisor,
		Scope: delivery.Scope{Kind: delivery.ScopeProject, ID: "web"}, State: delivery.StateReported}, Label: delivery.LabelNotAccepted}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit := requestCLIHit{method: r.Method, path: r.URL.RequestURI(), surface: r.Header.Get(federation.HeaderRequestSurface), auth: r.Header.Get("Authorization")}
		if data, _ := io.ReadAll(r.Body); len(data) > 0 {
			_ = json.Unmarshal(data, &hit.body)
		}
		*hits = append(*hits, hit)
		if status != 0 {
			http.Error(w, delivery.ErrNotHolder.Error(), status)
			return
		}
		var out any = view
		switch {
		case r.URL.Path == "/api/config":
			out = map[string]any{"delivery_requests": map[string]any{"request_take_by_seconds": 3600, "request_due_seconds": 86400,
				"request_due_seconds_by_kind": map[string]any{"bug": 60, "ask": 120}, "reminder_interval_seconds": 900,
				"reminders_per_session_hour": 2, "reminders_per_host_hour": 0}}
		case r.Method == http.MethodGet && r.URL.Path == "/api/requests" && r.URL.Query().Get("id") == "":
			out = map[string]any{"requests": []delivery.RequestView{view}}
		case r.URL.Path == "/api/requests/roles":
			out = map[string]any{"roles": []delivery.RoleBinding{}}
		case r.URL.Path == "/api/requests/roles/give":
			out = delivery.RoleBinding{Role: delivery.RoleSupervisor, Scope: view.Scope, SessionID: "h-s1"}
		case r.URL.Path == "/api/requests/roles/release":
			out = map[string]any{"ok": true}
		case r.URL.Path == "/api/requests/mine":
			out = delivery.MineReply{SessionID: "h-s1", Items: []delivery.RequestView{view}}
		case r.Method == http.MethodPost && (r.URL.Path == "/api/requests" || r.URL.Path == "/api/requests/from-backlog"):
			out = map[string]any{"request": view, "created": true}
		case strings.HasSuffix(r.URL.Path, "/take"):
			out = delivery.TakeReply{Request: view, Content: "do " + cliReqMarker, Digest: "d1"}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	oldURL, oldName := serverURL, serverName
	serverURL, serverName = srv.URL, ""
	t.Cleanup(func() { serverURL, serverName = oldURL, oldName })
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DATAWATCH_TOKEN", "t")
	return hits
}

func runRequestCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRequestCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return out.String(), err
}

var requestCLICases = []struct {
	args         []string
	method, path string
	body         map[string]any
	out          string
}{
	{[]string{"list"}, "GET", "/api/requests", nil, delivery.LabelNotAccepted},
	{[]string{"list", "--state", "waiting", "--kind", "ask", "--role", "supervisor", "--scope-kind", "project", "--scope", "web", "--holder", "h-s1"},
		"GET", "/api/requests?holder=h-s1&kind=ask&role=supervisor&scope_id=web&scope_kind=project&state=waiting", nil, "r-1a2b"},
	{[]string{"show", "r-1a2b"}, "GET", "/api/requests?id=r-1a2b", nil, "state reported"},
	{[]string{"r-1a2b"}, "GET", "/api/requests?id=r-1a2b", nil, "state reported"},
	{[]string{"mine"}, "GET", "/api/requests/mine", nil, "session h-s1"},
	{[]string{"roles"}, "GET", "/api/requests/roles", nil, "no roles are held"},
	{[]string{"role", "give", "--session", "h-s1", "--scope", "web", "--force", "--reason", "stuck"}, "POST", "/api/requests/roles/give",
		map[string]any{"role": "supervisor", "scope": map[string]any{"kind": "project", "id": "web"}, "session_id": "h-s1", "force": true, "reason_code": "stuck"}, "h-s1"},
	{[]string{"role", "release", "--scope", "web"}, "POST", "/api/requests/roles/release",
		map[string]any{"role": "supervisor", "scope": map[string]any{"kind": "project", "id": "web"}, "session_id": ""}, "released"},
	{[]string{"take", "r-1a2b"}, "POST", "/api/requests/r-1a2b/take", map[string]any{"via": "tool"}, "do " + cliReqMarker},
	{[]string{"accept", "r-1a2b", "--code", "rc-1"}, "POST", "/api/requests/r-1a2b/accept", map[string]any{"code": "rc-1"}, "r-1a2b"},
	{[]string{"report", "r-1a2b", "--text", "done it", "--result-id", "res", "--rules-version", "v1"}, "POST", "/api/requests/r-1a2b/report",
		map[string]any{"text": "done it", "result_id": "res", "rules_version": "v1"}, delivery.LabelNotAccepted},
	{[]string{"decline", "r-1a2b", "--reason", "declined", "--text", "no"}, "POST", "/api/requests/r-1a2b/decline",
		map[string]any{"reason_code": "declined", "text": "no"}, "r-1a2b"},
	{[]string{"reissue", "r-1a2b", "--reason", "again", "--repeat-ok"}, "POST", "/api/requests/r-1a2b/reissue",
		map[string]any{"reason_code": "again", "repeat_ok": true}, "r-1a2b"},
	{[]string{"drop", "r-1a2b", "--reason", "obsolete", "--words", "w"}, "POST", "/api/requests/r-1a2b/drop",
		map[string]any{"reason_code": "obsolete", "words": "w"}, "r-1a2b"},
	{[]string{"force", "r-1a2b", "--reason", "stuck", "--to-session", "h-s2"}, "POST", "/api/requests/r-1a2b/force",
		map[string]any{"reason_code": "stuck", "to_session": "h-s2"}, "may run twice"},
	{[]string{"accept-unproven", "r-1a2b", "--reason", "seen", "--words", "I looked"}, "POST", "/api/requests/r-1a2b/accept-unproven",
		map[string]any{"reason_code": "seen", "words": "I looked"}, "r-1a2b"},
}

func TestReqSurfaceCLICalls(t *testing.T) {
	for _, c := range requestCLICases {
		hits := requestCLIServer(t, 0)
		out, err := runRequestCLI(t, c.args...)
		if err != nil || len(*hits) != 1 {
			t.Errorf("%v: %d calls, %v", c.args, len(*hits), err)
			continue
		}
		hit := (*hits)[0]
		if hit.method != c.method || hit.path != c.path || hit.surface != "cli" || hit.auth != "Bearer t" {
			t.Errorf("%v: %+v", c.args, hit)
		}
		if c.body != nil && !reflect.DeepEqual(hit.body, c.body) {
			t.Errorf("%v: body %v, want %v", c.args, hit.body, c.body)
		}
		if !strings.Contains(out, c.out) {
			t.Errorf("%v: output %q lacks %q", c.args, out, c.out)
		}
		if c.args[0] != "take" && strings.Contains(out, cliReqMarker) {
			t.Errorf("%v: output holds content: %s", c.args, out)
		}
	}
}

func TestReqSurfaceCLICreate(t *testing.T) {
	hits := requestCLIServer(t, 0)
	out, err := runRequestCLI(t, "create", "--scope", "web", "--ask", cliReqMarker, "--detail", "d", "--param", "a=b=c",
		"--items", `[{"statement":"s","method":"confirm"}]`, "--key", "k1", "--take-by", "30m", "--due", "2h", "--after", "r-0",
		"--project-dir", "/p", "--source-type", "bug", "--source-id", "b1", "--host", "elsewhere")
	if err != nil || len(*hits) != 1 {
		t.Fatalf("%v %v", hits, err)
	}
	b := (*hits)[0].body
	want := map[string]any{"ask": cliReqMarker, "detail": "d", "project_dir": "/p", "params": map[string]any{"a": "b=c"},
		"items": []any{map[string]any{"statement": "s", "method": "confirm"}}}
	if !reflect.DeepEqual(b["content"], want) || b["key"] != "operator:k1" || b["kind"] != "ask" || b["role"] != "supervisor" ||
		b["take_by"] != float64(30*60*1e9) || b["due"] != float64(2*3600*1e9) || b["after"] != "r-0" || b["host"] != "elsewhere" {
		t.Errorf("body = %v", b)
	}
	if !strings.Contains(out, "created r-1a2b") || strings.Contains(out, cliReqMarker) {
		t.Errorf("output = %q", out)
	}
	hits = requestCLIServer(t, 0)
	if _, err := runRequestCLI(t, "from-backlog", "task:t1:failed", "--scope", "web", "--ask", "redo"); err != nil ||
		len(*hits) != 1 || (*hits)[0].path != "/api/requests/from-backlog" || (*hits)[0].body["entry_key"] != "task:t1:failed" {
		t.Errorf("from-backlog: %v %v", *hits, err)
	}
}

// A call aimed at another datawatch computer is refused before anything is
// sent.
func TestReqSurfaceCLIOtherServerSendsNothing(t *testing.T) {
	all := [][]string{{"create", "--scope", "web", "--ask", "x"}, {"from-backlog", "task:t1:failed", "--scope", "web"}, {"settings"}}
	for _, c := range requestCLICases {
		all = append(all, c.args)
	}
	for _, args := range all {
		hits := requestCLIServer(t, 0)
		serverName = "other-box"
		_, err := runRequestCLI(t, args...)
		if err == nil || !strings.Contains(err.Error(), delivery.ErrCrossHost.Error()) || len(*hits) != 0 {
			t.Errorf("%v: %v, %d calls", args, err, len(*hits))
		}
	}
	hits := requestCLIServer(t, 0)
	serverName = "local"
	if _, err := runRequestCLI(t, "list"); err != nil || len(*hits) != 1 {
		t.Errorf("--server local: %v", err)
	}
}

func TestReqSurfaceCLIRefusals(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--ask", "x"}, {"create", "--scope", "web", "--param", "novalue"}, {"create", "--scope", "web", "--take-by", "soon"},
		{"create", "--scope", "web", "--items", `[{"statement":"s","command":"rm"}]`},
		{"role", "give", "--scope", "web"}, {"role", "give", "--session", "s"}, {"decline", "r-1"}, {"reissue", "r-1"},
		{"drop", "r-1"}, {"force", "r-1"}, {"accept-unproven", "r-1", "--reason", "seen"}, {"take"}, {"seen", "r-1"}, {"finish"},
	} {
		hits := requestCLIServer(t, 0)
		if _, err := runRequestCLI(t, args...); err == nil || len(*hits) != 0 {
			t.Errorf("%v: err %v, %d calls", args, err, len(*hits))
		}
	}
	requestCLIServer(t, http.StatusForbidden)
	if _, err := runRequestCLI(t, "take", "r-1"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("refusal = %v", err)
	}
}

func TestReqSurfaceCLISettings(t *testing.T) {
	hits := requestCLIServer(t, 0)
	out, err := runRequestCLI(t, "settings")
	if err != nil || len(*hits) != 1 || (*hits)[0].path != "/api/config" {
		t.Fatalf("%v %v", *hits, err)
	}
	for _, want := range []string{"delivery.request_take_by_seconds = 3600", "delivery.request_due_seconds = 86400",
		"delivery.request_due_seconds_by_kind = ask=120,bug=60", "delivery.reminder_interval_seconds = 900",
		"delivery.reminders_per_session_hour = 2", "delivery.reminders_per_host_hour = 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("settings lack %q:\n%s", want, out)
		}
	}
}

func TestReqSurfaceCLIWiring(t *testing.T) {
	cmd := newRequestCmd()
	if cmd.Name() != "request" {
		t.Fatalf("name = %q", cmd.Name())
	}
	for _, n := range []string{"list", "show", "mine", "roles", "role", "create", "from-backlog", "take", "accept", "report",
		"decline", "reissue", "drop", "force", "accept-unproven", "settings"} {
		findSubCommand(t, cmd, n)
	}
	findSubCommand(t, findSubCommand(t, cmd, "role"), "give")
	findSubCommand(t, findSubCommand(t, cmd, "role"), "release")
}
