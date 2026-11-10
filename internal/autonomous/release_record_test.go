// Release record store — save, reload, and CI outcome update tests.
package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestReleaseRecordStore(t *testing.T) (*ReleaseRecordStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewReleaseRecordStore(dir)
	if err != nil {
		t.Fatalf("NewReleaseRecordStore: %v", err)
	}
	return s, filepath.Join(dir, ReleaseRecordFileName)
}

// TestReleaseRecordSaveAndReload records a push, then reopens the store
// from the same directory and checks every field survives the JSONL
// round trip.
func TestReleaseRecordSaveAndReload(t *testing.T) {
	s, path := newTestReleaseRecordStore(t)
	pushedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

	rec, err := s.recordReleasePush("datawatch", "v8.40.0", "abc123", "/rel/v8.40.0", pushedAt, "pushed")
	if err != nil {
		t.Fatalf("recordReleasePush: %v", err)
	}
	if rec.CIOutcome != ReleaseCIPending {
		t.Fatalf("new record CIOutcome = %q, want %q", rec.CIOutcome, ReleaseCIPending)
	}
	if rec.CIWatchedAt != nil {
		t.Fatalf("new record CIWatchedAt = %v, want nil", rec.CIWatchedAt)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s on disk: %v", path, err)
	}

	reopened, err := NewReleaseRecordStore(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := reopened.GetRelease("datawatch", "v8.40.0")
	if !ok {
		t.Fatal("record missing after reload")
	}
	if got.PushedCommit != "abc123" || got.ReleasePath != "/rel/v8.40.0" ||
		got.PushOutcome != "pushed" || got.CIOutcome != ReleaseCIPending ||
		!got.PushedAt.Equal(pushedAt) || got.CIWatchedAt != nil {
		t.Fatalf("reloaded record = %+v", got)
	}
	if n := len(reopened.List()); n != 1 {
		t.Fatalf("List len = %d, want 1", n)
	}
}

// TestReleaseRecordCIOutcomeUpdatePendingToRed moves a pending release
// to red, checks the watch time is stamped, and checks the update is
// persisted across a reload.
func TestReleaseRecordCIOutcomeUpdatePendingToRed(t *testing.T) {
	s, path := newTestReleaseRecordStore(t)
	if _, err := s.recordReleasePush("datawatch", "v8.40.0", "abc123", "/rel", time.Now(), "pushed"); err != nil {
		t.Fatalf("recordReleasePush: %v", err)
	}
	watchedAt := time.Date(2026, 11, 9, 10, 30, 0, 0, time.UTC)

	updated, err := s.setReleaseCIOutcome("datawatch", "v8.40.0", ReleaseCIRed, watchedAt)
	if err != nil {
		t.Fatalf("setReleaseCIOutcome: %v", err)
	}
	if updated.CIOutcome != ReleaseCIRed {
		t.Fatalf("CIOutcome = %q, want %q", updated.CIOutcome, ReleaseCIRed)
	}
	if updated.CIWatchedAt == nil || !updated.CIWatchedAt.Equal(watchedAt) {
		t.Fatalf("CIWatchedAt = %v, want %v", updated.CIWatchedAt, watchedAt)
	}

	reopened, err := NewReleaseRecordStore(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := reopened.GetRelease("datawatch", "v8.40.0")
	if !ok || got.CIOutcome != ReleaseCIRed || got.CIWatchedAt == nil {
		t.Fatalf("reloaded after update = %+v (ok=%v)", got, ok)
	}
}

// TestReleaseRecordCIOutcomeRejectsInvalid checks an outcome outside the
// vocabulary and an unknown release are both refused without changing
// the stored record.
func TestReleaseRecordCIOutcomeRejectsInvalid(t *testing.T) {
	s, _ := newTestReleaseRecordStore(t)
	if _, err := s.recordReleasePush("datawatch", "v1.0.0", "c1", "/r", time.Now(), "pushed"); err != nil {
		t.Fatalf("recordReleasePush: %v", err)
	}
	if _, err := s.setReleaseCIOutcome("datawatch", "v1.0.0", "bogus", time.Now()); err == nil {
		t.Fatal("invalid CI outcome accepted, want error")
	}
	if _, err := s.setReleaseCIOutcome("datawatch", "v9.9.9", ReleaseCIGreen, time.Now()); err == nil {
		t.Fatal("update of missing release accepted, want error")
	}
	got, _ := s.GetRelease("datawatch", "v1.0.0")
	if got.CIOutcome != ReleaseCIPending {
		t.Fatalf("CIOutcome changed to %q after rejected updates", got.CIOutcome)
	}
}

// TestReleaseRecordSaveRequiresIdentity checks a record without project
// or tag is refused rather than stored under an empty key.
func TestReleaseRecordSaveRequiresIdentity(t *testing.T) {
	s, _ := newTestReleaseRecordStore(t)
	if _, err := s.recordReleasePush("", "v1.0.0", "c1", "/r", time.Now(), "pushed"); err == nil {
		t.Fatal("empty project accepted, want error")
	}
	if _, err := s.recordReleasePush("datawatch", "", "c1", "/r", time.Now(), "pushed"); err == nil {
		t.Fatal("empty tag accepted, want error")
	}
	if n := len(s.List()); n != 0 {
		t.Fatalf("store holds %d records after refused saves", n)
	}
}

// TestReleaseRecordLoadRejectsCorruptLine checks a corrupt JSONL line
// fails the reopen instead of silently dropping the record.
func TestReleaseRecordLoadRejectsCorruptLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ReleaseRecordFileName), []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReleaseRecordStore(dir); err == nil {
		t.Fatal("corrupt store opened without error, want error")
	}
}
