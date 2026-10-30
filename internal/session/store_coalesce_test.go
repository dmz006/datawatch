package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Tests for the deferred, coalescing session-store writer. See
// docs/plans/2026-10-23-session-store-write-amplification.md.

func coalesceSession(i int, task string) *Session {
	id := fmt.Sprintf("%04x", i)
	return &Session{ID: id, FullID: "co-" + id, Task: task, State: StateRunning, Hostname: "co"}
}

func TestStoreCoalesce(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"), WithFlushInterval(100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	sess := coalesceSession(1, "t")
	const saves = 200
	for i := 0; i < saves; i++ {
		st.Mutate(func() { sess.InputCount = i })
		if err := st.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(400 * time.Millisecond)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	w := st.writes.Load()
	if w < 1 || w > saves/10 {
		t.Fatalf("file writes = %d for %d saves, want between 1 and %d", w, saves, saves/10)
	}
	re, err := NewStore(st.path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := re.Get(sess.FullID)
	if !ok || got.InputCount != saves-1 {
		t.Fatalf("reloaded session = %+v ok=%v, want InputCount %d", got, ok, saves-1)
	}
}

func TestStoreFlushOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	// An hour-long interval: only Close can get the data to disk.
	st, err := NewStore(path, WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(coalesceSession(1, "one")); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(coalesceSession(2, "two")); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("co-0001"); err != nil {
		t.Fatal(err)
	}
	if len(recordFiles(t, st)) != 0 {
		t.Fatal("records written before Close despite the long interval")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	re, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := re.Get("co-0001"); ok {
		t.Fatal("deleted session survived Close")
	}
	if _, ok := re.Get("co-0002"); !ok {
		t.Fatal("saved session missing after Close")
	}
	// After Close the store writes through.
	if err := st.Save(coalesceSession(3, "three")); err != nil {
		t.Fatal(err)
	}
	re, _ = NewStore(path)
	if _, ok := re.Get("co-0003"); !ok {
		t.Fatal("post-Close Save was not written through")
	}
	if err := st.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestStoreFlushExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, _ := NewStore(path, WithFlushInterval(time.Hour))
	defer func() { _ = st.Close() }()
	_ = st.Save(coalesceSession(1, "x"))
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	re, _ := NewStore(path)
	if _, ok := re.Get("co-0001"); !ok {
		t.Fatal("Flush did not write the saved session")
	}
	// Compact output: no indentation.
	data, _ := os.ReadFile(st.recordPath("co-0001"))
	if strings.Contains(string(data), "\n  ") {
		t.Fatal("record is indented")
	}
}

func TestStoreWriteThrough(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, _ := NewStore(path, WithFlushInterval(0))
	for i := 0; i < 5; i++ {
		_ = st.Save(coalesceSession(i, "x"))
	}
	if w := st.writes.Load(); w != 5 {
		t.Fatalf("write-through writes = %d, want 5", w)
	}
	st.SetFlushInterval(time.Hour)
	st.SetFlushInterval(-1)
	_ = st.Delete("co-0000")
	re, _ := NewStore(path)
	if _, ok := re.Get("co-0000"); ok {
		t.Fatal("write-through Delete not on disk")
	}
}

// crashHelperEnv selects the helper-process mode of TestStoreCrashSafety.
const crashHelperEnv = "DATAWATCH_STORE_CRASH_HELPER"

const (
	crashSessions = 300
	crashPayload  = 20 * 1024
)

// crashState builds generation gen: every session carries the same marker, so
// a reloaded file mixing two generations (or a truncated one) is detectable.
func crashState(gen int) []*Session {
	pad := strings.Repeat("p", crashPayload)
	out := make([]*Session, 0, crashSessions)
	for i := 0; i < crashSessions; i++ {
		out = append(out, coalesceSession(i, "gen-"+strconv.Itoa(gen)+"-"+pad))
	}
	return out
}

// TestStoreCrashSafetyHelper is the subprocess body: it rewrites the store in
// a loop, one full generation per write, until the parent kills it. It only
// touches the file system, never tmux.
func TestStoreCrashSafetyHelper(t *testing.T) {
	path := os.Getenv(crashHelperEnv)
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

func TestStoreCrashSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a subprocess")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	// Seed a valid previous state so the file always exists.
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
		cmd := exec.Command(os.Args[0], "-test.run=^TestStoreCrashSafetyHelper$")
		// env -u TMUX -u TMUX_TMPDIR semantics: the helper never sees tmux.
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
				continue
			}
			cmd.Env = append(cmd.Env, kv)
		}
		cmd.Env = append(cmd.Env, crashHelperEnv+"="+path)
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
				t.Fatal("helper never completed its first write")
			}
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(time.Duration(3+round*7) * time.Millisecond) // land mid-write
		_ = cmd.Process.Signal(syscall.SIGKILL)
		_ = cmd.Wait()

		// Every record is a complete generation (records are atomic one by one).
		recs, err := filepath.Glob(filepath.Join(dir, recordsDirName, "*"+recordExt))
		if err != nil || len(recs) != crashSessions {
			t.Fatalf("round %d: %d record files after kill, want %d (%v)", round, len(recs), crashSessions, err)
		}
		for _, f := range recs {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("round %d: read: %v", round, err)
			}
			var s Session
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatalf("round %d: record %s is not valid JSON after kill: %v", round, filepath.Base(f), err)
			}
			if !strings.HasPrefix(s.Task, "gen-") || len(s.Task) < crashPayload {
				t.Fatalf("round %d: truncated record %s", round, filepath.Base(f))
			}
		}
		// The store itself must load the file without invoking recovery.
		re, err := NewStore(path)
		if err != nil {
			t.Fatalf("round %d: reload: %v", round, err)
		}
		if got := len(re.List()); got != crashSessions {
			t.Fatalf("round %d: reloaded %d sessions, want %d", round, got, crashSessions)
		}
		matches, _ := filepath.Glob(path + ".corrupted-*")
		if len(matches) > 0 {
			t.Fatalf("round %d: store needed corruption recovery: %v", round, matches)
		}
	}
}

// TestStoreConcurrent hammers Save/Delete/Mutate/Get/List/Flush from many
// goroutines; run it with -race.
func TestStoreConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, err := NewStore(path, WithFlushInterval(5*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	const iters = 150
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			sess := coalesceSession(w, "w")
			for i := 0; i < iters; i++ {
				st.Mutate(func() { sess.InputCount = i })
				if err := st.Save(sess); err != nil {
					t.Error(err)
					return
				}
				_ = st.Snapshot(sess)
				_, _ = st.Get(sess.FullID)
				_ = st.List()
				switch i % 25 {
				case 7:
					_ = st.Flush()
				case 13:
					_ = st.Delete(fmt.Sprintf("co-%04x", (w+1)%workers))
				case 19:
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
}
