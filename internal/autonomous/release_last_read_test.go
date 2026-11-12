// Last-release read tests — no release, one release, and two releases
// where the newer one must be returned.
package autonomous

import (
	"strings"
	"testing"
	"time"
)

func newLastReleaseStore(t *testing.T) *ReleaseRecordStore {
	t.Helper()
	s, _ := newTestReleaseRecordStore(t)
	return s
}

// TestReadLastReleaseNoRelease: a project with no release reads as nil.
func TestReadLastReleaseNoRelease(t *testing.T) {
	s := newLastReleaseStore(t)
	if got := lastReleaseIn(s, "datawatch"); got != nil {
		t.Fatalf("no release: got %+v, want nil", got)
	}
	if got := lastReleaseIn(nil, "datawatch"); got != nil {
		t.Fatalf("nil store: got %+v, want nil", got)
	}
	if got := ReadLastRelease("datawatch"); got != nil {
		t.Fatalf("no store installed: got %+v, want nil", got)
	}
}

// TestReadLastReleaseOneRelease: one release returns all its fields.
func TestReadLastReleaseOneRelease(t *testing.T) {
	s := newLastReleaseStore(t)
	pushedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	if _, err := s.recordReleasePush("datawatch", "v8.40.0", "abc123", "/rel/v8.40.0", pushedAt, "pushed"); err != nil {
		t.Fatalf("recordReleasePush: %v", err)
	}
	watched := time.Date(2026, 11, 9, 10, 5, 0, 0, time.UTC)
	if _, err := s.setReleaseCIOutcome("datawatch", "v8.40.0", ReleaseCIGreen, watched); err != nil {
		t.Fatalf("setReleaseCIOutcome: %v", err)
	}

	got := lastReleaseIn(s, "datawatch")
	if got == nil {
		t.Fatal("one release: got nil")
	}
	if got.Project != "datawatch" || got.Tag != "v8.40.0" || got.PushedCommit != "abc123" ||
		got.ReleasePath != "/rel/v8.40.0" || got.PushOutcome != "pushed" || got.CIOutcome != ReleaseCIGreen {
		t.Fatalf("fields mismatch: %+v", got)
	}
	if !got.PushedAt.Equal(pushedAt) {
		t.Errorf("PushedAt = %v, want %v", got.PushedAt, pushedAt)
	}
	if got.CIWatchedAt == nil || !got.CIWatchedAt.Equal(watched) {
		t.Errorf("CIWatchedAt = %v, want %v", got.CIWatchedAt, watched)
	}

	// Other projects do not see this release.
	if other := lastReleaseIn(s, "other"); other != nil {
		t.Errorf("other project: got %+v, want nil", other)
	}
}

// TestReadLastReleaseTwoReleasesReturnsNewer: with two releases the one
// pushed later is returned, whichever was recorded first.
func TestReadLastReleaseTwoReleasesReturnsNewer(t *testing.T) {
	s := newLastReleaseStore(t)
	newer := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	older := newer.Add(-time.Hour)
	// Record the newer release first so insertion order cannot explain
	// the result.
	if _, err := s.recordReleasePush("datawatch", "v8.41.0", "def456", "/rel/v8.41.0", newer, "pushed"); err != nil {
		t.Fatalf("record newer: %v", err)
	}
	if _, err := s.recordReleasePush("datawatch", "v8.40.0", "abc123", "/rel/v8.40.0", older, "pushed"); err != nil {
		t.Fatalf("record older: %v", err)
	}

	got := lastReleaseIn(s, "datawatch")
	if got == nil {
		t.Fatal("two releases: got nil")
	}
	if got.Tag != "v8.41.0" || got.PushedCommit != "def456" {
		t.Fatalf("got tag %q commit %q, want v8.41.0 def456", got.Tag, got.PushedCommit)
	}
}

// TestLastReleaseSummary covers the one-line comm channel rendering.
func TestLastReleaseSummary(t *testing.T) {
	if got := LastReleaseSummary(nil); got != "no release recorded" {
		t.Errorf("nil: got %q", got)
	}
	watched := time.Date(2026, 11, 9, 10, 5, 0, 0, time.UTC)
	rec := &ReleaseRecord{
		Project: "datawatch", Tag: "v8.40.0", PushedCommit: "abc123",
		PushOutcome: "pushed", CIOutcome: ReleaseCIGreen, CIWatchedAt: &watched,
	}
	got := LastReleaseSummary(rec)
	want := "datawatch v8.40.0 at abc123 (pushed), CI green at 2026-11-09 10:05 UTC"
	if got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	pending := &ReleaseRecord{Project: "datawatch", Tag: "v8.40.0", PushedCommit: "abc123", PushOutcome: "pushed", CIOutcome: ReleaseCIPending}
	if s := LastReleaseSummary(pending); !strings.Contains(s, "CI pending (not yet observed)") {
		t.Errorf("pending summary = %q", s)
	}
}
