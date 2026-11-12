// The exclude-ancestor-CLAUDE.md option is turned on for autonomous (PRD)
// workers — whether they run in a PRD worktree or the main checkout — and
// explicitly turned off for interactive sessions, because the claude-code
// backend is a shared singleton and would otherwise carry a stale value.

package session

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
)

// excludeRecorder is a backend whose SetExcludeAncestorClaudeMd history is
// recorded together with the value in force at each Launch.
type excludeRecorder struct {
	name     string
	mu       sync.Mutex
	current  bool
	set      []bool // every value passed to the setter
	atLaunch []bool // value in force when Launch/LaunchResume ran
}

func (b *excludeRecorder) Name() string                   { return b.name }
func (b *excludeRecorder) SupportsInteractiveInput() bool { return true }
func (b *excludeRecorder) Version() string                { return "test-stub" }
func (b *excludeRecorder) SetExcludeAncestorClaudeMd(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = on
	b.set = append(b.set, on)
}
func (b *excludeRecorder) Launch(_ context.Context, _, _, _, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.atLaunch = append(b.atLaunch, b.current)
	return nil
}
func (b *excludeRecorder) LaunchResume(_ context.Context, _, _, _, _, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.atLaunch = append(b.atLaunch, b.current)
	return nil
}

func (b *excludeRecorder) lastLaunch(t *testing.T) bool {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.atLaunch) == 0 {
		t.Fatal("backend was never launched")
	}
	return b.atLaunch[len(b.atLaunch)-1]
}

func startWith(t *testing.T, b *excludeRecorder, dir, prdID string) {
	t.Helper()
	mgr, _ := newTestManagerWithFake(t)
	if _, err := mgr.Start(context.Background(), "task", "", dir, &StartOptions{
		Backend: b.name, BackendObj: b, LaunchFn: b.Launch, PRDID: prdID, TaskID: "t1",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestStart_ExcludeAncestorClaudeMd_OnlyForPRDWorkers(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, "prd-worktrees", "9a400001")
	checkout := filepath.Join(root, "workspace", "datawatch")
	for _, d := range []string{worktree, checkout} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name  string
		dir   string
		prdID string
		want  bool
	}{
		{"prd worktree", worktree, "prd1", true},
		{"prd main checkout", checkout, "prd1", true},
		{"interactive", checkout, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &excludeRecorder{name: "excl-start-" + c.prdID + filepath.Base(c.dir)}
			startWith(t, b, c.dir, c.prdID)
			if got := b.lastLaunch(t); got != c.want {
				t.Errorf("option at launch = %v, want %v", got, c.want)
			}
		})
	}
}

// A singleton backend left ON by a PRD worker must be reset by the next
// interactive launch.
func TestStart_ExcludeAncestorClaudeMd_InteractiveResetsStaleOn(t *testing.T) {
	b := &excludeRecorder{name: "excl-singleton"}
	dir := t.TempDir()
	startWith(t, b, dir, "prd1")
	if !b.lastLaunch(t) {
		t.Fatal("PRD launch should have turned the option on")
	}
	startWith(t, b, dir, "")
	if b.lastLaunch(t) {
		t.Error("interactive launch after a PRD launch still has the option on")
	}
}

func TestRestart_ExcludeAncestorClaudeMd_FollowsPRDID(t *testing.T) {
	for _, c := range []struct {
		prdID string
		want  bool
	}{{"prd1", true}, {"", false}} {
		b := &excludeRecorder{name: "excl-restart-" + c.prdID}
		llm.Register(b)
		mgr, _ := newTestManagerWithFake(t)
		b.mu.Lock()
		b.current = !c.want // stale value from a previous launch
		b.mu.Unlock()
		sess := &Session{
			ID: "r001", FullID: "testhost-r001", Hostname: "testhost", TmuxSession: "cs-r001",
			BackendFamily: b.name, State: StateKilled, Task: "task", PRDID: c.prdID,
			ProjectDir: t.TempDir(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
			LogFile: t.TempDir() + "/r001.log",
		}
		if err := mgr.store.Save(sess); err != nil {
			t.Fatal(err)
		}
		if _, err := mgr.Restart(context.Background(), sess.FullID); err != nil {
			t.Fatalf("Restart: %v", err)
		}
		if got := b.lastLaunch(t); got != c.want {
			t.Errorf("prdID=%q: option at relaunch = %v, want %v", c.prdID, got, c.want)
		}
	}
}
