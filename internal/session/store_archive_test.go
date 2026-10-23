package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tests for the session archive and per-field cap (store_archive.go).

func archKey() []byte { return []byte("0123456789abcdef0123456789abcdef") }

func archStore(t *testing.T, key []byte) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	var s *Store
	var err error
	if key != nil {
		s, err = NewStoreEncrypted(path, key, WithFlushInterval(0))
	} else {
		s, err = NewStore(path, WithFlushInterval(0))
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func archSess(id string, st State, age time.Duration) *Session {
	return &Session{ID: id, FullID: "h-" + id, Name: "name-" + id, Task: "task " + id, State: st,
		CreatedAt: time.Now().Add(-age - time.Hour), UpdatedAt: time.Now().Add(-age)}
}

const day = 24 * time.Hour

func TestStoreArchiveMovesOldTerminal(t *testing.T) {
	s, dir := archStore(t, nil)
	for _, sess := range []*Session{
		archSess("aa01", StateComplete, 20*day),
		archSess("aa02", StateFailed, 15*day),
		archSess("aa03", StateKilled, 30*day),
		archSess("aa04", StateComplete, day), // too recent
	} {
		if err := s.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.ArchiveOnce(time.Now())
	if err != nil || n != 3 {
		t.Fatalf("archived %d, err %v; want 3", n, err)
	}
	if _, ok := s.Get("h-aa01"); ok {
		t.Fatal("archived session still active")
	}
	if _, ok := s.Get("h-aa04"); !ok {
		t.Fatal("recent session left the active set")
	}
	got, ok := s.GetArchived("h-aa01")
	if !ok || got.Task != "task aa01" {
		t.Fatalf("GetArchived = %+v, %v", got, ok)
	}
	if got, ok := s.GetArchived("aa02"); !ok || got.FullID != "h-aa02" { // short id
		t.Fatalf("GetArchived(short) = %+v, %v", got, ok)
	}
	if _, ok := s.GetAny("h-aa03"); !ok {
		t.Fatal("GetAny does not fall back to the archive")
	}
	if _, err := os.Stat(filepath.Join(dir, archiveFileName)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.recordPath("h-aa01")); !os.IsNotExist(err) {
		t.Fatalf("record of archived session still on disk: %v", err)
	}

	// A fresh store neither loads the archive nor resurrects the sessions,
	// but still reads them.
	s2, err := NewStore(filepath.Join(dir, "sessions.json"), WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if len(s2.List()) != 1 {
		t.Fatalf("reopened active set has %d sessions, want 1", len(s2.List()))
	}
	if _, ok := s2.GetArchived("h-aa03"); !ok {
		t.Fatal("archive unreadable after reopen")
	}

	// Paging and search.
	page, total, err := s2.ListArchived("", 0, 2)
	if err != nil || total != 3 || len(page) != 2 || page[0].FullID != "h-aa02" {
		t.Fatalf("page = %v total=%d err=%v", ids(page), total, err)
	}
	page, total, _ = s2.ListArchived("", 2, 2)
	if total != 3 || len(page) != 1 || page[0].FullID != "h-aa03" {
		t.Fatalf("page 2 = %v total=%d", ids(page), total)
	}
	page, total, _ = s2.ListArchived("NAME-AA03", 0, 10)
	if total != 1 || len(page) != 1 || page[0].FullID != "h-aa03" {
		t.Fatalf("search = %v total=%d", ids(page), total)
	}
}

func ids(l []*Session) []string {
	var out []string
	for _, s := range l {
		out = append(out, s.FullID)
	}
	return out
}

func TestStoreArchiveKeepsNonTerminal(t *testing.T) {
	s, _ := archStore(t, nil)
	for _, sess := range []*Session{
		archSess("bb01", StateRunning, 60*day),
		archSess("bb02", StateWaitingInput, 60*day),
		archSess("bb03", StateRateLimited, 60*day),
		archSess("bb04", StateComplete, 0),
	} {
		_ = s.Save(sess)
	}
	zero := archSess("bb05", StateComplete, 0)
	zero.UpdatedAt = time.Time{}
	_ = s.Save(zero)
	if n, err := s.ArchiveOnce(time.Now()); err != nil || n != 0 {
		t.Fatalf("archived %d, err %v; want 0", n, err)
	}
	if len(s.List()) != 5 {
		t.Fatalf("active = %d, want 5", len(s.List()))
	}
	s.SetArchiveAfter(0)
	_ = s.Save(archSess("bb06", StateKilled, 90*day))
	if n, _ := s.ArchiveOnce(time.Now()); n != 0 {
		t.Fatal("archiving ran while disabled")
	}
	if _, err := os.Stat(s.archivePath()); !os.IsNotExist(err) {
		t.Fatalf("archive file created with nothing archived: %v", err)
	}
}

func TestStoreRestoreFromArchive(t *testing.T) {
	s, dir := archStore(t, nil)
	_ = s.Save(archSess("cc01", StateComplete, 40*day))
	_ = s.Save(archSess("cc02", StateComplete, 40*day))
	if n, _ := s.ArchiveOnce(time.Now()); n != 2 {
		t.Fatalf("archived %d, want 2", n)
	}
	sess, err := s.RestoreFromArchive("cc01")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(sess.UpdatedAt) > time.Minute {
		t.Fatalf("restored UpdatedAt not refreshed: %v", sess.UpdatedAt)
	}
	if got, ok := s.Get("h-cc01"); !ok || got.Task != "task cc01" {
		t.Fatal("restored session not active")
	}
	if s.IsArchived("h-cc01") {
		t.Fatal("restored session still listed as archived")
	}
	if n, _ := s.ArchiveOnce(time.Now()); n != 0 {
		t.Fatalf("restored session archived again at once (%d)", n)
	}
	if _, err := s.RestoreFromArchive("h-cc01"); err == nil {
		t.Fatal("restoring an active session succeeded")
	}
	if _, err := s.RestoreFromArchive("nope"); err == nil {
		t.Fatal("restoring an unknown session succeeded")
	}
	// The restore survives a reopen; the other session stays archived.
	s2, err := NewStore(filepath.Join(dir, "sessions.json"), WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, ok := s2.Get("h-cc01"); !ok {
		t.Fatal("restored session lost on reopen")
	}
	if s2.IsArchived("h-cc01") || !s2.IsArchived("h-cc02") {
		t.Fatal("archive index wrong after reopen")
	}
}

func TestStoreArchiveEncrypted(t *testing.T) {
	s, dir := archStore(t, archKey())
	sess := archSess("dd01", StateComplete, 30*day)
	sess.Task = "super-secret-task-text"
	_ = s.Save(sess)
	if n, _ := s.ArchiveOnce(time.Now()); n != 1 {
		t.Fatal("not archived")
	}
	raw, err := os.ReadFile(filepath.Join(dir, archiveFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret-task-text") {
		t.Fatal("archive holds plaintext task text")
	}
	if got, ok := s.GetArchived("h-dd01"); !ok || got.Task != "super-secret-task-text" {
		t.Fatalf("decrypted = %+v, %v", got, ok)
	}
	s2, err := NewStoreEncrypted(filepath.Join(dir, "sessions.json"), archKey(), WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if res, total, _ := s2.ListArchived("secret", 0, 5); total != 1 || len(res) != 1 {
		t.Fatalf("search over encrypted archive: total=%d", total)
	}
	// Without the key the records cannot be read.
	s3, err := NewStore(filepath.Join(dir, "x", "sessions.json"), WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	_ = os.Rename(filepath.Join(dir, archiveFileName), filepath.Join(dir, "x", archiveFileName))
	if _, ok := s3.GetArchived("h-dd01"); ok {
		t.Fatal("encrypted archive read without a key")
	}
}

func TestStoreArchiveTornLine(t *testing.T) {
	s, dir := archStore(t, nil)
	_ = s.Save(archSess("ee01", StateComplete, 30*day))
	s.ArchiveOnce(time.Now()) //nolint:errcheck
	f, _ := os.OpenFile(filepath.Join(dir, archiveFileName), os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString(`{"id":"h-torn","ts":1,"rec":{"id`) // crash mid-append, no newline
	f.Close()
	s2, err := NewStore(filepath.Join(dir, "sessions.json"), WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	_ = s2.Save(archSess("ee02", StateFailed, 30*day))
	if n, err := s2.ArchiveOnce(time.Now()); err != nil || n != 1 {
		t.Fatalf("archive after torn line: %d, %v", n, err)
	}
	if _, ok := s2.GetArchived("h-ee01"); !ok {
		t.Fatal("lost the session before the torn line")
	}
	if _, ok := s2.GetArchived("h-ee02"); !ok {
		t.Fatal("session appended after a torn line is unreadable")
	}
}

func TestStoreFieldCapTruncatesWithMarker(t *testing.T) {
	s, dir := archStore(t, nil)
	s.SetFieldCap(1024)
	big := strings.Repeat("é", 2000) // 4000 bytes, multi-byte
	sess := archSess("ff01", StateRunning, 0)
	sess.Task = big
	sess.LastResponse = "small"
	if err := s.Save(sess); err != nil {
		t.Fatal(err)
	}
	if sess.Task != big {
		t.Fatal("in-memory session was modified")
	}
	disk, err := s.readRecord(s.recordPath("h-ff01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Task) > 1024+200 || !strings.Contains(disk.Task, "[truncated 2976 bytes, full text in side file sessions/h-ff01/fields/task.txt]") {
		t.Fatalf("persisted task = %d bytes, tail %q", len(disk.Task), disk.Task[len(disk.Task)-90:])
	}
	if !strings.HasPrefix(disk.Task, "ééé") || disk.LastResponse != "small" {
		t.Fatal("prefix or small field damaged")
	}
	side, err := os.ReadFile(filepath.Join(dir, "sessions", "h-ff01", "fields", "task.txt"))
	if err != nil || string(side) != big {
		t.Fatalf("side file = %d bytes, err %v", len(side), err)
	}
	full, ok, err := s.ReadField("h-ff01", "task")
	if err != nil || !ok || full != big {
		t.Fatalf("ReadField ok=%v err=%v len=%d", ok, err, len(full))
	}
	if _, ok, _ := s.ReadField("h-ff01", "last_response"); ok {
		t.Fatal("side file reported for an untruncated field")
	}
	if _, _, err := s.ReadField("h-ff01", "../x"); err == nil {
		t.Fatal("ReadField accepted an unknown field")
	}

	// Reload keeps the marked text and does not overwrite the side file with it.
	s2, err := NewStore(filepath.Join(dir, "sessions.json"), WithFlushInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	s2.SetFieldCap(1024)
	got, _ := s2.Get("h-ff01")
	_ = s2.Save(got)
	if err := s2.Flush(); err != nil {
		t.Fatal(err)
	}
	if full, _, _ := s2.ReadField("h-ff01", "task"); full != big {
		t.Fatalf("side file overwritten by the truncated text (%d bytes)", len(full))
	}
}

func TestStoreFieldCapDisabledAndEncrypted(t *testing.T) {
	s, dir := archStore(t, archKey())
	s.SetFieldCap(100)
	sess := archSess("gg01", StateRunning, 0)
	sess.Task = strings.Repeat("secret", 100)
	_ = s.Save(sess)
	raw, _ := os.ReadFile(filepath.Join(dir, "sessions", "h-gg01", "fields", "task.txt"))
	if strings.Contains(string(raw), "secret") {
		t.Fatal("side file is plaintext in an encrypted store")
	}
	if full, ok, err := s.ReadField("h-gg01", "task"); err != nil || !ok || full != sess.Task {
		t.Fatalf("encrypted side file round trip: ok=%v err=%v", ok, err)
	}
	s.SetFieldCap(0)
	sess2 := archSess("gg02", StateRunning, 0)
	sess2.Task = strings.Repeat("x", 5000)
	_ = s.Save(sess2)
	if d, _ := s.readRecord(s.recordPath("h-gg02")); d.Task != sess2.Task {
		t.Fatal("task truncated while the cap is disabled")
	}
}

func TestStoreArchiveConcurrent(t *testing.T) {
	s, _ := archStore(t, nil)
	for i := 0; i < 60; i++ {
		_ = s.Save(archSess(fmt.Sprintf("%04x", i), StateComplete, 30*day))
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				id := fmt.Sprintf("%04x", (g*15+i)%60)
				switch i % 5 {
				case 0:
					_, _ = s.ArchiveOnce(time.Now())
				case 1:
					_, _ = s.RestoreFromArchive(id)
				case 2:
					_, _, _ = s.ListArchived("name", 0, 10)
				case 3:
					_, _ = s.GetArchived(id)
				default:
					_ = s.Save(archSess(fmt.Sprintf("n%d-%d", g, i), StateRunning, 0))
					_ = s.List()
				}
			}
		}(g)
	}
	wg.Wait()
	// Every session is in exactly one place.
	seen := map[string]int{}
	for _, sess := range s.List() {
		if strings.HasPrefix(sess.ID, "n") && len(sess.ID) < 4 {
			continue
		}
		seen[sess.FullID]++
	}
	arch, _, _ := s.ListArchived("", 0, 0)
	for _, sess := range arch {
		seen[sess.FullID]++
	}
	for i := 0; i < 60; i++ {
		if c := seen[fmt.Sprintf("h-%04x", i)]; c != 1 {
			t.Fatalf("session %04x is in %d places", i, c)
		}
	}
}
