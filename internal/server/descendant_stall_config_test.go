// autonomous.descendant_stall_seconds — config round-trip: the patch case
// stores the value and GET /api/config exposes it. Zero is stored as zero
// here; the 2700-second fallback (DescendantStallDefaultSeconds) is applied
// at consumption time via config.NormalizeDescendantStallSeconds.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// descStallGetResponse mirrors the autonomous block of the GET /api/config
// payload for the one key under test.
type descStallGetResponse struct {
	Autonomous struct {
		DescendantStallSeconds int `json:"descendant_stall_seconds"`
	} `json:"autonomous"`
}

func TestDescendantStall_RoundTrip(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.descendant_stall_seconds": 1800})
	if cfg.Autonomous.DescendantStallSeconds != 1800 {
		t.Fatalf("DescendantStallSeconds = %d, want 1800", cfg.Autonomous.DescendantStallSeconds)
	}

	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got descStallGetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.DescendantStallSeconds != 1800 {
		t.Errorf("GET descendant_stall_seconds = %d, want 1800", got.Autonomous.DescendantStallSeconds)
	}
}

// A zero-valued config reads back 0: the default is applied at use, not storage.
func TestDescendantStall_ZeroValueReadsBackZero(t *testing.T) {
	cfg := &config.Config{}
	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got descStallGetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.DescendantStallSeconds != 0 {
		t.Errorf("GET descendant_stall_seconds = %d, want 0", got.Autonomous.DescendantStallSeconds)
	}
	if cfg.Autonomous.DescendantStallSeconds != 0 {
		t.Errorf("stored DescendantStallSeconds = %d, want 0", cfg.Autonomous.DescendantStallSeconds)
	}
}

// A negative value is refused by the patch case, like
// session_idle_timeout_seconds: the stored value is unchanged.
func TestDescendantStall_NegativeValueRejected(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.descendant_stall_seconds": -10})
	if cfg.Autonomous.DescendantStallSeconds != 0 {
		t.Errorf("DescendantStallSeconds = %d, want 0 (negative write must be refused)", cfg.Autonomous.DescendantStallSeconds)
	}
}

// Zero/negative values are accepted on write only as zero and normalize to
// the 2700-second (45m) default at read time, exactly like
// session_idle_timeout_seconds.
func TestDescendantStall_ZeroAndNegativeNormalizeOnRead(t *testing.T) {
	if got := config.NormalizeDescendantStallSeconds(0); got != config.DescendantStallDefaultSeconds {
		t.Errorf("NormalizeDescendantStallSeconds(0) = %d, want %d", got, config.DescendantStallDefaultSeconds)
	}
	if config.DescendantStallDefaultSeconds != 2700 {
		t.Errorf("DescendantStallDefaultSeconds = %d, want 2700 (45 minutes)", config.DescendantStallDefaultSeconds)
	}
	if got := config.NormalizeDescendantStallSeconds(-10); got != config.DescendantStallDefaultSeconds {
		t.Errorf("NormalizeDescendantStallSeconds(-10) = %d, want %d", got, config.DescendantStallDefaultSeconds)
	}
	if got := config.NormalizeDescendantStallSeconds(600); got != 600 {
		t.Errorf("NormalizeDescendantStallSeconds(600) = %d, want 600", got)
	}
}
