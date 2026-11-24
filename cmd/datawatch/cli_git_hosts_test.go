package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const gitHostsSample = `{"hosts":[
 {"name":"gh","kind":"github","credential_source":"vault","vault_secret":"gh-token",
  "readiness":{"logged_in":true,"account":"octo"}},
 {"name":"gl","kind":"gitlab","credential_source":"host_login",
  "readiness":{"logged_in":false,"problem":"not logged in","fix_command":"glab auth login"}}]}`

func gitHostsTestServer(t *testing.T, status int, puts *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/config":
			b, _ := io.ReadAll(r.Body)
			*puts = append(*puts, string(b))
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"git.hosts.gh: vault_secret is required"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/git/hosts":
			_, _ = w.Write([]byte(gitHostsSample))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGitHostsCLI(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		var puts []string
		srv := gitHostsTestServer(t, http.StatusOK, &puts)
		defer srv.Close()
		var out bytes.Buffer
		if err := runGitHostsConfigSet(&out, srv.URL, "", "git.hosts.gh.credential_source", "vault"); err != nil {
			t.Fatalf("set: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(puts[0]), &body); err != nil || body["git.hosts.gh.credential_source"] != "vault" {
			t.Fatalf("PUT body = %q", puts[0])
		}
		s := out.String()
		for _, want := range []string{"Set git.hosts.gh.credential_source = vault", "gh (github)", "ready: yes (octo)"} {
			if !strings.Contains(s, want) {
				t.Errorf("output missing %q:\n%s", want, s)
			}
		}
		// A non-credential key prints no readiness.
		out.Reset()
		if err := runGitHostsConfigSet(&out, srv.URL, "", "git.hosts.gh.sign_tags", "true"); err != nil {
			t.Fatalf("set sign_tags: %v", err)
		}
		if strings.Contains(out.String(), "ready:") || !strings.Contains(puts[1], `:true`) {
			t.Errorf("sign_tags: out=%q put=%q", out.String(), puts[1])
		}
	})

	t.Run("bad value", func(t *testing.T) {
		var puts []string
		srv := gitHostsTestServer(t, http.StatusBadRequest, &puts)
		defer srv.Close()
		var out bytes.Buffer
		err := runGitHostsConfigSet(&out, srv.URL, "", "git.hosts.gh.credential_source", "bogus")
		if err == nil {
			t.Fatal("daemon refusal must be an error")
		}
		if strings.Contains(out.String(), "Set ") {
			t.Errorf("no success line on refusal: %q", out.String())
		}
		if err := runGitHostsConfigSet(&out, srv.URL, "", "git.hosts.gh.sign_tags", "maybe"); err == nil {
			t.Error("non-boolean sign_tags must be refused locally")
		}
		if err := runGitHostsConfigSet(&out, srv.URL, "", "git.hosts.gh", "x"); err == nil {
			t.Error("malformed key must be refused")
		}
		if len(puts) != 1 {
			t.Errorf("only the daemon-refused set should reach the daemon, got %d PUTs", len(puts))
		}
	})

	t.Run("list", func(t *testing.T) {
		var puts []string
		srv := gitHostsTestServer(t, http.StatusOK, &puts)
		defer srv.Close()
		var out bytes.Buffer
		if err := runGitHostsList(&out, srv.URL, ""); err != nil {
			t.Fatalf("list: %v", err)
		}
		s := out.String()
		for _, want := range []string{"gh (github) credential_source=vault vault_secret=gh-token", "gl (gitlab)", "ready: no", "problem: not logged in", "fix: glab auth login"} {
			if !strings.Contains(s, want) {
				t.Errorf("output missing %q:\n%s", want, s)
			}
		}
	})

	t.Run("config set command prints readiness after a credential source set", func(t *testing.T) {
		var puts []string
		srv := gitHostsTestServer(t, http.StatusOK, &puts)
		defer srv.Close()
		old := serverURL
		serverURL = srv.URL
		t.Cleanup(func() { serverURL = old })

		// runConfigSet is the body of `datawatch config set`; for a git host
		// key it must dispatch to the dedicated helper, send the value as the
		// field's type, and print the readiness result after the daemon
		// accepts it.
		var err error
		s := captureStdout(t, func() {
			err = runConfigSet(&config.Config{}, "git.hosts.gh.credential_source", "vault")
		})
		if err != nil {
			t.Fatalf("config set: %v", err)
		}
		for _, want := range []string{"Set git.hosts.gh.credential_source = vault", "gh (github)", "ready: yes (octo)"} {
			if !strings.Contains(s, want) {
				t.Errorf("config set output missing %q:\n%s", want, s)
			}
		}
		// sign_tags goes over the wire as a JSON boolean, and a non-credential
		// set prints no readiness.
		s = captureStdout(t, func() {
			err = runConfigSet(&config.Config{}, "git.hosts.gh.sign_tags", "true")
		})
		if err != nil {
			t.Fatalf("config set sign_tags: %v", err)
		}
		if strings.Contains(s, "ready:") {
			t.Errorf("sign_tags set must not print readiness:\n%s", s)
		}
		if !strings.Contains(puts[len(puts)-1], `"git.hosts.gh.sign_tags":true`) {
			t.Errorf("sign_tags must be sent as a JSON boolean, PUT body %q", puts[len(puts)-1])
		}
		// A daemon refusal is an error and no success line is printed.
		srv.Close()
		srv2 := gitHostsTestServer(t, http.StatusBadRequest, &puts)
		defer srv2.Close()
		serverURL = srv2.URL
		s = captureStdout(t, func() {
			err = runConfigSet(&config.Config{}, "git.hosts.gh.credential_source", "bogus")
		})
		if err == nil {
			t.Fatal("daemon refusal must be an error")
		}
		if strings.Contains(s, "Set ") {
			t.Errorf("no success line on refusal:\n%s", s)
		}
	})

	t.Run("git-hosts list command", func(t *testing.T) {
		var puts []string
		srv := gitHostsTestServer(t, http.StatusOK, &puts)
		defer srv.Close()
		old := serverURL
		serverURL = srv.URL
		t.Cleanup(func() { serverURL = old })

		// runGitHostsListCmd is the body of `datawatch git-hosts`.
		var err error
		s := captureStdout(t, func() {
			err = runGitHostsListCmd(&config.Config{})
		})
		if err != nil {
			t.Fatalf("git-hosts: %v", err)
		}
		for _, want := range []string{"gh (github) credential_source=vault vault_secret=gh-token", "gl (gitlab)", "ready: no", "fix: glab auth login"} {
			if !strings.Contains(s, want) {
				t.Errorf("git-hosts output missing %q:\n%s", want, s)
			}
		}
	})
}

func TestGitHostsStatsCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/git/hosts/stats" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"configured":2,"window_seconds":3600,"operations":{"push":3,"merge":1},"failed":1,"last_success_at":"0001-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	var out bytes.Buffer
	if err := runGitHostsStats(&out, srv.URL, ""); err != nil {
		t.Fatalf("stats: %v", err)
	}
	for _, want := range []string{"configured hosts: 2", "failed: 1", "last success: never", "push: 3", "merge: 1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}
