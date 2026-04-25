package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// fakeParent collects the requests that the bridge would send to the
// real datawatch parent, so tests can assert on the wire contract.
type fakeParent struct {
	srv      *httptest.Server
	mu       atomic.Value // []receivedRequest
	received []receivedRequest
}

type receivedRequest struct {
	Path   string
	Body   map[string]any
	Bearer string
}

func newFakeParent(t *testing.T) *fakeParent {
	t.Helper()
	fp := &fakeParent{}
	fp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		fp.received = append(fp.received, receivedRequest{
			Path:   r.URL.Path,
			Body:   m,
			Bearer: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
		})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fp.srv.Close)
	return fp
}

// helper to build a configured bridge against the fake parent.
func newTestBridge(t *testing.T, fp *fakeParent, sessionID, token string) *bridge {
	t.Helper()
	mcpSrv := server.NewMCPServer(bridgeName, bridgeVersion, server.WithToolCapabilities(true))
	b := &bridge{
		cfg: config{
			channelPort: 0,
			apiURL:      fp.srv.URL,
			token:       token,
			sessionID:   sessionID,
		},
		srv: mcpSrv,
	}
	mcpSrv.AddTool(b.replyTool(), b.handleReply)
	return b
}

func TestReplyTool_PostsToParentWithSessionID(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "ralfthewise-cdbb", "sekret")

	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "reply"
	req.Params.Arguments = map[string]any{
		"text": "hello from claude",
	}

	res, err := b.handleReply(context.Background(), req)
	if err != nil || res == nil {
		t.Fatalf("handleReply: err=%v res=%v", err, res)
	}
	if len(fp.received) != 1 {
		t.Fatalf("expected 1 parent request, got %d", len(fp.received))
	}
	got := fp.received[0]
	if got.Path != "/api/channel/reply" {
		t.Errorf("path = %q want /api/channel/reply", got.Path)
	}
	if got.Body["text"] != "hello from claude" {
		t.Errorf("text = %v", got.Body["text"])
	}
	if got.Body["session_id"] != "ralfthewise-cdbb" {
		t.Errorf("session_id = %v want ralfthewise-cdbb", got.Body["session_id"])
	}
	if got.Bearer != "sekret" {
		t.Errorf("bearer = %q want sekret", got.Bearer)
	}
}

func TestReplyTool_ExplicitSessionIDOverridesEnv(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "env-default", "")

	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "reply"
	req.Params.Arguments = map[string]any{
		"text":       "explicit",
		"session_id": "explicit-id",
	}
	if _, err := b.handleReply(context.Background(), req); err != nil {
		t.Fatalf("handleReply: %v", err)
	}
	if got := fp.received[0].Body["session_id"]; got != "explicit-id" {
		t.Errorf("session_id = %v want explicit-id", got)
	}
}

func TestReplyTool_MissingTextErrors(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "", "")

	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "reply"
	req.Params.Arguments = map[string]any{}

	res, err := b.handleReply(context.Background(), req)
	if err != nil {
		t.Fatalf("handleReply unexpected err: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError result for missing text, got %+v", res)
	}
	if len(fp.received) != 0 {
		t.Errorf("parent should not be hit on bad input, got %d calls", len(fp.received))
	}
}

func TestHTTPSendForwardsAsNotification(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "", "")

	srv := httptest.NewServer(b.httpHandler())
	defer srv.Close()

	body := `{"text":"hello","source":"signal","session_id":"abc"}`
	resp, err := http.Post(srv.URL+"/send", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post /send: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d want 200", resp.StatusCode)
	}
	resp.Body.Close()
	// We don't assert on the broadcast itself — there are no MCP clients
	// connected in unit tests — but the handler must not 500.
}

func TestHTTPSendRejectsNonPOST(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "", "")

	srv := httptest.NewServer(b.httpHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/send")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d want 405", resp.StatusCode)
	}
}

func TestHTTPHealthOK(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "", "")
	srv := httptest.NewServer(b.httpHandler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("health: err=%v code=%v", err, resp.StatusCode)
	}
}

func TestNotifyReadyIdempotent(t *testing.T) {
	fp := newFakeParent(t)
	b := newTestBridge(t, fp, "id-1", "")
	b.actualPort = 7000

	if err := b.notifyReady(); err != nil {
		t.Fatalf("notifyReady #1: %v", err)
	}
	if err := b.notifyReady(); err != nil {
		t.Fatalf("notifyReady #2: %v", err)
	}
	if got := len(fp.received); got != 1 {
		t.Errorf("expected 1 ready post (idempotent), got %d", got)
	}
	if fp.received[0].Path != "/api/channel/ready" {
		t.Errorf("path = %q", fp.received[0].Path)
	}
	if fp.received[0].Body["port"].(float64) != 7000 {
		t.Errorf("port = %v want 7000", fp.received[0].Body["port"])
	}
}

func TestPostToParent_FailsOnUnreachable(t *testing.T) {
	// Server immediately closed → connection refused. Ensures the
	// helper surfaces transport errors rather than panicking.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	b := &bridge{cfg: config{apiURL: url}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := b.postToParent(ctx, "/api/channel/reply", map[string]any{"text": "x"}); err == nil {
		t.Fatal("expected error against closed server, got nil")
	}
}
