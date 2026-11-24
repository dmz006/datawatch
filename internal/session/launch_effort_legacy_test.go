// The session effort quick/normal/thorough sets claude's --effort level at
// launch and restart, unless a claude level was named explicitly.

package session

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// launchThenRestart starts a session with opt, then restarts it after another
// launch has left its own level on the shared backend. It returns the level
// each of the two launches ran with.
func launchThenRestart(t *testing.T, backend, defaultEffort string, opt StartOptions) (atStart, atRestart string) {
	t.Helper()
	b := &effortRecorder{name: backend}
	llm.Register(b)
	mgr, _ := newTestManagerWithFake(t)
	if defaultEffort != "" {
		mgr.SetDefaultEffort(defaultEffort)
	}
	opt.Backend, opt.BackendObj, opt.LaunchFn, opt.TaskID = b.name, b, b.Launch, "t1"
	sess, err := mgr.Start(context.Background(), "task", "", t.TempDir(), &opt)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	atStart = b.last(t)

	stored, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatal("session not stored")
	}
	stored.State = StateKilled
	if err := mgr.store.Save(stored); err != nil {
		t.Fatal(err)
	}
	b.SetEffort("xhigh") // left behind by some other session
	if _, err := mgr.Restart(context.Background(), sess.FullID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	return atStart, b.last(t)
}

func assertEffort(t *testing.T, name, defaultEffort string, opt StartOptions, want string) {
	t.Helper()
	start, restart := launchThenRestart(t, name, defaultEffort, opt)
	if start != want {
		t.Errorf("launch ran with --effort %q, want %q", start, want)
	}
	if restart != want {
		t.Errorf("restart ran with --effort %q, want %q", restart, want)
	}
}

func TestSessionEffortQuickIsClaudeLow(t *testing.T) {
	assertEffort(t, "effort-quick", "", StartOptions{Effort: "quick"}, "low")
}

func TestSessionEffortNormalIsClaudeMedium(t *testing.T) {
	assertEffort(t, "effort-normal", "", StartOptions{Effort: "normal"}, "medium")
}

func TestSessionEffortThoroughIsClaudeHigh(t *testing.T) {
	assertEffort(t, "effort-thorough", "", StartOptions{Effort: "thorough"}, "high")
}

func TestConfiguredDefaultSessionEffortSetsClaudeEffort(t *testing.T) {
	assertEffort(t, "effort-default-normal", "normal", StartOptions{}, "medium")
	assertEffort(t, "effort-default-thorough", "thorough", StartOptions{}, "high")
}

func TestExplicitClaudeEffortWinsOverSessionEffort(t *testing.T) {
	assertEffort(t, "effort-explicit-max", "thorough", StartOptions{Effort: "quick", ClaudeEffort: "max"}, "max")
	assertEffort(t, "effort-explicit-low", "", StartOptions{Effort: "thorough", ClaudeEffort: "low"}, "low")
}

func TestWorkerProfileEffortWinsOverSessionEffort(t *testing.T) {
	profile := workerprofile.WorkerProfile{Name: "cheap", Effort: "low"}
	assertEffort(t, "effort-profile", "", StartOptions{Effort: "thorough", WorkerProfile: profile}, "low")
	assertEffort(t, "effort-profile-explicit", "", StartOptions{ClaudeEffort: "xhigh", WorkerProfile: profile}, "xhigh")
	// A profile value that is no claude level names nothing.
	odd := workerprofile.WorkerProfile{Name: "odd", Effort: "lots"}
	assertEffort(t, "effort-profile-odd", "", StartOptions{Effort: "quick", WorkerProfile: odd}, "low")
}
