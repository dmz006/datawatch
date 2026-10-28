// usage_readings.go — the REST surface of the shared usage calculator readings series
// (decision 8, "share the readings across federated daemons through a keeper").
//
// The daemon with an empty usage.calculator.keeper keeps the series in its local store
// and answers the keeper's endpoints; every other daemon reads through the keeper and
// forwards writes to it, holding a last copy on disk (usagecalc.SharedSeries).
//
//	GET    /api/usage/readings           usage:read   the record; hop-marked requests
//	                                                        (X-Usage-Hop: 1, sent by a peer's
//	                                                        fetch) always see the local store
//	POST   /api/usage/readings           usage:write  append one reading (source: typed is
//	                                                        built; pasted and account answer 501;
//	                                                        an unknown source answers 400)
//	DELETE /api/usage/readings?id=<id>   usage:write  delete one reading (operator form);
//	                                                        /api/usage/readings/{id} is the same
//	                                                        operation in the peer's wire form
//	PUT    /api/usage/extra-credit       usage:write  set the bought extra credit
//
// Reads served from the last on-disk copy because the keeper could not be
// reached are flagged with the X-Usage-Stale / X-Usage-Copied-At response
// headers and carry is_keeper / stale / stale_reason / copied_at in the body;
// the hop-marked (peer fetch) body stays the bare account record so a peer
// and the keeper see the same wire shape.
//
// Record mutations audit per AGENT.md's Audit Logging Rule: one entry per
// request, emitted by the daemon that stores the write — the keeper for the
// shared series, the local daemon for its remaining-work figure (see the
// emitters in usage_calculator.go).
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// usageSeriesCache holds the lazily-built shared series and rebuilds it when the
// keeper setting changes at runtime (the config can be patched live).
type usageSeriesCache struct {
	mu     sync.Mutex
	keeper string
	ready  bool
	series *usagecalc.SharedSeries
	// store is this daemon's local record behind the shared series (decision 6:
	// the typed remaining-work figure is daemon-local). Built with the series.
	store *usagecalc.Store
}

func (s *Server) usageSeriesCache() *usageSeriesCache {
	s.usageSeriesOnce.Do(func() { s.usageSeriesCacheVal = &usageSeriesCache{} })
	return s.usageSeriesCacheVal
}

// usageSeries returns the shared series for the current keeper setting.
func (s *Server) usageSeries() (*usagecalc.SharedSeries, error) {
	keeper := ""
	if s.cfg != nil {
		keeper = strings.TrimSpace(s.cfg.Usage.Calculator.Keeper)
	}
	cache := s.usageSeriesCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.ready && cache.keeper == keeper {
		return cache.series, nil
	}
	series, store, err := s.buildUsageSeries(keeper)
	if err != nil {
		return nil, fmt.Errorf("usage series unavailable: %v", err)
	}
	cache.series = series
	cache.store = store
	cache.keeper = keeper
	cache.ready = true
	return series, nil
}

func (s *Server) buildUsageSeries(keeper string) (*usagecalc.SharedSeries, *usagecalc.Store, error) {
	dataDir := ""
	if s.cfg != nil {
		dataDir = s.cfg.DataDir
	}
	if dataDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataDir = filepath.Join(home, ".datawatch")
		}
	}
	store, err := usagecalc.OpenStore(dataDir, time.Now)
	if err != nil {
		return nil, nil, err
	}
	return usagecalc.NewSharedSeries(store, keeper, s.usagePeerLookup), store, nil
}

// usagePeerLookup resolves a registered peer's REST API, the PeerLookup seam the
// shared series uses to find the keeper.
func (s *Server) usagePeerLookup(name string) (string, string, bool) {
	if s.serverStore == nil {
		return "", "", false
	}
	return s.serverStore.GetByName(name)
}

// handleUsageReadings serves the shared series record (GET), appends a reading (POST)
// and deletes one (DELETE, ?id=<id>; the path form is routed separately).
func (s *Server) handleUsageReadings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleUsageSeriesRead(w, r)
	case http.MethodPost:
		s.handleUsageReadingAdd(w, r)
	case http.MethodDelete:
		s.deleteUsageReading(w, r, "")
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUsageSeriesRead(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, usageCapabilityRead) {
		return
	}
	series, err := s.usageSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	// Hop guard: a request marked by a peer's fetch (X-Usage-Hop: 1) always gets the
	// local store, whatever this daemon's own keeper setting is.
	if usagecalc.HasHopHeader(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(series.RecordFromLocal())
		return
	}
	snap, err := series.Snapshot(r.Context())
	if err != nil {
		if errors.Is(err, usagecalc.ErrNoPeerCopy) {
			http.Error(w, "usage series: no local copy yet and the keeper could not be read", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if snap.Stale {
		w.Header().Set("X-Usage-Stale", snap.StaleReason)
		if !snap.CopiedAt.IsZero() {
			w.Header().Set("X-Usage-Copied-At", snap.CopiedAt.UTC().Format(time.RFC3339))
		}
	}
	view := usageReadingsView{AccountRecord: snap.Record, IsKeeper: snap.IsKeeper, Stale: snap.Stale, StaleReason: snap.StaleReason}
	if !snap.CopiedAt.IsZero() {
		copiedAt := snap.CopiedAt
		view.CopiedAt = &copiedAt
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}

func (s *Server) handleUsageReadingAdd(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, usageCapabilityWrite) {
		return
	}
	var in usagecalc.Reading
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, "invalid reading: "+err.Error(), http.StatusBadRequest)
		return
	}
	if status, msg := usageSourceGate(in.Source); status != http.StatusOK {
		http.Error(w, msg, status)
		return
	}
	// A reading without an explicit time is stamped now, the shared default the
	// CLI flag, the MCP tool, and the comm grammar all promise. The store is the
	// authority and requires a stored time, so this single shared ingress applies
	// the default: the surfaces that carry no time of their own (REST, MCP, comm)
	// then match the CLI, which stamps its own now client-side.
	if in.At.IsZero() {
		in.At = time.Now().UTC()
	}
	series, err := s.usageSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	// As keeper the store appends directly and hands back the stored reading
	// (with its final time and id) for the audit entry; as a peer the read is
	// forwarded and the keeper's handler emits the single entry.
	var stored *usagecalc.Reading
	if series.Keeper() == "" {
		cache := s.usageSeriesCache()
		cache.mu.Lock()
		store := cache.store
		cache.mu.Unlock()
		rec, aerr := store.Append(in)
		err = aerr
		if aerr == nil {
			stored = &rec
		}
	} else {
		// As a peer the keeper's answer carries the stored reading (id, final time).
		// A keeper on the old {"ok":true} shape still confirms the write; the reply
		// just omits the reading.
		var rec usagecalc.Reading
		rec, err = series.AddReading(r.Context(), in)
		if err == nil {
			stored = &rec
		}
	}
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		if stored != nil {
			s.auditUsageReadingAdded(r, *stored)
			_ = json.NewEncoder(w).Encode(struct {
				OK      bool               `json:"ok"`
				Reading *usagecalc.Reading `json:"reading"`
			}{true, stored})
		} else {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	case errors.Is(err, usagecalc.ErrKeeperUnreachable):
		http.Error(w, "usage series: the keeper refused or could not be reached; nothing was stored", http.StatusServiceUnavailable)
	case errors.Is(err, usagecalc.ErrInvalidReading), errors.Is(err, usagecalc.ErrInvalidValue):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
}

// handleUsageReadingDelete serves DELETE /api/usage/readings/{id}, the peer's
// wire form the shared series forwards to its keeper.
func (s *Server) handleUsageReadingDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/usage/readings/")
	id = strings.Trim(id, "/")
	if unescaped, err := url.PathUnescape(id); err == nil {
		id = unescaped
	}
	s.deleteUsageReading(w, r, id)
}

// deleteUsageReading is the shared delete flow for both wire forms (design 8):
// the operator's /api/usage/readings?id=<id> and the peer's
// /api/usage/readings/{id}. Either form is served at the keeper and any peer;
// a peer forwards to its keeper. The keeper — the daemon that stores the write
// — emits the single audit entry (AGENT.md's Audit Logging Rule).
func (s *Server) deleteUsageReading(w http.ResponseWriter, r *http.Request, idFromPath string) {
	id := strings.TrimSpace(idFromPath)
	if id == "" {
		id = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	if !s.fedCap(w, r, usageCapabilityWrite) {
		return
	}
	if id == "" {
		http.Error(w, "a reading id is required: /api/usage/readings?id=<id>", http.StatusBadRequest)
		return
	}
	series, err := s.usageSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	err = series.DeleteReading(r.Context(), id)
	switch {
	case err == nil:
		if series.Keeper() == "" {
			s.auditUsageReadingDeleted(r, id)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	case errors.Is(err, usagecalc.ErrReadingNotFound):
		http.Error(w, "usage reading not found", http.StatusNotFound)
	case errors.Is(err, usagecalc.ErrKeeperUnreachable):
		http.Error(w, "usage series: the keeper refused or could not be reached; nothing was stored", http.StatusServiceUnavailable)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// handleUsageExtraCredit serves PUT /api/usage/extra-credit.
func (s *Server) handleUsageExtraCredit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, usageCapabilityWrite) {
		return
	}
	series, err := s.usageSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	var in struct {
		Points float64 `json:"points"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, "an extra credit figure is required: "+err.Error(), http.StatusBadRequest)
		return
	}
	err = series.SetExtraCredit(r.Context(), in.Points)
	switch {
	case err == nil:
		if series.Keeper() == "" {
			s.auditUsageExtraCreditSet(r, in.Points)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	case errors.Is(err, usagecalc.ErrKeeperUnreachable):
		http.Error(w, "usage series: the keeper refused or could not be reached; nothing was stored", http.StatusServiceUnavailable)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
