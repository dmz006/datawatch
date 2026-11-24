package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const finishedWorkCLIBody = `{"mode":"on","generated_at":"2026-11-24T12:00:00Z","last_sweep":"2026-11-24T11:50:00Z","waiting":[
 {"automaton":"abcd0001","title":"Git hosts","status":"completed","cause":"finished_not_merged","branch":"automaton/abcd0001","ahead":57,"owner":"dave","since":"2026-11-20T12:00:00Z","question_id":"q-1"}]}`

func finishedWorkDaemon(t *testing.T, body string) (method, path, reqBody *string) {
	t.Helper()
	var m, p, b string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, p = r.Method, r.URL.Path
		raw := make([]byte, 512)
		n, _ := r.Body.Read(raw)
		b = string(raw[:n])
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return &m, &p, &b
}

func runFinishedWorkCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := &cobra.Command{Use: "autonomous", SilenceUsage: true, SilenceErrors: true}
	c.AddCommand(newAutonomousFinishedWorkCmd())
	c.SetArgs(append([]string{"finished-work"}, args...))
	out := &strings.Builder{}
	c.SetOut(out)
	err := c.Execute()
	return out.String(), err
}

func TestFinishedWorkCLIRegistered(t *testing.T) {
	for _, sub := range newAutonomousCmd().Commands() {
		if sub.Name() == "finished-work" {
			return
		}
	}
	t.Error("finished-work is not registered under autonomous")
}

func TestFinishedWorkCLIReadsTheDigest(t *testing.T) {
	method, path, _ := finishedWorkDaemon(t, finishedWorkCLIBody)
	out, err := runFinishedWorkCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if *method != http.MethodGet || *path != "/api/autonomous/finished-work" {
		t.Errorf("%s %s", *method, *path)
	}
	for _, want := range []string{"abcd0001", "Git hosts", "57", "dave", "q-1", "4d", "autonomous.finished_work: on"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %s", want, out)
		}
	}
	raw, err := runFinishedWorkCmd(t, "--json")
	if err != nil || strings.TrimSpace(raw) != strings.TrimSpace(finishedWorkCLIBody) {
		t.Errorf("--json = %q, err %v", raw, err)
	}
}

func TestFinishedWorkCLIRunPostsAndRejectsUnknown(t *testing.T) {
	method, path, body := finishedWorkDaemon(t, `{"mode":"on","scanned":9,"by_cause":{"finished_not_merged":2},"findings":[{},{}]}`)
	out, err := runFinishedWorkCmd(t, "run", "audit")
	if err != nil {
		t.Fatal(err)
	}
	if *method != http.MethodPost || *path != "/api/autonomous/finished-work/run" || !strings.Contains(*body, `"run":"audit"`) {
		t.Errorf("%s %s %s", *method, *path, *body)
	}
	if !strings.Contains(out, "read 9 Automata, 2 finding(s)") || !strings.Contains(out, "finished_not_merged: 2") {
		t.Errorf("output = %s", out)
	}
	if _, err := runFinishedWorkCmd(t, "run", "everything"); err == nil {
		t.Error("an unknown run must be refused")
	}
}

func TestPRDCancelCLICarriesTheDecision(t *testing.T) {
	method, path, _ := finishedWorkDaemon(t, `{"status":"cancelled"}`)
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*method, *path, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"status":"cancelled"}`))
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()
	c := newAutonomousPRDCancelCmd()
	c.SetArgs([]string{"abcd0001", "--decision", "keep", "--reason", "for review"})
	c.SetOut(&strings.Builder{})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if *method != http.MethodDelete || !strings.HasSuffix(*path, "/abcd0001") || !strings.Contains(gotQuery, "decision=keep") || !strings.Contains(gotQuery, "reason=for+review") {
		t.Errorf("%s %s ?%s", *method, *path, gotQuery)
	}
}
