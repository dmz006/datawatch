package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "identity.yaml")
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m, path
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func listBackups(t *testing.T, m *Manager) []Backup {
	t.Helper()
	bs, err := m.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	return bs
}

func backupBytes(t *testing.T, m *Manager, name string) []byte {
	t.Helper()
	return mustRead(t, filepath.Join(m.backupDir(), name))
}

// wantNewestBackup asserts the newest backup holds exactly prev.
func wantNewestBackup(t *testing.T, m *Manager, prev []byte) {
	t.Helper()
	bs := listBackups(t, m)
	if len(bs) == 0 {
		t.Fatal("no backup was made")
	}
	if got := backupBytes(t, m, bs[0].Name); !bytes.Equal(got, prev) {
		t.Fatalf("newest backup differs from previous file\nbackup:\n%s\nwant:\n%s", got, prev)
	}
}

func TestBackupBeforeEachWrite(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "one"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if n := len(listBackups(t, m)); n != 0 {
		t.Fatalf("first write made %d backups, want 0", n)
	}

	prev := mustRead(t, path)
	if _, err := m.Set(Identity{Role: "two"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	wantNewestBackup(t, m, prev)

	prev = mustRead(t, path)
	if _, err := m.Update(Identity{CurrentFocus: "focus"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantNewestBackup(t, m, prev)

	prev = mustRead(t, path)
	if _, err := m.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	wantNewestBackup(t, m, prev)

	if _, err := m.Set(Identity{Role: "three"}); err != nil {
		t.Fatalf("Set after reset: %v", err)
	}
	prev = mustRead(t, path)
	if _, err := m.Restore("latest"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	wantNewestBackup(t, m, prev)
	if n := len(listBackups(t, m)); n != 4 {
		t.Fatalf("got %d backups, want 4", n)
	}
}

func TestNoBackupForUnworthyFiles(t *testing.T) {
	cases := map[string]string{
		"whitespace":    "  \n\t\n",
		"empty":         "",
		"updated only":  "updated_at: 2026-01-01T00:00:00Z\n",
		"explicit null": "role: \"\"\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			m, path := newTestManager(t)
			mustWrite(t, path, content)
			if _, err := m.Update(Identity{Role: "x"}); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if _, err := os.Stat(m.backupDir()); !os.IsNotExist(err) {
				t.Fatalf("backup dir should not exist, stat err=%v", err)
			}
		})
	}

	t.Run("missing", func(t *testing.T) {
		m, _ := newTestManager(t)
		if _, err := m.Set(Identity{Role: "x"}); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if n := len(listBackups(t, m)); n != 0 {
			t.Fatalf("got %d backups, want 0", n)
		}
	})
}

func TestNoBackupWhenUnchanged(t *testing.T) {
	m, _ := newTestManager(t)
	id := Identity{Role: "same", Values: []string{"a"}}
	for i := 0; i < 5; i++ {
		if _, err := m.Set(id); err != nil {
			t.Fatalf("Set %d: %v", i, err)
		}
	}
	if n := len(listBackups(t, m)); n != 1 {
		t.Fatalf("got %d backups after repeated identical saves, want 1", n)
	}
}

func TestRetentionPrunesToLimit(t *testing.T) {
	m, _ := newTestManager(t)
	for i := 0; i < 15; i++ {
		if _, err := m.Set(Identity{Role: string(rune('a' + i))}); err != nil {
			t.Fatalf("Set %d: %v", i, err)
		}
	}
	bs := listBackups(t, m)
	if len(bs) != DefaultBackupsToKeep {
		t.Fatalf("got %d backups, want %d", len(bs), DefaultBackupsToKeep)
	}
	// 15 saves leave 14 backups; the newest 10 are roles e..n, newest first.
	if bs[0].Role != string(rune('a'+13)) || bs[len(bs)-1].Role != "e" {
		t.Fatalf("wrong backups kept: first=%q last=%q", bs[0].Role, bs[len(bs)-1].Role)
	}
}

func TestRetentionProviderChangeAppliesNextWrite(t *testing.T) {
	m, _ := newTestManager(t)
	var n atomic.Int64
	n.Store(10)
	m.SetBackupsToKeep(func() int { return int(n.Load()) })
	for i := 0; i < 8; i++ {
		if _, err := m.Set(Identity{Role: string(rune('a' + i))}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	if got := len(listBackups(t, m)); got != 7 {
		t.Fatalf("got %d backups, want 7", got)
	}
	n.Store(3)
	if _, err := m.Update(Identity{Role: "z"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := len(listBackups(t, m)); got != 3 {
		t.Fatalf("got %d backups after lowering limit, want 3", got)
	}
}

func TestRetentionPrunesOnUnchangedSave(t *testing.T) {
	m, _ := newTestManager(t)
	var n atomic.Int64
	n.Store(10)
	m.SetBackupsToKeep(func() int { return int(n.Load()) })
	for i := 0; i < 5; i++ {
		if _, err := m.Set(Identity{Role: string(rune('a' + i))}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	n.Store(2)
	// Same content as the file already on disk and as the newest backup.
	if _, err := m.Set(Identity{Role: "e"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := m.Set(Identity{Role: "e"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := len(listBackups(t, m)); got > 3 {
		t.Fatalf("got %d backups, want the limit applied", got)
	}
}

func TestBackupsToKeepFallsBackToDefault(t *testing.T) {
	m, _ := newTestManager(t)
	if got := m.BackupsToKeep(); got != DefaultBackupsToKeep {
		t.Fatalf("no provider: got %d", got)
	}
	for _, v := range []int{0, -2} {
		m.SetBackupsToKeep(func() int { return v })
		if got := m.BackupsToKeep(); got != DefaultBackupsToKeep {
			t.Fatalf("provider %d: got %d, want %d", v, got, DefaultBackupsToKeep)
		}
	}
	m.SetBackupsToKeep(func() int { return 1 })
	if got := m.BackupsToKeep(); got != 1 {
		t.Fatalf("provider 1: got %d", got)
	}
}

func TestSetEmptyOverNonEmptyIsRefused(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "keep me", Values: []string{"v"}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	before := mustRead(t, path)
	_, err := m.Set(Identity{})
	if !errors.Is(err, ErrEmptyOverwrite) {
		t.Fatalf("got %v, want ErrEmptyOverwrite", err)
	}
	if !strings.Contains(err.Error(), "role") || !strings.Contains(err.Error(), "values") {
		t.Errorf("error should name the fields that would be lost: %v", err)
	}
	if !bytes.Equal(before, mustRead(t, path)) {
		t.Fatal("file changed after a refused Set")
	}
	if n := len(listBackups(t, m)); n != 0 {
		t.Fatalf("refused Set made %d backups", n)
	}
	if m.Get().Role != "keep me" {
		t.Fatal("cache changed after a refused Set")
	}
}

func TestSetEmptyOverEmptyAllowed(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{}); err != nil {
		t.Fatalf("Set on missing file: %v", err)
	}
	if _, err := m.Set(Identity{}); err != nil {
		t.Fatalf("Set over empty file: %v", err)
	}
	if n := len(listBackups(t, m)); n != 0 {
		t.Fatalf("empty files used %d backup slots", n)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing: %v", err)
	}
}

func TestResetClearsAndKeepsRestorableBackup(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "ops", NorthStarGoals: []string{"g"}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	prev := mustRead(t, path)
	got, err := m.Reset()
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if !got.IsEmpty() || got.UpdatedAt.IsZero() {
		t.Fatalf("Reset result: %+v", got)
	}
	if !m.Get().IsEmpty() {
		t.Fatal("cache not cleared")
	}
	wantNewestBackup(t, m, prev)

	back, err := m.Restore("latest")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if back.Role != "ops" || len(back.NorthStarGoals) != 1 {
		t.Fatalf("restored: %+v", back)
	}
}

func TestResetAlreadyEmptyChangesNothing(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Reset(); err != nil {
		t.Fatalf("Reset on missing file: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Reset created a file: %v", err)
	}
	mustWrite(t, path, "updated_at: 2026-01-01T00:00:00Z\n")
	before := mustRead(t, path)
	if _, err := m.Reset(); err != nil {
		t.Fatalf("Reset on empty file: %v", err)
	}
	if !bytes.Equal(before, mustRead(t, path)) {
		t.Fatal("Reset rewrote an already empty file")
	}
	if n := len(listBackups(t, m)); n != 0 {
		t.Fatalf("got %d backups", n)
	}
}

func TestResetOnCorruptFileBacksUpRaw(t *testing.T) {
	m, path := newTestManager(t)
	raw := "role: [unclosed\n"
	mustWrite(t, path, raw)
	if _, err := m.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	wantNewestBackup(t, m, []byte(raw))
	if err := m.Reload(); err != nil {
		t.Fatalf("file should parse after Reset: %v", err)
	}
}

func TestListBackups(t *testing.T) {
	m, _ := newTestManager(t)
	bs, err := m.ListBackups()
	if err != nil || bs == nil || len(bs) != 0 {
		t.Fatalf("missing dir: bs=%v err=%v", bs, err)
	}

	for _, r := range []string{"a", "b", "c"} {
		if _, err := m.Set(Identity{Role: r, Values: []string{"v"}, ContextNotes: "n"}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	// Strays that must be ignored.
	mustWrite(t, filepath.Join(m.backupDir(), "notes.txt"), "x")
	mustWrite(t, filepath.Join(m.backupDir(), "identity-bad.yaml"), "x")
	if err := os.Mkdir(filepath.Join(m.backupDir(), "identity-20260101T000000.000Z.yaml"), 0o700); err != nil {
		t.Fatal(err)
	}

	bs = listBackups(t, m)
	if len(bs) != 2 {
		t.Fatalf("got %d backups, want 2: %+v", len(bs), bs)
	}
	if bs[0].Role != "b" || bs[1].Role != "a" {
		t.Fatalf("not newest first: %+v", bs)
	}
	if bs[0].FieldCount != 3 || bs[0].SizeBytes == 0 || bs[0].CreatedAt.IsZero() {
		t.Fatalf("bad metadata: %+v", bs[0])
	}
	if !sort.SliceIsSorted(bs, func(i, j int) bool { return bs[i].Name > bs[j].Name }) {
		t.Fatal("names are not in descending order")
	}
	js, err := json.Marshal(bs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"name"`, `"created_at"`, `"size_bytes"`, `"role"`, `"field_count"`} {
		if !strings.Contains(string(js), k) {
			t.Errorf("json missing %s: %s", k, js)
		}
	}
}

func TestListBackupsUnreadableDir(t *testing.T) {
	m, _ := newTestManager(t)
	mustWrite(t, m.backupDir(), "a file, not a directory")
	if _, err := m.ListBackups(); err == nil {
		t.Fatal("expected an error when the backup path is a file")
	}
}

func TestListBackupsDescribesCorruptBackup(t *testing.T) {
	m, _ := newTestManager(t)
	if err := os.MkdirAll(m.backupDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(m.backupDir(), "identity-20260101T000000.000Z.yaml"), "role: [oops\n")
	bs := listBackups(t, m)
	if len(bs) != 1 || bs[0].FieldCount != 0 || bs[0].Role != "" || bs[0].SizeBytes == 0 {
		t.Fatalf("corrupt backup should still be listed: %+v", bs)
	}
}

func TestRestoreByNameAndLatest(t *testing.T) {
	m, _ := newTestManager(t)
	for _, r := range []string{"first", "second", "third"} {
		if _, err := m.Set(Identity{Role: r}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	bs := listBackups(t, m) // second, first
	if len(bs) != 2 {
		t.Fatalf("got %d backups", len(bs))
	}

	got, err := m.Restore(bs[1].Name)
	if err != nil {
		t.Fatalf("Restore by name: %v", err)
	}
	if got.Role != "first" || got.UpdatedAt.IsZero() || m.Get().Role != "first" {
		t.Fatalf("restored by name: %+v", got)
	}
	m2, err := NewManager(m.Path())
	if err != nil || m2.Get().Role != "first" {
		t.Fatalf("restored identity not on disk: %v %+v", err, m2.Get())
	}

	got, err = m.Restore("latest")
	if err != nil {
		t.Fatalf("Restore latest: %v", err)
	}
	// The restore above backed up "third"; that is now the newest.
	if got.Role != "third" {
		t.Fatalf("latest restored %q, want third", got.Role)
	}
}

func TestSecondRestoreUndoesFirst(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.Set(Identity{Role: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Set(Identity{Role: "new"}); err != nil {
		t.Fatal(err)
	}
	if got, err := m.Restore("latest"); err != nil || got.Role != "old" {
		t.Fatalf("first restore: %+v %v", got, err)
	}
	if got, err := m.Restore("latest"); err != nil || got.Role != "new" {
		t.Fatalf("second restore should undo the first: %+v %v", got, err)
	}
}

func TestRestoreRejectsBadNames(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Set(Identity{Role: "y"}); err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, path)
	for _, name := range []string{
		"../x", "a/b.yaml", `a\b.yaml`, "..", "", "nope.yaml",
		"identity-20260101T000000.000Z.yaml", // well formed, but absent
	} {
		if _, err := m.Restore(name); !errors.Is(err, ErrBackupNotFound) {
			t.Errorf("Restore(%q) = %v, want ErrBackupNotFound", name, err)
		}
	}
	if !bytes.Equal(before, mustRead(t, path)) {
		t.Fatal("a rejected Restore changed the file")
	}
}

func TestRestoreLatestWithoutBackups(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.Restore("latest"); !errors.Is(err, ErrBackupNotFound) {
		t.Fatalf("got %v, want ErrBackupNotFound", err)
	}
}

func TestRestoreEmptyAndCorruptBackups(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.Set(Identity{Role: "live"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(m.backupDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	empty := "identity-20260101T000000.000Z.yaml"
	corrupt := "identity-20260102T000000.000Z.yaml"
	mustWrite(t, filepath.Join(m.backupDir(), empty), "updated_at: 2026-01-01T00:00:00Z\n")
	mustWrite(t, filepath.Join(m.backupDir(), corrupt), "role: [oops\n")

	if _, err := m.Restore(empty); !errors.Is(err, ErrBackupEmpty) {
		t.Fatalf("got %v, want ErrBackupEmpty", err)
	}
	_, err := m.Restore(corrupt)
	if err == nil || errors.Is(err, ErrBackupEmpty) || errors.Is(err, ErrBackupNotFound) {
		t.Fatalf("corrupt backup: got %v", err)
	}
	// "latest" skips both and finds nothing with content.
	if _, err := m.Restore("latest"); !errors.Is(err, ErrBackupNotFound) {
		t.Fatalf("latest over unusable backups: %v", err)
	}
	if m.Get().Role != "live" {
		t.Fatal("identity changed by failed restores")
	}
}

func TestOutOfBandEditIsMergedAndBackedUp(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "cached role", CurrentFocus: "old focus"}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, "role: edited by hand\ncontext_notes: written elsewhere\n")
	edited := mustRead(t, path)

	got, err := m.Update(Identity{CurrentFocus: "new focus"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Role != "edited by hand" || got.ContextNotes != "written elsewhere" || got.CurrentFocus != "new focus" {
		t.Fatalf("out-of-band edit lost: %+v", got)
	}
	wantNewestBackup(t, m, edited)
}

func TestOutOfBandEmptyingBlocksEmptySetOnlyIfDiskHasContent(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "r"}); err != nil {
		t.Fatal(err)
	}
	// Disk gained content the cache does not know about.
	mustWrite(t, path, "role: r\nvalues: [kept]\n")
	if _, err := m.Set(Identity{}); !errors.Is(err, ErrEmptyOverwrite) {
		t.Fatalf("got %v", err)
	}
	// Disk was emptied by hand: the guard follows the disk, not the cache.
	mustWrite(t, path, "updated_at: 2026-01-01T00:00:00Z\n")
	if _, err := m.Set(Identity{}); err != nil {
		t.Fatalf("empty over hand-emptied file: %v", err)
	}
}

func TestUpdateFallsBackToCacheWhenFileGone(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "cached"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got, err := m.Update(Identity{CurrentFocus: "f"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Role != "cached" || got.CurrentFocus != "f" {
		t.Fatalf("cache not used: %+v", got)
	}
	if n := len(listBackups(t, m)); n != 0 {
		t.Fatalf("missing file produced %d backups", n)
	}
}

func TestCorruptFileIsBackedUpRaw(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "cached"}); err != nil {
		t.Fatal(err)
	}
	raw := []byte("role: [unclosed\n  : : :\n\x00binary")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := m.Update(Identity{CurrentFocus: "f"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantNewestBackup(t, m, raw)
	if got.Role != "cached" {
		t.Fatalf("cache should be the base for a corrupt file: %+v", got)
	}
	if err := m.Reload(); err != nil {
		t.Fatalf("file should be valid again: %v", err)
	}
}

func TestBackupDirFailureAbortsWrite(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "original"}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, m.backupDir(), "a regular file where the directory belongs")
	before := mustRead(t, path)

	if _, err := m.Update(Identity{Role: "changed"}); err == nil {
		t.Fatal("Update should fail when the backup dir cannot be created")
	}
	if _, err := m.Set(Identity{Role: "changed"}); err == nil {
		t.Fatal("Set should fail when the backup dir cannot be created")
	}
	if _, err := m.Reset(); err == nil {
		t.Fatal("Reset should fail when the backup dir cannot be created")
	}
	if !bytes.Equal(before, mustRead(t, path)) {
		t.Fatal("identity file touched despite backup failure")
	}
	if m.Get().Role != "original" {
		t.Fatalf("cache changed despite backup failure: %+v", m.Get())
	}
}

func TestRestoreBackupFailureAbortsWrite(t *testing.T) {
	m, path := newTestManager(t)
	for _, r := range []string{"a", "b"} {
		if _, err := m.Set(Identity{Role: r}); err != nil {
			t.Fatal(err)
		}
	}
	name := listBackups(t, m)[0].Name
	// A backup directory that lists fine but refuses new files.
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	mustWrite(t, path, "role: changed by hand\n")
	before := mustRead(t, path)
	if err := os.Chmod(m.backupDir(), 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(m.backupDir(), 0o700) //nolint:errcheck
	if _, err := m.Restore(name); err == nil {
		t.Fatal("Restore should fail when the current file cannot be backed up")
	}
	if !bytes.Equal(before, mustRead(t, path)) {
		t.Fatal("file touched by failed Restore")
	}
}

func TestAtomicWriteFailureLeavesFileIntact(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	m, path := newTestManager(t)
	for _, r := range []string{"a", "b"} {
		if _, err := m.Set(Identity{Role: r}); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Dir(path)
	before := mustRead(t, path)
	// The backup dir already exists and stays writable; the parent does not.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700) //nolint:errcheck
	if _, err := m.Update(Identity{Role: "c"}); err == nil {
		t.Fatal("Update should fail when no temp file can be created")
	}
	if !bytes.Equal(before, mustRead(t, path)) {
		t.Fatal("identity file changed by a failed write")
	}
	if m.Get().Role != "b" {
		t.Fatalf("cache changed by a failed write: %+v", m.Get())
	}
}

func TestUnreadableIdentityPathIsAnError(t *testing.T) {
	// A directory sits where the identity file should be.
	path := filepath.Join(t.TempDir(), "identity.yaml")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{path: path}
	if _, err := m.Set(Identity{Role: "x"}); err == nil {
		t.Error("Set should fail")
	}
	if _, err := m.Update(Identity{Role: "x"}); err == nil {
		t.Error("Update should fail")
	}
	if _, err := m.Reset(); err == nil {
		t.Error("Reset should fail")
	}
	if _, err := m.Restore("latest"); !errors.Is(err, ErrBackupNotFound) {
		t.Errorf("Restore latest with no backups: %v", err)
	}
	if err := os.MkdirAll(m.backupDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(m.backupDir(), "identity-20260101T000000.000Z.yaml"), "role: r\n")
	if _, err := m.Restore("latest"); err == nil {
		t.Error("Restore should fail when the current file cannot be read")
	}
}

func TestBackupListFailureAbortsWrite(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "x"}); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	if err := os.MkdirAll(m.backupDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(m.backupDir(), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(m.backupDir(), 0o700) //nolint:errcheck
	before := mustRead(t, path)
	if _, err := m.Update(Identity{Role: "y"}); err == nil {
		t.Fatal("Update should fail when the backup dir cannot be listed")
	}
	if _, err := m.Restore("latest"); err == nil {
		t.Fatal("Restore should fail when the backup dir cannot be listed")
	}
	if !bytes.Equal(before, mustRead(t, path)) {
		t.Fatal("identity file changed")
	}
}

func TestFileModes(t *testing.T) {
	m, path := newTestManager(t)
	for _, r := range []string{"a", "b"} {
		if _, err := m.Set(Identity{Role: r}); err != nil {
			t.Fatal(err)
		}
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("identity file mode: %v %v", st, err)
	}
	st, err := os.Stat(m.backupDir())
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("backup dir mode: %v %v", st, err)
	}
	for _, b := range listBackups(t, m) {
		st, err := os.Stat(filepath.Join(m.backupDir(), b.Name))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("backup %s mode: %v %v", b.Name, st, err)
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".identity-*.tmp"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestWriteBackupFileUniqueNamesSortChronologically(t *testing.T) {
	dir := t.TempDir()
	var names []string
	newest := ""
	for i := 0; i < 4; i++ {
		n, err := writeBackupFile(dir, "20260101T000000.000Z", newest, []byte{byte('a' + i)})
		if err != nil {
			t.Fatalf("writeBackupFile: %v", err)
		}
		names, newest = append(names, n), n
	}
	// A later millisecond, then a clock that stepped back.
	for _, stamp := range []string{"20260101T000000.001Z", "20251231T000000.000Z"} {
		n, err := writeBackupFile(dir, stamp, newest, []byte("later"))
		if err != nil {
			t.Fatal(err)
		}
		names, newest = append(names, n), n
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] || !backupNameRE.MatchString(n) {
			t.Fatalf("duplicate or malformed name %q in %v", n, names)
		}
		seen[n] = true
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("names do not sort in creation order: %v", names)
	}
}

func TestWriteBackupFileAfterPrunedBareName(t *testing.T) {
	dir := t.TempDir()
	// The bare name of this millisecond was pruned; only suffixed ones remain.
	n, err := writeBackupFile(dir, "20260101T000000.000Z", "identity-20260101T000000.000Z_007.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != "identity-20260101T000000.000Z_008.yaml" {
		t.Fatalf("got %q", n)
	}
	// A name already taken by a racing writer is skipped.
	n, err = writeBackupFile(dir, "20260101T000000.000Z", "identity-20260101T000000.000Z_007.yaml", nil)
	if err != nil || n != "identity-20260101T000000.000Z_009.yaml" {
		t.Fatalf("got %q %v", n, err)
	}
}

func TestWriteBackupFileFailures(t *testing.T) {
	if _, err := writeBackupFile(filepath.Join(t.TempDir(), "missing"), "20260101T000000.000Z", "", nil); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
	dir := t.TempDir()
	for i := 0; i < 1000; i++ {
		name := "identity-20260101T000000.000Z.yaml"
		if i > 0 {
			name = fmt.Sprintf("identity-20260101T000000.000Z_%03d.yaml", i)
		}
		mustWrite(t, filepath.Join(dir, name), "x")
	}
	if _, err := writeBackupFile(dir, "20260101T000000.000Z", "", nil); err == nil {
		t.Fatal("expected an error when every name is taken")
	}
}

func TestConcurrentUpdatesLeaveValidFile(t *testing.T) {
	m, path := newTestManager(t)
	if _, err := m.Set(Identity{Role: "start"}); err != nil {
		t.Fatal(err)
	}
	const workers, rounds = 8, 6
	var wg sync.WaitGroup
	errs := make(chan error, workers*rounds)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				if _, err := m.Update(Identity{CurrentFocus: strings.Repeat("x", w+r+1)}); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Update: %v", err)
	}
	m2, err := NewManager(path)
	if err != nil {
		t.Fatalf("file is not valid after concurrent updates: %v", err)
	}
	if m2.Get().Role != "start" || m2.Get().CurrentFocus == "" {
		t.Fatalf("lost data: %+v", m2.Get())
	}
	if n := len(listBackups(t, m)); n == 0 || n > DefaultBackupsToKeep {
		t.Fatalf("got %d backups", n)
	}
}

func TestStripUpdatedAt(t *testing.T) {
	a := stripUpdatedAt([]byte("role: x\nupdated_at: 2026-01-01T00:00:00Z\n"))
	b := stripUpdatedAt([]byte("role: x\nupdated_at: 2027-05-05T00:00:00Z\n"))
	if !bytes.Equal(a, b) {
		t.Fatalf("%q != %q", a, b)
	}
	c := stripUpdatedAt([]byte("role: y\nupdated_at: 2026-01-01T00:00:00Z\n"))
	if bytes.Equal(a, c) {
		t.Fatal("different content compared equal")
	}
}
