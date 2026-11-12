package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatePathJoinsUnderConfiguredDataDir checks the basic contract: with a
// configured data directory, the helper joins its elements under it and does
// not wander anywhere else.
func TestStatePathJoinsUnderConfiguredDataDir(t *testing.T) {
	dataDir := t.TempDir()

	s := &Server{dataDir: dataDir}

	got := s.statePath("alerts", "alerts.json")
	root := filepath.Clean(dataDir) + string(os.PathSeparator)
	if !strings.HasPrefix(got, root) {
		t.Fatalf("statePath = %q, want a path under %q", got, filepath.Clean(dataDir))
	}
	want := filepath.Join(dataDir, "alerts", "alerts.json")
	if got != want {
		t.Fatalf("statePath = %q, want %q", got, want)
	}
}

// TestStatePathExpandsLeadingTildeInDataDir checks the one case where the
// helper may touch the home directory: the operator wrote a leading tilde in
// the configured value, so it expands against the process home.
func TestStatePathExpandsLeadingTildeInDataDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no process home directory available: %v", err)
	}

	s := &Server{dataDir: "~/dw-state"}

	got := s.statePath("sessions.json")
	want := filepath.Join(home, "dw-state", "sessions.json")
	if got != want {
		t.Fatalf("statePath = %q, want %q", got, want)
	}

	// A bare tilde means the home directory itself.
	sBare := &Server{dataDir: "~"}
	gotBare := sBare.statePath("sessions.json")
	wantBare := filepath.Join(home, "sessions.json")
	if gotBare != wantBare {
		t.Fatalf("statePath with bare tilde = %q, want %q", gotBare, wantBare)
	}
}

// TestStatePathIgnoresHomeWhenDataDirSet proves the single-source
// guarantee: when a data directory is configured, the result is rooted at it
// regardless of what HOME points at — even though "home" is right there.
func TestStatePathIgnoresHomeWhenDataDirSet(t *testing.T) {
	dataDir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	s := &Server{dataDir: dataDir}

	got := s.statePath("alerts", "alerts.json")
	if !strings.HasPrefix(got, filepath.Clean(dataDir)+string(os.PathSeparator)) {
		t.Fatalf("statePath = %q, want a path under the configured data dir %q", got, filepath.Clean(dataDir))
	}
	if strings.HasPrefix(got, filepath.Clean(fakeHome)+string(os.PathSeparator)) {
		t.Fatalf("statePath = %q, must not live under HOME (%q)", got, filepath.Clean(fakeHome))
	}

	// dataDirPath mirrors the same behavior without a Server in scope.
	gotPkg := dataDirPath(dataDir, "alerts", "alerts.json")
	if gotPkg != got {
		t.Fatalf("dataDirPath = %q, want %q (same as statePath)", gotPkg, got)
	}
}
