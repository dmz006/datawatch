// v5.27.9 (BL213, datawatch#31) — tests for Signal device-linking
// API additions: /api/link/qr alias + DELETE /api/link/<deviceId> +
// parseListDevicesOutput.

package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseListDevicesOutput_TwoDevices(t *testing.T) {
	in := `Device 1:
 Name: primary phone
 Created: 2024-01-15
 Last seen: 2024-04-30
Device 2:
 Name: tablet
 Created: 2024-03-01
 Last seen: 2024-04-29
`
	got := parseListDevicesOutput(in)
	want := []map[string]interface{}{
		{
			"id":        1,
			"name":      "primary phone",
			"created":   "2024-01-15",
			"last_seen": "2024-04-30",
		},
		{
			"id":        2,
			"name":      "tablet",
			"created":   "2024-03-01",
			"last_seen": "2024-04-29",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseListDevicesOutput mismatch\n got:  %#v\n want: %#v", got, want)
	}
}

func TestParseListDevicesOutput_Empty(t *testing.T) {
	got := parseListDevicesOutput("")
	if len(got) != 0 {
		t.Errorf("empty input should produce 0 devices, got %d", len(got))
	}
}

func TestParseListDevicesOutput_PartialBlock(t *testing.T) {
	// Malformed input — only Name field on a single device. Parser
	// is conservative and includes whatever was extracted.
	in := `Device 1:
 Name: only-this-one
`
	got := parseListDevicesOutput(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 device, got %d", len(got))
	}
	if got[0]["id"] != 1 || got[0]["name"] != "only-this-one" {
		t.Errorf("partial-block parse wrong: %+v", got[0])
	}
}

func TestHandleLinkUnlink_RejectsNonDelete(t *testing.T) {
	s := bl90Server(t)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		req := httptest.NewRequest(method, "/api/link/2", nil)
		rr := httptest.NewRecorder()
		s.handleLinkUnlink(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s should be 405, got %d", method, rr.Code)
		}
	}
}

func TestHandleLinkUnlink_RejectsMissingId(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/link/", nil)
	rr := httptest.NewRecorder()
	s.handleLinkUnlink(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("missing id should be 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleLinkUnlink_RejectsNonNumericId(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/link/abc", nil)
	rr := httptest.NewRecorder()
	s.handleLinkUnlink(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("non-numeric id should be 400, got %d", rr.Code)
	}
}

func TestHandleLinkUnlink_RejectsPrimaryDevice(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Signal.AccountNumber = "+15555550100" // operator-configured primary
	req := httptest.NewRequest(http.MethodDelete, "/api/link/1", nil)
	rr := httptest.NewRecorder()
	s.handleLinkUnlink(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("device 1 (primary) removal should be 400, got %d", rr.Code)
	}
}

func TestHandleLinkUnlink_RejectsNoAccountConfigured(t *testing.T) {
	s := bl90Server(t)
	// AccountNumber deliberately empty.
	req := httptest.NewRequest(http.MethodDelete, "/api/link/2", nil)
	rr := httptest.NewRecorder()
	s.handleLinkUnlink(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("missing account_number should be 503, got %d", rr.Code)
	}
}
