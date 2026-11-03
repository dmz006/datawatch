package server

// git.secret_scan (v9.0.0) — config surface tests. The pre-push secret scan
// runs in internal/git; this file covers the REST surface every other
// surface funnels through: GET /api/config reports the effective
// git.secret_scan values and PUT /api/config (applyConfigPatch) accepts
// them as JSON arrays or comma-separated strings, with mode and
// fingerprint allow-list entries pre-validated (a bad value rejects the
// whole PUT before anything is applied).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func secretScanGET(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Git map[string]any `json:"git"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	sec, ok := got.Git["secret_scan"].(map[string]any)
	if !ok {
		t.Fatalf("git.secret_scan: absent from the git block: %s", rr.Body.String())
	}
	return sec
}

func secretScanPUT(t *testing.T, s *Server, patch map[string]any) int {
	t.Helper()
	b, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
	return rr.Code
}

func TestApplyConfigPatch_GitSecretScan(t *testing.T) {
	cfg := config.DefaultConfig()
	// CLI config set / MCP config_set / comm configure all funnel into
	// applyConfigPatch with JSON-decoded values.
	applyConfigPatch(cfg, map[string]interface{}{
		"git.secret_scan.mode":               "enforce",
		"git.secret_scan.detectors":          []interface{}{"private_key", "github_token"},
		"git.secret_scan.allow_list.paths":   "vendor, testdata",
		"git.secret_scan.allow_list.fingerprints": []interface{}{"0123456789abcdef"},
	})
	if cfg.Git.SecretScan.Mode != config.GitSecretScanEnforce {
		t.Errorf("mode: got %q want enforce", cfg.Git.SecretScan.Mode)
	}
	if len(cfg.Git.SecretScan.Detectors) != 2 || cfg.Git.SecretScan.Detectors[0] != "private_key" {
		t.Errorf("detectors: got %v", cfg.Git.SecretScan.Detectors)
	}
	if len(cfg.Git.SecretScan.AllowList.Paths) != 2 || cfg.Git.SecretScan.AllowList.Paths[1] != "testdata" {
		t.Errorf("allow_list.paths: got %v", cfg.Git.SecretScan.AllowList.Paths)
	}
	if len(cfg.Git.SecretScan.AllowList.Fingerprints) != 1 || cfg.Git.SecretScan.AllowList.Fingerprints[0] != "0123456789abcdef" {
		t.Errorf("allow_list.fingerprints: got %v", cfg.Git.SecretScan.AllowList.Fingerprints)
	}
	// Empty values clear back to the effective defaults (shadow mode,
	// all built-in detectors, no allow-list).
	applyConfigPatch(cfg, map[string]interface{}{
		"git.secret_scan.mode":                 "off",
		"git.secret_scan.detectors":            "",
		"git.secret_scan.allow_list.paths":     "",
		"git.secret_scan.allow_list.fingerprints": []interface{}{},
	})
	if cfg.Git.SecretScan.Mode != config.GitSecretScanOff {
		t.Errorf("mode reset: got %q want off", cfg.Git.SecretScan.Mode)
	}
	if dets := cfg.Git.EffectiveSecretScanDetectors(); dets == nil || len(dets) != len(config.GitSecretScanDefaultDetectors) {
		t.Errorf("detectors reset: got %v want the %d built-ins", cfg.Git.SecretScan.Detectors, len(config.GitSecretScanDefaultDetectors))
	}
	if len(cfg.Git.SecretScan.AllowList.Paths) != 0 || len(cfg.Git.SecretScan.AllowList.Fingerprints) != 0 {
		t.Errorf("allow-lists reset: paths=%v fps=%v", cfg.Git.SecretScan.AllowList.Paths, cfg.Git.SecretScan.AllowList.Fingerprints)
	}
}

func TestHandleGetConfig_GitSecretScanDefaults(t *testing.T) {
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}
	sec := secretScanGET(t, s)
	// Unset config shows its effective defaults: shadow, all built-in
	// detectors, empty allow-lists.
	if sec["mode"] != config.GitSecretScanShadow {
		t.Errorf("mode default: got %v want shadow", sec["mode"])
	}
	dets, ok := sec["detectors"].([]any)
	if !ok || len(dets) != len(config.GitSecretScanDefaultDetectors) {
		t.Errorf("detectors default: got %v want the %d built-ins", sec["detectors"], len(config.GitSecretScanDefaultDetectors))
	}
	al, ok := sec["allow_list"].(map[string]any)
	if !ok {
		t.Fatalf("allow_list: absent: %v", sec)
	}
	if paths, _ := al["paths"].([]any); len(paths) != 0 {
		t.Errorf("allow_list.paths default: got %v want empty", al["paths"])
	}
	if fps, _ := al["fingerprints"].([]any); len(fps) != 0 {
		t.Errorf("allow_list.fingerprints default: got %v want empty", al["fingerprints"])
	}
}

func TestHandlePutConfig_GitSecretScanRoundTrip(t *testing.T) {
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}
	if code := secretScanPUT(t, s, map[string]any{
		"git.secret_scan.mode":                 "enforce",
		"git.secret_scan.detectors":            "private_key, github_token",
		"git.secret_scan.allow_list.paths":     []any{"vendor"},
		"git.secret_scan.allow_list.fingerprints": "0123456789abcdef",
	}); code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d", code)
	}
	if s.cfg.Git.SecretScan.Mode != config.GitSecretScanEnforce {
		t.Errorf("mode: got %q", s.cfg.Git.SecretScan.Mode)
	}
	sec := secretScanGET(t, s)
	if sec["mode"] != config.GitSecretScanEnforce {
		t.Errorf("GET mode: got %v", sec["mode"])
	}
	// Case-insensitive mode, trimmed.
	if code := secretScanPUT(t, s, map[string]any{"git.secret_scan.mode": " SHADOW "}); code != http.StatusOK {
		t.Fatalf("PUT mode=SHADOW: want 200, got %d", code)
	}
	if s.cfg.Git.SecretScan.Mode != config.GitSecretScanShadow {
		t.Errorf("mode: got %q want shadow", s.cfg.Git.SecretScan.Mode)
	}
}

func TestHandlePutConfig_GitSecretScanModeRejected(t *testing.T) {
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}
	// An unknown mode rejects the whole PUT — including any other key in
	// the same patch — leaving the stored value untouched.
	if code := secretScanPUT(t, s, map[string]any{
		"git.secret_scan.mode":              "paranoid",
		"git.secret_scan.allow_list.paths":  "vendor",
	}); code != http.StatusBadRequest {
		t.Fatalf("PUT mode=paranoid: want 400, got %d", code)
	}
	if s.cfg.Git.SecretScan.Mode != "" || len(s.cfg.Git.SecretScan.AllowList.Paths) != 0 {
		t.Errorf("rejected PUT must not apply anything: mode=%q paths=%v", s.cfg.Git.SecretScan.Mode, s.cfg.Git.SecretScan.AllowList.Paths)
	}
	// A non-string mode is rejected too (JSON decode delivers e.g. a number).
	if code := secretScanPUT(t, s, map[string]any{"git.secret_scan.mode": 42}); code != http.StatusBadRequest {
		t.Fatalf("PUT mode=42: want 400, got %d", code)
	}
	// The three known modes are accepted.
	for _, m := range []string{config.GitSecretScanOff, config.GitSecretScanShadow, config.GitSecretScanEnforce} {
		if code := secretScanPUT(t, s, map[string]any{"git.secret_scan.mode": m}); code != http.StatusOK {
			t.Errorf("PUT mode=%s: want 200, got %d", m, code)
		}
	}
}

func TestHandlePutConfig_GitSecretScanFingerprintsRejected(t *testing.T) {
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}
	// Fingerprint entries must be 16- or 64-char hex; anything else
	// (garbage, wrong length, non-hex) rejects the whole PUT.
	for _, bad := range []any{"nothex", "0123456789abcde", "0123456789abcdef0", strings.Repeat("0", 15), "g123456789abcdef"} {
		if code := secretScanPUT(t, s, map[string]any{"git.secret_scan.allow_list.fingerprints": bad}); code != http.StatusBadRequest {
			t.Errorf("PUT fingerprints=%v: want 400, got %d", bad, code)
		}
	}
	if len(s.cfg.Git.SecretScan.AllowList.Fingerprints) != 0 {
		t.Errorf("rejected PUTs must not apply: fps=%v", s.cfg.Git.SecretScan.AllowList.Fingerprints)
	}
	// Short 16-hex and full 64-hex both stick.
	if code := secretScanPUT(t, s, map[string]any{
		"git.secret_scan.allow_list.fingerprints": "0123456789abcdef, " + strings.Repeat("ab", 32),
	}); code != http.StatusOK {
		t.Fatalf("PUT valid fingerprints: want 200, got %d", code)
	}
	if fps := s.cfg.Git.SecretScan.AllowList.Fingerprints; len(fps) != 2 || fps[0] != "0123456789abcdef" || len(fps[1]) != 64 {
		t.Errorf("fingerprints: got %v", fps)
	}
}

func TestIsSecretFingerprint(t *testing.T) {
	good := []string{"0123456789abcdef", "0123456789ABCDEF", strings.Repeat("f", 64)}
	for _, g := range good {
		if !isSecretFingerprint(g) {
			t.Errorf("isSecretFingerprint(%q): want true", g)
		}
	}
	bad := []string{"", "0123456789abcde", "0123456789abcdef0", "0123456789abcdeg", "secret"}
	for _, b := range bad {
		if isSecretFingerprint(b) {
			t.Errorf("isSecretFingerprint(%q): want false", b)
		}
	}
}
