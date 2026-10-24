// Gate scope setting (full | touched-plus-smoke | touched) and
// smoke_packages on the CLI surface: the autonomous config-set /
// config-get help must name both keys and the valid scope values.
package main

import (
	"strings"
	"testing"
)

func TestGateScopeConfigSetHelp_MentionsScopeAndSmokePackages(t *testing.T) {
	help := newAutonomousConfigSetCmd().Long
	for _, want := range []string{
		"default_quality_gates",
		"scope",
		"smoke_packages",
		"touched-plus-smoke",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("config-set help lacks %q:\n%s", want, help)
		}
	}
	if !strings.Contains(help, `{"default_quality_gates":{"scope":"touched-plus-smoke","smoke_packages":["internal/pipeline","internal/autonomous"]}}`) {
		t.Errorf("config-set help lacks the scope/smoke_packages example:\n%s", help)
	}
}

func TestGateScopeConfigGetHelp_MentionsScopeAndSmokePackages(t *testing.T) {
	help := newAutonomousConfigGetCmd().Long
	if !strings.Contains(help, "scope") {
		t.Errorf("config-get help lacks scope:\n%s", help)
	}
	if !strings.Contains(help, "smoke_packages") {
		t.Errorf("config-get help lacks smoke_packages:\n%s", help)
	}
	// The three valid values must be named.
	for _, v := range []string{"full", "touched-plus-smoke", "touched"} {
		if !strings.Contains(help, v) {
			t.Errorf("config-get help does not name %q:\n%s", v, help)
		}
	}
}
