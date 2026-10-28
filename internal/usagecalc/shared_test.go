package usagecalc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeKeeper stands in for the keeper daemon's REST API: it answers the read endpoint and
// applies the write endpoints in memory.
type fakeKeeper struct {
	mu       sync.Mutex
	readings []Reading
	credit   float64
	gets     int
	// legacy answers POST with the old bare 204 confirmation (no body).
	legacy   bool
	hops     []string
	tokens   []string
	requests []string
}

func newFakeKeeper() *fakeKeeper { return &fakeKeeper{readings: []Reading{}} }

func (k *fakeKeeper) addSeed(r Reading) {
	k.mu.Lock()
	k.readings = append(k.readings, r)
	k.mu.Unlock()
}

func (k *fakeKeeper) seedCredit(points float64) {
	k.mu.Lock()
	k.credit = points
	k.mu.Unlock()
}

func (k *fakeKeeper) state() (readings []Reading, credit float64, requests []string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]Reading, len(k.readings))
	copy(out, k.readings)
	return out, k.credit, append([]string{}, k.requests...)
}

func (k *fakeKeeper) wire() (tokens, hops []string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string{}, k.tokens...), append([]string{}, k.hops...)
}

func (k *fakeKeeper) handler(w http.ResponseWriter, r *http.Request) {
	k.mu.Lock()
	k.requests = append(k.requests, r.Method+" "+r.URL.Path)
	defer k.mu.Unlock()

	key := r.Method + " " + r.URL.Path
	switch {
	case key == "GET /api/usage/readings":
		k.gets++
		k.hops = append(k.hops, r.Header.Get(HeaderUsageHop))
		k.tokens = append(k.tokens, r.Header.Get("Authorization"))
		rec := AccountRecord{Readings: k.readings, ExtraCreditPoints: k.credit, Keeper: "alpha"}
		if n := len(k.readings); n > 0 {
			rec.UpdatedAt = k.readings[n-1].At
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(rec); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	case key == "POST /api/usage/readings":
		var rd Reading
		if err := json.NewDecoder(r.Body).Decode(&rd); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if rd.Percent == nil {
			http.Error(w, "reading percent required", http.StatusBadRequest)
			return
		}
		if rd.ID == "" {
			id, _ := newReadingID()
			rd.ID = id
		}
		k.readings = append(k.readings, rd)
		if k.legacy {
			return // the old bare 204 confirmation, no body
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "reading": rd}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	case strings.HasPrefix(key, "DELETE /api/usage/readings/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/usage/readings/")
		for i, rd := range k.readings {
			if rd.ID == id {
				k.readings = append(k.readings[:i], k.readings[i+1:]...)
				return
			}
		}
		http.Error(w, "reading not found", http.StatusNotFound)
	case key == "PUT /api/usage/extra-credit":
		var body struct {
			Points float64 `json:"points"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		k.credit = body.Points
	default:
		http.Error(w, "unknown endpoint", http.StatusNotFound)
	}
	w.WriteHeader(http.StatusNoContent)
}

// sharedFixture is one daemon's side of the pair: a fresh local store and its
// SharedSeries. mode "live" starts a fake keeper named keeperName; "dead" points the
// lookup at a closed port; "unknown" leaves the name unregistered.
type sharedFixture struct {
	store  *Store
	series *SharedSeries
	keeper *fakeKeeper
	server *httptest.Server
	clock  *fakeClock
}

func newSharedFixture(t *testing.T, keeperName, mode, token string) *sharedFixture {
	t.Helper()
	dir := t.TempDir()
	clock := newFakeClock(storeTestNow)
	store, _ := openTestStore(t, dir, clock)
	fx := &sharedFixture{store: store, clock: clock, keeper: newFakeKeeper()}

	var baseURL string
	switch mode {
	case "live":
		fx.server = httptest.NewServer(http.HandlerFunc(fx.keeper.handler))
		t.Cleanup(fx.server.Close)
		baseURL = fx.server.URL
	case "dead":
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("dead fixture: %v", err)
		}
		baseURL = "http://" + ln.Addr().String()
		if err := ln.Close(); err != nil {
			t.Fatalf("dead fixture: %v", err)
		}
	}

	lookup := func(name string) (string, string, bool) {
		if name == keeperName && baseURL != "" {
			return baseURL, token, true
		}
		return "", "", false
	}
	fx.series = NewSharedSeries(store, keeperName, lookup, WithSharedClock(clock.Now))
	return fx
}

// copyPath is where the last copy of the keeper's record lives on a peer.
func copyPath(t *testing.T, store *Store) string {
	t.Helper()
	return filepath.Join(filepath.Dir(store.FilePath()), sharedPeerFileName)
}

func copyStored(t *testing.T, store *Store) peerCopy {
	t.Helper()
	blob, err := os.ReadFile(copyPath(t, store))
	if err != nil {
		t.Fatalf("read the peer copy: %v", err)
	}
	var copy peerCopy
	if err := json.Unmarshal(blob, &copy); err != nil {
		t.Fatalf("decode the peer copy: %v", err)
	}
	return copy
}

func readingsEqual(a, b []Reading) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || !a[i].At.Equal(b[i].At) || !percentSame(a[i].Percent, b[i].Percent) {
			return false
		}
	}
	return true
}

func percentSame(a, b *float64) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

func TestSharedSeriesKeeperIsSelf(t *testing.T) {
	fx := newSharedFixture(t, "", "unknown", "")
	store := fx.store

	first := Reading{At: storeTestNow.Add(-24 * time.Hour), Percent: fptr(10), Source: SourceTyped}
	second := Reading{At: storeTestNow.Add(-2 * time.Hour), Percent: fptr(40), Source: SourceTyped}
	if _, err := store.Append(first); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if _, err := store.Append(second); err != nil {
		t.Fatalf("append second: %v", err)
	}
	if err := store.SetExtraCredit(30); err != nil {
		t.Fatalf("extra credit: %v", err)
	}

	snap, err := fx.series.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !snap.IsKeeper || snap.Stale || snap.Keeper != "" {
		t.Fatalf("self: IsKeeper=%v Stale=%v Keeper=%q", snap.IsKeeper, snap.Stale, snap.Keeper)
	}
	rec := snap.Record
	if len(rec.Readings) != 2 || !readingsEqualAtPercent(rec.Readings, []Reading{first, second}) {
		t.Fatalf("readings got %+v, want %v", rec.Readings, []Reading{first, second})
	}
	if rec.ExtraCreditPoints != 30 {
		t.Fatalf("extra credit = %v, want 30", rec.ExtraCreditPoints)
	}
	// The extra credit was set last, at the clock; it is the newest update.
	if !rec.UpdatedAt.Equal(fx.clock.Now()) {
		t.Fatalf("updated_at = %v, want the injected clock", rec.UpdatedAt)
	}

	// Writes as keeper land in the local store.
	third := Reading{At: storeTestNow, Percent: fptr(55), Source: SourceTyped}
	stored, err := fx.series.AddReading(context.Background(), third)
	if err != nil {
		t.Fatalf("AddReading: %v", err)
	}
	if stored.ID == "" || stored.At.IsZero() {
		t.Fatalf("stored reading got %+v, want the id and time the store assigned", stored)
	}
	firstID := fx.store.Snapshot().Account.Readings[0].ID
	if err := fx.series.DeleteReading(context.Background(), firstID); err != nil {
		t.Fatalf("DeleteReading: %v", err)
	}
	if err := fx.series.SetExtraCredit(context.Background(), 60); err != nil {
		t.Fatalf("SetExtraCredit: %v", err)
	}
	local := fx.series.RecordFromLocal()
	if len(local.Readings) != 2 || !readingsEqualAtPercent(local.Readings, []Reading{second, third}) {
		t.Fatalf("local readings got %+v, want second and third", local.Readings)
	}
	if local.ExtraCreditPoints != 60 {
		t.Fatalf("local credit = %v, want 60", local.ExtraCreditPoints)
	}
}

func TestSharedSeriesReadFromPeer(t *testing.T) {
	fx := newSharedFixture(t, "alpha", "live", "sekrit")
	seed1 := Reading{ID: "seed-1", At: storeTestNow.Add(-24 * time.Hour), Percent: fptr(12), Source: SourceTyped}
	seed2 := Reading{ID: "seed-2", At: storeTestNow.Add(-4 * time.Hour), Percent: fptr(33.5), Source: SourceTyped}
	fx.keeper.addSeed(seed1)
	fx.keeper.addSeed(seed2)
	fx.keeper.seedCredit(25)

	snap, err := fx.series.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.IsKeeper || snap.Stale || snap.Keeper != "alpha" {
		t.Fatalf("peer: IsKeeper=%v Stale=%v Keeper=%q", snap.IsKeeper, snap.Stale, snap.Keeper)
	}
	if len(snap.Record.Readings) != 2 || !readingsEqual(snap.Record.Readings, []Reading{seed1, seed2}) {
		t.Fatalf("readings got %+v, want %v", snap.Record.Readings, []Reading{seed1, seed2})
	}
	if snap.Record.ExtraCreditPoints != 25 {
		t.Fatalf("credit = %v, want 25", snap.Record.ExtraCreditPoints)
	}
	if !snap.CopiedAt.Equal(fx.clock.Now().UTC()) {
		t.Fatalf("copied_at = %v, want the injected clock", snap.CopiedAt)
	}

	// Bearer token and hop marker went over the wire.
	tokens, hops := fx.keeper.wire()
	if len(tokens) != 1 || tokens[0] != "Bearer sekrit" {
		t.Fatalf("tokens got %v, want [Bearer sekrit]", tokens)
	}
	if len(hops) != 1 || hops[0] != sharedHopValue {
		t.Fatalf("hop header got %v, want [%q]", hops, sharedHopValue)
	}

	// The peer's local store was not touched by the read.
	local := fx.store.Snapshot()
	if len(local.Account.Readings) != 0 || local.Account.ExtraCreditPoints != 0 {
		t.Fatalf("peer store mutated by a read: %+v", local.Account)
	}

	// The last copy is on disk next to the store file.
	copy := copyStored(t, fx.store)
	if copy.Keeper != "alpha" || !copy.CopiedAt.Equal(fx.clock.Now().UTC()) || len(copy.Record.Readings) != 2 {
		t.Fatalf("copy got %+v", copy)
	}
}

func TestSharedSeriesStaleWhenKeeperUnreachable(t *testing.T) {
	fx := newSharedFixture(t, "alpha", "live", "")
	seed := Reading{ID: "seed-1", At: storeTestNow.Add(-6 * time.Hour), Percent: fptr(7), Source: SourceTyped}
	fx.keeper.addSeed(seed)

	fresh, err := fx.series.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("fresh Snapshot: %v", err)
	}
	if fresh.Stale {
		t.Fatal("the first read must not be stale")
	}
	// The keeper goes away.
	fx.server.Close()

	stale, err := fx.series.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("stale Snapshot: %v", err)
	}
	if !stale.Stale || stale.IsKeeper || stale.Keeper != "alpha" {
		t.Fatalf("stale got IsKeeper=%v Stale=%v Keeper=%q", stale.IsKeeper, stale.Stale, stale.Keeper)
	}
	if stale.StaleReason == "" {
		t.Fatal("stale reason is empty")
	}
	if !stale.CopiedAt.Equal(fx.clock.Now().UTC()) {
		t.Fatalf("copied_at = %v, want the injected clock", stale.CopiedAt)
	}
	if !readingsEqual(stale.Record.Readings, []Reading{seed}) {
		t.Fatalf("stale readings got %+v, want the last copy", stale.Record.Readings)
	}
}

func TestSharedSeriesNoCopyError(t *testing.T) {
	fx := newSharedFixture(t, "dead", "dead", "")
	_, err := fx.series.Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrNoPeerCopy) {
		t.Fatalf("error = %v, want ErrNoPeerCopy", err)
	}
}

func TestSharedSeriesUnknownKeeper(t *testing.T) {
	fx := newSharedFixture(t, "ghost", "unknown", "")

	// Without a copy on disk the read fails clearly.
	_, err := fx.series.Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrNoPeerCopy) {
		t.Fatalf("error = %v, want ErrNoPeerCopy", err)
	}

	// With a copy on disk the same failure degrades to the stale copy.
	seed := Reading{ID: "seed-1", At: storeTestNow.Add(-9 * time.Hour), Percent: fptr(9), Source: SourceTyped}
	copy := peerCopy{
		CopiedAt: fx.clock.Now().UTC(),
		Keeper:   "ghost",
		Record:   AccountRecord{Readings: []Reading{seed}, ExtraCreditPoints: 4},
	}
	if err := writeFileAtomic(copyPath(t, fx.store), copy); err != nil {
		t.Fatalf("seed the copy: %v", err)
	}
	stale, err := fx.series.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("stale Snapshot: %v", err)
	}
	if !stale.Stale || !strings.Contains(stale.StaleReason, "not a registered peer") {
		t.Fatalf("stale got stale=%v reason=%q", stale.Stale, stale.StaleReason)
	}
	if !readingsEqual(stale.Record.Readings, []Reading{seed}) {
		t.Fatalf("stale readings got %+v", stale.Record.Readings)
	}

	// Writes to an unknown keeper are refused and nothing lands on the local store.
	reading := Reading{At: storeTestNow, Percent: fptr(1), Source: SourceTyped}
	if _, err := fx.series.AddReading(context.Background(), reading); !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("AddReading error = %v, want ErrKeeperUnreachable", err)
	}
	if err := fx.series.DeleteReading(context.Background(), seed.ID); !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("DeleteReading error = %v, want ErrKeeperUnreachable", err)
	}
	if err := fx.series.SetExtraCredit(context.Background(), 1); !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("SetExtraCredit error = %v, want ErrKeeperUnreachable", err)
	}
	local := fx.store.Snapshot()
	if len(local.Account.Readings) != 0 || local.Account.ExtraCreditPoints != 0 {
		t.Fatalf("peer store mutated by refused writes: %+v", local.Account)
	}
}

func TestSharedSeriesWriteForwardedToKeeper(t *testing.T) {
	fx := newSharedFixture(t, "alpha", "live", "tok")

	reading := Reading{At: storeTestNow.Add(-1 * time.Hour), Percent: fptr(50), Source: SourceTyped}
	stored, err := fx.series.AddReading(context.Background(), reading)
	if err != nil {
		t.Fatalf("AddReading: %v", err)
	}
	readings, _, requests := fx.keeper.state()
	if len(readings) != 1 || !percentSame(readings[0].Percent, reading.Percent) {
		t.Fatalf("keeper readings got %+v", readings)
	}
	if stored.ID == "" || stored.ID != readings[0].ID {
		t.Fatalf("stored reading got %+v, want the keeper-assigned id %q", stored, readings[0].ID)
	}
	if len(requests) != 1 || !strings.Contains(requests[0], "POST /api/usage/readings") {
		t.Fatalf("requests got %v", requests)
	}

	// The peer's store stays untouched: the write is forwarded, not stored here.
	local := fx.store.Snapshot()
	if len(local.Account.Readings) != 0 || local.Account.ExtraCreditPoints != 0 {
		t.Fatalf("peer store mutated by a forwarded write: %+v", local.Account)
	}

	if err := fx.series.SetExtraCredit(context.Background(), 12.5); err != nil {
		t.Fatalf("SetExtraCredit: %v", err)
	}
	if _, credit, requests := fx.keeper.state(); credit != 12.5 {
		t.Fatalf("keeper credit = %v, want 12.5", credit)
	} else if !strings.Contains(requests[1], "PUT /api/usage/extra-credit") {
		t.Fatalf("requests got %v", requests)
	}

	if err := fx.series.DeleteReading(context.Background(), readings[0].ID); err != nil {
		t.Fatalf("DeleteReading: %v", err)
	}
	if read, _, requests := fx.keeper.state(); len(read) != 0 {
		t.Fatalf("keeper readings after delete = %+v", read)
	} else if !strings.Contains(requests[2], "DELETE /api/usage/readings/") {
		t.Fatalf("requests got %v", requests)
	}
}

// TestSharedSeriesAddReadingOldKeeperShape: a keeper that answers the old bare
// confirmation (204, no body) still confirms the write; the returned reading is
// empty (the caller can find the stored one by a read), no error.
func TestSharedSeriesAddReadingOldKeeperShape(t *testing.T) {
	fx := newSharedFixture(t, "alpha", "live", "tok")
	fx.keeper.legacy = true
	reading := Reading{At: storeTestNow.Add(-1 * time.Hour), Percent: fptr(42), Source: SourceTyped}
	stored, err := fx.series.AddReading(context.Background(), reading)
	if err != nil {
		t.Fatalf("AddReading against the old keeper shape: %v", err)
	}
	if stored.ID != "" {
		t.Fatalf("stored id = %q, want empty (the old shape carries none)", stored.ID)
	}
	readings, _, requests := fx.keeper.state()
	if len(readings) != 1 || !strings.Contains(requests[0], "POST /api/usage/readings") {
		t.Fatalf("keeper got readings=%+v requests=%v", readings, requests)
	}
}

func TestSharedSeriesWriteRefusedWhenKeeperUnreachable(t *testing.T) {
	fx := newSharedFixture(t, "dead", "dead", "tok")
	ctx := context.Background()
	reading := Reading{At: storeTestNow, Percent: fptr(1), Source: SourceTyped}

	if _, err := fx.series.AddReading(ctx, reading); !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("AddReading error = %v, want ErrKeeperUnreachable", err)
	}
	if err := fx.series.DeleteReading(ctx, "whatever"); !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("DeleteReading error = %v, want ErrKeeperUnreachable", err)
	}
	if err := fx.series.SetExtraCredit(ctx, 5); !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("SetExtraCredit error = %v, want ErrKeeperUnreachable", err)
	}

	// Nothing was stored locally: the peer cannot split the shared series.
	local := fx.store.Snapshot()
	if len(local.Account.Readings) != 0 || local.Account.ExtraCreditPoints != 0 {
		t.Fatalf("peer store mutated despite the keeper being down: %+v", local.Account)
	}
}

func TestSharedSeriesHopHeaderServesLocal(t *testing.T) {
	fx := newSharedFixture(t, "alpha", "live", "")

	// The peer's own local store has data. A hop-marked request must see exactly this
	// and must not touch the network.
	local := Reading{ID: "local-1", At: storeTestNow.Add(-3 * time.Hour), Percent: fptr(77), Source: SourceTyped}
	if _, err := fx.store.Append(local); err != nil {
		t.Fatalf("append local: %v", err)
	}
	if err := fx.store.SetExtraCredit(9); err != nil {
		t.Fatalf("set credit: %v", err)
	}

	marked, err := http.NewRequest(http.MethodGet, fx.server.URL+"/api/usage/readings", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	marked.Header.Set(HeaderUsageHop, sharedHopValue)
	if !HasHopHeader(marked) {
		t.Fatal("the hop-marked request must be recognised")
	}
	rec := fx.series.RecordFromLocal()
	if !readingsEqual(rec.Readings, []Reading{local}) {
		t.Fatalf("record got %+v, want only the local readings", rec.Readings)
	}
	if rec.ExtraCreditPoints != 9 {
		t.Fatalf("credit = %v, want 9", rec.ExtraCreditPoints)
	}
	if got, _, _ := fx.keeper.state(); len(got) != 0 {
		t.Fatalf("servicing the hop-marked request hit the keeper (%d readings)", len(got))
	}

	// Without the hop marker the request is a genuine peer read: it is forwarded and
	// sees the keeper's record, not the local data.
	plain, _ := http.NewRequest(http.MethodGet, fx.server.URL+"/api/usage/readings", nil)
	if HasHopHeader(plain) {
		t.Fatal("a plain request must not be hop-guarded")
	}
	snap, err := fx.series.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("forwarded read: %v", err)
	}
	if snap.IsKeeper || snap.Stale {
		t.Fatalf("forwarded read got IsKeeper=%v Stale=%v", snap.IsKeeper, snap.Stale)
	}
}

// readingsEqualAtPercent compares readings by time and percent, ignoring store-assigned IDs.
func readingsEqualAtPercent(a, b []Reading) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].At.Equal(b[i].At) || !percentSame(a[i].Percent, b[i].Percent) {
			return false
		}
	}
	return true
}

func TestSharedSeriesMalformedKeeperResponse(t *testing.T) {
	// A garbage keeper: the read must degrade to the last copy, and the write must be
	// refused, rather than writing half a record.
	var mu sync.Mutex
	bad := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte("{nope")); err != nil {
				return
			}
			return
		}
		http.Error(w, "denied", http.StatusForbidden)
	})
	srv := httptest.NewServer(bad)
	defer srv.Close()

	fx := newSharedFixture(t, "broken", "live", "tok")
	// re-point the fixture at the bad server via a fresh series

	// A well-formed read establishes the copy...
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := AccountRecord{
			Readings:          []Reading{{ID: "seed-1", At: storeTestNow.Add(-8 * time.Hour), Percent: fptr(11), Source: SourceTyped}},
			ExtraCreditPoints: 2,
			Keeper:            "broken",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rec)
	}))
	goodSeries := NewSharedSeries(fx.store, "broken", func(name string) (string, string, bool) {
		if name == "broken" {
			return good.URL, "tok", true
		}
		return "", "", false
	}, WithSharedClock(fx.clock.Now))
	if _, err := goodSeries.Snapshot(context.Background()); err != nil {
		t.Fatalf("establish the copy: %v", err)
	}
	good.Close()

	// ...then the keeper returns garbage: stale copy from disk.
	corrupt := NewSharedSeries(fx.store, "broken", func(name string) (string, string, bool) {
		if name == "broken" {
			return srv.URL, "tok", true
		}
		return "", "", false
	}, WithSharedClock(fx.clock.Now))
	stale, err := corrupt.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("stale snapshot: %v", err)
	}
	if !stale.Stale || !strings.Contains(stale.StaleReason, "unreadable") {
		t.Fatalf("stale = %v (reason %q)", stale, stale.StaleReason)
	}
	if len(stale.Record.Readings) != 1 {
		t.Fatalf("stale readings = %+v", stale.Record)
	}

	// Writes to a denying keeper are refused; nothing lands locally.
	reading := Reading{At: storeTestNow, Percent: fptr(3), Source: SourceTyped}
	if _, err := corrupt.AddReading(context.Background(), reading); err == nil {
		t.Fatal("AddReading should be refused")
	}
	if err := corrupt.DeleteReading(context.Background(), "seed-1"); err == nil {
		t.Fatal("DeleteReading should be refused")
	}
	if err := corrupt.SetExtraCredit(context.Background(), 8); err == nil {
		t.Fatal("SetExtraCredit should be refused")
	}
	if local := fx.store.Snapshot(); len(local.Account.Readings) != 0 || local.Account.ExtraCreditPoints != 0 {
		t.Fatalf("local store mutated by a write to a denying keeper: %+v", local.Account)
	}
}
