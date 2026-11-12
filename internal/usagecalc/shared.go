package usagecalc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// HeaderUsageHop marks a usage-readings request that already went through one
	// forwarding hop: a daemon that receives it must answer from its own local store,
	// whatever its own keeper setting says. That is what makes a loop of peered daemons
	// terminate at the keeper.
	HeaderUsageHop = "X-Usage-Hop"

	sharedHopValue     = "1"
	sharedFetchTimeout = 3 * time.Second
	sharedMaxResponse  = 512 * 1024
	sharedPeerFileName = "usage.peer-copy.json"
)

// Shared errors.
var (
	// ErrKeeperUnreachable: a write forwarded to the keeper could not reach it (the
	// connection failed, the name is not a registered peer, or the keeper denied the
	// write). The write is refused; nothing is stored on the peer's local store, so the
	// series cannot split.
	ErrKeeperUnreachable = errors.New("usage keeper unreachable")

	// ErrNoPeerCopy: on a peer, the keeper could not be read and there is no last copy on
	// disk to fall back to.
	ErrNoPeerCopy = errors.New("usage series: no local copy and the keeper is unreachable")
)

// PeerLookup resolves a registered peer name to the base URL and bearer token of its REST
// API, mirroring the signature the daemon's multiserver store already exposes.
type PeerLookup func(name string) (baseURL, token string, ok bool)

// AccountRecord is the wire record for one account's shared readings series, the body of
// GET /api/usage/readings on the keeper and the payload a peer forwards on the write
// endpoints. Keeper is the peer name that owns the series (""); UpdatedAt is the keeper's
// newest update stamp.
type AccountRecord struct {
	Readings          []Reading `json:"readings"`
	ExtraCreditPoints float64   `json:"extra_credit_points"`
	UpdatedAt         time.Time `json:"updated_at"`
	Keeper            string    `json:"keeper"`
}

// Snapshot is what a daemon sees when it reads the shared series:
//
//	IsKeeper      true when this daemon owns the series (empty keeper setting) and the
//	              record came from its own store.
//	Stale/...     on a peer, true when the keeper could not be read and the record is the
//	              last copy found on disk (Stale says why, CopiedAt when it was taken).
//	CopiedAt      when the record was fetched from the keeper.
type Snapshot struct {
	Record      AccountRecord
	Keeper      string
	IsKeeper    bool
	Stale       bool
	StaleReason string
	CopiedAt    time.Time
}

// SharedSeries serves one account's readings series across federated daemons. The daemon
// whose keeper setting is empty keeps the series in its local store and answers peer
// reads and writes; every other daemon reads through the keeper and forwards writes to it,
// holding a last copy on disk to serve when the keeper is down (decision 8).
type SharedSeries struct {
	store  *Store
	keeper func() string // peer name that keeps the series; empty: this daemon
	lookup PeerLookup
	client *http.Client
	now    func() time.Time
	stamp  LaneStamper // keeper only; nil stamps every reading unavailable
}

// SharedOption adjusts a SharedSeries at construction time.
type SharedOption func(*SharedSeries)

// WithSharedClock replaces the default wall clock.
func WithSharedClock(now func() time.Time) SharedOption {
	return func(s *SharedSeries) {
		if now != nil {
			s.now = now
		}
	}
}

// WithSharedClient replaces the default HTTP client.
func WithSharedclient(c *http.Client) SharedOption {
	return func(s *SharedSeries) {
		if c != nil {
			s.client = c
		}
	}
}

// WithLaneStamper sets the stamper the keeper calls for each reading it stores.
func WithLaneStamper(stamp LaneStamper) SharedOption {
	return func(s *SharedSeries) { s.stamp = stamp }
}

// NewSharedSeries wires a store to the keeper named by keeper (empty: this daemon is the
// keeper). lookup resolves peer names to their REST API; it is called only when the keeper
// is another daemon.
func NewSharedSeries(store *Store, keeper string, lookup PeerLookup, opts ...SharedOption) *SharedSeries {
	s := &SharedSeries{
		store:  store,
		keeper: func() string { return keeper },
		lookup: lookup,
		client: &http.Client{Timeout: sharedFetchTimeout},
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Keeper returns the peer name that keeps this daemon's series, or "" when the daemon
// itself is the keeper.
func (s *SharedSeries) Keeper() string {
	if s.keeper == nil {
		return ""
	}
	return strings.TrimSpace(s.keeper())
}

// isKeeper reports whether this daemon keeps the series.
func (s *SharedSeries) isKeeper() bool { return s.Keeper() == "" }

// HasHopHeader reports whether an incoming request carries the keeper-forwarding marker.
// The REST layer must serve such a request from the local store via RecordFromLocal, no
// matter what this daemon's own keeper setting says.
func HasHopHeader(r *http.Request) bool {
	return r.Header.Get(HeaderUsageHop) == sharedHopValue
}

// RecordFromLocal returns the local store's state as the account record. It is the
// hop-gated answer: requests carrying X-Usage-Hop:1 always see the local store, even on a
// peer whose keeper is some other daemon.
func (s *SharedSeries) RecordFromLocal() AccountRecord {
	st := s.store.Snapshot()
	rec := AccountRecord{
		Readings:          nonNilReadings(st.Account.Readings),
		ExtraCreditPoints: st.Account.ExtraCreditPoints,
		Keeper:            s.Keeper(),
	}
	// Newest update stamp the store knows: the latest reading's wall time, or the extra
	// credit's stamp, whichever is newer.
	var newest time.Time
	if n := len(rec.Readings); n > 0 {
		newest = rec.Readings[n-1].At
	}
	if st.Account.ExtraCreditUpdatedAt.After(newest) {
		newest = st.Account.ExtraCreditUpdatedAt
	}
	rec.UpdatedAt = newest
	return rec
}

func nonNilReadings(rs []Reading) []Reading {
	if rs == nil {
		return []Reading{}
	}
	return rs
}

// peerCopy is the on-disk last copy a peer keeps of the keeper's record.
type peerCopy struct {
	CopiedAt time.Time     `json:"copied_at"`
	Keeper   string        `json:"keeper"`
	Record   AccountRecord `json:"record"`
}

// copyPath is the peer copy file, right next to the store file.
func (s *SharedSeries) copyPath() string {
	return filepath.Join(filepath.Dir(s.store.FilePath()), sharedPeerFileName)
}

// Snapshot reads the shared series for this daemon. As keeper it is the local store's
// state. As a peer it is the keeper's record, freshly fetched and stored as this daemon's
// last copy; when the keeper cannot be read it falls back to the last copy, marked Stale,
// or fails with ErrNoPeerCopy when there is none.
func (s *SharedSeries) Snapshot(ctx context.Context) (Snapshot, error) {
	if s.isKeeper() {
		return Snapshot{Record: s.RecordFromLocal(), IsKeeper: true}, nil
	}

	keeper := s.Keeper()
	base, token, ok := s.lookupPeer(keeper)
	var (
		rec AccountRecord
		fet error
	)
	if ok {
		rec, fet = s.fetchRecord(ctx, keeper, base, token)
	} else {
		fet = fmt.Errorf("keeper %q is not a registered peer", keeper)
	}
	if fet != nil {
		copy, cerr := s.readCopy()
		if cerr == nil {
			return Snapshot{
				Record:      nonNilRecord(copy.Record),
				Keeper:      keeper,
				Stale:       true,
				StaleReason: fet.Error(),
				CopiedAt:    copy.CopiedAt,
			}, nil
		}
		return Snapshot{}, fmt.Errorf("%w: %v (no local copy: %v)", ErrNoPeerCopy, fet, cerr)
	}

	copiedAt := s.now().UTC()
	copy := peerCopy{CopiedAt: copiedAt, Keeper: keeper, Record: rec}
	if err := s.writeCopy(copy); err != nil {
		return Snapshot{}, fmt.Errorf("usage series: store the last copy: %w", err)
	}
	return Snapshot{Record: nonNilRecord(rec), Keeper: keeper, CopiedAt: copiedAt}, nil
}

func nonNilRecord(rec AccountRecord) AccountRecord {
	rec.Readings = nonNilReadings(rec.Readings)
	return rec
}

// lookupPeer resolves the keeper name through the configured lookup.
func (s *SharedSeries) lookupPeer(keeper string) (string, string, bool) {
	if s.lookup == nil {
		return "", "", false
	}
	base, token, ok := s.lookup(keeper)
	return strings.TrimRight(strings.TrimSpace(base), "/"), token, ok
}

// fetchRecord gets the record from the keeper, tagged with the hop marker.
func (s *SharedSeries) fetchRecord(ctx context.Context, keeper, baseURL, token string) (AccountRecord, error) {
	if err := ctx.Err(); err != nil {
		return AccountRecord{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/usage/readings", nil)
	if err != nil {
		return AccountRecord{}, fmt.Errorf("keeper %q unreachable: %v", keeper, err)
	}
	req.Header.Set(HeaderUsageHop, sharedHopValue)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return AccountRecord{}, fmt.Errorf("keeper %q unreachable: %v", keeper, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return AccountRecord{}, fmt.Errorf("keeper %q answered HTTP %d", keeper, resp.StatusCode)
	}
	var rec AccountRecord
	if err := json.NewDecoder(io.LimitReader(resp.Body, sharedMaxResponse)).Decode(&rec); err != nil {
		return AccountRecord{}, fmt.Errorf("keeper %q sent an unreadable record", keeper)
	}
	return nonNilRecord(rec), nil
}

// AddReading appends a reading to the series and returns the stored reading, with the id
// the store assigned. As keeper the local store gets it; as a peer the reading is
// forwarded to the keeper by POST /api/usage/readings, which answers with the stored
// reading (the wire shape the daemon's handler emits). Nothing is stored locally when the
// keeper is unreachable: ErrKeeperUnreachable.
func (s *SharedSeries) AddReading(ctx context.Context, r Reading) (Reading, error) {
	if s.isKeeper() {
		// The keeper alone decides the lane stamp; a value the caller sent is overwritten.
		r.Lanes, r.LanesSource = nil, LanesUnavailable
		if s.stamp != nil {
			r.Lanes, r.LanesSource = s.stamp(r.At, s.now())
		}
		return s.store.Append(r)
	}
	// A peer carries no ledger of the keeper's, so it sends the reading without a stamp.
	r.Lanes, r.LanesSource = nil, ""
	body, err := json.Marshal(r)
	if err != nil {
		return Reading{}, fmt.Errorf("usage series: encode the reading: %w", err)
	}
	respBody, err := s.writeToKeeper(ctx, http.MethodPost, "/api/usage/readings", map[string]string{"Content-Type": "application/json"}, body)
	if err != nil {
		return Reading{}, err
	}
	// A keeper without the field (the old {"ok":true} shape) still confirms the write.
	var out struct {
		Reading *Reading `json:"reading,omitempty"`
	}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &out); err != nil { // a non-JSON confirmation is still a success
			out.Reading = nil
		}
	}
	if out.Reading == nil {
		return Reading{}, nil
	}
	return *out.Reading, nil
}

// DeleteReading removes a reading by ID. As keeper the local store loses it; as a peer the
// delete is forwarded to the keeper by DELETE /api/usage/readings/{id}.
func (s *SharedSeries) DeleteReading(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: the reading id is empty", ErrInvalidValue)
	}
	if s.isKeeper() {
		return s.store.Delete(id)
	}
	_, err := s.writeToKeeper(ctx, http.MethodDelete, "/api/usage/readings/"+url.PathEscape(id), nil, nil)
	return err
}

// SetExtraCredit sets the bought extra credit. As keeper the local store gets it; as a
// peer the figure is forwarded to the keeper by PUT /api/usage/extra-credit.
func (s *SharedSeries) SetExtraCredit(ctx context.Context, points float64) error {
	if !finite(points) || points < 0 || points > maxPoints {
		return fmt.Errorf("%w: extra credit must be 0 or more", ErrInvalidValue)
	}
	if s.isKeeper() {
		return s.store.SetExtraCredit(points)
	}
	body, err := json.Marshal(struct {
		Points float64 `json:"points"`
	}{points})
	if err != nil {
		return fmt.Errorf("usage series: encode extra credit: %w", err)
	}
	_, err = s.writeToKeeper(ctx, http.MethodPut, "/api/usage/extra-credit", map[string]string{"Content-Type": "application/json"}, body)
	return err
}

// writeToKeeper forwards a write to the keeper. An unreachable or denying keeper yields
// ErrKeeperUnreachable (the caller must treat the write as refused; the local store is
// untouched).
func (s *SharedSeries) writeToKeeper(ctx context.Context, method, path string, hdr map[string]string, body []byte) ([]byte, error) {
	keeper := s.Keeper()
	base, token, ok := s.lookupPeer(keeper)
	if !ok {
		return nil, fmt.Errorf("%w: keeper %q is not a registered peer", ErrKeeperUnreachable, keeper)
	}
	var rdr io.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeeperUnreachable, err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeeperUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return respBody, fmt.Errorf("keeper %q answered HTTP %d to the write", keeper, resp.StatusCode)
	}
	return respBody, nil
}

// readCopy returns the last copy on disk.
func (s *SharedSeries) readCopy() (peerCopy, error) {
	raw, err := os.ReadFile(s.copyPath())
	if err != nil {
		return peerCopy{}, err
	}
	var copy peerCopy
	if err := json.Unmarshal(raw, &copy); err != nil {
		return peerCopy{}, err
	}
	return copy, nil
}

// writeCopy stores the last copy atomically.
func (s *SharedSeries) writeCopy(copy peerCopy) error {
	return writeFileAtomic(s.copyPath(), copy)
}
