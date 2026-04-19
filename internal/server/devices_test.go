// Issue #1 — REST handler tests for /api/devices.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/devices"
)

func deviceTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, err := devices.NewStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Server{deviceStore: store}
}

func TestRegisterEndpoint_HappyPath(t *testing.T) {
	s := deviceTestServer(t)
	body := bytes.NewBufferString(`{"device_token":"fcm-abc","kind":"fcm","platform":"android","app_version":"0.2.0"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/devices/register", body)
	rr := httptest.NewRecorder()
	s.handleDevicesRegister(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		DeviceID string `json:"device_id"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.DeviceID == "" {
		t.Error("device_id missing")
	}
}

func TestRegisterEndpoint_NoStore_503(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/devices/register",
		bytes.NewBufferString(`{"device_token":"x","kind":"fcm"}`))
	rr := httptest.NewRecorder()
	s.handleDevicesRegister(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rr.Code)
	}
}

func TestRegisterEndpoint_ValidationError_400(t *testing.T) {
	s := deviceTestServer(t)
	body := bytes.NewBufferString(`{"device_token":"","kind":"fcm"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/devices/register", body)
	rr := httptest.NewRecorder()
	s.handleDevicesRegister(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestListEndpoint_Empty(t *testing.T) {
	s := deviceTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	rr := httptest.NewRecorder()
	s.handleDevicesList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestListEndpoint_DeleteByID(t *testing.T) {
	s := deviceTestServer(t)
	d, _ := s.deviceStore.Register(devices.Device{Token: "x", Kind: devices.KindFCM})

	req := httptest.NewRequest(http.MethodDelete, "/api/devices/"+d.ID, nil)
	rr := httptest.NewRecorder()
	s.handleDevicesList(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status=%d want 200", rr.Code)
	}
	if len(s.deviceStore.List()) != 0 {
		t.Error("list not empty after delete")
	}
}

func TestListEndpoint_DeleteNotFound_404(t *testing.T) {
	s := deviceTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/devices/ghost", nil)
	rr := httptest.NewRecorder()
	s.handleDevicesList(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestListEndpoint_RejectsPutOnCollection(t *testing.T) {
	s := deviceTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/devices", nil)
	rr := httptest.NewRecorder()
	s.handleDevicesList(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
}
