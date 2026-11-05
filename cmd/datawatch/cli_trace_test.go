package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// traceFakeDaemon answers every request with an empty JSON object and
// records the last path and query.
func traceFakeDaemon(t *testing.T, path *string, q *url.Values) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.EscapedPath()
		*q = r.URL.Query()
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runTraceCLI(t *testing.T, srv *httptest.Server, args ...string) error {
	t.Helper()
	old := os.Stdout
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	os.Stdout = devnull
	defer func() { os.Stdout = old; _ = devnull.Close() }()
	return withServerURL(t, srv, func() error {
		c := newTraceCmd()
		c.SetArgs(args)
		c.SilenceUsage, c.SilenceErrors = true, true
		return c.Execute()
	})
}

func TestTraceCLI_Subcommands(t *testing.T) {
	c := newTraceCmd()
	got := map[string]bool{}
	for _, s := range c.Commands() {
		got[s.Name()] = true
	}
	for _, want := range []string{"session", "automaton", "rollup", "coverage"} {
		if !got[want] {
			t.Errorf("trace lacks the %s subcommand", want)
		}
	}
	if strings.Contains(c.Long+c.Short, "PRD") {
		t.Error("help text must say Automaton, not PRD")
	}
}

func TestTraceCLI_SessionPathAndFlags(t *testing.T) {
	var path string
	var q url.Values
	srv := traceFakeDaemon(t, &path, &q)
	err := runTraceCLI(t, srv, "session", "host/s 1", "--tool", "Read", "--backend", "pi", "--kind", "tool_call", "--errors", "--limit", "5", "--loop-threshold", "3")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/trace/session/host%2Fs%201" {
		t.Errorf("path = %q", path)
	}
	for k, want := range map[string]string{"tool": "Read", "backend": "pi", "kind": "tool_call", "errors": "true", "limit": "5", "loop_threshold": "3"} {
		if q.Get(k) != want {
			t.Errorf("query %s = %q, want %q", k, q.Get(k), want)
		}
	}
}

func TestTraceCLI_AutomatonRollupCoverage(t *testing.T) {
	var path string
	var q url.Values
	srv := traceFakeDaemon(t, &path, &q)
	if err := runTraceCLI(t, srv, "automaton", "a1"); err != nil || path != "/api/trace/automaton/a1" || len(q) != 0 {
		t.Errorf("automaton: err=%v path=%q q=%v", err, path, q)
	}
	if err := runTraceCLI(t, srv, "rollup", "--dimension", "backend", "--key", "pi"); err != nil ||
		path != "/api/trace/rollup" || q.Get("dimension") != "backend" || q.Get("key") != "pi" {
		t.Errorf("rollup: err=%v path=%q q=%v", err, path, q)
	}
	if err := runTraceCLI(t, srv, "coverage", "--backend", "ollama"); err != nil ||
		path != "/api/trace/coverage" || q.Get("backend") != "ollama" {
		t.Errorf("coverage: err=%v path=%q q=%v", err, path, q)
	}
}

func TestTraceCLI_ArgErrors(t *testing.T) {
	var path string
	var q url.Values
	srv := traceFakeDaemon(t, &path, &q)
	if err := runTraceCLI(t, srv, "session"); err == nil {
		t.Error("session without an id must fail")
	}
	if err := runTraceCLI(t, srv, "rollup", "extra"); err == nil {
		t.Error("rollup takes no positional argument")
	}
}
