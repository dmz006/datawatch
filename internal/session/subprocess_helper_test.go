package session

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shellSyntaxSubprocessTasks need a shell to mean what they say. A subprocess
// task is one program with arguments, so each is refused and nothing runs.
// MARK stands for a file the task would create if any of it were interpreted.
var shellSyntaxSubprocessTasks = []string{
	"echo a; touch MARK",
	"echo a | cat & touch MARK",
	"echo $(touch MARK)",
	"echo `touch MARK`",
	"echo ${HOME}",
	"echo $HOME",
	"echo \"$HOME\"",
	"echo \"`touch MARK`\"",
	"echo a && touch MARK",
	"echo a > MARK",
	"cat < MARK",
	"(touch MARK)",
	"echo line1\ntouch MARK",
}

// optionLikeSubprocessTasks start with a dash. They are valid program names
// (that simply do not exist), so they must reach exec as the program and never
// be parsed as an option by anything in between.
var optionLikeSubprocessTasks = []string{"-x", "--version", "-c", "--", "-n"}

// Shell syntax is refused by the daemon before anything is launched, and by the
// helper if it is ever handed such a task directly: exit 2, rc file records 2,
// and the would-be side effect never happens.
func TestSubprocessShellSyntaxIsRefused(t *testing.T) {
	m := newTestManager(t)
	for _, raw := range shellSyntaxSubprocessTasks {
		dir := t.TempDir()
		marker := filepath.Join(dir, "MARK")
		task := strings.ReplaceAll(raw, "MARK", marker)

		sess := newDetachedSubprocess(t, m, task)
		if cmd, err := subprocessCommand(sess); err == nil || cmd != nil {
			t.Errorf("task %q: want refusal and no command, got cmd=%v err=%v", raw, cmd, err)
		}

		rc := filepath.Join(dir, subprocessRCFile)
		code, out := runHelper(t, rc, "--", task)
		if code != subprocessHelperUsageExit {
			t.Errorf("task %q: helper exit %d, want %d (out %q)", raw, code, subprocessHelperUsageExit, out)
		}
		if !strings.Contains(out, "shell syntax") && !strings.Contains(out, "backslash") {
			t.Errorf("task %q: refusal must say why, got %q", raw, out)
		}
		if b, err := os.ReadFile(rc); err != nil || strings.TrimSpace(string(b)) != "2" {
			t.Errorf("task %q: rc file = %q err=%v, want 2", raw, b, err)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Errorf("task %q: the task ran", raw)
		}
	}
}

// An option-like task is exactly one argv entry of the fixed helper and is run
// as a program name, not as an option.
func TestSubprocessCommandOptionLikeTaskKeepsArgvShape(t *testing.T) {
	m := newTestManager(t)
	exe, err := subprocessHelperExe()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range optionLikeSubprocessTasks {
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
	sess := newDetachedSubprocess(t, m, "false")
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
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got %v", err)
	}
	if rc, ok := readSubprocessRC(sess); !ok || rc != 1 {
		t.Errorf("recorded rc = %d ok=%v, want 1", rc, ok)
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
		"NUL in task":     func(s *Session) { s.Task = "echo a\x00 b" },
		"shell syntax":    func(s *Session) { s.Task = "echo a; echo b" },
		"open quote":      func(s *Session) { s.Task = "echo 'a" },
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
	for _, ok := range []string{"x", "echo 'a; b'", "-x", "echo \"a|b\" c\\;d", "bash -c 'a && b'"} {
		if err := validateSubprocessTask(ok); err != nil {
			t.Errorf("task %q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " ", "\n\t", "a\x00b", "\x00", "a\nb", "a; b", "a | b", "echo 'x", "echo \"x", "echo x\\"} {
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

// The helper runs the task as a program with literal arguments. Quoted shell
// syntax is data, not syntax: nothing is expanded, split on operators or
// globbed.
func TestSubprocessHelperRunsProgramWithLiteralArguments(t *testing.T) {
	m := newTestManager(t)
	run := func(task string) (string, error) {
		sess := newDetachedSubprocess(t, m, task)
		cmd, err := subprocessCommand(sess)
		if err != nil {
			t.Fatalf("task %q: %v", task, err)
		}
		out, err := cmd.Output()
		return string(out), err
	}

	for task, want := range map[string]string{
		`echo 'a; b | c && $HOME *'`:   "a; b | c && $HOME *\n",
		`echo "a b" 'c d' e\ f ""`:     "a b c d e f \n",
		`echo "q\"uote" "back\\slash"`: "q\"uote back\\slash\n",
		`echo 'it'"'"'s'`:              "it's\n",
	} {
		out, err := run(task)
		if err != nil || out != want {
			t.Errorf("task %q: out %q err %v, want %q", task, out, err, want)
		}
	}

	// A program that does not exist is 127; one that exists but is not
	// executable is 126. Both are recorded, as a shell would report them.
	for _, task := range optionLikeSubprocessTasks {
		sess := newDetachedSubprocess(t, m, task)
		cmd, err := subprocessCommand(sess)
		if err != nil {
			t.Fatal(err)
		}
		out, err := cmd.CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 127 {
			t.Errorf("task %q: want exit 127 (not found), got %v out %q", task, err, out)
		}
		if rc, ok := readSubprocessRC(sess); !ok || rc != 127 {
			t.Errorf("task %q: recorded rc = %d ok=%v, want 127", task, rc, ok)
		}
	}
	sess := newDetachedSubprocess(t, m, "./plain-file")
	if err := os.WriteFile(filepath.Join(sess.ProjectDir, "plain-file"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 126 {
		t.Errorf("non-executable file: want exit 126, got %v", err)
	}
	if rc, ok := readSubprocessRC(sess); !ok || rc != 126 {
		t.Errorf("non-executable file: recorded rc = %d ok=%v, want 126", rc, ok)
	}
}

// A leading ~ is the one expansion a task gets, so paths like
// ~/workspace/imap.yaml keep working; a ~ elsewhere in a word stays literal.
func TestSplitSubprocessTaskExpandsOnlyLeadingTilde(t *testing.T) {
	cases := map[string][]string{
		`run ~/cfg.yaml`:   {"run", "/home/u/cfg.yaml"},
		`run ~`:            {"run", "/home/u"},
		`run a~b --x=~/y`:  {"run", "a~b", "--x=~/y"},
		`run '~/cfg.yaml'`: {"run", "~/cfg.yaml"},
		`run ~other/x`:     {"run", "~other/x"},
	}
	for task, want := range cases {
		got, err := splitSubprocessTask(task, "/home/u")
		if err != nil || strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("task %q: got %q err %v, want %q", task, got, err, want)
		}
	}
}

// The scripts that start subprocess sessions through the API quote a whole
// script with Python's shlex.quote and name bash as the program. That form
// (including the '"'"' sequence for an embedded single quote) must split into
// exactly [bash -c <the script>].
func TestSplitSubprocessTaskAcceptsShlexQuotedScript(t *testing.T) {
	script := `sleep 1; S=${X:-}; if [ -n "$S" ]; then curl -d '{"a":1}'; fi; exit 0`
	task := `bash -c 'sleep 1; S=${X:-}; if [ -n "$S" ]; then curl -d '"'"'{"a":1}'"'"'; fi; exit 0'`
	got, err := splitSubprocessTask(task, "/home/u")
	if err != nil || len(got) != 3 || got[0] != "bash" || got[1] != "-c" || got[2] != script {
		t.Errorf("got %q err %v, want [bash -c %q]", got, err, script)
	}
}

// An operator who wants a pipe or a list names the shell as the program. That
// is an explicit program choice, not interpretation by the launcher.
func TestSubprocessExplicitShellProgramStillWorks(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, `bash -c 'echo one; echo two && exit 3'`)
	cmd, err := subprocessCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 || string(out) != "one\ntwo\n" {
		t.Fatalf("out %q err %v, want one/two and exit 3", out, err)
	}
	if rc, ok := readSubprocessRC(sess); !ok || rc != 3 {
		t.Errorf("recorded rc = %d ok=%v, want 3", rc, ok)
	}
}

// A task ended by a signal is recorded as 128+signal, as a shell reports it.
func TestSubprocessHelperRecordsSignalStatus(t *testing.T) {
	m := newTestManager(t)
	sess := newDetachedSubprocess(t, m, `bash -c 'kill -TERM $$'`)
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
// is in its argument list.
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

// The helper never starts a shell either. This reads the helper's source and
// fails if a shell program or a "-c" command-string flag appears in any
// non-comment line of the launch path, so the no-shell property cannot
// regress without a test change.
func TestSubprocessLaunchPathNamesNoShell(t *testing.T) {
	for _, file := range []string{"subprocess_helper.go", "subprocess_resume.go", "subprocess_split.go"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "//") {
				continue
			}
			for _, banned := range []string{`"bash"`, `"sh"`, `"dash"`, `"zsh"`, `"-c"`, `"/bin/sh"`, `"/bin/bash"`} {
				if strings.Contains(code, banned) {
					t.Errorf("%s:%d names a shell or -c: %s", file, i+1, code)
				}
			}
		}
	}
}
