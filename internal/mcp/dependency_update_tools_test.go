package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

type fakeDependencyUpdateState struct{ lr DependencyUpdateLastRun }

func (f fakeDependencyUpdateState) LastDependencyUpdateRun() DependencyUpdateLastRun { return f.lr }

func callDependencyUpdateStatus(t *testing.T, s *Server) dependencyUpdateStatusBody {
	t.Helper()
	res, err := s.handleDependencyUpdateStatus(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil || res == nil || res.IsError {
		t.Fatalf("handler failed: res=%v err=%v", res, err)
	}
	txt, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content is %T, want text", res.Content[0])
	}
	var b dependencyUpdateStatusBody
	if err := json.Unmarshal([]byte(txt.Text), &b); err != nil {
		t.Fatalf("bad JSON %q: %v", txt.Text, err)
	}
	return b
}

func TestDependencyUpdateStatus_DefaultsAndNilState(t *testing.T) {
	b := callDependencyUpdateStatus(t, &Server{})
	if !b.Enabled || b.Schedule != "monday 05:00" || b.MaxMajors != 5 {
		t.Errorf("defaults = %+v", b)
	}
	if b.NextRun == "" {
		t.Error("next_run empty while enabled")
	}
	if b.LastRunAt != "" || b.LastRunAutomatonTitle != "" || b.LastRunOutcome != "" {
		t.Errorf("last-run fields must be empty with no state: %+v", b)
	}
}

func TestDependencyUpdateStatus_SettingsAndLastRun(t *testing.T) {
	off, zero := false, 0
	at := time.Date(2026, 11, 9, 5, 0, 0, 0, time.UTC)
	s := &Server{
		dependencyUpdateCfg: func() config.DependencyUpdateConfig {
			return config.DependencyUpdateConfig{Enabled: &off, Schedule: "friday 22:30", MaxMajors: &zero}
		},
	}
	s.SetDependencyUpdateState(fakeDependencyUpdateState{DependencyUpdateLastRun{
		At: at, AutomatonTitle: "Update dependencies", Outcome: DependencyUpdateOutcomeNeedsAttention,
	}})
	b := callDependencyUpdateStatus(t, s)
	if b.Enabled || b.Schedule != "friday 22:30" || b.MaxMajors != 0 {
		t.Errorf("settings = %+v", b)
	}
	if b.NextRun != "" {
		t.Errorf("next_run = %q, want empty while off", b.NextRun)
	}
	if b.LastRunAutomatonTitle != "Update dependencies" || b.LastRunOutcome != "needs attention" {
		t.Errorf("last run = %+v", b)
	}
	got, err := time.Parse(time.RFC3339, b.LastRunAt)
	if err != nil || !got.Equal(at) {
		t.Errorf("last_run_at = %q (%v), want %v", b.LastRunAt, err, at)
	}
}

func TestNextDependencyUpdateRun(t *testing.T) {
	loc := time.FixedZone("x", 3600)
	cases := []struct {
		now, sched, want string
		ok               bool
	}{
		// 2026-11-11 is a Wednesday.
		{"2026-11-11T10:00:00+01:00", "monday 05:00", "2026-11-16T05:00:00+01:00", true},
		{"2026-11-11T10:00:00+01:00", "wednesday 10:30", "2026-11-11T10:30:00+01:00", true},
		{"2026-11-11T10:30:00+01:00", "wednesday 10:30", "2026-11-18T10:30:00+01:00", true},
		{"2026-11-11T10:00:00+01:00", "nonsense", "", false},
	}
	for _, c := range cases {
		now, _ := time.Parse(time.RFC3339, c.now)
		got, ok := nextDependencyUpdateRun(c.sched, now.In(loc))
		if ok != c.ok {
			t.Errorf("%s: ok = %v", c.sched, ok)
			continue
		}
		if ok && got.Format(time.RFC3339) != c.want {
			t.Errorf("%s from %s: got %s want %s", c.sched, c.now, got.Format(time.RFC3339), c.want)
		}
	}
}

func TestDependencyUpdateStatusToolIsListedReadOnly(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range tools {
		if tl.Name == "dependency_update_status" {
			found = true
		}
	}
	if !found {
		t.Error(`tool "dependency_update_status" is not registered`)
	}
	if got, ok := federation.RequiredCapForMCPTool("dependency_update_status"); !ok || got != federation.CapAutonomousRead {
		t.Errorf("capability = %q (listed %v), want %q", got, ok, federation.CapAutonomousRead)
	}
	tool := s.toolDependencyUpdateStatus()
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("must be marked read-only")
	}
}
