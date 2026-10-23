package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestStoreConfigKeysRoundTrip(t *testing.T) {
	s := &Server{cfg: &config.Config{}, cfgPath: filepath.Join(t.TempDir(), "config.yaml")}

	get := func() map[string]interface{} {
		rec := httptest.NewRecorder()
		s.handleGetConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/config = %d: %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Session map[string]interface{} `json:"session"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Session
	}

	// Defaults are reported before anything is set.
	def := get()
	for k, want := range map[string]float64{
		"store_flush_interval": 1000, "store_archive_after_days": 14,
		"store_field_cap_kb": 256, "store_side_file_cap_kb": 64,
	} {
		if def[k] != want {
			t.Fatalf("default session.%s = %v, want %v", k, def[k], want)
		}
	}

	put := map[string]interface{}{
		"session.store_flush_interval":     0, // write through must survive the round trip
		"session.store_archive_after_days": 0, // archiving off must survive too
		"session.store_field_cap_kb":       128,
		"session.store_side_file_cap_kb":   32,
	}
	b, _ := json.Marshal(put)
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config = %d: %s", rec.Code, rec.Body.String())
	}
	got := get()
	for k, want := range map[string]float64{
		"store_flush_interval": 0, "store_archive_after_days": 0,
		"store_field_cap_kb": 128, "store_side_file_cap_kb": 32,
	} {
		if got[k] != want {
			t.Fatalf("GET session.%s after PUT = %v, want %v", k, got[k], want)
		}
	}

	// Invalid values are rejected without changing the stored ones.
	applyConfigPatch(s.cfg, map[string]interface{}{
		"session.store_flush_interval": -5,
		"session.store_field_cap_kb":   0,
	})
	again := get()
	if again["store_flush_interval"] != float64(0) || again["store_field_cap_kb"] != float64(128) {
		t.Fatalf("invalid patch changed values: %v", again)
	}
}
