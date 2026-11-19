package autonomous

import (
	"context"
	"os/exec"
	"testing"
)

// The server asserts the installed autonomous manager to these shapes; the
// daemon installs *API, so *API itself must satisfy them (a miss answers 503).
type reaperWiringReader interface {
	ReapPreview(ctx context.Context) (ReapReport, []ReapBackup, error)
}
type reaperWiringRunner interface {
	reaperWiringReader
	ReapRun(ctx context.Context) (ReapReport, error)
	ReapRestore(ctx context.Context, branch string) (string, string, error)
}

func TestReaperWiringAPISatisfiesServerInterfaces(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var installed interface{} = NewAPI(m)
	if _, ok := installed.(reaperWiringReader); !ok {
		t.Fatal("the daemon's API wrapper does not satisfy the branch reap read interface")
	}
	if _, ok := installed.(reaperWiringRunner); !ok {
		t.Fatal("the daemon's API wrapper does not satisfy the branch reap run interface")
	}
}

func TestReaperWiringReportReadThroughAPI(t *testing.T) {
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
	r, ok := installed.(reaperWiringReader)
	if !ok {
		t.Fatal("API does not satisfy the reap read interface")
	}
	if _, _, err := r.ReapPreview(context.Background()); err != nil {
		t.Fatalf("report read through the wiring failed: %v", err)
	}
}

func TestReaperWiringNoBaseRepoIsTypedError(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = NewAPI(m).ReapPreview(context.Background())
	if err != ErrMergeNoBaseRepo {
		t.Fatalf("want ErrMergeNoBaseRepo, got %v", err)
	}
}
