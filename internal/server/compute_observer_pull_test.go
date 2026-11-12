package server

// Pulled-peer binding tests: peers opted into observer pull
// (federated, observer_pull on) must be attachable/detachable to
// ComputeNodes exactly like pushed peers, be excluded from the free
// list while bound, and appear in the by-node grouping. Shadow peers
// (federated, observer_pull off, never pushed) carry no data to bind
// and must be rejected with HTTP 409. The implicit name-match
// auto-binding (a Node named like a pushed peer) must keep working and
// must NOT auto-bind a shadow row.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// newPullBindingServer wires a Server with:
//   - a fake push registry holding the pushed peer "pushpeer",
//   - a federation peer store with "pullpeer" (observer_pull on) and
//     "shadowpeer" (federated, observer_pull off, never pushed),
//   - a compute registry with nodes "node1", "pushpeer" (auto-created
//     style: name-matches the pushed peer, explicitly bound) and
//     "shadowpeer" (name-matches the shadow peer — proving the
//     implicit name match never auto-binds a shadow row).
func newPullBindingServer(t *testing.T) *Server {
	t.Helper()

	reg := newFakePeerRegistry()
	if _, err := reg.Register("pushpeer", "B", "v9.9", nil); err != nil {
		t.Fatalf("register pushpeer: %v", err)
	}

	st, err := multiserver.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	pullPeer := &multiserver.Entry{
		Name:                  "pullpeer",
		URL:                   "http://192.0.2.10:9001",
		Token:                 "tok-pullpeer",
		Enabled:               true,
		Federated:             true,
		ObserverPull:          true,
		ObserverPullIntervalS: 30,
	}
	shadowPeer := &multiserver.Entry{
		Name:      "shadowpeer",
		URL:       "http://192.0.2.11:9001",
		Token:     "tok-shadowpeer",
		Enabled:   true,
		Federated: true, // ObserverPull stays false — shadow row
	}
	if err := st.Add(pullPeer); err != nil {
		t.Fatalf("Add pullpeer: %v", err)
	}
	if err := st.Add(shadowPeer); err != nil {
		t.Fatalf("Add shadowpeer: %v", err)
	}

	comp, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	cap1 := compute.DeclaredCapacity{MaxConcurrentModels: 1}
	if err := comp.Add(&compute.Node{Name: "node1", Kind: compute.KindOllama, Address: "http://192.0.2.20:11434", DeclaredCapacity: cap1}); err != nil {
		t.Fatalf("Add node1: %v", err)
	}
	if err := comp.Add(&compute.Node{Name: "pushpeer", Kind: compute.KindOllama, Address: "http://192.0.2.22:11434", DeclaredCapacity: cap1, ObserverPeer: "pushpeer", AutoCreated: true}); err != nil {
		t.Fatalf("Add pushpeer node: %v", err)
	}
	if err := comp.Add(&compute.Node{Name: "shadowpeer", Kind: compute.KindOllama, Address: "http://192.0.2.21:11434", DeclaredCapacity: cap1}); err != nil {
		t.Fatalf("Add shadowpeer node: %v", err)
	}

	return &Server{peerRegistry: reg, serverStore: st, computeReg: comp}
}

func attachObserverPeer(t *testing.T, s *Server, node, peer string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"peer": peer})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/compute/nodes/"+node+"/observer-peer", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	s.handleComputeNodes(rr, req)
	return rr.Code, rr.Body.String()
}

func detachObserverPeer(t *testing.T, s *Server, node string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/compute/nodes/"+node+"/observer-peer", nil)
	s.handleComputeNodes(rr, req)
	return rr.Code, rr.Body.String()
}

// freePeerNames calls GET /api/observer/peers/free and returns the names.
func freePeerNames(t *testing.T, s *Server) []string {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/peers/free", nil)
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/observer/peers/free = %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Peers []map[string]any `json:"peers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	names := make([]string, 0, len(out.Peers))
	for _, p := range out.Peers {
		if n, _ := p["name"].(string); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// byNodeGroups calls GET /api/observer/peers/by-node and returns the
// per-node peer-name lists plus the unbound names.
func byNodeGroups(t *testing.T, s *Server) (map[string][]string, []string) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/peers/by-node", nil)
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/observer/peers/by-node = %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		ByNode  map[string][]map[string]any `json:"by_node"`
		Unbound []map[string]any            `json:"unbound"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	groups := map[string][]string{}
	for node, rows := range out.ByNode {
		for _, row := range rows {
			if n, _ := row["name"].(string); n != "" {
				groups[node] = append(groups[node], n)
			}
		}
	}
	unbound := make([]string, 0, len(out.Unbound))
	for _, row := range out.Unbound {
		if n, _ := row["name"].(string); n != "" {
			unbound = append(unbound, n)
		}
	}
	return groups, unbound
}

// nodeObserverPeer returns the ObserverPeer field of the named node.
func nodeObserverPeer(t *testing.T, s *Server, node string) string {
	t.Helper()
	n, err := s.computeReg.Get(node)
	if err != nil {
		t.Fatalf("Get node %q: %v", node, err)
	}
	return n.ObserverPeer
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func TestObserverPullBinding_AttachPulledPeer(t *testing.T) {
	s := newPullBindingServer(t)

	code, body := attachObserverPeer(t, s, "node1", "pullpeer")
	if code != http.StatusOK {
		t.Fatalf("attach pulled peer = %d, want 200: %s", code, body)
	}
	if got := nodeObserverPeer(t, s, "node1"); got != "pullpeer" {
		t.Errorf("node1.observer_peer = %q, want pullpeer", got)
	}

	// Pushed peers attach exactly as before.
	code, body = attachObserverPeer(t, s, "node1", "pushpeer")
	if code != http.StatusOK {
		t.Fatalf("attach pushed peer = %d, want 200: %s", code, body)
	}

	// A peer in neither the push registry nor the peer store is unknown.
	code, body = attachObserverPeer(t, s, "node1", "ghost")
	if code != http.StatusBadRequest {
		t.Errorf("attach ghost = %d, want 400: %s", code, body)
	}
}

func TestObserverPullBinding_AttachShadowRejected(t *testing.T) {
	s := newPullBindingServer(t)

	code, body := attachObserverPeer(t, s, "node1", "shadowpeer")
	if code != http.StatusConflict {
		t.Fatalf("attach shadow peer = %d, want 409: %s", code, body)
	}
	if !strings.Contains(body, "pull disabled") {
		t.Errorf("409 body = %q, want a message saying pull is disabled", body)
	}
	if got := nodeObserverPeer(t, s, "node1"); got != "" {
		t.Errorf("node1.observer_peer = %q after rejected attach, want empty", got)
	}
}

func TestObserverPullBinding_Detach(t *testing.T) {
	s := newPullBindingServer(t)

	if code, body := attachObserverPeer(t, s, "node1", "pullpeer"); code != http.StatusOK {
		t.Fatalf("attach = %d: %s", code, body)
	}
	code, body := detachObserverPeer(t, s, "node1")
	if code != http.StatusOK {
		t.Fatalf("detach = %d: %s", code, body)
	}
	if got := nodeObserverPeer(t, s, "node1"); got != "" {
		t.Errorf("node1.observer_peer = %q after detach, want empty", got)
	}
	if free := freePeerNames(t, s); !containsName(free, "pullpeer") {
		t.Errorf("free list after detach = %v, want pullpeer back in it", free)
	}
}

func TestObserverPullBinding_FreeListExcludesBound(t *testing.T) {
	s := newPullBindingServer(t)

	// Initial state: the name-matched pushed peer is NOT free (implicit
	// auto-binding still works), the shadow row is free (a node even
	// shares its name — shadow rows never auto-bind), and the pulled
	// peer is not yet bound.
	free := freePeerNames(t, s)
	if containsName(free, "pushpeer") {
		t.Errorf("free list = %v, want pushpeer excluded (name-match auto-binding)", free)
	}
	if !containsName(free, "shadowpeer") {
		t.Errorf("free list = %v, want shadowpeer present (shadow rows never auto-bind)", free)
	}
	if !containsName(free, "pullpeer") {
		t.Errorf("free list = %v, want unbound pullpeer present", free)
	}

	// After attaching the pulled peer to node1 it leaves the free list.
	if code, body := attachObserverPeer(t, s, "node1", "pullpeer"); code != http.StatusOK {
		t.Fatalf("attach = %d: %s", code, body)
	}
	free = freePeerNames(t, s)
	if containsName(free, "pullpeer") {
		t.Errorf("free list = %v, want bound pullpeer excluded", free)
	}
}

func TestObserverPullBinding_ByNodeIncludesBound(t *testing.T) {
	s := newPullBindingServer(t)

	if code, body := attachObserverPeer(t, s, "node1", "pullpeer"); code != http.StatusOK {
		t.Fatalf("attach = %d: %s", code, body)
	}

	groups, unbound := byNodeGroups(t, s)
	if !containsName(groups["node1"], "pullpeer") {
		t.Errorf("by_node[node1] = %v, want the bound pulled peer", groups)
	}
	if !containsName(groups["pushpeer"], "pushpeer") {
		t.Errorf("by_node[pushpeer] = %v, want the name-matched pushed peer", groups)
	}
	if !containsName(unbound, "shadowpeer") {
		t.Errorf("unbound = %v, want the shadow row (never auto-bound)", unbound)
	}
	for node, rows := range groups {
		if containsName(rows, "shadowpeer") {
			t.Errorf("shadow row appears under node %q: %v", node, groups)
		}
	}
}
