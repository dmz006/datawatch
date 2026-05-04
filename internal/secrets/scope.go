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
//   agent             — any agent (equivalent to agent:*)
//
// Empty Scopes slice → universally accessible (backward compatible).

package secrets

import (
	"errors"
	"strings"
)

// ErrScopeDenied is returned when a caller's identity does not match any
// declared scope on the secret. Use errors.Is to check.
var ErrScopeDenied = errors.New("secret access denied: caller not in scope")

// CallerCtx identifies an automated caller requesting a secret at runtime.
// Type is "agent" or "plugin". Name is the profile name or plugin name.
// Operator access never constructs a CallerCtx — it bypasses scope checks.
type CallerCtx struct {
	Type string // "agent" | "plugin"
	Name string
}

// CheckScope returns ErrScopeDenied when caller does not match any declared
// scope. Returns nil when the secret has no scopes (universally accessible)
// or when at least one scope entry matches the caller.
func CheckScope(secret Secret, caller CallerCtx) error {
	if len(secret.Scopes) == 0 {
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
