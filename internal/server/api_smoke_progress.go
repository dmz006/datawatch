package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// smokeRunsDir returns ~/.datawatch/smoke-runs, creating it on demand.
func smokeRunsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".datawatch", "smoke-runs")
	if err := os.MkdirAll(d, 0755); err != nil {
		return "", err
	}
	return d, nil
}

// legacyProgressPath returns the old single-file path for migration.
func legacyProgressPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".datawatch", "smoke-progress.json")
}

// smokeForwardClient is a shared HTTP client for forwarding smoke progress.
var smokeForwardClient = &http.Client{Timeout: 5e9} // 5 s

// smokeForward fires a fire-and-forget POST to the configured forward URL.
func (s *Server) smokeForward(body []byte) {
	if s.smokeForwardURL == "" {
		return
	}
	url := strings.TrimRight(s.smokeForwardURL, "/") + "/api/smoke/progress"
	go func() {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if s.smokeForwardToken != "" {
			req.Header.Set("Authorization", "Bearer "+s.smokeForwardToken)
		}
		resp, err := smokeForwardClient.Do(req)
		if err != nil {
			return
		}
		resp.Body.Close()
	}()
}

// handleSmokeForwardURL — GET/PUT /api/smoke/forward-url
// Allows reading and updating the cross-instance forward URL at runtime (#54).
func (s *Server) handleSmokeForwardURL(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSONOK(w, map[string]any{
			"forward_url":   s.smokeForwardURL,
			"forward_token": s.smokeForwardToken != "",
		})
	case http.MethodPut:
		var req struct {
			ForwardURL   string `json:"forward_url"`
			ForwardToken string `json:"forward_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.smokeForwardURL = req.ForwardURL
		if req.ForwardToken != "" {
			s.smokeForwardToken = req.ForwardToken
		}
		writeJSONOK(w, map[string]any{
			"ok":          true,
			"forward_url": s.smokeForwardURL,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSmokeProgress — multi-envelope smoke run API
//
//	GET  /api/smoke/progress         → array of all run envelopes (newest first)
//	GET  /api/smoke/progress/{id}    → full detail for one run
//	DELETE /api/smoke/progress       → delete ALL runs
//	DELETE /api/smoke/progress/{id}  → delete ONE run
func (s *Server) handleSmokeProgress(w http.ResponseWriter, r *http.Request) {
	// Extract optional run ID from path: /api/smoke/progress/{id}
	// First try Go 1.22+ path value
	runID := r.PathValue("id")
	if runID == "" {
		// Fallback to manual parsing (for non-Go1.22+ routes)
		rest := strings.TrimPrefix(r.URL.Path, "/api/smoke/progress")
		rest = strings.TrimPrefix(rest, "/")
		runID = rest
	}

	runsDir, err := smokeRunsDir()
	if err != nil {
		http.Error(w, "could not access smoke-runs dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Migrate legacy single file into runs dir on first access
	if legacy := legacyProgressPath(); runID == "" {
		if data, err2 := os.ReadFile(legacy); err2 == nil {
			var raw map[string]any
			if json.Unmarshal(data, &raw) == nil {
				id, _ := raw["run_id"].(string)
				if id == "" {
					id = "legacy"
				}
				dest := filepath.Join(runsDir, id+".json")
				if _, statErr := os.Stat(dest); os.IsNotExist(statErr) {
					_ = os.WriteFile(dest, data, 0644)
				}
				_ = os.Remove(legacy)
			}
		}
	}

	switch r.Method {
	case http.MethodPost, http.MethodPut:
		// Write / update a smoke-run envelope.
		// Body: JSON progress object with at least a "run_id" field.
		// POST /api/smoke/progress        — upsert by run_id in body
		// PUT  /api/smoke/progress/{id}   — upsert by path id
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		id := runID
		if id == "" {
			if v, ok := body["run_id"].(string); ok && v != "" {
				id = v
			}
		}
		if id == "" {
			http.Error(w, "run_id required in body or path", http.StatusBadRequest)
			return
		}
		data, _ := json.Marshal(body)
		dest := filepath.Join(runsDir, id+".json")
		if err := os.WriteFile(dest, data, 0644); err != nil {
			http.Error(w, "write failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// #54 — forward to production dashboard if configured.
		s.smokeForward(data)
		writeJSONOK(w, map[string]any{"ok": true, "id": id})

	case http.MethodDelete:
		if runID != "" {
			// Delete single run
			p := filepath.Join(runsDir, runID+".json")
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Delete all runs
		entries, _ := os.ReadDir(runsDir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				_ = os.Remove(filepath.Join(runsDir, e.Name()))
			}
		}
		w.WriteHeader(http.StatusNoContent)

	case http.MethodGet:
		if runID != "" {
			// Return full detail for one run
			p := filepath.Join(runsDir, runID+".json")
			data, err := os.ReadFile(p)
			if err != nil {
				if os.IsNotExist(err) {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(data) //nolint:errcheck
			return
		}

		// Return array of all runs (envelope summary, newest first)
		entries, err := os.ReadDir(runsDir)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]")) //nolint:errcheck
			return
		}

		type envelope struct {
			ID          string  `json:"id"`
			Type        string  `json:"type"`
			Pass        int     `json:"pass"`
			Fail        int     `json:"fail"`
			Skip        int     `json:"skip"`
			Total       int     `json:"total"`
			Active      bool    `json:"active"`
			CurrentName string  `json:"current_name,omitempty"`
			StartedAt   string  `json:"started_at,omitempty"`
			UpdatedAt   string  `json:"updated_at,omitempty"`
			Version     string  `json:"version,omitempty"`
			Pct         float64 `json:"pct"`
		}

		var envelopes []envelope
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, readErr := os.ReadFile(filepath.Join(runsDir, e.Name()))
			if readErr != nil {
				continue
			}
			var raw map[string]any
			if json.Unmarshal(data, &raw) != nil {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".json")
			if v, ok := raw["run_id"].(string); ok && v != "" {
				id = v
			}
			total := 82 // default smoke total
			if v, ok := raw["total"].(float64); ok {
				total = int(v)
			}
			pass, _ := raw["pass"].(float64)
			fail, _ := raw["fail"].(float64)
			skip, _ := raw["skip"].(float64)
			done := int(pass) + int(fail) + int(skip)
			pct := 0.0
			if total > 0 {
				pct = float64(done) / float64(total) * 100
				if pct > 100 {
					pct = 100
				}
			}
			runType, _ := raw["type"].(string)
			if runType == "" {
				runType = "smoke"
			}
			active, _ := raw["active"].(bool)
			currentName, _ := raw["current_name"].(string)
			startedAt, _ := raw["started_at"].(string)
			updatedAt, _ := raw["updated_at"].(string)
			version, _ := raw["version"].(string)

			envelopes = append(envelopes, envelope{
				ID:          id,
				Type:        runType,
				Pass:        int(pass),
				Fail:        int(fail),
				Skip:        int(skip),
				Total:       total,
				Active:      active,
				CurrentName: currentName,
				StartedAt:   startedAt,
				UpdatedAt:   updatedAt,
				Version:     version,
				Pct:         pct,
			})
		}

		// Sort newest first by UpdatedAt then StartedAt
		sort.Slice(envelopes, func(i, j int) bool {
			ti := envelopes[i].UpdatedAt
			if ti == "" {
				ti = envelopes[i].StartedAt
			}
			tj := envelopes[j].UpdatedAt
			if tj == "" {
				tj = envelopes[j].StartedAt
			}
			return ti > tj
		})

		w.Header().Set("Content-Type", "application/json")
		out, _ := json.Marshal(envelopes)
		w.Write(out) //nolint:errcheck

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
