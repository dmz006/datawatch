// git.hosts REST coverage: PUT each dotted key git.hosts.<name>.<field>
// then GET it back, PUT the whole list, and confirm rejected values
// (unknown field, unknown host, bad kind/source, non-boolean sign_tags,
// renaming the key) all come back 400 with a clear message and never
// clobber the stored list.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func gitHostsServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		cfg: &config.Config{
			Git: config.GitConfig{
				// One github host, the only defaults-filled entry we need.
				Hosts: []config.GitHostConfig{
					{Name: "example", Kind: "github", CredentialSource: config.GitCredentialHostLogin},
				},
			},
		},
		cfgPath: t.TempDir() + "/config.yaml",
	}
}

func gitHostsPut(t *testing.T, s *Server, patch map[string]any) (int, string) {
	t.Helper()
	body, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("encode PUT body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)
	return rr.Code, rr.Body.String()
}

func gitHostsGet(t *testing.T, s *Server) []map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d %s", rr.Code, rr.Body.String())
	}
	var v map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode GET /api/config: %v", err)
	}
	git, _ := v["git"].(map[string]any)
	if git == nil {
		t.Fatalf("GET /api/config response has no git section: %v", v)
	}
	arr, ok := git["hosts"].([]any)
	if !ok {
		t.Fatalf("git.hosts missing or not an array: %v", git["hosts"])
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("git.hosts entry is not an object: %v", e)
		}
		out = append(out, m)
	}
	return out
}

// gitHostByName finds the host entry named name in the GET response (or
// fails the test when it is absent).
func gitHostByName(t *testing.T, hosts []map[string]any, name string) map[string]any {
	t.Helper()
	for _, h := range hosts {
		if h["name"] == name {
			return h
		}
	}
	t.Fatalf("git host %q not in GET response: %v", name, hosts)
	return nil
}

func gitHostsRoundTrip(t *testing.T, key string, value any, want func(h map[string]any) bool) {
	t.Helper()
	s := gitHostsServer(t)
	if code, body := gitHostsPut(t, s, map[string]any{key: value}); code != http.StatusOK {
		t.Fatalf("PUT %s=%v: HTTP %d (%s)", key, value, code, body)
	}
	h := gitHostByName(t, gitHostsGet(t, s), "example")
	if !want(h) {
		t.Errorf("PUT %s=%v not read back: %v", key, value, h)
	}
}

func TestGitHostsConfigAPI(t *testing.T) {
	// One PUT-then-GET round trip per field.
	t.Run("base_url", func(t *testing.T) {
		gitHostsRoundTrip(t, "git.hosts.example.base_url", "https://git.example.com",
			func(h map[string]any) bool { return h["base_url"] == "https://git.example.com" })
	})
	t.Run("signing_key", func(t *testing.T) {
		gitHostsRoundTrip(t, "git.hosts.example.signing_key", "key-123",
			func(h map[string]any) bool { return h["signing_key"] == "key-123" })
	})
	t.Run("vault_secret", func(t *testing.T) {
		gitHostsRoundTrip(t, "git.hosts.example.vault_secret", "git-token",
			func(h map[string]any) bool { return h["vault_secret"] == "git-token" })
	})
	t.Run("credential_source vault reads back", func(t *testing.T) {
		s := gitHostsServer(t)
		// vault requires the secret NAME to be present — set it first.
		if code, body := gitHostsPut(t, s, map[string]any{"git.hosts.example.vault_secret": "git-token"}); code != http.StatusOK {
			t.Fatalf("PUT vault_secret: HTTP %d (%s)", code, body)
		}
		if code, body := gitHostsPut(t, s, map[string]any{"git.hosts.example.credential_source": "vault"}); code != http.StatusOK {
			t.Fatalf("PUT credential_source=vault: HTTP %d (%s)", code, body)
		}
		h := gitHostByName(t, gitHostsGet(t, s), "example")
		if h["credential_source"] != "vault" {
			t.Errorf("credential_source = %v, want vault", h["credential_source"])
		}
	})
	t.Run("credential_source effective default", func(t *testing.T) {
		s := gitHostsServer(t)
		if code, body := gitHostsPut(t, s, map[string]any{"git.hosts.example.credential_source": ""}); code != http.StatusOK {
			t.Fatalf("PUT credential_source clear: HTTP %d (%s)", code, body)
		}
		h := gitHostByName(t, gitHostsGet(t, s), "example")
		if h["credential_source"] != "host_login" {
			t.Errorf("effective credential_source = %v, want host_login", h["credential_source"])
		}
	})
	t.Run("kind", func(t *testing.T) {
		gitHostsRoundTrip(t, "git.hosts.example.kind", "gitlab",
			func(h map[string]any) bool { return h["kind"] == "gitlab" })
	})
	t.Run("sign_tags boolean round trip", func(t *testing.T) {
		s := gitHostsServer(t)
		if code, body := gitHostsPut(t, s, map[string]any{"git.hosts.example.sign_tags": true}); code != http.StatusOK {
			t.Fatalf("PUT sign_tags=true: HTTP %d (%s)", code, body)
		}
		h := gitHostByName(t, gitHostsGet(t, s), "example")
		if h["sign_tags"] != true {
			t.Errorf("sign_tags = %v, want true", h["sign_tags"])
		}
		if code, body := gitHostsPut(t, s, map[string]any{"git.hosts.example.sign_tags": false}); code != http.StatusOK {
			t.Fatalf("PUT sign_tags=false: HTTP %d (%s)", code, body)
		}
		h = gitHostByName(t, gitHostsGet(t, s), "example")
		if h["sign_tags"] != false {
			t.Errorf("sign_tags after false = %v, want false", h["sign_tags"])
		}
	})
	t.Run("whole list PUT", func(t *testing.T) {
		s := gitHostsServer(t)
		list := []map[string]any{
			{"name": "example", "kind": "github"},
			{"name": "selfgit", "kind": "git", "base_url": "https://git.own.example", "sign_tags": true},
		}
		if code, body := gitHostsPut(t, s, map[string]any{"git.hosts": list}); code != http.StatusOK {
			t.Fatalf("PUT git.hosts list: HTTP %d (%s)", code, body)
		}
		hosts := gitHostsGet(t, s)
		if len(hosts) != 2 {
			t.Fatalf("GET hosts = %d entries, want 2: %v", len(hosts), hosts)
		}
		if h := gitHostByName(t, hosts, "example"); h["kind"] != "github" {
			t.Errorf("example kind = %v, want github", h["kind"])
		}
		h := gitHostByName(t, hosts, "selfgit")
		if h["base_url"] != "https://git.own.example" || h["sign_tags"] != true || h["kind"] != "git" {
			t.Errorf("selfgit entry wrong: %v", h)
		}
	})
	t.Run("vault shows the secret name only", func(t *testing.T) {
		s := gitHostsServer(t)
		if code, body := gitHostsPut(t, s, map[string]any{
			"git.hosts.example.credential_source": "vault",
			"git.hosts.example.vault_secret":      "gh-token",
		}); code != http.StatusOK {
			t.Fatalf("PUT vault pair: HTTP %d (%s)", code, body)
		}
		h := gitHostByName(t, gitHostsGet(t, s), "example")
		if h["vault_secret"] != "gh-token" {
			t.Errorf("vault_secret = %v, want gh-token (the entry name only)", h["vault_secret"])
		}
	})
}

func TestGitHostsConfigAPIRejectsRejections(t *testing.T) {
	cases := []struct {
		key    string
		value  any
		wantIn string
	}{
		{"git.hosts.example.bogus", "x", "unknown git host field"},
		{"git.hosts.example.name", "renamed", "name cannot be changed"},
		{"git.hosts.unknown1.kind", "github", "not found"},
		{"git.hosts.example.kind", "svn", "unknown kind"},
		{"git.hosts.example.sign_tags", "yes", "true or false"},
		{"git.hosts.example", "github", "must be git.hosts.<name>.<field>"},
		{"git.hosts", "svn", "must be an array of host objects"},
		{"git.hosts.example.credential_source", "vault", "requires vault_secret"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+fmt.Sprintf("%v", tc.value), func(t *testing.T) {
			s := gitHostsServer(t)
			// The stored value that must survive the rejection.
			if code, body := gitHostsPut(t, s, map[string]any{"git.hosts.example.base_url": "https://keep.example"}); code != http.StatusOK {
				t.Fatalf("setup PUT: HTTP %d (%s)", code, body)
			}
			code, body := gitHostsPut(t, s, map[string]any{tc.key: tc.value})
			if code != http.StatusBadRequest {
				t.Fatalf("PUT %s=%v = HTTP %d, want 400 (%s)", tc.key, tc.value, code, body)
			}
			if !strings.Contains(body, tc.wantIn) {
				t.Errorf("rejection message %q does not explain %q", body, tc.wantIn)
			}
			h := gitHostByName(t, gitHostsGet(t, s), "example")
			if h["base_url"] != "https://keep.example" {
				t.Errorf("rejected PUT clobbered base_url: %v", h["base_url"])
			}
			if tc.key == "git.hosts.example.credential_source" && h["credential_source"] != "host_login" {
				t.Errorf("rejected vault PUT clobbered credential_source: %v", h["credential_source"])
			}
		})
	}
}
