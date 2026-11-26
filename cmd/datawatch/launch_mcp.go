package main

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/session"
)

// sessionMCPConfigEnv names the variable, set in the tmux environment of a
// session that is not claude-code, holding the path of its private MCP file.
// It carries a path, never a token.
const sessionMCPConfigEnv = "DATAWATCH_MCP_CONFIG"

// launchMCPInput is what the pre-launch MCP wiring of one session needs.
type launchMCPInput struct {
	// DataDir is the daemon's expanded data directory.
	DataDir string
	// ChannelJSPath is the fallback JS bridge when no native bridge exists.
	ChannelJSPath string
	// Env is this session's own bridge environment (API URL, CA path and
	// the session's scoped token).
	Env map[string]string
	// Extras are the configured extra MCP servers.
	Extras map[string]channel.MCPServerSpec
	// SetSessionEnv sets a variable in the session's tmux environment.
	SetSessionEnv func(tmuxSession, key, value string) error
}

// wireSessionMCPFiles writes the MCP configuration one session starts with.
// The datawatch entry (token and identity) goes only to the session's private
// launch folder; the project .mcp.json gets the configured extras and no
// datawatch entry.
//
// claude-code without a profile needs no file entry: its bridge is registered
// per session (channel.RegisterSessionMCP), and a project "datawatch" entry
// would spawn a second bridge on the default port.
func wireSessionMCPFiles(sess *session.Session, in launchMCPInput) {
	// A file left by an earlier run under this name never carries into this one.
	if err := channel.RemoveSessionMCPConfig(in.DataDir, sess.TmuxSession); err != nil {
		debugf("session MCP file for %s: %v", sess.FullID, err)
	}

	claude := sess.BackendFamily == "claude-code"
	// A profiled claude-code launch selecting datawatch starts with
	// --strict-mcp-config, so it needs this session's entry in its file. A
	// session whose profile already fell back launches profile-less.
	profiled := claude && sess.WorkerProfile != nil && sess.ProfileFallbackReason == "" &&
		sess.WorkerProfile.MCP.Includes(channel.ProjectMCPEntryName)

	if claude && !profiled {
		// Legacy global entry left by older daemon versions.
		if err := channel.RemoveProjectMCPEntry(sess.ProjectDir, channel.ProjectMCPEntryName); err != nil {
			debugf("BL109 remove stale .mcp.json datawatch entry: %v", err)
		}
	} else {
		if removed, err := channel.RemoveProjectDaemonEntry(sess.ProjectDir, nil); err != nil {
			fmt.Printf("[warn] remove old daemon entry from %s/.mcp.json: %v\n", sess.ProjectDir, err)
		} else if removed {
			fmt.Printf("[channel] removed an old daemon entry (it held a token) from %s/.mcp.json\n", sess.ProjectDir)
		}
		env := make(map[string]string, len(in.Env)+2)
		for k, v := range in.Env {
			env[k] = v
		}
		if profiled {
			env["CLAUDE_SESSION_ID"] = sess.FullID
			env["DATAWATCH_CHANNEL_PORT"] = "0"
		}
		path, err := channel.WriteSessionMCPConfig(in.DataDir, sess.TmuxSession, in.ChannelJSPath, env, in.Extras)
		switch {
		case err != nil:
			fmt.Printf("[warn] session %s: MCP file not written: %v\n", sess.FullID, err)
		case profiled:
			fmt.Printf("[channel] session %s: profile MCP=%q — wrote its private MCP file %s\n",
				sess.FullID, sess.WorkerProfile.MCP.Mode, path)
		case in.SetSessionEnv != nil:
			debugf("session %s (backend=%s): private MCP file %s", sess.FullID, sess.BackendFamily, path)
			if err := in.SetSessionEnv(sess.TmuxSession, sessionMCPConfigEnv, path); err != nil {
				debugf("set %s for %s: %v", sessionMCPConfigEnv, sess.FullID, err)
			}
		}
	}

	// Extra MCP servers stay in the project file (no datawatch entry).
	if err := channel.InjectExtrasIntoMCPConfig(sess.ProjectDir, in.Extras); err != nil {
		fmt.Printf("[warn] session %s: %v\n", sess.FullID, err)
	}
}

// endSessionMCPFiles removes what belongs to one ended session: its private
// launch folder, and a project entry that names this very session (written by
// an earlier daemon). An entry naming no session, or another one, stays.
func endSessionMCPFiles(sess *session.Session, dataDir string) {
	if err := channel.RemoveSessionMCPConfig(dataDir, sess.TmuxSession); err != nil {
		debugf("session MCP file cleanup for %s: %v", sess.FullID, err)
	}
	if _, err := channel.RemoveProjectDaemonEntry(sess.ProjectDir, func(env map[string]string) bool {
		return sess.FullID != "" && env["CLAUDE_SESSION_ID"] == sess.FullID
	}); err != nil {
		debugf("profile .mcp.json datawatch cleanup: %v", err)
	}
}

// sessionLaunchMCPSpecs returns the MCP server specs one session started
// with, so the idle check knows the session's own servers from its work.
func sessionLaunchMCPSpecs(tmuxSession, projectDir string) []channel.MCPServerSpec {
	specs := sessionOwnMCPServers(projectDir)
	for _, spec := range channel.SessionMCPServers(tmuxSession) {
		specs = append(specs, spec)
	}
	return specs
}
