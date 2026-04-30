// v5.27.5 — tests for the new claude-code per-session overrides
// (permission_mode, model, effort) and their interaction with the
// legacy --dangerously-skip-permissions shortcut.

package claudecode

import (
	"strings"
	"testing"
)

func TestPostFlagsStr_PermissionModeOverridesSkipPermissions(t *testing.T) {
	// When both skipPermissions=true AND permissionMode is set, the
	// explicit mode wins and --dangerously-skip-permissions is dropped.
	b := &Backend{skipPermissions: true, permissionMode: "plan"}
	got := b.postFlagsStr()
	if !strings.Contains(got, "--permission-mode 'plan'") {
		t.Errorf("missing --permission-mode 'plan' in %q", got)
	}
	if strings.Contains(got, "--dangerously-skip-permissions") {
		t.Errorf("--dangerously-skip-permissions must be suppressed when permissionMode set; got %q", got)
	}
}

func TestPostFlagsStr_LegacySkipPermissionsStillWorks(t *testing.T) {
	// permissionMode empty + skipPermissions true → legacy flag still fires.
	b := &Backend{skipPermissions: true}
	got := b.postFlagsStr()
	if !strings.Contains(got, "--dangerously-skip-permissions") {
		t.Errorf("legacy --dangerously-skip-permissions missing from %q", got)
	}
	if strings.Contains(got, "--permission-mode") {
		t.Errorf("--permission-mode must be absent when permissionMode is empty; got %q", got)
	}
}

func TestPostFlagsStr_ModelAndEffort(t *testing.T) {
	b := &Backend{model: "sonnet", effort: "high"}
	got := b.postFlagsStr()
	if !strings.Contains(got, "--model 'sonnet'") {
		t.Errorf("missing --model 'sonnet' in %q", got)
	}
	if !strings.Contains(got, "--effort 'high'") {
		t.Errorf("missing --effort 'high' in %q", got)
	}
}

func TestPostFlagsStr_FullCombination(t *testing.T) {
	b := &Backend{permissionMode: "plan", model: "opus", effort: "max", sessionName: "PRD-design"}
	got := b.postFlagsStr()
	for _, want := range []string{
		"--permission-mode 'plan'",
		"--model 'opus'",
		"--effort 'max'",
		"--name 'PRD-design'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestPostFlagsStr_EmptyAllReturnsEmpty(t *testing.T) {
	b := &Backend{}
	if got := b.postFlagsStr(); got != "" {
		t.Errorf("expected empty flags for default backend, got %q", got)
	}
}

func TestSetters_TypeAssertion(t *testing.T) {
	// The session manager applies overrides via interface assertions
	// like `backendObj.(interface{ SetPermissionMode(string) })`.
	// Verify the concrete *Backend satisfies each contract.
	var b interface{} = &Backend{}
	if _, ok := b.(interface{ SetPermissionMode(string) }); !ok {
		t.Error("*Backend must implement SetPermissionMode(string)")
	}
	if _, ok := b.(interface{ SetModel(string) }); !ok {
		t.Error("*Backend must implement SetModel(string)")
	}
	if _, ok := b.(interface{ SetEffort(string) }); !ok {
		t.Error("*Backend must implement SetEffort(string)")
	}
}
