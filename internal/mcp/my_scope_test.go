// BL463 — get_my_scope: a session must be able to confirm its own scope
// (identity + the MCP server name its channel was registered under + the
// shared .mcp.json entry name + mode + the set of every peer's names) and
// confirm that name is distinct from every peer's.

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// scopeFixture saves a self session, one active peer session, and one
// done peer session. Returns the self session's ID.
func scopeFixture(t *testing.T, s *Server) string {
	t.Helper()
	now := time.Now()
	self := &session.Session{
		ID: "aa01", FullID: "testhost-aa01", Hostname: "testhost",
		Name: "research", Task: "t", ProjectDir: "/tmp/proj-aa01",
		TmuxSession: "cs-testhost-aa01", State: session.StateRunning,
		ChannelReady: true, ChannelPort: 8471,
		CreatedAt: now, UpdatedAt: now,
	}
	peer := &session.Session{
		ID: "bb01", FullID: "testhost-bb01", Hostname: "testhost",
		Name: "other", Task: "t", ProjectDir: "/tmp/proj-bb01",
		State: session.StateWaitingInput, ChannelReady: true,
		CreatedAt: now, UpdatedAt: now,
	}
	done := &session.Session{
		ID: "cc01", FullID: "testhost-cc01", Hostname: "testhost",
		Task: "t", State: session.StateComplete,
		CreatedAt: now, UpdatedAt: now,
	}
	for _, ss := range []*session.Session{self, peer, done} {
		if err := s.manager.SaveSession(ss); err != nil { //nolint:errcheck
			t.Fatalf("SaveSession: %v", err)
		}
	}
	return self.ID
}

func callScope(t *testing.T, s *Server, ctx context.Context, args map[string]any) map[string]any {
	t.Helper()
	res, err := s.handleGetMyScope(ctx, call(args)) //nolint:errcheck
	if err != nil {
		t.Fatalf("handleGetMyScope: %v", err)
	}
	text := resultText(t, res, nil)
	if strings.HasPrefix(text, "Error:") {
		t.Fatalf("handler returned error: %s", text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return out
}

// TestGetMyScope_HostSessionConfirmsOwnScope is the requirement of BL463:
// a host-mode session calls get_my_scope and confirms (a) its own identity
// and channel/server names, and (b) that its name is distinct from every
// other session's MCP server name — so it can say which MCP server is
// legitimately "its own" and which are peers it must never use.
func TestGetMyScope_HostSessionConfirmsOwnScope(t *testing.T) {
	s := bl91Server(t)
	selfID := scopeFixture(t, s)

	out := callScope(t, s, context.Background(), map[string]any{"hint": selfID})

	if out["session_id"] != selfID {
		t.Errorf("session_id = %v, want %q", out["session_id"], selfID)
	}
	if out["full_id"] != "testhost-aa01" {
		t.Errorf("full_id = %v, want testhost-aa01", out["full_id"])
	}
	if out["mode"] != "host" {
		t.Errorf("mode = %v, want host (no AgentID set)", out["mode"])
	}
	if out["resolved_via"] != "hint" {
		t.Errorf("resolved_via = %v, want hint", out["resolved_via"])
	}
	if out["mcp_server_name"] != channel.ChannelServerName("testhost-aa01") {
		t.Errorf("mcp_server_name = %v, want %q", out["mcp_server_name"], channel.ChannelServerName("testhost-aa01"))
	}
	if out["shared_entry_name"] != channel.ProjectMCPEntryName {
		t.Errorf("shared_entry_name = %v, want %q", out["shared_entry_name"], channel.ProjectMCPEntryName)
	}
	if out["project_dir"] != "/tmp/proj-aa01" || out["tmux_session"] != "cs-testhost-aa01" {
		t.Errorf("project/tmux identity wrong: %v / %v", out["project_dir"], out["tmux_session"])
	}
	if out["channel_ready"] != true {
		t.Errorf("channel_ready = %v, want true", out["channel_ready"])
	}

	// Isolation surface: both peers named with their own server names, self
	// NOT in its own peers list, and self's name distinct from all peers'.
	raw, _ := json.Marshal(out["peers"]) //nolint:errcheck
	var peers []map[string]any
	if err := json.Unmarshal(raw, &peers); err != nil {
		t.Fatalf("peers malformed: %v", err)
	}
	byID := map[string]bool{}
	own := channel.ChannelServerName("testhost-aa01")
	for _, p := range peers {
		id, _ := p["session_id"].(string) //nolint:errcheck
		byID[id] = true
		if p["mcp_server_name"] == own {
			t.Errorf("peer %q claims self's MCP server name %q — isolation broken", id, own)
		}
	}
	if len(peers) != 2 || !byID["bb01"] || !byID["cc01"] {
		t.Errorf("peers = %v, want exactly bb01 and cc01 (self excluded)", byID)
	}
}

// TestGetMyScope_CallerScopeResolvesScopedCaller proves the in-band
// credential path (HLLM-004): a call arriving over a session token has the
// calling FullID on ctx, and get_my_scope resolves THAT session as self —
// no fallback guessing, even though other channel-ready sessions exist.
func TestGetMyScope_CallerScopeResolvesScopedCaller(t *testing.T) {
	s := bl91Server(t)
	selfID := scopeFixture(t, s)

	ctx := federation.WithCallerSessionID(context.Background(), "testhost-"+selfID)
	out := callScope(t, s, ctx, map[string]any{})

	if out["session_id"] != selfID {
		t.Errorf("session_id = %v, want scoped caller %q", out["session_id"], selfID)
	}
	if out["resolved_via"] != "caller-scope" {
		t.Errorf("resolved_via = %v, want caller-scope", out["resolved_via"])
	}
}

// TestGetMyScope_UnscopedCallFallsBackToChannelReady matches the pre-existing
// BL349 fallback behavior for unscoped (admin) calls, and flags it in
// resolved_via so the first-come ambiguity (BL467) is visible, not silent.
func TestGetMyScope_UnscopedCallFallsBackToChannelReady(t *testing.T) {
	s := bl91Server(t)
	now := time.Now()
	// Exactly ONE channel-ready active session — with two, the fallback is
	// genuinely ambiguous (that is BL467's whole point, surfaced via
	// resolved_via, not asserted as a winner here).
	save := &session.Session{
		ID: "aa01", FullID: "testhost-aa01", Hostname: "testhost",
		Name: "research", Task: "t", State: session.StateRunning, ChannelReady: true,
		CreatedAt: now, UpdatedAt: now,
	}
	done := &session.Session{
		ID: "bb01", FullID: "testhost-bb01", Hostname: "testhost",
		Task: "t", State: session.StateComplete, // not a fallback candidate
		CreatedAt: now, UpdatedAt: now,
	}
	for _, ss := range []*session.Session{save, done} {
		if err := s.manager.SaveSession(ss); err != nil { //nolint:errcheck
			t.Fatalf("SaveSession: %v", err)
		}
	}

	out := callScope(t, s, context.Background(), map[string]any{})

	if out["session_id"] != "aa01" {
		t.Errorf("fallback should pick the channel-ready active session aa01, got %v", out["session_id"])
	}
	if out["resolved_via"] != "channel-ready-fallback" {
		t.Errorf("resolved_via = %v, want channel-ready-fallback", out["resolved_via"])
	}
}

// TestGetMyScope_ContainerMode reports container (F10 agent) mode
// instead of host, driven by the same AgentID field the F10 pipeline sets.
func TestGetMyScope_ContainerMode(t *testing.T) {
	s := bl91Server(t)
	now := time.Now()
	_ = s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "dd01", FullID: "testhost-dd01", Hostname: "testhost",
		Task: "t", State: session.StateRunning, AgentID: "agent-xyz",
		CreatedAt: now, UpdatedAt: now,
	})

	out := callScope(t, s, context.Background(), map[string]any{"hint": "dd01"})
	if out["mode"] != "container(agent=agent-xyz)" {
		t.Errorf("mode = %v, want container(agent=agent-xyz)", out["mode"])
	}
}

// TestGetMyScope_HintUnknown is a clean error, not a panic and not an
// accidental self-resolution to some other session.
func TestGetMyScope_HintUnknown(t *testing.T) {
	s := bl91Server(t)
	scopeFixture(t, s)

	res, err := s.handleGetMyScope(context.Background(), call(map[string]any{"hint": "nope99"})) //nolint:errcheck
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := resultText(t, res, nil)
	if !strings.Contains(text, "not found") {
		t.Errorf("expected a 'not found' error, got: %s", text)
	}
}

// TestGetMyScope_NoSessions errors cleanly when no session exists at all.
func TestGetMyScope_NoSessions(t *testing.T) {
	s := bl91Server(t)

	res, err := s.handleGetMyScope(context.Background(), call(map[string]any{})) //nolint:errcheck
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := resultText(t, res, nil)
	if !strings.Contains(text, "no self-scope") {
		t.Errorf("expected a 'no self-scope' error, got: %s", text)
	}
}
