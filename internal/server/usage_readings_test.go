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
	// The POST reply carries the stored reading, with the id the store assigned:
	// an operator adding one reading can delete the exact one by that id.
	var addResp struct {
		OK      bool               `json:"ok"`
		Reading *usagecalc.Reading `json:"reading"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&addResp); err != nil {
		t.Fatalf("decode POST reply: %v (body=%s)", err, rr.Body.String())
	}
	if !addResp.OK || addResp.Reading == nil {
		t.Fatalf("POST reply = %s, want ok:true with the stored reading", rr.Body.String())
	}
	if addResp.Reading.ID == "" || addResp.Reading.Percent == nil || *addResp.Reading.Percent != 61.5 {
		t.Fatalf("stored reading in POST reply = %+v", addResp.Reading)
	}
	addedID := addResp.Reading.ID

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
	if readingID != addedID {
		t.Fatalf("GET id %q != POST reply id %q: the operator could not delete the exact reading", readingID, addedID)
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

// TestUsageReadingAddDefaultsTimeToNow pins the shared ingress default: a
// reading with no explicit time is stamped now. The CLI stamps its own now
// client-side; the comm and MCP surfaces carry no time of their own and the
// bare REST body omits it, so the single shared handler must apply the default
// (their documented "default: now") rather than fail the primary add path.
func TestUsageReadingAddDefaultsTimeToNow(t *testing.T) {
	dataDir := t.TempDir()
	s := newUsageTestServer(t, dataDir, "", "", "", "")
	before := time.Now().UTC()

	capReq := func(body string) *httptest.ResponseRecorder {
		req := withUsageCaps(httptest.NewRequest(http.MethodPost, "/api/usage/readings", nil),
			federation.CapUsageRead, federation.CapUsageWrite)
		req.Body = io.NopCloser(strings.NewReader(body))
		rr := httptest.NewRecorder()
		s.handleUsageReadings(rr, req)
		return rr
	}

	// A percent reading with no time (what comm and MCP send, and what a bare
	// REST body looks like) is accepted and stamped now.
	wantBefore := before.Add(-time.Minute)
	wantAfter := before.Add(5 * time.Minute) // store allows up to MaxFutureSkew (5m) ahead
	rr := capReq(`{"percent":55,"source":"typed"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST without time = %d: %s (want 200, the time must default to now)", rr.Code, rr.Body.String())
	}
	var addResp struct {
		OK      bool               `json:"ok"`
		Reading *usagecalc.Reading `json:"reading"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&addResp); err != nil {
		t.Fatalf("decode POST reply: %v (body=%s)", err, rr.Body.String())
	}
	if !addResp.OK || addResp.Reading == nil || addResp.Reading.At.IsZero() {
		t.Fatalf("POST reply = %s, want a stored reading stamped with a time", rr.Body.String())
	}
	if got := addResp.Reading.At; got.Before(wantBefore) || got.After(wantAfter) {
		t.Fatalf("defaults-time reading At = %v, want a now near %v", got, before)
	}

	// An explicit time still wins: the default only fills a zero time, so an
	// operator back-dating a reading is not silently moved to now.
	backfill := before.Truncate(time.Second).Add(-24 * time.Hour)
	rr2 := capReq(`{"percent":40,"at":"` + backfill.Format(time.RFC3339) + `"}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("POST with explicit time = %d: %s (want 200, an explicit time is honored)", rr2.Code, rr2.Body.String())
	}
	var addResp2 struct {
		OK      bool               `json:"ok"`
		Reading *usagecalc.Reading `json:"reading"`
	}
	if err := json.NewDecoder(rr2.Body).Decode(&addResp2); err != nil {
		t.Fatalf("decode POST reply: %v (body=%s)", err, rr2.Body.String())
	}
	if addResp2.Reading == nil || !addResp2.Reading.At.Equal(backfill) {
		t.Fatalf("explicit-time reading At = %+v, want the back-dated %v preserved (the now default must not override an explicit time)", addResp2.Reading, backfill)
	}
	if got := addResp2.Reading.At.Sub(before).Abs(); got < 20*time.Hour {
		t.Fatalf("explicit-time reading At = %v looks like the now default %v, not the back-dated %v", addResp2.Reading.At, before, backfill)
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
	// Through the peer the reply carries the keeper's stored reading (its id),
	// so the operator can delete the exact forwarding read.
	var peerAdd struct {
		OK      bool               `json:"ok"`
		Reading *usagecalc.Reading `json:"reading"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&peerAdd); err != nil {
		t.Fatalf("decode peer POST reply: %v (body=%s)", err, rr.Body.String())
	}
	if !peerAdd.OK || peerAdd.Reading == nil || peerAdd.Reading.ID == "" {
		t.Fatalf("peer POST reply = %s, want ok:true with the keeper's stored reading id", rr.Body.String())
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

// TestUsageReadingResponseCarriesLaneStamp checks the REST shape of a stored reading: it
// carries lanes and lanes_source, and a lanes value an older caller sends is accepted
// (200) and ignored.
func TestUsageReadingResponseCarriesLaneStamp(t *testing.T) {
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")
	s.inferenceReg = laneFakeRegistry(t)
	s.SetCapacity(laneFakeLedger(t, "task-a", "task-b"))

	post := func(body string) map[string]any {
		req := withUsageCaps(httptest.NewRequest(http.MethodPost, "/api/usage/readings", nil),
			federation.CapUsageRead, federation.CapUsageWrite)
		req.Body = io.NopCloser(strings.NewReader(body))
		rr := httptest.NewRecorder()
		s.handleUsageReadings(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("POST %s = %d: %s", body, rr.Code, rr.Body.String())
		}
		var resp struct {
			Reading map[string]any `json:"reading"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil || resp.Reading == nil {
			t.Fatalf("decode reply: %v (body=%s)", err, rr.Body.String())
		}
		return resp.Reading
	}

	for _, body := range []string{
		`{"percent":50,"source":"typed"}`,
		`{"percent":51,"source":"typed","lanes":7}`,
		`{"percent":52,"source":"typed","lanes":"not a number"}`, // accepted without validation
	} {
		got := post(body)
		if lanes, ok := got["lanes"].(float64); !ok || lanes != 2 {
			t.Fatalf("POST %s: lanes = %v, want the stamped 2", body, got["lanes"])
		}
		if got["lanes_source"] != usagecalc.LanesLive {
			t.Fatalf("POST %s: lanes_source = %v, want %q", body, got["lanes_source"], usagecalc.LanesLive)
		}
	}
}
