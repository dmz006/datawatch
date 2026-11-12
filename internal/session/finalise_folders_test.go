package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

var folderPassTime = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

// seedFolder writes <dataDir>/sessions/<full>/session.json for sess plus an
// output log and a notes file, and returns the folder path.
func seedFolder(t *testing.T, mgr *Manager, sess Session, logText string) string {
	t.Helper()
	if sess.FullID == "" {
		sess.FullID = "testhost-" + sess.ID
	}
	if sess.Hostname == "" {
		sess.Hostname = "testhost"
	}
	dir := filepath.Join(mgr.dataDir, "sessions", sess.FullID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(&sess, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFolderFile(t, dir, "session.json", string(data))
	writeFolderFile(t, dir, "output.log", logText)
	writeFolderFile(t, dir, "timeline.md", "created\n")
	return dir
}

func writeFolderFile(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFolderFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readFolderSession(t *testing.T, dir string) Session {
	t.Helper()
	var s Session
	if err := json.Unmarshal([]byte(readFolderFile(t, dir, "session.json")), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func finaliseFolders(t *testing.T, mgr *Manager) FinaliseResult {
	t.Helper()
	res := mgr.FinaliseSessionFolders(folderPassTime)
	if !res.TerminalListOK {
		t.Fatal("terminal listing must succeed against the fake")
	}
	return res
}

func assertFolderUntouched(t *testing.T, dir, before string) {
	t.Helper()
	if after := readFolderFile(t, dir, "session.json"); after != before {
		t.Fatalf("session.json changed:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestFinaliseFolderWithNoTerminalIsFinalised(t *testing.T) {
	cases := []struct {
		name string
		id   string
		log  string
		want State
	}{
		{"no marker is killed", "fa01", "work without the marker\n", StateKilled},
		{"marker is complete", "fa02", "done\n\x1b[32mDATAWATCH_COMPLETE: all finished\x1b[0m\n", StateComplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, state := range []State{StateRunning, StateWaitingInput, StateStalled, StateRateLimited} {
				mgr, _ := newTestManagerWithFake(t)
				dir := seedFolder(t, mgr, Session{ID: tc.id, TmuxSession: "cs-" + tc.id, State: state}, tc.log)
				res := finaliseFolders(t, mgr)

				got := readFolderSession(t, dir)
				if got.State != tc.want {
					t.Fatalf("from %s: state = %q, want %q", state, got.State, tc.want)
				}
				if got.EndNote != GoneEndNote {
					t.Fatalf("end_note = %q, want %q", got.EndNote, GoneEndNote)
				}
				if got.EndedAt == nil || !got.EndedAt.Equal(folderPassTime) {
					t.Fatalf("ended_at = %v, want %v", got.EndedAt, folderPassTime)
				}
				if len(res.Finalised) != 1 || res.Finalised[0] != "testhost-"+tc.id {
					t.Fatalf("Finalised = %v", res.Finalised)
				}
			}
		})
	}

	// Already-final folders and unrelated states are left alone.
	mgr, _ := newTestManagerWithFake(t)
	dir := seedFolder(t, mgr, Session{ID: "fa03", State: StateComplete}, "x\n")
	before := readFolderFile(t, dir, "session.json")
	res := finaliseFolders(t, mgr)
	assertFolderUntouched(t, dir, before)
	if len(res.Finalised) != 0 || res.Skipped != 1 {
		t.Fatalf("Finalised = %v, Skipped = %d", res.Finalised, res.Skipped)
	}
}

func TestFinaliseFolderWithLiveTerminalIsUntouched(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	if err := fake.NewSessionWithSize("cs-fl01", 80, 24); err != nil {
		t.Fatal(err)
	}
	live := seedFolder(t, mgr, Session{ID: "fl01", TmuxSession: "cs-fl01", State: StateRunning}, "output\n")
	liveBefore := readFolderFile(t, live, "session.json")

	// A recorded subprocess pid that still runs the task also keeps a folder.
	// The test process itself stands in for the task: its own command line
	// carries the test binary's name.
	if _, err := os.ReadFile("/proc/self/cmdline"); err != nil {
		t.Skip("no /proc: subprocess liveness cannot be checked")
	}
	sub := seedFolder(t, mgr, Session{ID: "fl02", Task: filepath.Base(os.Args[0]), State: StateRunning}, "output\n")
	writeFolderFile(t, sub, "subprocess.pid", strconv.Itoa(os.Getpid()))
	subBefore := readFolderFile(t, sub, "session.json")

	// A folder from another host is not judged here.
	foreign := seedFolder(t, mgr, Session{ID: "fl03", Hostname: "otherhost", FullID: "otherhost-fl03", State: StateRunning}, "output\n")
	foreignBefore := readFolderFile(t, foreign, "session.json")

	res := finaliseFolders(t, mgr)
	assertFolderUntouched(t, live, liveBefore)
	assertFolderUntouched(t, sub, subBefore)
	assertFolderUntouched(t, foreign, foreignBefore)
	if len(res.Finalised) != 0 {
		t.Fatalf("Finalised = %v, want none", res.Finalised)
	}
}

func TestFinaliseFolderHeldLiveByStoreIsUntouched(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)

	// Record held live by its terminal; the folder has no other sign of life.
	if err := fake.NewSessionWithSize("cs-fh01", 80, 24); err != nil {
		t.Fatal(err)
	}
	liveRec := Session{ID: "fh01", FullID: "testhost-fh01", Hostname: "testhost", TmuxSession: "cs-fh01", State: StateRunning, UpdatedAt: time.Now()}
	if err := mgr.SaveSession(&liveRec); err != nil {
		t.Fatal(err)
	}
	liveDir := seedFolder(t, mgr, Session{ID: "fh01", State: StateRunning}, "output\n")
	liveBefore := readFolderFile(t, liveDir, "session.json")

	// Record held live by a runner, with a folder whose own terminal is gone.
	runRec := Session{ID: "fh02", FullID: "testhost-fh02", Hostname: "testhost", State: StateRunning, UpdatedAt: time.Now()}
	if err := mgr.SaveSession(&runRec); err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRunner("testhost-fh02")
	runDir := seedFolder(t, mgr, Session{ID: "fh02", State: StateRunning}, "output\n")
	runBefore := readFolderFile(t, runDir, "session.json")

	// Record held for another host.
	forRec := Session{ID: "fh03", FullID: "testhost-fh03", Hostname: "otherhost", State: StateKilled, UpdatedAt: time.Now()}
	if err := mgr.SaveSession(&forRec); err != nil {
		t.Fatal(err)
	}
	forDir := seedFolder(t, mgr, Session{ID: "fh03", State: StateRunning}, "output\n")
	forBefore := readFolderFile(t, forDir, "session.json")

	res := finaliseFolders(t, mgr)
	assertFolderUntouched(t, liveDir, liveBefore)
	assertFolderUntouched(t, runDir, runBefore)
	assertFolderUntouched(t, forDir, forBefore)
	if len(res.Finalised) != 0 {
		t.Fatalf("Finalised = %v, want none", res.Finalised)
	}
}

func TestFinaliseFolderAlignsToFinalStoreRecord(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	ended := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	rec := Session{
		ID: "fn01", FullID: "testhost-fn01", Hostname: "testhost", TmuxSession: "cs-fn01",
		State: StateComplete, EndedAt: &ended, EndNote: "finished by the store",
		UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(&rec); err != nil {
		t.Fatal(err)
	}
	dir := seedFolder(t, mgr, Session{ID: "fn01", TmuxSession: "cs-fn01", State: StateRunning, Task: "keep me"}, "no marker here\n")

	res := finaliseFolders(t, mgr)
	got := readFolderSession(t, dir)
	if got.State != StateComplete {
		t.Fatalf("state = %q, want %q", got.State, StateComplete)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(ended) {
		t.Fatalf("ended_at = %v, want %v", got.EndedAt, ended)
	}
	if got.EndNote != "finished by the store" {
		t.Fatalf("end_note = %q", got.EndNote)
	}
	if got.Task != "keep me" || got.TmuxSession != "cs-fn01" {
		t.Fatalf("other saved fields changed: %+v", got)
	}
	if len(res.Finalised) != 1 || res.Finalised[0] != "testhost-fn01" {
		t.Fatalf("Finalised = %v", res.Finalised)
	}

	// A second pass finds the folder aligned and writes nothing.
	before := readFolderFile(t, dir, "session.json")
	res = finaliseFolders(t, mgr)
	assertFolderUntouched(t, dir, before)
	if len(res.Finalised) != 0 {
		t.Fatalf("second pass Finalised = %v", res.Finalised)
	}

	// The store record itself is never changed by the folder pass.
	stored, _ := mgr.store.Get("testhost-fn01")
	if stored.State != StateComplete || stored.EndNote != "finished by the store" {
		t.Fatalf("store record changed: %+v", stored)
	}
}

func TestFinaliseFolderKeepsOutputFiles(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	dir := seedFolder(t, mgr, Session{ID: "fk01", TmuxSession: "cs-fk01", State: StateRunning}, "the output log\n")
	writeFolderFile(t, dir, "output.log.enc", "encrypted bytes")
	writeFolderFile(t, dir, "conversation.md", "# Conversation\n")
	writeFolderFile(t, dir, "subprocess.rc", "0\n")

	before := map[string]string{}
	names := []string{"output.log", "output.log.enc", "conversation.md", "subprocess.rc", "timeline.md"}
	for _, n := range names {
		before[n] = readFolderFile(t, dir, n)
	}
	res := finaliseFolders(t, mgr)
	if len(res.Finalised) != 1 {
		t.Fatalf("Finalised = %v, want one", res.Finalised)
	}
	for _, n := range names {
		if after := readFolderFile(t, dir, n); after != before[n] {
			t.Fatalf("%s changed: %q -> %q", n, before[n], after)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(names)+1 { // the names above plus session.json
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		t.Fatalf("folder files = %v, want %d files with no temporary file left", got, len(names)+1)
	}
	if got := readFolderSession(t, dir); got.State != StateKilled {
		t.Fatalf("state = %q, want %q", got.State, StateKilled)
	}
}

func TestFinaliseFolderUnparseableIsSkipped(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	bad := filepath.Join(mgr.dataDir, "sessions", "testhost-fu01")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFolderFile(t, bad, "session.json", "{ this is not json")
	writeFolderFile(t, bad, "output.log", "output\n")
	// A stray directory without session.json and a stray file are ignored.
	if err := os.MkdirAll(filepath.Join(mgr.dataDir, "sessions", "stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFolderFile(t, filepath.Join(mgr.dataDir, "sessions"), "notes.txt", "x")
	good := seedFolder(t, mgr, Session{ID: "fu02", TmuxSession: "cs-fu02", State: StateRunning}, "output\n")

	res := finaliseFolders(t, mgr)
	if res.Unparseable != 1 {
		t.Fatalf("Unparseable = %d, want 1", res.Unparseable)
	}
	if got := readFolderFile(t, bad, "session.json"); got != "{ this is not json" {
		t.Fatalf("unparseable session.json changed: %q", got)
	}
	if got := readFolderSession(t, good); got.State != StateKilled {
		t.Fatalf("parseable folder state = %q, want %q", got.State, StateKilled)
	}
	if len(res.Finalised) != 1 || res.Finalised[0] != "testhost-fu02" {
		t.Fatalf("Finalised = %v", res.Finalised)
	}
}
