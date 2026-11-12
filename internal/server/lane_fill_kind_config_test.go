package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The seven lane fill keys added for stale work, eligibility and per-kind
// modes: the surface serves their values in effect, a PUT stores them, and a
// bad value is refused with a 400 that names the key.

func TestLaneFillNewKeysSurfaceShowsDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	block := laneFillSurface(t, s)
	want := map[string]string{
		"stale_after_hours":          "72",
		"require_confirmed_proposal": "true",
		"check_main_carries":         "true",
		"require_worktree":           "true",
		"lane_counts_from_caps":      "true",
		"gpu_mode":                   `""`,
		"claude_mode":                `""`,
	}
	for k, v := range want {
		if got := string(block[k]); got != v {
			t.Errorf("GET /api/config lane_fill.%s = %s, want %s", k, got, v)
		}
	}
}

func TestLaneFillNewKeysPutThenGetRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.lane_fill.stale_after_hours":          0,
		"autonomous.lane_fill.require_confirmed_proposal": false,
		"autonomous.lane_fill.check_main_carries":         false,
		"autonomous.lane_fill.require_worktree":           false,
		"autonomous.lane_fill.lane_counts_from_caps":      false,
		"autonomous.lane_fill.gpu_mode":                   "on",
		"autonomous.lane_fill.claude_mode":                " Off ",
	})
	block := laneFillSurface(t, s)
	want := map[string]string{
		"stale_after_hours":          "0",
		"require_confirmed_proposal": "false",
		"check_main_carries":         "false",
		"require_worktree":           "false",
		"lane_counts_from_caps":      "false",
		"gpu_mode":                   `"on"`,
		"claude_mode":                `"off"`,
	}
	for k, v := range want {
		if got := string(block[k]); got != v {
			t.Errorf("GET after PUT: lane_fill.%s = %s, want %s", k, got, v)
		}
	}
	live := mgr.Config().LaneFill
	if live.EffectiveStaleAfterHours() != 0 || live.StaleAfterHours == nil {
		t.Errorf("the live manager must hold the explicit stale 0, got %v", live.StaleAfterHours)
	}
	if live.EffectiveRequireConfirmedProposal() || live.EffectiveCheckMainCarries() ||
		live.EffectiveRequireWorktree() || live.EffectiveLaneCountsFromCaps() {
		t.Error("the live manager must hold the four explicit false switches")
	}
	if live.EffectiveModeFor("gpu") != "on" || live.EffectiveModeFor("claude") != "off" {
		t.Errorf("the live manager per-kind modes = %q/%q, want on/off", live.GPUMode, live.ClaudeMode)
	}
	// mode itself is untouched, so the shipped shadow default is intact.
	if live.EffectiveMode() != "shadow" {
		t.Errorf("shared mode changed to %q", live.EffectiveMode())
	}

	// Setting them back: an empty per-kind mode inherits again.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.lane_fill.gpu_mode":              "",
		"autonomous.lane_fill.claude_mode":           "",
		"autonomous.lane_fill.stale_after_hours":     96,
		"autonomous.lane_fill.lane_counts_from_caps": true,
	})
	block = laneFillSurface(t, s)
	if string(block["gpu_mode"]) != `""` || string(block["claude_mode"]) != `""` {
		t.Errorf("empty per-kind modes must read back empty: %s %s", block["gpu_mode"], block["claude_mode"])
	}
	if string(block["stale_after_hours"]) != "96" || string(block["lane_counts_from_caps"]) != "true" {
		t.Errorf("stale/caps = %s/%s, want 96/true", block["stale_after_hours"], block["lane_counts_from_caps"])
	}
}

func TestLaneFillNewKeysAutonomousConfigEndpointRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	b, _ := json.Marshal(map[string]any{"lane_fill": map[string]any{
		"stale_after_hours": 0, "require_worktree": false, "claude_mode": "on",
	}})
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/autonomous/config: %d %s", rr.Code, rr.Body.String())
	}
	live := mgr.Config().LaneFill
	if live.StaleAfterHours == nil || *live.StaleAfterHours != 0 || live.EffectiveRequireWorktree() ||
		live.EffectiveModeFor("claude") != "on" || live.EffectiveModeFor("gpu") != "shadow" {
		t.Fatalf("the autonomous config PUT lost the new keys: %+v", live)
	}
	block := laneFillSurface(t, s)
	if string(block["stale_after_hours"]) != "0" || string(block["require_worktree"]) != "false" || string(block["claude_mode"]) != `"on"` {
		t.Fatalf("GET /api/config after the autonomous config PUT: %v", block)
	}
}

func TestLaneFillBadGPUModeIs400(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	for _, key := range []string{"gpu_mode", "claude_mode"} {
		for _, bad := range []any{"auto", "yes", 1, true} {
			b, _ := json.Marshal(map[string]any{"autonomous.lane_fill." + key: bad})
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("%s=%v must be 400, got %d: %s", key, bad, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "autonomous.lane_fill."+key) {
				t.Errorf("%s=%v: the 400 must name the key, got %q", key, bad, rr.Body.String())
			}
		}
	}
	if live := mgr.Config().LaneFill; live.GPUMode != "" || live.ClaudeMode != "" {
		t.Fatalf("rejected PUTs must store nothing, got %q/%q", live.GPUMode, live.ClaudeMode)
	}
}

func TestLaneFillBadNewValuesAreRefused(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	cases := map[string][]any{
		"stale_after_hours":          {-1, "72", 1.5, true},
		"require_confirmed_proposal": {"yes", 1, "true"},
		"check_main_carries":         {"yes", 0},
		"require_worktree":           {"no", 1},
		"lane_counts_from_caps":      {"on", 1},
	}
	for key, bads := range cases {
		for _, bad := range bads {
			b, _ := json.Marshal(map[string]any{"autonomous.lane_fill." + key: bad})
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
			if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), key) {
				t.Errorf("%s=%v: want a 400 naming the key, got %d %q", key, bad, rr.Code, rr.Body.String())
			}
		}
	}
	// the nested endpoint refuses a bad per-kind mode too.
	b, _ := json.Marshal(map[string]any{"lane_fill": map[string]any{"gpu_mode": "auto"}})
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "gpu_mode") {
		t.Fatalf("PUT /api/autonomous/config bad gpu_mode: %d %s", rr.Code, rr.Body.String())
	}
}

func TestLaneFillNewKeysNamedInConfigureUsage(t *testing.T) {
	src := readRepoFile(t, "internal/router/router.go")
	for _, k := range []string{"stale_after_hours", "require_confirmed_proposal", "check_main_carries",
		"require_worktree", "lane_counts_from_caps", "gpu_mode", "claude_mode"} {
		if n := strings.Count(src, "autonomous.lane_fill."+k); n < 2 {
			t.Errorf("%s must appear in both configure usage strings; found %d", k, n)
		}
	}
}
