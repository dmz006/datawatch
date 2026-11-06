package deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }
func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.t = c.t.Add(d)
	return nil
}

type fakeDrain struct{ n int }

func (d *fakeDrain) RunningFinalGate(context.Context) (int, error) { return d.n, nil }

type fakeRestart struct{}

func (fakeRestart) RunningSessionIDs(context.Context) ([]string, error) { return []string{"s1"}, nil }
func (fakeRestart) HeldLeaseCount(context.Context) (int, error)         { return 2, nil }

type fakeCmd struct {
	calls  []string
	codes  map[string]int
	onCall func(line string)
}

func (f *fakeCmd) Run(_ context.Context, _, line string) (CmdResult, error) {
	f.calls = append(f.calls, line)
	if f.onCall != nil {
		f.onCall(line)
	}
	return CmdResult{Out: "out", Code: f.codes[line]}, nil
}

type memStore struct {
	s     State
	ok    bool
	saves int
}

func (m *memStore) Save(s State) error         { m.s, m.ok = s, true; m.saves++; return nil }
func (m *memStore) Load() (State, bool, error) { return m.s, m.ok, nil }

func setup(t *testing.T, cfg config.DeployConfig) (*Runner, *fakeCmd, *fakeDrain, *memStore) {
	t.Helper()
	data := t.TempDir()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(bin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if cfg.InstallCommand != "" {
		cfg.BinaryPath = bin
	}
	cmd := &fakeCmd{codes: map[string]int{}}
	dr := &fakeDrain{}
	st := &memStore{}
	return &Runner{Config: cfg, Dir: t.TempDir(), DataDir: data, Cmd: cmd, Drain: dr,
		Clock: &fakeClock{t: time.Unix(1000, 0)}, Store: st, Restart: fakeRestart{}}, cmd, dr, st
}

func TestDeployNoSettingsSkipped(t *testing.T) {
	r, cmd, _, st := setup(t, config.DeployConfig{})
	res, err := r.Run(context.Background())
	if err != nil || res.Status != StatusSkipped || res.Reason == "" {
		t.Fatalf("got %+v %v", res, err)
	}
	if len(cmd.calls) != 0 || st.saves != 0 {
		t.Fatal("something changed")
	}
}

func TestDeployBuildFailureInstallsNothing(t *testing.T) {
	r, cmd, _, st := setup(t, config.DeployConfig{BuildCommand: "b", InstallCommand: "i", RestartCommand: "r"})
	cmd.codes["b"] = 1
	res, _ := r.Run(context.Background())
	if res.Status != StatusFailed {
		t.Fatalf("status %s", res.Status)
	}
	if len(cmd.calls) != 1 || st.saves != 0 {
		t.Fatalf("calls %v saves %d", cmd.calls, st.saves)
	}
}

func TestDeployDrainTimeoutDefersThenRetryRuns(t *testing.T) {
	r, cmd, dr, st := setup(t, config.DeployConfig{BuildCommand: "b", RestartCommand: "r", DrainTimeout: "20s"})
	dr.n = 1
	res, err := r.Run(context.Background())
	if err != nil || res.Status != StatusDeferred {
		t.Fatalf("got %+v %v", res, err)
	}
	if st.s.Phase != PhaseDeferred || st.s.Reason == "" || st.s.RequestedAt.IsZero() {
		t.Fatalf("state %+v", st.s)
	}
	for _, c := range cmd.calls {
		if c == "r" {
			t.Fatal("restart ran while deferred")
		}
	}
	dr.n = 0
	res, err = r.Retry(context.Background())
	if err != nil || res.Status != StatusRestarting {
		t.Fatalf("retry %+v %v", res, err)
	}
	if st.s.PreRestart.LeaseCount != 2 || len(st.s.PreRestart.SessionIDs) != 1 {
		t.Fatalf("pre-restart %+v", st.s.PreRestart)
	}
}

func TestDeploySnapshotExistsBeforeInstall(t *testing.T) {
	r, cmd, _, _ := setup(t, config.DeployConfig{BuildCommand: "b", InstallCommand: "i"})
	seen := false
	cmd.onCall = func(line string) {
		if line == "i" {
			b, err := os.ReadFile(r.snapshotPath())
			seen = err == nil && string(b) == "old-binary"
			fi, _ := os.Stat(r.snapshotPath())
			if fi == nil || fi.Mode().Perm() != 0o755 {
				seen = false
			}
		}
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("snapshot missing or wrong at install time")
	}
}

func TestDeployMissingBinaryStopsBeforeChange(t *testing.T) {
	r, cmd, _, st := setup(t, config.DeployConfig{BuildCommand: "b", InstallCommand: "i"})
	r.Config.BinaryPath = filepath.Join(t.TempDir(), "absent")
	res, _ := r.Run(context.Background())
	if res.Status != StatusFailed || len(cmd.calls) != 0 || st.saves != 0 {
		t.Fatalf("%+v %v", res, cmd.calls)
	}
}

func TestDeployStatePersistedBeforeInstall(t *testing.T) {
	r, cmd, _, st := setup(t, config.DeployConfig{InstallCommand: "i", RestartCommand: "r"})
	ok := false
	cmd.onCall = func(line string) {
		if line == "i" {
			ok = st.ok && st.s.Phase == PhaseRestarting && st.s.SnapshotPath != "" && st.s.SnapshotHash != ""
		}
	}
	res, _ := r.Run(context.Background())
	if !ok || res.Status != StatusRestarting {
		t.Fatalf("ok=%v res=%+v", ok, res)
	}
}

func TestDeployInstallFailureKeepsSnapshotPath(t *testing.T) {
	r, cmd, _, st := setup(t, config.DeployConfig{InstallCommand: "i", RestartCommand: "r"})
	cmd.codes["i"] = 3
	res, _ := r.Run(context.Background())
	if res.Status != StatusFailed || st.s.SnapshotPath == "" || st.s.Phase != PhaseInstallFailed {
		t.Fatalf("%+v %+v", res, st.s)
	}
	for _, c := range cmd.calls {
		if c == "r" {
			t.Fatal("restart ran after failed install")
		}
	}
}

func TestDeployFileStoreAtomicRoundTrip(t *testing.T) {
	fs := FileStore{Path: filepath.Join(t.TempDir(), "d", "state.json")}
	if err := fs.Save(State{Phase: PhaseRestarting, SnapshotPath: "p"}); err != nil {
		t.Fatal(err)
	}
	s, ok, err := fs.Load()
	if err != nil || !ok || s.Version != StateVersion || s.SnapshotPath != "p" {
		t.Fatalf("%+v %v %v", s, ok, err)
	}
	if got := redact("token=abc123"); got == "token=abc123" {
		t.Fatal("not redacted")
	}
}
