// The request store: one file per request, <data dir>/queue.d/<id>.rec, with a
// readable index and, on a secure install, sealed content from the first
// write. It shares the delivery store key (storekey.go). Owner: part A.
//
// What it keeps (design v2 1.4):
//   - Secure record: {"v":1,"index":Request,"sealed":"..."}; the sealed part
//     holds the content and a copy of the index. A readable index that differs
//     from the sealed copy is a foreign write: red, never believed.
//     Non-secure record: {"v":1,"index":Request,"content":RequestContent}.
//   - A sealed manifest, meta.rec, with each file's last write number and hash
//     (I12). An unknown, changed or missing file, or a plain one in a sealed
//     store, is red. A record that cannot be opened is red (I17), never "not
//     there". While anything is red nothing here is written.
//   - Opening writes nothing.
//
// One write is one commit: the manifest is saved first, holding the new bytes
// of every file the change touches; then the files are written; then the
// manifest is saved without them. The first save is the commit. If it fails
// nothing changed. If a later one fails the change stands, the bytes stay in
// the manifest and are written again, and the store reports that it cannot
// record.

package delivery

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

const (
	metaFileName  = "meta.rec"
	queuePrefix   = "q-"
	recordSuffix  = ".rec"
	recordVersion = 1
	// noticesKept is how long a reminder record counts.
	noticesKept = 24 * time.Hour
)

// fileSum is the manifest's record of one file.
type fileSum struct {
	Seq  uint64 `json:"seq"`
	Hash string `json:"hash"`
}

// pendingFile is a write the manifest holds until the file has it.
type pendingFile struct {
	Seq    uint64 `json:"seq"`
	Data   []byte `json:"data,omitempty"`
	Delete bool   `json:"delete,omitempty"`
}

// condState is what the store saw of one watched condition.
type condState struct {
	Holds bool   `json:"holds,omitempty"`
	Value string `json:"value,omitempty"` // the hash of the value
	N     int    `json:"n"`
}

type storeMeta struct {
	Version    int                     `json:"version"`
	Seq        uint64                  `json:"seq"`
	Files      map[string]fileSum      `json:"files"`
	Pending    map[string]*pendingFile `json:"pending,omitempty"`
	Bindings   []RoleBinding           `json:"bindings,omitempty"`
	Notices    []RequestNotice         `json:"notices,omitempty"`
	Conditions map[string]condState    `json:"conditions,omitempty"`
	LastTick   time.Time               `json:"last_tick"`
	// Guarded: the mass-expiry guard is holding expiries back.
	Guarded bool `json:"guarded,omitempty"`
}

func newStoreMeta() *storeMeta {
	return &storeMeta{Version: recordVersion, Files: map[string]fileSum{}, Pending: map[string]*pendingFile{},
		Conditions: map[string]condState{}}
}

func (m *storeMeta) clone() *storeMeta {
	c := cloneJSON(m)
	if c.Files == nil {
		c.Files = map[string]fileSum{}
	}
	if c.Pending == nil {
		c.Pending = map[string]*pendingFile{}
	}
	if c.Conditions == nil {
		c.Conditions = map[string]condState{}
	}
	return c
}

// diskRecord is one request file.
type diskRecord struct {
	V       int             `json:"v"`
	Index   json.RawMessage `json:"index"`
	Content *RequestContent `json:"content,omitempty"`
	Sealed  string          `json:"sealed,omitempty"`
}

type sealedPart struct {
	Index   json.RawMessage `json:"index"`
	Content *RequestContent `json:"content,omitempty"`
}

// storedRequest is one request in memory. content is nil for a stub.
type storedRequest struct {
	index   Request
	content *RequestContent
}

// storeFault is one file the store cannot believe.
type storeFault struct {
	name      string
	invariant string
	what      string
}

// Requests is the store and the service over it.
type Requests struct {
	opts RequestOptions

	// opMu lets one change that spans a read and a write run at a time. It is
	// held while part C's keeper and verifier run, and they call back into the
	// book, which takes only mu. tickMu lets one check run at a time.
	opMu   sync.Mutex
	tickMu sync.Mutex
	mu     sync.Mutex

	meta  *storeMeta
	reqs  map[string]*storedRequest
	keys  map[string]string // key hash -> request ID
	queue map[string]json.RawMessage

	faults       []storeFault
	cannotRecord bool
	alarmed      bool
	// reseal: plain files were read because the store key is new.
	reseal bool
}

var (
	_ RequestService = (*Requests)(nil)
	_ RequestDaemon  = (*Requests)(nil)
	_ RequestBook    = (*Requests)(nil)
)

// OpenRequests opens the store. It writes nothing. An error means the store
// cannot be used at all: no key, a wrong one, or a damaged manifest. Single
// files that cannot be believed leave the store open and red.
func OpenRequests(opts RequestOptions) (*Requests, error) {
	if opts.Dir == "" {
		return nil, errors.New("requests: no folder")
	}
	s := newRequests(opts)
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// newRequests is an empty store with the options' defaults filled in.
func newRequests(opts RequestOptions) *Requests {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	if opts.writeFile == nil {
		opts.writeFile = func(path string, data []byte) error { return writeRecordFile(path, data, nil) }
	}
	return &Requests{opts: opts, meta: newStoreMeta(), reqs: map[string]*storedRequest{}, keys: map[string]string{},
		queue: map[string]json.RawMessage{}}
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// open reads bytes the store wrote. plainOK: this file may be plain in a
// sealed store.
func (s *Requests) openSealed(data []byte, plainOK bool) (plain []byte, wasPlain bool, err error) {
	if secfile.IsEncrypted(data) {
		if s.opts.Key == nil {
			return nil, false, errors.New("it is sealed and datawatch runs without a store key")
		}
		plain, err = secfile.Decrypt(data, s.opts.Key)
		return plain, false, err
	}
	if s.opts.Key != nil && !plainOK {
		return nil, false, errors.New("a plain record in a sealed store")
	}
	return data, true, nil
}

func (s *Requests) load() error {
	entries, err := os.ReadDir(s.opts.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRecords, err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, recordSuffix) && n != metaFileName {
			onDisk[n] = true
		}
	}
	data, err := os.ReadFile(filepath.Join(s.opts.Dir, metaFileName))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// With no manifest every record is unknown.
	case err != nil:
		return fmt.Errorf("%w: %s: %v", ErrRecords, metaFileName, err)
	default:
		plain, wasPlain, err := s.openSealed(data, s.opts.AdoptPlain)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrRecords, metaFileName, err)
		}
		m := newStoreMeta()
		if err := json.Unmarshal(plain, m); err != nil || m.Version != recordVersion {
			return fmt.Errorf("%w: %s is damaged", ErrRecords, metaFileName)
		}
		s.meta = m.clone()
		s.reseal = wasPlain && s.opts.Key != nil
	}

	names := map[string]bool{}
	for n := range onDisk {
		names[n] = true
	}
	for n := range s.meta.Files {
		names[n] = true
	}
	for n := range s.meta.Pending {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		s.loadFile(name, onDisk[name])
	}
	return nil
}

func (s *Requests) fault(name, invariant, what string) {
	s.faults = append(s.faults, storeFault{name: name, invariant: invariant, what: what})
}

func (s *Requests) loadFile(name string, there bool) {
	sum, known := s.meta.Files[name]
	pend := s.meta.Pending[name]
	var data []byte
	if there {
		var err error
		if data, err = os.ReadFile(filepath.Join(s.opts.Dir, name)); err != nil {
			s.fault(name, InvUnopenable, "unreadable")
			return
		}
	}
	switch {
	case pend != nil:
		// The file holds what it held, or what the manifest is writing.
		old := (known && there && hashOf(data) == sum.Hash) || (!known && !there)
		done := (pend.Delete && !there) || (!pend.Delete && there && bytes.Equal(data, pend.Data))
		if !old && !done {
			s.fault(name, "I12", "changed")
			return
		}
		if pend.Delete {
			return
		}
		data = pend.Data
	case !known:
		s.fault(name, "I12", "unknown")
		return
	case !there:
		s.fault(name, "I12", "missing")
		return
	case hashOf(data) != sum.Hash:
		s.fault(name, "I12", "changed")
		// It is still looked at, so that a record that cannot be opened is
		// named as such too.
		if _, _, err := s.openSealed(bodyOf(data), s.opts.AdoptPlain); err != nil {
			s.fault(name, InvUnopenable, "unopenable")
		}
		return
	}
	if strings.HasPrefix(name, queuePrefix) {
		plain, wasPlain, err := s.openSealed(data, s.opts.AdoptPlain)
		if err != nil || !json.Valid(plain) {
			s.fault(name, InvUnopenable, "unopenable")
			return
		}
		s.reseal = s.reseal || (wasPlain && s.opts.Key != nil)
		s.queue[strings.TrimSuffix(strings.TrimPrefix(name, queuePrefix), recordSuffix)] = plain
		return
	}
	st, wasPlain, invariant, err := s.decodeRecord(data)
	if err != nil {
		s.fault(name, invariant, "unopenable")
		return
	}
	if st.index.ID+recordSuffix != name {
		s.fault(name, "I12", "renamed")
		return
	}
	s.reseal = s.reseal || wasPlain
	s.reqs[st.index.ID] = st
	s.keys[st.index.KeyHash] = st.index.ID
}

// bodyOf is the sealed part of a request file, or the whole file.
func bodyOf(data []byte) []byte {
	var d diskRecord
	if json.Unmarshal(data, &d) == nil && d.Sealed != "" {
		return []byte(d.Sealed)
	}
	return data
}

func (s *Requests) decodeRecord(data []byte) (st *storedRequest, wasPlain bool, invariant string, err error) {
	var d diskRecord
	if err := json.Unmarshal(data, &d); err != nil || d.V != recordVersion {
		return nil, false, InvUnopenable, errors.New("not a request record")
	}
	st = &storedRequest{content: d.Content}
	if err := json.Unmarshal(d.Index, &st.index); err != nil {
		return nil, false, InvUnopenable, err
	}
	if d.Sealed == "" {
		if s.opts.Key != nil && !s.opts.AdoptPlain {
			return nil, false, "I12", errors.New("a plain record in a sealed store")
		}
		return st, s.opts.Key != nil, "", nil
	}
	plain, _, err := s.openSealed([]byte(d.Sealed), false)
	if err != nil {
		return nil, false, InvUnopenable, err
	}
	var part sealedPart
	if err := json.Unmarshal(plain, &part); err != nil {
		return nil, false, InvUnopenable, err
	}
	if !sameJSON(part.Index, d.Index) {
		return nil, false, "I12", errors.New("the readable index differs from the sealed copy")
	}
	st.content = part.Content
	return st, false, "", nil
}

func sameJSON(a, b []byte) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

func (s *Requests) encodeRecord(st *storedRequest) ([]byte, error) {
	index, err := json.Marshal(st.index)
	if err != nil {
		return nil, err
	}
	d := diskRecord{V: recordVersion, Index: index}
	if s.opts.Key == nil {
		d.Content = st.content
	} else {
		part, err := json.Marshal(sealedPart{Index: index, Content: st.content})
		if err != nil {
			return nil, err
		}
		sealed, err := secfile.Encrypt(part, s.opts.Key)
		if err != nil {
			return nil, err
		}
		d.Sealed = string(sealed)
	}
	return json.Marshal(d)
}

func (s *Requests) seal(data []byte) ([]byte, error) {
	if s.opts.Key == nil {
		return data, nil
	}
	return secfile.Encrypt(data, s.opts.Key)
}

// txn is one change: what goes into one commit.
type txn struct {
	reqs  map[string]*storedRequest // nil value: remove
	queue map[string]json.RawMessage
	meta  []func(*storeMeta)
}

func newTxn() *txn {
	return &txn{reqs: map[string]*storedRequest{}, queue: map[string]json.RawMessage{}}
}

func (t *txn) empty() bool { return len(t.reqs) == 0 && len(t.queue) == 0 && len(t.meta) == 0 }

func (s *Requests) writeMeta(m *storeMeta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if data, err = s.seal(data); err != nil {
		return err
	}
	if err := os.MkdirAll(s.opts.Dir, 0o700); err != nil {
		return err
	}
	return s.opts.writeFile(filepath.Join(s.opts.Dir, metaFileName), data)
}

// commitLocked saves one change. Callers hold s.mu. On ErrCannotRecord or
// ErrRecords nothing changed.
func (s *Requests) commitLocked(t *txn) error {
	if len(s.faults) > 0 {
		return ErrRecords
	}
	next := s.meta.clone()
	for _, f := range t.meta {
		f(next)
	}
	put := func(name string, data []byte, remove bool) {
		next.Seq++
		next.Pending[name] = &pendingFile{Seq: next.Seq, Data: data, Delete: remove}
	}
	for id, st := range t.reqs {
		if st == nil {
			put(id+recordSuffix, nil, true)
			continue
		}
		st.index.Seq = next.Seq + 1
		data, err := s.encodeRecord(st)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrCannotRecord, err)
		}
		put(id+recordSuffix, data, false)
	}
	for id, item := range t.queue {
		name := queuePrefix + id + recordSuffix
		if item == nil {
			put(name, nil, true)
			continue
		}
		data, err := s.seal(item)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrCannotRecord, err)
		}
		put(name, data, false)
	}
	if err := s.writeMeta(next); err != nil {
		s.failedLocked()
		return fmt.Errorf("%w: %v", ErrCannotRecord, err)
	}
	s.meta = next
	for id, st := range t.reqs {
		if old := s.reqs[id]; old != nil && st == nil {
			delete(s.keys, old.index.KeyHash)
		}
		if st == nil {
			delete(s.reqs, id)
			continue
		}
		s.reqs[id] = st
		s.keys[st.index.KeyHash] = id
	}
	for id, item := range t.queue {
		if item == nil {
			delete(s.queue, id)
			continue
		}
		s.queue[id] = append(json.RawMessage(nil), item...)
	}
	s.applyLocked()
	return nil
}

// applyLocked writes the files the manifest is still holding.
func (s *Requests) applyLocked() {
	if len(s.meta.Pending) == 0 {
		return
	}
	names := make([]string, 0, len(s.meta.Pending))
	for n := range s.meta.Pending {
		names = append(names, n)
	}
	sort.Strings(names)
	next := s.meta.clone()
	failed, removed := false, false
	for _, name := range names {
		p := next.Pending[name]
		path := filepath.Join(s.opts.Dir, name)
		if p.Delete {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				failed = true
				continue
			}
			removed = true
			delete(next.Files, name)
		} else {
			if err := s.opts.writeFile(path, p.Data); err != nil {
				failed = true
				continue
			}
			next.Files[name] = fileSum{Seq: p.Seq, Hash: hashOf(p.Data)}
		}
		delete(next.Pending, name)
	}
	if removed && syncFolder(s.opts.Dir) != nil {
		failed = true
	}
	// The manifest on disk still holds the bytes until this save works, so a
	// failure here loses nothing.
	if err := s.writeMeta(next); err != nil {
		s.failedLocked()
		return
	}
	s.meta = next
	if failed {
		s.failedLocked()
		return
	}
	s.cannotRecord, s.alarmed = false, false
}

// failedLocked notes that the store cannot record.
func (s *Requests) failedLocked() { s.cannotRecord = true }

// alarm raises, once per fault, that the store cannot record. It runs without
// s.mu: the delivery list may read this store.
func (s *Requests) alarm() {
	s.mu.Lock()
	raise := s.cannotRecord && !s.alarmed
	if raise {
		s.alarmed = true
	}
	s.mu.Unlock()
	if raise {
		s.record(Candidate{Kind: KindAlarm, ID: "requests_cannot_record", State: alarmState,
			Invariant: InvOverdue, Problem: alarmReason, Sticky: true, WaitsOn: WaitsOnOperator})
	}
}

func (s *Requests) record(c Candidate) {
	if s.opts.Record == nil {
		return
	}
	if c.Since.IsZero() {
		c.Since = s.opts.Now()
	}
	_ = s.opts.Record(c) // the list keeps what it cannot save yet and says so
}

func (s *Requests) commit(t *txn) error {
	s.mu.Lock()
	err := s.commitLocked(t)
	s.mu.Unlock()
	s.alarm()
	return err
}

// Healthy returns ErrCannotRecord while the store cannot save. It tries first
// to write what is waiting. The delivery check does not write its heartbeat
// while this fails.
func (s *Requests) Healthy() error {
	s.mu.Lock()
	if s.cannotRecord && len(s.faults) == 0 {
		if len(s.meta.Pending) > 0 {
			s.applyLocked()
		} else if s.writeMeta(s.meta) == nil {
			s.cannotRecord, s.alarmed = false, false
		}
	}
	bad := s.cannotRecord
	s.mu.Unlock()
	s.alarm()
	if bad {
		return ErrCannotRecord
	}
	return nil
}

// SealAdopted seals, at once, plain files that were read because the store key
// is new. It writes nothing otherwise.
func (s *Requests) SealAdopted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.faults) > 0 {
		return ErrRecords
	}
	if !s.reseal {
		return nil
	}
	t := newTxn()
	for id, st := range s.reqs {
		t.reqs[id] = st.clone()
	}
	for id, item := range s.queue {
		t.queue[id] = item
	}
	if err := s.commitLocked(t); err != nil {
		return err
	}
	if s.cannotRecord {
		return ErrCannotRecord
	}
	s.reseal, s.opts.AdoptPlain = false, false
	return nil
}

func (st *storedRequest) clone() *storedRequest {
	c := &storedRequest{index: *cloneJSON(&st.index)}
	if st.content != nil {
		c.content = cloneJSON(st.content)
	}
	return c
}

// newRandom returns n random bytes in hex.
func newRandom(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---- RequestBook --------------------------------------------------------------

func matches(r *Request, f RequestFilter) bool {
	return (f.State == "" || r.State == f.State) && (f.Kind == "" || r.Kind == f.Kind) &&
		(f.Role == "" || r.Role == f.Role) && (f.ScopeKind == "" || r.Scope.Kind == f.ScopeKind) &&
		(f.ScopeID == "" || r.Scope.ID == f.ScopeID) &&
		(f.Holder == "" || (r.Holder != nil && r.Holder.SessionID == f.Holder))
}

// Requests implements RequestBook: copies, oldest first.
func (s *Requests) Requests(f RequestFilter) []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Request{}
	for _, st := range s.reqs {
		if matches(&st.index, f) {
			out = append(out, *cloneJSON(&st.index))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.Before(out[j].Created)
		}
		return out[i].Seq < out[j].Seq
	})
	return out
}

// Request implements RequestBook. A record that cannot be believed is not
// returned; Content and the calls say ErrRecords for it, never "not found".
func (s *Requests) Request(id string) (Request, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.reqs[id]
	if !ok {
		return Request{}, false
	}
	return *cloneJSON(&st.index), true
}

// lookupLocked finds a request. While any file is red an unknown ID reads
// "cannot be read", never "no such request".
func (s *Requests) lookupLocked(id string) (*storedRequest, error) {
	if st, ok := s.reqs[id]; ok {
		return st, nil
	}
	if len(s.faults) > 0 {
		return nil, ErrRecords
	}
	return nil, ErrRequestNotFound
}

// Content implements RequestBook.
func (s *Requests) Content(id string) (RequestContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.lookupLocked(id)
	if err != nil {
		return RequestContent{}, err
	}
	if st.content == nil {
		return RequestContent{}, errors.Join(ErrRequestState, errors.New("the content was removed; only the stub is left"))
	}
	return *cloneJSON(st.content), nil
}

// Change implements RequestBook. The ID, kind, key and sequence stay.
func (s *Requests) Change(id string, fn func(*Request) error) (Request, error) {
	s.mu.Lock()
	st, err := s.lookupLocked(id)
	if err != nil {
		s.mu.Unlock()
		return Request{}, err
	}
	next := st.clone()
	if err := fn(&next.index); err != nil {
		s.mu.Unlock()
		return Request{}, err
	}
	next.index.ID, next.index.Kind, next.index.KeyHash = st.index.ID, st.index.Kind, st.index.KeyHash
	t := newTxn()
	t.reqs[id] = next
	err = s.commitLocked(t)
	out := *cloneJSON(&next.index)
	s.mu.Unlock()
	s.alarm()
	if err != nil {
		return Request{}, err
	}
	return out, nil
}

// Bindings implements RequestBook.
func (s *Requests) Bindings() []RoleBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RoleBinding(nil), s.meta.clone().Bindings...)
}

// PutBinding implements RequestBook: one holder per role and scope.
func (s *Requests) PutBinding(b RoleBinding) error {
	return s.commit(&txn{meta: []func(*storeMeta){func(m *storeMeta) {
		m.Bindings = append(withoutBinding(m.Bindings, b.Role, b.Scope), b)
	}}})
}

// DropBinding implements RequestBook.
func (s *Requests) DropBinding(role Role, scope Scope) error {
	return s.commit(&txn{meta: []func(*storeMeta){func(m *storeMeta) {
		m.Bindings = withoutBinding(m.Bindings, role, scope)
	}}})
}

func withoutBinding(in []RoleBinding, role Role, scope Scope) []RoleBinding {
	out := make([]RoleBinding, 0, len(in))
	for _, b := range in {
		if b.Role != role || b.Scope != scope {
			out = append(out, b)
		}
	}
	return out
}

// Notices implements RequestBook.
func (s *Requests) Notices(since time.Time) []RequestNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []RequestNotice
	for _, n := range s.meta.clone().Notices {
		if !n.At.Before(since) {
			out = append(out, n)
		}
	}
	return out
}

// AddNotice implements RequestBook.
func (s *Requests) AddNotice(n RequestNotice) error {
	now := s.opts.Now()
	err := s.commit(&txn{meta: []func(*storeMeta){func(m *storeMeta) {
		kept := m.Notices[:0:0]
		for _, o := range m.Notices {
			if now.Sub(o.At) < noticesKept {
				kept = append(kept, o)
			}
		}
		m.Notices = append(kept, n)
	}}})
	return err
}

// ---- the older work queue (session.QueueRecords) --------------------------------

var queueIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// LoadQueue returns every queue item. While any file is red it fails: a queue
// that cannot be read whole is never an empty one.
func (s *Requests) LoadQueue() ([]json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.faults) > 0 {
		return nil, ErrRecords
	}
	ids := make([]string, 0, len(s.queue))
	for id := range s.queue {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		out = append(out, append(json.RawMessage(nil), s.queue[id]...))
	}
	return out, nil
}

// SaveQueueItem writes one queue item.
func (s *Requests) SaveQueueItem(id string, data []byte) error {
	if !queueIDRe.MatchString(id) || !json.Valid(data) {
		return errors.Join(ErrRequestInput, errors.New("a queue item needs a plain ID and JSON"))
	}
	t := newTxn()
	t.queue[id] = append(json.RawMessage(nil), data...)
	return s.commit(t)
}

// DeleteQueueItem removes one queue item.
func (s *Requests) DeleteQueueItem(id string) error {
	s.mu.Lock()
	_, ok := s.queue[id]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	t := newTxn()
	t.queue[id] = nil
	return s.commit(t)
}
