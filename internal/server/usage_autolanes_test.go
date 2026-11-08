package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// autoLanesClaudeEntry adds one enabled claude-code LLM with the given
// max_inflight to a fresh registry and wires it onto the server.
func autoLanesClaudeEntry(t *testing.T, s *Server, maxInflight int) {
	t.Helper()
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "claude", Kind: inference.KindClaudeCode, MaxInflight: maxInflight}); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	s.inferenceReg = reg
}

// getAutoLanes runs the status endpoint and decodes its body.
func getAutoLanes(t *testing.T, s *Server) (int, *httptest.ResponseRecorder, autoLanesStatus) {
	t.Helper()
	req := withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/autolanes", nil), usageCapabilityRead)
	rr := httptest.NewRecorder()
	s.handleUsageAutoLanes(rr, req)
	var st autoLanesStatus
	decodeJSON(t, rr, &st)
	return rr.Code, rr, st
}

// TestAutoLanesDisabled: auto_lanes off — the endpoint reports the settings,
// the current cap, the log and the special count, with no target or
// direction.
func TestAutoLanesDisabled(t *testing.T) {
	s := newUsageCalcServer(t)
	s.cfg.Usage.Calculator.AutoLanes = false
	s.cfg.Usage.Calculator.AutoLanesMax = 4
	s.SetAutoLanesSpecialCounter(func(threshold int) int { return 2 })
	autoLanesClaudeEntry(t, s, 3)

	code, rr, st := getAutoLanes(t, s)
	if code != http.StatusOK {
		t.Fatalf("GET = %d: %s", code, rr.Body.String())
	}
	if st.Enabled {
		t.Errorf("enabled = true, want false")
	}
	if st.Policy != "use-all" {
		t.Errorf("policy = %q, want default use-all", st.Policy)
	}
	if st.Max != 4 {
		t.Errorf("max = %d, want 4", st.Max)
	}
	if st.CurrentMaxInflight == nil || *st.CurrentMaxInflight != 3 {
		t.Errorf("current_max_inflight = %v, want 3 (the enabled claude-code entry)", st.CurrentMaxInflight)
	}
	if st.Target != nil || st.Direction != nil {
		t.Errorf("off: target = %v, direction = %v, want both null", st.Target, st.Direction)
	}
	if st.LastChange != nil {
		t.Errorf("last_change = %+v, want nil (no decisions recorded)", st.LastChange)
	}
	if st.SpecialItems != 2 {
		t.Errorf("special_items = %d, want 2 (the wired counter)", st.SpecialItems)
	}
	if len(st.RecentChanges) != 0 {
		t.Errorf("recent_changes has %d entries, want none", len(st.RecentChanges))
	}
}

// TestAutoLanesEnabled: auto_lanes on — the decision is the same pure rule
// the hourly loop applies: the advice lanes plus the special items, clamped
// to 1..max, against the cap applied now.
func TestAutoLanesEnabled(t *testing.T) {
	s := newUsageCalcServer(t)
	s.cfg.Usage.Calculator.AutoLanes = true
	s.cfg.Usage.Calculator.AutoLanesMax = 1 // force the clamp: the target is 1 whatever the advice is
	s.SetAutoLanesSpecialCounter(func(threshold int) int { return 2 })

	// One percent reading, so the advice has something to plan from.
	if err := s.postAutoLanesReading(t); err != nil {
		t.Fatalf("seed reading: %v", err)
	}

	autoLanesClaudeEntry(t, s, 3)
	code, rr, st := getAutoLanes(t, s)
	if code != http.StatusOK {
		t.Fatalf("GET = %d: %s", code, rr.Body.String())
	}
	if !st.Enabled || st.Max != 1 || st.Policy != "use-all" {
		t.Errorf("settings = %+v, want enabled=true, max=1, policy=use-all", st)
	}
	if st.CurrentMaxInflight == nil || *st.CurrentMaxInflight != 3 {
		t.Fatalf("current_max_inflight = %v, want 3", st.CurrentMaxInflight)
	}
	if st.SpecialItems != 2 {
		t.Errorf("special_items = %d, want 2", st.SpecialItems)
	}
	if st.Target == nil {
		t.Fatalf("target = null, want set (auto_lanes is on)")
	}
	if *st.Target != 1 {
		t.Errorf("target = %d, want 1 (clamped within max 1)", *st.Target)
	}
	if st.Direction == nil || *st.Direction != usagecalc.AutoLanesDown {
		t.Errorf("direction = %v, want %q (the cap is 3, the target 1)", st.Direction, usagecalc.AutoLanesDown)
	}
}

// TestAutoLanesEnabledUnchanged: the target equal to the applied cap is
// reported unchanged.
func TestAutoLanesEnabledUnchanged(t *testing.T) {
	s := newUsageCalcServer(t)
	s.cfg.Usage.Calculator.AutoLanes = true
	s.cfg.Usage.Calculator.AutoLanesMax = 1
	if err := s.postAutoLanesReading(t); err != nil {
		t.Fatalf("seed reading: %v", err)
	}
	autoLanesClaudeEntry(t, s, 1) // cap already at the forced target

	code, rr, st := getAutoLanes(t, s)
	if code != http.StatusOK {
		t.Fatalf("GET = %d: %s", code, rr.Body.String())
	}
	if st.Target == nil || *st.Target != 1 || st.Direction == nil || *st.Direction != usagecalc.AutoLanesUnchanged {
		t.Errorf("target = %v, direction = %v, want 1 unchanged", st.Target, st.Direction)
	}
}

// TestAutoLanesDisabledServiceMissing: enabled with no advice service wired
// is a 503 like the advice endpoint's own gate.
func TestAutoLanesEnabledServiceMissing(t *testing.T) {
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")
	s.cfg.Usage.Calculator.AutoLanes = true

	req := withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/autolanes", nil), usageCapabilityRead)
	rr := httptest.NewRecorder()
	s.handleUsageAutoLanes(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET = %d: %s, want 503 (the calculator is not wired)", rr.Code, rr.Body.String())
	}
}

// TestAutoLanesRecentChanges: the decision log is served newest first and
// last_change is the newest decision that actually moved the cap.
func TestAutoLanesRecentChanges(t *testing.T) {
	dataDir := t.TempDir()
	s := newUsageTestServer(t, dataDir, "", "", "", "")

	logStore, err := usagecalc.OpenAutoLanesLog(dataDir)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	entries := []usagecalc.AutoLanesLogEntry{
		{At: base.Add(-4 * time.Hour), From: 3, To: 1, Direction: usagecalc.AutoLanesDown, Reason: "first move"},
		{At: base.Add(-3 * time.Hour), From: 1, To: 1, Direction: usagecalc.AutoLanesUnchanged, Reason: "still one"},
		{At: base.Add(-2 * time.Hour), From: 1, To: 2, Direction: usagecalc.AutoLanesUp, Reason: "second move"},
		{At: base.Add(-1 * time.Hour), From: 2, To: 2, Direction: usagecalc.AutoLanesUnchanged, Reason: "still two"},
	}
	for _, e := range entries { // append order, oldest first
		if err := logStore.Append(e); err != nil {
			t.Fatalf("append entry: %v", err)
		}
	}

	code, rr, st := getAutoLanes(t, s)
	if code != http.StatusOK {
		t.Fatalf("GET = %d: %s", code, rr.Body.String())
	}
	if len(st.RecentChanges) != len(entries) {
		t.Fatalf("recent_changes has %d entries, want %d", len(st.RecentChanges), len(entries))
	}
	// Newest first.
	for i, want := range []string{"still two", "second move", "still one", "first move"} {
		if got := st.RecentChanges[i].Reason; got != want {
			t.Errorf("recent_changes[%d].reason = %q, want %q (newest first)", i, got, want)
		}
	}
	// last_change skips the newest no-op and takes the newest real move.
	if st.LastChange == nil || st.LastChange.Reason != "second move" ||
		st.LastChange.From != 1 || st.LastChange.To != 2 {
		t.Errorf("last_change = %+v, want 1 -> 2 (%q)", st.LastChange, "second move")
	}
}

// TestAutoLanesRecentChangesCap: at most autoLanesMaxRecentChanges entries
// are served, the newest ones.
func TestAutoLanesRecentChangesCap(t *testing.T) {
	dataDir := t.TempDir()
	s := newUsageTestServer(t, dataDir, "", "", "", "")

	logStore, err := usagecalc.OpenAutoLanesLog(dataDir)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	total := autoLanesMaxRecentChanges + 5
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < total; i++ {
		e := usagecalc.AutoLanesLogEntry{
			At:        base.Add(time.Duration(i) * time.Minute),
			From:      1, To: 2,
			Direction: usagecalc.AutoLanesUp,
			Reason:    "decision",
		}
		if err := logStore.Append(e); err != nil {
			t.Fatalf("append entry %d: %v", i, err)
		}
	}

	code, rr, st := getAutoLanes(t, s)
	if code != http.StatusOK {
		t.Fatalf("GET = %d: %s", code, rr.Body.String())
	}
	if len(st.RecentChanges) != autoLanesMaxRecentChanges {
		t.Fatalf("recent_changes has %d entries, want the newest %d", len(st.RecentChanges), autoLanesMaxRecentChanges)
	}
	wantNewest := base.Add(time.Duration(total-1) * time.Minute) // the last appended
	if !st.RecentChanges[0].At.Equal(wantNewest) {
		t.Errorf("recent_changes[0].at = %s, want %s (the newest decision)", st.RecentChanges[0].At, wantNewest)
	}
	// Oldest first within the served window: strictly increasing toward the tail.
	for i := 1; i < len(st.RecentChanges); i++ {
		if !st.RecentChanges[i-1].At.After(st.RecentChanges[i].At) {
			t.Fatalf("recent_changes is not newest first at index %d: %s !> %s", i, st.RecentChanges[i-1].At, st.RecentChanges[i].At)
		}
	}
	if st.LastChange == nil || !st.LastChange.At.Equal(wantNewest) {
		t.Errorf("last_change = %+v, want the newest decision at %s", st.LastChange, wantNewest)
	}
}

// TestAutoLanesReadOnly: the endpoint takes no writes and no other methods.
func TestAutoLanesReadOnly(t *testing.T) {
	s := newUsageCalcServer(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := withUsageCaps(httptest.NewRequest(method, "/api/usage/autolanes", nil), usageCapabilityRead, usageCapabilityWrite)
		rr := httptest.NewRecorder()
		s.handleUsageAutoLanes(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, rr.Code)
		}
	}
}

// TestAutoLanesRequiresUsageRead: the endpoint gates a session credential on
// the usage read capability like the rest of the calculator surface (a
// request with no session context at all is the daemon's own admin path and
// stays unrestricted, as with every other endpoint).
func TestAutoLanesRequiresUsageRead(t *testing.T) {
	s := newUsageCalcServer(t)
	req := withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/autolanes", nil), federation.CapSessionsRead)
	rr := httptest.NewRecorder()
	s.handleUsageAutoLanes(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("GET with a session credential lacking the usage read capability answered %d, want 403", rr.Code)
	}
}

// postAutoLanesReading appends one typed percent reading so the advice has
// an input to plan from.
func (s *Server) postAutoLanesReading(t *testing.T) error {
	t.Helper()
	b := jsonMarshalUsage(t, usagecalc.Reading{
		At:      time.Now().UTC(),
		Percent: fptrUsage(61.5),
		Source:  usagecalc.SourceTyped,
	})
	rr := doUsage(t, s, http.MethodPost, "/api/usage/readings", b, s.handleUsageReadings)
	if rr.Code != http.StatusOK {
		return fmt.Errorf("POST reading = %d: %s", rr.Code, rr.Body.String())
	}
	return nil
}
