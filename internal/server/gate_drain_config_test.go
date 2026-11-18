package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// autonomous.gate_drain_minutes over REST: the default reads as 10, a PUT
// stores, surfaces and propagates it (0 included), and a bad value is
// refused with 400 and stores nothing.
func TestGateDrainConfigAPI_RoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	if got := daemonTaskConfigGetInt(t, s, "gate_drain_minutes"); got != "10" {
		t.Fatalf("default GET = %s, want 10", got)
	}
	for _, v := range []any{0, 25, "7"} {
		code, body := putDaemonTaskConfig(t, s, map[string]any{"autonomous.gate_drain_minutes": v})
		if code != http.StatusOK {
			t.Fatalf("PUT %v: want 200, got %d %s", v, code, body)
		}
	}
	if got := daemonTaskConfigGetInt(t, s, "gate_drain_minutes"); got != "7" {
		t.Fatalf("GET after PUT = %s, want 7", got)
	}
	if mgr.Config().GateDrainMinutes != 7 {
		t.Fatalf("live manager = %d, want 7", mgr.Config().GateDrainMinutes)
	}
	if code, _ := putDaemonTaskConfig(t, s, map[string]any{"autonomous.gate_drain_minutes": 0}); code != http.StatusOK {
		t.Fatal("0 must be accepted")
	}
	if got := daemonTaskConfigGetInt(t, s, "gate_drain_minutes"); got != "0" {
		t.Fatalf("explicit 0 read back as %s", got)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Autonomous.GateDrainMinutes != 0 {
		t.Fatalf("saved config = %d, want 0", loaded.Autonomous.GateDrainMinutes)
	}
}

func TestGateDrainConfigAPI_Refuses(t *testing.T) {
	for _, v := range []any{-1, 1.5, "abc", true} {
		s, mgr, _ := newMaxConcurrentTasksServer(t)
		code, body := putDaemonTaskConfig(t, s, map[string]any{"autonomous.gate_drain_minutes": v})
		if code != http.StatusBadRequest || !strings.Contains(body, "autonomous.gate_drain_minutes") {
			t.Fatalf("PUT %v: want 400 naming the key, got %d %q", v, code, body)
		}
		if got := daemonTaskConfigGetInt(t, s, "gate_drain_minutes"); got != "10" {
			t.Fatalf("stored after refusal = %s, want 10", got)
		}
		if mgr.Config().GateDrainMinutes == -1 {
			t.Fatal("refused value reached the manager")
		}
	}
}
