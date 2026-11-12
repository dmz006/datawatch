package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/observer"
)

// fakeListPeers is a peer registry fake that only serves List.
type fakeListPeers struct {
	PeerRegistryAPI
	peers []observer.PeerEntry
}

func (f *fakeListPeers) List() []observer.PeerEntry { return f.peers }

func newUnansweredServer(t *testing.T, peers ...string) (*Server, *compute.Registry) {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	fp := &fakeListPeers{}
	for _, p := range peers {
		fp.peers = append(fp.peers, observer.PeerEntry{Name: p, Shape: "B"})
	}
	return &Server{computeReg: reg, peerRegistry: fp}, reg
}

func listNodesBody(t *testing.T, srv *Server) map[string]json.RawMessage {
	t.Helper()
	w := httptest.NewRecorder()
	srv.handleComputeNodes(w, httptest.NewRequest(http.MethodGet, "/api/compute/nodes", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d: %s", w.Code, w.Body.String())
	}
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func namesOf(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	if raw == nil {
		t.Fatalf("key missing")
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode rows: %v (%s)", err, raw)
	}
	out := []string{}
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func TestListUnansweredAutoCreatedReport(t *testing.T) {
	srv, reg := newUnansweredServer(t)
	now := time.Now().UTC()
	add := func(name string, auto bool, answered time.Time) {
		n := &compute.Node{Name: name, Kind: compute.KindRemote, Address: "https://" + name + ".invalid:11434", AutoCreated: auto, LastAnsweredAt: answered}
		if err := reg.Add(n); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}
	add("dead-auto", true, time.Time{})
	add("live-auto", true, now.Add(-time.Minute))
	add("manual-never", false, time.Time{})

	body := listNodesBody(t, srv)
	got := namesOf(t, body["unanswered_auto_created"])
	if len(got) != 1 || got[0] != "dead-auto" {
		t.Fatalf("unanswered_auto_created = %v, want [dead-auto]", got)
	}
	var rows []map[string]any
	_ = json.Unmarshal(body["unanswered_auto_created"], &rows)
	for _, k := range []string{"name", "address", "kind", "created_at"} {
		if _, ok := rows[0][k]; !ok {
			t.Fatalf("report entry missing %q: %v", k, rows[0])
		}
	}
	if n := len(namesOf(t, body["nodes"])); n != 3 {
		t.Fatalf("nodes key changed: %d rows, want 3", n)
	}
}

func TestListEmptyKeysPresent(t *testing.T) {
	srv, _ := newUnansweredServer(t)
	body := listNodesBody(t, srv)
	for _, k := range []string{"observed_peers", "unanswered_auto_created"} {
		if strings.TrimSpace(string(body[k])) != "[]" {
			t.Fatalf("%s = %s, want []", k, body[k])
		}
	}
}

func TestListObservedPeersAndAddNodeClearsPeer(t *testing.T) {
	srv, reg := newUnansweredServer(t, "free-peer", "bound-peer")
	if err := reg.Add(&compute.Node{Name: "bound-peer", Kind: compute.KindRemote, Address: "https://bound.invalid:11434"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got := namesOf(t, listNodesBody(t, srv)["observed_peers"])
	if len(got) != 1 || got[0] != "free-peer" {
		t.Fatalf("observed_peers = %v, want [free-peer]", got)
	}

	w := httptest.NewRecorder()
	srv.handleComputeNodes(w, httptest.NewRequest(http.MethodPost, "/api/compute/nodes?probe=skip",
		strings.NewReader(`{"name":"free-peer","kind":"ollama","address":"https://free.invalid:11434"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("add node: got %d: %s", w.Code, w.Body.String())
	}
	got = namesOf(t, listNodesBody(t, srv)["observed_peers"])
	if len(got) != 0 {
		t.Fatalf("observed_peers after add = %v, want empty", got)
	}
}

func TestMarkAnsweredPersistsOnlyOnMeaningfulChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.json")
	reg, err := compute.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(&compute.Node{Name: "n1", Kind: compute.KindRemote, Address: "https://n1.invalid:11434", AutoCreated: true}); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now().UTC()
	if err := reg.MarkAnswered("n1", t0); err != nil {
		t.Fatal(err)
	}
	if err := reg.MarkAnswered("n1", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	n, _ := reg.Get("n1")
	if !n.LastAnsweredAt.Equal(t0) {
		t.Fatalf("LastAnsweredAt = %v, want unchanged %v", n.LastAnsweredAt, t0)
	}
	t1 := t0.Add(compute.AnsweredPersistInterval + time.Second)
	_ = reg.MarkAnswered("n1", t1)
	n, _ = reg.Get("n1")
	if !n.LastAnsweredAt.Equal(t1) {
		t.Fatalf("LastAnsweredAt = %v, want %v", n.LastAnsweredAt, t1)
	}
	if got := reg.UnansweredAutoCreated(); len(got) != 0 {
		t.Fatalf("answered node reported: %v", got)
	}
	reg2, err := compute.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	n2, _ := reg2.Get("n1")
	if n2.LastAnsweredAt.IsZero() {
		t.Fatal("LastAnsweredAt not persisted")
	}
	if err := reg.MarkAnswered("missing", t1); err != compute.ErrNotFound {
		t.Fatalf("missing node err = %v", err)
	}
}
