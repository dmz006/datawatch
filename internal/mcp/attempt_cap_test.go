package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// resetBody runs the reset_task tool against a stand-in daemon and returns the
// body the daemon received.
func resetBody(t *testing.T, ctx context.Context, args map[string]any) map[string]any {
	t.Helper()
	var got map[string]any
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer rest.Close()
	_, portStr, err := net.SplitHostPort(rest.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	s := &Server{webPort: port}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = args
	if _, err := s.handleAutonomousPRDResetTask(ctx, req); err != nil {
		t.Fatalf("handler: %v", err)
	}
	return got
}

// TestAttemptCap_ResetActor: a reset made with a session's scoped credential is
// sent as that session's, so it cannot clear the attempt count; one made with
// the admin credential is the operator's.
func TestAttemptCap_ResetActor(t *testing.T) {
	args := map[string]any{"id": "p1", "task_id": "t1", "force": true}
	if got := resetBody(t, context.Background(), args); got["actor"] != "operator" || got["force"] != true {
		t.Fatalf("admin reset body = %v, want actor operator and force true", got)
	}
	withReason := map[string]any{"id": "p1", "task_id": "t1", "force": true, "reason": "retry by hand"}
	if got := resetBody(t, context.Background(), withReason); got["reason"] != "retry by hand" {
		t.Fatalf("reset body = %v, want the reason passed through", got)
	}
	ctx := federation.WithCallerSessionID(context.Background(), "host-sess-9")
	if got := resetBody(t, ctx, args); got["actor"] != "session:host-sess-9" {
		t.Fatalf("session reset body actor = %v, want session:host-sess-9", got["actor"])
	}
}

func TestAttemptCap_ConfigArgs(t *testing.T) {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"attempt_cap_enabled":         false,
		"attempt_cap_max_total":       float64(4),
		"attempt_cap_backoff_minutes": []any{},
	}
	got := attemptCapConfigArgs(req)
	if len(got) != 3 || got["autonomous.attempt_cap.enabled"] != false || got["autonomous.attempt_cap.max_total"] != float64(4) {
		t.Fatalf("args = %v", got)
	}
	if l, ok := got["autonomous.attempt_cap.backoff_minutes"].([]any); !ok || len(l) != 0 {
		t.Fatalf("an explicit empty list must be sent as given, got %v", got["autonomous.attempt_cap.backoff_minutes"])
	}
	if len(attemptCapConfigArgs(mcpsdk.CallToolRequest{})) != 0 {
		t.Fatalf("no arguments must send nothing")
	}
	if n := len(attemptCapConfigOptions()); n != 4 {
		t.Fatalf("options = %d, want 4", n)
	}
}
