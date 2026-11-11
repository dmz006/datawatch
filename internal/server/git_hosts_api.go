// Package server — REST surface for configured git hosts.
//
// GET /api/git/hosts lists every git.hosts entry with its readiness result.
// POST /api/git/hosts/{name}/test runs git.TestHostConnection for one host.
// Neither response carries a token. A vault credential is shown by its secret
// NAME only (vault_secret); the secret value is never read by these handlers.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/stats"
)

// gitHostsPath is the collection route; actions live under gitHostsPath + "/{name}/test".
const gitHostsPath = "/api/git/hosts"

// gitHostStatsPath serves the git host counters from the system stats.
const gitHostStatsPath = gitHostsPath + "/stats"

// gitHostEntry is one row of GET /api/git/hosts.
type gitHostEntry struct {
	Name             string            `json:"name"`
	Kind             string            `json:"kind"`
	BaseURL          string            `json:"base_url,omitempty"`
	CredentialSource string            `json:"credential_source"`
	VaultSecret      string            `json:"vault_secret,omitempty"`
	Readiness        git.HostReadiness `json:"readiness"`
}

// gitHostTestResponse is the body of POST /api/git/hosts/{name}/test.
type gitHostTestResponse struct {
	Host string `json:"host"`
	git.ConnectionResult
}

// configuredGitHosts returns git.hosts from the live config (nil when unset).
func (s *Server) configuredGitHosts() []config.GitHostConfig {
	if s.cfg == nil {
		return nil
	}
	return s.cfg.Git.Hosts
}

// toGitHostCheckConfig converts a config entry into the mirror type the git
// package reads. Only the fields the readiness check needs are copied.
func toGitHostCheckConfig(h config.GitHostConfig) git.GitHostConfig {
	return git.GitHostConfig{
		Name:             h.Name,
		Kind:             h.Kind,
		BaseURL:          h.BaseURL,
		CredentialSource: h.CredentialSource,
		VaultSecret:      h.VaultSecret,
	}
}

// handleGitHosts serves GET /api/git/hosts.
func (s *Server) handleGitHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	hosts := s.configuredGitHosts()
	entries := make([]gitHostEntry, 0, len(hosts))
	for _, h := range hosts {
		entries = append(entries, gitHostEntry{
			Name:             h.Name,
			Kind:             h.Kind,
			BaseURL:          h.BaseURL,
			CredentialSource: h.CredentialSource,
			VaultSecret:      h.VaultSecret,
			Readiness:        git.CheckHostReadiness(r.Context(), toGitHostCheckConfig(h)),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"hosts": entries})
}

// handleGitHostAction serves POST /api/git/hosts/{name}/test. Any other path
// under the prefix is 404; an unknown host name is 404.
func (s *Server) handleGitHostAction(w http.ResponseWriter, r *http.Request) {
	name, action, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, gitHostsPath+"/"), "/")
	if !ok || action != "test" || name == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	hosts := s.configuredGitHosts()
	var target *config.GitHostConfig
	for i := range hosts {
		if hosts[i].Name == name {
			target = &hosts[i]
			break
		}
	}
	if target == nil {
		writeJSONError(w, http.StatusNotFound, "git host not found")
		return
	}
	res := git.TestHostConnection(r.Context(), toGitHostCheckConfig(*target))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(gitHostTestResponse{Host: name, ConnectionResult: res})
}

// handleGitHostStats serves GET /api/git/hosts/stats: the git host counter
// block the system stats carry. With no collector, or no git host configured,
// it answers a zeroed block with an empty operations map rather than an error.
func (s *Server) handleGitHostStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
	out := stats.GitHostStats{Operations: map[string]int{}}
	if s.statsCollector != nil {
		if gh := s.statsCollector.Latest().GitHosts; gh != nil {
			out = *gh
			if out.Operations == nil {
				out.Operations = map[string]int{}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}
