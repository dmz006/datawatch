// BL242 Phase 5a — secret scope enforcement.
//
// Scopes restrict which automated callers may access a secret at runtime.
// Operator access (CLI, REST with daemon bearer token, MCP) is always
// unrestricted and never calls CheckScope.
//
// Scope format: "type:name" or "type" (any-name wildcard).
//   agent:ci-runner   — only the agent profile named ci-runner
//   agent:*           — any agent
//   plugin:gh-hooks   — only the plugin named gh-hooks
//   plugin:*          — any plugin
//   service:imap-mcp  — only the external service named imap-mcp
//                        (GH#203 — ServiceTokenStore; a persistent,
//                        operator-minted token for an independent
//                        service that is neither a spawned F10 agent
//                        nor a federation peer)
//   service:*         — any external service
//   agent             — any agent (equivalent to agent:*)
//
// Empty Scopes slice → universally accessible to agent/plugin callers
// (backward compatible) — EXCEPT a "service" caller, which always requires
// an explicit scope (see CheckScope's doc comment for why).

package secrets

import (
	"errors"
	"strings"
)

// ErrScopeDenied is returned when a caller's identity does not match any
// declared scope on the secret. Use errors.Is to check.
var ErrScopeDenied = errors.New("secret access denied: caller not in scope")

// CallerCtx identifies an automated caller requesting a secret at runtime.
// Type is "agent", "plugin", or "service" (GH#203). Name is the profile/
// plugin/service name. Operator access never constructs a CallerCtx — it
// bypasses scope checks.
type CallerCtx struct {
	Type string // "agent" | "plugin" | "service"
	Name string
}

// CheckScope returns ErrScopeDenied when caller does not match any declared
// scope. Returns nil when the secret has no scopes (universally accessible
// to agent/plugin callers, for backward compatibility) or when at least one
// scope entry matches the caller.
//
// Security fix (2026-10-09, found by a peer session coordinating the
// imap-mcp rollout, independently verified): the "empty scopes = universal"
// backward-compat rule predates GH#203's "service" caller type and was
// written for agent/plugin callers only — short-lived, in-process, spawned
// by this daemon. A service token is persistent and reaches this check over
// the network from an external, non-sandboxed process
// (GET /api/external/secrets/{name}); applying the same "empty = universal"
// default to it silently widened every unscoped secret's exposure the
// moment GH#203 shipped. A service caller now ALWAYS requires an explicit
// service:<name> or service:* scope — agent/plugin behavior is unchanged.
func CheckScope(secret Secret, caller CallerCtx) error {
	if len(secret.Scopes) == 0 {
		if caller.Type == "service" {
			return ErrScopeDenied
		}
		return nil
	}
	for _, s := range secret.Scopes {
		if matchScope(s, caller) {
			return nil
		}
	}
	return ErrScopeDenied
}

// matchScope reports whether a single scope entry matches caller.
func matchScope(scope string, caller CallerCtx) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return false
	}
	parts := strings.SplitN(scope, ":", 2)
	if parts[0] != caller.Type {
		return false
	}
	if len(parts) == 1 {
		// "agent" or "plugin" with no name qualifier → any of that type
		return true
	}
	name := parts[1]
	return name == "*" || name == caller.Name
}
