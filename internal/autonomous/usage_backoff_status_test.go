package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSetConfig_UsageBackoffKeysMergeAndPersist(t *testing.T) {
	api, m := newOverrideConfigAPI(t)
	if err := api.SetConfig(json.RawMessage(`{"usage_backoff":"off","backoff_max_wait_minutes":15}`)); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if c := m.Config(); c.UsageBackoff != "off" || c.BackoffMaxWaitMinutes != 15 {
		t.Fatalf("config = %q / %d, want off / 15", c.UsageBackoff, c.BackoffMaxWaitMinutes)
	}
	// An unrelated partial update leaves both alone.
	if err := api.SetConfig(json.RawMessage(`{"poll_interval_seconds":7}`)); err != nil {
		t.Fatal(err)
	}
	if c := m.Config(); c.UsageBackoff != "off" || c.BackoffMaxWaitMinutes != 15 {
		t.Fatalf("partial update changed the keys: %q / %d", c.UsageBackoff, c.BackoffMaxWaitMinutes)
	}
	// The JSON the REST layer round-trips onto config.AutonomousConfig
	// carries both keys under their config names.
	b, err := json.Marshal(m.Config())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"usage_backoff":"off"`, `"backoff_max_wait_minutes":15`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("marshaled config lacks %s: %s", want, b)
		}
	}
}

func TestStatus_UsageBackoffDefaultsWithoutRegistry(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	st := m.Status()
	if st.UsageBackoff == nil {
		t.Fatal("Status().UsageBackoff is nil")
	}
	if st.UsageBackoff.Config.Mode != "pause_backend" || st.UsageBackoff.Config.MaxWaitMinutes != 60 {
		t.Errorf("config = %+v, want defaults pause_backend/60", st.UsageBackoff.Config)
	}
	b, _ := json.Marshal(st)
	if !strings.Contains(string(b), `"usage_backoff":{"active":[]`) {
		t.Errorf("status JSON should carry an empty active array: %s", b)
	}
}

func TestStatus_UsageBackoffListsLiveFlags(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.Config()
	cfg.UsageBackoff = "off"
	cfg.BackoffMaxWaitMinutes = 20
	m.SetConfig(cfg)

	bo := NewBackoff()
	now := time.Now()
	bo.Set("claude-code", now.Add(time.Hour), "operator back-off", BackoffSourceOperator)
	bo.Set("stale", now.Add(-time.Minute), "expired, not yet swept", BackoffSourceRateLimit)
	m.SetBackoff(bo)

	st := m.Status().UsageBackoff
	if st == nil || len(st.Active) != 1 || st.Active[0].Backend != "claude-code" || st.Active[0].Source != BackoffSourceOperator {
		t.Fatalf("usage_backoff = %+v, want only the live claude-code flag", st)
	}
	if st.Config.Mode != "off" || st.Config.MaxWaitMinutes != 20 {
		t.Errorf("config = %+v, want off/20", st.Config)
	}
	b, _ := json.Marshal(st)
	for _, key := range []string{`"backend"`, `"until"`, `"reason"`, `"source"`, `"max_wait_minutes"`, `"mode"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("status JSON lacks %s: %s", key, b)
		}
	}

	// Clearing through the shared registry is reflected immediately.
	bo.ClearAll()
	if got := m.Status().UsageBackoff.Active; len(got) != 0 {
		t.Errorf("active after ClearAll = %+v", got)
	}
}

func TestBackoff_LiveAndSweepExpired(t *testing.T) {
	b, clk := newTestBackoff()
	b.Set("a", clk.t.Add(time.Hour), "r", BackoffSourceRateLimit)
	b.Set("b", clk.t.Add(10*time.Minute), "r", BackoffSourceOperator)
	if got := b.Live(); len(got) != 2 || got[0].Backend != "a" || got[1].Backend != "b" {
		t.Fatalf("Live = %+v", got)
	}
	clk.Advance(30 * time.Minute)
	if got := b.Live(); len(got) != 1 || got[0].Backend != "a" {
		t.Fatalf("Live after b expiry = %+v", got)
	}
	if got := b.Active(); len(got) != 2 {
		t.Fatalf("Active must still hold the unswept entry: %+v", got)
	}
	exp := b.SweepExpired(clk.t)
	if len(exp) != 1 || exp[0].Backend != "b" || !exp[0].Until.Equal(time.Date(2026, 10, 22, 12, 10, 0, 0, time.UTC)) {
		t.Fatalf("SweepExpired = %+v", exp)
	}
	if got := b.SweepExpired(clk.t); len(got) != 0 {
		t.Errorf("second sweep = %+v, want none", got)
	}
}
