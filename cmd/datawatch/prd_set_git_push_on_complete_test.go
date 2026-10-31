// prd-set-git — --push-on-complete is sent only when the flag was set;
// "inherit" sends "" so the daemon's git.push_on_complete applies.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/spf13/cobra"
)

func runSetGitCmd(t *testing.T, c *cobra.Command, argv []string) map[string]any {
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

func TestCLISetGitPushOnComplete_FlagSentWhenSet(t *testing.T) {
	got := runSetGitCmd(t, newAutonomousPRDSetGitCmd(), []string{"p1", "on", "--push-on-complete", "branch"})
	if _, ok := got["push_on_complete"]; !ok {
		t.Fatalf("body missing push_on_complete: %v", got)
	}
	if got["push_on_complete"] != "branch" {
		t.Errorf("push_on_complete = %v, want branch", got["push_on_complete"])
	}
	if got["auto_pr"] != true {
		t.Errorf("auto_pr = %v, want true", got["auto_pr"])
	}
}

func TestCLISetGitPushOnComplete_InheritSendsEmpty(t *testing.T) {
	got := runSetGitCmd(t, newAutonomousPRDSetGitCmd(), []string{"p1", "off", "--push-on-complete", "inherit"})
	if _, ok := got["push_on_complete"]; !ok {
		t.Fatalf("explicit --push-on-complete=inherit must be forwarded as \"\": %v", got)
	}
	if got["push_on_complete"] != "" {
		t.Errorf("push_on_complete = %v, want \"\"", got["push_on_complete"])
	}
	if got["auto_pr"] != false {
		t.Errorf("auto_pr = %v, want false", got["auto_pr"])
	}
}

func TestCLISetGitPushOnComplete_OmittedNotSent(t *testing.T) {
	got := runSetGitCmd(t, newAutonomousPRDSetGitCmd(), []string{"p1", "on", "--base-branch", "main"})
	if _, ok := got["push_on_complete"]; ok {
		t.Errorf("push_on_complete must be absent when the flag was not set, got %v", got)
	}
	if got["base_branch"] != "main" {
		t.Errorf("base_branch = %v, want main", got["base_branch"])
	}
}
