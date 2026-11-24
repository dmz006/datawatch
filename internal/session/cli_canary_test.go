package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func canaryManager(t *testing.T, version string) (*Manager, *[]string) {
	t.Helper()
	mgr, _ := newTestManagerWithFake(t)
	cur := &version
	mgr.SetClaudeCLIVersionRunner(func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(*cur + " (Claude Code)\n"), nil
	})
	var alerts []string
	mgr.SetPushAlertFn(func(level, title, body string) { alerts = append(alerts, level+"|"+title+"|"+body) })
	t.Cleanup(func() { mgr.SetClaudeCLIVersionRunner(nil) })
	return mgr, &alerts
}

// Every recorded pane has a stated expectation, so a fixture added without one
// cannot silently pass the replay.
func TestCLICanary_EveryFixtureHasAnExpectation(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "delivery"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		if _, ok := canaryExpectations[e.Name()]; !ok {
			t.Errorf("fixture %s has no canary expectation", e.Name())
		}
	}
	for name := range canaryExpectations {
		if _, err := os.Stat(filepath.Join("testdata", "delivery", name)); err != nil {
			t.Errorf("canary expectation for %s has no fixture: %v", name, err)
		}
	}
}

// With the shipped fixtures the replay is clean on the first run, and a run
// with an unchanged version does nothing at all.
func TestCLICanary_CleanReplayRecordsVersionAndStaysQuiet(t *testing.T) {
	mgr, alerts := canaryManager(t, "2.1.331")
	res := mgr.RunClaudeCLICanary()
	if !res.Replayed || len(res.Failures) != 0 {
		t.Fatalf("first run = %+v, want a clean replay", res)
	}
	if len(*alerts) != 0 {
		t.Errorf("clean replay raised alerts: %v", *alerts)
	}
	if res := mgr.RunClaudeCLICanary(); res.Replayed {
		t.Errorf("second run with the same version replayed again: %+v", res)
	}
}

// A changed version replays once; a pane that no longer reads as expected
// raises exactly one alert naming the version and the fixture, and the alert is
// not repeated on the next run.
func TestCLICanary_VersionChangeWithBrokenFixtureRaisesOneAlert(t *testing.T) {
	mgr, alerts := canaryManager(t, "2.1.331")
	mgr.RunClaudeCLICanary()

	// The next CLI draws the idle input row so the check no longer finds it.
	restore := canaryFixtureOverride(map[string]string{
		"claude-2.1.331-startup-idle.txt": "Welcome\n> Try something\n",
	})
	defer restore()
	mgr.SetClaudeCLIVersionRunner(func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("2.1.340 (Claude Code)\n"), nil
	})
	res := mgr.RunClaudeCLICanary()
	if !res.Replayed || len(res.Failures) == 0 {
		t.Fatalf("run after a version change = %+v, want failures", res)
	}
	if len(*alerts) != 1 {
		t.Fatalf("alerts = %v, want exactly one", *alerts)
	}
	for _, want := range []string{"2.1.340", "claude-2.1.331-startup-idle.txt"} {
		if !strings.Contains((*alerts)[0], want) {
			t.Errorf("alert %q does not name %q", (*alerts)[0], want)
		}
	}
	mgr.RunClaudeCLICanary()
	if len(*alerts) != 1 {
		t.Errorf("alert repeated for the same version: %v", *alerts)
	}
}

// An unknown version (no claude tool) never replays and never alerts.
func TestCLICanary_UnknownVersionDoesNothing(t *testing.T) {
	mgr, alerts := canaryManager(t, "x")
	mgr.SetClaudeCLIVersionRunner(func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, os.ErrNotExist
	})
	if res := mgr.RunClaudeCLICanary(); res.Replayed || len(*alerts) != 0 {
		t.Errorf("unknown version: %+v alerts %v", res, *alerts)
	}
}
