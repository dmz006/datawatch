package usagecalc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// StoreSchemaVersion is the version written to the store file.
	StoreSchemaVersion = 1

	// ReadingRetention is how long a reading is kept; older ones are pruned on write.
	ReadingRetention = 6 * 7 * 24 * time.Hour

	// MaxFutureSkew is how far ahead of the clock a reading may be dated.
	MaxFutureSkew = 5 * time.Minute

	// MaxPercentReading is the largest percentage a reading may carry.
	MaxPercentReading = 1000.0

	storeDirName  = "usagecalc"
	storeFileName = "usage.json"
)

// ErrInvalidReading is wrapped by every Append validation failure.
var ErrInvalidReading = errors.New("invalid usage reading")

// ErrInvalidValue is wrapped by a rejected extra-credit or remaining-work figure.
var ErrInvalidValue = errors.New("invalid usage value")

// ErrReadingNotFound is returned by Delete when no reading has the given ID.
var ErrReadingNotFound = errors.New("usage reading not found")

// LocalAccountRecord is the part of the local store that describes the account rather
// than this daemon: the readings series and the bought extra credit. A keeper daemon
// holds one of these per account and serves it to peers through SharedSeries
// (shared.go); the peer side keeps a copy of it next to its own store file.
type LocalAccountRecord struct {
	Readings             []Reading `json:"readings"`
	ExtraCreditPoints    float64   `json:"extra_credit_points"`
	ExtraCreditUpdatedAt time.Time `json:"extra_credit_updated_at"`
}

// LocalRecord is the part of the store that belongs to this daemon only and is not part of
// the shared account record.
type LocalRecord struct {
	// RemainingWork is the typed remaining-work figure in points; nil means none is typed.
	RemainingWork *float64 `json:"remaining_work,omitempty"`
}

type storeFile struct {
	Version int                `json:"version"`
	Account LocalAccountRecord `json:"account"`
	Local   LocalRecord        `json:"local"`
}

// StoreSnapshot is a copy of the stored state; changing it never changes the store.
type StoreSnapshot struct {
	Version int
	Account LocalAccountRecord
	Local   LocalRecord
}

// Store keeps the usage calculator's readings, extra credit and typed remaining work in
// one JSON file under the daemon data directory.
type Store struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
	logf func(format string, args ...any)
	data storeFile
}

// StoreOption adjusts a Store at open time.
type StoreOption func(*Store)

// WithLogger replaces the default logger, which writes through the standard log package.
func WithLogger(logf func(format string, args ...any)) StoreOption {
	return func(s *Store) {
		if logf != nil {
			s.logf = logf
		}
	}
}

// OpenStore opens the store under dataDir/usagecalc, creating the directory if needed.
// now is the injected clock. A file that cannot be parsed is moved aside with a .bad
// suffix, one error is logged, and the store starts empty; the bad file is never deleted.
func OpenStore(dataDir string, now func() time.Time, opts ...StoreOption) (*Store, error) {
	if dataDir == "" {
		return nil, errors.New("usagecalc store: data directory is required")
	}
	if now == nil {
		return nil, errors.New("usagecalc store: a clock is required")
	}
	dir := filepath.Join(dataDir, storeDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("usagecalc store: mkdir %s: %w", dir, err)
	}
	s := &Store{
		path: filepath.Join(dir, storeFileName),
		now:  now,
		logf: log.Printf,
		data: emptyStoreFile(),
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func emptyStoreFile() storeFile {
	return storeFile{Version: StoreSchemaVersion, Account: LocalAccountRecord{Readings: []Reading{}}}
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("usagecalc store: read %s: %w", s.path, err)
	}
	var f storeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return s.moveAside(fmt.Sprintf("does not parse: %v", err))
	}
	if f.Version != StoreSchemaVersion {
		return s.moveAside(fmt.Sprintf("has schema version %d, want %d", f.Version, StoreSchemaVersion))
	}
	if f.Account.Readings == nil {
		f.Account.Readings = []Reading{}
	}
	for i := range f.Account.Readings {
		f.Account.Readings[i] = markTypedLanes(f.Account.Readings[i])
	}
	sortReadings(f.Account.Readings)
	s.data = f
	return nil
}

// moveAside renames the unreadable file to the first free .bad name, logs once and leaves
// the store empty. It fails only when the file cannot be moved, so it is never overwritten.
func (s *Store) moveAside(reason string) error {
	dest := s.path + ".bad"
	for i := 1; ; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		dest = fmt.Sprintf("%s.bad.%d", s.path, i)
	}
	if err := os.Rename(s.path, dest); err != nil {
		return fmt.Errorf("usagecalc store: %s %s and could not be moved aside: %w", s.path, reason, err)
	}
	s.logf("usagecalc store: %s %s; moved to %s and starting empty", s.path, reason, dest)
	return nil
}

func sortReadings(rs []Reading) {
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].At.Before(rs[j].At) })
}

func copyFloat(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyReading(r Reading) Reading {
	r.Percent = copyFloat(r.Percent)
	r.SpendUSD = copyFloat(r.SpendUSD)
	r.SessionPercent = copyFloat(r.SessionPercent)
	if r.Lanes != nil {
		n := *r.Lanes
		r.Lanes = &n
	}
	return r
}

func copyReadings(rs []Reading) []Reading {
	out := make([]Reading, len(rs))
	for i, r := range rs {
		out[i] = copyReading(r)
	}
	return out
}

func (f storeFile) clone() storeFile {
	f.Account.Readings = copyReadings(f.Account.Readings)
	f.Local.RemainingWork = copyFloat(f.Local.RemainingWork)
	return f
}

func newReadingID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func validPercentFigure(v float64) bool { return finite(v) && v >= 0 && v <= MaxPercentReading }

// validateReading checks r against the clock and returns the cleaned record.
func validateReading(r Reading, now time.Time) (Reading, error) {
	r = copyReading(r)
	switch {
	case r.Percent != nil && r.SpendUSD != nil:
		return r, fmt.Errorf("%w: carry either a percent or a spend figure, not both", ErrInvalidReading)
	case r.Percent == nil && r.SpendUSD == nil:
		return r, fmt.Errorf("%w: carry a percent or a spend figure", ErrInvalidReading)
	}
	if r.Percent != nil && !validPercentFigure(*r.Percent) {
		return r, fmt.Errorf("%w: percent must be from 0 to %g", ErrInvalidReading, MaxPercentReading)
	}
	if r.SpendUSD != nil && (!finite(*r.SpendUSD) || *r.SpendUSD < 0) {
		return r, fmt.Errorf("%w: spend must be 0 or more", ErrInvalidReading)
	}
	if r.SessionPercent != nil && !validPercentFigure(*r.SessionPercent) {
		return r, fmt.Errorf("%w: session percent must be from 0 to %g", ErrInvalidReading, MaxPercentReading)
	}
	if r.At.IsZero() {
		return r, fmt.Errorf("%w: a reading needs a time", ErrInvalidReading)
	}
	if r.At.After(now.Add(MaxFutureSkew)) {
		return r, fmt.Errorf("%w: time is more than %s ahead of the clock", ErrInvalidReading, MaxFutureSkew)
	}
	r.At = r.At.UTC()
	// A lanes value from the request is not rejected, older callers still send one; the
	// keeper's stamp replaces it. A bare count with no source is an operator-typed one.
	return markTypedLanes(r), nil
}

// markTypedLanes gives a reading that carries a lane count but no source the typed source,
// which is what every such reading stored before the keeper stamped lanes is.
func markTypedLanes(r Reading) Reading {
	if r.Lanes != nil && r.LanesSource == "" {
		r.LanesSource = LanesTyped
	}
	return r
}

// save prunes old readings from next, writes it atomically and, only on success, makes it
// the current state. Callers hold s.mu.
func (s *Store) save(next storeFile) error {
	cutoff := s.now().Add(-ReadingRetention)
	kept := next.Account.Readings[:0:0]
	for _, r := range next.Account.Readings {
		if !r.At.Before(cutoff) {
			kept = append(kept, r)
		}
	}
	next.Account.Readings = kept
	next.Version = StoreSchemaVersion
	if err := writeFileAtomic(s.path, next); err != nil {
		return err
	}
	s.data = next
	return nil
}

// writeFileAtomic writes v as JSON to a temp file in path's directory, then renames it
// over path. The file mode is 0600.
func writeFileAtomic(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("usagecalc store: encode: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("usagecalc store: temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("usagecalc store: chmod: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("usagecalc store: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("usagecalc store: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("usagecalc store: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("usagecalc store: rename: %w", err)
	}
	return nil
}

// Append validates r and stores it, returning the stored record. An empty ID is assigned.
// A reading at the timestamp of an existing one replaces it and keeps its ID when r has none.
func (s *Store) Append(r Reading) (Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := validateReading(r, s.now())
	if err != nil {
		return Reading{}, err
	}
	next := s.data.clone()
	replaced := -1
	for i, old := range next.Account.Readings {
		if old.At.Equal(r.At) {
			replaced = i
			break
		}
	}
	if r.ID == "" {
		if replaced >= 0 {
			r.ID = next.Account.Readings[replaced].ID
		}
		if r.ID == "" {
			id, err := newReadingID()
			if err != nil {
				return Reading{}, fmt.Errorf("usagecalc store: reading id: %w", err)
			}
			r.ID = id
		}
	}
	if replaced >= 0 {
		next.Account.Readings[replaced] = r
	} else {
		next.Account.Readings = append(next.Account.Readings, r)
	}
	sortReadings(next.Account.Readings)
	if err := s.save(next); err != nil {
		return Reading{}, err
	}
	return copyReading(r), nil
}

// List returns the readings at or after since, oldest first. A zero since returns all.
func (s *Store) List(since time.Time) []Reading {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Reading{}
	for _, r := range s.data.Account.Readings {
		if since.IsZero() || !r.At.Before(since) {
			out = append(out, copyReading(r))
		}
	}
	return out
}

// Delete removes the reading with the given ID, for fixing a typo.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data.clone()
	for i, r := range next.Account.Readings {
		if id != "" && r.ID == id {
			next.Account.Readings = append(next.Account.Readings[:i], next.Account.Readings[i+1:]...)
			return s.save(next)
		}
	}
	return fmt.Errorf("%w: %q", ErrReadingNotFound, id)
}

// SetExtraCredit stores the bought extra credit, in points, and stamps the update time.
func (s *Store) SetExtraCredit(points float64) error {
	if !finite(points) || points < 0 || points > maxPoints {
		return fmt.Errorf("%w: extra credit must be 0 or more", ErrInvalidValue)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data.clone()
	next.Account.ExtraCreditPoints = points
	next.Account.ExtraCreditUpdatedAt = s.now().UTC()
	return s.save(next)
}

// SetRemainingWork stores the typed remaining-work figure, or clears it when work is nil.
// The figure is local to this daemon.
func (s *Store) SetRemainingWork(work *float64) error {
	if work != nil && (!finite(*work) || *work < 0 || *work > maxPoints) {
		return fmt.Errorf("%w: remaining work must be 0 or more", ErrInvalidValue)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data.clone()
	next.Local.RemainingWork = copyFloat(work)
	return s.save(next)
}

// Snapshot returns a copy of the stored state.
func (s *Store) Snapshot() StoreSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.data.clone()
	return StoreSnapshot(c)
}

// FilePath returns the store file's path, so a SharedSeries can keep its peer copy right
// next to it.
func (s *Store) FilePath() string { return s.path }
