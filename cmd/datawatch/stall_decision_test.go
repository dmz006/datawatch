package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

const workerStallReasonPrefix = "no output, no children, no file changes for"

func TestWorkerStallDecisionKillsOnlyWithoutEvidence(t *testing.T) {
	const threshold = 10 * time.Minute
	alive := func() bool { return true }

	cases := []struct {
		name        string
		logAge      time.Duration // negative: no log file
		fileAge     time.Duration // negative: no file in the work dir
		childAlive  func() bool
		backendBusy func() bool
		threshold   time.Duration
		wantStalled bool
	}{
		{
			name:        "no evidence",
			logAge:      time.Hour,
			fileAge:     time.Hour,
			threshold:   threshold,
			wantStalled: true,
		},
		{
			name:        "no evidence and no files at all",
			logAge:      time.Hour,
			fileAge:     -1,
			threshold:   threshold,
			wantStalled: true,
		},
		{
			name:       "live child",
			logAge:     time.Hour,
			fileAge:    time.Hour,
			childAlive: alive,
			threshold:  threshold,
		},
		{
			name:        "busy backend",
			logAge:      time.Hour,
			fileAge:     time.Hour,
			backendBusy: alive,
			threshold:   threshold,
		},
		{
			name:      "recent file",
			logAge:    time.Hour,
			fileAge:   time.Minute,
			threshold: threshold,
		},
		{
			name:      "threshold zero",
			logAge:    time.Hour,
			fileAge:   time.Hour,
			threshold: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "session.log")
			writeAgedFile(t, logPath, tc.logAge)
			workDir := t.TempDir()
			if tc.fileAge >= 0 {
				writeAgedFile(t, filepath.Join(workDir, "changed.go"), tc.fileAge)
			}

			childAlive := tc.childAlive
			if childAlive == nil {
				childAlive = noSignal
			}
			backendBusy := tc.backendBusy
			if backendBusy == nil {
				backendBusy = noSignal
			}

			stalled, reason := decideWorkerStall(workerStallObservation{
				LogPath:     logPath,
				WorkDir:     workDir,
				Threshold:   tc.threshold,
				Now:         time.Now(),
				ChildAlive:  childAlive,
				BackendBusy: backendBusy,
			})
			if stalled != tc.wantStalled {
				t.Fatalf("decideWorkerStall stalled = %v, want %v (reason %q)", stalled, tc.wantStalled, reason)
			}
			if tc.wantStalled && !strings.Contains(reason, workerStallReasonPrefix) {
				t.Errorf("stalled reason = %q, want it to contain %q", reason, workerStallReasonPrefix)
			}
		})
	}
}

func TestWorkerStallDecisionReasonNamesEvidence(t *testing.T) {
	const threshold = 10 * time.Minute
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session.log")
	writeAgedFile(t, logPath, time.Hour)

	stalled, reason := decideWorkerStall(workerStallObservation{
		LogPath:     logPath,
		WorkDir:     t.TempDir(),
		Threshold:   threshold,
		Now:         time.Now(),
		ChildAlive:  noSignal,
		BackendBusy: noSignal,
	})
	if !stalled {
		t.Fatalf("expected a stall with no evidence, reason %q", reason)
	}

	err := workerStallError(reason, threshold)
	msg := err.Error()
	for _, want := range []string{"no output", "no children", "no file changes for", "stale_task_seconds=600s"} {
		if !strings.Contains(msg, want) {
			t.Errorf("failure text %q does not contain %q", msg, want)
		}
	}
	if !errors.Is(err, autonomouspkg.ErrWorkerStalled) {
		t.Errorf("failure %v does not wrap ErrWorkerStalled", err)
	}
}
