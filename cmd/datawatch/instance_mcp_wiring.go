package main

import (
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// instance_mcp_wiring.go — daemon-side wiring for the scoped-instance-token
// lifecycle (channel.InstanceMCPConfig) so the daemon's own long-lived
// server token never lands in the instance-level .mcp.json.
//
// The daemon holds one *channel.InstanceMCPConfig per run (built where the
// claude channel is wired, see main.go):
//
//   - Refresh on every session pre-launch: the .mcp.json gets a short-lived
//     token for the instance owner instead of the server token.
//   - Release when no live sessions remain (last session ended, or daemon
//     shutdown): the scoped token is revoked and the .mcp.json entry is
//     removed, so no stale credential sits on disk.
//
// The small pure helpers below keep the release decision (live-session
// counting) and the session grant derivation testable without a daemon.

// instanceMCPLifecycle is the slice of channel.InstanceMCPConfig (or a test
// fake) the daemon's release path needs.
type instanceMCPLifecycle interface {
	Refresh() (bool, error)
	Release() error
}

// instanceMCPLiveSessions counts sessions that are not in a terminal state
// (session.State.IsTerminal: complete / failed / killed), ignoring the
// session that is ending (excludeFullID, "" when releasing from a path
// without a specific ending session, e.g. daemon shutdown).
func instanceMCPLiveSessions(sessions []*session.Session, excludeFullID string) int {
	n := 0
	for _, s := range sessions {
		if s == nil {
			continue
		}
		if excludeFullID != "" && s.FullID == excludeFullID {
			continue
		}
		if !s.State.IsTerminal() {
			n++
		}
	}
	return n
}

// releaseInstanceMCPIfIdle implements the release rule: revoke the
// instance token and remove the .mcp.json entry only when no OTHER live
// session remains after the ending session is excluded. A non-nil lifecycle
// with at least one other live session leaves the token in place. Release
// failures are logged (debugf) so a bridge cannot authenticate with a
// revoked token while the operator still has a live session. Returns true
// when Release was called.
func releaseInstanceMCPIfIdle(l instanceMCPLifecycle, sessions []*session.Session, endingFullID string) bool {
	if l == nil {
		return false
	}
	if n := instanceMCPLiveSessions(sessions, endingFullID); n != 0 {
		return false
	}
	if err := l.Release(); err != nil {
		debugf("instance .mcp.json release: %v", err)
	}
	return true
}

// instanceSessionGrantCaps derives the capability grant list: the explicit
// per-session capabilities (cfg.Session.Capabilities) when set, otherwise
// the built-in session default grant (which folds in the self-config
// alias per selfConfigEnabled). Always returned resolved against the
// default groups (federation.Resolve), so callers hand straight capability
// names to a token minter.
func instanceSessionGrantCaps(explicitCaps []string, selfConfigEnabled bool) []string {
	caps := explicitCaps
	if len(caps) == 0 {
		caps = federation.SessionDefaultGrantCaps(selfConfigEnabled)
	}
	return federation.Resolve(caps, nil)
}
