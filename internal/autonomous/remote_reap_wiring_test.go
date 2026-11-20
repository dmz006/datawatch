package autonomous

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// The server asserts the installed autonomous manager to these shapes; the
// daemon installs *API, so *API itself must satisfy them (a miss answers 503).
type remoteReapWiringReader interface {
	RemoteReapPreview(ctx context.Context) (RemoteReapSurfaceReport, []ReapBackup, error)
}
type remoteReapWiringRunner interface {
	remoteReapWiringReader
	RemoteReapRun(ctx context.Context) (RemoteReapReport, error)
	RemoteReapRestore(ctx context.Context, branch string) (RemoteReapRestoreResult, error)
}

func TestRemoteReapWiringAPISatisfiesServerInterfaces(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var installed interface{} = NewAPI(m)
	if _, ok := installed.(remoteReapWiringReader); !ok {
		t.Fatal("the daemon's API wrapper does not satisfy the remote reap read interface")
	}
	if _, ok := installed.(remoteReapWiringRunner); !ok {
		t.Fatal("the daemon's API wrapper does not satisfy the remote reap run interface")
	}
}

func TestRemoteReapWiringReportReadThroughAPI(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = repo
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	var installed interface{} = NewAPI(m)
	r, ok := installed.(remoteReapWiringReader)
	if !ok {
		t.Fatal("API does not satisfy the remote reap read interface")
	}
	if _, _, err := r.RemoteReapPreview(context.Background()); err != nil {
		t.Fatalf("report read through the wiring failed: %v", err)
	}
}

func TestRemoteReapWiringNoBaseRepoIsTypedError(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(m)
	if _, _, err := api.RemoteReapPreview(context.Background()); !errors.Is(err, ErrMergeNoBaseRepo) {
		t.Fatalf("preview: want ErrMergeNoBaseRepo, got %v", err)
	}
	if _, err := api.RemoteReapRun(context.Background()); !errors.Is(err, ErrMergeNoBaseRepo) {
		t.Fatalf("run: want ErrMergeNoBaseRepo, got %v", err)
	}
	if _, err := api.RemoteReapRestore(context.Background(), "automaton/prd-1"); !errors.Is(err, ErrMergeNoBaseRepo) {
		t.Fatalf("restore: want ErrMergeNoBaseRepo, got %v", err)
	}
}

// The live settings are read on every pass: unset defaults to disabled with
// the standard retention window, and a wired resolver answers its value.
func TestRemoteReapSettingsDefaultsAndWiring(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	enabled, days := m.remoteReapSettings()
	if enabled {
		t.Error("the default must be report-only (disabled)")
	}
	if days != config.RemoteReapRetentionDaysDefault {
		t.Errorf("default retention days = %d, want %d", days, config.RemoteReapRetentionDaysDefault)
	}

	m.SetRemoteReapSettings(func() (bool, int) { return true, 7 })
	enabled, days = m.remoteReapSettings()
	if !enabled || days != 7 {
		t.Errorf("wired settings = (%v, %d), want (true, 7)", enabled, days)
	}
}
