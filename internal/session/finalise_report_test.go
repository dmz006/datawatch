package session

import (
	"testing"
	"time"
)

var finaliseReportTime = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func TestLastFinaliseEmptyBeforeFirstRun(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sum, ok := mgr.LastFinalise()
	if ok {
		t.Fatal("LastFinalise reported a run before any run happened")
	}
	if sum != (FinaliseSummary{}) {
		t.Fatalf("summary before the first run = %+v, want the zero value", sum)
	}
}

func TestRunFinaliseRecordsCountsAndTime(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	seedGoneRecord(t, mgr, "fr01", StateRunning, "output without the marker\n")
	// A record that is already final is skipped.
	seedGoneRecord(t, mgr, "fr02", StateComplete, "done\n")

	sum := mgr.RunFinalise(finaliseReportTime)
	if !sum.At.Equal(finaliseReportTime) {
		t.Fatalf("At = %v, want %v", sum.At, finaliseReportTime)
	}
	if !sum.TerminalListOK {
		t.Fatal("TerminalListOK must be true against the fake")
	}
	if sum.FinalisedRecords != 1 {
		t.Fatalf("FinalisedRecords = %d, want 1", sum.FinalisedRecords)
	}
	if sum.Skipped < 1 {
		t.Fatalf("Skipped = %d, want at least 1", sum.Skipped)
	}

	got, ok := mgr.LastFinalise()
	if !ok {
		t.Fatal("LastFinalise reported no run after RunFinalise")
	}
	if got != sum {
		t.Fatalf("LastFinalise = %+v, want the returned summary %+v", got, sum)
	}

	// A later run replaces the stored summary.
	later := finaliseReportTime.Add(time.Hour)
	second := mgr.RunFinalise(later)
	if second.FinalisedRecords != 0 || second.FinalisedFolders != 0 {
		t.Fatalf("second run finalised %+v, want nothing", second)
	}
	got, _ = mgr.LastFinalise()
	if !got.At.Equal(later) || got != second {
		t.Fatalf("LastFinalise after second run = %+v, want %+v", got, second)
	}
}

func TestRunFinaliseCountsStoreAndFolderPasses(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	// Two gone records held by the daemon.
	seedGoneRecord(t, mgr, "fs01", StateRunning, "output\n")
	seedGoneRecord(t, mgr, "fs02", StateStalled, "output\n")
	// One saved folder with no record behind it.
	dir := seedFolder(t, mgr, Session{ID: "fs03", TmuxSession: "cs-fs03", State: StateRunning}, "output\n")
	// A saved folder that is already final is skipped by the folder pass.
	final := seedFolder(t, mgr, Session{ID: "fs04", State: StateComplete}, "output\n")
	finalBefore := readFolderFile(t, final, "session.json")

	sum := mgr.RunFinalise(finaliseReportTime)
	if sum.FinalisedRecords != 2 {
		t.Fatalf("FinalisedRecords = %d, want 2", sum.FinalisedRecords)
	}
	if sum.FinalisedFolders != 1 {
		t.Fatalf("FinalisedFolders = %d, want 1", sum.FinalisedFolders)
	}
	if sum.Skipped < 1 {
		t.Fatalf("Skipped = %d, want at least 1 (the final folder)", sum.Skipped)
	}
	if !sum.TerminalListOK {
		t.Fatal("TerminalListOK must be true against the fake")
	}

	if got := readFolderSession(t, dir); got.State != StateKilled {
		t.Fatalf("folder state = %q, want %q", got.State, StateKilled)
	}
	assertFolderUntouched(t, final, finalBefore)
}
