package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/tagregistry"
)

// newTagStoreManager returns an autonomous manager with its own store, for one
// daemon in a test.
func newTagStoreManager(t *testing.T) *autonomous.Manager {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	return mgr
}

// fakeTagKeeper is a keeper daemon in this process. It serves the two keeper
// routes from a keeper registry. While down it answers 503, the way an
// unreachable keeper looks to a peer.
type fakeTagKeeper struct {
	reg  *tagregistry.Registry
	down atomic.Bool
	srv  *httptest.Server
}

func newFakeTagKeeper(t *testing.T) *fakeTagKeeper {
	t.Helper()
	reg, err := tagregistry.New(tagregistry.Options{
		NodeName:   "keeper",
		KeeperName: "keeper",
		Store:      tagregistry.AutonomousStore{Book: newTagStoreManager(t)},
	})
	if err != nil {
		t.Fatalf("keeper registry: %v", err)
	}
	k := &fakeTagKeeper{reg: reg}
	mux := http.NewServeMux()
	mux.HandleFunc(tagregistry.SnapshotPath, func(w http.ResponseWriter, r *http.Request) {
		if k.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		snap, err := k.reg.KeeperSnapshot()
		if err != nil {
			writeFakeKeeperRefusal(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(snap)
	})
	mux.HandleFunc(tagregistry.WritePath, func(w http.ResponseWriter, r *http.Request) {
		if k.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		var wr tagregistry.Write
		if err := json.NewDecoder(r.Body).Decode(&wr); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res, err := k.reg.ApplyPeerWrite(wr)
		if err != nil {
			writeFakeKeeperRefusal(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	})
	k.srv = httptest.NewServer(mux)
	t.Cleanup(k.srv.Close)
	return k
}

// writeFakeKeeperRefusal answers a refusal with the {error, kind} body.
func writeFakeKeeperRefusal(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if tagregistry.ErrorKind(err) == tagregistry.KindNotFound {
		status = http.StatusNotFound
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "kind": tagregistry.ErrorKind(err)})
}

// client returns the keeper client a peer uses to reach this fake keeper.
func (k *fakeTagKeeper) client() *tagregistry.HTTPKeeperClient {
	return &tagregistry.HTTPKeeperClient{
		Peers: func(name string) (string, string, bool) {
			if name != "keeper" {
				return "", "", false
			}
			return k.srv.URL, "tok", true
		},
		Keeper: func() string { return "keeper" },
	}
}

// newTagPeer returns a server whose registry is a peer of keeper, and the peer's
// registry.
func newTagPeer(t *testing.T, keeper *fakeTagKeeper) (*Server, *tagregistry.Registry) {
	t.Helper()
	mgr := newTagStoreManager(t)
	reg, err := tagregistry.New(tagregistry.Options{
		NodeName:   "edge",
		KeeperName: "keeper",
		Store:      tagregistry.AutonomousStore{Book: mgr},
		Keeper:     keeper.client(),
	})
	if err != nil {
		t.Fatalf("peer registry: %v", err)
	}
	s := &Server{autonomousMgr: newTestAPI(mgr), tagRegistry: reg}
	return s, reg
}

// fedPeerRequest builds a request as a federation peer holding the given
// capabilities would send it.
func fedPeerRequest(method, target, body string, caps ...string) (*http.Request, *httptest.ResponseRecorder) {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	peer := &multiserver.Entry{Name: "caller", URL: "http://caller:8080", Token: "tok", Federated: true, Enabled: true, Capabilities: caps}
	r = r.WithContext(context.WithValue(r.Context(), fedPeerKey, peer))
	return r, httptest.NewRecorder()
}

// decodeRegistryStatus reads the registry object of a GET /api/autonomous/tags reply.
func decodeRegistryStatus(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out struct {
		Registry map[string]any `json:"registry"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode tags reply: %v: %s", err, body)
	}
	if out.Registry == nil {
		t.Fatalf("reply has no registry object: %s", body)
	}
	return out.Registry
}

func TestTagRegistryRouteListCarriesRoleAndStale(t *testing.T) {
	keeper := newFakeTagKeeper(t)
	s, reg := newTagPeer(t, keeper)
	keeper.down.Store(true)
	if err := reg.Sync(context.Background()); err == nil {
		t.Fatal("Sync with the keeper down returned no error")
	}
	rec := tagCall(t, s, http.MethodGet, "/api/autonomous/tags", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	status := decodeRegistryStatus(t, rec.Body.Bytes())
	if status["role"] != "peer" || status["keeper"] != "keeper" {
		t.Fatalf("role/keeper = %v/%v, want peer/keeper", status["role"], status["keeper"])
	}
	if stale, ok := status["stale"].(bool); !ok || !stale {
		t.Fatalf("stale = %v, want true while the keeper is down", status["stale"])
	}
	if status["last_error"] == "" {
		t.Fatal("last_error is empty after a failed sync")
	}
	for _, k := range []string{"last_synced_at", "revision", "pending_sync_count", "notes"} {
		if _, ok := status[k]; !ok {
			t.Fatalf("registry object lacks %q: %v", k, status)
		}
	}
}

func TestTagRegistryRouteListWithoutRegistryReportsStandalone(t *testing.T) {
	s, _ := newTagTestServer(t)
	rec := tagCall(t, s, http.MethodGet, "/api/autonomous/tags?counts=true", "")
	reg := decodeRegistryStatus(t, rec.Body.Bytes())
	if reg["role"] != "standalone" || reg["stale"] != false {
		t.Fatalf("registry = %v, want standalone and not stale", reg)
	}
}

func TestTagRegistryRoutePeerWriteForwardedAndConflictRelayed(t *testing.T) {
	keeper := newFakeTagKeeper(t)
	s, reg := newTagPeer(t, keeper)
	ctx := context.Background()
	created, err := keeper.reg.Create(ctx, tagregistry.CreateInput{Name: "alpha"})
	if err != nil {
		t.Fatalf("keeper create: %v", err)
	}
	if err := reg.Sync(ctx); err != nil {
		t.Fatalf("peer sync: %v", err)
	}
	// The keeper renames again after the peer's last sync, so the peer's rename
	// is based on an older revision and the keeper keeps its own name.
	if _, err := keeper.reg.Rename(ctx, created.Tag.ID, "delta"); err != nil {
		t.Fatalf("keeper rename: %v", err)
	}
	rec := tagCall(t, s, http.MethodPatch, "/api/autonomous/tags/"+created.Tag.ID, `{"name":"gamma"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("forwarded rename: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Name          string `json:"name"`
		Conflict      bool   `json:"conflict"`
		ConflictField string `json:"conflict_field"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Conflict || out.ConflictField != "name" || out.Name != "delta" {
		t.Fatalf("reply = %+v, want the keeper's name kept with a name conflict", out)
	}
	kept := keeper.reg.View().Tags
	if len(kept) != 1 || kept[0].Name != "delta" {
		t.Fatalf("keeper names = %v, want delta", kept)
	}
}

func TestTagRegistryRouteWriteRefusedWhileKeeperUnreachable(t *testing.T) {
	keeper := newFakeTagKeeper(t)
	s, reg := newTagPeer(t, keeper)
	keeper.down.Store(true)

	// A create is still allowed and is kept pending sync.
	rec := tagCall(t, s, http.MethodPost, "/api/autonomous/tags", `{"name":"offline"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create while keeper down: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID          string `json:"id"`
		PendingSync bool   `json:"pending_sync"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !created.PendingSync {
		t.Fatal("a tag created while the keeper is down is not marked pending sync")
	}

	// Rename needs the keeper, so it is refused and the message says why.
	rec = tagCall(t, s, http.MethodPatch, "/api/autonomous/tags/"+created.ID, `{"name":"renamed"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("rename while keeper down: %d %s, want 503", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "last copy is shown") {
		t.Fatalf("refusal text does not say the last copy is shown: %s", rec.Body.String())
	}
	if n := reg.View().Tags; len(n) != 1 || n[0].Name != "offline" {
		t.Fatalf("a refused rename changed the copy: %v", n)
	}
}

func TestTagRegistryRouteDismissRemovesOneNote(t *testing.T) {
	keeper := newFakeTagKeeper(t)
	s, reg := newTagPeer(t, keeper)
	ctx := context.Background()

	// Two tags created on the peer while the keeper is away, and the same two
	// names created on the keeper meanwhile, merge into the keeper's tags when the
	// peer syncs. Each merge leaves one note on the peer.
	keeper.down.Store(true)
	for _, name := range []string{"dup-one", "dup-two"} {
		if rec := tagCall(t, s, http.MethodPost, "/api/autonomous/tags", `{"name":"`+name+`"}`); rec.Code != http.StatusOK {
			t.Fatalf("offline create %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	keeper.down.Store(false)
	for _, name := range []string{"dup-one", "dup-two"} {
		if _, err := keeper.reg.Create(ctx, tagregistry.CreateInput{Name: name}); err != nil {
			t.Fatalf("keeper create %s: %v", name, err)
		}
	}
	if err := reg.Sync(ctx); err != nil {
		t.Fatalf("peer sync: %v", err)
	}
	notes := reg.View().Notes
	if len(notes) != 2 {
		t.Fatalf("notes after the merges = %d, want 2", len(notes))
	}
	dismiss := notes[0].ID

	r, rec := fedPeerRequest(http.MethodPost, "/api/autonomous/tags/registry/notes/"+dismiss+"/dismiss", "", federation.CapAutonomousWrite)
	s.handleTagRegistryNotes(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss: %d %s", rec.Code, rec.Body.String())
	}
	left := reg.View().Notes
	if len(left) != 1 || left[0].ID == dismiss {
		t.Fatalf("notes after dismissing one = %v, want the other one", left)
	}

	// Dismissing a note that does not exist is a 404.
	r, rec = fedPeerRequest(http.MethodPost, "/api/autonomous/tags/registry/notes/nope/dismiss", "", federation.CapAutonomousWrite)
	s.handleTagRegistryNotes(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("dismiss unknown note: %d, want 404", rec.Code)
	}
}

func TestTagRegistryRouteSnapshotNeedsReadCapability(t *testing.T) {
	keeper := newFakeTagKeeper(t)
	keeperServer := &Server{tagRegistry: keeper.reg}

	r, rec := fedPeerRequest(http.MethodGet, "/api/autonomous/tags/registry", "", federation.CapAutonomousWrite)
	keeperServer.handleTagRegistrySnapshot(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("snapshot with write-only capability: %d, want 403", rec.Code)
	}

	r, rec = fedPeerRequest(http.MethodGet, "/api/autonomous/tags/registry", "", federation.CapAutonomousRead)
	keeperServer.handleTagRegistrySnapshot(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot with read capability: %d %s", rec.Code, rec.Body.String())
	}
	var snap tagregistry.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}

	// A peer does not keep the registry, so it refuses the snapshot.
	peerServer, _ := newTagPeer(t, keeper)
	r, rec = fedPeerRequest(http.MethodGet, "/api/autonomous/tags/registry", "", federation.CapAutonomousRead)
	peerServer.handleTagRegistrySnapshot(rec, r)
	if rec.Code != http.StatusConflict {
		t.Fatalf("snapshot from a peer: %d, want 409", rec.Code)
	}
}

func TestTagRegistryRouteWriteNeedsWriteCapability(t *testing.T) {
	keeper := newFakeTagKeeper(t)
	keeperServer := &Server{tagRegistry: keeper.reg}
	body := `{"kind":"create","tag_id":"t1","name":"forwarded","origin_node":"edge"}`

	r, rec := fedPeerRequest(http.MethodPost, "/api/autonomous/tags/registry/write", body, federation.CapAutonomousRead)
	keeperServer.handleTagRegistryWrite(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("forwarded write with read-only capability: %d, want 403", rec.Code)
	}

	r, rec = fedPeerRequest(http.MethodPost, "/api/autonomous/tags/registry/write", body, federation.CapAutonomousWrite)
	keeperServer.handleTagRegistryWrite(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("forwarded write with write capability: %d %s", rec.Code, rec.Body.String())
	}
	if got := keeper.reg.View().Tags; len(got) != 1 || got[0].Name != "forwarded" {
		t.Fatalf("keeper tags after the forwarded create = %v", got)
	}
}

func TestTagRegistryRouteDismissNeedsWriteCapability(t *testing.T) {
	keeper := newFakeTagKeeper(t)
	s, _ := newTagPeer(t, keeper)
	r, rec := fedPeerRequest(http.MethodPost, "/api/autonomous/tags/registry/notes/n1/dismiss", "", federation.CapAutonomousRead)
	s.handleTagRegistryNotes(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("dismiss with read-only capability: %d, want 403", rec.Code)
	}
}

func TestTagRegistryRouteWithRegistryKeepsLocalWrites(t *testing.T) {
	keeper := newFakeTagKeeper(t)
	mgr := newTagStoreManager(t)
	reg, err := tagregistry.New(tagregistry.Options{
		NodeName: "solo",
		Store:    tagregistry.AutonomousStore{Book: mgr},
	})
	if err != nil {
		t.Fatalf("standalone registry: %v", err)
	}
	s := &Server{autonomousMgr: newTestAPI(mgr), tagRegistry: reg}
	rec := tagCall(t, s, http.MethodPost, "/api/autonomous/tags", `{"name":"local"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("standalone create: %d %s", rec.Code, rec.Body.String())
	}
	if got := keeper.reg.View().Tags; len(got) != 0 {
		t.Fatalf("a standalone write reached the keeper: %v", got)
	}
	list := tagCall(t, s, http.MethodGet, "/api/autonomous/tags", "")
	if !strings.Contains(list.Body.String(), `"name":"local"`) {
		t.Fatalf("standalone tag missing from the list: %s", list.Body.String())
	}
}
