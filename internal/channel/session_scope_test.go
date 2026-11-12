// BL463 — BuildSelfScope: the shared self-scope builder serving both the
// get_my_scope MCP tool and GET /api/sessions/self/scope. Both surfaces
// report identical data from this single implementation.

package channel

import (
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func scopeTestSession(id, fullID string, agentID string, ready bool) *session.Session {
	return &session.Session{
		ID: id, FullID: fullID, Hostname: "h1",
		Task: "t", State: session.StateRunning,
		ProjectDir: "/tmp/" + id, TmuxSession: "cs-h1-" + id,
		AgentID: agentID, ChannelReady: ready,
	}
}

func TestBuildSelfScope_HostSession(t *testing.T) {
	self := scopeTestSession("aa01", "h1-aa01", "", true)
	peer := scopeTestSession("bb01", "h1-bb01", "", false)
	all := []*session.Session{self, peer}

	sc := BuildSelfScope(self, all)

	if sc.SessionID != "aa01" || sc.FullID != "h1-aa01" || sc.Hostname != "h1" {
		t.Errorf("identity wrong: %+v", sc)
	}
	if sc.Mode != "host" {
		t.Errorf("mode = %q, want host", sc.Mode)
	}
	if sc.MCPServerName != "datawatch-h1-aa01" {
		t.Errorf("mcp_server_name = %q, want datawatch-h1-aa01", sc.MCPServerName)
	}
	if sc.SharedEntryName != ProjectMCPEntryName || ProjectMCPEntryName != "datawatch" {
		t.Errorf("shared entry name = %q (const %q), want datawatch", sc.SharedEntryName, ProjectMCPEntryName)
	}
	if len(sc.Peers) != 1 || sc.Peers[0].FullID != "h1-bb01" {
		t.Fatalf("peers = %+v, want exactly the one peer h1-bb01", sc.Peers)
	}
	if sc.Peers[0].MCPServerName != "datawatch-h1-bb01" {
		t.Errorf("peer mcp_server_name = %q, want datawatch-h1-bb01", sc.Peers[0].MCPServerName)
	}
}

func TestBuildSelfScope_ContainerMode(t *testing.T) {
	self := scopeTestSession("aa01", "h1-aa01", "agent-42", false)
	sc := BuildSelfScope(self, []*session.Session{self})
	if sc.Mode != "container(agent=agent-42)" {
		t.Errorf("mode = %q, want container(agent=agent-42)", sc.Mode)
	}
}

func TestBuildSelfScope_EmptyPeers(t *testing.T) {
	self := scopeTestSession("aa01", "h1-aa01", "", true)
	sc := BuildSelfScope(self, []*session.Session{self})
	if sc.Peers == nil || len(sc.Peers) != 0 {
		t.Errorf("peers = %+v, want non-nil empty slice", sc.Peers)
	}
}

func TestBuildSelfScope_NilPeerList(t *testing.T) {
	self := scopeTestSession("aa01", "h1-aa01", "", true)
	sc := BuildSelfScope(self, nil)
	if sc.Peers == nil {
		t.Error("peers must be non-nil even with a nil all")
	}
}

func TestBuildSelfScope_DistinctPeerNames(t *testing.T) {
	// The isolation guarantee: for any set of sessions, no two scope
	// entries (self or peer) may share an MCP server name, because every
	// name is built from a distinct FullID.
	all := []*session.Session{
		scopeTestSession("aa01", "h1-aa01", "", true),
		scopeTestSession("bb01", "h1-bb01", "", true),
		scopeTestSession("cc01", "h2-cc01", "", true),
	}
	seen := map[string]string{}
	own := BuildSelfScope(all[0], all).MCPServerName
	if seen[own] != "" {
		t.Fatalf("self name %q already used by %q", own, seen[own])
	}
	seen[own] = all[0].FullID
	for _, p := range all[1:] {
		sc := BuildSelfScope(p, all)
		if used, dup := seen[sc.MCPServerName]; dup {
			t.Errorf("MCP server name %q claimed by both %q and %q", sc.MCPServerName, used, p.FullID)
		}
		seen[sc.MCPServerName] = p.FullID
	}
}
