// S14a (v4.8.0) — federation loop-prevention tests for the
// observer peer-push handler. The handler must reject pushes whose
// chain field already contains the receiving primary's own name,
// preventing primary-A→primary-B→primary-A federation cycles.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/observer"
)

// fakePeerRegistryAcceptAll satisfies PeerRegistryAPI for tests
// that exercise loop prevention but don't care about token storage.
type fakePeerRegistryAcceptAll struct {
	pushed []*observer.StatsResponse
}

func (f *fakePeerRegistryAcceptAll) Register(string, string, string, map[string]any) (string, error) {
	return "tok-test", nil
}
func (f *fakePeerRegistryAcceptAll) Verify(string, string) (*observer.PeerEntry, error) {
	return &observer.PeerEntry{Name: "upstream", Shape: "P"}, nil
}
func (f *fakePeerRegistryAcceptAll) RecordPush(_ string, snap *observer.StatsResponse) error {
	f.pushed = append(f.pushed, snap)
	return nil
}
func (f *fakePeerRegistryAcceptAll) Get(name string) (observer.PeerEntry, bool) {
	return observer.PeerEntry{Name: name, Shape: "P"}, true
}
func (f *fakePeerRegistryAcceptAll) LastPayload(string) *observer.StatsResponse { return nil }
func (f *fakePeerRegistryAcceptAll) List() []observer.PeerEntry                 { return nil }
func (f *fakePeerRegistryAcceptAll) Delete(string) error                        { return nil }

func TestFederationPush_ChainContainingSelf_Rejected(t *testing.T) {
	reg := &fakePeerRegistryAcceptAll{}
	s := &Server{peerRegistry: reg, federationSelfName: "datawatch-root"}

	body, _ := json.Marshal(map[string]any{
		"shape":     "P",
		"peer_name": "datawatch-east",
		"snapshot":  &observer.StatsResponse{V: 2},
		"chain":     []string{"datawatch-west", "datawatch-root", "datawatch-east"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/observer/peers/datawatch-east/stats", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test")
	rr := httptest.NewRecorder()
	s.handlePeerPush(rr, req, "datawatch-east")

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if len(reg.pushed) != 0 {
		t.Errorf("loop-rejected push should not have been recorded, got %d", len(reg.pushed))
	}
}

func TestFederationPush_ChainNotContainingSelf_Accepted(t *testing.T) {
	reg := &fakePeerRegistryAcceptAll{}
	s := &Server{peerRegistry: reg, federationSelfName: "datawatch-root"}

	body, _ := json.Marshal(map[string]any{
		"shape":     "P",
		"peer_name": "datawatch-east",
		"snapshot":  &observer.StatsResponse{V: 2},
		"chain":     []string{"datawatch-west", "datawatch-east"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/observer/peers/datawatch-east/stats", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test")
	rr := httptest.NewRecorder()
	s.handlePeerPush(rr, req, "datawatch-east")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if len(reg.pushed) != 1 {
		t.Errorf("expected 1 recorded push, got %d", len(reg.pushed))
	}
}

func TestFederationPush_EmptySelfName_LoopCheckSkipped(t *testing.T) {
	reg := &fakePeerRegistryAcceptAll{}
	// federationSelfName empty — no loop guard.
	s := &Server{peerRegistry: reg}

	body, _ := json.Marshal(map[string]any{
		"shape":     "P",
		"peer_name": "datawatch-east",
		"snapshot":  &observer.StatsResponse{V: 2},
		"chain":     []string{"anyone", "everyone"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/observer/peers/datawatch-east/stats", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test")
	rr := httptest.NewRecorder()
	s.handlePeerPush(rr, req, "datawatch-east")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestFederationPush_NoChain_NoOp(t *testing.T) {
	reg := &fakePeerRegistryAcceptAll{}
	s := &Server{peerRegistry: reg, federationSelfName: "datawatch-root"}

	// No chain field — single-hop Shape A/B/C peer push, untouched.
	body, _ := json.Marshal(map[string]any{
		"shape":     "B",
		"peer_name": "agt-1",
		"snapshot":  &observer.StatsResponse{V: 2},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/observer/peers/agt-1/stats", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test")
	rr := httptest.NewRecorder()
	s.handlePeerPush(rr, req, "agt-1")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for chainless push, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}
