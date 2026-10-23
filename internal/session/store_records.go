package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// Per-session record layout. Instead of one sessions.json holding every
// session, the store keeps one file per session in a sibling directory:
//
//	<data_dir>/sessions.d/LAYOUT            layout marker (written last when migrating)
//	<data_dir>/sessions.d/p.<fullID>.rec    one session, compact JSON
//	<data_dir>/sessions.d/x.<sha256>.rec    same, for IDs with unsafe or overlong names
//
// Each record is written on its own (temp file plus rename through secfile)
// and, when the store has a key, encrypted on its own. The background writer
// persists only the sessions marked dirty since its last cycle and removes the
// records of deleted ones, so the cost of a write follows the size of the
// change, not the size of the store.
//
// The directory is deliberately not called "sessions": <data_dir>/sessions/<id>/
// already holds per-session working data that mergeFromSessionDirs reads.
//
// A store whose directory has no LAYOUT marker and whose sessions.json exists
// is migrated once at open (see migrateToRecords). If migration cannot
// complete, the store keeps reading and writing the old whole-file format.
// See docs/plans/2026-10-23-session-store-write-amplification.md.
const (
	recordsDirName         = "sessions.d"
	recordsLayoutFile      = "LAYOUT"
	recordsLayoutTag       = "datawatch-session-records v1\n"
	recordExt              = ".rec"
	preRecordsBackupSuffix = ".pre-records.bak"
	staleTmpAge            = time.Minute
)

// recordFileName maps a FullID to a record file name. IDs made only of safe
// characters stay readable; anything else is named by its SHA-256 so a hostile
// or odd hostname can never escape the directory or collide. The ID itself
// is read back from the record, never from the file name.
func recordFileName(fullID string) string {
	safe := fullID != "" && len(fullID) <= 120
	for i := 0; safe && i < len(fullID); i++ {
		c := fullID[i]
		safe = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
	}
	if safe {
		return "p." + fullID + recordExt
	}
	sum := sha256.Sum256([]byte(fullID))
	return "x." + hex.EncodeToString(sum[:]) + recordExt
}

func (s *Store) recordsDir() string { return filepath.Join(filepath.Dir(s.path), recordsDirName) }

func (s *Store) recordPath(fullID string) string {
	return filepath.Join(s.recordsDir(), recordFileName(fullID))
}

func recordsLayoutPresent(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, recordsLayoutFile))
	return err == nil
}

// sessionSum is the change-detection hash of a session's compact JSON.
func sessionSum(data []byte) [32]byte { return sha256.Sum256(data) }

// markRecordDirtyLocked records that fullID must be rewritten. mu held.
func (s *Store) markRecordDirtyLocked(fullID string) {
	if !s.records {
		return
	}
	delete(s.deletedIDs, fullID)
	s.dirtyIDs[fullID] = struct{}{}
}

// markRecordDeletedLocked records that fullID's record must be removed. mu held.
func (s *Store) markRecordDeletedLocked(fullID string) {
	if !s.records {
		return
	}
	delete(s.dirtyIDs, fullID)
	s.deletedIDs[fullID] = struct{}{}
}

// readRecord loads one record file.
func (s *Store) readRecord(path string) (*Session, error) {
	data, err := secfile.ReadFile(path, s.encKey)
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// loadRecords reads every record in the directory. A record that cannot be
// read or parsed is skipped with a warning (mergeFromSessionDirs may still
// recover it); its count is returned. Leftover temp files from a crashed
// write older than staleTmpAge are removed.
func (s *Store) loadRecords() (map[string]*Session, int, error) {
	dir := s.recordsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	out := make(map[string]*Session, len(entries))
	skipped := 0
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if strings.HasSuffix(name, recordExt+".tmp") {
			if info, err := ent.Info(); err == nil && time.Since(info.ModTime()) > staleTmpAge {
				_ = os.Remove(filepath.Join(dir, name))
			}
			continue
		}
		if !strings.HasSuffix(name, recordExt) {
			continue
		}
		sess, err := s.readRecord(filepath.Join(dir, name))
		if err != nil {
			skipped++
			fmt.Fprintf(os.Stderr, "[warn] sessions store: skipping record %s: %v\n", name, err)
			continue
		}
		out[sess.FullID] = sess
	}
	return out, skipped, nil
}

// initWritten seeds the change-detection hashes from the in-memory sessions,
// so the first scan does not rewrite records that are already on disk.
func (s *Store) initWritten() {
	s.written = make(map[string][32]byte, len(s.sessions))
	for id, sess := range s.sessions {
		if data, err := json.Marshal(sess); err == nil {
			s.written[id] = sessionSum(data)
		}
	}
}

// openRecords loads the store from an existing record layout.
func (s *Store) openRecords() error {
	loaded, _, err := s.loadRecords()
	if err != nil {
		return fmt.Errorf("read records %s: %w", s.recordsDir(), err)
	}
	s.sessions = loaded
	s.records = true
	s.layoutReady = true
	s.initWritten()
	return nil
}

// ensureLayout creates the records directory and the LAYOUT marker. Call with
// writeMu held.
func (s *Store) ensureLayout() error {
	if s.layoutReady {
		return nil
	}
	dir := s.recordsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create records dir: %w", err)
	}
	if !recordsLayoutPresent(dir) {
		if err := secfile.WriteFile(filepath.Join(dir, recordsLayoutFile), []byte(recordsLayoutTag), 0644, nil); err != nil {
			return fmt.Errorf("write layout marker: %w", err)
		}
		syncDir(dir)
	}
	s.layoutReady = true
	return nil
}

// syncDir fsyncs a directory so renames and removals in it are durable.
// Best effort: not every filesystem supports it.
func syncDir(dir string) {
	if f, err := os.Open(dir); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
}

// writeRecord writes one record atomically (secfile: temp file, fsync, rename).
func (s *Store) writeRecord(fullID string, data []byte) error {
	if err := secfile.WriteFile(s.recordPath(fullID), data, 0644, s.encKey); err != nil {
		return fmt.Errorf("write record %s: %w", fullID, err)
	}
	s.bytesWritten.Add(int64(len(data)))
	s.recordWrites.Add(1)
	return nil
}

// backupLegacyFile copies the old sessions.json to <path>.pre-records.bak and
// returns the backup path. An existing backup is never overwritten: a later
// copy gets a numeric suffix. The copy is verified against the source.
func backupLegacyFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	base := path + preRecordsBackupSuffix
	for n := 0; n < 1000; n++ {
		dst := base
		if n > 0 {
			dst = fmt.Sprintf("%s.%d", base, n)
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(raw)
		if werr == nil {
			werr = f.Sync()
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(dst)
			return "", werr
		}
		if got, err := os.ReadFile(dst); err != nil || len(got) != len(raw) {
			_ = os.Remove(dst)
			return "", fmt.Errorf("backup %s did not verify", dst)
		}
		return dst, nil
	}
	return "", fmt.Errorf("no free backup name for %s", base)
}

// migrateToRecords converts the legacy sessions.json (already loaded into
// s.sessions through the current reader, including corruption recovery and
// the per-session directory merge) to the record layout:
//
//  1. take the cross-process flock;
//  2. copy sessions.json to sessions.json.pre-records.bak;
//  3. write every session as a record;
//  4. reload the records and require the same set of sessions;
//  5. write the LAYOUT marker (the switch) and remove sessions.json.
//
// Any failure before step 5 leaves sessions.json untouched and the store in
// the legacy format; the next start tries again. If another process finished
// the migration while this one waited for the lock, its records are used.
func (s *Store) migrateToRecords() error {
	lockFile, err := flockAcquire(s.lockPath)
	if err != nil {
		return fmt.Errorf("acquire sessions lock: %w", err)
	}
	if lockFile == nil {
		return fmt.Errorf("sessions lock timeout")
	}
	defer flockRelease(lockFile)

	if recordsLayoutPresent(s.recordsDir()) {
		return s.openRecords()
	}
	if _, err := os.Stat(s.path); err != nil {
		return fmt.Errorf("legacy store vanished: %w", err)
	}
	backup, err := backupLegacyFile(s.path)
	if err != nil {
		return fmt.Errorf("backup %s: %w", s.path, err)
	}
	if err := os.MkdirAll(s.recordsDir(), 0755); err != nil {
		return fmt.Errorf("create records dir: %w", err)
	}
	sums := make(map[string][32]byte, len(s.sessions))
	for id, sess := range s.sessions {
		data, err := json.Marshal(sess)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", id, err)
		}
		if err := s.writeRecord(id, data); err != nil {
			return err
		}
		sums[id] = sessionSum(data)
	}
	syncDir(s.recordsDir())
	loaded, skipped, err := s.loadRecords()
	if err != nil {
		return fmt.Errorf("verify records: %w", err)
	}
	if skipped != 0 || len(loaded) != len(s.sessions) {
		return fmt.Errorf("verify records: loaded %d of %d sessions (%d unreadable)", len(loaded), len(s.sessions), skipped)
	}
	for id := range s.sessions {
		if _, ok := loaded[id]; !ok {
			return fmt.Errorf("verify records: session %q missing", id)
		}
	}
	s.records = true
	s.layoutReady = false
	if err := s.ensureLayout(); err != nil {
		s.records = false
		return err
	}
	s.written = sums
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[warn] sessions store: could not remove migrated %s: %v\n", s.path, err)
	}
	fmt.Fprintf(os.Stderr, "[migrate] sessions store: %d session(s) moved to per-session records in %s; old file kept as %s\n",
		len(s.sessions), s.recordsDir(), backup)
	return nil
}

// flushRecords persists the sessions marked dirty and removes the records of
// deleted ones. force (Flush) also scans every session against its last
// written hash, so a change made through Mutate with no Save is still found.
// The marshal happens under the read lock; file writes and the flock happen
// outside the store lock.
func (s *Store) flushRecords(force bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if !s.dirty.Swap(false) && !force {
		return nil
	}

	s.mu.Lock()
	dirtyIDs, delIDs, scan := s.dirtyIDs, s.deletedIDs, s.scan || force
	s.dirtyIDs, s.deletedIDs, s.scan = map[string]struct{}{}, map[string]struct{}{}, false
	s.mu.Unlock()

	type put struct {
		id   string
		data []byte
		sum  [32]byte
	}
	var puts []put
	var dels []string

	s.mu.RLock()
	var merr error
	collect := func(id string, sess *Session) {
		data, err := json.Marshal(sess)
		if err != nil {
			merr = fmt.Errorf("marshal session %s: %w", id, err)
			return
		}
		sum := sessionSum(data)
		if old, ok := s.written[id]; ok && old == sum {
			return
		}
		puts = append(puts, put{id, data, sum})
	}
	for id := range dirtyIDs {
		if sess, ok := s.sessions[id]; ok {
			collect(id, sess)
		} else {
			dels = append(dels, id)
		}
	}
	for id := range delIDs {
		if _, ok := s.sessions[id]; !ok {
			dels = append(dels, id)
		}
	}
	if scan {
		for id, sess := range s.sessions {
			if _, done := dirtyIDs[id]; !done {
				collect(id, sess)
			}
		}
		for id := range s.written {
			if _, ok := s.sessions[id]; !ok {
				dels = append(dels, id)
			}
		}
	}
	s.mu.RUnlock()

	requeue := func(puts []put, dels []string) {
		s.mu.Lock()
		for _, p := range puts {
			s.dirtyIDs[p.id] = struct{}{}
		}
		for _, id := range dels {
			if _, back := s.sessions[id]; !back {
				s.deletedIDs[id] = struct{}{}
			}
		}
		s.mu.Unlock()
		s.dirty.Store(true)
	}
	if merr != nil {
		requeue(puts, dels)
		return merr
	}
	if len(puts) == 0 && len(dels) == 0 {
		if force {
			return s.ensureLayoutLocked()
		}
		return nil
	}

	lockFile, lockErr := flockAcquire(s.lockPath)
	if lockErr != nil {
		requeue(puts, dels)
		return fmt.Errorf("acquire sessions lock: %w", lockErr)
	}
	if lockFile == nil { // timeout: keep everything queued, retry next cycle
		requeue(puts, dels)
		return nil
	}
	defer flockRelease(lockFile)

	if err := s.ensureLayout(); err != nil {
		requeue(puts, dels)
		return err
	}
	for i, p := range puts {
		if err := s.writeRecord(p.id, p.data); err != nil {
			requeue(puts[i:], dels)
			return err
		}
		s.written[p.id] = p.sum
	}
	for i, id := range dels {
		if err := os.Remove(s.recordPath(id)); err != nil && !os.IsNotExist(err) {
			requeue(nil, dels[i:])
			return fmt.Errorf("remove record %s: %w", id, err)
		}
		delete(s.written, id)
	}
	syncDir(s.recordsDir())
	s.writes.Add(1)
	return nil
}

// ensureLayoutLocked takes the flock around ensureLayout for a forced flush
// that found nothing to write (an empty new store still gets its marker).
func (s *Store) ensureLayoutLocked() error {
	if s.layoutReady {
		return nil
	}
	lockFile, err := flockAcquire(s.lockPath)
	if err != nil || lockFile == nil {
		return err
	}
	defer flockRelease(lockFile)
	return s.ensureLayout()
}
