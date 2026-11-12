// usage_autolanes_smoke_test.go — the in-process smoke check of the auto-lanes
// status endpoint (GET /api/usage/autolanes). The real calculator service reads
// recorded readings from a fake source under a fixed clock, the decision log is
// written through the same AutoLanesLog the hourly loop appends to, and the
// endpoint is called over an httptest server. No daemon, no operator folders
// and no model are involved: every directory is a temporary one.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

var autoLanesSmokeNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// autoLanesSmokeServer wires a Server whose calculator reads the given percent
// readings (at 48h, 24h and 1h before the fixed clock) from a temporary store,
// and returns it with an httptest server that serves the status endpoint.
func autoLanesSmokeServer(t *testing.T, dataDir string, percents []float64) (*Server, *httptest.Server) {
	t.Helper()
	s := newUsageTestServer(t, dataDir, "", "", "", "")
	clock := func() time.Time { return autoLanesSmokeNow }
	store, err := usagecalc.OpenStore(t.TempDir(), clock)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	for i, hoursAgo := range []int{48, 24, 1} {
		p := percents[i]
		if _, err := store.Append(usagecalc.Reading{
			At: autoLanesSmokeNow.Add(-time.Duration(hoursAgo) * time.Hour), Percent: &p, Source: usagecalc.SourceTyped,
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	series := usagecalc.NewSharedSeries(store, "", nil, usagecalc.WithSharedClock(clock))
	s.SetUsageCalculator(usagecalc.NewService(series, store,
		func() usagecalc.Settings { return UsageCalculatorSettings(s.cfg.Usage) },
		nil, usagecalc.WithServiceNow(clock)))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleUsageAutoLanes(w, withUsageCaps(r, usageCapabilityRead))
	}))
	t.Cleanup(ts.Close)
	return s, ts
}

// fetchAutoLanesStatus calls the endpoint over HTTP and decodes the body.
func fetchAutoLanesStatus(t *testing.T, ts *httptest.Server) autoLanesStatus {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/usage/autolanes")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	var st autoLanesStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return st
}

// TestAutoLanesSmokeStatusOn: with the feature on, the endpoint reports the
// lane count the rule chooses from the recorded readings, the direction against
// the cap in force, the special count, and the change the loop logged.
func TestAutoLanesSmokeStatusOn(t *testing.T) {
	dataDir := t.TempDir()
	s, ts := autoLanesSmokeServer(t, dataDir, []float64{10, 11, 12}) // light burn: 3 lanes
	s.cfg.Usage.Calculator.AutoLanes = true
	s.cfg.Usage.Calculator.AutoLanesMax = 4
	s.SetAutoLanesSpecialCounter(func(int) int { return 0 })
	autoLanesClaudeEntry(t, s, 1)

	// The loop's own writer records the move it made at the fixed clock.
	logStore, err := usagecalc.OpenAutoLanesLog(dataDir)
	if err != nil {
		t.Fatalf("OpenAutoLanesLog: %v", err)
	}
	if err := logStore.Append(usagecalc.AutoLanesLogEntry{
		At: autoLanesSmokeNow, From: 2, To: 1, Direction: usagecalc.AutoLanesDown,
		Reason: "the advice reading", Policy: usagecalc.PolicyUseAll,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	st := fetchAutoLanesStatus(t, ts)
	if !st.Enabled || st.Max != 4 || st.Policy != usagecalc.PolicyUseAll {
		t.Errorf("settings = enabled %v, max %d, policy %q; want true, 4, use-all", st.Enabled, st.Max, st.Policy)
	}
	if st.CurrentMaxInflight == nil || *st.CurrentMaxInflight != 1 {
		t.Errorf("current_max_inflight = %v, want 1", st.CurrentMaxInflight)
	}
	if st.Target == nil || *st.Target != 3 {
		t.Errorf("target = %v, want 3 (light burn)", st.Target)
	}
	if st.Direction == nil || *st.Direction != usagecalc.AutoLanesUp {
		t.Errorf("direction = %v, want up", st.Direction)
	}
	if st.SpecialItems != 0 {
		t.Errorf("special_items = %d, want 0", st.SpecialItems)
	}
	if st.LastChange == nil || st.LastChange.From != 2 || st.LastChange.To != 1 || !st.LastChange.At.Equal(autoLanesSmokeNow) {
		t.Errorf("last_change = %+v, want the logged 2 -> 1 move at the fixed clock", st.LastChange)
	}
	if len(st.RecentChanges) != 1 {
		t.Errorf("recent_changes has %d entries, want 1", len(st.RecentChanges))
	}
}

// TestAutoLanesSmokeStatusHeavyBurnAndMaximum: heavy burn drives the target to
// 1 (down from 3); light burn plus special items never goes above the operator
// maximum.
func TestAutoLanesSmokeStatusHeavyBurnAndMaximum(t *testing.T) {
	s, ts := autoLanesSmokeServer(t, t.TempDir(), []float64{20, 50, 80})
	s.cfg.Usage.Calculator.AutoLanes = true
	s.cfg.Usage.Calculator.AutoLanesMax = 4
	autoLanesClaudeEntry(t, s, 3)
	st := fetchAutoLanesStatus(t, ts)
	if st.Target == nil || *st.Target != 1 || st.Direction == nil || *st.Direction != usagecalc.AutoLanesDown {
		t.Errorf("heavy burn: target %v direction %v, want 1 down", st.Target, st.Direction)
	}

	s2, ts2 := autoLanesSmokeServer(t, t.TempDir(), []float64{10, 11, 12})
	s2.cfg.Usage.Calculator.AutoLanes = true
	s2.cfg.Usage.Calculator.AutoLanesMax = 4
	s2.SetAutoLanesSpecialCounter(func(int) int { return 3 })
	autoLanesClaudeEntry(t, s2, 1)
	st2 := fetchAutoLanesStatus(t, ts2)
	if st2.Target == nil || *st2.Target != 4 {
		t.Errorf("light burn + 3 special: target = %v, want 4 (the operator maximum)", st2.Target)
	}
}

// TestAutoLanesSmokeStatusOff: with the feature off, the endpoint shows the
// operator's own cap untouched and offers no target or direction, whatever the
// readings say.
func TestAutoLanesSmokeStatusOff(t *testing.T) {
	for _, percents := range [][]float64{{10, 11, 12}, {20, 50, 80}} {
		s, ts := autoLanesSmokeServer(t, t.TempDir(), percents)
		s.cfg.Usage.Calculator.AutoLanes = false
		s.cfg.Usage.Calculator.AutoLanesMax = 4
		autoLanesClaudeEntry(t, s, 3)
		st := fetchAutoLanesStatus(t, ts)
		if st.Enabled {
			t.Errorf("enabled = true, want false")
		}
		if st.CurrentMaxInflight == nil || *st.CurrentMaxInflight != 3 {
			t.Errorf("current_max_inflight = %v, want the operator's 3", st.CurrentMaxInflight)
		}
		if st.Target != nil || st.Direction != nil {
			t.Errorf("off: target %v direction %v, want both null", st.Target, st.Direction)
		}
		if len(st.RecentChanges) != 0 || st.LastChange != nil {
			t.Errorf("off: recent %d last %+v, want no log entries", len(st.RecentChanges), st.LastChange)
		}
		// The status call itself never moves the operator's cap.
		entry, _ := autoLanesTargetEntry(s.inferenceReg)
		if entry.MaxInflight != 3 {
			t.Errorf("operator cap after status = %d, want 3", entry.MaxInflight)
		}
	}
}
