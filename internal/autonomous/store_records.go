// Per-PRD record storage for the autonomous Store.
//
// prds.jsonl used to be rewritten in full whenever any task changed, and one
// PRD can be 13 MB. The record layout stores each PRD in its own file and
// rewrites only the PRDs that changed:
//
//	<dir>/prds/LAYOUT                    marker; its presence selects this layout
//	<dir>/prds/<id>.json                 one PRD, compact JSON
//	<dir>/prds/<id>.files/<sha256>.txt   large text of that PRD (side files)
//	<dir>/prds.jsonl                     old format, only read when no layout exists
//	<dir>/prds.jsonl.pre-records.bak     copy of the old file taken at migration
//
// A PRD id that is not made of letters, digits, '-' and '_' is stored as
// x.<sha256 of id> instead. The id is always read from the record.
//
// A string field above the cap (default 64 KB), and a field named like a
// bulky output (RawJudge and similar) above a smaller floor, is written to a
// side file named after the SHA-256 of its text. The record holds a reference
// string in its place, so the PRD struct and every API shape are unchanged:
// the reference is resolved when the record is read. Side files are content
// addressed, so text that did not change is never rewritten, and files no
// longer referenced are removed after the record is written.
//
// Every file is written to a temp file, fsynced and renamed, so a crash
// leaves each file at its old or new generation. Side files are written
// before the record that references them.

package autonomous

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	prdRecordsDirName = "prds"
	prdLayoutFile     = "LAYOUT"
	prdLayoutBody     = "prd-records-v1\n"
	prdLegacyFile     = "prds.jsonl"
	prdBackupSuffix   = ".pre-records.bak"

	// DefaultSideFileCap is the string length above which a field moves to a
	// side file.
	DefaultSideFileCap = 64 << 10
	// DefaultSideFileMax is the hard limit on one side file; longer text is
	// cut and ends with a truncation marker.
	DefaultSideFileMax = 32 << 20

	// heavyFieldFloor is the smaller threshold for fields known to carry
	// bulky machine output.
	heavyFieldFloor = 4 << 10

	sideRefPrefix = "datawatch-side-file:v1:"

	// staleTempFileAge is how old a '.tmp-*' file in the records directory
	// must be before opening the store removes it. A temp file younger than
	// this may belong to a write still in progress in another Store or
	// process (removing it would make its rename fail and its save lost);
	// an older one is a leftover of an interrupted write.
	staleTempFileAge = 10 * time.Minute
)

// heavyFieldNames are struct field names that hold outputs, checkpoints or
// verifier text; they move to side files above heavyFieldFloor.
var heavyFieldNames = map[string]bool{
	"RawJudge":   true,
	"Output":     true,
	"Checkpoint": true,
	"Transcript": true,
	"Error":      true,
	"Spec":       true,
}

// prdRecords is the record-layout state held by Store (guarded by Store.mu).
type prdRecords struct {
	active   bool
	sideCap  int
	sideMax  int
	dirty    map[string]struct{}
	deleted  map[string]struct{}
	allDirty bool
	hashes   map[string][sha256.Size]byte

	bytesWritten int64 // record and side-file bytes written since open
	fileWrites   int64
	truncated    int64
}

func newPRDRecords() prdRecords {
	return prdRecords{
		sideCap: DefaultSideFileCap,
		sideMax: DefaultSideFileMax,
		dirty:   map[string]struct{}{},
		deleted: map[string]struct{}{},
		hashes:  map[string][sha256.Size]byte{},
	}
}

// SetSideFileCap sets the length above which a string field is stored in a
// side file. n <= 0 restores the default. Existing records are rewritten with
// the new cap the next time their PRD changes.
func (s *Store) SetSideFileCap(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 {
		n = DefaultSideFileCap
	}
	s.rec.sideCap = n
}

// SetSideFileMax sets the hard limit on one side file. n <= 0 restores the
// default. Text over the limit is cut and ends with a marker.
func (s *Store) SetSideFileMax(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 {
		n = DefaultSideFileMax
	}
	s.rec.sideMax = n
}

// RecordStats reports record-layout activity since the store opened: whether
// the layout is active, bytes and files written, and how many fields were cut.
func (s *Store) RecordStats() (active bool, bytesWritten, fileWrites, truncated int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec.active, s.rec.bytesWritten, s.rec.fileWrites, s.rec.truncated
}

func (s *Store) recordsDir() string { return filepath.Join(s.dir, prdRecordsDirName) }

// recordName maps a PRD id to the base name of its record files.
func recordName(id string) string {
	ok := id != "" && len(id) <= 64
	for i := 0; ok && i < len(id); i++ {
		c := id[i]
		ok = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
	}
	if ok {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return "x." + hex.EncodeToString(sum[:])
}

// markPRDDirtyLocked flags one PRD for rewrite. An id the store does not
// hold (a task whose PRD id is empty or stale) flags a scan of every PRD,
// which writes only the ones whose record changed.
func (s *Store) markPRDDirtyLocked(id string) {
	if _, ok := s.prds[id]; !ok {
		s.rec.allDirty = true
		return
	}
	s.rec.dirty[id] = struct{}{}
}

// persistPRD marks the given PRDs dirty and writes them. Callers hold s.mu.
func (s *Store) persistPRD(ids ...string) error {
	for _, id := range ids {
		s.markPRDDirtyLocked(id)
	}
	return s.flushPRDsLocked()
}

// persistDeleted removes the records of deleted PRDs. Callers hold s.mu.
func (s *Store) persistDeleted(ids ...string) error {
	for _, id := range ids {
		delete(s.rec.dirty, id)
		delete(s.rec.hashes, id)
		s.rec.deleted[id] = struct{}{}
	}
	return s.flushPRDsLocked()
}

// flushPRDsLocked writes every dirty PRD and removes deleted ones. A PRD
// whose write fails stays dirty and is retried by the next call.
func (s *Store) flushPRDsLocked() error {
	if !s.rec.active {
		// Old whole-file format (migration failed or not yet possible).
		s.rec.dirty = map[string]struct{}{}
		s.rec.deleted = map[string]struct{}{}
		s.rec.allDirty = false
		return writeJSONL(filepath.Join(s.dir, prdLegacyFile), len(s.prds), func(emit func(any) error) error {
			for _, p := range s.prds {
				if err := emit(p); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if s.rec.allDirty {
		for id := range s.prds {
			s.rec.dirty[id] = struct{}{}
		}
		s.rec.allDirty = false
	}
	var firstErr error
	for id := range s.rec.deleted {
		if _, live := s.prds[id]; live {
			delete(s.rec.deleted, id)
			continue
		}
		if err := s.removePRDFiles(id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delete(s.rec.deleted, id)
	}
	for id := range s.rec.dirty {
		p, ok := s.prds[id]
		if !ok {
			delete(s.rec.dirty, id)
			continue
		}
		if err := s.writePRDRecord(p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delete(s.rec.dirty, id)
	}
	return firstErr
}

func (s *Store) removePRDFiles(id string) error {
	delete(s.rec.hashes, id)
	return s.removeRecordFiles(recordName(id))
}

func (s *Store) removeRecordFiles(name string) error {
	dir := s.recordsDir()
	// name itself must be one safe element: ".files" and ".json" alone are
	// valid elements, so an empty name would pass the checks below.
	if !validPathElement(name) {
		return fmt.Errorf("%w: %q", errBadPathElement, name)
	}
	filesDir, err := containedElement(dir, name+".files")
	if err != nil {
		return err
	}
	recFile, err := containedElement(dir, name+".json")
	if err != nil {
		return err
	}
	if err := os.RemoveAll(filesDir); err != nil {
		return err
	}
	if err := os.Remove(recFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// writePRDRecord writes one PRD: side files first, then the record, then
// removal of side files the record no longer references. A PRD whose record
// bytes equal the last write is skipped.
func (s *Store) writePRDRecord(p *PRD) error {
	rec, sides, truncated, err := s.encodePRDRecord(p)
	if err != nil {
		return fmt.Errorf("encode prd %s: %w", p.ID, err)
	}
	sum := sha256.Sum256(rec)
	if prev, ok := s.rec.hashes[p.ID]; ok && prev == sum {
		return nil
	}
	name := recordName(p.ID)
	dir := s.recordsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	filesDir := filepath.Join(dir, name+".files")
	if len(sides) > 0 {
		if err := os.MkdirAll(filesDir, 0o755); err != nil {
			return err
		}
		for fname, body := range sides {
			path := filepath.Join(filesDir, fname)
			if st, err := os.Stat(path); err == nil && st.Size() == int64(len(body)) {
				continue // content addressed: same name, same text
			}
			if err := atomicWriteFile(path, body); err != nil {
				return err
			}
			s.rec.bytesWritten += int64(len(body))
			s.rec.fileWrites++
		}
	}
	if err := atomicWriteFile(filepath.Join(dir, name+".json"), rec); err != nil {
		return err
	}
	s.rec.bytesWritten += int64(len(rec))
	s.rec.fileWrites++
	s.rec.truncated += int64(truncated)
	s.rec.hashes[p.ID] = sum
	// Drop side files this record no longer references.
	if entries, err := os.ReadDir(filesDir); err == nil {
		for _, e := range entries {
			if _, keep := sides[e.Name()]; !keep {
				_ = os.Remove(filepath.Join(filesDir, e.Name()))
			}
		}
		if len(sides) == 0 {
			_ = os.Remove(filesDir)
		}
	}
	return nil
}

// encodePRDRecord marshals a deep copy of p with large strings replaced by
// side-file references. It returns the record, the side files keyed by file
// name, and how many fields were cut at the hard limit.
func (s *Store) encodePRDRecord(p *PRD) ([]byte, map[string][]byte, int, error) {
	cp := deepCopyPRD(p)
	name := recordName(p.ID)
	sides := map[string][]byte{}
	truncated := 0
	walkStrings(reflect.ValueOf(cp), "", func(field, str string) (string, bool) {
		heavy := heavyFieldNames[field] && len(str) > heavyFieldFloor
		if len(str) <= s.rec.sideCap && !heavy && !strings.HasPrefix(str, sideRefPrefix) {
			return "", false
		}
		body := str
		if len(body) > s.rec.sideMax {
			body = truncateText(body, s.rec.sideMax)
			truncated++
		}
		sum := sha256.Sum256([]byte(body))
		fname := hex.EncodeToString(sum[:]) + ".txt"
		sides[fname] = []byte(body)
		return sideRefPrefix + name + ".files/" + fname, true
	})
	rec, err := json.Marshal(cp)
	return rec, sides, truncated, err
}

// truncateText cuts str so the result, marker included, is at most max bytes
// and ends on a rune boundary. The marker states how much was dropped. Text
// already within max is never touched, so a reloaded cut text is stable.
func truncateText(str string, max int) string {
	marker := func(dropped int) string {
		return "\n[datawatch: truncated, " + strconv.Itoa(dropped) + " bytes dropped at the " + strconv.Itoa(max) + " byte limit]"
	}
	cut := max - len(marker(len(str)))
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(str[cut]) {
		cut--
	}
	return str[:cut] + marker(len(str)-cut)
}

// resolveSideRefs replaces every side-file reference in p with the text it
// points at. A missing or unreadable file leaves a visible marker instead of
// failing the load.
func (s *Store) resolveSideRefs(p *PRD) {
	dir := s.recordsDir()
	walkStrings(reflect.ValueOf(p), "", func(_, str string) (string, bool) {
		if !strings.HasPrefix(str, sideRefPrefix) {
			return "", false
		}
		rel := strings.TrimPrefix(str, sideRefPrefix)
		clean := filepath.Clean(filepath.FromSlash(rel))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "[datawatch: invalid side file reference]", true
		}
		body, err := os.ReadFile(filepath.Join(dir, clean))
		if err != nil {
			log.Printf("[autonomous] prd %s: side file %s: %v", p.ID, rel, err)
			return "[datawatch: side file missing: " + rel + "]", true
		}
		return string(body), true
	})
}

// walkStrings visits every settable string reachable from v, passing the name
// of the struct field that holds it, and replaces it when fn reports true.
func walkStrings(v reflect.Value, field string, fn func(field, str string) (string, bool)) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			if repl, ok := fn(field, v.String()); ok {
				v.SetString(repl)
			}
		}
	case reflect.Pointer:
		if !v.IsNil() {
			walkStrings(v.Elem(), field, fn)
		}
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		inner := reflect.New(v.Elem().Type()).Elem()
		inner.Set(v.Elem())
		walkStrings(inner, field, fn)
		if v.CanSet() {
			v.Set(inner)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if !t.Field(i).IsExported() {
				continue
			}
			walkStrings(v.Field(i), t.Field(i).Name, fn)
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return
		}
		for i := 0; i < v.Len(); i++ {
			walkStrings(v.Index(i), field, fn)
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		for _, k := range v.MapKeys() {
			elem := reflect.New(v.Type().Elem()).Elem()
			elem.Set(v.MapIndex(k))
			walkStrings(elem, field, fn)
			v.SetMapIndex(k, elem)
		}
	}
}

// atomicWriteFile writes data to a temp file in the same directory, fsyncs it
// and renames it over path.
func atomicWriteFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// adoptPRD normalizes a decoded PRD and registers it with the id maps.
func (s *Store) adoptPRD(p *PRD) {
	p.Status = NormalizePRDStatus(p.Status)
	migrateGuidedMode(p) // BL406 Phase 5 — B111's real fix
	s.prds[p.ID] = p
	for i := range p.Story {
		st := &p.Story[i]
		s.stories[st.ID] = st
		for j := range st.Tasks {
			s.tasks[st.Tasks[j].ID] = &st.Tasks[j]
		}
	}
}

// loadPRDs fills s.prds from the record layout or the old prds.jsonl and
// migrates the old file on first start.
func (s *Store) loadPRDs() error {
	legacyPath := filepath.Join(s.dir, prdLegacyFile)
	_, layoutErr := os.Stat(filepath.Join(s.recordsDir(), prdLayoutFile))
	haveLayout := layoutErr == nil
	_, legacyErr := os.Stat(legacyPath)
	haveLegacy := legacyErr == nil

	if haveLayout {
		s.rec.active = true
		if err := s.loadRecords(); err != nil {
			return err
		}
		if haveLegacy {
			return s.importLegacyIntoRecords(legacyPath)
		}
		return nil
	}
	if err := s.loadLegacyFile(legacyPath, false); err != nil {
		return err
	}
	if !haveLegacy {
		// New data directory: start in the record layout.
		return s.activateLayout()
	}
	if err := s.migrateToRecords(legacyPath); err != nil {
		// Stay on the old format; the next start retries.
		log.Printf("[autonomous] prd record migration failed, staying on %s: %v", prdLegacyFile, err)
		s.rec.active = false
	}
	return nil
}

// loadLegacyFile reads prds.jsonl. With onlyMissing, ids already loaded are
// skipped and the ids it added are returned through s.rec.dirty.
func (s *Store) loadLegacyFile(path string, onlyMissing bool) error {
	return loadJSONL(path, func(line []byte) error {
		var p PRD
		if err := json.Unmarshal(line, &p); err != nil {
			return err
		}
		if onlyMissing {
			if _, ok := s.prds[p.ID]; ok {
				return nil
			}
			s.rec.dirty[p.ID] = struct{}{}
		}
		s.adoptPRD(&p)
		return nil
	})
}

func (s *Store) loadRecords() error {
	dir := s.recordsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".tmp-") {
			// Age check: a fresh temp file may belong to a write in progress
			// in another Store or process; only old ones may go.
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleTempFileAge {
				_ = os.Remove(filepath.Join(dir, n)) // leftover of an interrupted write
			}
			continue
		}
		if e.IsDir() || !strings.HasSuffix(n, ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return err
		}
		var p PRD
		if err := json.Unmarshal(data, &p); err != nil {
			log.Printf("[autonomous] skipping unreadable prd record %s: %v", n, err)
			continue
		}
		sum := sha256.Sum256(data)
		s.resolveSideRefs(&p)
		s.rec.hashes[p.ID] = sum
		s.adoptPRD(&p)
	}
	return nil
}

// importLegacyIntoRecords handles a prds.jsonl that sits next to a record
// layout: PRDs only the old file holds are imported, records win for the
// rest, and the old file is renamed so it is not read again.
func (s *Store) importLegacyIntoRecords(path string) error {
	if err := s.loadLegacyFile(path, true); err != nil {
		return err
	}
	if err := s.flushPRDsLocked(); err != nil {
		return err
	}
	return os.Rename(path, uniqueName(path+".imported"))
}

// activateLayout creates the record directory and marker.
func (s *Store) activateLayout() error {
	if err := os.MkdirAll(s.recordsDir(), 0o755); err != nil {
		return err
	}
	if err := atomicWriteFile(filepath.Join(s.recordsDir(), prdLayoutFile), []byte(prdLayoutBody)); err != nil {
		return err
	}
	s.rec.active = true
	return nil
}

// uniqueName returns path, or path.1, path.2, ... for the first name that
// does not exist, so nothing is ever overwritten.
func uniqueName(path string) string {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return path
	}
	for i := 1; ; i++ {
		cand := path + "." + strconv.Itoa(i)
		if _, err := os.Lstat(cand); os.IsNotExist(err) {
			return cand
		}
	}
}

// migrateToRecords converts the PRDs loaded from prds.jsonl: back up the old
// file, write every record, read them back and require the same PRDs, write
// the marker, then remove prds.jsonl. Any failure before the marker leaves
// prds.jsonl untouched.
func (s *Store) migrateToRecords(legacyPath string) error {
	if err := copyFileExcl(legacyPath, uniqueName(legacyPath+prdBackupSuffix)); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	s.rec.active = true
	s.rec.hashes = map[string][sha256.Size]byte{}
	want := map[string][]byte{}
	for id, p := range s.prds {
		if err := s.writePRDRecord(p); err != nil {
			return fmt.Errorf("write prd %s: %w", id, err)
		}
		rec, _, truncated, err := s.encodePRDRecord(p)
		if err != nil {
			return err
		}
		if truncated == 0 {
			want[id] = rec
		}
	}
	// Records of PRDs the old file does not hold (a half-finished earlier
	// attempt) must not survive into the layout.
	dir := s.recordsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		have[strings.TrimSuffix(e.Name(), ".json")] = true
	}
	expect := map[string]bool{}
	for id := range s.prds {
		expect[recordName(id)] = true
	}
	for n := range have {
		if !expect[n] {
			if err := s.removeRecordFiles(n); err != nil {
				return err
			}
		}
	}
	// Read everything back and compare with what was written.
	check := &Store{dir: s.dir, prds: map[string]*PRD{}, stories: map[string]*Story{}, tasks: map[string]*Task{}, rec: newPRDRecords()}
	check.rec.sideCap, check.rec.sideMax = s.rec.sideCap, s.rec.sideMax
	if err := check.loadRecords(); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if len(check.prds) != len(s.prds) {
		return fmt.Errorf("verify: %d records for %d PRDs", len(check.prds), len(s.prds))
	}
	for id, rec := range want {
		got, ok := check.prds[id]
		if !ok {
			return fmt.Errorf("verify: prd %s missing from records", id)
		}
		back, _, _, err := check.encodePRDRecord(got)
		if err != nil {
			return err
		}
		if !bytes.Equal(back, rec) {
			return fmt.Errorf("verify: prd %s differs after round trip", id)
		}
	}
	if err := atomicWriteFile(filepath.Join(dir, prdLayoutFile), []byte(prdLayoutBody)); err != nil {
		return err
	}
	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// copyFileExcl copies src to dst, failing if dst exists.
func copyFileExcl(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// recordIDs lists the PRD ids that have a record file (for tests and
// diagnostics), sorted.
func (s *Store) recordIDs() ([]string, error) {
	entries, err := os.ReadDir(s.recordsDir())
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	sort.Strings(ids)
	return ids, nil
}
