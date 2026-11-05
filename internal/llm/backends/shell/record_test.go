package shell

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLaunchAndExitRecords(t *testing.T) {
	dir := t.TempDir()
	fakeTmux := filepath.Join(dir, "tmux")
	if err := os.WriteFile(fakeTmux, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ch := make(chan CommandRecord, 2)
	SetRecorder(func(r CommandRecord) { ch <- r })
	defer SetRecorder(nil)
	t0 := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	recordNow = func() time.Time { return t0 }
	defer func() { recordNow = time.Now }()

	b := &Backend{}
	if err := b.Launch(context.Background(), "", "cs-rec", dir, ""); err != nil {
		t.Fatal(err)
	}
	var launch CommandRecord
	select {
	case launch = <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no launch record")
	}
	if launch.Session != "cs-rec" || launch.Command == "" || launch.ExitCode != nil || !launch.Start.Equal(t0) {
		t.Errorf("launch record %+v", launch)
	}

	ReportExit("cs-rec", launch.Command, 2, launch.Start)
	select {
	case r := <-ch:
		if r.ExitCode == nil || *r.ExitCode != 2 || r.Session != "cs-rec" || !r.Start.Equal(t0) {
			t.Errorf("exit record %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no exit record")
	}
}
