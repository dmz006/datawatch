// HLLM-003 (docs/plans/historical-plans/2026-09-02-sec-design-c-audit-config.md
// §C1) — "audit cannot distinguish operator from LLM": every audit entry
// hardcoded Actor: "operator" regardless of who actually presented the
// credential, so a spawned session's secret_get read the same as the
// operator's own in the audit log. Design A3 already gives every spawned
// session its own distinguishable scoped credential; this file is the
// missing piece that reads that identity back out of request context at
// the point an audit entry is written, instead of a hardcoded string.
//
// Full C1 scope (every security-relevant event, both JSONL and CEF) is
// larger than this pass closes — see the CHANGELOG entry for exactly which
// call sites now derive the real actor vs. which still hardcode "operator"
// (a known, flagged gap, not a silent one).

package server

import "context"

// auditActor derives the real caller identity for an audit entry's Actor
// field from request context, mirroring fedCap's own precedence: a
// session-scoped credential (A3) names the owning session; a federation
// peer names itself; anything else (nil peer, nil session caps) is the
// admin token, i.e. "operator". Never returns "" — falls back to
// "operator" if a session token's owning session can't be resolved (e.g.
// the token store isn't wired), matching the pre-HLLM-003 behavior for
// that edge case rather than leaving the field empty.
func (s *Server) auditActor(ctx context.Context) string {
	if sessionCapsFromContext(ctx) != nil {
		if s.sessionTokens != nil {
			if tok := callerTokenFromContext(ctx); tok != "" {
				if sessID := s.sessionTokens.SessionIDForToken(tok); sessID != "" {
					return "session:" + sessID
				}
			}
		}
		return "operator"
	}
	if peer := peerFromContext(ctx); peer != nil {
		return "peer:" + peer.Name
	}
	return "operator"
}
