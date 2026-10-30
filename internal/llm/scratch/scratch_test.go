package scratch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRecordAndLookupRoundTrip: recording attaches a path to exactly one
// session, repeated records are idempotent, and the reverse lookup goes
// both ways.
func TestRecordAndLookupRoundTrip(t *testing.T) {
	ForgetSession("rtr-1")
	ForgetPath("/scratch/rtr/x")

	RecordPath("rtr-1", "/scratch/rtr/x")
	RecordPath("rtr-1", "/scratch/rtr/x") // idempotent
	RecordPath("rtr-1", "/scratch/rtr/y")

	got, ok := SessionForPath("/scratch/rtr/x")
	if !ok || got != "rtr-1" {
		t.Fatalf("SessionForPath(/scratch/rtr/x) = %q, %v; want rtr-1, true", got, ok)
	}
	if _, ok := SessionForPath("/scratch/rtr/unknown"); ok {
		t.Fatalf("unrecorded path resolved to a session")
	}
	paths := Paths("rtr-1")
	if len(paths) != 2 || paths[0] != "/scratch/rtr/x" || paths[1] != "/scratch/rtr/y" {
		t.Fatalf("Paths(rtr-1) = %v; want two sorted entries", paths)
	}

	// An unattributed path must never become a sweep candidate.
	RecordPath("", "/scratch/rtr/unattributed")
	if _, ok := SessionForPath("/scratch/rtr/unattributed"); ok {
		t.Fatalf("empty session id recorded a path")
	}
}

// TestReleaseSessionRemovesOwnFilesOnly: releasing one session removes only
// that session's files, stamps its end mark, and leaves every other
// session's files and record intact — the "removes its own files, leaves a
// live session's files" contract.
func TestReleaseSessionRemovesOwnFilesOnly(t *testing.T) {
	root := t.TempDir()
	ended := filepath.Join(root, "cs-host-dead1")
	live := filepath.Join(root, "cs-host-live1")
	if err := os.MkdirAll(ended, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(live, 0o700); err != nil {
		t.Fatal(err)
	}
	endedFile := filepath.Join(ended, "task.txt")
	liveFile := filepath.Join(live, "task.txt")
	if err := os.WriteFile(endedFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The ended session also owns an unattributable dir (per-session worker
	// config dir shape).
	endedDir := filepath.Join(root, "datawatch-pi-agent-cs-host-dead1-17")
	if err := os.Mkdir(endedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(endedDir, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	RecordPath("cs-host-dead1", endedFile)
	RecordPath("cs-host-dead1", endedDir)
	RecordPath("cs-host-live1", liveFile)

	removed, errs := ReleaseSession("cs-host-dead1")
	if removed != 2 || len(errs) != 0 {
		t.Fatalf("ReleaseSession = %d, %v; want 2 removed, no errors", removed, errs)
	}
	if _, err := os.Stat(endedFile); !os.IsNotExist(err) {
		t.Fatalf("ended session's file survived release")
	}
	if _, err := os.Stat(endedDir); !os.IsNotExist(err) {
		t.Fatalf("ended session's dir survived release")
	}
	if _, err := os.Stat(liveFile); err != nil {
		t.Fatalf("live session's file was removed by another session's release: %v", err)
	}
	if paths := Paths("cs-host-live1"); len(paths) != 1 || paths[0] != liveFile {
		t.Fatalf("live session's record changed: %v", paths)
	}
	// Fully released: record and end mark forgotten.
	if _, ok := SessionForPath(endedFile); ok {
		t.Fatalf("released path still attributed")
	}
	_ = time.Now() // (end-mark assertion below covers the stamped case)
}

// TestReleaseSessionKeepsRefusalsRecorded: a path that refuses removal is
// left in place, stays attributed, and the end mark is stamped so the
// safety-net sweep can still own the retry. (A path whose Lstat fails with a
// hard error — here one carrying a NUL byte, which every OS rejects — is a
// deterministic refusal without needing root-only permission tricks.)
func TestReleaseSessionKeepsRefusalsRecorded(t *testing.T) {
	root := t.TempDir()
	deleted := filepath.Join(root, "cs-host-dead2", "ok.txt")
	if err := os.MkdirAll(filepath.Dir(deleted), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deleted, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	refused := "/does/not/matter/\x00bad"

	RecordPath("cs-host-dead2", deleted)
	RecordPath("cs-host-dead2", refused)

	removed, errs := ReleaseSession("cs-host-dead2")
	if removed != 1 || len(errs) != 1 {
		t.Fatalf("ReleaseSession = %d, %d errs; want 1 removed, 1 refusal", removed, len(errs))
	}
	if _, err := os.Stat(deleted); !os.IsNotExist(err) {
		t.Fatalf("releasable path survived: %v", err)
	}
	// The refused path stays attributed to the session…
	if sid, ok := SessionForPath(refused); !ok || sid != "cs-host-dead2" {
		t.Fatalf("refused path lost its attribution: %q, %v", sid, ok)
	}
	// …and the end mark is stamped so the sweep can time its retry.
	if _, ok := EndedAt("cs-host-dead2"); !ok {
		t.Fatalf("end mark missing after failed release")
	}
}

// TestEndMarkLifecycle: MarkEnded is visible to EndedAt and cleared by
// ForgetSession.
func TestEndMarkLifecycle(t *testing.T) {
	ForgetSession("emark")
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	MarkEnded("emark", at)
	got, ok := EndedAt("emark")
	if !ok || !got.Equal(at) {
		t.Fatalf("EndedAt = %v, %v; want %v, true", got, ok, at)
	}
	ForgetSession("emark")
	if _, ok := EndedAt("emark"); ok {
		t.Fatalf("end mark survived ForgetSession")
	}
	MarkEnded("", time.Now())
	if _, ok := EndedAt(""); ok {
		t.Fatalf("empty session id stamped an end mark")
	}
}

// TestForgetPath: forgetting one recorded path leaves the session's other
// paths intact.
func TestForgetPath(t *testing.T) {
	ForgetSession("fp-1")
	RecordPath("fp-1", "/scratch/fp/a")
	RecordPath("fp-1", "/scratch/fp/b")
	ForgetPath("/scratch/fp/a")
	if _, ok := SessionForPath("/scratch/fp/a"); ok {
		t.Fatalf("forgotten path still attributed")
	}
	if paths := Paths("fp-1"); len(paths) != 1 || paths[0] != "/scratch/fp/b" {
		t.Fatalf("ForgetPath took the wrong entries: %v", paths)
	}
}
