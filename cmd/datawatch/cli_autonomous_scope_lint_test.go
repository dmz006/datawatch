package main

import (
	"strings"
	"testing"
)

// The scope_lint key and its three values must be discoverable from the
// config-set help, with an example that uses the key.
func TestAutonomousConfigSetHelp_ScopeLintListsKeyAndModes(t *testing.T) {
	help := newAutonomousConfigSetCmd().Long
	if !strings.Contains(help, "scope_lint") {
		t.Fatalf("config-set help lacks scope_lint:\n%s", help)
	}
	if !strings.Contains(help, `config-set '{"scope_lint":"warn"}'`) {
		t.Errorf("config-set help lacks scope_lint example:\n%s", help)
	}
	for _, mode := range []string{"off", "warn", "enforce"} {
		if !strings.Contains(help, mode) {
			t.Errorf("config-set help does not name the %q mode:\n%s", mode, help)
		}
	}
}
