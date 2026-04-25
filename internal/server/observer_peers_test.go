package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/observer"
)

// fakePeerRegistry is an in-memory PeerRegistryAPI implementation used
// by the handler tests so we don't pull bcrypt into the path.
type fakePeerRegistry struct {
	peers       map[string]string                    // name → token
	hostInfos   map[string]map[string]any
	versions    map[string]string
	lastPayload map[string]*observer.StatsResponse
	pushes      map[string]int
	failRegister bool
}

func newFakePeerRegistry() *fakePeerRegistry {
	return &fakePeerRegistry{
		peers:       map[string]string{},
		hostInfos:   map[string]map[string]any{},
		versions:    map[string]string{},
		lastPayload: map[string]*observer.StatsResponse{},
		pushes:      map[string]int{},
	}
}

func (f *fakePeerRegistry) Register(name, shape, version string, hostInfo map[string]any) (string, error) {
	if f.failRegister {
		return "", errors.New("register fails")
	}
	tok := "tok-" + name
	f.peers[name] = tok
	f.hostInfos[name] = hostInfo
	f.versions[name] = version
	return tok, nil
}
func (f *fakePeerRegistry) Verify(name, token string) (*observer.PeerEntry, error) {
	stored, ok := f.peers[name]
	if !ok {
		return nil, errors.New("unknown peer")
	}
	if stored != token {
		return nil, errors.New("invalid token")
	}
	return &observer.PeerEntry{Name: name}, nil
}
func (f *fakePeerRegistry) RecordPush(name string, snap *observer.StatsResponse) error {
	if _, ok := f.peers[name]; !ok {
		return errors.New("unknown peer")
	}
	f.lastPayload[name] = snap
	f.pushes[name]++
	return nil
}
func (f *fakePeerRegistry) Get(name string) (observer.PeerEntry, bool) {
	if _, ok := f.peers[name]; !ok {
		return observer.PeerEntry{}, false
	}
	return observer.PeerEntry{Name: name, Version: f.versions[name]}, true
}
func (f *fakePeerRegistry) LastPayload(name string) *observer.StatsResponse {
	return f.lastPayload[name]
}
func (f *fakePeerRegistry) List() []observer.PeerEntry {
	out := []observer.PeerEntry{}
	for name := range f.peers {
		out = append(out, observer.PeerEntry{Name: name, Version: f.versions[name]})
	}
	return out
}
func (f *fakePeerRegistry) Delete(name string) error {
	if _, ok := f.peers[name]; !ok {
		return errors.New("unknown peer")
	}
	delete(f.peers, name)
	return nil
}

func newServerWithRegistry(reg PeerRegistryAPI) *Server {
	s := &Server{}
	s.SetPeerRegistry(reg)
	return s
}

func TestPeers_Disabled503(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/peers", nil)
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d want 503", rr.Code)
	}
}

func TestPeers_RegisterReturnsToken(t *testing.T) {
	reg := newFakePeerRegistry()
	s := newServerWithRegistry(reg)

	body := `{"name":"ollama","shape":"B","version":"0.1.0"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/observer/peers", strings.NewReader(body))
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["token"] != "tok-ollama" {
		t.Errorf("token = %v want tok-ollama", got["token"])
	}
	if reg.peers["ollama"] != "tok-ollama" {
		t.Errorf("registry not updated")
	}
}

func TestPeers_RegisterMissingName400(t *testing.T) {
	s := newServerWithRegistry(newFakePeerRegistry())
	req := httptest.NewRequest(http.MethodPost, "/api/observer/peers", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d want 400", rr.Code)
	}
}

func TestPeers_ListReturnsPeers(t *testing.T) {
	reg := newFakePeerRegistry()
	_, _ = reg.Register("a", "B", "v", nil)
	_, _ = reg.Register("b", "B", "v", nil)
	s := newServerWithRegistry(reg)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/observer/peers", nil)
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	var got struct {
		Peers []observer.PeerEntry `json:"peers"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Peers) != 2 {
		t.Errorf("got %d peers want 2", len(got.Peers))
	}
}

func TestPeers_DeleteRemoves(t *testing.T) {
	reg := newFakePeerRegistry()
	_, _ = reg.Register("box", "B", "v", nil)
	s := newServerWithRegistry(reg)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/observer/peers/box", nil)
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if _, ok := reg.peers["box"]; ok {
		t.Errorf("peer still present")
	}
}

func TestPeers_PushHappyPath(t *testing.T) {
	reg := newFakePeerRegistry()
	_, _ = reg.Register("box", "B", "v", nil)
	s := newServerWithRegistry(reg)

	body, _ := json.Marshal(map[string]any{
		"shape":     "B",
		"peer_name": "box",
		"snapshot":  &observer.StatsResponse{V: 2},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/observer/peers/box/stats", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok-box")
	rr := httptest.NewRecorder()
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rr.Code, rr.Body.String())
	}
	if reg.pushes["box"] != 1 {
		t.Errorf("push count = %d want 1", reg.pushes["box"])
	}
}

func TestPeers_PushRejectsBadToken(t *testing.T) {
	reg := newFakePeerRegistry()
	_, _ = reg.Register("box", "B", "v", nil)
	s := newServerWithRegistry(reg)
	body := `{"shape":"B","peer_name":"box","snapshot":{"v":2}}`

	cases := []struct {
		name string
		auth string
		want int
	}{
		{"no auth", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic foo", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/observer/peers/box/stats", strings.NewReader(body))
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rr := httptest.NewRecorder()
			s.handleObserverPeers(rr, req)
			if rr.Code != tc.want {
				t.Errorf("code = %d want %d", rr.Code, tc.want)
			}
		})
	}
}

func TestPeers_PushRejectsMismatchedName(t *testing.T) {
	reg := newFakePeerRegistry()
	_, _ = reg.Register("box", "B", "v", nil)
	s := newServerWithRegistry(reg)

	body := `{"shape":"B","peer_name":"someone-else","snapshot":{"v":2}}`
	req := httptest.NewRequest(http.MethodPost, "/api/observer/peers/box/stats", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok-box")
	rr := httptest.NewRecorder()
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d want 400", rr.Code)
	}
}

func TestPeers_GetLastSnapshot(t *testing.T) {
	reg := newFakePeerRegistry()
	_, _ = reg.Register("box", "B", "v", nil)
	reg.lastPayload["box"] = &observer.StatsResponse{V: 2}
	s := newServerWithRegistry(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/observer/peers/box/stats", nil)
	rr := httptest.NewRecorder()
	s.handleObserverPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	var got observer.StatsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.V != 2 {
		t.Errorf("V = %d want 2", got.V)
	}
}
