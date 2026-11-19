package autonomous

import (
	"context"
	"errors"
	"testing"
)

// TestAPIHousekeepingForward checks that the *API wrapper exposes the seven
// housekeeping Manager methods and forwards to them, so the server's
// unlandedWorkOps assertion (internal/server/unlanded_work_api.go) succeeds
// on the live daemon instead of answering 503.
func TestAPIHousekeepingForward(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	api := NewAPI(m)
	ctx := context.Background()

	// The full interface surface must compile against *API.
	var ops interface {
		UnlandedWork(ctx context.Context) ([]UnlandedBranch, error)
		KeepUnlanded(ctx context.Context, branch, note string) error
		ArchiveUnlanded(ctx context.Context, branch string) (string, error)
		DeleteUnlandedWorktree(ctx context.Context, branch string, confirm bool) (DeleteWorktreeResult, error)
		UncommittedWorkDiff(ctx context.Context, worktree string) (string, error)
		CommitUncommittedWork(ctx context.Context, worktree, message string) (string, error)
		FixPermissions(paths []string) (int, error)
	} = api

	// Behavior: with no base repository configured the off-state read is an
	// empty list and every action reports that nothing is configured, exactly
	// as the Manager would (this is also the delegation result, since no
	// branch or worktree can be listed).
	got, err := ops.UnlandedWork(ctx)
	if err != nil {
		t.Fatalf("UnlandedWork: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("UnlandedWork: want empty list with no base repo, got %v", got)
	}

	if err := ops.KeepUnlanded(ctx, "automaton/x", "note"); !errors.Is(err, ErrNoBaseRepo) {
		t.Fatalf("KeepUnlanded: want ErrNoBaseRepo, got %v", err)
	}
	if out, err := ops.ArchiveUnlanded(ctx, "automaton/x"); out != "" || !errors.Is(err, ErrNoBaseRepo) {
		t.Fatalf("ArchiveUnlanded: want ErrNoBaseRepo, got %q, %v", out, err)
	}
	if res, err := ops.DeleteUnlandedWorktree(ctx, "automaton/x", true); !errors.Is(err, ErrNoBaseRepo) {
		t.Fatalf("DeleteUnlandedWorktree: want ErrNoBaseRepo, got %+v, %v", res, err)
	}
	if out, err := ops.UncommittedWorkDiff(ctx, "no-such-worktree"); out != "" && err == nil {
		t.Fatalf("UncommittedWorkDiff: want empty diff only with an error or empty, got %q", out)
	}
	if out, err := ops.CommitUncommittedWork(ctx, "no-such-worktree", "msg"); out != "" && err == nil {
		t.Fatalf("CommitUncommittedWork: want empty sha only with an error or empty, got %q", out)
	}
	if n, err := ops.FixPermissions([]string{}); n != 0 && err != nil {
		t.Fatalf("FixPermissions: got n=%d err=%v", n, err)
	}

	// Nil-Manager guards: the wrapper must not panic when constructed
	// without a manager.
	var nilAPI *API
	if _, err := nilAPI.UnlandedWork(ctx); err != nil {
		t.Fatalf("nil UnlandedWork: %v", err)
	}
	if err := nilAPI.KeepUnlanded(ctx, "b", "n"); err == nil {
		t.Fatal("nil KeepUnlanded: want error, got nil")
	}
	if _, err := nilAPI.ArchiveUnlanded(ctx, "b"); err == nil {
		t.Fatal("nil ArchiveUnlanded: want error, got nil")
	}
	if _, err := nilAPI.DeleteUnlandedWorktree(ctx, "b", true); err == nil {
		t.Fatal("nil DeleteUnlandedWorktree: want error, got nil")
	}
	if _, err := nilAPI.UncommittedWorkDiff(ctx, "wt"); err == nil {
		t.Fatal("nil UncommittedWorkDiff: want error, got nil")
	}
	if _, err := nilAPI.CommitUncommittedWork(ctx, "wt", "msg"); err == nil {
		t.Fatal("nil CommitUncommittedWork: want error, got nil")
	}
	if _, err := nilAPI.FixPermissions([]string{}); err == nil {
		t.Fatal("nil FixPermissions: want error, got nil")
	}
}
