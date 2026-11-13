package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompletedMigrateCLI_FlagsMapToQuery(t *testing.T) {
	cases := []struct {
		name string
		a    completedMigrateArgs
		want string
	}{
		{"none", completedMigrateArgs{}, ""},
		{"dry run", completedMigrateArgs{dryRun: true}, "dry_run=true"},
		{"limit", completedMigrateArgs{limit: 5}, "limit=5"},
		{"id", completedMigrateArgs{id: " abcd1234 "}, "id=abcd1234"},
		{"all", completedMigrateArgs{dryRun: true, limit: 2, id: "x"}, "dry_run=true&id=x&limit=2"},
	}
	for _, c := range cases {
		got, err := c.a.query()
		if err != nil || got != c.want {
			t.Errorf("%s: query = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	if (&completedMigrateArgs{}).path() != "/api/autonomous/completed/migrate" {
		t.Errorf("wrong path")
	}
}

func TestCompletedMigrateCLI_BadLimitRejected(t *testing.T) {
	a := &completedMigrateArgs{limit: -1}
	if _, err := a.query(); !errors.Is(err, errCompletedMigrateLimit) {
		t.Fatalf("query err = %v; want errCompletedMigrateLimit", err)
	}
	if err := a.run(); !errors.Is(err, errCompletedMigrateLimit) {
		t.Fatalf("run err = %v; want errCompletedMigrateLimit", err)
	}
	cmd := newAutonomousCompletedMigrateCmd()
	if err := cmd.Flags().Parse([]string{"--limit", "abc"}); err == nil {
		t.Errorf("a non-numeric --limit must be rejected")
	}
}

func TestCompletedMigrateCLI_CommandWiring(t *testing.T) {
	cmd := newAutonomousCompletedMigrateCmd()
	if cmd.Name() != "completed-migrate" {
		t.Fatalf("Name = %q", cmd.Name())
	}
	for _, f := range []string{"dry-run", "limit", "id"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("missing --%s", f)
		}
	}
	found := false
	for _, c := range newAutonomousCmd().Commands() {
		if c.Name() == "completed-migrate" {
			found = true
		}
	}
	if !found {
		t.Errorf("completed-migrate is not registered under autonomous")
	}
}

func TestCompletedMigrateCLI_RunSendsQueryAndHandles409(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		if status != http.StatusOK {
			http.Error(w, "a final gate is running", status)
			return
		}
		_, _ = w.Write([]byte(`{"considered":2,"changed":0}`))
	}))
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	defer func() { serverURL = old }()

	a := &completedMigrateArgs{dryRun: true, limit: 3, id: "ab12"}
	if err := a.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/autonomous/completed/migrate" {
		t.Errorf("sent %s %s", gotMethod, gotPath)
	}
	if gotQuery != "dry_run=true&id=ab12&limit=3" {
		t.Errorf("query = %q", gotQuery)
	}

	status = http.StatusConflict
	err := (&completedMigrateArgs{}).run()
	if err == nil {
		t.Fatalf("409 must return an error so the command exits non-zero")
	}
	if strings.Contains(err.Error(), "HTTP 409") || !strings.Contains(err.Error(), "a final gate is running") {
		t.Errorf("409 error = %q; want the daemon's plain message", err)
	}
}
