package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStaleWorkerCheck_FreshFileIsNotStale(t *testing.T) {
	f := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, since, err := staleWorkerCheck(f, 30*time.Minute, time.Now())
	if err != nil || stale {
		t.Fatalf("stale=%v since=%v err=%v", stale, since, err)
	}
}

func TestStaleWorkerCheck_OldFileIsStale(t *testing.T) {
	f := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-53 * time.Minute)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
	stale, since, err := staleWorkerCheck(f, 30*time.Minute, time.Now())
	if err != nil || !stale || since < 50*time.Minute {
		t.Fatalf("stale=%v since=%v err=%v", stale, since, err)
	}
}

func TestStaleWorkerCheck_ThresholdZeroDisabled(t *testing.T) {
	f := filepath.Join(t.TempDir(), "output.log")
	os.WriteFile(f, []byte("x"), 0o644)                        //nolint:errcheck
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(f, old, old)                                    //nolint:errcheck
	if stale, _, _ := staleWorkerCheck(f, 0, time.Now()); stale {
		t.Fatal("threshold<=0 must disable the check")
	}
}

func TestStaleWorkerCheck_MissingFileReturnsError(t *testing.T) {
	if _, _, err := staleWorkerCheck(filepath.Join(t.TempDir(), "gone.log"), time.Minute, time.Now()); err == nil {
		t.Fatal("missing file must return an error, not a silent false")
	}
}
