package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// fakeDoneKiller records EndSession calls in place of the session manager, so
// these tests never touch a real tmux server.
type fakeDoneKiller struct {
	killed []string
	whys   []session.EndReason
}

func (f *fakeDoneKiller) EndSession(fullID string, why session.EndReason) error {
	f.killed = append(f.killed, fullID)
	f.whys = append(f.whys, why)
	return nil
}

const doneCheckpointAllDone = "## Steps completed\n- Step 1\n- Step 2\n\n## Current step\nnone\n\n## Remaining\nnone\n"

const doneCheckpointOpen = "## Steps completed\n- Step 1\n\n## Current step\nnone\n\n## Remaining\n- Step 2: write the deliverable\n"

func writeDoneScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return name
}

func TestDoneCheckpointVerifyPassingScriptCompletesAttempt(t *testing.T) {
	dir := t.TempDir()
	script := writeDoneScript(t, dir, "verify.sh", "#!/usr/bin/env bash\necho deliverable > deliverable.txt\nexit 0\n")
	killer := &fakeDoneKiller{}
	committed := 0
	commit := func() error {
		committed++
		return nil
	}

	vr, out, passed := doneCheckpointVerify(killer, "sess1", doneCheckpointAllDone, script, dir, commit)

	if !passed {
		t.Fatalf("passed = false, want true; script output %q", out)
	}
	if !vr.OK {
		t.Errorf("VerificationResult.OK = false, want true")
	}
	if !strings.Contains(vr.Summary, "checkpoint reported all steps done") || !strings.Contains(vr.Summary, "verification script passed") {
		t.Errorf("Summary = %q, want it to say the checkpoint reported all steps done and the script passed", vr.Summary)
	}
	if _, err := os.Stat(filepath.Join(dir, "deliverable.txt")); err != nil {
		t.Errorf("deliverable missing: %v", err)
	}
	if len(killer.killed) != 1 || killer.killed[0] != "sess1" {
		t.Errorf("Kill calls = %v, want [sess1]", killer.killed)
	}
	if committed != 1 {
		t.Errorf("commit calls = %d, want 1", committed)
	}
}

func TestDoneCheckpointVerifyFailingScriptStillStalls(t *testing.T) {
	dir := t.TempDir()
	script := writeDoneScript(t, dir, "verify.sh", "#!/usr/bin/env bash\necho 'deliverable not found'\nexit 1\n")
	killer := &fakeDoneKiller{}
	committed := 0
	commit := func() error {
		committed++
		return nil
	}

	vr, out, passed := doneCheckpointVerify(killer, "sess1", doneCheckpointAllDone, script, dir, commit)

	if passed {
		t.Fatalf("passed = true, want false: a failing script must fall through to the stall path")
	}
	if vr.OK {
		t.Errorf("VerificationResult.OK = true, want the zero value on fall-through")
	}
	if !strings.Contains(out, "deliverable not found") {
		t.Errorf("script output = %q, want it to carry the script output for the retry hint", out)
	}
	if len(killer.killed) != 0 {
		t.Errorf("Kill calls = %v, want none: the stall path kills after its own snapshot", killer.killed)
	}
	if committed != 0 {
		t.Errorf("commit calls = %d, want 0", committed)
	}
}

func TestDoneCheckpointVerifySkippedWithoutDoneCheckpoint(t *testing.T) {
	dir := t.TempDir()
	script := writeDoneScript(t, dir, "verify.sh", "#!/usr/bin/env bash\necho ran > marker.txt\nexit 0\n")
	killer := &fakeDoneKiller{}

	_, out, passed := doneCheckpointVerify(killer, "sess1", doneCheckpointOpen, script, dir, nil)

	if passed {
		t.Fatalf("passed = true, want false when the checkpoint is not done")
	}
	if out != "" {
		t.Errorf("script output = %q, want empty because the script must not run", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker.txt")); !os.IsNotExist(err) {
		t.Errorf("marker.txt exists (err=%v): the verification script ran although the checkpoint was not done", err)
	}
	if len(killer.killed) != 0 {
		t.Errorf("Kill calls = %v, want none", killer.killed)
	}
}

func TestDoneCheckpointVerifyReadsCheckpointFromWorkDir(t *testing.T) {
	dir := t.TempDir()
	if got := readDoneCheckpoint(dir); got != "" {
		t.Errorf("readDoneCheckpoint with no file = %q, want empty", got)
	}
	if err := os.WriteFile(filepath.Join(dir, doneCheckpointFile), []byte(doneCheckpointAllDone), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readDoneCheckpoint(dir); got != doneCheckpointAllDone {
		t.Errorf("readDoneCheckpoint = %q, want the file text", got)
	}
}
