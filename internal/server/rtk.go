// BL85 (v4.0.1) — RTK auto-update check + trigger.
//
// Endpoints:
//   GET  /api/rtk/version        return cached VersionStatus
//   POST /api/rtk/check          force a fresh GitHub check + update status
//   POST /api/rtk/update         download + install the latest binary
//
// Wires the pre-existing internal/rtk version-checker + updater into
// the REST surface so the operator doesn't have to shell into the
// host. All three endpoints are bearer-authenticated.

package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/rtk"
)

func (s *Server) handleRTKVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONOK(w, rtk.GetVersionStatus())
}

func (s *Server) handleRTKCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONOK(w, rtk.CheckLatestVersion())
}

func (s *Server) handleRTKUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	newVer, err := rtk.UpdateBinary()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"status": "ok", "version": newVer,
	})
}
