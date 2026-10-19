// prd-create / template-instantiate --project-dir: omitting the flag must
// send an empty project_dir (daemon applies its own default) instead of the
// CLI substituting the process cwd.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func runPRDCreateCmd(t *testing.T, c *cobra.Command, argv []string) map[string]any {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	c.SetArgs(argv)
	c.SetOut(io.Discard)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return got
}

func TestPRDCreate_OmittedProjectDirIsEmpty(t *testing.T) {
	got := runPRDCreateCmd(t, newAutonomousPRDCreateCmd(), []string{"Create a test feature"})
	v, ok := got["project_dir"]
	if !ok {
		t.Fatal("request body missing project_dir key")
	}
	if v != "" {
		t.Errorf("project_dir=%q want \"\" (omitted flag must not fall back to cwd)", v)
	}
	cwd, _ := os.Getwd()
	if cwd != "" && v == cwd {
		t.Errorf("project_dir equals process cwd %q; fallback is not supposed to run", cwd)
	}
}

func TestPRDCreate_ExplicitProjectDirSent(t *testing.T) {
	got := runPRDCreateCmd(t, newAutonomousPRDCreateCmd(), []string{"Create a test feature", "--project-dir", "/some/explicit/dir"})
	if got["project_dir"] != "/some/explicit/dir" {
		t.Errorf("project_dir=%v want \"/some/explicit/dir\"", got["project_dir"])
	}
}
