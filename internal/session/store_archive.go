package session

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// Session archive. Terminal sessions that have not been updated for
// archiveAfter move out of the active store into an append-only file:
//
//	<data_dir>/sessions-archive.jsonl
//
// One JSON line per event. A session line is {"id","ts","rec"}: id and ts
// (updated_at in unix nanoseconds) stay readable so the file can be indexed
// and paged without decrypting anything; rec is the session record, a JSON
// object, or a JSON string holding the secfile ciphertext when the store has a
// key. A restore appends a tombstone {"id","del":true}. The file is never
// rewritten and nothing in it is ever deleted.
//
// The active store keeps only an index of id -> file offset for the archive,
// never the sessions themselves. Reads fetch one line by offset; text search
// streams the lines.
//
// The same file also carries the per-field size cap: a string field longer than
// the cap is written truncated with a marker, and the full text goes to
// <data_dir>/sessions/<id>/fields/<field>.txt (see marshalCapped).
const (
	archiveFileName = "sessions-archive.jsonl"

	// DefaultArchiveAfter is how long a terminal session may sit unchanged in
	// the active store before it is archived.
	DefaultArchiveAfter = 14 * 24 * time.Hour
	// DefaultFieldCap is the largest string field persisted inline, in bytes.
	DefaultFieldCap = 256 * 1024
	// DefaultArchiveInterval is how often the background pass runs.
	DefaultArchiveInterval = time.Hour

	fieldsDirName = "fields"
)

// archiveLine is one line of the archive file.
type archiveLine struct {
	ID  string          `json:"id"`
	TS  int64           `json:"ts,omitempty"`
	Del bool            `json:"del,omitempty"`
	Rec json.RawMessage `json:"rec,omitempty"`
}

// archiveRef locates a live (not restored) session line in the archive file.
type archiveRef struct {
	off int64
	n   int
	ts  int64
}

// archiveState is the archive part of Store.
type archiveState struct {
	mu      sync.Mutex // guards everything below; taken before Store.mu, never after
	loaded  bool
	idx     map[string]archiveRef
	size    int64
	after   atomic.Int64 // time.Duration; atomic: read while other locks are held
	fieldCp atomic.Int64
	started bool
	passMu  sync.Mutex // one archive pass at a time
	wg      sync.WaitGroup
}

func (s *Store) archivePath() string { return filepath.Join(filepath.Dir(s.path), archiveFileName) }

// SetArchiveAfter sets how old (since the last update) a terminal session must
// be to be archived. Zero or negative disables archiving.
func (s *Store) SetArchiveAfter(d time.Duration) {
	s.arc.after.Store(int64(d))
}

// ArchiveAfter returns the current archive age.
func (s *Store) ArchiveAfter() time.Duration {
	return time.Duration(s.arc.after.Load())
}

// SetFieldCap sets the largest string field, in bytes, that is persisted
// inline. Longer fields are truncated with a marker and the full text goes to a
// side file. Zero or negative disables the cap. It applies to later writes.
func (s *Store) SetFieldCap(n int) {
	s.arc.fieldCp.Store(int64(n))
}

// FieldCap returns the current per-field size cap in bytes.
func (s *Store) FieldCap() int {
	return int(s.arc.fieldCp.Load())
}

// loadArchiveIndexLocked builds the id -> offset index from the file on first
// use. A trailing partial line (a crash mid-append) is ignored and later
// appends start on a fresh line. Call with arc.mu held.
func (s *Store) loadArchiveIndexLocked() error {
	if s.arc.loaded {
		return nil
	}
	idx := map[string]archiveRef{}
	f, err := os.Open(s.archivePath())
	if err != nil {
		if os.IsNotExist(err) {
			s.arc.idx, s.arc.size, s.arc.loaded = idx, 0, true
			return nil
		}
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 1<<20)
	var off int64
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var al archiveLine
			if jerr := json.Unmarshal(bytes.TrimSpace(line), &al); jerr == nil && al.ID != "" {
				if al.Del {
					delete(idx, al.ID)
				} else {
					idx[al.ID] = archiveRef{off: off, n: len(bytes.TrimRight(line, "\r\n")), ts: al.TS}
				}
			}
			off += int64(len(line))
		}
		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("read archive: %w", err)
			}
			break
		}
	}
	s.arc.idx, s.arc.size, s.arc.loaded = idx, off, true
	return nil
}

// appendArchiveLocked appends lines and updates the index. Call with arc.mu held.
func (s *Store) appendArchiveLocked(lines []archiveLine) error {
	if err := s.loadArchiveIndexLocked(); err != nil {
		return err
	}
	f, err := os.OpenFile(s.archivePath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()
	var buf bytes.Buffer
	if s.arc.size > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, s.arc.size-1); err == nil && last[0] != '\n' {
			buf.WriteByte('\n') // a torn final line from a crash
		}
	}
	type pend struct {
		id  string
		ref archiveRef
		del bool
	}
	pends := make([]pend, 0, len(lines))
	base := s.arc.size
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			return err
		}
		pends = append(pends, pend{l.ID, archiveRef{off: base + int64(buf.Len()), n: len(b), ts: l.TS}, l.Del})
		buf.Write(b)
		buf.WriteByte('\n')
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("append archive: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync archive: %w", err)
	}
	s.arc.size = base + int64(buf.Len())
	for _, p := range pends {
		if p.del {
			delete(s.arc.idx, p.id)
		} else {
			s.arc.idx[p.id] = p.ref
		}
	}
	return nil
}

// encodeRec wraps one marshalled session for the archive line.
func (s *Store) encodeRec(data []byte) (json.RawMessage, error) {
	if s.encKey == nil {
		return json.RawMessage(data), nil
	}
	enc, err := secfile.Encrypt(data, s.encKey)
	if err != nil {
		return nil, err
	}
	return json.Marshal(strings.TrimSpace(string(enc)))
}

// decodeRec is the inverse of encodeRec.
func (s *Store) decodeRec(rec json.RawMessage) (*Session, error) {
	data := []byte(rec)
	if len(data) > 0 && data[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return nil, err
		}
		if s.encKey == nil {
			return nil, errors.New("archive record is encrypted and the store has no key")
		}
		plain, err := secfile.Decrypt([]byte(str+"\n"), s.encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt archive record: %w", err)
		}
		data = plain
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// readArchived reads the session at ref. Call with arc.mu held.
func (s *Store) readArchivedLocked(ref archiveRef) (*Session, error) {
	f, err := os.Open(s.archivePath())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, ref.n)
	if _, err := f.ReadAt(buf, ref.off); err != nil {
		return nil, err
	}
	var al archiveLine
	if err := json.Unmarshal(buf, &al); err != nil {
		return nil, err
	}
	return s.decodeRec(al.Rec)
}

// ArchiveOnce moves every terminal session older than the archive age into the
// archive and returns how many moved. Sessions are written to the archive
// first and removed from the active set after, so a crash between the two
// leaves a copy in both (the active one wins) and never loses a session.
func (s *Store) ArchiveOnce(now time.Time) (int, error) {
	after := s.ArchiveAfter()
	if after <= 0 {
		return 0, nil
	}
	s.arc.passMu.Lock()
	defer s.arc.passMu.Unlock()

	type cand struct {
		sess *Session
		upd  time.Time
		st   State
		line archiveLine
		side []sideFile
	}
	var cands []cand
	s.mu.RLock()
	for _, sess := range s.sessions {
		if !sess.State.IsTerminal() || sess.UpdatedAt.IsZero() || now.Sub(sess.UpdatedAt) < after {
			continue
		}
		data, side, err := s.marshalCapped(sess)
		if err != nil {
			s.mu.RUnlock()
			return 0, err
		}
		rec, err := s.encodeRec(data)
		if err != nil {
			s.mu.RUnlock()
			return 0, err
		}
		cands = append(cands, cand{sess: sess, upd: sess.UpdatedAt, st: sess.State,
			line: archiveLine{ID: sess.FullID, TS: sess.UpdatedAt.UnixNano(), Rec: rec}, side: side})
	}
	s.mu.RUnlock()
	if len(cands) == 0 {
		return 0, nil
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].line.ID < cands[j].line.ID })

	lines := make([]archiveLine, 0, len(cands))
	for _, c := range cands {
		if err := s.writeSideFiles(c.side); err != nil {
			return 0, err
		}
		lines = append(lines, c.line)
	}
	s.arc.mu.Lock()
	err := s.appendArchiveLocked(lines)
	s.arc.mu.Unlock()
	if err != nil {
		return 0, err
	}

	// Remove from the active set what is still unchanged; undo the archive
	// line of anything that changed meanwhile.
	var undo []archiveLine
	moved := 0
	s.mu.Lock()
	for _, c := range cands {
		cur, ok := s.sessions[c.line.ID]
		if !ok {
			continue // deleted since; the archive copy stays (nothing is lost)
		}
		if cur != c.sess || !cur.UpdatedAt.Equal(c.upd) || cur.State != c.st {
			undo = append(undo, archiveLine{ID: c.line.ID, Del: true})
			continue
		}
		delete(s.sessions, c.line.ID)
		s.markRecordDeletedLocked(c.line.ID)
		moved++
	}
	inline := false
	if moved > 0 {
		inline = s.markDirtyLocked()
	}
	s.mu.Unlock()
	if len(undo) > 0 {
		s.arc.mu.Lock()
		_ = s.appendArchiveLocked(undo)
		s.arc.mu.Unlock()
	}
	if inline {
		if err := s.flush(false); err != nil {
			return moved, err
		}
	}
	return moved, nil
}

// StartArchiver runs ArchiveOnce now and then every interval until Close.
// Calling it again is a no-op.
func (s *Store) StartArchiver(interval time.Duration) {
	if interval <= 0 {
		interval = DefaultArchiveInterval
	}
	s.arc.mu.Lock()
	if s.arc.started {
		s.arc.mu.Unlock()
		return
	}
	s.arc.started = true
	s.arc.wg.Add(1)
	s.arc.mu.Unlock()
	go func() {
		defer s.arc.wg.Done()
		run := func() {
			if _, err := s.ArchiveOnce(time.Now()); err != nil {
				fmt.Fprintf(os.Stderr, "[warn] sessions archive: %v\n", err)
			}
		}
		run()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				run()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// IsArchived reports whether fullID is in the archive.
func (s *Store) IsArchived(fullID string) bool {
	s.arc.mu.Lock()
	defer s.arc.mu.Unlock()
	if s.loadArchiveIndexLocked() != nil {
		return false
	}
	_, ok := s.arc.idx[fullID]
	return ok
}

// resolveArchivedLocked maps a full or short ID to an archived full ID.
func (s *Store) resolveArchivedLocked(id string) (string, bool) {
	if _, ok := s.arc.idx[id]; ok {
		return id, true
	}
	short := strings.ToLower(id)
	if short == "" || strings.Contains(short, "-") && len(short) > 8 {
		return "", false
	}
	var found string
	for full := range s.arc.idx {
		if strings.HasSuffix(strings.ToLower(full), "-"+short) {
			if found != "" {
				return "", false // ambiguous
			}
			found = full
		}
	}
	return found, found != ""
}

// GetArchived looks up one archived session by full or short ID. A session that
// is also in the active set is not archived: use Get for it.
func (s *Store) GetArchived(id string) (*Session, bool) {
	s.arc.mu.Lock()
	defer s.arc.mu.Unlock()
	if err := s.loadArchiveIndexLocked(); err != nil {
		return nil, false
	}
	full, ok := s.resolveArchivedLocked(id)
	if !ok || s.activeLocked(full) {
		return nil, false
	}
	sess, err := s.readArchivedLocked(s.arc.idx[full])
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] sessions archive: read %s: %v\n", full, err)
		return nil, false
	}
	return sess, true
}

func (s *Store) activeLocked(fullID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.sessions[fullID]
	return ok
}

// GetAny is Get with a fallback to the archive.
func (s *Store) GetAny(id string) (*Session, bool) {
	if sess, ok := s.Get(id); ok {
		return sess, true
	}
	if sess, ok := s.GetByShortID(id); ok {
		return sess, true
	}
	return s.GetArchived(id)
}

// ArchivedQuery selects archived sessions for ListArchivedFiltered.
type ArchivedQuery struct {
	Text   string              // case-insensitive substring of name, id, full id or task; empty = all
	Match  func(*Session) bool // extra filter, nil = none
	Before time.Time           // only sessions updated at or before this time; zero = no limit
	Offset int                 // matches to skip
	Limit  int                 // matches to return; <= 0 = all
}

// ListArchived returns archived sessions, newest update first, that contain
// text (empty = all), skipping offset matches and returning at most limit
// (<= 0 = all). total is the number of matches.
func (s *Store) ListArchived(text string, offset, limit int) (out []*Session, total int, err error) {
	return s.ListArchivedFiltered(ArchivedQuery{Text: text, Offset: offset, Limit: limit})
}

// ListArchivedFiltered is ListArchived with extra selection. Without Text and
// Match only the requested page is read from the file; with either, matching
// reads the candidates in order.
func (s *Store) ListArchivedFiltered(q ArchivedQuery) (out []*Session, total int, err error) {
	s.arc.mu.Lock()
	defer s.arc.mu.Unlock()
	if err := s.loadArchiveIndexLocked(); err != nil {
		return nil, 0, err
	}
	type ent struct {
		id  string
		ref archiveRef
	}
	ents := make([]ent, 0, len(s.arc.idx))
	before := int64(0)
	if !q.Before.IsZero() {
		before = q.Before.UnixNano()
	}
	for id, ref := range s.arc.idx {
		if before != 0 && ref.ts > before {
			continue
		}
		if s.activeLocked(id) {
			continue
		}
		ents = append(ents, ent{id, ref})
	}
	sort.Slice(ents, func(i, j int) bool {
		if ents[i].ref.ts != ents[j].ref.ts {
			return ents[i].ref.ts > ents[j].ref.ts
		}
		return ents[i].id < ents[j].id
	})
	text := strings.ToLower(strings.TrimSpace(q.Text))
	skip := q.Offset
	if skip < 0 {
		skip = 0
	}
	fast := text == "" && q.Match == nil
	if fast {
		total = len(ents)
		if skip > len(ents) {
			skip = len(ents)
		}
		ents = ents[skip:]
		if q.Limit > 0 && len(ents) > q.Limit {
			ents = ents[:q.Limit]
		}
		skip = 0
	}
	for _, e := range ents {
		if !fast && q.Limit > 0 && len(out) >= q.Limit && skip == 0 {
			// Window is full; only the match count is left to finish.
			sess, rerr := s.readArchivedLocked(e.ref)
			if rerr == nil && archivedWants(sess, text, q.Match) {
				total++
			}
			continue
		}
		sess, rerr := s.readArchivedLocked(e.ref)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "[warn] sessions archive: read %s: %v\n", e.id, rerr)
			continue
		}
		if !fast {
			if !archivedWants(sess, text, q.Match) {
				continue
			}
			total++
			if skip > 0 {
				skip--
				continue
			}
		}
		out = append(out, sess)
	}
	return out, total, nil
}

func archivedWants(sess *Session, text string, match func(*Session) bool) bool {
	if text != "" && !archivedMatchesText(sess, text) {
		return false
	}
	return match == nil || match(sess)
}

func archivedMatchesText(sess *Session, lowered string) bool {
	for _, f := range []string{sess.Name, sess.ID, sess.FullID, sess.Task} {
		if strings.Contains(strings.ToLower(f), lowered) {
			return true
		}
	}
	return false
}

// RestoreFromArchive moves an archived session back to the active set and
// returns it. Its UpdatedAt is set to now, so the next archive pass does not
// take it straight back. The session is made durable in the active store before
// the archive is told, so a crash cannot lose it.
func (s *Store) RestoreFromArchive(id string) (*Session, error) {
	s.arc.mu.Lock()
	defer s.arc.mu.Unlock()
	if err := s.loadArchiveIndexLocked(); err != nil {
		return nil, err
	}
	full, ok := s.resolveArchivedLocked(id)
	if !ok {
		return nil, fmt.Errorf("session %q is not in the archive", id)
	}
	if s.activeLocked(full) {
		return nil, fmt.Errorf("session %q is already active", full)
	}
	sess, err := s.readArchivedLocked(s.arc.idx[full])
	if err != nil {
		return nil, fmt.Errorf("read archived session %s: %w", full, err)
	}
	sess.UpdatedAt = time.Now()
	if err := s.Save(sess); err != nil {
		return nil, err
	}
	if err := s.flush(true); err != nil {
		return nil, err
	}
	if err := s.appendArchiveLocked([]archiveLine{{ID: full, Del: true}}); err != nil {
		return sess, fmt.Errorf("restored %s but could not update the archive: %w", full, err)
	}
	return sess, nil
}

// ---- per-field size cap ----

// sideFile is the full text of a truncated field.
type sideFile struct {
	path string // absolute
	text string
}

type cappedField struct {
	idx  int
	name string // json name
}

var cappedFields = func() []cappedField {
	var out []cappedField
	t := reflect.TypeOf(Session{})
	str := reflect.TypeOf("")
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type != str {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out = append(out, cappedField{i, name})
	}
	return out
}()

// fieldsDirFor is the side-file directory (relative to the data dir, slash
// separated) for a session.
func fieldsDirFor(fullID string) string {
	dir := fullID
	safe := fullID != "" && len(fullID) <= 120
	for i := 0; safe && i < len(fullID); i++ {
		c := fullID[i]
		safe = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
	}
	if !safe {
		sum := sha256.Sum256([]byte(fullID))
		dir = "x." + hex.EncodeToString(sum[:])
	}
	return path.Join("sessions", dir, fieldsDirName)
}

func isTruncationMarked(v string) bool {
	i := strings.LastIndex(v, "\n[truncated ")
	return i >= 0 && strings.HasSuffix(v, "]") && strings.Contains(v[i:], "full text in side file")
}

// cutUTF8 returns the longest prefix of s of at most n bytes that does not
// split a rune.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// marshalCapped marshals sess for persistence. A string field longer than the
// field cap is replaced in the output (never in sess) by its first cap bytes and
// a marker naming the side file that holds the full text; the side files to
// write are returned and must be written before the record.
func (s *Store) marshalCapped(sess *Session) ([]byte, []sideFile, error) {
	capN := s.FieldCap()
	if capN <= 0 {
		data, err := json.Marshal(sess)
		return data, nil, err
	}
	var cp *Session
	var side []sideFile
	v := reflect.ValueOf(sess).Elem()
	for _, f := range cappedFields {
		orig := v.Field(f.idx).String()
		if len(orig) <= capN || isTruncationMarked(orig) {
			continue
		}
		if cp == nil {
			c := *sess
			cp = &c
		}
		rel := path.Join(fieldsDirFor(sess.FullID), f.name+".txt")
		kept := cutUTF8(orig, capN)
		marker := fmt.Sprintf("\n[truncated %d bytes, full text in side file %s]", len(orig)-len(kept), rel)
		reflect.ValueOf(cp).Elem().Field(f.idx).SetString(kept + marker)
		side = append(side, sideFile{path: filepath.Join(filepath.Dir(s.path), filepath.FromSlash(rel)), text: orig})
	}
	if cp == nil {
		cp = sess
	}
	data, err := json.Marshal(cp)
	return data, side, err
}

// writeSideFiles writes the side files, skipping any whose content is already
// on disk.
func (s *Store) writeSideFiles(files []sideFile) error {
	for _, f := range files {
		if cur, err := secfile.ReadFile(f.path, s.encKey); err == nil && string(cur) == f.text {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.path), 0755); err != nil {
			return fmt.Errorf("create fields dir: %w", err)
		}
		if err := secfile.WriteFile(f.path, []byte(f.text), 0644, s.encKey); err != nil {
			return fmt.Errorf("write side file %s: %w", f.path, err)
		}
	}
	return nil
}

// ReadField returns the full text of a session field that was truncated when
// persisted (field is its json name, such as "task"). ok is false when the
// field has no side file.
func (s *Store) ReadField(fullID, field string) (text string, ok bool, err error) {
	valid := false
	for _, f := range cappedFields {
		if f.name == field {
			valid = true
		}
	}
	if !valid {
		return "", false, fmt.Errorf("unknown field %q", field)
	}
	p := filepath.Join(filepath.Dir(s.path), filepath.FromSlash(fieldsDirFor(fullID)), field+".txt")
	data, err := secfile.ReadFile(p, s.encKey)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}
