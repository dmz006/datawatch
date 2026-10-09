// Server handlers for /api/devices — closes #1.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/devices"
	"github.com/dmz006/datawatch/internal/federation"
)

// SetDeviceStore wires the device store. Empty/nil disables the
// endpoints (they return 503).
func (s *Server) SetDeviceStore(store *devices.Store) { s.deviceStore = store }

// DeviceStore returns the wired device store, or nil if none is set.
// BL397 Phase 4 — lets the alert-listener closure in main.go reach the
// store without needing its own hoisted variable (devStore in main.go
// is scoped to the if-block that constructs it, well before the
// alert-listener registration).
func (s *Server) DeviceStore() *devices.Store { return s.deviceStore }

// handleDevicesRegister implements POST /api/devices/register per
// issue #1. Body format:
//
//	{ "device_token": "...", "kind": "fcm"|"ntfy"|"apns",
//	  "app_version": "x.y.z", "platform": "android"|"ios",
//	  "profile_hint": "label",
//	  "apns_environment": "production"|"development" }  // apns only, GH#183
//
// Returns {"device_id": "<uuid>"}. Re-registering the same token
// refreshes the metadata without duplicating the record.
func (s *Server) handleDevicesRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if s.deviceStore == nil {
		http.Error(w, "device registration not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		DeviceToken     string `json:"device_token"`
		Kind            string `json:"kind"`
		AppVersion      string `json:"app_version"`
		Platform        string `json:"platform"`
		ProfileHint     string `json:"profile_hint"`
		ApnsEnvironment string `json:"apns_environment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	got, err := s.deviceStore.Register(devices.Device{
		Token:           req.DeviceToken,
		Kind:            devices.Kind(req.Kind),
		AppVersion:      req.AppVersion,
		Platform:        devices.Platform(req.Platform),
		ProfileHint:     req.ProfileHint,
		ApnsEnvironment: devices.ApnsEnvironment(req.ApnsEnvironment),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// GH#201 Phase 4 — who gets paged matters; never logs the raw push
	// token (device_token is credential-shaped, same reasoning as
	// sec017's config-masking rule).
	s.audit(r.Context(), "register", "device", got.ID, map[string]any{
		"kind":     req.Kind,
		"platform": req.Platform,
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"device_id": got.ID})
}

// handleDevicesList implements GET /api/devices (list) and
// DELETE /api/devices/{id} per issue #1.
func (s *Server) handleDevicesList(w http.ResponseWriter, r *http.Request) {
	// Path handling:
	//   /api/devices        → list
	//   /api/devices/{id}   → delete (DELETE only)
	rest := strings.TrimPrefix(r.URL.Path, "/api/devices")
	rest = strings.TrimPrefix(rest, "/")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		if s.deviceStore == nil {
			http.Error(w, "device registration not enabled", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.deviceStore.List())
	case rest != "" && r.Method == http.MethodDelete:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.deviceStore == nil {
			http.Error(w, "device registration not enabled", http.StatusServiceUnavailable)
			return
		}
		if err := s.deviceStore.Delete(rest); err != nil {
			if errors.Is(err, devices.ErrNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.audit(r.Context(), "delete", "device", rest, nil) // GH#201 Phase 4
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
