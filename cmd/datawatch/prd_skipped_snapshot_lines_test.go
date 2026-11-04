// autonomous prd-get shows a counts-first line per task with a non-empty
// skipped_snapshot_paths ("left unstaged: <task> N (reason=count, ...)") in
// addition to the raw JSON (pretty-printed to stdout), which keeps the
// detailed path list.

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

const skippedSnapshotPRDBody = `{"id":"prd1","stories":[{"tasks":[
  {"id":"t1","skipped_snapshot_paths":[
     {"path":"a.log","reason":"ignored"},
     {"path":"b.tmp","reason":"size"},
     {"path":"c.tmp","reason":"size"}
  ]},
  {"id":"t2","files_touched":["src/main.go"]}
]}]}`

// runPRDGet executes the prd-get command against the capture daemon and
// returns the command's out stream plus os.Stdout (prettyPrint writes the
// raw JSON there directly).
func runPRDGet(t *testing.T, args ...string) (cmdOut string, stdout string) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	out := &bytes.Buffer{}
	cmd := newAutonomousPRDGetCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		_ = w.Close()
		t.Fatalf("execute: %v (output: %s)", err, out)
	}
	_ = w.Close()
	data, _ := io.ReadAll(r)
	return out.String(), string(data)
}

func TestAutonomousPRDGetShowsSkippedSnapshotLines(t *testing.T) {
	c := &captureDaemon{replyBody: skippedSnapshotPRDBody}
	withCaptureServer(t, c)

	cmdOut, stdout := runPRDGet(t, "prd1")

	// The raw JSON read keeps the detailed path list.
	if !strings.Contains(stdout, `"skipped_snapshot_paths"`) || !strings.Contains(stdout, `"a.log"`) {
		t.Errorf("raw JSON lacks the skipped_snapshot_paths detail: %s", stdout)
	}
	// The compact counts-first line shows for the task that skipped files.
	if !strings.Contains(cmdOut, "left unstaged: t1 3 (ignored=1, size=2)") {
		t.Errorf("missing the counts-first line: %s", cmdOut)
	}
	// ...and not for the task that skipped nothing.
	if strings.Contains(cmdOut, "left unstaged: t2") {
		t.Errorf("t2 skipped nothing, no line expected: %s", cmdOut)
	}
}

func TestAutonomousPRDGetNoSkippedPathsNoLine(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"prd1","stories":[{"tasks":[{"id":"t1"}]}]}`}
	withCaptureServer(t, c)

	cmdOut, _ := runPRDGet(t, "prd1")
	if strings.Contains(cmdOut, "left unstaged:") {
		t.Errorf("no task skipped anything, no line expected: %s", cmdOut)
	}
}
