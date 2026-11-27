// No path that types into a session writes the text into the daemon log.

package session

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// daemonOutputDuring returns what fn wrote to stdout and stderr, where the
// daemon log goes.
func daemonOutputDuring(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	out := make(chan string, 1)
	go func() { b, _ := io.ReadAll(r); out <- string(b) }()
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	fn()
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = w.Close()
	return <-out
}

func TestNoSendPathLogsTheText(t *testing.T) {
	const marker = "SECRET-ZQX-77"
	paths := map[string]func(t *testing.T, mgr *Manager, f *FakeTmux, sched *ScheduleStore, waiting, running *Session){
		"next prompt, session already waiting, timer pass": func(t *testing.T, mgr *Manager, f *FakeTmux, sched *ScheduleStore, waiting, _ *Session) {
			first, _ := sched.Add(waiting.FullID, marker+" on input", time.Time{}, "")
			_, _ = sched.Add(waiting.FullID, marker+" chained", time.Time{}, first.ID)
			mgr.processScheduledItems(t.Context())
		},
		"timed, timer pass": func(t *testing.T, mgr *Manager, f *FakeTmux, sched *ScheduleStore, _, running *Session) {
			_, _ = sched.Add(running.FullID, marker+" timed", time.Now().Add(-time.Second), "")
			mgr.processScheduledItems(t.Context())
		},
		"failed send": func(t *testing.T, mgr *Manager, f *FakeTmux, sched *ScheduleStore, waiting, _ *Session) {
			_, _ = sched.AddFull(AddOptions{SessionID: waiting.FullID, Command: marker, CronExpr: "*/5 * * * *", RunAt: time.Now().Add(-time.Second)})
			f.FailNextOp = map[string]int{"send-keys": 1, "send-keys-settle": 1}
			mgr.processScheduledItems(t.Context())
		},
		"refused by a prompt": func(t *testing.T, mgr *Manager, f *FakeTmux, sched *ScheduleStore, waiting, _ *Session) {
			f.Pane[waiting.TmuxSession] = approvalPane
			_, _ = sched.Add(waiting.FullID, marker, time.Time{}, "")
			mgr.processScheduledItems(t.Context())
		},
		"direct, tool and raw sends": func(t *testing.T, mgr *Manager, f *FakeTmux, _ *ScheduleStore, _, running *Session) {
			for _, source := range []string{"web", "schedule", "channel-task", "exit-hook", "discussion-sub"} {
				_ = mgr.SendInput(running.FullID, marker, source)
			}
			_ = mgr.SendToolInput(running.FullID, marker, "mcp")
			_ = mgr.SendRawKeys(running.FullID, marker)
			_ = mgr.SendAutomatedRawKeys(running.FullID, marker, "automaton")
		},
		"a launch with a task": func(t *testing.T, mgr *Manager, _ *FakeTmux, _ *ScheduleStore, _, _ *Session) {
			stub := &claudeModelStubBackend{}
			if _, err := mgr.Start(context.Background(), marker+" the task", "", t.TempDir(), &StartOptions{
				LaunchFn: stub.Launch, BackendObj: stub, Model: "haiku",
			}); err != nil {
				t.Fatal(err)
			}
		},
	}
	for label, run := range paths {
		t.Run(label, func(t *testing.T) {
			mgr, f := newTestManagerWithFake(t)
			sched := newSchedT(t)
			mgr.SetScheduleStore(sched)
			mgr.verbose = true // debug logging included
			var journal strings.Builder
			mgr.SetJournal(&journal)
			waiting := addNamedHolder(t, mgr, f, "aa01", "w", StateWaitingInput, idlePane)
			running := addNamedHolder(t, mgr, f, "aa02", "r", StateRunning, idlePane)
			f.NoEcho = true
			logged := daemonOutputDuring(t, func() { run(t, mgr, f, sched, waiting, running) })
			mgr.WaitBackground()
			for _, line := range strings.Split(logged+journal.String(), "\n") {
				if strings.Contains(line, marker) {
					t.Errorf("logged: %s", line)
				}
			}
		})
	}
}

// No log call anywhere in the daemon takes a schedule's command text.
func TestNoLogCallTakesAScheduleCommand(t *testing.T) {
	logCall := regexp.MustCompile(`(Printf|Println|debugf|journalf|logf|log\.\w+)\(.*\b(sc|next|claimed|sched)\.Command\b`)
	for _, root := range []string{"..", filepath.Join("..", "..", "cmd")} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			raw, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if logCall.MatchString(line) {
					t.Errorf("%s:%d: %s", p, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
