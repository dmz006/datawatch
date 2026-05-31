// Tests for Manager.OutputSince — specifically the delta-read and
// no-new-output early-return behaviour added in v8.9.9.
package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLog writes content to a temp log file and returns its path.
func writeLog(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "output.log")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("writeLog: %v", err)
	}
	return p
}

// newManagerWithSession creates a test Manager and inserts a session whose
// LogFile points at logPath.
func newManagerWithSession(t *testing.T, logPath string) (*Manager, *Session) {
	t.Helper()
	mgr, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	sess := &Session{
		ID:      "ab01",
		FullID:  "testhost-ab01",
		LogFile: logPath,
		State:   StateRunning,
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return mgr, sess
}

// TestOutputSince_FromZero reads the full file when byteOffset == 0.
func TestOutputSince_FromZero(t *testing.T) {
	dir := t.TempDir()
	logPath := writeLog(t, dir, "line one\nline two\nline three\n")
	mgr, sess := newManagerWithSession(t, logPath)

	text, newOffset, err := mgr.OutputSince(sess.FullID, 0)
	if err != nil {
		t.Fatalf("OutputSince error: %v", err)
	}
	if !strings.Contains(text, "line one") {
		t.Errorf("expected full content, got %q", text)
	}
	if newOffset == 0 {
		t.Errorf("newOffset should be > 0, got 0")
	}
}

// TestOutputSince_Delta reads only the bytes added after the previous offset.
func TestOutputSince_Delta(t *testing.T) {
	dir := t.TempDir()
	initial := "old line one\nold line two\n"
	logPath := writeLog(t, dir, initial)
	mgr, sess := newManagerWithSession(t, logPath)

	// First read: full file.
	_, firstOffset, err := mgr.OutputSince(sess.FullID, 0)
	if err != nil {
		t.Fatalf("first OutputSince: %v", err)
	}

	// Append new content.
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("new line alpha\nnew line beta\n") //nolint:errcheck
	f.Close()                                         //nolint:errcheck

	// Second read: delta only.
	text, secondOffset, err := mgr.OutputSince(sess.FullID, firstOffset)
	if err != nil {
		t.Fatalf("second OutputSince: %v", err)
	}
	if strings.Contains(text, "old line") {
		t.Errorf("delta should not contain old lines, got %q", text)
	}
	if !strings.Contains(text, "new line alpha") || !strings.Contains(text, "new line beta") {
		t.Errorf("delta should contain new lines, got %q", text)
	}
	if secondOffset <= firstOffset {
		t.Errorf("secondOffset (%d) should be > firstOffset (%d)", secondOffset, firstOffset)
	}
}

// TestOutputSince_NoNewOutput returns empty string and same offset when the
// file has not grown since the last call. This is the primary bug fix:
// previously this case re-read the full file.
func TestOutputSince_NoNewOutput(t *testing.T) {
	dir := t.TempDir()
	logPath := writeLog(t, dir, "static content\n")
	mgr, sess := newManagerWithSession(t, logPath)

	// First read to get the offset.
	_, offset, err := mgr.OutputSince(sess.FullID, 0)
	if err != nil {
		t.Fatalf("first OutputSince: %v", err)
	}

	// Second read with offset == file size: no new output.
	text, newOffset, err := mgr.OutputSince(sess.FullID, offset)
	if err != nil {
		t.Fatalf("second OutputSince: %v", err)
	}
	if strings.TrimSpace(text) != "" {
		t.Errorf("expected empty text when no new output, got %q", text)
	}
	if newOffset != offset {
		t.Errorf("offset should be unchanged (%d), got %d", offset, newOffset)
	}
}

// TestOutputSince_RepeatedPolling simulates the UI polling current-status
// repeatedly with no new session output — each call must return empty, not
// the full file, so the LLM is not called unnecessarily.
func TestOutputSince_RepeatedPolling(t *testing.T) {
	dir := t.TempDir()
	logPath := writeLog(t, dir, "the agent finished the task\n")
	mgr, sess := newManagerWithSession(t, logPath)

	// Seed the offset (first real summary).
	_, offset, _ := mgr.OutputSince(sess.FullID, 0)

	// Poll 5 times with no new output.
	for i := range 5 {
		text, newOff, err := mgr.OutputSince(sess.FullID, offset)
		if err != nil {
			t.Fatalf("poll %d: %v", i, err)
		}
		if strings.TrimSpace(text) != "" {
			t.Errorf("poll %d: expected empty, got %q", i, text)
		}
		if newOff != offset {
			t.Errorf("poll %d: offset drifted from %d to %d", i, offset, newOff)
		}
	}
}

// TestOutputSince_LogRotated handles byteOffset > file size (log rotated or
// truncated) by reading from the beginning rather than erroring.
func TestOutputSince_LogRotated(t *testing.T) {
	dir := t.TempDir()
	logPath := writeLog(t, dir, "fresh start after rotation\n")
	mgr, sess := newManagerWithSession(t, logPath)

	// Pass an offset larger than the file — simulates a rotated log.
	text, newOffset, err := mgr.OutputSince(sess.FullID, 99999)
	if err != nil {
		t.Fatalf("OutputSince: %v", err)
	}
	// Should fall back to reading the whole file.
	if !strings.Contains(text, "fresh start") {
		t.Errorf("expected full file content after rotation, got %q", text)
	}
	if newOffset == 0 {
		t.Errorf("newOffset should be > 0")
	}
}

// TestOutputSince_ANSIStripped verifies that ANSI escape sequences are
// removed from the returned text.
func TestOutputSince_ANSIStripped(t *testing.T) {
	dir := t.TempDir()
	logPath := writeLog(t, dir, "\x1b[32mgreen text\x1b[0m\nnormal text\n")
	mgr, sess := newManagerWithSession(t, logPath)

	text, _, err := mgr.OutputSince(sess.FullID, 0)
	if err != nil {
		t.Fatalf("OutputSince: %v", err)
	}
	if strings.Contains(text, "\x1b[") {
		t.Errorf("ANSI sequences should be stripped, got %q", text)
	}
	if !strings.Contains(text, "green text") || !strings.Contains(text, "normal text") {
		t.Errorf("content should be preserved, got %q", text)
	}
}
