package session

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hostileSubprocessTasks are task strings built to break out of a command
// string: shell metacharacters, an embedded newline, and leading dashes that
// bash (or the helper) could mistake for an option.
var hostileSubprocessTasks = []string{
	"echo a; echo b",
	"echo a | cat & echo b",
	"echo $(echo sub) `echo tick` ${HOME}",
	"echo 'quote\" \\ back'",
	"echo line1\necho line2",
	"-x",
	"--version",
	"-c",
	"--",
	"-- ; echo after",
}

// A hostile task is always exactly one argv entry of a fixed program: it can
// not change the program, add arguments, or be split by the daemon.
func TestSubprocessCommandHostileTaskKeepsArgvShape(t *testing.T) {
	m := newTestManager(t)
	exe, err := subprocessHelperExe()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range hostileSubprocessTasks {
		sess := newDetachedSubprocess(t, m, task)
		cmd, err := subprocessCommand(sess)
		if err != nil {
			t.Fatalf("task %q: %v", task, err)
		}
		want := []string{exe, subprocessHelperArg, subprocessFile(sess, subprocessRCFile), "--", task}
		if len(cmd.Args) != len(want) {
			t.Fatalf("task %q: args = %q, want %q", task, cmd.Args, want)
		}
		for i := range want {
			if cmd.Args[i] != want[i] {
				t.Errorf("task %q: arg %d = %q, want %q", task, i, cmd.Args[i], want[i])
			}
		}
		if cmd.Path != exe {
			t.Errorf("task %q: program = %q, want %q", task, cmd.Path, exe)
		}
		// The daemon's restart check reads the task back out of cmdline.
		if !strings.Contains(strings.Join(cmd.Args, "\x00"), task) {
			t.Errorf("task %q must stay intact in the command line", task)
		}
	}
}

// A hostile rc path (the session directory's name is chosen to look like shell
// syntax) is still one argv entry, and running the helper creates nothing but
// subprocess.rc inside that directory.
func TestSubprocessCommandHostileRCPathIsInert(t *testing.T) {
	m := newTestManager(t)
	root := t.TempDir()
	dir := filepath.Join(root, "a b;touch PWNED;$(touch PWNED2)`touch PWNED3`\nPWNED4")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sess := newDetachedSubprocess(t, m, "exit 4")
	sess.LogFile = filepath.Join(dir, "output.log")
	sess.ProjectDir = dir

	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Args) != 5 || cmd.Args[2] != filepath.Join(dir, subprocessRCFile) {
		t.Fatalf("args = %q, rc path must be one entry", cmd.Args)
	}
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 4 {
		t.Fatalf("want exit 4, got %v", err)
	}
	if rc, ok := readSubprocessRC(sess); !ok || rc != 4 {
		t.Errorf("recorded rc = %d ok=%v, want 4", rc, ok)
	}
	for _, name := range []string{"PWNED", "PWNED2", "PWNED3", "PWNED4"} {
		for _, d := range []string{root, dir} {
			if _, err := os.Stat(filepath.Join(d, name)); err == nil {
				t.Errorf("%s created in %s: the path was executed", name, d)
			}
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != subprocessRCFile {
			t.Errorf("unexpected file %q in the session dir", e.Name())
		}
	}
}

// subprocessCommand refuses anything it cannot validate and returns no
// command, so there is nothing to run.
func TestSubprocessCommandFailsClosedOnInvalidInput(t *testing.T) {
	m := newTestManager(t)
	cases := map[string]func(*Session){
		"empty task":      func(s *Session) { s.Task = "" },
		"blank task":      func(s *Session) { s.Task = " \t\n" },
		"NUL in task":     func(s *Session) { s.Task = "echo a\x00; echo b" },
		"no log file":     func(s *Session) { s.LogFile = "" },
		"NUL in log file": func(s *Session) { s.LogFile = "/tmp/x\x00/output.log" },
		"NUL in project":  func(s *Session) { s.ProjectDir = "/tmp/x\x00y" },
	}
	for name, mutate := range cases {
		sess := newDetachedSubprocess(t, m, "echo ok")
		mutate(sess)
		if cmd, err := subprocessCommand(sess); err == nil || cmd != nil {
			t.Errorf("%s: want error and no command, got cmd=%v err=%v", name, cmd, err)
		}
	}
}

// A relative log path still yields an absolute rc path, because the helper runs
// in the project directory and would otherwise write somewhere else.
func TestSubprocessCommandRCPathIsAbsolute(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, "echo ok")
	sess.LogFile = filepath.Join("relative", "dir", "output.log")
	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	rc := cmd.Args[2]
	if !filepath.IsAbs(rc) || filepath.Base(rc) != subprocessRCFile {
		t.Errorf("rc path = %q, want absolute .../%s", rc, subprocessRCFile)
	}
}

func TestSubprocessValidateRCPath(t *testing.T) {
	good := filepath.Join(t.TempDir(), subprocessRCFile)
	if err := validateSubprocessRCPath(good); err != nil {
		t.Errorf("valid path rejected: %v", err)
	}
	for _, bad := range []string{
		"",
		"subprocess.rc",
		"./subprocess.rc",
		"../subprocess.rc",
		"/tmp/x/../subprocess.rc",
		"/tmp/x/subprocess.rc/",
		"/tmp/x/other.rc",
		"/tmp/x/subprocess.rc.bak",
		"/tmp/x\x00/subprocess.rc",
	} {
		if err := validateSubprocessRCPath(bad); err == nil {
			t.Errorf("path %q must be rejected", bad)
		}
	}
}

func TestSubprocessValidateTask(t *testing.T) {
	for _, ok := range []string{"x", "echo a; echo b", "-x", "a\nb"} {
		if err := validateSubprocessTask(ok); err != nil {
			t.Errorf("task %q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " ", "\n\t", "a\x00b", "\x00"} {
		if err := validateSubprocessTask(bad); err == nil {
			t.Errorf("task %q must be rejected", bad)
		}
	}
}

// runHelper re-executes the test binary as the helper with exactly args.
func runHelper(t *testing.T, args ...string) (int, string) {
	t.Helper()
	exe, err := subprocessHelperExe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(exe, append([]string{subprocessHelperArg}, args...)...).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("helper did not run: %v", err)
	}
	return ee.ExitCode(), string(out)
}

// The helper fails closed: input it cannot validate is refused with the usage
// status and the task never runs. The task here would create a marker file.
func TestSubprocessHelperFailsClosedOnInvalidInput(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	rc := filepath.Join(dir, subprocessRCFile)
	touch := "touch " + marker

	cases := map[string][]string{
		"no arguments":        {},
		"one argument":        {rc},
		"missing terminator":  {rc, touch, "extra"},
		"wrong terminator":    {rc, "-c", touch},
		"extra argument":      {rc, "--", touch, "extra"},
		"relative rc path":    {"subprocess.rc", "--", touch},
		"dotdot rc path":      {dir + "/sub/../" + subprocessRCFile, "--", touch},
		"other file name":     {filepath.Join(dir, "other.rc"), "--", touch},
		"empty rc path":       {"", "--", touch},
		"rc path as the task": {touch, "--", rc},
	}
	for name, args := range cases {
		code, out := runHelper(t, args...)
		if code != subprocessHelperUsageExit {
			t.Errorf("%s: exit %d, want %d (out %q)", name, code, subprocessHelperUsageExit, out)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s: the task ran", name)
		}
		if _, err := os.Stat(filepath.Join(dir, "other.rc")); err == nil {
			t.Fatalf("%s: wrote a file other than %s", name, subprocessRCFile)
		}
	}
	if _, err := os.Stat(rc); err == nil {
		t.Error("an invalid rc path must not produce an rc file")
	}

	// A valid rc path with an unusable task records the refusal, so a
	// restarted daemon sees a failed task, not a missing one.
	for _, task := range []string{"", "   ", "\n"} {
		_ = os.Remove(rc)
		code, _ := runHelper(t, rc, "--", task)
		if code != subprocessHelperUsageExit {
			t.Errorf("task %q: exit %d, want %d", task, code, subprocessHelperUsageExit)
		}
		b, err := os.ReadFile(rc)
		if err != nil || strings.TrimSpace(string(b)) != "2" {
			t.Errorf("task %q: rc file = %q err=%v, want 2", task, b, err)
		}
	}
}

// The shell the helper starts receives the task as its command string and
// nothing else: no extra positional parameters, and a task that looks like an
// option is run as a command, not parsed as an option.
func TestSubprocessHelperPassesTaskAsSingleCommandString(t *testing.T) {
	m := newTestManager(t)

	sess := newDetachedSubprocess(t, m, `echo "args=$#"`)
	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "args=0\n" {
		t.Errorf("output = %q, want args=0", out)
	}

	// Without the "--" terminator bash would treat --version as its own
	// option, print its version and exit 0. As a command it is not found.
	for _, task := range []string{"--version", "-x", "-c", "--", "-n"} {
		sess := newDetachedSubprocess(t, m, task)
		cmd, err := subprocessCommand(sess)
		if err != nil {
			t.Fatal(err)
		}
		out, err := cmd.CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 127 {
			t.Errorf("task %q: want exit 127 (command not found), got %v out %q", task, err, out)
		}
		if strings.Contains(string(out), "GNU bash") {
			t.Errorf("task %q was parsed as a bash option: %q", task, out)
		}
		if rc, ok := readSubprocessRC(sess); !ok || rc != 127 {
			t.Errorf("task %q: recorded rc = %d ok=%v, want 127", task, rc, ok)
		}
	}

	// An embedded newline is part of the one command string.
	sess = newDetachedSubprocess(t, m, "echo line1\necho line2")
	cmd, err = subprocessCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := cmd.Output(); err != nil || string(out) != "line1\nline2\n" {
		t.Errorf("multi-line task: out %q err %v", out, err)
	}
}

// A task ended by a signal is recorded as 128+signal, as a shell reports it.
func TestSubprocessHelperRecordsSignalStatus(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, "kill -TERM $$")
	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 143 {
		t.Fatalf("want exit 143, got %v", err)
	}
	if rc, ok := readSubprocessRC(sess); !ok || rc != 143 {
		t.Errorf("recorded rc = %d ok=%v, want 143", rc, ok)
	}
}

// runSubprocess with an invalid task ends the session failed without starting
// anything, and says why in the log.
func TestSubprocessRunRefusesInvalidTask(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, "")
	m.runSubprocess(sess)

	got := waitForState(t, m, sess.FullID, StateFailed)
	if got.State != StateFailed {
		t.Fatalf("state = %s", got.State)
	}
	b, _ := os.ReadFile(sess.LogFile)
	if !strings.Contains(string(b), "refused") {
		t.Errorf("log must say the task was refused, got %q", b)
	}
	if _, err := os.Stat(subprocessFile(sess, subprocessPIDFile)); err == nil {
		t.Error("no process was started, so no pid file may exist")
	}
}

// The daemon itself never starts a shell: the program it launches for a
// subprocess session is the helper (its own binary), and no "-c" command string
// is in its argument list. This is the structural property the command-injection
// fix rests on; the shell the helper starts below it is a separate, documented
// matter (see the plan doc's open decision).
func TestSubprocessCommandDaemonLaunchIsNotAShell(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, "echo ok")
	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	switch filepath.Base(cmd.Path) {
	case "bash", "sh", "dash", "zsh", "ksh", "fish":
		t.Errorf("daemon launches a shell: %q", cmd.Path)
	}
	for _, a := range cmd.Args[1:] {
		if a == "-c" {
			t.Errorf("daemon argument list carries -c: %q", cmd.Args)
		}
	}
	if len(cmd.Args) < 2 || cmd.Args[1] != subprocessHelperArg {
		t.Errorf("first argument must select the helper, args = %q", cmd.Args)
	}
}
