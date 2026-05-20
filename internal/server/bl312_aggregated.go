// BL312 S5 — aggregated fan-out endpoints for Alerts and Automata.
//
//	GET /api/alerts/aggregated         → [{...alert, server:"name"}, ...]
//	GET /api/autonomous/prds/aggregated → [{...prd,   server:"name"}, ...]
//
// Both endpoints fan out in parallel to every enabled server (cfg.Servers +
// runtime store).  Per-server failures are logged and skipped; the caller
// always gets local data even when remotes are unreachable.

package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// handleAggregatedAlerts implements GET /api/alerts/aggregated.
func (s *Server) handleAggregatedAlerts(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapFederationList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Local alerts
	var results []map[string]any
	if s.alertStore != nil {
		for _, a := range s.alertStore.List() {
			b, _ := json.Marshal(a)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			m["server"] = "local"
			results = append(results, m)
		}
	}

	// Fan out to runtime servers
	remotes := s.runtimeServers()
	if len(remotes) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, srv := range remotes {
			wg.Add(1)
			go func(sv config.RemoteServerConfig) {
				defer wg.Done()
				items, err := fetchRemoteJSON(sv.URL, sv.Token, "/api/alerts")
				if err != nil {
					log.Printf("[bl312] aggregated alerts %s: %v", sv.Name, err)
					return
				}
				// items may be {"alerts":[...]} or [...]
				var list []map[string]any
				switch v := items.(type) {
				case []any:
					for _, x := range v {
						if m, ok := x.(map[string]any); ok {
							list = append(list, m)
						}
					}
				case map[string]any:
					if arr, ok := v["alerts"].([]any); ok {
						for _, x := range arr {
							if m, ok2 := x.(map[string]any); ok2 {
								list = append(list, m)
							}
						}
					}
				}
				mu.Lock()
				for _, m := range list {
					m["server"] = sv.Name
					results = append(results, m)
				}
				mu.Unlock()
			}(srv)
		}
		wg.Wait()
	}

	if results == nil {
		results = []map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

// handleAggregatedPRDs implements GET /api/autonomous/prds/aggregated.
func (s *Server) handleAggregatedPRDs(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapFederationList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Local PRDs
	var results []map[string]any
	if s.autonomousMgr != nil {
		for _, p := range s.autonomousMgr.ListPRDs() {
			b, _ := json.Marshal(p)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			m["server"] = "local"
			results = append(results, m)
		}
	}

	// Fan out to runtime servers
	remotes := s.runtimeServers()
	if len(remotes) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, srv := range remotes {
			wg.Add(1)
			go func(sv config.RemoteServerConfig) {
				defer wg.Done()
				items, err := fetchRemoteJSON(sv.URL, sv.Token, "/api/autonomous/prds")
				if err != nil {
					log.Printf("[bl312] aggregated prds %s: %v", sv.Name, err)
					return
				}
				var list []map[string]any
				switch v := items.(type) {
				case []any:
					for _, x := range v {
						if m, ok := x.(map[string]any); ok {
							list = append(list, m)
						}
					}
				case map[string]any:
					if arr, ok := v["prds"].([]any); ok {
						for _, x := range arr {
							if m, ok2 := x.(map[string]any); ok2 {
								list = append(list, m)
							}
						}
					}
				}
				mu.Lock()
				for _, m := range list {
					m["server"] = sv.Name
					results = append(results, m)
				}
				mu.Unlock()
			}(srv)
		}
		wg.Wait()
	}

	if results == nil {
		results = []map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}

// fetchRemoteJSON fetches a JSON endpoint from a remote server.
// Returns the decoded value or an error.
func fetchRemoteJSON(baseURL, token, path string) (any, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	url := strings.TrimRight(baseURL, "/") + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, err
	}
	return v, nil
}
