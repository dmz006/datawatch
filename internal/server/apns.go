// BL397 Phase 4 / BL335 — APNs push dispatch REST surface.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/apns"
	"github.com/dmz006/datawatch/internal/devices"
	"github.com/dmz006/datawatch/internal/federation"
)

// handlePushAPNsTest sends a test push to one or all registered APNs
// devices, for verifying config.yaml's push.apns.* setup without
// waiting for a real alert to fire. POST /api/push/apns/test
// Body (optional): {"device_id": "..."} — omit to send to every
// registered APNs device.
func (s *Server) handlePushAPNsTest(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.apnsDispatcher == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "apns not enabled"}) //nolint:errcheck
		return
	}
	if s.deviceStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "device store not available"}) //nolint:errcheck
		return
	}

	var body struct {
		DeviceID string `json:"device_id,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // empty body is valid (send to all)

	var targets []devices.Device
	if body.DeviceID != "" {
		d, err := s.deviceStore.Get(body.DeviceID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "device not found"}) //nolint:errcheck
			return
		}
		if d.Kind != devices.KindAPNS {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "device is not an apns device"}) //nolint:errcheck
			return
		}
		targets = []devices.Device{d}
	} else {
		targets = s.deviceStore.ListByKind(devices.KindAPNS)
	}

	type result struct {
		DeviceID string `json:"device_id"`
		OK       bool   `json:"ok"`
		Error    string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(targets))
	for _, d := range targets {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		title := "datawatch"
		err := s.apnsDispatcher.Send(ctx, d.Token, apns.Payload{
			Aps: apns.ApsPayload{
				Alert: &apns.AlertPayload{Title: title, Body: "Test push from datawatch"},
			},
			Type: "test",
		}, string(d.ApnsEnvironment))
		cancel()
		res := result{DeviceID: d.ID, OK: err == nil}
		if err != nil {
			res.Error = err.Error()
			var apnsErr *apns.ErrAPNs
			if errors.As(err, &apnsErr) && apnsErr.Unregistered() {
				_ = s.deviceStore.Delete(d.ID)
				res.Error += " (device unregistered, removed)"
			}
		}
		results = append(results, res)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"sent": len(results), "results": results}) //nolint:errcheck
}
