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
//	POST   /api/usage/readings           usage:write  append one reading
//	DELETE /api/usage/readings/{id}      usage:write  delete one reading
//	PUT    /api/usage/extra-credit       usage:write  set the bought extra credit
//
// Reads served from the last on-disk copy because the keeper could not be reached are
// flagged with the X-Usage-Stale / X-Usage-Copied-At response headers; the body always
// stays the bare account record so a peer and a local operator see the same shape.
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

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// usageSeriesCache holds the lazily-built shared series and rebuilds it when the
// keeper setting changes at runtime (the config can be patched live).
type usageSeriesCache struct {
	mu     sync.Mutex
	keeper string
	ready  bool
	series *usagecalc.SharedSeries
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
	series, err := s.buildUsageSeries(keeper)
	if err != nil {
		return nil, fmt.Errorf("usage series unavailable: %v", err)
	}
	cache.series = series
	cache.keeper = keeper
	cache.ready = true
	return series, nil
}

func (s *Server) buildUsageSeries(keeper string) (*usagecalc.SharedSeries, error) {
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
		return nil, err
	}
	return usagecalc.NewSharedSeries(store, keeper, s.usagePeerLookup), nil
}

// usagePeerLookup resolves a registered peer's REST API, the PeerLookup seam the
// shared series uses to find the keeper.
func (s *Server) usagePeerLookup(name string) (string, string, bool) {
	if s.serverStore == nil {
		return "", "", false
	}
	return s.serverStore.GetByName(name)
}

// handleUsageReadings serves the shared series record (GET) and appends a reading (POST).
func (s *Server) handleUsageReadings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleUsageSeriesRead(w, r)
	case http.MethodPost:
		s.handleUsageReadingAdd(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUsageSeriesRead(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapUsageRead) {
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snap.Record)
}

func (s *Server) handleUsageReadingAdd(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapUsageWrite) {
		return
	}
	series, err := s.usageSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	var in usagecalc.Reading
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, "invalid reading: "+err.Error(), http.StatusBadRequest)
		return
	}
	err = series.AddReading(r.Context(), in)
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	case errors.Is(err, usagecalc.ErrKeeperUnreachable):
		http.Error(w, "usage series: the keeper refused or could not be reached; nothing was stored", http.StatusServiceUnavailable)
	case errors.Is(err, usagecalc.ErrInvalidReading), errors.Is(err, usagecalc.ErrInvalidValue):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// handleUsageReadingDelete serves DELETE /api/usage/readings/{id}.
func (s *Server) handleUsageReadingDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapUsageWrite) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/usage/readings/")
	id = strings.Trim(id, "/")
	if unescaped, err := url.PathUnescape(id); err == nil {
		id = unescaped
	}
	if id == "" {
		http.Error(w, "a reading id is required", http.StatusBadRequest)
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
	if !s.fedCap(w, r, federation.CapUsageWrite) {
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	case errors.Is(err, usagecalc.ErrKeeperUnreachable):
		http.Error(w, "usage series: the keeper refused or could not be reached; nothing was stored", http.StatusServiceUnavailable)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
