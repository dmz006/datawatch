package planrecords

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dmz006/datawatch/internal/secfile"
)

// Audit action names emitted by every write. The audit hook receives the
// action, the record title as subject and a details map (uid, id, kind,
// project, actor, and for updates the changed fields).
const (
	AuditCreated = "plan_record_created"
	AuditUpdated = "plan_record_updated"
)

// AuditFunc receives one audit event per successful write.
type AuditFunc func(action, subject string, details map[string]any)

// Sealer seals and opens a record's payload. A nil Sealer stores plaintext.
type Sealer interface {
	Seal(plain []byte) (string, error)
	Open(sealed string) ([]byte, error)
}

type keySealer struct{ key []byte }

// KeySealer seals with the shared per-record file sealing (internal/secfile,
// XChaCha20-Poly1305) using a 32-byte key.
func KeySealer(key []byte) (Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("planrecords: key must be 32 bytes, got %d", len(key))
	}
	return keySealer{key: append([]byte(nil), key...)}, nil
}

func (k keySealer) Seal(plain []byte) (string, error) {
	b, err := secfile.Encrypt(plain, k.key)
	return string(b), err
}

func (k keySealer) Open(sealed string) ([]byte, error) {
	return secfile.Decrypt([]byte(sealed), k.key)
}

// Options are the store settings. They mirror the autonomous.plan_records
// config block; the config layer fills them in.
type Options struct {
	// WriteThrough is carried for callers that mirror records into docs; the
	// store itself does not read it.
	WriteThrough bool
	// ImportConflictPolicy is the default policy for PreviewConflicts.
	ImportConflictPolicy ConflictPolicy
	// Audit receives one event per write; nil disables it.
	Audit AuditFunc
	// Logf logs skipped files and other store problems; nil uses log.Printf.
	Logf func(format string, args ...any)
	// Now is the clock; nil uses time.Now.
	Now func() time.Time
}

// ErrNotFound is returned when no record matches a reference.
var ErrNotFound = errors.New("planrecords: no record matches that reference")

// ErrAmbiguous is returned when a reference matches more than one record.
type ErrAmbiguous struct {
	Ref        string
	Candidates []Record
}

func (e *ErrAmbiguous) Error() string {
	parts := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		parts = append(parts, fmt.Sprintf("%q (%s, project %s)", c.Title, c.ID, c.Project))
	}
	return fmt.Sprintf("planrecords: %q is ambiguous, which one: %s", e.Ref, strings.Join(parts, "; "))
}

// ErrTitleConflict is returned when a title is already used inside the project.
type ErrTitleConflict struct {
	Title    string
	Existing Record
}

func (e *ErrTitleConflict) Error() string {
	return fmt.Sprintf("planrecords: title %q is already used in project %s by %q (%s)",
		e.Title, e.Existing.Project, e.Existing.Title, e.Existing.ID)
}

// ErrLegacyNumberConflict is returned when a registry row number is already
// held by another record in the project.
type ErrLegacyNumberConflict struct {
	Number   int
	Existing Record
}

func (e *ErrLegacyNumberConflict) Error() string {
	return fmt.Sprintf("planrecords: row %d is already held by %q (%s)", e.Number, e.Existing.Title, e.Existing.ID)
}

// Store is the file-backed record store with an in-memory index.
type Store struct {
	root   string
	sealer Sealer
	opts   Options

	mu      sync.RWMutex
	byUID   map[string]*Record
	byID    map[string]string // id -> uid
	byTitle map[string]string // project|normTitle -> uid
	li      linkIndex         // summary index and plan <-> Automaton/Automation lookups
	skipped []string
}

var allKinds = []Kind{KindPlan, KindRegistryRow, KindFinding}

// Open opens (creating if needed) the store under dir/plan-records and
// rebuilds the index from the files. A file that cannot be read, decrypted or
// parsed is logged and skipped; it never fails the open.
func Open(dir string, sealer Sealer, opts ...Options) (*Store, error) {
	s := &Store{
		root:    filepath.Join(dir, "plan-records"),
		sealer:  sealer,
		byUID:   map[string]*Record{},
		byID:    map[string]string{},
		byTitle: map[string]string{},
		li:      newLinkIndex(),
	}
	if len(opts) > 0 {
		s.opts = opts[0]
	}
	if s.opts.ImportConflictPolicy == "" {
		s.opts.ImportConflictPolicy = PolicySuffix
	}
	for _, k := range allKinds {
		if err := os.MkdirAll(filepath.Join(s.root, string(k)), 0o700); err != nil {
			return nil, fmt.Errorf("planrecords: create store dir: %w", err)
		}
	}
	for _, k := range allKinds {
		s.loadKind(k)
	}
	return s, nil
}

func (s *Store) logf(format string, args ...any) {
	if s.opts.Logf != nil {
		s.opts.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (s *Store) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now().UTC()
	}
	return time.Now().UTC()
}

// Skipped lists the files the last open could not load.
func (s *Store) Skipped() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.skipped...)
}

// Len is the number of records in the index.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byUID)
}

func (s *Store) path(k Kind, uid string) string {
	return filepath.Join(s.root, string(k), uid+".json")
}

func (s *Store) skip(path string, err error) {
	s.skipped = append(s.skipped, path)
	s.logf("planrecords: skipping %s: %v", path, err)
}

func (s *Store) loadKind(k Kind) {
	dir := filepath.Join(s.root, string(k))
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.skip(dir, err)
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		// A leftover temp file from an interrupted write ends in .tmp-N, not .json.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		p := filepath.Join(dir, n)
		rec, err := s.readFile(p)
		if err != nil {
			s.skip(p, err)
			continue
		}
		if rec.Kind != k || rec.UID+".json" != n {
			s.skip(p, fmt.Errorf("kind or uid does not match the file location"))
			continue
		}
		if _, dup := s.byID[rec.ID]; dup {
			s.skip(p, fmt.Errorf("duplicate id %q", rec.ID))
			continue
		}
		s.index(&rec)
		if s.byTitle[titleKey(rec.Project, rec.Title)] != rec.UID {
			s.logf("planrecords: %s shares a title with another record in project %s", p, rec.Project)
		}
	}
}

// envelope is the on-disk form of a sealed record: the metadata that stays
// readable plus the sealed payload holding the whole record.
type envelope struct {
	Version int      `json:"v"`
	Meta    metadata `json:"meta"`
	Sealed  string   `json:"sealed"`
}

// metadata is the exact set of fields a sealed file keeps readable: kind,
// project, state, id, uid, legacy_number and the three dates. Nothing else
// (no title, body, alias, link or Automaton id) is readable without the key.
type metadata struct {
	Kind         Kind   `json:"kind"`
	Project      string `json:"project"`
	State        State  `json:"state"`
	ID           string `json:"id"`
	UID          string `json:"uid"`
	LegacyNumber int    `json:"legacy_number,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	ClosedAt     string `json:"closed_at,omitempty"`
}

func (s *Store) readFile(path string) (Record, error) {
	var rec Record
	raw, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}
	var probe struct {
		Sealed *string `json:"sealed"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return rec, fmt.Errorf("corrupt json: %w", err)
	}
	if probe.Sealed != nil {
		if s.sealer == nil {
			return rec, errors.New("file is sealed but the store has no key")
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return rec, fmt.Errorf("corrupt envelope: %w", err)
		}
		plain, err := s.sealer.Open(env.Sealed)
		if err != nil {
			return rec, fmt.Errorf("cannot open sealed payload: %w", err)
		}
		raw = plain
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return rec, fmt.Errorf("corrupt record: %w", err)
	}
	if rec.UID == "" || rec.ID == "" {
		return rec, errors.New("record has no id or uid")
	}
	if err := rec.validate(); err != nil {
		return rec, err
	}
	return rec, nil
}

// writeFile persists rec through a temp file and an atomic rename.
func (s *Store) writeFile(rec Record) error {
	payload, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if s.sealer != nil {
		sealed, err := s.sealer.Seal(payload)
		if err != nil {
			return fmt.Errorf("seal: %w", err)
		}
		payload, err = json.MarshalIndent(envelope{
			Version: 1,
			Meta: metadata{Kind: rec.Kind, Project: rec.Project, State: rec.State, ID: rec.ID,
				UID: rec.UID, LegacyNumber: rec.LegacyNumber, CreatedAt: rec.CreatedAt,
				UpdatedAt: rec.UpdatedAt, ClosedAt: rec.ClosedAt},
			Sealed: sealed,
		}, "", "  ")
		if err != nil {
			return err
		}
	}
	dest := s.path(rec.Kind, rec.UID)
	tmp, err := os.CreateTemp(filepath.Dir(dest), rec.UID+".json.tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(payload); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func titleKey(project, title string) string { return project + "\x00" + NormTitle(title) }

// index adds rec to the maps; the caller holds the write lock (or is Open).
func (s *Store) index(rec *Record) {
	s.byUID[rec.UID] = rec
	s.byID[rec.ID] = rec.UID
	s.reindexLinks(nil, rec)
	if _, taken := s.byTitle[titleKey(rec.Project, rec.Title)]; !taken {
		s.byTitle[titleKey(rec.Project, rec.Title)] = rec.UID
	}
}

func (s *Store) newID(title string, kind Kind, uid string) string {
	base := slugify(title, 40)
	if base == "" {
		base = map[Kind]string{KindRegistryRow: "row", KindFinding: "finding"}[kind]
	}
	if _, taken := s.byID[base]; !taken {
		return base
	}
	compact := strings.ReplaceAll(uid, "-", "")
	for n := 4; n <= len(compact); n += 2 {
		id := base + "-" + compact[:n]
		if _, taken := s.byID[id]; !taken {
			return id
		}
	}
	return base + "-" + compact
}

func (s *Store) legacyHolder(project string, n int) *Record {
	for _, r := range s.byUID {
		if r.Kind == KindRegistryRow && r.Project == project && r.LegacyNumber == n {
			return r
		}
	}
	return nil
}

// Create stores a new record. The store assigns uid, created_at, updated_at
// and closed_at; id is generated from the title unless a free valid one is
// given. State defaults to open.
func (s *Store) Create(in Record) (Record, error) {
	rec := in.clone()
	rec.Title = strings.TrimSpace(rec.Title)
	rec.Aliases = dedupeAliases(rec.Aliases, rec.Title)
	if rec.State == "" {
		rec.State = StateOpen
	}
	if len(rec.Automations)+len(rec.Automata)+len(rec.Findings) > 0 || len(rec.LinkInfo) > 0 ||
		rec.AttachKind != "" || rec.AttachRef != "" {
		return Record{}, invalid("links are added with the attach methods, not at creation")
	}
	if err := rec.validate(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if uidHolder, ok := s.byTitle[titleKey(rec.Project, rec.Title)]; ok {
		return Record{}, &ErrTitleConflict{Title: rec.Title, Existing: s.byUID[uidHolder].clone()}
	}
	if rec.LegacyNumber > 0 {
		if h := s.legacyHolder(rec.Project, rec.LegacyNumber); h != nil {
			return Record{}, &ErrLegacyNumberConflict{Number: rec.LegacyNumber, Existing: h.clone()}
		}
	}
	rec.UID = uuid.NewString()
	if rec.Kind == KindPlan && rec.Order == 0 {
		rec.Order = s.nextPlanOrderLocked()
	}
	if rec.ID == "" {
		rec.ID = s.newID(rec.Title, rec.Kind, rec.UID)
	} else if _, taken := s.byID[rec.ID]; taken {
		return Record{}, invalid("id %q is already used", rec.ID)
	}
	ts := s.now().Format(time.RFC3339)
	rec.CreatedAt, rec.UpdatedAt, rec.ClosedAt = ts, ts, ""
	if IsClosed(rec.Kind, rec.State) {
		rec.ClosedAt = ts
	}
	if err := s.writeFile(rec); err != nil {
		return Record{}, fmt.Errorf("planrecords: write %s: %w", rec.ID, err)
	}
	stored := rec
	stored.Actor = ""
	s.index(&stored)
	s.audit(AuditCreated, rec, map[string]any{"state": string(rec.State)}, rec.Actor)
	return stored.clone(), nil
}

func dedupeAliases(aliases []string, title string) []string {
	seen := map[string]bool{NormTitle(title): true}
	var out []string
	for _, a := range aliases {
		a = strings.TrimSpace(a)
		if k := NormTitle(a); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, a)
		}
	}
	return out
}

func (s *Store) audit(action string, rec Record, extra map[string]any, actor string) {
	if s.opts.Audit == nil {
		return
	}
	if actor == "" {
		actor = "system"
	}
	d := map[string]any{"uid": rec.UID, "id": rec.ID, "kind": string(rec.Kind), "project": rec.Project, "actor": actor}
	for k, v := range extra {
		d[k] = v
	}
	s.opts.Audit(action, rec.Title, d)
}

// Update applies a patch to the record a reference resolves to. A rename
// keeps the old title as an alias; a title that collides inside the project
// is refused with *ErrTitleConflict. Closing a record is a state change.
func (s *Store) Update(ref string, p Patch) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.resolveLocked(ref, "")
	if err != nil {
		return Record{}, err
	}
	next := cur.clone()
	var changed []string
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if NormTitle(t) != NormTitle(cur.Title) || t != cur.Title {
			changed = append(changed, "title")
			if NormTitle(t) != NormTitle(cur.Title) {
				if h, ok := s.byTitle[titleKey(cur.Project, t)]; ok && h != cur.UID {
					return Record{}, &ErrTitleConflict{Title: t, Existing: s.byUID[h].clone()}
				}
				next.Aliases = append(next.Aliases, cur.Title)
			}
			next.Title = t
		}
	}
	set := func(name string, dst *string, v *string) {
		if v != nil && *v != *dst {
			*dst = *v
			changed = append(changed, name)
		}
	}
	set("body", &next.Body, p.Body)
	set("legacy_source", &next.LegacySource, p.LegacySource)
	set("shipped_by", &next.ShippedBy, p.ShippedBy)
	set("raised_by", &next.RaisedBy, p.RaisedBy)
	set("shipped_version", &next.ShippedVersion, p.ShippedVersion)
	set("review_reason", &next.ReviewReason, p.ReviewReason)
	set("goal", &next.Goal, p.Goal)
	if p.Order != nil && *p.Order != cur.Order {
		next.Order = *p.Order
		changed = append(changed, "order")
	}
	if p.State != nil && *p.State != cur.State {
		next.State = *p.State
		changed = append(changed, "state")
	}
	if cur.Kind == KindPlan {
		// A state written through this path is a status set by hand.
		by := cur.SetByHand
		if p.State != nil {
			by = true
		}
		if p.SetByHand != nil {
			by = *p.SetByHand
		}
		if by != cur.SetByHand {
			next.SetByHand = by
			changed = append(changed, "set_by_hand")
		}
	}
	if p.NeedsReview != nil && *p.NeedsReview != cur.NeedsReview {
		next.NeedsReview = *p.NeedsReview
		changed = append(changed, "needs_review")
	}
	if p.Links != nil {
		next.Links = append([]Link(nil), *p.Links...)
		changed = append(changed, "links")
	}
	if len(p.AddAliases) > 0 {
		next.Aliases = append(next.Aliases, p.AddAliases...)
		changed = append(changed, "aliases")
	}
	next.Aliases = dedupeAliases(next.Aliases, next.Title)
	if err := next.validate(); err != nil {
		return Record{}, err
	}
	if len(changed) == 0 {
		return cur.clone(), nil
	}
	ts := s.now().Format(time.RFC3339)
	next.UpdatedAt = ts
	switch {
	case IsClosed(next.Kind, next.State) && !IsClosed(cur.Kind, cur.State):
		next.ClosedAt = ts
	case !IsClosed(next.Kind, next.State):
		next.ClosedAt = ""
	}
	if err := s.writeFile(next); err != nil {
		return Record{}, fmt.Errorf("planrecords: write %s: %w", next.ID, err)
	}
	// Re-point the title index when the title changed.
	if NormTitle(next.Title) != NormTitle(cur.Title) {
		if s.byTitle[titleKey(cur.Project, cur.Title)] == cur.UID {
			delete(s.byTitle, titleKey(cur.Project, cur.Title))
		}
		s.byTitle[titleKey(next.Project, next.Title)] = cur.UID
	}
	stored := next
	s.byUID[cur.UID] = &stored
	s.reindexLinks(&cur, &stored)
	s.audit(AuditUpdated, stored, map[string]any{"changed": changed, "state": string(stored.State)}, p.Actor)
	return stored.clone(), nil
}

// Get returns the record a reference resolves to (see Resolve).
func (s *Store) Get(ref string) (Record, error) { return s.Resolve(ref, "") }

// Resolve finds one record by id, uid, "row <number>", exact title, an old
// alias, or an unambiguous title prefix, tried in that order. A non-empty
// project limits every step to that project. A step that matches several
// records returns *ErrAmbiguous with the candidates; no match is ErrNotFound.
func (s *Store) Resolve(ref, project string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, err := s.resolveLocked(ref, project)
	if err != nil {
		return Record{}, err
	}
	return r.clone(), nil
}

func (s *Store) resolveLocked(ref, project string) (Record, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Record{}, ErrNotFound
	}
	inProject := func(r *Record) bool { return project == "" || r.Project == project }
	if uid, ok := s.byID[ref]; ok && inProject(s.byUID[uid]) {
		return *s.byUID[uid], nil
	}
	if r, ok := s.byUID[ref]; ok && inProject(r) {
		return *r, nil
	}
	pick := func(match func(*Record) bool) (Record, bool, error) {
		var hits []Record
		for _, r := range s.byUID {
			if inProject(r) && match(r) {
				hits = append(hits, *r)
			}
		}
		switch len(hits) {
		case 0:
			return Record{}, false, nil
		case 1:
			return hits[0], true, nil
		}
		sortCandidates(hits)
		for i := range hits {
			hits[i] = hits[i].clone()
		}
		return Record{}, false, &ErrAmbiguous{Ref: ref, Candidates: hits}
	}
	steps := []func(*Record) bool{}
	if n, ok := ParseRowRef(ref); ok {
		steps = append(steps, func(r *Record) bool { return r.Kind == KindRegistryRow && r.LegacyNumber == n })
	}
	norm := NormTitle(ref)
	steps = append(steps,
		func(r *Record) bool { return NormTitle(r.Title) == norm },
		func(r *Record) bool {
			for _, a := range r.Aliases {
				if NormTitle(a) == norm {
					return true
				}
			}
			return false
		},
		func(r *Record) bool { return strings.HasPrefix(NormTitle(r.Title), norm) },
	)
	for _, m := range steps {
		r, found, err := pick(m)
		if err != nil {
			return Record{}, err
		}
		if found {
			return r, nil
		}
	}
	return Record{}, ErrNotFound
}

func sortCandidates(rs []Record) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Project != rs[j].Project {
			return rs[i].Project < rs[j].Project
		}
		return rs[i].ID < rs[j].ID
	})
}

// nextPlanOrderLocked is the order a new plan gets: after every existing plan.
func (s *Store) nextPlanOrderLocked() int {
	max := 0
	for _, sm := range s.li.sums {
		if sm.Kind == KindPlan && sm.Order > max {
			max = sm.Order
		}
	}
	if max >= MaxOrder {
		return MaxOrder
	}
	return max + 1
}
