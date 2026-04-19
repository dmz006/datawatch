// BL104 — REST handler tests for the peer-broker proxy endpoints.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/profile"
)

// peerFixture wires a Server with a real agents.Manager and broker
// plus two spawned agents so Send actually has somewhere to deliver.
func peerFixture(t *testing.T) (*Server, *agents.PeerBroker, string, string) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name:               "p",
		Git:                profile.GitSpec{URL: "https://g/y"},
		ImagePair:          profile.ImagePair{Agent: "agent-claude"},
		Memory:             profile.MemorySpec{Mode: profile.MemorySyncBack},
		AllowPeerMessaging: true,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	mgr := agents.NewManager(ps, cs)
	mgr.RegisterDriver(&fakePeerDriver{kind: "docker"})
	a, err := mgr.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "t1", Branch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := mgr.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "t2", Branch: "feat",
	})
	if err != nil {
		t.Fatal(err)
	}
	broker := agents.NewPeerBroker(mgr, 0)
	s := &Server{peerBroker: broker}
	return s, broker, a.ID, b.ID
}

type fakePeerDriver struct{ kind string }

func (f *fakePeerDriver) Kind() string { return f.kind }
func (f *fakePeerDriver) Spawn(_ context.Context, a *agents.Agent) error {
	a.DriverInstance = "fake-" + a.ID
	a.ContainerAddr = "127.0.0.1:9999"
	return nil
}
func (f *fakePeerDriver) Status(_ context.Context, _ *agents.Agent) (agents.State, error) {
	return agents.StateReady, nil
}
func (f *fakePeerDriver) Logs(_ context.Context, _ *agents.Agent, _ int) (string, error) {
	return "", nil
}
func (f *fakePeerDriver) Terminate(_ context.Context, _ *agents.Agent) error { return nil }

func TestHandlePeerSend_HappyPath(t *testing.T) {
	s, broker, fromID, toID := peerFixture(t)

	body := bytes.NewBufferString(`{"from":"` + fromID + `","to":["` + toID + `"],"topic":"build","body":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/peer/send", body)
	rr := httptest.NewRecorder()
	s.handlePeerSend(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Delivered int      `json:"delivered"`
		Dropped   []string `json:"dropped"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Delivered != 1 {
		t.Errorf("delivered=%d want 1; dropped=%v", got.Delivered, got.Dropped)
	}
	if broker.InboxLen(toID) != 1 {
		t.Errorf("recipient inbox len=%d want 1", broker.InboxLen(toID))
	}
}

func TestHandlePeerSend_NoBroker_503(t *testing.T) {
	s := &Server{}
	body := bytes.NewBufferString(`{"from":"a","to":["b"],"body":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/peer/send", body)
	rr := httptest.NewRecorder()
	s.handlePeerSend(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rr.Code)
	}
}

func TestHandlePeerSend_BadJSON_400(t *testing.T) {
	s, _, _, _ := peerFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/peer/send",
		bytes.NewBufferString("not json"))
	rr := httptest.NewRecorder()
	s.handlePeerSend(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestHandlePeerInbox_Drain(t *testing.T) {
	s, broker, fromID, toID := peerFixture(t)
	_, _, err := broker.Send(fromID, []string{toID}, "topic", "x")
	if err != nil {
		t.Fatal(err)
	}
	if broker.InboxLen(toID) != 1 {
		t.Fatal("setup: send did not deliver")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents/peer/inbox?id="+toID, nil)
	rr := httptest.NewRecorder()
	s.handlePeerInbox(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Recipient string               `json:"recipient"`
		Messages  []agents.PeerMessage `json:"messages"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Messages) != 1 {
		t.Errorf("messages=%d want 1", len(got.Messages))
	}
	if broker.InboxLen(toID) != 0 {
		t.Errorf("inbox should be drained, len=%d", broker.InboxLen(toID))
	}
}

func TestHandlePeerInbox_Peek(t *testing.T) {
	s, broker, fromID, toID := peerFixture(t)
	_, _, _ = broker.Send(fromID, []string{toID}, "", "x")

	req := httptest.NewRequest(http.MethodGet, "/api/agents/peer/inbox?id="+toID+"&peek=1", nil)
	rr := httptest.NewRecorder()
	s.handlePeerInbox(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code)
	}
	if broker.InboxLen(toID) != 1 {
		t.Errorf("peek should leave inbox intact, len=%d", broker.InboxLen(toID))
	}
}

func TestHandlePeerInbox_MissingID(t *testing.T) {
	s, _, _, _ := peerFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/peer/inbox", nil)
	rr := httptest.NewRecorder()
	s.handlePeerInbox(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}
