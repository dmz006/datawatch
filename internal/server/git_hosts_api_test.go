package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/secrets"
	"github.com/dmz006/datawatch/internal/stats"
)

// vaultTokenValue is the secret value stored for the vault host. The tests
// assert it never appears in any response body.
const vaultTokenValue = "ghp_TESTVAULTVALUE0123456789abcdef"

// writeFakeGH puts a fake gh binary first on PATH. It answers the two calls
// the readiness check and the connection test make: `gh auth status` and
// `gh api user`. It never prints a token.
func writeFakeGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1" in
  auth)
    echo "github.com"
    echo "  ✓ Logged in to github.com account fakeuser (keyring)"
    echo "  - Token scopes: 'repo', 'workflow'"
    exit 0 ;;
  api)
    echo '{"login":"fakeuser"}'
    exit 0 ;;
esac
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// gitHostTestServer builds a Server with two github hosts: one host_login and
// one vault-backed, plus a fake secrets store holding the vault value.
func gitHostTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{cfg: &config.Config{Git: config.GitConfig{Hosts: []config.GitHostConfig{
		{Name: "gh-main", Kind: "github", CredentialSource: config.GitCredentialHostLogin},
		{Name: "gh-vault", Kind: "github", CredentialSource: config.GitCredentialVault, VaultSecret: "gh_vault_token"},
	}}}}
	s.SetSecretsStore(&fakeSecretsStore{m: map[string]secrets.Secret{
		"gh_vault_token": {Name: "gh_vault_token", Value: vaultTokenValue},
	}})
	return s
}

// gitHostTestMux mirrors the two routes server.go registers.
func gitHostTestMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(gitHostsPath, s.handleGitHosts)
	mux.HandleFunc(gitHostsPath+"/", s.handleGitHostAction)
	return mux
}

func TestGitHostStatusAPI(t *testing.T) {
	writeFakeGH(t)
	s := gitHostTestServer(t)
	mux := gitHostTestMux(s)

	t.Run("list returns each host with readiness and no token", func(t *testing.T) {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, gitHostsPath, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Hosts []struct {
				Name             string `json:"name"`
				Kind             string `json:"kind"`
				CredentialSource string `json:"credential_source"`
				VaultSecret      string `json:"vault_secret"`
				Readiness        struct {
					Host         string `json:"host"`
					LoggedIn     bool   `json:"logged_in"`
					Account      string `json:"account"`
					HasRepoScope bool   `json:"has_repo_scope"`
					HasWorkflow  bool   `json:"has_workflow_scope"`
					Problem      string `json:"problem"`
				} `json:"readiness"`
			} `json:"hosts"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v body=%s", err, rr.Body.String())
		}
		if len(resp.Hosts) != 2 {
			t.Fatalf("want 2 hosts, got %d", len(resp.Hosts))
		}
		byName := map[string]int{}
		for i, h := range resp.Hosts {
			byName[h.Name] = i
		}
		main := resp.Hosts[byName["gh-main"]]
		if !main.Readiness.LoggedIn || main.Readiness.Account != "fakeuser" {
			t.Errorf("gh-main readiness = %+v, want logged in as fakeuser", main.Readiness)
		}
		if !main.Readiness.HasRepoScope || !main.Readiness.HasWorkflow {
			t.Errorf("gh-main scopes repo=%v workflow=%v, want both", main.Readiness.HasRepoScope, main.Readiness.HasWorkflow)
		}
		if main.Readiness.Problem != "" {
			t.Errorf("gh-main unexpected problem %q", main.Readiness.Problem)
		}
		vault := resp.Hosts[byName["gh-vault"]]
		if vault.CredentialSource != config.GitCredentialVault || vault.VaultSecret != "gh_vault_token" {
			t.Errorf("gh-vault source=%q secret=%q, want vault and the secret name", vault.CredentialSource, vault.VaultSecret)
		}
		if strings.Contains(rr.Body.String(), vaultTokenValue) {
			t.Errorf("list body leaks the vault token value: %s", rr.Body.String())
		}
	})

	t.Run("test endpoint runs TestHostConnection", func(t *testing.T) {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, gitHostsPath+"/gh-main/test", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Host    string `json:"host"`
			Ok      bool   `json:"ok"`
			Account string `json:"account"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v body=%s", err, rr.Body.String())
		}
		if resp.Host != "gh-main" || !resp.Ok || resp.Account != "fakeuser" {
			t.Errorf("test result = %+v, want ok for gh-main as fakeuser", resp)
		}
		if want := "Connected to github.com as fakeuser."; resp.Message != want {
			t.Errorf("message = %q, want %q", resp.Message, want)
		}
	})

	t.Run("unknown host name is 404", func(t *testing.T) {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, gitHostsPath+"/no-such-host/test", nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status %d, want 404 body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("unknown action under a known host is 404", func(t *testing.T) {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, gitHostsPath+"/gh-main/bogus", nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status %d, want 404", rr.Code)
		}
	})

	t.Run("test endpoint rejects GET", func(t *testing.T) {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, gitHostsPath+"/gh-main/test", nil))
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status %d, want 405", rr.Code)
		}
	})

	t.Run("test response body contains no token value", func(t *testing.T) {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, gitHostsPath+"/gh-vault/test", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), vaultTokenValue) {
			t.Errorf("test body leaks the vault token value: %s", rr.Body.String())
		}
	})
}

// TestGitHostStatusAPI_NoGHOnPath pins the readiness problem text when the gh
// command is missing, so the list never fails outright.
func TestGitHostStatusAPI_NoGHOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := gitHostTestServer(t)
	rr := httptest.NewRecorder()
	gitHostTestMux(s).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, gitHostsPath, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "gh command is not installed") {
		t.Errorf("want missing-gh problem in body, got %s", rr.Body.String())
	}
}

func TestGitHostStatsAPI(t *testing.T) {
	col := stats.NewCollector(t.TempDir())
	col.SetGitHostStatsFunc(func(s *stats.SystemStats) {
		s.GitHosts = &stats.GitHostStats{
			Configured:    2,
			WindowSeconds: 3600,
			Operations:    map[string]int{"push": 3, "merge": 1},
			Failed:        1,
			LastSuccessAt: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go col.Start(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for col.Latest().GitHosts == nil {
		if time.Now().After(deadline) {
			t.Fatal("collector produced no sample")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s := &Server{statsCollector: col}
	mux := http.NewServeMux()
	mux.HandleFunc(gitHostStatsPath, s.handleGitHostStats)

	t.Run("shape", func(t *testing.T) {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, gitHostStatsPath, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"configured", "window_seconds", "operations", "failed", "last_success_at"} {
			if _, ok := got[k]; !ok {
				t.Errorf("missing key %q in %s", k, rr.Body.String())
			}
		}
		if got["configured"].(float64) != 2 || got["failed"].(float64) != 1 || got["window_seconds"].(float64) != 3600 {
			t.Errorf("unexpected values: %s", rr.Body.String())
		}
		if ops := got["operations"].(map[string]any); ops["push"].(float64) != 3 {
			t.Errorf("operations: %v", ops)
		}
	})

	t.Run("no collector gives empty block", func(t *testing.T) {
		empty := &Server{}
		rr := httptest.NewRecorder()
		empty.handleGitHostStats(rr, httptest.NewRequest(http.MethodGet, gitHostStatsPath, nil))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"operations":{}`) {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("other methods are 405", func(t *testing.T) {
		for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(m, gitHostStatsPath, nil))
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s: status %d, want 405", m, rr.Code)
			}
		}
	})
}
