package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestGitHostsCommVerb covers the git.hosts comm surface (git_hosts_comm.go):
// a set, a list, and a rejected value — and proves the list and the set
// confirmation never carry a credential value, only the vault entry name.
func TestGitHostsCommVerb(t *testing.T) {
	// hostsRouter returns a router whose local API serves the daemon's
	// /api/git/hosts envelope with three fake git hosts, in the style of
	// auditCommRouter (audit_query_filters_test.go).
	hostsRouter := func(t *testing.T) *Router {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/git/hosts" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"hosts":[
				{"name":"github","kind":"github","base_url":"https://github.com","credential_source":"host-login","vault_secret":"","readiness":"ready"},
				{"name":"origin","kind":"gitlab","base_url":"https://git.example.com","credential_source":"host-login","vault_secret":"","readiness":"ready"},
				{"name":"private","kind":"gitea","base_url":"https://git.internal.example","credential_source":"vault","vault_secret":"codehost-pat","readiness":"ready"}
			]}`))
		}))
		t.Cleanup(srv.Close)
		port, err := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
		if err != nil {
			t.Fatal(err)
		}
		r := NewRouter("git-hosts-test", "g", nil, nil, 10, nil)
		r.SetWebPort(port)
		return r
	}

	t.Run("list shows kind and credential source, never the token", func(t *testing.T) {
		r := hostsRouter(t)
		r.SetConfigureFunc(func(key, value string) error {
			t.Fatalf("list verb must not write config (got %q, %q)", key, value)
			return nil
		})

		captured := r.HandleTestMessage("configure git.hosts list")
		out := strings.Join(captured, "\n")

		if !strings.Contains(out, "[git-hosts-test] git-hosts") {
			t.Fatalf("list output missing header:\n%s", out)
		}
		for _, want := range []string{
			"github",
			"kind=github",
			"kind=gitlab",
			"kind=gitea",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("list output missing %q:\n%s", want, out)
			}
		}
		// Each host shows its credential source; the vault-sourced host shows
		// the vault entry NAME — the secret value itself must never reach the
		// chat channel.
		for _, want := range []string{
			"credential_source=host-login",
			"credential_source=vault",
			"vault_secret=codehost-pat",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("list output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("set sends the key=value, confirmation carries the key only", func(t *testing.T) {
		r := hostsRouter(t)

		var mu sync.Mutex
		var gotKey, gotValue string
		r.SetConfigureFunc(func(key, value string) error {
			mu.Lock()
			gotKey, gotValue = key, value
			mu.Unlock()
			return nil
		})

		// A base_url whose value itself contains '=' proves the key/value
		// split happens at the first '=' of the field, not the last one.
		captured := r.HandleTestMessage("configure git.hosts.private.base_url=https://git.internal.example?token=abc=def")
		out := strings.Join(captured, "\n")

		mu.Lock()
		k, v := gotKey, gotValue
		mu.Unlock()
		if k != "git.hosts.private.base_url" || v != "https://git.internal.example?token=abc=def" {
			t.Fatalf("configure sent as (%q, %q); want key git.hosts.private.base_url and the full value", k, v)
		}
		if !strings.Contains(out, "configured git.hosts.private.base_url") {
			t.Fatalf("set confirmation missing:\n%s", out)
		}
		if strings.Contains(out, "abc=def") {
			t.Fatalf("set confirmation echoed the credential-bearing value:\n%s", out)
		}
	})

	t.Run("a rejected value is not sent as a change and the refusal is reported", func(t *testing.T) {
		r := hostsRouter(t)

		sent := 0
		mu := sync.Mutex{}

		// 1) A key without a value: nothing may be sent at all.
		r.SetConfigureFunc(func(key, value string) error {
			mu.Lock()
			sent++
			mu.Unlock()
			return nil
		})
		captured := r.HandleTestMessage("configure git.hosts.private.base_url")
		if sent != 0 {
			t.Fatalf("configure was sent for a key without a value")
		}
		if out := strings.Join(captured, "\n"); !strings.Contains(out, "invalid:") {
			t.Fatalf("expected the missing-value rejection:\n%s", out)
		}

		// 2) The daemon refuses the value (the whole host map is re-validated
		// before the write): the refusal must surface, success must not.
		rejected := 0
		r.SetConfigureFunc(func(key, value string) error {
			mu.Lock()
			defer mu.Unlock()
			if key == "git.hosts.private.kind" && value == "solaris" {
				rejected++
				return errors.New(`host kind must be github, gitlab, gitea, bitbucket or git, got "solaris"`)
			}
			t.Errorf("unexpected configure (%q, %q)", key, value)
			return nil
		})
		captured = r.HandleTestMessage("configure git.hosts.private.kind=solaris")
		out := strings.Join(captured, "\n")
		mu.Lock()
		n := rejected
		mu.Unlock()
		if n != 1 {
			t.Fatalf("the server-side rejection was not exercised exactly once (got %d)", n)
		}
		if !strings.Contains(out, "rejected git.hosts.private.kind") || !strings.Contains(out, "solaris") {
			t.Fatalf("expected the rejected-value reply with the refusal reason:\n%s", out)
		}
		if strings.Contains(out, "configured git.hosts.private.kind") {
			t.Fatalf("a rejected value reported success:\n%s", out)
		}
	})
}
