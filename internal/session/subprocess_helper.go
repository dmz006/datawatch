package session

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// A subprocess session's task is one program with arguments, never a shell
// command line. The daemon starts a fixed helper (this binary re-executed with
// the reserved first argument below) and hands it the rc-file path and the
// task as separate argv entries. The helper splits the task with shell-style
// quoting only (subprocess_split.go), runs the program directly with
// exec.Command(program, args...), and records the exit status in subprocess.rc
// so a daemon restart can still find it. No shell is started by the daemon or
// by the helper: pipes, lists, redirects and expansions are refused, not
// interpreted. A task that needs them names the program that provides them,
// e.g. bash -c 'a && b', which is then the operator's explicit choice.
const (
	// subprocessHelperArg is the reserved first argument that selects the
	// helper. The daemon passes it as exactly: <exe> subprocessHelperArg
	// <rc-file> -- <task>.
	subprocessHelperArg = "__subprocess-run"

	// subprocessHelperUsageExit is the helper's exit status for input it
	// refuses to run. It is distinct from any shell status a task would
	// normally produce for a mistake of its own (1, 126, 127).
	subprocessHelperUsageExit = 2
)

// init lets any binary that links this package act as the helper, so the
// daemon, the CLI and every test binary re-execute themselves the same way.
// It only fires on the reserved sentinel, which no real command line uses.
func init() {
	if len(os.Args) > 1 && os.Args[1] == subprocessHelperArg {
		os.Exit(runSubprocessHelper(os.Args[2:]))
	}
}

// validateSubprocessTask rejects a task the helper must not run: empty (or
// only whitespace), anything containing a NUL byte (which no argv can carry),
// and anything splitSubprocessTask refuses (shell syntax, bad quoting).
func validateSubprocessTask(task string) error {
	if strings.TrimSpace(task) == "" {
		return errors.New("subprocess task is empty")
	}
	if strings.ContainsRune(task, 0) {
		return errors.New("subprocess task contains a NUL byte")
	}
	_, err := splitSubprocessTask(task, "")
	return err
}

// validateSubprocessRCPath checks the rc-file path the helper will write: an
// absolute, already-clean path whose final element is exactly subprocess.rc.
// Anything else (relative, "..", another file name, NUL) is refused.
func validateSubprocessRCPath(p string) error {
	switch {
	case p == "":
		return errors.New("subprocess rc path is empty")
	case strings.ContainsRune(p, 0):
		return errors.New("subprocess rc path contains a NUL byte")
	case !filepath.IsAbs(p):
		return fmt.Errorf("subprocess rc path %q is not absolute", p)
	case filepath.Clean(p) != p:
		return fmt.Errorf("subprocess rc path %q is not clean", p)
	case filepath.Base(p) != subprocessRCFile:
		return fmt.Errorf("subprocess rc path %q must end in %s", p, subprocessRCFile)
	}
	return nil
}

// subprocessHelperExe returns the program the daemon re-executes as the
// helper: its own binary. After an in-place upgrade /proc reports the old
// inode as "<path> (deleted)"; the file at <path> is the new binary, which
// speaks the same helper protocol.
func subprocessHelperExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate own executable: %w", err)
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	if !filepath.IsAbs(exe) {
		return "", fmt.Errorf("own executable path %q is not absolute", exe)
	}
	return exe, nil
}

// runSubprocessHelper is the helper's body. args are what follows the
// sentinel: exactly <rc-file> -- <task>. It returns the process exit status:
// the task's own status, or subprocessHelperUsageExit for input it refused.
func runSubprocessHelper(args []string) int {
	if len(args) != 3 || args[1] != "--" {
		fmt.Fprintf(os.Stderr, "usage: %s <rc-file> -- <task>\n", subprocessHelperArg)
		return subprocessHelperUsageExit
	}
	rcFile, task := args[0], args[2]
	if err := validateSubprocessRCPath(rcFile); err != nil {
		// Without a trustworthy rc path nothing can be recorded.
		fmt.Fprintf(os.Stderr, "[subprocess] refused: %v\n", err)
		return subprocessHelperUsageExit
	}
	if err := validateSubprocessTask(task); err != nil {
		fmt.Fprintf(os.Stderr, "[subprocess] refused: %v\n", err)
		writeSubprocessRC(rcFile, subprocessHelperUsageExit)
		return subprocessHelperUsageExit
	}
	home, _ := os.UserHomeDir()
	argv, err := splitSubprocessTask(task, home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[subprocess] refused: %v\n", err)
		writeSubprocessRC(rcFile, subprocessHelperUsageExit)
		return subprocessHelperUsageExit
	}

	// The task is a program and its arguments; there is no shell to parse it.
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	rc := subprocessExitStatus(cmd.Run())
	writeSubprocessRC(rcFile, rc)
	return rc
}

// subprocessExitStatus maps the result of running the task to a shell-style
// exit status: the task's own code, 128+signal when a signal ended it, 127
// when the program was not found and 126 when it was found but cannot run.
func subprocessExitStatus(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "[subprocess] cannot start task: %v\n", err)
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return 127
	}
	return 126
}

// writeSubprocessRC records the exit status. A failure to write is reported
// but cannot change the status the task already had.
func writeSubprocessRC(rcFile string, rc int) {
	if err := os.WriteFile(rcFile, []byte(fmt.Sprintf("%d\n", rc)), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[subprocess] cannot record exit status: %v\n", err)
	}
}
