package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runKindCmd runs c against a fake daemon and reports the request it saw. path
// stays empty when the command refused before sending anything.
func runKindCmd(t *testing.T, c *cobra.Command, argv []string) (path string, body map[string]any, err error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if werr := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); werr != nil {
		t.Fatalf("write config: %v", werr)
	}
	c.SetArgs(argv)
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	err = c.Execute()
	return path, body, err
}

func TestProgressKindCLI_SetKind(t *testing.T) {
	for _, kind := range []string{"feature", "bug", "other"} {
		path, body, err := runKindCmd(t, newAutonomousPRDSetKindCmd(), []string{"p1", kind})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if path != "/api/autonomous/prds/p1/set_kind" || !reflect.DeepEqual(body, map[string]any{"kind": kind}) {
			t.Errorf("%s: path=%s body=%v", kind, path, body)
		}
	}
}

func TestProgressKindCLI_SetKindNormalizesCase(t *testing.T) {
	_, body, err := runKindCmd(t, newAutonomousPRDSetKindCmd(), []string{"p1", "Bug"})
	if err != nil || body["kind"] != "bug" {
		t.Fatalf("err=%v body=%v", err, body)
	}
}

func TestProgressKindCLI_SetKindRejectsUnknownBeforeSending(t *testing.T) {
	for _, bad := range []string{"chore", ""} {
		path, _, err := runKindCmd(t, newAutonomousPRDSetKindCmd(), []string{"p1", bad})
		if err == nil || !strings.Contains(err.Error(), "feature, bug or other") {
			t.Errorf("%q: expected a clear error, got %v", bad, err)
		}
		if path != "" {
			t.Errorf("%q: nothing may be sent for an unknown kind, saw %s", bad, path)
		}
	}
}

func TestProgressKindCLI_SetKindArgCount(t *testing.T) {
	for _, argv := range [][]string{{"p1"}, {"p1", "bug", "extra"}} {
		if _, _, err := runKindCmd(t, newAutonomousPRDSetKindCmd(), argv); err == nil {
			t.Errorf("%v: expected an argument error", argv)
		}
	}
}

func TestProgressKindCLI_CreateSendsKind(t *testing.T) {
	path, body, err := runKindCmd(t, newAutonomousPRDCreateCmd(), []string{"Fix the leak", "--kind", "Bug"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/autonomous/prds" || body["kind"] != "bug" {
		t.Errorf("path=%s body=%v", path, body)
	}
}

func TestProgressKindCLI_CreateOmitsKindByDefault(t *testing.T) {
	_, body, err := runKindCmd(t, newAutonomousPRDCreateCmd(), []string{"Add paging"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["kind"]; ok {
		t.Errorf("kind must be left out when the flag is absent, body=%v", body)
	}
}

func TestProgressKindCLI_CreateRejectsUnknownKindBeforeSending(t *testing.T) {
	path, _, err := runKindCmd(t, newAutonomousPRDCreateCmd(), []string{"Add paging", "--kind", "chore"})
	if err == nil || !strings.Contains(err.Error(), "feature, bug or other") {
		t.Errorf("expected a clear error, got %v", err)
	}
	if path != "" {
		t.Errorf("nothing may be sent for an unknown kind, saw %s", path)
	}
}

func TestProgressKindCLI_Registered(t *testing.T) {
	found := false
	for _, c := range newAutonomousCmd().Commands() {
		if c.Name() == "prd-set-kind" {
			found = true
		}
	}
	if !found {
		t.Error("prd-set-kind is not registered under autonomous")
	}
}
