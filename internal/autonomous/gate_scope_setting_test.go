package autonomous

// Gate-scope Setting model (docs/plans/2026-10-23-quality-gate-scope.md):
// resolution order per-Automaton → global → built-in default, an empty
// Automaton scope never hides a global scope, smoke-list inheritance,
// SetConfig sanitising of unrecognized scopes, and SetPRDQualityGates
// accept/reject of scope values.

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/pipeline"
)

func newGateScopeManager(t *testing.T, global pipeline.QualityGateConfig) *Manager {
	t.Helper()
	mgr, err := NewManager(t.TempDir(), Config{Enabled: true, AutoFixRetries: 0, DefaultQualityGates: global}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

func setGateScopeOnPRD(t *testing.T, mgr *Manager, scope string, smoke []string) *PRD {
	t.Helper()
	prd, err := mgr.Store().CreatePRD("gate scope", "/proj", "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	got, err := mgr.SetPRDQualityGates(prd.ID, true, "go test ./...", 30, true, scope, smoke)
	if err != nil {
		t.Fatalf("SetPRDQualityGates(scope=%q): %v", scope, err)
	}
	return got
}

func TestGateScope_ResolveGateScopeSettingsOverrideOrder(t *testing.T) {
	cases := []struct {
		name                  string
		prdScope              string
		globalScope           string
		wantScope, wantSource string
	}{
		{"automaton_touched_wins", pipeline.GateScopeTouched, pipeline.GateScopeFull, pipeline.GateScopeTouched, "automaton"},
		{"automaton_full_beats_global_touched", pipeline.GateScopeFull, pipeline.GateScopeTouched, pipeline.GateScopeFull, "automaton"},
		{"empty_automaton_inherits_global", "", pipeline.GateScopeTouchedPlusSmoke, pipeline.GateScopeTouchedPlusSmoke, "global"},
		{"both_empty_inherit_default_full", "", "", pipeline.GateScopeFull, "default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := newGateScopeManager(t, pipeline.QualityGateConfig{
				Enabled:     true,
				TestCommand: "go test ./...",
				Scope:       tc.globalScope,
			})
			prd := setGateScopeOnPRD(t, mgr, tc.prdScope, nil)
			gotScope, gotSource, _ := mgr.resolveGateScopeSettings(prd)
			if gotScope != tc.wantScope || gotSource != tc.wantSource {
				t.Errorf("resolveGateScopeSettings = (%q, %q), want (%q, %q)",
					gotScope, gotSource, tc.wantScope, tc.wantSource)
			}

			// PRD with no gate config at all also falls to the global value.
			bare, err := mgr.Store().CreatePRD("bare", "/proj", "claude-code", "", EffortNormal)
			if err != nil {
				t.Fatalf("CreatePRD: %v", err)
			}
			wantBareSource := "global"
			wantBareScope := tc.globalScope
			if tc.globalScope == "" {
				wantBareScope, wantBareSource = pipeline.GateScopeFull, "default"
			}
			bareScope, bareSource, _ := mgr.resolveGateScopeSettings(bare)
			if bareScope != wantBareScope || bareSource != wantBareSource {
				t.Errorf("no-config PRD resolved = (%q, %q), want (%q, %q)",
					bareScope, bareSource, wantBareScope, wantBareSource)
			}
		})
	}
}

func TestGateScope_EmptyAutomatonScopeInheritsGlobalOnResolve(t *testing.T) {
	mgr := newGateScopeManager(t, pipeline.QualityGateConfig{
		Enabled:     true,
		TestCommand: "make test",
		Scope:       pipeline.GateScopeTouchedPlusSmoke,
	})
	prd := setGateScopeOnPRD(t, mgr, "", nil) // per-PRD override exists but its scope is empty

	resolved := mgr.resolveQualityGates(prd)
	if resolved.Scope != pipeline.GateScopeTouchedPlusSmoke {
		t.Errorf("resolved scope = %q, want %q (empty Automaton scope must not hide the global scope)",
			resolved.Scope, pipeline.GateScopeTouchedPlusSmoke)
	}
	if resolved.TestCommand != "go test ./..." {
		t.Errorf("other per-PRD fields must keep current behavior: TestCommand = %q", resolved.TestCommand)
	}
}

func TestGateScope_SmokeListInheritance(t *testing.T) {
	globalSmoke := []string{"./internal/server/...", "./internal/audit"}
	mgr := newGateScopeManager(t, pipeline.QualityGateConfig{
		Enabled:       true,
		TestCommand:   "go test ./...",
		SmokePackages: globalSmoke,
	})
	prdSmoke := []string{"./internal/pipeline", "./internal/audit"}

	t.Run("automaton_smoke_wins", func(t *testing.T) {
		prd := setGateScopeOnPRD(t, mgr, pipeline.GateScopeTouchedPlusSmoke, prdSmoke)
		_, _, got := mgr.resolveGateScopeSettings(prd)
		if !reflect.DeepEqual(got, prdSmoke) {
			t.Errorf("smoke = %v, want per-PRD %v", got, prdSmoke)
		}
	})

	t.Run("empty_automaton_smoke_inherits_global", func(t *testing.T) {
		prd := setGateScopeOnPRD(t, mgr, "", nil)
		_, _, got := mgr.resolveGateScopeSettings(prd)
		if !reflect.DeepEqual(got, globalSmoke) {
			t.Errorf("smoke = %v, want global %v", got, globalSmoke)
		}
		// resolveQualityGates must agree with the resolver.
		resolved := mgr.resolveQualityGates(prd)
		if !reflect.DeepEqual(resolved.SmokePackages, globalSmoke) {
			t.Errorf("resolved smoke = %v, want global %v", resolved.SmokePackages, globalSmoke)
		}
	})

	t.Run("both_empty_smoke_inherits_builtin", func(t *testing.T) {
		mgrBare := newGateScopeManager(t, pipeline.QualityGateConfig{Enabled: true, TestCommand: "go test ./..."})
		prd := setGateScopeOnPRD(t, mgrBare, "", nil)
		_, _, got := mgrBare.resolveGateScopeSettings(prd)
		if !reflect.DeepEqual(got, pipeline.DefaultGateSmokePackages) {
			t.Errorf("smoke = %v, want built-in %v", got, pipeline.DefaultGateSmokePackages)
		}
	})
}

func TestGateScope_SetConfigSanitisesUnrecognizedScope(t *testing.T) {
	base := Config{Enabled: true, AutoFixRetries: 0}
	mgr := newGateScopeManager(t, pipeline.QualityGateConfig{})

	mgr.SetConfig(autonomouscfgForGateScopeTest(base, "typo-value"))
	if got := mgr.Config().DefaultQualityGates.Scope; got != "" {
		t.Errorf("unrecognized scope must be sanitized to empty, got %q", got)
	}

	for _, valid := range []string{"full", "touched-plus-smoke", "touched"} {
		mgr.SetConfig(autonomouscfgForGateScopeTest(base, valid))
		if got := mgr.Config().DefaultQualityGates.Scope; got != valid {
			t.Errorf("valid scope %q was dropped, got %q", valid, got)
		}
	}
}

// autonomouscfgForGateScopeTest builds a manager Config carrying the given
// default gate scope (helper local to this test).
func autonomouscfgForGateScopeTest(base Config, scope string) Config {
	base.DefaultQualityGates = pipeline.QualityGateConfig{
		Enabled:     true,
		TestCommand: "go test ./...",
		Scope:       scope,
	}
	return base
}

func TestGateScope_SetPRDQualityGatesAcceptsValidScopes(t *testing.T) {
	for _, scope := range []string{"", pipeline.GateScopeFull, pipeline.GateScopeTouchedPlusSmoke, pipeline.GateScopeTouched} {
		t.Run("scope="+scope, func(t *testing.T) {
			mgr := newGateScopeManager(t, pipeline.QualityGateConfig{Enabled: true, TestCommand: "make test"})
			prd, err := mgr.Store().CreatePRD("scope set", "/proj", "claude-code", "", EffortNormal)
			if err != nil {
				t.Fatalf("CreatePRD: %v", err)
			}
			got, err := mgr.SetPRDQualityGates(prd.ID, true, "go test ./...", 30, true, scope, nil)
			if err != nil {
				t.Fatalf("SetPRDQualityGates(scope=%q): %v", scope, err)
			}
			if got.QualityGates == nil || got.QualityGates.Scope != scope {
				t.Errorf("stored scope = %+v, want %q", got.QualityGates, scope)
			}
		})
	}
}

func TestGateScope_SetPRDQualityGatesRejectsInvalidScope(t *testing.T) {
	mgr := newGateScopeManager(t, pipeline.QualityGateConfig{Enabled: true, TestCommand: "make test"})
	prd, err := mgr.Store().CreatePRD("scope reject", "/proj", "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_, err = mgr.SetPRDQualityGates(prd.ID, true, "go test ./...", 30, true, "bogus-scope", nil)
	if err == nil {
		t.Fatal("SetPRDQualityGates with an unrecognized scope succeeded, want a 400-able error")
	}
	stored, ok := mgr.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing after rejected call")
	}
	if stored.QualityGates != nil {
		t.Errorf("rejected call must not store gate config, got %+v", stored.QualityGates)
	}
}
