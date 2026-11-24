package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

// supervisorProfileFire registers a spawn schedule through EnsureNamedSpawn
// with launch role supervisor, installs profileFn as the scheduled-spawn
// profile hook, fires the due schedule and returns the spawned session plus
// the options the fake launch function ran under.
func supervisorProfileFire(t *testing.T, profileFn func(ds DeferredSession, opts *StartOptions) *string) (*Session, int) {
	t.Helper()
	mgr, _ := newTestManagerWithFake(t)
	store, err := NewScheduleStore(t.TempDir() + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetScheduleStore(store)
	mgr.SetScheduledSpawnProfileFn(profileFn)

	if _, created, err := store.EnsureNamedSpawn(AddSpawnOptions{
		Task:         "supervisor pass",
		LaunchRole:   "supervisor",
		OneShot:      true,
		ScheduleName: "supervisor-profile-regression",
		Model:        "sonnet",
		RunAt:        time.Now().Add(-time.Second),
	}); err != nil || !created {
		t.Fatalf("EnsureNamedSpawn: created=%v err=%v", created, err)
	}

	before := len(mgr.store.List())
	mgr.processScheduledItems(context.Background())
	after := mgr.store.List()
	if len(after) != before+1 {
		t.Fatalf("schedule fire spawned %d sessions, want 1", len(after)-before)
	}
	got, _ := mgr.store.Get(after[before].FullID)
	if got == nil {
		t.Fatal("spawned session missing from store")
	}
	return got, len(after)
}

// A scheduled supervisor spawn must start with the resolved worker profile
// recorded on the session.
func TestSupervisorProfile_ScheduledSpawnRecordsWorkerProfile(t *testing.T) {
	var mu sync.Mutex
	var seenRole string
	launched := 0
	reason := ""
	sess, _ := supervisorProfileFire(t, func(ds DeferredSession, opts *StartOptions) *string {
		mu.Lock()
		seenRole = ds.LaunchRole
		mu.Unlock()
		opts.WorkerProfile = workerprofile.WorkerProfile{Name: "datawatch-tools"}
		opts.LaunchFn = func(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
			mu.Lock()
			launched++
			mu.Unlock()
			return nil
		}
		return &reason
	})
	mu.Lock()
	defer mu.Unlock()
	if seenRole != "supervisor" {
		t.Fatalf("profile hook saw launch role %q, want supervisor", seenRole)
	}
	if sess.WorkerProfile == nil && sess.ProfileFallbackReason == "" {
		t.Fatal("scheduled supervisor spawn recorded neither WorkerProfile nor ProfileFallbackReason")
	}
	if sess.WorkerProfile == nil || sess.WorkerProfile.Name != "datawatch-tools" {
		t.Fatalf("WorkerProfile = %+v, want datawatch-tools", sess.WorkerProfile)
	}
	if sess.LaunchRole != "supervisor" {
		t.Fatalf("LaunchRole = %q, want supervisor", sess.LaunchRole)
	}
	if launched != 1 {
		t.Fatalf("fake launch function ran %d times, want 1", launched)
	}
}

// When the profile cannot be applied the spawn still starts, and the reason
// is persisted on the session record rather than lost.
func TestSupervisorProfile_ScheduledSpawnRecordsFallbackReason(t *testing.T) {
	reason := "profile datawatch-tools could not be applied: test"
	sess, _ := supervisorProfileFire(t, func(ds DeferredSession, opts *StartOptions) *string {
		opts.LaunchFn = func(ctx context.Context, task, tmuxSession, projectDir, logFile string) error { return nil }
		return &reason
	})
	if sess.WorkerProfile != nil {
		t.Fatalf("WorkerProfile = %+v, want none on fallback", sess.WorkerProfile)
	}
	if sess.ProfileFallbackReason != reason {
		t.Fatalf("ProfileFallbackReason = %q, want %q", sess.ProfileFallbackReason, reason)
	}
}
