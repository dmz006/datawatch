// Server handlers for /api/devices — closes #1.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/devices"
)

// SetDeviceStore wires the device store. Empty/nil disables the
// endpoints (they return 503).
func (s *Server) SetDeviceStore(store *devices.Store) { s.deviceStore = store }

// handleDevicesRegister implements POST /api/devices/register per
// issue #1. Body format:
//
//   { "device_token": "...", "kind": "fcm"|"ntfy",
//     "app_version": "x.y.z", "platform": "android"|"ios",
//     "profile_hint": "label" }
//
// Returns {"device_id": "<uuid>"}. Re-registering the same token
// refreshes the metadata without duplicating the record.
func (s *Server) handleDevicesRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.deviceStore == nil {
		http.Error(w, "device registration not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		DeviceToken string `json:"device_token"`
		Kind        string `json:"kind"`
		AppVersion  string `json:"app_version"`
		Platform    string `json:"platform"`
		ProfileHint string `json:"profile_hint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	got, err := s.deviceStore.Register(devices.Device{
		Token:       req.DeviceToken,
		Kind:        devices.Kind(req.Kind),
		AppVersion:  req.AppVersion,
		Platform:    devices.Platform(req.Platform),
		ProfileHint: req.ProfileHint,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"device_id": got.ID})
}

// handleDevicesList implements GET /api/devices (list) and
// DELETE /api/devices/{id} per issue #1.
func (s *Server) handleDevicesList(w http.ResponseWriter, r *http.Request) {
	if s.deviceStore == nil {
		http.Error(w, "device registration not enabled", http.StatusServiceUnavailable)
		return
	}
	// Path handling:
	//   /api/devices        → list
	//   /api/devices/{id}   → delete (DELETE only)
	rest := strings.TrimPrefix(r.URL.Path, "/api/devices")
	rest = strings.TrimPrefix(rest, "/")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.deviceStore.List())
	case rest != "" && r.Method == http.MethodDelete:
		if err := s.deviceStore.Delete(rest); err != nil {
			if errors.Is(err, devices.ErrNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
