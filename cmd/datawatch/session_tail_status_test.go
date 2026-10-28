package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/pilog"
	"github.com/dmz006/datawatch/internal/session"
)

// writeStatusPiLog writes a pi session log for workDir under root with two
// assistant turns (one bash toolCall, one write toolCall) whose newest event
// timestamp is last. Returns the session start time to use in the fixture.
func writeStatusPiLog(t *testing.T, root, workDir string, last time.Time) time.Time {
	t.Helper()
	dir := filepath.Join(root, pilog.ProjectDirName(workDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	started := last.Add(-time.Hour).UTC().Truncate(time.Second)
	name := started.Format("2006-01-02T15-04-05-000Z") + "_fake.jsonl"
	ts := func(d time.Time) string { return d.UTC().Format(time.RFC3339Nano) }
	lines := []string{
		`{"type":"message","timestamp":"` + ts(last.Add(-10*time.Second)) + `"`,
		`,"message":{"role":"assistant","content":[{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"ls -la"}}]}}`,
		`{"type":"message","timestamp":"` + ts(last) + `",`,
		`"message":{"role":"assistant","content":[{"type":"toolCall","id":"call_2","name":"write","arguments":{"path":"version.go"}},{"type":"text","text":"done"}]}}`,
	}
	content := ""
	for i := 0; i < len(lines); i += 2 {
		content += lines[i] + lines[i+1] + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return started
}

// withPiSessionsRoot points PI_CODING_AGENT_DIR at a fresh temp dir and
// returns the sessions root underneath it.
func withPiSessionsRoot(t *testing.T) string {
	t.Helper()
	agentDir := filepath.Join(t.TempDir(), "pi-agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	return filepath.Join(agentDir, "sessions")
}

func TestPiTailStatusLinePiSessions(t *testing.T) {
	root := withPiSessionsRoot(t)
	workDir := "/work/proj"
	last := time.Date(2026, 2, 7, 10, 0, 0, 0, time.UTC)
	started := writeStatusPiLog(t, root, workDir, last)
	want := "progress: turns=2 tools=bash:1,write:1 last=write idle=12s"

	for _, family := range []string{"pi", "pi-prompt"} {
		sess := &session.Session{
			ID:            "a3f2",
			FullID:        "host-a3f2",
			BackendFamily: family,
			ProjectDir:    workDir,
			CreatedAt:     started,
			LogFile:       filepath.Join(t.TempDir(), "output.log"),
		}
		if got := piTailStatusLine(sess, last.Add(12*time.Second)); got != want {
			t.Errorf("family %q: got %q, want %q", family, got, want)
		}
	}
}

func TestPiTailStatusLineAbsentForNonPiAndMissingLog(t *testing.T) {
	root := withPiSessionsRoot(t)
	workDir := "/work/proj"
	last := time.Date(2026, 2, 7, 10, 0, 0, 0, time.UTC)
	started := writeStatusPiLog(t, root, workDir, last)
	now := last.Add(12 * time.Second)

	base := &session.Session{
		ID:         "b4e1",
		FullID:     "host-b4e1",
		ProjectDir: workDir,
		CreatedAt:  started,
	}

	// Non-pi family: no status line.
	base.BackendFamily = "claude-code"
	if got := piTailStatusLine(base, now); got != "" {
		t.Errorf("claude-code session: got %q, want empty", got)
	}

	// Pi family but log unavailable (wrong work dir): empty, no panic.
	base.BackendFamily = "pi-prompt"
	base.ProjectDir = "/work/other"
	if got := piTailStatusLine(base, now); got != "" {
		t.Errorf("missing log: got %q, want empty", got)
	}
}

// captureStdout redirects os.Stdout for the duration of the call.
func captureStdoutErr(t *testing.T, call func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	cerr := call()
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out), cerr
}

// TestSessionTailEmitsStatusLineForPiOnly covers the CLI tail path: the one
// shot tail prints the stored output plus the one status line for a pi
// session, prints no status line for a non-pi session, never appends the
// status line into the stored output log, and leaves the DATAWATCH_COMPLETE
// marker extraction of the stored log unaffected.
func TestSessionTailEmitsStatusLineForPiOnly(t *testing.T) {
	root := withPiSessionsRoot(t)
	workDir := "/work/proj"
	last := time.Date(2026, 2, 7, 10, 0, 0, 0, time.UTC)
	started := writeStatusPiLog(t, root, workDir, last)
	// The status line depends on the wall clock; assert its shape rather
	// than the exact idle value in this end-to-end test.
	wantPrefix := "progress: turns=2 tools=bash:1,write:1 last=write idle="

	storedDir := t.TempDir()
	savedLog := filepath.Join(storedDir, "output.log")
	storedContent := "line one\nline two\nDATAWATCH_COMPLETE: done\n"
	if err := os.WriteFile(savedLog, []byte(storedContent), 0o600); err != nil {
		t.Fatal(err)
	}

	mkStore := func(dir string) *session.Store {
		t.Helper()
		st, err := session.NewStore(filepath.Join(dir, "sessions.json"))
		if err != nil {
			t.Fatal(err)
		}
		return st
	}

	tests := []struct {
		name       string
		family     string
		wantStatus bool
	}{
		{name: "pi-prompt", family: "pi-prompt", wantStatus: true},
		{name: "pi", family: "pi", wantStatus: true},
		{name: "claude-code", family: "claude-code", wantStatus: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			st := mkStore(dataDir)
			sess := &session.Session{
				ID:            "c5f0",
				FullID:        "host-c5f0",
				BackendFamily: tc.family,
				ProjectDir:    workDir,
				CreatedAt:     started,
				LogFile:       savedLog,
			}
			if err := st.Save(sess); err != nil {
				t.Fatal(err)
			}
			if err := st.Flush(); err != nil { // Save is write-behind; the CLI reads the file
				t.Fatal(err)
			}
			cfg := &config.Config{DataDir: dataDir}

			before, err := os.ReadFile(savedLog)
			if err != nil {
				t.Fatal(err)
			}

			out, err := captureStdoutErr(t, func() error {
				return runSessionTail(cfg, "c5f0", 20, false, 0)
			})
			if err != nil {
				t.Fatalf("runSessionTail: %v", err)
			}

			// Stored output is shown.
			if !strings.Contains(out, "line one") {
				t.Errorf("tail output missing stored line: %q", out)
			}
			if tc.wantStatus {
				if !strings.Contains(out, wantPrefix) {
					t.Errorf("tail output missing status line, got %q", out)
				}
			} else if strings.Contains(out, "progress:") {
				t.Errorf("non-pi tail output must not contain a status line, got %q", out)
			}

			// The status line must not be appended to the stored session
			// output (completion detection reads only the stored log).
			after, err := os.ReadFile(savedLog)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Errorf("stored log changed after tail:\nbefore=%q\nafter=%q", before, after)
			}
			if sum := session.ExtractCompletionSummary(string(after)); sum != "done" {
				t.Errorf("ExtractCompletionSummary after tail = %q, want %q", sum, "done")
			}
		})
	}
}

func TestSessionTailFollowRejectsNonPi(t *testing.T) {
	withPiSessionsRoot(t)
	dataDir := t.TempDir()
	st, err := session.NewStore(filepath.Join(dataDir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	savedLog := filepath.Join(tmpDir, "output.log")
	if err := os.WriteFile(savedLog, []byte("line one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(&session.Session{
		ID:            "d6a9",
		FullID:        "host-d6a9",
		BackendFamily: "opencode",
		CreatedAt:     time.Now().Add(-time.Hour),
		LogFile:       savedLog,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Flush(); err != nil { // Save is write-behind; the CLI reads the file
		t.Fatal(err)
	}
	cfg := &config.Config{DataDir: dataDir}
	_, err = captureStdoutErr(t, func() error {
		return runSessionTail(cfg, "d6a9", 20, true, time.Second)
	})
	if err == nil || !strings.Contains(err.Error(), "only supports pi sessions") {
		t.Errorf("follow on non-pi session: err = %v, want rejection", err)
	}
}
