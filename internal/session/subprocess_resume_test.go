package session

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// newDetachedSubprocess returns a running subprocess session whose task
// directory lives under t.TempDir(), saved to the manager's store.
func newDetachedSubprocess(t *testing.T, m *Manager, task string) *Session {
	t.Helper()
	dir := t.TempDir()
	sess := &Session{
		FullID:     "bl462-sub-" + filepath.Base(dir),
		Hostname:   m.hostname,
		Task:       task,
		ProjectDir: dir,
		LogFile:    filepath.Join(dir, "output.log"),
		Subprocess: true,
		State:      StateRunning,
	}
	if err := m.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}
	return sess
}

func waitForState(t *testing.T, m *Manager, id string, want State) *Session {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := m.store.Get(id); ok && got.State == want {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
	got, _ := m.store.Get(id)
	t.Fatalf("session %s: want state %s, got %+v", id, want, got)
	return nil
}

// BL462 F4: a subprocess task that is still running when the daemon restarts
// keeps its record running, and is finished by pid once it exits, with the
// exit status it recorded rather than a guess.
func TestSubprocessSurvivesDaemonRestart(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, "sleep 1")

	// Start the task the way runSubprocess does, but do not wait for it:
	// that is the state a daemon leaves behind when it is killed.
	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatalf("subprocessCommand: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	if err := os.WriteFile(subprocessFile(sess, subprocessPIDFile), []byte(fmt.Sprint(cmd.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}
	// The command line is empty until exec completes, so wait for it first.
	deadline := time.Now().Add(5 * time.Second)
	for !subprocessAlive(cmd.Process.Pid, sess.Task) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !m.reattachSubprocess(sess) {
		t.Fatal("a live subprocess must be re-attached, not failed")
	}
	if got, _ := m.store.Get(sess.FullID); got.State != StateRunning {
		t.Fatalf("live subprocess must stay running, got %s", got.State)
	}

	waitForState(t, m, sess.FullID, StateComplete)
	_ = cmd.Wait()
	if _, err := os.Stat(subprocessFile(sess, subprocessPIDFile)); !os.IsNotExist(err) {
		t.Fatalf("pid file must be removed once the task has ended, err=%v", err)
	}
}

// A task that ended while the daemon was down reports the exit status it
// recorded, not a guessed failure.
func TestSubprocessEndedWhileDaemonDownUsesRecordedStatus(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, "false")
	if err := os.WriteFile(subprocessFile(sess, subprocessRCFile), []byte("3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !m.reattachSubprocess(sess) {
		t.Fatal("an ended subprocess with a recorded status must be handled")
	}
	waitForState(t, m, sess.FullID, StateFailed)
}

// A task that is gone with no pid match and no recorded status is still
// marked failed, the boot behaviour before this fix.
func TestSubprocessGoneWithoutStatusIsNotReattached(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, "sleep 1")
	if m.reattachSubprocess(sess) {
		t.Fatal("no pid and no status must not re-attach")
	}

	// A recorded pid whose process is gone is not re-attached either, even
	// though its command line matched once.
	cmd := exec.Command("sleep", "0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subprocessFile(sess, subprocessPIDFile), []byte(fmt.Sprint(cmd.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}
	if m.reattachSubprocess(sess) {
		t.Fatal("a pid that is no longer running must not re-attach")
	}
}

// subprocessCommand starts the fixed helper with the rc-file path and the task
// as separate argv entries and no shell, so the daemon's later cmdline
// comparison finds the task, and the task stays a single argument.
func TestSubprocessCommandConstruction(t *testing.T) {
	m := newTestManager(t)
	task := "echo one two"
	sess := newDetachedSubprocess(t, m, task)
	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatalf("subprocessCommand: %v", err)
	}

	exe, err := subprocessHelperExe()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{exe, subprocessHelperArg, subprocessFile(sess, subprocessRCFile), "--", task}
	if len(cmd.Args) != len(want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
	if cmd.Path != exe {
		t.Errorf("Path = %q, want %q", cmd.Path, exe)
	}
	if cmd.Dir != sess.ProjectDir {
		t.Errorf("Dir = %q, want %q", cmd.Dir, sess.ProjectDir)
	}
}

// The task runs as one program, with no shell, and the helper records its exit
// status.
func TestSubprocessCommandRunsTaskWithoutShell(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, "ls /nonexistent-datawatch-dir")
	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatalf("subprocessCommand: %v", err)
	}
	_, err = cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() == 0 {
		t.Fatalf("want a non-zero exit, got %v", err)
	}
	rc, ok := readSubprocessRC(sess)
	if !ok || rc != ee.ExitCode() {
		t.Errorf("recorded rc = %d ok=%v, want %d", rc, ok, ee.ExitCode())
	}
}
