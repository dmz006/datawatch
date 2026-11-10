// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3b) — handleSmokeProgress joined an unvalidated run id directly into a
// filesystem path (filepath.Join(runsDir, id+".json")) on every method.
// The POST body-sourced "run_id" was the actually-reachable gap (query/body
// values are never touched by ServeMux's path cleaning, unlike the other
// §3b findings sourced from a URL path segment); the path-sourced id is
// validated too, for defense in depth, same as the rest of this review.
//
// Also covers the companion capability-model fix: both handlers were
// previously gated entirely by CapAnalyticsRead, a capability hand out to
// broadly-distributed read-only presets (monitor, analytics-viewer,
// read-only) -- meaning any of those could write/delete arbitrary
// *.json-suffixed paths. Write methods (POST/PUT/DELETE on
// /api/smoke/progress, PUT on /api/smoke/forward-url) now additionally
// require the new CapAnalyticsWrite, granted to a new "smoke-reporter"
// preset and to full-control; GET keeps the original CapAnalyticsRead.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// smokeTestServer isolates handleSmokeProgress's data-directory-rooted
// storage (<data_dir>/smoke-runs) into a per-test temp dir. These Servers
// carry no data-dir override, so the default data directory resolves from
// $HOME at call time (a "~/.datawatch" value), which is what t.Setenv pins.
func smokeTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return newTestServer(t, nil, nil)
}

// asPeer tags req's context with a federated peer holding exactly caps
// (group names or individual capability strings), so fedCap enforces the
// real CBAC check instead of the admin (nil-peer) bypass every other test
// in this file relies on.
func asPeer(req *http.Request, caps ...string) *http.Request {
	peer := &multiserver.Entry{Federated: true, Capabilities: caps}
	return req.WithContext(context.WithValue(req.Context(), fedPeerKey, peer))
}

func postSmokeProgress(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handleSmokeProgress(rr, httptest.NewRequest(http.MethodPost, "/api/smoke/progress", bytes.NewReader(b)))
	return rr
}

func TestSmokeProgress_RejectsTraversalInBodyRunID(t *testing.T) {
	s := smokeTestServer(t)
	rr := postSmokeProgress(t, s, map[string]any{"run_id": "../../../../tmp/evil"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rr.Code, rr.Body.String())
	}
	// Confirm nothing escaped the intended directory.
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, "evil.json")); !os.IsNotExist(err) {
		t.Fatalf("traversal payload reached the filesystem outside smoke-runs/: err=%v", err)
	}
}

func TestSmokeProgress_RejectsTraversalInPathID(t *testing.T) {
	s := smokeTestServer(t)
	rr := httptest.NewRecorder()
	// Bypasses ServeMux's own '..' cleaning by calling the handler
	// directly (as every other test in this package does) -- still must
	// be rejected by the handler's own guard, not just rely on ServeMux.
	req := httptest.NewRequest(http.MethodPut, "/api/smoke/progress/../../../../tmp/evil2", bytes.NewReader([]byte(`{}`)))
	s.handleSmokeProgress(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rr.Code, rr.Body.String())
	}
}

func TestSmokeProgress_RejectsTraversalOnDelete(t *testing.T) {
	s := smokeTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/smoke/progress/../../../../tmp/evil3", nil)
	s.handleSmokeProgress(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rr.Code, rr.Body.String())
	}
}

func TestSmokeProgress_LegitimateRunIDStillWorks(t *testing.T) {
	s := smokeTestServer(t)
	rr := postSmokeProgress(t, s, map[string]any{"run_id": "run-2026-10-04-abc123", "pass": float64(3)})
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rr.Code, rr.Body.String())
	}

	getRR := httptest.NewRecorder()
	s.handleSmokeProgress(getRR, httptest.NewRequest(http.MethodGet, "/api/smoke/progress/run-2026-10-04-abc123", nil))
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s, want 200", getRR.Code, getRR.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(getRR.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["run_id"] != "run-2026-10-04-abc123" {
		t.Errorf("run_id=%v, want run-2026-10-04-abc123", got["run_id"])
	}

	delRR := httptest.NewRecorder()
	s.handleSmokeProgress(delRR, httptest.NewRequest(http.MethodDelete, "/api/smoke/progress/run-2026-10-04-abc123", nil))
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d, want 204", delRR.Code)
	}
}

func TestSmokeProgress_PostedRunAppearsInList(t *testing.T) {
	s := smokeTestServer(t)

	post := postSmokeProgress(t, s, map[string]any{
		"run_id": "run-live-1", "type": "smoke", "version": "v9.99.0",
		"pass": float64(5), "fail": float64(1), "skip": float64(0), "total": float64(8),
		"active": true, "started_at": "2026-11-09T00:00:00Z",
	})
	if post.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s, want 200", post.Code, post.Body.String())
	}

	// The card reads the LIST (array) endpoint, not the single-run one: confirm
	// the posted run is present there with its computed progress fields intact.
	listRR := httptest.NewRecorder()
	s.handleSmokeProgress(listRR, httptest.NewRequest(http.MethodGet, "/api/smoke/progress", nil))
	if listRR.Code != http.StatusOK {
		t.Fatalf("GET list status=%d body=%s, want 200", listRR.Code, listRR.Body.String())
	}
	var list []map[string]any
	if err := json.Unmarshal(listRR.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var found *map[string]any
	for i := range list {
		if list[i]["id"] == "run-live-1" { found = &list[i]; break }
	}
	if found == nil {
		t.Fatalf("run-live-1 not in list of %d: %s", len(list), listRR.Body.String())
	}
	got := *found
	if got["type"] != "smoke" { t.Errorf("type=%v, want smoke", got["type"]) }
	if got["version"] != "v9.99.0" { t.Errorf("version=%v, want v9.99.0", got["version"]) }
	if got["pass"] != float64(5) { t.Errorf("pass=%v, want 5", got["pass"]) }
	if got["fail"] != float64(1) { t.Errorf("fail=%v, want 1", got["fail"]) }
	if got["active"] != true { t.Errorf("active=%v, want true", got["active"]) }
	// 6 of 8 checks settled (5 pass + 1 fail) = 75%, computed server-side for the card.
	if got["pct"] != float64(75) { t.Errorf("pct=%v, want 75", got["pct"]) }
}

func TestSmokeProgress_MonitorPeerCanReadButNotWrite(t *testing.T) {
	s := smokeTestServer(t)

	getRR := httptest.NewRecorder()
	s.handleSmokeProgress(getRR, asPeer(httptest.NewRequest(http.MethodGet, "/api/smoke/progress", nil), "monitor"))
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET (monitor) status=%d body=%s, want 200", getRR.Code, getRR.Body.String())
	}

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rr := httptest.NewRecorder()
		req := asPeer(httptest.NewRequest(m, "/api/smoke/progress", bytes.NewReader([]byte(`{"run_id":"x"}`))), "monitor")
		s.handleSmokeProgress(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s (monitor) status=%d body=%s, want 403 (monitor is read-only)", m, rr.Code, rr.Body.String())
		}
	}
}

func TestSmokeProgress_SmokeReporterPeerCanWrite(t *testing.T) {
	s := smokeTestServer(t)
	rr := httptest.NewRecorder()
	req := asPeer(httptest.NewRequest(http.MethodPost, "/api/smoke/progress", bytes.NewReader([]byte(`{"run_id":"from-reporter"}`))), "smoke-reporter")
	s.handleSmokeProgress(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST (smoke-reporter) status=%d body=%s, want 200", rr.Code, rr.Body.String())
	}
}

func TestSmokeForwardURL_MonitorPeerCanReadButNotWrite(t *testing.T) {
	s := smokeTestServer(t)

	getRR := httptest.NewRecorder()
	s.handleSmokeForwardURL(getRR, asPeer(httptest.NewRequest(http.MethodGet, "/api/smoke/forward-url", nil), "monitor"))
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET (monitor) status=%d body=%s, want 200", getRR.Code, getRR.Body.String())
	}

	putRR := httptest.NewRecorder()
	putReq := asPeer(httptest.NewRequest(http.MethodPut, "/api/smoke/forward-url", bytes.NewReader([]byte(`{"forward_url":"http://evil"}`))), "monitor")
	s.handleSmokeForwardURL(putRR, putReq)
	if putRR.Code != http.StatusForbidden {
		t.Errorf("PUT (monitor) status=%d body=%s, want 403 (monitor is read-only)", putRR.Code, putRR.Body.String())
	}
}

func TestSmokeForwardURL_SmokeReporterPeerCanWrite(t *testing.T) {
	s := smokeTestServer(t)
	rr := httptest.NewRecorder()
	req := asPeer(httptest.NewRequest(http.MethodPut, "/api/smoke/forward-url", bytes.NewReader([]byte(`{"forward_url":"http://prod-dashboard"}`))), "smoke-reporter")
	s.handleSmokeForwardURL(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT (smoke-reporter) status=%d body=%s, want 200", rr.Code, rr.Body.String())
	}
	if s.smokeForwardURL != "http://prod-dashboard" {
		t.Errorf("smokeForwardURL=%q, want http://prod-dashboard", s.smokeForwardURL)
	}
}
