package autonomous

import "testing"

func swTmuxKinds(fs []SupervisorWatchFinding) int {
	n := 0
	for _, f := range fs {
		if f.Kind == SupervisorWatchTmuxRestart {
			n++
		}
	}
	return n
}

// The pass builds a fresh host every poll, so the tmux baseline must live on
// the manager: with no configured baseline the first poll stores one, a
// restart after it raises, and the same state afterwards does not raise again.
func TestSupervisorWatchTmuxBaselinePersistsAcrossPolls(t *testing.T) {
	m := swNewManager(t)
	probe := &fakeSupervisorWatchHostProbe{restartCount: 3, activeSince: "stamp-1"}
	m.SetSupervisorWatchProbe(probe)
	cfg := DefaultSupervisorWatchConfig()

	if got := swTmuxKinds(m.supervisorWatchRawFindings(cfg, nil)); got != 0 {
		t.Fatalf("first poll raised %d tmux_restart findings, want 0 (first sight stores the baseline)", got)
	}
	if got := swTmuxKinds(m.supervisorWatchRawFindings(cfg, nil)); got != 0 {
		t.Fatalf("unchanged second poll raised %d, want 0", got)
	}
	probe.restartCount = 4
	if got := swTmuxKinds(m.supervisorWatchRawFindings(cfg, nil)); got != 1 {
		t.Fatalf("poll after a restart raised %d, want 1", got)
	}
	if got := swTmuxKinds(m.supervisorWatchRawFindings(cfg, nil)); got != 0 {
		t.Fatalf("poll after the restart was seen raised %d, want 0 (baseline moved on)", got)
	}
}

// A configured baseline seeds the first poll; changing the configured value
// reseeds, and clearing it goes back to first-sight behaviour.
func TestSupervisorWatchTmuxBaselineConfigSeedsAndReseeds(t *testing.T) {
	m := swNewManager(t)
	probe := &fakeSupervisorWatchHostProbe{restartCount: 3, activeSince: "stamp-1"}
	m.SetSupervisorWatchProbe(probe)
	cfg := DefaultSupervisorWatchConfig()

	cfg.TmuxRestartBaseline = "2" + supervisorWatchBaselineSep + "stamp-1"
	if got := swTmuxKinds(m.supervisorWatchRawFindings(cfg, nil)); got != 1 {
		t.Fatalf("poll against a different configured baseline raised %d, want 1", got)
	}
	if got := swTmuxKinds(m.supervisorWatchRawFindings(cfg, nil)); got != 0 {
		t.Fatalf("configured baseline raised again on the next poll (%d), want 0", got)
	}
	cfg.TmuxRestartBaseline = "9" + supervisorWatchBaselineSep + "stamp-1"
	if got := swTmuxKinds(m.supervisorWatchRawFindings(cfg, nil)); got != 1 {
		t.Fatalf("a new configured baseline raised %d, want 1", got)
	}
	cfg.TmuxRestartBaseline = ""
	if got := swTmuxKinds(m.supervisorWatchRawFindings(cfg, nil)); got != 0 {
		t.Fatalf("clearing the configured baseline raised %d, want 0", got)
	}
	probe.restartCount = 5
	if got := swTmuxKinds(m.supervisorWatchRawFindings(cfg, nil)); got != 1 {
		t.Fatalf("restart after clearing raised %d, want 1", got)
	}
}
