package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// Tests for the per-session record layout. See store_records.go and
// docs/plans/2026-10-23-session-store-write-amplification.md.

func recSession(i int, task string) *Session {
	id := fmt.Sprintf("%04x", i)
	return &Session{ID: id, FullID: "rec-" + id, Name: "n" + id, Task: task, State: StateRunning, Hostname: "rec"}
}

// writeLegacyStore writes an old-format sessions.json (JSON array) of n
// sessions, encrypted when key is set, and returns the raw file bytes.
func writeLegacyStore(t *testing.T, path string, n int, key []byte) []byte {
	t.Helper()
	list := make([]*Session, 0, n)
	for i := 0; i < n; i++ {
		list = append(list, recSession(i, strings.Repeat("t", 200)+fmt.Sprint(i)))
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := secfile.WriteFile(path, data, 0644, key); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func recordFiles(t *testing.T, st *Store) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(st.recordsDir(), "*"+recordExt))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStoreIncremental(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, err := NewStore(path, WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	if !st.records {
		t.Fatal("new store is not in the record layout")
	}
	const n = 60
	for i := 0; i < n; i++ {
		if err := st.Save(recSession(i, strings.Repeat("x", 4096))); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(recordFiles(t, st)); got != n {
		t.Fatalf("record files = %d, want %d", got, n)
	}
	type fp struct {
		mod  time.Time
		size int64
	}
	before := map[string]fp{}
	for _, f := range recordFiles(t, st) {
		fi, _ := os.Stat(f)
		before[f] = fp{fi.ModTime(), fi.Size()}
	}
	time.Sleep(20 * time.Millisecond) // make an mtime change visible

	bytes0, recs0 := st.bytesWritten.Load(), st.recordWrites.Load()
	sess, _ := st.Get("rec-0007")
	st.Mutate(func() { sess.InputCount = 42 })
	if err := st.Save(sess); err != nil {
		t.Fatal(err)
	}
	wrote := st.bytesWritten.Load() - bytes0
	if got := st.recordWrites.Load() - recs0; got != 1 {
		t.Fatalf("record writes for one change = %d, want 1", got)
	}
	one, _ := json.Marshal(sess)
	if wrote != int64(len(one)) {
		t.Fatalf("bytes written = %d, want %d (one record)", wrote, len(one))
	}
	changed := recordPathOf(st, "rec-0007")
	for f, old := range before {
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if f == changed {
			if fi.ModTime().Equal(old.mod) {
				t.Fatal("changed record was not rewritten")
			}
			continue
		}
		if !fi.ModTime().Equal(old.mod) || fi.Size() != old.size {
			t.Fatalf("untouched record %s was rewritten", filepath.Base(f))
		}
	}
	// A Save with nothing changed writes nothing.
	recs1 := st.recordWrites.Load()
	if err := st.Save(sess); err != nil {
		t.Fatal(err)
	}
	if st.recordWrites.Load() != recs1 {
		t.Fatal("unchanged session was rewritten")
	}
	// Delete removes only its record.
	if err := st.Delete("rec-0003"); err != nil {
		t.Fatal(err)
	}
	if got := len(recordFiles(t, st)); got != n-1 {
		t.Fatalf("record files after delete = %d, want %d", got, n-1)
	}
	re, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := re.Get("rec-0007"); !ok || got.InputCount != 42 {
		t.Fatalf("reloaded session = %+v ok=%v", got, ok)
	}
	if _, ok := re.Get("rec-0003"); ok {
		t.Fatal("deleted session reloaded")
	}
	// A Mutate with no Save is still found by a flush.
	other, _ := st.Get("rec-0010")
	st.Mutate(func() { other.InputCount = 9 })
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	re, _ = NewStore(path)
	if got, _ := re.Get("rec-0010"); got == nil || got.InputCount != 9 {
		t.Fatal("Mutate-only change lost")
	}
}

func recordPathOf(st *Store, fullID string) string { return st.recordPath(fullID) }

func TestStoreDirtySetCoalesces(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	a, b := recSession(1, "a"), recSession(2, "b")
	_ = st.Save(a)
	_ = st.Save(b)
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	r0 := st.recordWrites.Load()
	for i := 0; i < 100; i++ {
		st.Mutate(func() { a.InputCount = i })
		_ = st.Save(a)
	}
	_ = st.Close()
	if got := st.recordWrites.Load() - r0; got < 1 || got > 5 {
		t.Fatalf("record writes for 100 saves of one session = %d, want 1..5", got)
	}
}

func TestStoreMigrateOldFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	const n = 500
	raw := writeLegacyStore(t, path, n, nil)

	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.records {
		t.Fatal("old store was not migrated")
	}
	if got := len(st.List()); got != n {
		t.Fatalf("sessions after migration = %d, want %d", got, n)
	}
	if got := len(recordFiles(t, st)); got != n {
		t.Fatalf("record files = %d, want %d", got, n)
	}
	bak, err := os.ReadFile(path + preRecordsBackupSuffix)
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if !bytes.Equal(bak, raw) {
		t.Fatal("backup differs from the original file")
	}
	if !recordsLayoutPresent(st.recordsDir()) {
		t.Fatal("layout marker missing")
	}
	// Reopen: reads the records, does not touch the backup.
	re, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(re.List()); got != n {
		t.Fatalf("reopened sessions = %d, want %d", got, n)
	}
	for i := 0; i < n; i++ {
		got, ok := re.Get(fmt.Sprintf("rec-%04x", i))
		if !ok || !strings.HasSuffix(got.Task, fmt.Sprint(i)) {
			t.Fatalf("session %d lost or altered: %+v", i, got)
		}
	}
	// Changes after migration persist incrementally.
	s0, _ := re.Get("rec-0000")
	re.Mutate(func() { s0.Name = "renamed" })
	_ = re.Save(s0)
	_ = re.Close()
	again, _ := NewStore(path)
	if got, _ := again.Get("rec-0000"); got == nil || got.Name != "renamed" {
		t.Fatal("post-migration change lost")
	}
}

func TestStoreMigrateKeepsExistingBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	writeLegacyStore(t, path, 3, nil)
	old := []byte("precious earlier backup")
	if err := os.WriteFile(path+preRecordsBackupSuffix, old, 0600); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.records || len(st.List()) != 3 {
		t.Fatalf("records=%v sessions=%d", st.records, len(st.List()))
	}
	got, _ := os.ReadFile(path + preRecordsBackupSuffix)
	if !bytes.Equal(got, old) {
		t.Fatal("existing backup was overwritten")
	}
	if _, err := os.Stat(path + preRecordsBackupSuffix + ".1"); err != nil {
		t.Fatalf("second backup not written: %v", err)
	}
}

func TestStoreMigrateRecoversCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	body := `[{"id":"aaaa","full_id":"host-aaaa","state":"complete"},{"id":"bbbb","full_id":"host-bbbb","state":"complete"},{"id":"cc`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sessions", "host-cccc")
	_ = os.MkdirAll(sub, 0755)
	_ = os.WriteFile(filepath.Join(sub, "session.json"), []byte(`{"id":"cccc","full_id":"host-cccc","state":"running"}`), 0644)
	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.records || len(st.List()) != 3 {
		t.Fatalf("records=%v sessions=%d, want 3 (2 recovered + 1 merged)", st.records, len(st.List()))
	}
	re, _ := NewStore(path)
	for _, id := range []string{"host-aaaa", "host-bbbb", "host-cccc"} {
		if _, ok := re.Get(id); !ok {
			t.Fatalf("%s lost across migration", id)
		}
	}
}

func TestStoreEncryptedRecords(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	wrong := bytes.Repeat([]byte{9}, 32)
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	// Migration of an encrypted old file.
	writeLegacyStore(t, path, 40, key)
	st, err := NewStoreEncrypted(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if !st.records || len(st.List()) != 40 {
		t.Fatalf("records=%v sessions=%d, want 40", st.records, len(st.List()))
	}
	sess := recSession(900, "secret-task-marker")
	if err := st.Save(sess); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	// Records are unreadable without the key.
	for _, f := range recordFiles(t, st) {
		raw, _ := os.ReadFile(f)
		if bytes.Contains(raw, []byte("secret-task-marker")) || bytes.Contains(raw, []byte(`"full_id"`)) {
			t.Fatalf("record %s holds plaintext", filepath.Base(f))
		}
	}
	// The backup of the encrypted file stays encrypted too.
	bak, _ := os.ReadFile(path + preRecordsBackupSuffix)
	if bytes.Contains(bak, []byte(`"full_id"`)) {
		t.Fatal("backup of an encrypted store is plaintext")
	}
	// A store opened with the wrong key skips every record rather than
	// returning garbage; with no key the same.
	for _, k := range [][]byte{wrong, nil} {
		bad, err := newStoreWithKey(path, k)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(bad.List()); got != 0 {
			t.Fatalf("store with wrong key read %d sessions", got)
		}
	}
	// Readable with the key.
	good, err := NewStoreEncrypted(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(good.List()); got != 41 {
		t.Fatalf("sessions with key = %d, want 41", got)
	}
	if got, ok := good.Get(sess.FullID); !ok || got.Task != "secret-task-marker" {
		t.Fatalf("encrypted record not readable: %+v", got)
	}
}

func TestStoreOddSessionIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, _ := NewStore(path, WithFlushInterval(0))
	// Safe single path elements, including ones recordFileName hashes.
	ids := []string{"host-1", "ho st", ".", strings.Repeat("z", 300)}
	for _, id := range ids {
		if err := st.Save(&Session{FullID: id, ID: "1", Task: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "etc")); err == nil {
		t.Fatal("record escaped its directory")
	}
	re, _ := NewStore(path)
	for _, id := range ids {
		if _, ok := re.Get(id); !ok {
			t.Fatalf("session %q lost", id)
		}
	}
}

func TestCheckedRecordPathRefusesUnsafeIDs(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"), WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "a/b", "/abs", "../x", "..", "a..b", "../../etc/passwd", `a\b`, "a\x00b"} {
		if p, err := st.checkedRecordPath(id); !errors.Is(err, errBadRecordID) {
			t.Errorf("checkedRecordPath(%q) = %q, %v; want errBadRecordID", id, p, err)
		}
	}
	for _, id := range []string{"host-1", "ho st", ".", "a.b", strings.Repeat("z", 300)} {
		p, err := st.checkedRecordPath(id)
		if err != nil {
			t.Errorf("checkedRecordPath(%q): %v", id, err)
			continue
		}
		if p != st.recordPath(id) || filepath.Dir(p) != st.recordsDir() {
			t.Errorf("checkedRecordPath(%q) = %q, want %q inside %s", id, p, st.recordPath(id), st.recordsDir())
		}
	}
}

// TestStoreRefusesUnsafeIDsWithoutTouchingOutside: a record ID with a slash or
// '..' is neither written nor removed, other sessions still persist, and a
// file outside the records directory survives.
func TestStoreRefusesUnsafeIDsWithoutTouchingOutside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	st, err := NewStore(path, WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	bad := []string{"../victim", "a/b", "x..y", ""}
	good := &Session{FullID: "host-ok", ID: "1", Task: "t"}
	for _, id := range bad {
		if err := st.Save(&Session{FullID: id, ID: "1", Task: "t"}); err != nil {
			t.Fatalf("Save(%q) wedged the flush: %v", id, err)
		}
	}
	if err := st.Save(good); err != nil {
		t.Fatal(err)
	}
	if got := len(recordFiles(t, st)); got != 1 {
		t.Fatalf("record files = %d, want only the safe session", got)
	}
	for _, id := range bad {
		if err := st.Delete(id); err != nil {
			t.Fatalf("Delete(%q): %v", id, err)
		}
	}
	if b, err := os.ReadFile(victim); err != nil || string(b) != "keep" {
		t.Fatalf("file outside the store was touched: %q, %v", b, err)
	}
	re, _ := NewStore(path)
	if _, ok := re.Get("host-ok"); !ok {
		t.Fatal("safe session lost")
	}
}

// TestStoreRecordsCorruptRecord: one unreadable record costs only that
// session, and mergeFromSessionDirs can still restore it.
func TestStoreRecordsCorruptRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	st, _ := NewStore(path, WithFlushInterval(0))
	for i := 0; i < 5; i++ {
		_ = st.Save(recSession(i, "t"))
	}
	if err := os.WriteFile(st.recordPath("rec-0002"), []byte("{trunc"), 0644); err != nil {
		t.Fatal(err)
	}
	re, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(re.List()); got != 4 {
		t.Fatalf("sessions = %d, want 4", got)
	}
	sub := filepath.Join(dir, "sessions", "rec-0002")
	_ = os.MkdirAll(sub, 0755)
	_ = os.WriteFile(filepath.Join(sub, "session.json"), []byte(`{"id":"0002","full_id":"rec-0002","state":"running"}`), 0644)
	re2, _ := NewStore(path)
	if _, ok := re2.Get("rec-0002"); !ok {
		t.Fatal("corrupt record not restored from the session directory")
	}
	re3, _ := NewStore(path)
	if got, ok := re3.Get("rec-0002"); !ok || got.State != StateRunning {
		t.Fatal("restored record was not persisted")
	}
}

// recCrashHelperEnv selects the helper-process mode of TestStoreRecordsCrashSafety.
const recCrashHelperEnv = "DATAWATCH_STORE_RECORDS_CRASH_HELPER"

// TestStoreRecordsCrashHelper is the subprocess body: it keeps rewriting every
// record with a new generation until the parent kills it.
func TestStoreRecordsCrashHelper(t *testing.T) {
	path := os.Getenv(recCrashHelperEnv)
	if path == "" {
		t.Skip("helper process only")
	}
	st, err := NewStore(path, WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	for gen := 1; ; gen++ {
		st.Mutate(func() {
			for _, s := range crashState(gen) {
				st.sessions[s.FullID] = s
			}
		})
		if err := st.Flush(); err != nil {
			t.Fatal(err)
		}
		if gen == 1 {
			_ = os.WriteFile(path+".ready", []byte("1"), 0600)
		}
	}
}

// TestStoreRecordsCrashSafety SIGKILLs a writer mid-flush five times and
// reloads: every record must be complete (old or new generation, never torn),
// the set of sessions intact, and no recovery needed.
func TestStoreRecordsCrashSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a subprocess")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	seed, _ := NewStore(path, WithFlushInterval(0))
	seed.Mutate(func() {
		for _, s := range crashState(0) {
			seed.sessions[s.FullID] = s
		}
	})
	if err := seed.Flush(); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 5; round++ {
		_ = os.Remove(path + ".ready")
		cmd := exec.Command(os.Args[0], "-test.run=^TestStoreRecordsCrashHelper$")
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
				continue
			}
			cmd.Env = append(cmd.Env, kv)
		}
		cmd.Env = append(cmd.Env, recCrashHelperEnv+"="+path)
		// The helper never survives the SIGKILL below, so its TestMain-deferred
		// cleanup never runs: point its scratch space at a test-scoped dir so
		// the testhome dw-test-home-*/dw-test-tmux-* trees it creates are
		// removed by our cleanup instead of orphaning in $TMPDIR. (Last
		// duplicate wins in exec.Cmd.Env, overriding any inherited TMPDIR.)
		cmd.Env = append(cmd.Env, "TMPDIR="+t.TempDir())
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for {
			if _, err := os.Stat(path + ".ready"); err == nil {
				break
			}
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				t.Fatal("helper never completed its first flush")
			}
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(time.Duration(3+round*7) * time.Millisecond)
		_ = cmd.Process.Signal(syscall.SIGKILL)
		_ = cmd.Wait()

		re, err := NewStore(path)
		if err != nil {
			t.Fatalf("round %d: reload: %v", round, err)
		}
		if got := len(re.List()); got != crashSessions {
			t.Fatalf("round %d: reloaded %d sessions, want %d", round, got, crashSessions)
		}
		for _, s := range re.List() {
			if !strings.HasPrefix(s.Task, "gen-") || len(s.Task) < crashPayload {
				t.Fatalf("round %d: torn record %s (%d bytes)", round, s.FullID, len(s.Task))
			}
		}
		if m, _ := filepath.Glob(path + ".corrupted-*"); len(m) > 0 {
			t.Fatalf("round %d: recovery ran: %v", round, m)
		}
	}
}

// TestStoreRecordsRace hammers Save/Delete/Mutate/Flush/Get/List from many
// goroutines against the record layout; run it with -race.
func TestStoreRecordsRace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, err := NewStore(path, WithFlushInterval(3*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	const iters = 200
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			sess := recSession(w, "w")
			for i := 0; i < iters; i++ {
				st.Mutate(func() { sess.InputCount = i })
				if err := st.Save(sess); err != nil {
					t.Error(err)
					return
				}
				_ = st.Snapshot(sess)
				_, _ = st.Get(sess.FullID)
				_ = st.List()
				switch i % 20 {
				case 5:
					_ = st.Flush()
				case 11:
					_ = st.Delete(fmt.Sprintf("rec-%04x", (w+1)%workers))
				case 17:
					st.SetFlushInterval(time.Duration(1+i%3) * time.Millisecond)
				}
			}
		}(w)
	}
	wg.Wait()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	re, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(re.List()), len(st.List()); got != want {
		t.Fatalf("reloaded %d sessions, in memory %d", got, want)
	}
	for _, s := range st.List() {
		got, ok := re.Get(s.FullID)
		if !ok || got.InputCount != s.InputCount {
			t.Fatalf("session %s: reloaded %+v, memory InputCount %d", s.FullID, got, s.InputCount)
		}
	}
}

// TestStoreRecordsTwoProcessesShareDir: two stores on one directory (the
// re-exec overlap) each persist their own sessions without losing the other's.
func TestStoreRecordsTwoStoresShareDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	a, _ := NewStore(path, WithFlushInterval(0))
	b, _ := NewStore(path, WithFlushInterval(0))
	for i := 0; i < 10; i++ {
		_ = a.Save(recSession(i, "a"))
		_ = b.Save(recSession(100+i, "b"))
	}
	re, _ := NewStore(path)
	if got := len(re.List()); got != 20 {
		t.Fatalf("sessions = %d, want 20", got)
	}
}
