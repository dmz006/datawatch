package main

import (
	"errors"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// recordVerifierRunForTest mirrors the verifier record site: it runs the
// run-cap retry hook with the scripted runs, lets onCap set the cap flag the
// way the closure in main does, then records the category into stats.
func recordVerifierRunForTest(t *testing.T, stats *autonomouspkg.VerifierStats, f *fakeVerifierRuns, summaryOf func(error) string) {
	t.Helper()
	runCapEnded := false
	_, serr := runVerifierSessionWithRunCapHook([]string{"ollama-a"}, "ollama-a", "m1", f.run,
		retryModelRegistryDefault,
		func(capEnd *autonomouspkg.OneShotNoOutputError) {
			if capEnd != nil && capEnd.RunCap {
				runCapEnded = true
			}
		})
	ok, resultCategory := serr == nil, ""
	if serr != nil {
		resultCategory = verifierSessionInfraResult(serr).Category
	}
	cat := verifierStatsCategory(ok, resultCategory, summaryOf(serr), false, serr, runCapEnded)
	stats.Record(0, time.Minute, cat)
}

func TestVerifierRunCapStatsCountCapEndIncrementsOnce(t *testing.T) {
	stats := autonomouspkg.NewVerifierStats(nil)
	f := &fakeVerifierRuns{errs: map[string]error{"ollama-a": runCapErr()}}
	recordVerifierRunForTest(t, stats, f, func(err error) string {
		if err == nil {
			return ""
		}
		return "verifier(session): " + err.Error()
	})
	v := stats.Snapshot()
	if v.RunCapEnds != 1 {
		t.Fatalf("RunCapEnds = %d, want 1", v.RunCapEnds)
	}
	if v.Categories[autonomouspkg.VerifierCatRunCap] != 1 {
		t.Fatalf("run_cap category = %d, want 1", v.Categories[autonomouspkg.VerifierCatRunCap])
	}
	if v.Runs != 1 {
		t.Fatalf("Runs = %d, want 1", v.Runs)
	}
}

func TestVerifierRunCapStatsCountWatchdogEndUnchanged(t *testing.T) {
	stats := autonomouspkg.NewVerifierStats(nil)
	watchdogErr := errors.New("verifier no-progress watchdog: stalled")
	f := &fakeVerifierRuns{errs: map[string]error{"ollama-a": watchdogErr}}
	recordVerifierRunForTest(t, stats, f, func(err error) string {
		return "verifier(session): " + err.Error()
	})
	v := stats.Snapshot()
	if v.RunCapEnds != 0 || v.Categories[autonomouspkg.VerifierCatRunCap] != 0 {
		t.Fatalf("RunCapEnds=%d run_cap=%d, want 0 0", v.RunCapEnds, v.Categories[autonomouspkg.VerifierCatRunCap])
	}
	if v.WatchdogEnds != 1 {
		t.Fatalf("WatchdogEnds = %d, want 1", v.WatchdogEnds)
	}
}

func TestVerifierRunCapStatsCountPassUnchanged(t *testing.T) {
	stats := autonomouspkg.NewVerifierStats(nil)
	f := &fakeVerifierRuns{
		errs:    map[string]error{},
		content: map[string]string{"ollama-a": `{"ok":true}`},
	}
	recordVerifierRunForTest(t, stats, f, func(error) string { return "" })
	v := stats.Snapshot()
	if v.RunCapEnds != 0 || v.Categories[autonomouspkg.VerifierCatRunCap] != 0 {
		t.Fatalf("RunCapEnds=%d run_cap=%d, want 0 0", v.RunCapEnds, v.Categories[autonomouspkg.VerifierCatRunCap])
	}
	if v.Categories[autonomouspkg.VerifierCatPass] != 1 {
		t.Fatalf("pass = %d, want 1", v.Categories[autonomouspkg.VerifierCatPass])
	}
}

func TestVerifierRunCapStatsCountStatsCategoryHelper(t *testing.T) {
	if got := verifierStatsCategory(false, "content", "bad diff", false, nil, true); got != autonomouspkg.VerifierCatRunCap {
		t.Fatalf("cap = %q", got)
	}
	if got := verifierStatsCategory(false, "content", "bad diff", false, nil, false); got != autonomouspkg.VerifierCatContent {
		t.Fatalf("content = %q", got)
	}
	if got := verifierStatsCategory(false, "", "boom", false, errors.New("boom"), false); got != autonomouspkg.VerifierCatInfra {
		t.Fatalf("err infra = %q", got)
	}
}
