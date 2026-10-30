// Tests for the cleanup REST surface: GET /api/autonomous/cleanup (status
// read, write-method rejection, federation gate) and the config round-trip
// of the four automation cleanup knobs through PUT /api/autonomous/config
// (absent keys keep the current value, present ones are checked strictly:
// cleanup_mode off|report|on, retention hours and per-pass limit whole
// numbers of at least 1, disk alert percent whole in 1..100).

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// cleanupSpy is the double for the cleanup status route and the config
// round-trip. It keeps a JSON config state with SetConfig merging onto the
// current value (as the real Manager.SetConfig does) and serves a fixed
// cleanup status; call counts prove a refused request never reaches it.
type cleanupSpy struct {
	fakeOrchAutonomous
	state  map[string]any
	status autonomous.CleanupStatus
	calls  int
}

func (c *cleanupSpy) Config() any {
	out := map[string]any{}
	for k, v := range c.state {
		out[k] = v
	}
	return out
}

func (c *cleanupSpy) SetConfig(v any) error {
	var patch map[string]any
	switch t := v.(type) {
	case json.RawMessage:
		if err := json.Unmarshal(t, &patch); err != nil {
			return err
		}
	case map[string]any:
		patch = t
	default:
		return errUnsupportedCleanupConfig
	}
	if c.state == nil {
		c.state = map[string]any{}
	}
	for k, val := range patch {
		if sub, ok := val.(map[string]any); ok {
			if cur, ok2 := c.state[k].(map[string]any); ok2 {
				merged := map[string]any{}
				for kk, vv := range cur {
					merged[kk] = vv
				}
				for kk, vv := range sub {
					merged[kk] = vv
				}
				c.state[k] = merged
				continue
			}
		}
		c.state[k] = val
	}
	return nil
}

func (c *cleanupSpy) CleanupStatus() autonomous.CleanupStatus {
	c.calls++
	return c.status
}

var errUnsupportedCleanupConfig = errors.New("cleanupSpy: unsupported config payload")

// cleanupRequest hits the named route on s without a network hop.
func cleanupRequest(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, rdr)
	switch path {
	case "/api/autonomous/cleanup":
		s.handleCleanupStatus(rec, req)
	case "/api/autonomous/config":
		s.handleAutonomousConfig(rec, req)
	default:
		t.Fatalf("unknown cleanup route %s", path)
	}
	return rec
}

// cleanupCfgServer builds a Server whose config PUT persists to a temp
// path, so the handler's config.Save round-trip is exercised for real.
func cleanupCfgServer(t *testing.T, spy *cleanupSpy) *Server {
	t.Helper()
	return &Server{
		autonomousMgr: spy,
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
	}
}

func cleanupFixtureStatus() autonomous.CleanupStatus {
	lastRun := time.Date(2026, 11, 1, 8, 30, 0, 0, time.UTC)
	fired := lastRun.Add(2 * time.Hour)
	pct := 87
	return autonomous.CleanupStatus{
		Worktrees: autonomous.CleanupPassStatus{
			Mode: "report", LastRun: &lastRun,
			Removed: 3, WouldRemove: 1, Skipped: 2, Failed: 1,
			BytesFreed: 123456789, LastError: "permission denied",
			OwnerRemoved: 2, NetRemoved: 1,
			OwnerBytesFreed: 100000000, NetBytesFreed: 23456789,
		},
		Temp:             autonomous.CleanupPassStatus{Mode: "report"},
		DiskAlertPercent: 85,
		WorktreeDisk:     &autonomous.CleanupDiskCheck{Path: "/data", Percent: &pct, Alert: true},
		DiskAlert:        true,
		LastDiskAlertAt:  &fired,
	}
}

func TestCleanupRoute_StatusShape(t *testing.T) {
	spy := &cleanupSpy{status: cleanupFixtureStatus()}
	s := &Server{autonomousMgr: spy}
	rec := cleanupRequest(t, s, http.MethodGet, "/api/autonomous/cleanup", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	got := make([]string, 0, len(body))
	for k := range body {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"disk_alert", "disk_alert_percent", "last_disk_alert_at", "temp", "worktree_disk", "worktrees"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("top-level keys = %v, want %v", got, want)
	}
	wt := body["worktrees"].(map[string]any)
	if wt["mode"] != "report" || wt["removed"] != float64(3) || wt["would_remove"] != float64(1) ||
		wt["skipped"] != float64(2) || wt["failed"] != float64(1) || wt["bytes_freed"] != float64(123456789) ||
		wt["last_error"] != "permission denied" || wt["owner_removed"] != float64(2) || wt["net_removed"] != float64(1) ||
		wt["owner_bytes_freed"] != float64(100000000) || wt["net_bytes_freed"] != float64(23456789) {
		t.Fatalf("worktrees pass wrong: %v", wt)
	}
	if wt["last_run"] == nil {
		t.Fatalf("worktrees.last_run must be set: %v", wt)
	}
	tp := body["temp"].(map[string]any)
	if tp["mode"] != "report" || tp["removed"] != float64(0) {
		t.Fatalf("temp pass wrong: %v", tp)
	}
	if disk := body["worktree_disk"].(map[string]any); disk["path"] != "/data" || disk["percent"] != float64(87) || disk["alert"] != true {
		t.Fatalf("worktree_disk wrong: %v", disk)
	}
	if body["disk_alert"] != true || body["disk_alert_percent"] != float64(85) {
		t.Fatalf("disk alert fields wrong: alert=%v pct=%v", body["disk_alert"], body["disk_alert_percent"])
	}
	if spy.calls != 1 {
		t.Fatalf("status reads = %d, want 1", spy.calls)
	}
}

func TestCleanupRoute_WriteMethodsRejected(t *testing.T) {
	spy := &cleanupSpy{status: cleanupFixtureStatus()}
	s := &Server{autonomousMgr: spy}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := cleanupRequest(t, s, m, "/api/autonomous/cleanup", `{"cleanup_mode":"on"}`)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: status = %d, want 405: %s", m, rec.Code, rec.Body)
		}
		if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
			t.Fatalf("%s: Allow = %q, want GET", m, allow)
		}
	}
	if spy.calls != 0 {
		t.Fatalf("status reads = %d, want 0 (write attempts must not reach the manager)", spy.calls)
	}
}

func TestCleanupRoute_AutonomousDisabled(t *testing.T) {
	s := &Server{}
	rec := cleanupRequest(t, s, http.MethodGet, "/api/autonomous/cleanup", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
	}
}

func TestCleanupRoute_PeerWithoutCapability(t *testing.T) {
	spy := &cleanupSpy{status: cleanupFixtureStatus()}
	s := &Server{autonomousMgr: spy}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/cleanup", nil)
	peer := &multiserver.Entry{Name: "read-only-peer", Enabled: true, Federated: true}
	req = req.WithContext(context.WithValue(req.Context(), fedPeerKey, peer))
	s.handleCleanupStatus(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "autonomous:read") {
		t.Fatalf("error must name the required capability: %s", rec.Body)
	}
	if spy.calls != 0 {
		t.Fatalf("status reads = %d, want 0 (refused request must not reach the manager)", spy.calls)
	}
}

func TestCleanupConfigRoundTrip(t *testing.T) {
	spy := &cleanupSpy{state: map[string]any{
		"enabled": true,
		"automation": map[string]any{
			"cleanup_mode":                  "report",
			"cleanup_retention_hours":       float64(24),
			"cleanup_max_removals_per_pass": float64(10),
			"disk_alert_percent":            float64(85),
		},
	}}
	s := cleanupCfgServer(t, spy)

	// PUT the four knobs; response is status + warnings only.
	rec := cleanupRequest(t, s, http.MethodPut, "/api/autonomous/config",
		`{"enabled":true,"automation":{"cleanup_mode":"on","cleanup_retention_hours":48,"cleanup_max_removals_per_pass":5,"disk_alert_percent":70}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var putResp struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &putResp); err != nil {
		t.Fatalf("decode PUT response: %v: %s", err, rec.Body)
	}
	if putResp.Status != "ok" || len(putResp.Warnings) != 0 {
		t.Fatalf("PUT response = %+v, want status ok and no warnings", putResp)
	}

	// GET reads back exactly what was written.
	rec = cleanupRequest(t, s, http.MethodGet, "/api/autonomous/config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got struct {
		Automation struct {
			CleanupMode               string   `json:"cleanup_mode"`
			CleanupRetentionHours     *float64 `json:"cleanup_retention_hours"`
			CleanupMaxRemovalsPerPass *float64 `json:"cleanup_max_removals_per_pass"`
			DiskAlertPercent          *float64 `json:"disk_alert_percent"`
		} `json:"automation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET response: %v: %s", err, rec.Body)
	}
	auto := got.Automation
	if auto.CleanupMode != "on" || auto.CleanupRetentionHours == nil || *auto.CleanupRetentionHours != 48 ||
		auto.CleanupMaxRemovalsPerPass == nil || *auto.CleanupMaxRemovalsPerPass != 5 ||
		auto.DiskAlertPercent == nil || *auto.DiskAlertPercent != 70 {
		t.Fatalf("readback = %q %v %v %v, want on 48 5 70",
			auto.CleanupMode, auto.CleanupRetentionHours, auto.CleanupMaxRemovalsPerPass, auto.DiskAlertPercent)
	}

	// The daemon config followed the manager (GH#180 sync path): the
	// Effective* accessors must now see the written values.
	a := s.cfg.Autonomous.Automation
	if a.EffectiveCleanupMode() != "on" || a.EffectiveCleanupRetentionHours() != 48 ||
		a.EffectiveCleanupMaxRemovalsPerPass() != 5 || a.EffectiveDiskAlertPercent() != 70 {
		t.Fatalf("effective = %q %d %d %d, want on 48 5 70",
			a.EffectiveCleanupMode(), a.EffectiveCleanupRetentionHours(), a.EffectiveCleanupMaxRemovalsPerPass(), a.EffectiveDiskAlertPercent())
	}
}

func TestCleanupConfigAbsentKeysKeepCurrent(t *testing.T) {
	spy := &cleanupSpy{state: map[string]any{
		"enabled": true,
		"automation": map[string]any{
			"cleanup_mode":                  "on",
			"cleanup_retention_hours":       float64(48),
			"cleanup_max_removals_per_pass": float64(5),
			"disk_alert_percent":            float64(70),
		},
	}}
	s := cleanupCfgServer(t, spy)
	rec := cleanupRequest(t, s, http.MethodPut, "/api/autonomous/config", `{"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: %s", rec.Code, rec.Body)
	}
	// A single-key PUT changes only that key.
	rec = cleanupRequest(t, s, http.MethodPut, "/api/autonomous/config", `{"automation":{"cleanup_mode":"report"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: %s", rec.Code, rec.Body)
	}
	auto := spy.state["automation"].(map[string]any)
	if auto["cleanup_mode"] != "report" || auto["cleanup_retention_hours"] != float64(48) ||
		auto["cleanup_max_removals_per_pass"] != float64(5) || auto["disk_alert_percent"] != float64(70) {
		t.Fatalf("state after partial PUTs = %v, want mode=report and the other three untouched", auto)
	}
}

func TestCleanupConfigValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty_mode", `{"automation":{"cleanup_mode":""}}`},
		{"unknown_mode", `{"automation":{"cleanup_mode":"destroy"}}`},
		{"numeric_mode", `{"automation":{"cleanup_mode":5}}`},
		{"array_mode", `{"automation":{"cleanup_mode":["on"]}}`},
		{"zero_retention", `{"automation":{"cleanup_retention_hours":0}}`},
		{"negative_retention", `{"automation":{"cleanup_retention_hours":-4}}`},
		{"fractional_retention", `{"automation":{"cleanup_retention_hours":1.5}}`},
		{"string_retention", `{"automation":{"cleanup_retention_hours":"24h"}}`},
		{"zero_max_removals", `{"automation":{"cleanup_max_removals_per_pass":0}}`},
		{"fractional_max_removals", `{"automation":{"cleanup_max_removals_per_pass":2.5}}`},
		{"zero_percent", `{"automation":{"disk_alert_percent":0}}`},
		{"percent_above_100", `{"automation":{"disk_alert_percent":101}}`},
		{"fractional_percent", `{"automation":{"disk_alert_percent":55.5}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &cleanupSpy{state: map[string]any{
				"automation": map[string]any{"cleanup_mode": "report"},
			}}
			s := cleanupCfgServer(t, spy)
			rec := cleanupRequest(t, s, http.MethodPut, "/api/autonomous/config", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), `"status":"ok"`) {
				t.Fatalf("refused PUT must not report ok: %s", rec.Body)
			}
			auto := spy.state["automation"].(map[string]any)
			if auto["cleanup_mode"] != "report" {
				t.Fatalf("rejected PUT must not change state: %v", auto)
			}
		})
	}
}

func TestCleanupConfigValidBoundariesAccepted(t *testing.T) {
	rec := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		spy := &cleanupSpy{}
		s := cleanupCfgServer(t, spy)
		return cleanupRequest(t, s, http.MethodPut, "/api/autonomous/config", body)
	}
	if r := rec(t, `{"automation":{"cleanup_mode":"off","cleanup_retention_hours":1,"cleanup_max_removals_per_pass":1,"disk_alert_percent":1}}`); r.Code != http.StatusOK {
		t.Fatalf("lower boundary: status = %d, want 200: %s", r.Code, r.Body)
	}
	if r := rec(t, `{"automation":{"cleanup_mode":"report","disk_alert_percent":100}}`); r.Code != http.StatusOK {
		t.Fatalf("upper boundary: status = %d, want 200: %s", r.Code, r.Body)
	}
	if r := rec(t, `{"automation":{"cleanup_mode":"on","cleanup_retention_hours":240,"cleanup_max_removals_per_pass":100}}`); r.Code != http.StatusOK {
		t.Fatalf("large values: status = %d, want 200: %s", r.Code, r.Body)
	}
}
