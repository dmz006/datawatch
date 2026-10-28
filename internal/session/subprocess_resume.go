package session

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Subprocess sessions (BL462 F4) run the task as a shell command outside tmux,
// through the fixed helper in subprocess_helper.go. When the daemon restarts,
// the child keeps running and keeps writing to its log file, so the boot path
// must not declare it failed while it is still alive. Two small files in the
// session directory let a new daemon generation find it:
//
//   - subprocess.pid: the pid of the helper process, written at start.
//   - subprocess.rc:  the task's exit status, written by the helper when the
//     task ends. It survives a daemon restart, so an exit can still be
//     recorded after the goroutine that would have seen it is gone.

const (
	subprocessPIDFile = "subprocess.pid"
	subprocessRCFile  = "subprocess.rc"
	// subprocessWatchInterval is how often a re-attached waiter polls the pid.
	subprocessWatchInterval = 2 * time.Second
)

func subprocessFile(sess *Session, name string) string {
	return filepath.Join(filepath.Dir(sess.LogFile), name)
}

// subprocessCommand builds the command for a subprocess session: this binary
// re-executed as the helper, with the rc-file path and the task as separate
// argv entries and no shell in between. Every value is validated first; on any
// invalid input no command is returned, so nothing runs.
//
// The helper's command line is <exe> __subprocess-run <rc-file> -- <task>, so
// subprocessAlive can still find the task in /proc/<pid>/cmdline.
func subprocessCommand(sess *Session) (*exec.Cmd, error) {
	if err := validateSubprocessTask(sess.Task); err != nil {
		return nil, err
	}
	if strings.ContainsRune(sess.ProjectDir, 0) {
		return nil, errors.New("subprocess project dir contains a NUL byte")
	}
	rcFile, err := subprocessRCPath(sess)
	if err != nil {
		return nil, err
	}
	exe, err := subprocessHelperExe()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, subprocessHelperArg, rcFile, "--", sess.Task)
	cmd.Dir = sess.ProjectDir
	return cmd, nil
}

// subprocessRCPath returns the absolute rc-file path for sess and checks that
// it lies directly inside the session directory (the log file's directory).
func subprocessRCPath(sess *Session) (string, error) {
	if sess.LogFile == "" || strings.ContainsRune(sess.LogFile, 0) {
		return "", errors.New("subprocess session has no valid log file")
	}
	// Absolute, because the helper runs in the project dir, not the daemon's.
	dir, err := filepath.Abs(filepath.Dir(sess.LogFile))
	if err != nil {
		return "", fmt.Errorf("resolve session dir: %w", err)
	}
	rcFile := filepath.Join(dir, subprocessRCFile)
	if rel, err := filepath.Rel(dir, rcFile); err != nil || rel != subprocessRCFile {
		return "", fmt.Errorf("subprocess rc path %q is outside the session dir", rcFile)
	}
	if err := validateSubprocessRCPath(rcFile); err != nil {
		return "", err
	}
	return rcFile, nil
}

// subprocessAlive reports whether pid is still running the session's task.
// The command line is compared, not just the pid, so a recycled pid is not
// mistaken for the task. Without /proc this reports false, and the caller
// falls back to the boot-time failure path.
func subprocessAlive(pid int, task string) bool {
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	return bytes.Contains(cmdline, []byte(task))
}

// readSubprocessRC returns the recorded exit status, if the task has ended.
func readSubprocessRC(sess *Session) (int, bool) {
	b, err := os.ReadFile(subprocessFile(sess, subprocessRCFile))
	if err != nil {
		return 0, false
	}
	rc, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, false
	}
	return rc, true
}

// readSubprocessPID returns the recorded wrapper pid.
func readSubprocessPID(sess *Session) (int, bool) {
	b, err := os.ReadFile(subprocessFile(sess, subprocessPIDFile))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// reattachSubprocess is the boot-time counterpart of runSubprocess. It
// returns true when the session was handled: either its task is still
// running and a waiter now watches it, or it had already ended and was
// finished from the recorded exit status. It returns false when the task
// is gone without a recorded status, and the caller marks it failed.
func (m *Manager) reattachSubprocess(sess *Session) bool {
	if rc, ok := readSubprocessRC(sess); ok {
		m.finishDetachedSubprocess(sess, rc)
		return true
	}
	pid, ok := readSubprocessPID(sess)
	if !ok || !subprocessAlive(pid, sess.Task) {
		return false
	}
	go m.watchDetachedSubprocess(sess, pid)
	return true
}

// watchDetachedSubprocess waits for a re-attached task to end, then finishes
// the session from its recorded exit status.
func (m *Manager) watchDetachedSubprocess(sess *Session, pid int) {
	for subprocessAlive(pid, sess.Task) {
		time.Sleep(subprocessWatchInterval)
	}
	if rc, ok := readSubprocessRC(sess); ok {
		m.finishDetachedSubprocess(sess, rc)
		return
	}
	// The task is gone but no status was written. Its exit code cannot be
	// recovered, so record that and fail the session instead of guessing.
	m.finishDetachedSubprocessUnknown(sess)
}

func (m *Manager) finishDetachedSubprocess(sess *Session, rc int) {
	f, err := os.OpenFile(sess.LogFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err == nil {
		if rc == 0 {
			_, _ = fmt.Fprintf(f, "\n[subprocess] completed (exit 0) after daemon restart\n")
		} else {
			_, _ = fmt.Fprintf(f, "\n[subprocess] failed (exit %d) after daemon restart\n", rc)
		}
		_ = f.Close()
	}
	_ = os.Remove(subprocessFile(sess, subprocessPIDFile))
	var runErr error
	if rc != 0 {
		runErr = fmt.Errorf("exit status %d", rc)
	}
	m.subprocessFinish(sess, runErr)
	m.releaseCommitGuard(sess.FullID, sess.ProjectDir)
}

func (m *Manager) finishDetachedSubprocessUnknown(sess *Session) {
	f, err := os.OpenFile(sess.LogFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err == nil {
		_, _ = fmt.Fprintf(f, "\n[subprocess] ended after daemon restart; exit status unknown\n")
		_ = f.Close()
	}
	_ = os.Remove(subprocessFile(sess, subprocessPIDFile))
	m.subprocessFinish(sess, fmt.Errorf("exit status unknown after daemon restart"))
	m.releaseCommitGuard(sess.FullID, sess.ProjectDir)
}
