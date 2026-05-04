package secrets

import (
	"errors"
	"testing"
)

func TestCheckScope_NoScopes(t *testing.T) {
	s := Secret{Name: "x", Scopes: nil}
	if err := CheckScope(s, CallerCtx{Type: "agent", Name: "worker"}); err != nil {
		t.Fatalf("nil scopes should be universally accessible, got %v", err)
	}
}

func TestCheckScope_EmptyScopes(t *testing.T) {
	s := Secret{Name: "x", Scopes: []string{}}
	if err := CheckScope(s, CallerCtx{Type: "agent", Name: "worker"}); err != nil {
		t.Fatalf("empty scopes should be universally accessible, got %v", err)
	}
}

func TestCheckScope_ExactMatch(t *testing.T) {
	s := Secret{Name: "x", Scopes: []string{"agent:ci-runner"}}
	if err := CheckScope(s, CallerCtx{Type: "agent", Name: "ci-runner"}); err != nil {
		t.Fatalf("exact match should pass, got %v", err)
	}
}

func TestCheckScope_WrongName(t *testing.T) {
	s := Secret{Name: "x", Scopes: []string{"agent:ci-runner"}}
	err := CheckScope(s, CallerCtx{Type: "agent", Name: "scan-worker"})
	if !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("wrong name should be denied, got %v", err)
	}
}

func TestCheckScope_WildcardName(t *testing.T) {
	s := Secret{Name: "x", Scopes: []string{"agent:*"}}
	if err := CheckScope(s, CallerCtx{Type: "agent", Name: "any-agent-name"}); err != nil {
		t.Fatalf("wildcard should match any agent, got %v", err)
	}
}

func TestCheckScope_WrongType(t *testing.T) {
	s := Secret{Name: "x", Scopes: []string{"plugin:*"}}
	err := CheckScope(s, CallerCtx{Type: "agent", Name: "worker"})
	if !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("wrong type should be denied, got %v", err)
	}
}

func TestCheckScope_TypeOnlyNoColon(t *testing.T) {
	s := Secret{Name: "x", Scopes: []string{"agent"}}
	if err := CheckScope(s, CallerCtx{Type: "agent", Name: "any-name"}); err != nil {
		t.Fatalf("bare type scope should match any caller of that type, got %v", err)
	}
	err := CheckScope(s, CallerCtx{Type: "plugin", Name: "some-plugin"})
	if !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("bare agent scope should deny plugin caller, got %v", err)
	}
}

func TestCheckScope_MultipleScopes(t *testing.T) {
	s := Secret{Name: "x", Scopes: []string{"agent:ci-runner", "plugin:github-hooks"}}
	if err := CheckScope(s, CallerCtx{Type: "agent", Name: "ci-runner"}); err != nil {
		t.Fatalf("agent match in multi-scope list, got %v", err)
	}
	if err := CheckScope(s, CallerCtx{Type: "plugin", Name: "github-hooks"}); err != nil {
		t.Fatalf("plugin match in multi-scope list, got %v", err)
	}
	err := CheckScope(s, CallerCtx{Type: "agent", Name: "other-worker"})
	if !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("non-matching caller should be denied, got %v", err)
	}
}

func TestCheckScope_EmptyScopeEntry(t *testing.T) {
	// Empty strings in Scopes should not cause a spurious match.
	s := Secret{Name: "x", Scopes: []string{"", "agent:ci-runner"}}
	if err := CheckScope(s, CallerCtx{Type: "agent", Name: "ci-runner"}); err != nil {
		t.Fatalf("valid entry after empty string should match, got %v", err)
	}
	err := CheckScope(s, CallerCtx{Type: "agent", Name: "other"})
	if !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("empty scope entry should not cause wildcard match, got %v", err)
	}
}
