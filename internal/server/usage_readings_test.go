package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// newUsageTestServer builds a minimal Server whose shared series reads from
// (and, as keeper, writes to) the usage store rooted at dataDir.
func newUsageTestServer(t *testing.T, dataDir, keeper, peerName, peerURL, peerToken string) *Server {
	t.Helper()
	cfg := &config.Config{DataDir: dataDir}
	if keeper != "" {
		cfg.Usage.Calculator.Keeper = keeper
	}
	s := &Server{hostname: "testhost", cfg: cfg}
	if peerName != "" {
		store, err := multiserver.NewStore(t.TempDir(), nil)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		if err := store.Add(&multiserver.Entry{
			Name: peerName, URL: peerURL, Token: peerToken,
			Enabled: true, Federated: true,
		}); err != nil {
			t.Fatalf("Add peer: %v", err)
		}
		s.serverStore = store
	}
	return s
}

// withUsageCaps simulates the fed-auth middleware having resolved the caller's
// capabilities (Design A3 sessions / peer grants).
func withUsageCaps(r *http.Request, caps ...string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sessionCapsKey, caps))
}

func readUsageRecord(t *testing.T, rr *httptest.ResponseRecorder) usagecalc.AccountRecord {
	t.Helper()
	var rec usagecalc.AccountRecord
	if err := json.NewDecoder(rr.Body).Decode(&rec); err != nil {
		t.Fatalf("decode record: %v (body=%s)", err, rr.Body.String())
	}
	return rec
}

func postReading(t *testing.T, s *Server, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(usagecalc.Reading{
		At:      time.Now().UTC().Add(-time.Hour),
		Percent: fptrUsage(61.5),
		Source:  usagecalc.SourceTyped,
	})
	r.Body = io.NopCloser(bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleUsageReadings(rr, r)
	return rr
}

func fptrUsage(v float64) *float64 { return &v }

func TestUsageSeriesKeeperLocalCRUD(t *testing.T) {
	dataDir := t.TempDir()
	s := newUsageTestServer(t, dataDir, "", "", "", "")

	// POST a reading.
	rr := postReading(t, s, withUsageCaps(httptest.NewRequest(http.MethodPost, "/api/usage/readings", nil),
		federation.CapUsageRead, federation.CapUsageWrite))
	if rr.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", rr.Code, rr.Body.String())
	}

	// GET returns it in the bare record shape, stamped non-stale, and the hop-
	// marked request (what a peer's fetch always sends) sees the same local store.
	req := withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil), federation.CapUsageRead)
	rrw := httptest.NewRecorder()
	s.handleUsageReadings(rrw, req)
	if rrw.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rrw.Code, rrw.Body.String())
	}
	rec := readUsageRecord(t, rrw)
	if len(rec.Readings) != 1 || rec.Readings[0].Percent == nil || *rec.Readings[0].Percent != 61.5 {
		t.Fatalf("record = %+v, want one 61.5 reading", rec)
	}
	readingID := rec.Readings[0].ID
	if readingID == "" {
		t.Fatal("the appended reading must carry an id")
	}

	hopReq := httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil)
	hopReq.Header.Set(usagecalc.HeaderUsageHop, "1")
	hopReq = withUsageCaps(hopReq, federation.CapUsageRead)
	hopRr := httptest.NewRecorder()
	s.handleUsageReadings(hopRr, hopReq)
	hopRec := readUsageRecord(t, hopRr)
	if hopRr.Code != http.StatusOK || len(hopRec.Readings) != 1 || hopRec.Readings[0].ID != readingID {
		t.Fatalf("hop-marked GET = %d %+v, want the local reading", hopRr.Code, hopRec)
	}

	// PUT the extra credit; the record reflects it.
	creditReq := withUsageCaps(httptest.NewRequest(http.MethodPut, "/api/usage/extra-credit",
		strings.NewReader(`{"points": 25}`)), federation.CapUsageWrite)
	creditReq.Body = io.NopCloser(strings.NewReader(`{"points": 25}`))
	creditRr := httptest.NewRecorder()
	s.handleUsageExtraCredit(creditRr, creditReq)
	if creditRr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", creditRr.Code, creditRr.Body.String())
	}
	s.handleUsageReadings(rrw, withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil), federation.CapUsageRead))
	rec = readUsageRecord(t, rrw)
	if rec.ExtraCreditPoints != 25 {
		t.Fatalf("credit = %v, want 25", rec.ExtraCreditPoints)
	}

	// DELETE the reading; the record is empty again.
	delReq := withUsageCaps(httptest.NewRequest(http.MethodDelete, "/api/usage/readings/"+readingID, nil), federation.CapUsageWrite)
	delRr := httptest.NewRecorder()
	s.handleUsageReadingDelete(delRr, delReq)
	if delRr.Code != http.StatusOK {
		t.Fatalf("DELETE = %d: %s", delRr.Code, delRr.Body.String())
	}
	s.handleUsageReadings(rrw, withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil), federation.CapUsageRead))
	rec = readUsageRecord(t, rrw)
	if len(rec.Readings) != 0 {
		t.Fatalf("after delete, record = %+v, want no readings", rec)
	}
}

func TestUsageSeriesCapabilityGates(t *testing.T) {
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")

	// No usage:read — the series is invisible.
	req := httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil).
		WithContext(context.WithValue(context.Background(), sessionCapsKey, []string{federation.CapHealthRead}))
	rr := httptest.NewRecorder()
	s.handleUsageReadings(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("GET without usage:read = %d, want 403", rr.Code)
	}

	// usage:read only — reads work, writes are refused.
	req = withUsageCaps(httptest.NewRequest(http.MethodPost, "/api/usage/readings", strings.NewReader(`{}`)), federation.CapUsageRead)
	rr = httptest.NewRecorder()
	s.handleUsageReadings(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("POST with usage:read only = %d, want 403", rr.Code)
	}
	req = withUsageCaps(httptest.NewRequest(http.MethodPut, "/api/usage/extra-credit", strings.NewReader(`{"points":1}`)), federation.CapUsageRead)
	rr = httptest.NewRecorder()
	s.handleUsageExtraCredit(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("PUT with usage:read only = %d, want 403", rr.Code)
	}
	req = withUsageCaps(httptest.NewRequest(http.MethodDelete, "/api/usage/readings/x", nil), federation.CapUsageRead)
	rr = httptest.NewRecorder()
	s.handleUsageReadingDelete(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("DELETE with usage:read only = %d, want 403", rr.Code)
	}

	// usage:write works for writes.
	req = withUsageCaps(httptest.NewRequest(http.MethodPut, "/api/usage/extra-credit", strings.NewReader(`{"points":1}`)), federation.CapUsageWrite)
	rr = httptest.NewRecorder()
	s.handleUsageExtraCredit(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT with usage:write = %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUsageSeriesPeerServesKeeperAndStaleCopy(t *testing.T) {
	dataDir := t.TempDir()

	// The keeper, serving its own local store.
	keeperStoreDir := t.TempDir()
	keeper := newUsageTestServer(t, keeperStoreDir, "", "", "", "")
	if err := keeper.seriesAppendLocal(t); err != nil {
		t.Fatalf("keeper seed: %v", err)
	}
	ks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), sessionCapsKey, []string{
			federation.CapUsageRead, federation.CapUsageWrite,
		}))
		switch {
		case r.URL.Path == "/api/usage/readings" && r.Method == http.MethodPost:
			keeper.handleUsageReadings(w, r)
		case r.URL.Path == "/api/usage/readings" && r.Method == http.MethodGet:
			keeper.handleUsageReadings(w, r)
		case r.URL.Path == "/api/usage/extra-credit":
			keeper.handleUsageExtraCredit(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/usage/readings/"):
			keeper.handleUsageReadingDelete(w, r)
		}
	}))
	defer ks.Close()
	keeperURL := ks.URL

	// The peer, keeper = alpha.
	peer := newUsageTestServer(t, dataDir, "alpha", "alpha", keeperURL, "alpha-token")

	// Fresh read: the peer's response is the keeper's record and lands on the wire
	// with the hop marker (the peer must not re-forward).
	rr := httptest.NewRecorder()
	peer.handleUsageReadings(rr, withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil),
		federation.CapUsageRead, federation.CapUsageWrite))
	if rr.Code != http.StatusOK {
		t.Fatalf("peer GET = %d: %s", rr.Code, rr.Body.String())
	}
	rec := readUsageRecord(t, rr)
	if rr.Header().Get("X-Usage-Stale") != "" {
		t.Fatalf("fresh read must not be stale; got %q", rr.Header().Get("X-Usage-Stale"))
	}
	if len(rec.Readings) != 1 {
		t.Fatalf("peer record = %+v, want the keeper's one reading", rec)
	}

	// A write through the peer reaches the keeper: the keeper's store gains the reading and
	// the peer's own local store stays untouched.
	rr = postReading(t, peer, withUsageCaps(httptest.NewRequest(http.MethodPost, "/api/usage/readings", nil),
		federation.CapUsageRead, federation.CapUsageWrite))
	if rr.Code != http.StatusOK {
		t.Fatalf("peer POST = %d: %s", rr.Code, rr.Body.String())
	}
	peerLocal := httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil)
	peerLocal.Header.Set(usagecalc.HeaderUsageHop, "1")
	peerLocal = withUsageCaps(peerLocal, federation.CapUsageRead)
	localRr := httptest.NewRecorder()
	peer.handleUsageReadings(localRr, peerLocal)
	localRec := readUsageRecord(t, localRr)
	if len(localRec.Readings) != 0 {
		t.Fatalf("the peer stored the forwarded reading locally: %+v", localRec)
	}
	rr = httptest.NewRecorder()
	peer.handleUsageReadings(rr, withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil),
		federation.CapUsageRead))
	rec = readUsageRecord(t, rr)
	if len(rec.Readings) != 2 {
		t.Fatalf("after the forwarded write the keeper's record = %+v, want two readings", rec)
	}

	// Keeper down: the last copy is served, flagged stale.
	ks.Close()
	rr = httptest.NewRecorder()
	peer.handleUsageReadings(rr, withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil),
		federation.CapUsageRead))
	if rr.Code != http.StatusOK {
		t.Fatalf("stale GET = %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-Usage-Stale") == "" {
		t.Fatalf("keeper-down GET must carry X-Usage-Stale; got %q", rr.Header().Get("X-Usage-Stale"))
	}
	rec = readUsageRecord(t, rr)
	if len(rec.Readings) != 2 {
		t.Fatalf("stale record = %+v, want the last copy (two readings)", rec)
	}

	// A write with the keeper down is refused and stores nothing locally.
	rr = postReading(t, peer, withUsageCaps(httptest.NewRequest(http.MethodPost, "/api/usage/readings", nil),
		federation.CapUsageRead, federation.CapUsageWrite))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("peer POST with keeper down = %d, want 503 (%s)", rr.Code, rr.Body.String())
	}
	localRr = httptest.NewRecorder()
	peer.handleUsageReadings(localRr, withUsageCaps(peerLocal.Clone(context.Background()), federation.CapUsageRead))
	localRec = readUsageRecord(t, localRr)
	if len(localRec.Readings) != 0 {
		t.Fatalf("the refused write leaked into the peer's store: %+v", localRec)
	}
}

func TestUsageSeriesPeerWithoutCopyAndKeeperDown(t *testing.T) {
	// Keeper registered but unreachable, and no local copy yet: the read fails with a
	// plain 503 rather than serving an empty fake record.
	peer := newUsageTestServer(t, t.TempDir(), "alpha", "alpha", "http://127.0.0.1:9", "tok")
	rr := httptest.NewRecorder()
	peer.handleUsageReadings(rr, withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil),
		federation.CapUsageRead))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET with no copy and keeper unreachable = %d, want 503 (%s)", rr.Code, rr.Body.String())
	}
}

// seriesAppendLocal appends one reading straight to the keeper's store, the way a local
// operator's write would land.
func (s *Server) seriesAppendLocal(t *testing.T) error {
	t.Helper()
	store, err := usagecalc.OpenStore(s.cfg.DataDir, time.Now)
	if err != nil {
		return err
	}
	_, err = store.Append(usagecalc.Reading{At: time.Now().UTC().Add(-2 * time.Hour), Percent: fptrUsage(42), Source: usagecalc.SourceTyped})
	return err
}
