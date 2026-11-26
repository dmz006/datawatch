// autonomous.automation.cleanup_shared_cache_sweep must be readable and
// writable through GET and PUT /api/config, reach the running manager in both
// directions (an explicit false too), and refuse a value that is not a boolean.

package server

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

const sharedCacheSweepKey = "autonomous.automation.cleanup_shared_cache_sweep"

func TestSharedCacheSweepConfigRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	read := func() string {
		raw := getAutonomousConfigMap(t, s)["automation"]
		switch {
		case strings.Contains(string(raw), `"cleanup_shared_cache_sweep":true`):
			return "true"
		case strings.Contains(string(raw), `"cleanup_shared_cache_sweep":false`):
			return "false"
		}
		return "missing"
	}
	live := func() bool {
		return mgr.Config().Automation.CleanupSharedCacheSweep
	}
	if got := read(); got != "false" {
		t.Fatalf("default on GET = %s, want false", got)
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{sharedCacheSweepKey: true})
	if got := read(); got != "true" || !live() {
		t.Fatalf("after PUT true: GET %s, running manager %v", got, live())
	}
	saved, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "cleanup_shared_cache_sweep: true") {
		t.Fatalf("config file lacks the switch:\n%s", saved)
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{sharedCacheSweepKey: false})
	if got := read(); got != "false" || live() {
		t.Fatalf("after PUT false: GET %s, running manager %v; switching off must reach it", got, live())
	}
	for _, bad := range []any{"maybe", "on", 1} {
		if code := progressPutStatus(t, s, map[string]any{sharedCacheSweepKey: bad}); code != http.StatusBadRequest {
			t.Errorf("PUT %v: want 400, got %d", bad, code)
		}
	}
}

func TestSharedCacheSweepAutonomousConfigRoute(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	put := func(body map[string]any) {
		t.Helper()
		putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", body)
	}
	put(map[string]any{"enabled": mgr.Config().Enabled, "automation": map[string]any{"cleanup_shared_cache_sweep": true}})
	if !mgr.Config().Automation.CleanupSharedCacheSweep || !s.cfg.Autonomous.Automation.CleanupSharedCacheSweep {
		t.Fatal("PUT /api/autonomous/config true did not reach the manager and the daemon config")
	}
	// Another key leaves it as it is; an explicit false turns it off.
	put(map[string]any{"automation": map[string]any{"cleanup_mode": "report"}})
	if !mgr.Config().Automation.CleanupSharedCacheSweep {
		t.Fatal("a PUT of another key switched the sweep off")
	}
	put(map[string]any{"automation": map[string]any{"cleanup_shared_cache_sweep": false}})
	if mgr.Config().Automation.CleanupSharedCacheSweep || s.cfg.Autonomous.Automation.CleanupSharedCacheSweep {
		t.Fatal("an explicit false did not switch the sweep off")
	}
}
