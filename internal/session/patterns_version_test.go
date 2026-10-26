package session

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newPatternsManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(m)
	// Inject a canned claude version so no test depends on a real claude
	// binary being on this machine's PATH.
	m.SetClaudeCLIVersionRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("claude 2.0.55 (f742c65)\n"), nil
	})
	return m
}

func TestPatternsVersion_StableAcrossCalls(t *testing.T) {
	m := newPatternsManager(t)
	a := m.RateLimitPatternsVersion()
	if a == "" || len(a) != patternsVersionLen {
		t.Fatalf("version %q, want %d hex chars", a, patternsVersionLen)
	}
	for i := 0; i < 5; i++ {
		if b := m.RateLimitPatternsVersion(); b != a {
			t.Fatalf("call %d: version %q != %q", i, b, a)
		}
	}
	// A second manager with the same effective set agrees.
	if other := newPatternsManager(t).RateLimitPatternsVersion(); other != a {
		t.Errorf("same patterns, different managers: %q != %q", other, a)
	}
}

func TestPatternsVersion_ChangesExactlyWithPatternSet(t *testing.T) {
	m := newPatternsManager(t)
	base := m.RateLimitPatternsVersion()

	// Override with a custom set: changes.
	m.SetDetection(config.DetectionConfig{RateLimitPatterns: []string{"a limit", "b limit"}})
	custom := m.RateLimitPatternsVersion()
	if custom == base {
		t.Fatal("overriding the pattern set did not change the version")
	}

	// Order and duplicates are not a change.
	m.SetDetection(config.DetectionConfig{RateLimitPatterns: []string{"b limit", "a limit", "b limit"}})
	if got := m.RateLimitPatternsVersion(); got != custom {
		t.Errorf("reorder/duplicate changed version: %q != %q", got, custom)
	}

	// Add, edit and remove each change it.
	seen := map[string]string{custom: "custom"}
	for name, set := range map[string][]string{
		"added":   {"a limit", "b limit", "c limit"},
		"edited":  {"a limit", "b limits"},
		"removed": {"a limit"},
		"joined":  {"a limitb limit"}, // concatenation must not collide
	} {
		m.SetDetection(config.DetectionConfig{RateLimitPatterns: set})
		v := m.RateLimitPatternsVersion()
		if prev, dup := seen[v]; dup {
			t.Errorf("%s collides with %s: %q", name, prev, v)
		}
		seen[v] = name
	}

	// Clearing the override restores the built-in set's version.
	m.SetDetection(config.DetectionConfig{})
	if got := m.RateLimitPatternsVersion(); got != base {
		t.Errorf("cleared override: version %q, want %q", got, base)
	}
}

func TestPatternsStale_FlipsOnChangeAndClearsOnRecheck(t *testing.T) {
	m := newPatternsManager(t)

	// First run without a recheck is stale.
	v, stale := m.RateLimitPatternsStatus()
	if !stale || v != m.RateLimitPatternsVersion() {
		t.Fatalf("first run: version=%q stale=%v, want stale=true", v, stale)
	}

	got, err := m.RecheckRateLimitPatterns()
	if err != nil {
		t.Fatal(err)
	}
	if got != v {
		t.Errorf("recheck returned %q, want %q", got, v)
	}
	if v2, stale := m.RateLimitPatternsStatus(); stale || v2 != v {
		t.Fatalf("after recheck: version=%q stale=%v, want %q stale=false", v2, stale, v)
	}

	// Changing the pattern set flips stale back to true with a new version.
	m.SetDetection(config.DetectionConfig{RateLimitPatterns: []string{"new wording"}})
	v3, stale := m.RateLimitPatternsStatus()
	if !stale || v3 == v {
		t.Fatalf("after change: version=%q stale=%v, want new version and stale=true", v3, stale)
	}

	// Recheck clears it again; reverting to the earlier set is stale again.
	if _, err := m.RecheckRateLimitPatterns(); err != nil {
		t.Fatal(err)
	}
	if _, stale := m.RateLimitPatternsStatus(); stale {
		t.Error("stale after second recheck")
	}
	m.SetDetection(config.DetectionConfig{})
	if _, stale := m.RateLimitPatternsStatus(); !stale {
		t.Error("reverting to the earlier set must be stale until rechecked")
	}
}

func TestPatternsRecheck_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	fake := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("claude 2.0.55 (f742c65)\n"), nil
	}
	m1, err := NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(m1)
	m1.SetClaudeCLIVersionRunner(fake)
	if _, err := m1.RecheckRateLimitPatterns(); err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(m2)
	m2.SetClaudeCLIVersionRunner(fake)
	if _, stale := m2.RateLimitPatternsStatus(); stale {
		t.Error("recheck must survive a restart (same data dir)")
	}
	m2.SetDetection(config.DetectionConfig{RateLimitPatterns: []string{"x"}})
	if _, stale := m2.RateLimitPatternsStatus(); !stale {
		t.Error("changed patterns after restart must be stale")
	}
}

func TestPatternsRecheck_UnreadableEntryIsStale(t *testing.T) {
	m := newPatternsManager(t)
	if err := os.WriteFile(m.patternsRecheckedPath(), []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stale := m.RateLimitPatternsStatus(); !stale {
		t.Error("a mismatching rechecked entry must read as stale")
	}
	if _, err := m.RecheckRateLimitPatterns(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(m.patternsRecheckedPath()); strings.Contains(string(b), "garbage") {
		t.Error("recheck entry should be rewritable")
	}
	if _, stale := m.RateLimitPatternsStatus(); stale {
		t.Error("stale after recheck over a bad entry")
	}
}
