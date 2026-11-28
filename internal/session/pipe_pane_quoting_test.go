package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The log file path reaches a shell command that tmux runs for the pane's
// output. Whatever characters the path holds, tmux must write the output to
// exactly that file and run nothing else. Real tmux, on a throwaway socket.
func TestPipeOutputTakesAnyLogPathLiterally(t *testing.T) {
	realBin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not found")
	}
	root, err := os.MkdirTemp("", "dwpq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	sock := filepath.Join(root, "s")
	if len(sock) > 100 {
		t.Skipf("socket path too long for a unix socket: %s", sock)
	}
	binDir, logs, marks := filepath.Join(root, "bin"), filepath.Join(root, "logs"), filepath.Join(root, "marks")
	for _, d := range []string{binDir, logs, marks} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	wrapper := "#!/bin/sh\nunset TMUX TMUX_TMPDIR\nexec " + shellSingleQuote(realBin) + " -S " + shellSingleQuote(sock) + " -f /dev/null \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "tmux"), []byte(wrapper), 0o700); err != nil { // #nosec G306 -- test wrapper must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() { _ = exec.Command(realBin, "-S", sock, "-f", os.DevNull, "kill-server").Run() })
	// the commands run by tmux start in this folder, so a bare file name marks them
	t.Chdir(marks)

	names := map[string]string{
		"plain":         "output.log",
		"space-quote":   "it's here.log",
		"quote-command": "a'; touch ran-quote; echo '.log",
		"tmux-command":  "b#(touch ran-format).log",
		"tmux-variable": "c#{session_name}#S.log",
		"percent":       "d-100%Y-%%.log",
		"shell-chars":   "e$(touch ran-dollar)`touch ran-tick`.log",
		"tmux-style":    "f#[x]##[y].log",
	}
	tm := &TmuxManager{}
	for _, how := range []string{"pipe", "repipe"} {
		for label, name := range names {
			session := "pq-" + how + "-" + label
			logFile := filepath.Join(logs, how+"-"+name)
			if out, err := TmuxCommand("new-session", "-d", "-s", session, "-c", marks, "cat").CombinedOutput(); err != nil {
				t.Fatalf("%s: new-session: %v %s", session, err, out)
			}
			pipe := tm.PipeOutput
			if how == "repipe" {
				pipe = tm.RepipeOutput
			}
			if err := pipe(session, logFile); err != nil {
				t.Fatalf("%s: pipe: %v", session, err)
			}
			if out, err := TmuxCommand("send-keys", "-t", session, "line-"+label, "Enter").CombinedOutput(); err != nil {
				t.Fatalf("%s: send-keys: %v %s", session, err, out)
			}
			wrote := false
			for i := 0; i < 100 && !wrote; i++ {
				b, _ := os.ReadFile(logFile)
				wrote = strings.Contains(string(b), "line-"+label)
				if !wrote {
					time.Sleep(50 * time.Millisecond)
				}
			}
			if !wrote {
				t.Errorf("%s: the output did not reach the named file %q", session, logFile)
			}
			_ = TmuxCommand("pipe-pane", "-t", session).Run() // close the pipe: whatever follows the copy runs now
			_ = TmuxCommand("kill-session", "-t", session).Run()
		}
	}
	time.Sleep(500 * time.Millisecond)
	ran, err := os.ReadDir(marks)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ran {
		t.Errorf("a command taken from the log path ran and left %q", e.Name())
	}
}
