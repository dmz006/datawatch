// compute_placement.* on the REST config surface: GET /api/config returns
// the block (defaults read through), PUT /api/config validates and stores
// each of the five keys, and a rejected PUT changes nothing.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newPlacementServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	// empty file: exactly the "fresh config" the daemon gets before any
	// compute_placement key was ever written; Load applies defaults just
	// like a real daemon start.
	if err := os.WriteFile(cfgPath, nil, 0o644); err != nil {
		t.Fatalf("write empty config.yaml: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load(empty file): %v", err)
	}
	return &Server{cfg: cfg, cfgPath: cfgPath}, cfgPath
}

// getPlacementBlock returns the compute_placement section of GET /api/config.
func getPlacementBlock(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	raw, ok := top["compute_placement"]
	if !ok {
		t.Fatalf("GET /api/config has no compute_placement block:\n%s", rr.Body.String())
	}
	var block map[string]any
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("decode compute_placement: %v", err)
	}
	return block
}

func TestComputePlacement_GetDefaults(t *testing.T) {
	s, _ := newPlacementServer(t)
	b := getPlacementBlock(t, s)
	want := map[string]any{
		"missing_model":        "ask",
		"mem_headroom_pct":     float64(config.ComputePlacementDefaultMemHeadroomPct),
		"disk_headroom_gb":     float64(config.ComputePlacementDefaultDiskHeadroomGB),
		"ask_timeout_minutes":  float64(config.ComputePlacementDefaultAskTimeoutMinutes),
		"assume_model_size_gb": float64(config.ComputePlacementDefaultAssumeModelSizeGB),
	}
	for k, v := range want {
		if b[k] != v {
			t.Fatalf("GET compute_placement.%s = %v, want %v", k, b[k], v)
		}
	}
	vocab, ok := b["missing_model_vocab"].([]any)
	if !ok || len(vocab) != 3 {
		t.Fatalf("missing_model_vocab = %v, want the three policies", b["missing_model_vocab"])
	}
}

func TestComputePlacement_PutRoundTripEachKey(t *testing.T) {
	cases := []struct {
		key   string
		value any
		field string
		want  any
	}{
		{"compute_placement.missing_model", "auto_load", "missing_model", "auto_load"},
		{"compute_placement.missing_model", "refuse", "missing_model", "refuse"},
		{"compute_placement.mem_headroom_pct", 25, "mem_headroom_pct", float64(25)},
		{"compute_placement.disk_headroom_gb", 32, "disk_headroom_gb", float64(32)},
		{"compute_placement.disk_headroom_gb", 0, "disk_headroom_gb", float64(0)},
		{"compute_placement.ask_timeout_minutes", 45, "ask_timeout_minutes", float64(45)},
		{"compute_placement.assume_model_size_gb", 8, "assume_model_size_gb", float64(8)},
		{"compute_placement.assume_model_size_gb", 0, "assume_model_size_gb", float64(0)},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			s, _ := newPlacementServer(t)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{c.key: c.value})
			b := getPlacementBlock(t, s)
			if b[c.field] != c.want {
				t.Fatalf("GET after PUT = %v, want %v", b[c.field], c.want)
			}
			// the stored config object must carry the value (no restart needed)
			if got := s.cfg.ComputePlacement.EffectiveMissingModel(); c.key == "compute_placement.missing_model" && got != c.want.(string) {
				t.Fatalf("s.cfg.ComputePlacement.EffectiveMissingModel() = %q; want %v", got, c.want)
			}
			if c.field == "mem_headroom_pct" && s.cfg.ComputePlacement.MemHeadroomPct != c.value.(int) {
				t.Fatalf("s.cfg.ComputePlacement.MemHeadroomPct = %d; want %v", s.cfg.ComputePlacement.MemHeadroomPct, c.value)
			}
			if c.field == "disk_headroom_gb" && s.cfg.ComputePlacement.DiskHeadroomGB != c.value.(int) {
				t.Fatalf("s.cfg.ComputePlacement.DiskHeadroomGB = %d; want %v", s.cfg.ComputePlacement.DiskHeadroomGB, c.value)
			}
			if c.field == "ask_timeout_minutes" && s.cfg.ComputePlacement.AskTimeoutMinutes != c.value.(int) {
				t.Fatalf("s.cfg.ComputePlacement.AskTimeoutMinutes = %d; want %v", s.cfg.ComputePlacement.AskTimeoutMinutes, c.value)
			}
			if c.field == "assume_model_size_gb" && s.cfg.ComputePlacement.AssumeModelSizeGB != c.value.(int) {
				t.Fatalf("s.cfg.ComputePlacement.AssumeModelSizeGB = %d; want %v", s.cfg.ComputePlacement.AssumeModelSizeGB, c.value)
			}
		})
	}
}

func TestComputePlacement_PutCombinedPatchLeavesUnsetNameDefaults(t *testing.T) {
	s, _ := newPlacementServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"compute_placement.missing_model":        "auto_load",
		"compute_placement.mem_headroom_pct":     20,
		"compute_placement.disk_headroom_gb":     10,
		"compute_placement.ask_timeout_minutes":  15,
		"compute_placement.assume_model_size_gb": 4,
	})
	b := getPlacementBlock(t, s)
	if b["missing_model"] != "auto_load" || b["mem_headroom_pct"] != float64(20) ||
		b["disk_headroom_gb"] != float64(10) || b["ask_timeout_minutes"] != float64(15) ||
		b["assume_model_size_gb"] != float64(4) {
		t.Fatalf("combined PUT not reflected in GET: %v", b)
	}
}

func TestComputePlacement_PutSurvivesSaveAndLoad(t *testing.T) {
	s, cfgPath := newPlacementServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"compute_placement.missing_model":    "refuse",
		"compute_placement.mem_headroom_pct": 42,
	})
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	for _, want := range []string{"compute_placement:", "missing_model: refuse", "mem_headroom_pct: 42"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("config.yaml lacks %q:\n%s", want, raw)
		}
	}
}

func TestComputePlacement_PutRejectsInvalidKeyValues(t *testing.T) {
	cases := []map[string]any{
		{"compute_placement.missing_model": "banana"},
		{"compute_placement.mem_headroom_pct": 150},
		{"compute_placement.mem_headroom_pct": -1},
		{"compute_placement.disk_headroom_gb": -3},
		{"compute_placement.ask_timeout_minutes": -5},
		{"compute_placement.assume_model_size_gb": -8},
		{"compute_placement.assume_model_size_gb": 2.5},
	}
	for _, patch := range cases {
		t.Run(fmt.Sprintf("%v", patch), func(t *testing.T) {
			s, _ := newPlacementServer(t)
			var key string
			for k := range patch {
				key = k
			}
			rr := httptest.NewRecorder()
			b, _ := json.Marshal(patch)
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT %v: want 400, got %d", patch, rr.Code)
			}
			if !strings.Contains(rr.Body.String(), key+":") {
				t.Fatalf("PUT %v: error %q does not name the key %q", patch, rr.Body.String(), key)
			}
			got := getPlacementBlock(t, s)
			if got["missing_model"] != "ask" {
				t.Fatalf("rejected %s PUT mutated missing_model to %v", key, got["missing_model"])
			}
		})
	}
}

// TestComputePlacement_PutInvalidRejectsWholePatch pins "one bad key in a
// multi-key patch rejects the whole PUT" — no partial apply.
func TestComputePlacement_PutInvalidRejectsWholePatch(t *testing.T) {
	s, _ := newPlacementServer(t)
	rr := httptest.NewRecorder()
	b, _ := json.Marshal(map[string]any{
		"compute_placement.missing_model":    "auto_load", // valid
		"compute_placement.mem_headroom_pct": 200,         // invalid
	})
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
	got := getPlacementBlock(t, s)
	if got["missing_model"] != "ask" {
		t.Fatalf("the valid key in a rejected patch must not be applied; missing_model = %v", got["missing_model"])
	}
	if got["mem_headroom_pct"] != float64(config.ComputePlacementDefaultMemHeadroomPct) {
		t.Fatalf("mem_headroom_pct = %v, want the default %d", got["mem_headroom_pct"], config.ComputePlacementDefaultMemHeadroomPct)
	}
}
