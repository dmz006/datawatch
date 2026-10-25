package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fakeLaunchDrainer records the grace it was asked to drain with and returns
// a canned set of rolled-back session ids.
type fakeLaunchDrainer struct {
	graces []time.Duration
	rolled []string
}

func (f *fakeLaunchDrainer) DrainLaunches(grace time.Duration) []string {
	f.graces = append(f.graces, grace)
	return f.rolled
}

// TestShutdownLaunchPassesGraceConstant checks the helper drains with the fixed
// shutdown grace constant, exactly once.
func TestShutdownLaunchPassesGraceConstant(t *testing.T) {
	d := &fakeLaunchDrainer{}
	drainLaunchesOnShutdown(d, &bytes.Buffer{})

	if len(d.graces) != 1 {
		t.Fatalf("DrainLaunches calls = %d, want 1", len(d.graces))
	}
	if d.graces[0] != shutdownLaunchGrace {
		t.Fatalf("grace = %v, want shutdownLaunchGrace %v", d.graces[0], shutdownLaunchGrace)
	}
	if shutdownLaunchGrace != 15*time.Second {
		t.Fatalf("shutdownLaunchGrace = %v, want 15s", shutdownLaunchGrace)
	}
}

// TestShutdownLaunchLogsRolledBackIDs checks every rolled-back session id gets
// its own log line, in the order the drainer returned them.
func TestShutdownLaunchLogsRolledBackIDs(t *testing.T) {
	d := &fakeLaunchDrainer{rolled: []string{"host-aaaa1111", "host-bbbb2222"}}
	var out bytes.Buffer
	drainLaunchesOnShutdown(d, &out)

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines = %d, want 2; output:\n%s", len(lines), out.String())
	}
	for i, id := range d.rolled {
		if !strings.HasPrefix(lines[i], "[session] ") || !strings.Contains(lines[i], id) {
			t.Fatalf("line %d = %q, want a [session] line naming %s", i, lines[i], id)
		}
	}
}

// TestShutdownLaunchEmptyDrainNoLog checks an empty drain, meaning every launch
// finished inside the grace, writes no rollback line.
func TestShutdownLaunchEmptyDrainNoLog(t *testing.T) {
	d := &fakeLaunchDrainer{rolled: nil}
	var out bytes.Buffer
	drainLaunchesOnShutdown(d, &out)

	if out.Len() != 0 {
		t.Fatalf("empty drain wrote %q, want no output", out.String())
	}
}
