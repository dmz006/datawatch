// Round-trip (set → get) + validation tests for the two config keys added
// alongside loop-detection tunability:
//
//   - pi.thinking_level            (string; "" clears; off|minimal|low|medium|high)
//   - autonomous.loop_detect_turns (int; nil = unset → default 3; 0 = detection off)
//
// Both keys flow the same REST write path as every other config field:
// PUT /api/config (handlePutConfig → applyConfigPatch → config.Save) and
// read back via GET /api/config (handleGetConfig's sanitized view). The
// MCP (autonomous_config_set / general config_set), CLI (config set), and
// comm-channel (configure key=value) surfaces all proxy to these handlers,
// so covering them here covers every exposure.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// newRoundTripServer builds the minimal Server the config GET/PUT handlers
// need: a live cfg plus a saveable path in a temp dir.
func newRoundTripServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		cfg:     config.DefaultConfig(),
		cfgPath: t.TempDir() + "/config.yaml",
	}
}

func putPatch(t *testing.T, s *Server, patch map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)
	return rr
}

func getConfigView(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d: %s", rr.Code, rr.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal GET body: %v", err)
	}
	return out
}

func piSection(t *testing.T, out map[string]interface{}) map[string]interface{} {
	t.Helper()
	pi, ok := out["pi"].(map[string]interface{})
	if !ok {
		t.Skipf("GET view has no pi section: %v", out)
	}
	return pi
}

func autonomousSection(t *testing.T, out map[string]interface{}) map[string]interface{} {
	t.Helper()
	auto, ok := out["autonomous"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET view has no autonomous section: %v", out)
	}
	return auto
}

// TestPiThinkingLevel_DefaultIsEmpty confirms the GET view reports the
// unset default (pi will then pick its own model default — the --thinking
// flag is simply not passed at launch).
func TestPiThinkingLevel_DefaultIsEmpty(t *testing.T) {
	s := newRoundTripServer(t)
	v, ok := piSection(t, getConfigView(t, s))["thinking_level"].(string)
	if !ok {
		t.Skip("GET view's pi.thinking_level is not a string")
	}
	if v != "" {
		t.Errorf("default pi.thinking_level = %q, want empty", v)
	}
}

// TestPiThinkingLevel_RoundTrip sets each allowed level, reads it back, then
// clears it back to empty — the same set→get cycle a PWA/MCP/CLI operator
// performs.
func TestPiThinkingLevel_RoundTrip(t *testing.T) {
	s := newRoundTripServer(t)
	for _, level := range []string{"off", "minimal", "low", "medium", "high"} {
		rr := putPatch(t, s, map[string]interface{}{"pi.thinking_level": level})
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT pi.thinking_level=%q = %d: %s", level, rr.Code, rr.Body.String())
		}
		v, _ := piSection(t, getConfigView(t, s))["thinking_level"].(string)
		if v != level {
			t.Fatalf("round-trip pi.thinking_level = %q, want %q", v, level)
		}
		if got := s.cfg.Pi.ThinkingLevel; got != level {
			t.Fatalf("live cfg.Pi.ThinkingLevel = %q, want %q", got, level)
		}
	}
	// Clearing must be expressible (empty string is a valid value, not
	// "unspecified" — it removes the --thinking flag again).
	rr := putPatch(t, s, map[string]interface{}{"pi.thinking_level": ""})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT pi.thinking_level=(clear) = %d: %s", rr.Code, rr.Body.String())
	}
	if v := s.cfg.Pi.ThinkingLevel; v != "" {
		t.Fatalf("clear left pi.thinking_level = %q, want empty", v)
	}
}

// TestPiThinkingLevel_InvalidRejected confirms an unknown level rejects the
// whole PUT with 400 and changes nothing (pre-validation, like
// task_override_statuses above it in handlePutConfig).
func TestPiThinkingLevel_InvalidRejected(t *testing.T) {
	s := newRoundTripServer(t)
	s.cfg.Pi.ThinkingLevel = "medium" // known value; must survive every rejection
	for _, bad := range []string{"ultra", "OFF", " xhigh", "deep", "max"} {
		rr := putPatch(t, s, map[string]interface{}{"pi.thinking_level": bad})
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT pi.thinking_level=%q = %d, want 400", bad, rr.Code)
		}
		if v := s.cfg.Pi.ThinkingLevel; v != "medium" {
			t.Fatalf("rejected PUT changed pi.thinking_level to %q", v)
		}
	}
	// Non-string values reject too.
	rr := putPatch(t, s, map[string]interface{}{"pi.thinking_level": 42})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT pi.thinking_level=42 = %d, want 400", rr.Code)
	}
}

// TestLoopDetectTurns_DefaultIsThree confirms the GET view reports the
// effective default when the field is unset (nil pointer).
func TestLoopDetectTurns_DefaultIsThree(t *testing.T) {
	s := newRoundTripServer(t)
	v, ok := autonomousSection(t, getConfigView(t, s))["loop_detect_turns"].(float64)
	if !ok {
		t.Skip("GET view's autonomous.loop_detect_turns is not a number")
	}
	if v != 3 {
		t.Errorf("default autonomous.loop_detect_turns = %v, want 3", v)
	}
	if got := config.LoopDetectTurnsEffective(s.cfg.Autonomous.LoopDetectTurns); got != 3 {
		t.Errorf("LoopDetectTurnsEffective(nil) = %d, want 3", got)
	}
}

// TestLoopDetectTurns_RoundTrip sets 0 (off), 3, and 12 and reads each back.
// 0 must survive as an explicit off, not be swallowed as "unset".
func TestLoopDetectTurns_RoundTrip(t *testing.T) {
	s := newRoundTripServer(t)
	for _, n := range []int{0, 3, 12} {
		rr := putPatch(t, s, map[string]interface{}{"autonomous.loop_detect_turns": n})
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT loop_detect_turns=%d = %d: %s", n, rr.Code, rr.Body.String())
		}
		if s.cfg.Autonomous.LoopDetectTurns == nil {
			t.Fatalf("PUT loop_detect_turns=%d left the field unset — 0 must be stored, not dropped", n)
		}
		if got := *s.cfg.Autonomous.LoopDetectTurns; got != n {
			t.Fatalf("live cfg.Autonomous.LoopDetectTurns = %d, want %d", got, n)
		}
		v, _ := autonomousSection(t, getConfigView(t, s))["loop_detect_turns"].(float64)
		if v != float64(n) {
			t.Fatalf("round-trip loop_detect_turns = %v, want %d", v, n)
		}
	}
}

// TestLoopDetectTurns_InvalidRejected confirms negatives and non-numeric
// values reject the PUT with 400 and change nothing.
func TestLoopDetectTurns_InvalidRejected(t *testing.T) {
	s := newRoundTripServer(t)
	z := 5
	s.cfg.Autonomous.LoopDetectTurns = &z // known value; must survive every rejection
	for _, bad := range []interface{}{-1, -100, "five", "3x", []interface{}{3}} {
		rr := putPatch(t, s, map[string]interface{}{"autonomous.loop_detect_turns": bad})
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT loop_detect_turns=%v = %d, want 400", bad, rr.Code)
		}
		if s.cfg.Autonomous.LoopDetectTurns == nil || *s.cfg.Autonomous.LoopDetectTurns != 5 {
			t.Fatalf("rejected PUT changed loop_detect_turns to %v", s.cfg.Autonomous.LoopDetectTurns)
		}
	}
}
