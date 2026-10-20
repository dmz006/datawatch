package channel

import "github.com/dmz006/datawatch/internal/session"

// SelfScope is the self-description a session needs to answer "what is my
// scope?" — identity (id, full_id, hostname, state, name), which MCP server
// name its channel was registered under (the per-session
// ChannelServerName(FullID), the authoritative one for claude-code
// user-scope registration), the shared/global .mcp.json entry name
// (ProjectMCPEntryName), which mode it runs in (host vs container/agent
// per the F10 AgentID marker), the project/tmux surface it occupies, and —
// for isolation clarity — every other session on the daemon with each one's
// own MCP server name, so a session can confirm its name is distinct from
// every peer's and never use one of theirs (BL463).
//
// BuildSelfScope fills one from a session record + the daemon's full session
// list. internal/mcp (get_my_scope) and internal/server
// (GET /api/sessions/self/scope) both serve it, so the MCP tool and its REST
// mirror report identical data — ResolvedVia is set by the serving side
// (only it knows how the self-session was resolved for that specific call).
type SelfScope struct {
	SessionID          string      `json:"session_id"`
	FullID             string      `json:"full_id"`
	Hostname           string      `json:"hostname"`
	State              string      `json:"state"`
	Name               string      `json:"name,omitempty"`
	BackendFamily      string      `json:"backend_family,omitempty"`
	Mode               string      `json:"mode"` // "host" | "container(agent=<agent_id>)"
	ProjectDir         string      `json:"project_dir"`
	TmuxSession        string      `json:"tmux_session"`
	EphemeralWorkspace bool        `json:"ephemeral_workspace"` // daemon-owned ProjectDir (reaped on delete)
	ChannelReady       bool        `json:"channel_ready"`
	ChannelPort        int         `json:"channel_port,omitempty"`
	MCPServerName      string      `json:"mcp_server_name"` // ChannelServerName(FullID) — this session's channel
	SharedEntryName    string      `json:"shared_entry_name"` // ProjectMCPEntryName — the legacy/global .mcp.json entry
	ResolvedVia        string      `json:"resolved_via"`    // "hint" | "caller-scope" | "channel-ready-fallback" (set by caller)
	Peers              []PeerScope `json:"peers"`
}

// PeerScope is one other session, named the same way SelfScope is, so a
// session can compare its own MCP server name against every peer's.
type PeerScope struct {
	SessionID     string `json:"session_id"`
	FullID        string `json:"full_id"`
	State         string `json:"state"`
	ChannelReady  bool   `json:"channel_ready"`
	MCPServerName string `json:"mcp_server_name"`
}

// BuildSelfScope computes the scope payload for sess from the daemon's full
// session list (all). Peers is every other session in all (stable: same
// order as ListSessions), each with its own MCP channel name. sess itself
// never appears in Peers.
func BuildSelfScope(sess *session.Session, all []*session.Session) SelfScope {
	sc := SelfScope{
		SessionID:          sess.ID,
		FullID:             sess.FullID,
		Hostname:           sess.Hostname,
		State:              string(sess.State),
		Name:               sess.Name,
		BackendFamily:      sess.BackendFamily,
		Mode:               "host",
		ProjectDir:         sess.ProjectDir,
		TmuxSession:        sess.TmuxSession,
		EphemeralWorkspace: sess.EphemeralWorkspace,
		ChannelReady:       sess.ChannelReady,
		ChannelPort:        sess.ChannelPort,
		MCPServerName:      ChannelServerName(sess.FullID),
		SharedEntryName:    ProjectMCPEntryName,
		Peers:              []PeerScope{},
	}
	if sess.AgentID != "" {
		// F10 marker (internal/session/store.go AgentID): this session runs
		// inside a parent-spawned worker container, not directly on host.
		sc.Mode = "container(agent=" + sess.AgentID + ")"
	}
	for _, other := range all {
		if other == nil || other.FullID == "" || other.FullID == sess.FullID {
			continue
		}
		sc.Peers = append(sc.Peers, PeerScope{
			SessionID:     other.ID,
			FullID:        other.FullID,
			State:         string(other.State),
			ChannelReady:  other.ChannelReady,
			MCPServerName: ChannelServerName(other.FullID),
		})
	}
	return sc
}
