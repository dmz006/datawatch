package tagregistry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// keeperHTTP serves a keeper registry the way the daemon's routes do, so the
// HTTP client can be tested against a real keeper in this process.
func keeperHTTP(t *testing.T, keeper *Registry, wantToken string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(SnapshotPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+wantToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		snap, err := keeper.KeeperSnapshot()
		if err != nil {
			writeTestRefusal(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(snap)
	})
	mux.HandleFunc(WritePath, func(w http.ResponseWriter, r *http.Request) {
		var wr Write
		if err := json.NewDecoder(r.Body).Decode(&wr); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res, err := keeper.ApplyPeerWrite(wr)
		if err != nil {
			writeTestRefusal(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// writeTestRefusal answers a refusal the way the daemon does: a status and the
// {error, kind} body.
func writeTestRefusal(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch ErrorKind(err) {
	case KindNotFound:
		status = http.StatusNotFound
	case KindNotKeeper, KindInUse, KindCycle, KindHasChildren:
		status = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "kind": ErrorKind(err)})
}

// httpPeerClient builds a client that reaches the keeper at url with token.
func httpPeerClient(url, token string) *HTTPKeeperClient {
	return &HTTPKeeperClient{
		Peers: func(name string) (string, string, bool) {
			if name != "keeper" {
				return "", "", false
			}
			return url, token, true
		},
		Keeper: func() string { return "keeper" },
	}
}

func TestRegistryHTTPClientForwardsWriteAndRelaysConflict(t *testing.T) {
	g := newRig(t)
	created, err := g.keeper.Create(context.Background(), CreateInput{Name: "alpha"})
	if err != nil {
		t.Fatalf("keeper create: %v", err)
	}
	srv := keeperHTTP(t, g.keeper, "tok")
	client := httpPeerClient(srv.URL, "tok")

	// A rename based on an old revision after the keeper has moved on is a
	// conflict the keeper decided; the client returns it as a result, not an error.
	if _, err := g.keeper.Rename(context.Background(), created.Tag.ID, "beta"); err != nil {
		t.Fatalf("keeper rename: %v", err)
	}
	res, err := client.Write(context.Background(), Write{
		Kind: WriteRename, TagID: created.Tag.ID, BaseRevision: 0, Name: "gamma",
	})
	if err != nil {
		t.Fatalf("forwarded write: %v", err)
	}
	if !res.Conflict || res.ConflictField != fieldName {
		t.Fatalf("conflict = %v field %q, want a name conflict", res.Conflict, res.ConflictField)
	}
	if res.Tag.Name != "beta" {
		t.Fatalf("kept name = %q, want the keeper's beta", res.Tag.Name)
	}
}

func TestRegistryHTTPClientMapsKeeperRefusalToSentinel(t *testing.T) {
	g := newRig(t)
	srv := keeperHTTP(t, g.keeper, "tok")
	client := httpPeerClient(srv.URL, "tok")

	_, err := client.Write(context.Background(), Write{Kind: WriteRename, TagID: "missing", Name: "x"})
	if !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("err = %v, want a not-found refusal matching ErrTagNotFound", err)
	}
	if err.Error() == "" {
		t.Fatal("refusal text is empty")
	}
}

func TestRegistryHTTPClientSnapshotNeedsToken(t *testing.T) {
	g := newRig(t)
	srv := keeperHTTP(t, g.keeper, "tok")
	if _, err := httpPeerClient(srv.URL, "wrong").FetchSnapshot(context.Background()); err == nil {
		t.Fatal("snapshot with a wrong token succeeded; the keeper must refuse it")
	}
	snap, err := httpPeerClient(srv.URL, "tok").FetchSnapshot(context.Background())
	if err != nil {
		t.Fatalf("FetchSnapshot: %v", err)
	}
	if snap.Revision != g.keeper.revision() {
		t.Fatalf("snapshot revision = %d, want %d", snap.Revision, g.keeper.revision())
	}
}

func TestRegistryHTTPClientUnreachableKeeper(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := httpPeerClient(url, "tok").Write(context.Background(), Write{Kind: WriteRename, TagID: "x", Name: "y"})
	if !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("err = %v, want ErrKeeperUnreachable", err)
	}
	unknown := &HTTPKeeperClient{
		Peers:  func(string) (string, string, bool) { return "", "", false },
		Keeper: func() string { return "keeper" },
	}
	if _, err := unknown.FetchSnapshot(context.Background()); !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("unnamed keeper err = %v, want ErrKeeperUnreachable", err)
	}
}

func TestRegistryErrorKindRoundTrip(t *testing.T) {
	for _, sentinel := range []error{ErrTagNotFound, ErrTagInvalid, ErrTagHasChildren, ErrTagInUse, ErrTagCycle, ErrNotKeeper, ErrNoteNotFound} {
		kind := ErrorKind(sentinel)
		if kind == "" {
			t.Fatalf("no wire kind for %v", sentinel)
		}
		if got := refusalFromWire(kind, sentinel.Error()); !errors.Is(got, sentinel) {
			t.Fatalf("kind %q rebuilt as %v, want a match for %v", kind, got, sentinel)
		}
	}
	if ErrorKind(errors.New("other")) != "" {
		t.Fatal("an unnamed error got a wire kind")
	}
}

func TestRegistryRunStartsOnlyForPeerRole(t *testing.T) {
	g := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A keeper and a standalone daemon return at once from Run.
	standalone := g.standalone(newFakeStore())
	for name, r := range map[string]*Registry{"keeper": g.keeper, "standalone": standalone} {
		done := make(chan struct{})
		go func() {
			r.Run(ctx)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s Run did not return at once", name)
		}
	}

	// A peer syncs from the keeper it names.
	peer, _ := g.peer("edge", nil)
	fetched := make(chan struct{}, 8)
	g.links["edge"].fetched = fetched
	go peer.Run(ctx)
	select {
	case <-fetched:
	case <-time.After(2 * time.Second):
		t.Fatal("peer Run did not sync from the keeper")
	}
}

func TestRegistryRunStartsOneLoopPerPeer(t *testing.T) {
	g := newRig(t)
	peer, _ := g.peer("edge", nil)
	fetched := make(chan struct{}, 8)
	g.links["edge"].fetched = fetched
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go peer.Run(ctx)
	<-fetched
	// A second Run while the loop is running returns at once.
	done := make(chan struct{})
	go func() {
		peer.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second Run on a running peer did not return at once")
	}
}

func TestRegistryReconfigureChangesRole(t *testing.T) {
	g := newRig(t)
	peer, _ := g.peer("edge", nil)
	if err := peer.Reconfigure("", 0); err != nil {
		t.Fatalf("Reconfigure to standalone: %v", err)
	}
	if peer.Role() != RoleStandalone || peer.KeeperName() != "" {
		t.Fatalf("role = %s keeper = %q after clearing the keeper", peer.Role(), peer.KeeperName())
	}
	if err := peer.Reconfigure("keeper", 10*time.Second); err != nil {
		t.Fatalf("Reconfigure to peer: %v", err)
	}
	if peer.Role() != RolePeer || peer.syncInterval() != 10*time.Second {
		t.Fatalf("role = %s interval = %s after naming the keeper", peer.Role(), peer.syncInterval())
	}
	if err := peer.Reconfigure("edge", 0); err != nil {
		t.Fatalf("Reconfigure to own name: %v", err)
	}
	if peer.Role() != RoleKeeper || peer.syncInterval() != DefaultSyncEvery {
		t.Fatalf("role = %s interval = %s after naming itself", peer.Role(), peer.syncInterval())
	}

	standalone := g.standalone(newFakeStore())
	if err := standalone.Reconfigure("keeper", time.Second); err == nil {
		t.Fatal("a daemon with no keeper client accepted a keeper name")
	}
}

func TestRegistrySnapshotChangeCallsHookOnlyWhenStateChanges(t *testing.T) {
	g := newRig(t)
	calls := 0
	st := newFakeStore()
	link := &fakeKeeper{reg: g.keeper}
	g.links["edge"] = link
	peer := mustNew(t, Options{
		NodeName: "edge", KeeperName: "keeper", Store: st, Keeper: link, Clock: g.clock, NewID: g.ids,
		OnSnapshotChange: func() { calls++ },
	})
	if err := peer.Sync(context.Background()); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if calls != 0 {
		t.Fatalf("hook ran for an empty registry: %d calls", calls)
	}
	if _, err := g.keeper.Create(context.Background(), CreateInput{Name: "fresh"}); err != nil {
		t.Fatalf("keeper create: %v", err)
	}
	if err := peer.Sync(context.Background()); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if calls != 1 {
		t.Fatalf("hook calls after a new keeper tag = %d, want 1", calls)
	}
	if err := peer.Sync(context.Background()); err != nil {
		t.Fatalf("third Sync: %v", err)
	}
	if calls != 1 {
		t.Fatalf("hook ran again with no change: %d calls", calls)
	}
}

func TestRegistryStatusReportsStaleAfterKeeperLoss(t *testing.T) {
	g := newRig(t)
	peer, _ := g.peer("edge", nil)
	if err := peer.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	g.links["edge"].setDown(true)
	if err := peer.Sync(context.Background()); err == nil {
		t.Fatal("Sync with the keeper down returned no error")
	}
	v := peer.View()
	if !v.Stale || v.LastError == "" {
		t.Fatalf("view stale = %v error = %q, want a stale copy with the reason", v.Stale, v.LastError)
	}
	if v.LastSyncedAt.IsZero() {
		t.Fatal("last synced time is zero after an earlier successful sync")
	}
}

// fakeTagBook is the autonomous side the AutonomousStore adapter needs.
type fakeTagBook struct {
	tags     map[string]*autonomous.Tag
	assigned map[string]int
	moved    []string
}

func newFakeTagBook() *fakeTagBook {
	return &fakeTagBook{tags: map[string]*autonomous.Tag{}, assigned: map[string]int{}}
}

func (b *fakeTagBook) ListTags() []*autonomous.Tag {
	out := make([]*autonomous.Tag, 0, len(b.tags))
	for _, t := range b.tags {
		out = append(out, t)
	}
	return out
}

func (b *fakeTagBook) SaveTagRecords(tags []*autonomous.Tag) error {
	b.tags = map[string]*autonomous.Tag{}
	for _, t := range tags {
		b.tags[t.ID] = t
	}
	return nil
}

func (b *fakeTagBook) TagAssignmentCount(tagID string) int { return b.assigned[tagID] }

func (b *fakeTagBook) RewriteTagAssignments(fromID, toID string) (int, error) {
	b.moved = append(b.moved, "rewrite:"+fromID+">"+toID)
	return b.assigned[fromID], nil
}

func (b *fakeTagBook) RemoveTagAssignments(tagID string) (int, error) {
	b.moved = append(b.moved, "remove:"+tagID)
	return b.assigned[tagID], nil
}

func TestRegistryAutonomousStoreRoundTripsRecords(t *testing.T) {
	book := newFakeTagBook()
	st := AutonomousStore{Book: book}
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	in := []Tag{
		{ID: "a", Name: "Root", Color: "#111", Description: "d", CreatedAt: now, UpdatedAt: now},
		{ID: "b", Name: "Child", ParentID: "a", CreatedAt: now, UpdatedAt: now, PendingSync: true},
	}
	if err := st.ApplySnapshot(in); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	out, err := st.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	byID := map[string]Tag{}
	for _, t := range out {
		byID[t.ID] = t
	}
	if len(out) != 2 || byID["b"].ParentID != "a" || byID["a"].Color != "#111" || byID["a"].Description != "d" {
		t.Fatalf("round trip lost fields: %+v", out)
	}
	if len(book.tags) != 2 {
		t.Fatalf("book holds %d records, want 2", len(book.tags))
	}
	if err := st.ApplySnapshot([]Tag{in[0]}); err != nil {
		t.Fatalf("ApplySnapshot shrink: %v", err)
	}
	if _, ok := book.tags["b"]; ok {
		t.Fatal("a tag missing from the snapshot was kept")
	}

	book.assigned["a"] = 3
	if n, _ := st.AssignmentCount("a"); n != 3 {
		t.Fatalf("AssignmentCount = %d, want 3", n)
	}
	if _, err := st.RewriteAssignments("a", "z"); err != nil {
		t.Fatalf("RewriteAssignments: %v", err)
	}
	if _, err := st.RemoveAssignments("a"); err != nil {
		t.Fatalf("RemoveAssignments: %v", err)
	}
	if len(book.moved) != 2 || book.moved[0] != "rewrite:a>z" || book.moved[1] != "remove:a" {
		t.Fatalf("moves = %v", book.moved)
	}
}
