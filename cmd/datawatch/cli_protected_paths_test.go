package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// protectedPathsCLISample is a status with one task held on two protected
// paths, a question waiting for an answer, and one Automaton that authorises a
// path in advance.
const protectedPathsCLISample = `{
  "action": "hold",
  "rules_source": "base_commit",
  "paths": ["AGENT.md", "docs/plans/"],
  "held": [
    {
      "automaton_id": "abcd1234",
      "automaton_title": "Docs refresh",
      "task_id": "t7",
      "task_title": "Rewrite the intro",
      "paths": ["AGENT.md", "docs/plans/a.md"],
      "action": "hold",
      "reason": "the commit changed protected paths",
      "question_id": "q42",
      "since": "2026-11-01T09:00:00Z",
      "awaiting_spec_change": false
    }
  ],
  "authorized": [
    {"automaton_id": "ef567890", "automaton_title": "Release notes", "paths": ["CHANGELOG.md"]}
  ]
}`

// protectedPathsDaemon is a loopback daemon double: it answers every request
// with one status and body, records the requests, and points the CLI at itself
// through a temporary HOME. With rejectBadJSON it refuses a body that is not
// valid JSON the way the daemon does.
type protectedPathsDaemon struct {
	mu                  sync.Mutex
	calls               int
	method, path, query string
	body                string
	url                 string // base URL of the double
}

func (d *protectedPathsDaemon) last() (calls int, method, path, query, body string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls, d.method, d.path, d.query, d.body
}

func newProtectedPathsDaemon(t *testing.T, status int, reply string, rejectBadJSON bool) *protectedPathsDaemon {
	t.Helper()
	d := &protectedPathsDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if rejectBadJSON && !json.Valid(raw) {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		d.calls++
		d.method, d.path, d.query, d.body = r.Method, r.URL.Path, r.URL.RawQuery, string(raw)
		d.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	d.url = srv.URL
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(filepath.Join(tmp, ".datawatch", "config.yaml"), "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return d
}

func TestProtectedPathsCLI(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		d := newProtectedPathsDaemon(t, http.StatusOK, protectedPathsCLISample, false)
		out, err := runLaneFillCmd(t, newAutonomousProtectedPathsCmd(), []string{"--json"})
		if err != nil {
			t.Fatal(err)
		}
		if out != protectedPathsCLISample {
			t.Errorf("--json must print the body untouched\ngot:  %q\nwant: %q", out, protectedPathsCLISample)
		}
		calls, method, path, query, _ := d.last()
		if calls != 1 || method != http.MethodGet || path != "/api/autonomous/protected-paths" || query != "" {
			t.Errorf("%d x %s %s?%s; want one GET of the route with no filter", calls, method, path, query)
		}
	})

	t.Run("human output", func(t *testing.T) {
		d := newProtectedPathsDaemon(t, http.StatusOK, protectedPathsCLISample, false)
		out, err := runLaneFillCmd(t, newAutonomousProtectedPathsCmd(), []string{"--automaton", "abcd1234"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, path, query, _ := d.last(); path != "/api/autonomous/protected-paths" || query != "automaton=abcd1234" {
			t.Errorf("path=%s query=%q; want the route with the automaton filter", path, query)
		}
		for _, want := range []string{
			"Automaton protected paths (action: hold, rules source: base_commit):",
			"Protected list (2): AGENT.md, docs/plans/",
			"Held (1):",
			"Automaton Docs refresh (abcd1234), task Rewrite the intro (t7)",
			"paths: AGENT.md, docs/plans/a.md",
			"reason: the commit changed protected paths",
			"held since: 2026-11-01 09:00:00 UTC",
			"question: q42 (answer approve, reject or change the spec)",
			"Authorised in advance (1):",
			"Automaton Release notes (ef567890): CHANGELOG.md",
			"answer the question in the question queue",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
		for _, bad := range []string{"PRD", "BL"} {
			if strings.Contains(out, bad) {
				t.Errorf("output contains %q:\n%s", bad, out)
			}
		}
	})

	t.Run("empty status", func(t *testing.T) {
		newProtectedPathsDaemon(t, http.StatusOK,
			`{"action":"warn","rules_source":"worktree","paths":[],"held":[],"authorized":[]}`, false)
		out, err := runLaneFillCmd(t, newAutonomousProtectedPathsCmd(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"(action: warn, rules source: worktree)",
			"Protected list: none",
			"no task is held on protected paths",
			"no Automaton authorises protected paths",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("registered with help", func(t *testing.T) {
		names := map[string]bool{}
		for _, c := range newAutonomousCmd().Commands() {
			names[c.Name()] = true
		}
		for _, want := range []string{"protected-paths", "authorize-paths"} {
			if !names[want] {
				t.Errorf("%s is not registered under autonomous", want)
			}
		}
		help := newAutonomousCmd().Long
		for _, want := range []string{"protected-paths [--automaton ID] [--json]", "authorize-paths <automaton-id> [--path P ...] [--clear]"} {
			if !strings.Contains(help, want) {
				t.Errorf("the autonomous Subcommands help lacks %q", want)
			}
		}
	})

	t.Run("authorize", func(t *testing.T) {
		d := newProtectedPathsDaemon(t, http.StatusOK,
			`{"id":"abcd1234","title":"Docs refresh","authorized_paths":["AGENT.md","docs/plans/"]}`, false)
		out, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(),
			[]string{"abcd1234", "--path", "docs/plans/", "--path", "AGENT.md"})
		if err != nil {
			t.Fatal(err)
		}
		calls, method, path, _, body := d.last()
		if calls != 1 || method != http.MethodPost || path != "/api/autonomous/prds/abcd1234/set_authorized_paths" {
			t.Errorf("%d x %s %s; want one POST of set_authorized_paths", calls, method, path)
		}
		var sent struct {
			Paths []string `json:"paths"`
		}
		if err := json.Unmarshal([]byte(body), &sent); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if !reflect.DeepEqual(sent.Paths, []string{"docs/plans/", "AGENT.md"}) {
			t.Errorf("sent paths = %v, want the --path values in order", sent.Paths)
		}
		if want := "Automaton Docs refresh (abcd1234) authorises 2 protected path(s): AGENT.md, docs/plans/"; !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	})

	t.Run("authorize keeps a comma in a path", func(t *testing.T) {
		d := newProtectedPathsDaemon(t, http.StatusOK, `{"id":"abcd1234","authorized_paths":["a,b.md"]}`, false)
		if _, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(), []string{"abcd1234", "--path", "a,b.md"}); err != nil {
			t.Fatal(err)
		}
		if _, _, _, _, body := d.last(); body != `{"paths":["a,b.md"]}` {
			t.Errorf("body = %s, want the path kept whole", body)
		}
	})

	t.Run("clear", func(t *testing.T) {
		d := newProtectedPathsDaemon(t, http.StatusOK, `{"id":"abcd1234","title":"Docs refresh"}`, false)
		out, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(), []string{"abcd1234", "--clear"})
		if err != nil {
			t.Fatal(err)
		}
		calls, method, path, _, body := d.last()
		if calls != 1 || method != http.MethodPost || path != "/api/autonomous/prds/abcd1234/set_authorized_paths" {
			t.Errorf("%d x %s %s; want one POST of set_authorized_paths", calls, method, path)
		}
		// An empty list goes on the wire as [] (clear), never null.
		if body != `{"paths":[]}` {
			t.Errorf("body = %s, want {\"paths\":[]}", body)
		}
		if want := "Automaton Docs refresh (abcd1234) authorises no protected paths"; !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	})

	t.Run("neither flag is refused", func(t *testing.T) {
		d := newProtectedPathsDaemon(t, http.StatusOK, `{}`, false)
		_, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(), []string{"abcd1234"})
		if err == nil {
			t.Fatal("authorize-paths with neither --path nor --clear must refuse")
		}
		if !strings.Contains(err.Error(), "--path") || !strings.Contains(err.Error(), "--clear") {
			t.Errorf("error = %q, want it to name --path and --clear", err)
		}
		// A blank --path is no path.
		if _, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(), []string{"abcd1234", "--path", "  "}); err == nil {
			t.Error("a blank --path must be refused like no flag")
		}
		if calls, _, _, _, _ := d.last(); calls != 0 {
			t.Errorf("a refused run must send nothing, got %d call(s)", calls)
		}
	})

	t.Run("path and clear together are refused", func(t *testing.T) {
		d := newProtectedPathsDaemon(t, http.StatusOK, `{}`, false)
		if _, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(), []string{"abcd1234", "--path", "a/", "--clear"}); err == nil {
			t.Fatal("--path with --clear must be refused")
		}
		if calls, _, _, _, _ := d.last(); calls != 0 {
			t.Errorf("a refused run must send nothing, got %d call(s)", calls)
		}
	})

	t.Run("bad id and missing id are refused", func(t *testing.T) {
		d := newProtectedPathsDaemon(t, http.StatusOK, `{}`, false)
		if _, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(), []string{"ab/cd", "--clear"}); err == nil {
			t.Error("an id with a slash must be refused")
		}
		if _, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(), []string{"--clear"}); err == nil {
			t.Error("a missing Automaton id must be refused")
		}
		if calls, _, _, _, _ := d.last(); calls != 0 {
			t.Errorf("a refused run must send nothing, got %d call(s)", calls)
		}
	})

	t.Run("unknown Automaton", func(t *testing.T) {
		newProtectedPathsDaemon(t, http.StatusNotFound, "not found", false)
		_, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(), []string{"nosuchid", "--path", "a/"})
		if err == nil {
			t.Fatal("an unknown Automaton must be an error")
		}
		if !strings.Contains(err.Error(), "Automaton nosuchid was not found") {
			t.Errorf("error = %q, want it to name the unknown Automaton", err)
		}
	})

	t.Run("daemon refusal is passed back", func(t *testing.T) {
		newProtectedPathsDaemon(t, http.StatusBadRequest, `entry "/etc" is absolute; use a repository-relative path`, false)
		_, err := runLaneFillCmd(t, newAutonomousAuthorizePathsCmd(), []string{"abcd1234", "--path", "/etc"})
		if err == nil || !strings.Contains(err.Error(), "is absolute") {
			t.Errorf("error = %v, want the daemon's refusal text", err)
		}
	})
}

// The three protected-paths settings are written with the existing
// `datawatch config set`: the key and value reach PUT /api/config unchanged,
// so no parallel config command exists. The double refuses a body that is not
// valid JSON the way the daemon does, which makes config set retry with a
// quoted string.
func TestProtectedPathsCLIConfigSetIsGeneric(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		want       any
	}{
		{"autonomous.automation.protected_paths", "docs/plans/,AGENT.md", "docs/plans/,AGENT.md"},
		{"autonomous.automation.protected_paths", `["docs/plans/","AGENT.md"]`, []any{"docs/plans/", "AGENT.md"}},
		{"autonomous.automation.protected_paths_action", "block", "block"},
		{"autonomous.automation.rules_review_source", "worktree", "worktree"},
	} {
		d := newProtectedPathsDaemon(t, http.StatusOK, `{"ok":true}`, true)
		// Keep the command off the operator's real config: a path that does not
		// exist loads the defaults, and serverURL points it at the double.
		prevURL, prevCfg := serverURL, cfgPath
		serverURL = d.url
		cfgPath = filepath.Join(t.TempDir(), "config.yaml")
		c := newConfigSetCmd()
		c.SetArgs([]string{tc.key, tc.value})
		err := usageRun(t, c)
		serverURL, cfgPath = prevURL, prevCfg
		if err != nil {
			t.Fatalf("config set %s: %v", tc.key, err)
		}
		calls, method, path, _, body := d.last()
		if calls != 1 || method != http.MethodPut || path != "/api/config" {
			t.Errorf("%s: %d x %s %s; want one PUT /api/config", tc.key, calls, method, path)
		}
		var sent map[string]any
		if err := json.Unmarshal([]byte(body), &sent); err != nil {
			t.Fatalf("%s: body %q: %v", tc.key, body, err)
		}
		if len(sent) != 1 || !reflect.DeepEqual(sent[tc.key], tc.want) {
			t.Errorf("%s: body = %s, want the one key with %v", tc.key, body, tc.want)
		}
	}
}
