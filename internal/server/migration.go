// v7.0.0-alpha.15 (#229) — surface the v7-migration result so the PWA
// can render a one-time info toast on next load.
//
// GET  /api/migration/status              → reads ~/.datawatch/v7-migration-status.json
// DELETE /api/migration/status            → operator dismisses ("don't show again")
//
// Operator-spec'd 2026-05-09 (Q3 of plan): "BOTH PWA toast AND a
// docs/howto/v7-compute-migration.md walkthrough linked from the
// toast". Toast suppression is server-side via DELETE — the PWA
// reads localStorage too as a belt-and-suspenders client-side
// dismiss, but the daemon source-of-truth ensures the toast only
// shows after a real migration ran.

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/compute"
)

// handleMigrationComputeKinds — v7.0.0-alpha.23 (Q1, Q2):
//
// GET  /api/migration/compute-kinds        → list ComputeNodes whose Kind
//                                             is no longer supported (one of
//                                             local/remote/ssh/docker/k8s/
//                                             remote-proxy). Returns
//                                             {nodes: [{name, current_kind,
//                                             address}], supported_kinds: [...]}.
// PUT  /api/migration/compute-kinds/<name> → body {"kind":"ollama"} —
//                                             validates new kind is in
//                                             supported set + persists.
func (s *Server) handleMigrationComputeKinds(w http.ResponseWriter, r *http.Request) {
	if s.computeReg == nil {
		http.Error(w, "compute registry not wired", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		out := []map[string]any{}
		for _, n := range s.computeReg.List() {
			if n.Kind.IsDeprecated() {
				out = append(out, map[string]any{
					"name":          n.Name,
					"current_kind":  string(n.Kind),
					"address":       n.Address,
				})
			}
		}
		supp := make([]string, 0, len(compute.SupportedKinds))
		for _, k := range compute.SupportedKinds {
			supp = append(supp, string(k))
		}
		writeJSONOK(w, map[string]any{
			"nodes":           out,
			"supported_kinds": supp,
			"count":           len(out),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMigrationComputeKindsUpdate — PUT /api/migration/compute-kinds/<name>
// applies a new Kind to one Node. Validation enforces the supported set
// (refuses re-pick to another deprecated value).
func (s *Server) handleMigrationComputeKindsUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.computeReg == nil {
		http.Error(w, "compute registry not wired", http.StatusServiceUnavailable)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/migration/compute-kinds/")
	if name == "" {
		http.Error(w, "node name required", http.StatusBadRequest)
		return
	}
	var body struct{ Kind string `json:"kind"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	newKind := compute.NodeKind(body.Kind)
	supported := false
	for _, k := range compute.SupportedKinds {
		if k == newKind {
			supported = true
			break
		}
	}
	if !supported {
		http.Error(w, "kind "+body.Kind+" is not in the supported set (use one of "+joinKinds(compute.SupportedKinds)+")", http.StatusBadRequest)
		return
	}
	n, err := s.computeReg.Get(name)
	if err != nil {
		http.Error(w, "node not found: "+name, http.StatusNotFound)
		return
	}
	n.Kind = newKind
	if err := s.computeReg.Update(n); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{"name": name, "kind": string(newKind), "migrated": true})
}

func joinKinds(ks []compute.NodeKind) string {
	out := ""
	for i, k := range ks {
		if i > 0 {
			out += ", "
		}
		out += string(k)
	}
	return out
}

func (s *Server) handleMigrationStatus(w http.ResponseWriter, r *http.Request) {
	dataDir := ""
	if s.cfg != nil {
		dataDir = s.cfg.DataDir
	}
	if dataDir == "" {
		dataDir = "."
	}
	// Expand ~ if present (cfg may carry user-relative path).
	if strings.HasPrefix(dataDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dataDir = filepath.Join(home, dataDir[2:])
		}
	}
	path := filepath.Join(dataDir, "v7-migration-status.json")
	switch r.Method {
	case http.MethodGet:
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSONOK(w, map[string]any{"migrated": []string{}, "show": false})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		doc["show"] = true
		writeJSONOK(w, doc)
	case http.MethodDelete:
		_ = os.Remove(path) // suppress further notice
		writeJSONOK(w, map[string]any{"dismissed": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
