package inference

import (
	"reflect"
	"strings"
	"testing"
)

func roleEntries() []*LLM {
	return []*LLM{
		{Name: "zeta", Kind: KindOllama, Roles: []string{"planning"}},
		{Name: "alpha", Kind: KindOllama, Roles: []string{"planning", "verification"}},
		{Name: "plain", Kind: KindOllama},
		{Name: "off", Kind: KindOllama, Roles: []string{"verification"}, Disabled: true},
	}
}

func TestResolveRole_ExplicitOverrideWins(t *testing.T) {
	for _, c := range [][2]string{{"x", ""}, {"", "m"}} {
		got, warns := ResolveRole(roleEntries(), "planning", c[0], c[1])
		if got != nil || len(warns) != 0 {
			t.Fatalf("override %v: want no entry/warnings, got %v %v", c, got, warns)
		}
	}
}

func TestResolveRole_UsedWhenNoOverride(t *testing.T) {
	got, warns := ResolveRole(roleEntries(), " Verification ", "", "")
	if got == nil || got.Name != "alpha" || len(warns) != 0 {
		t.Fatalf("got %v %v", got, warns)
	}
}

func TestResolveRole_AmbiguousIsDeterministicWithWarning(t *testing.T) {
	for i := 0; i < 5; i++ {
		got, warns := ResolveRole(roleEntries(), "planning", "", "")
		if got == nil || got.Name != "alpha" {
			t.Fatalf("got %v", got)
		}
		if len(warns) != 1 || !strings.Contains(warns[0], "planning") ||
			!strings.Contains(warns[0], "alpha") || !strings.Contains(warns[0], "zeta") {
			t.Fatalf("warnings %v", warns)
		}
	}
}

func TestResolveRole_EntryWithNoRolesAndUnknownRole(t *testing.T) {
	if got, warns := ResolveRole([]*LLM{{Name: "plain", Kind: KindOllama}}, "planning", "", ""); got != nil || warns != nil {
		t.Fatalf("no-roles entry matched: %v %v", got, warns)
	}
	if got, warns := ResolveRole(roleEntries(), "nonesuch", "", ""); got != nil || warns != nil {
		t.Fatalf("unknown role: %v %v", got, warns)
	}
}

func TestRoleNormalize(t *testing.T) {
	got, err := NormalizeRoles([]string{" Planning", "planning", "Verify "})
	if err != nil || !reflect.DeepEqual(got, []string{"planning", "verify"}) {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := NormalizeRoles([]string{"ok", "  "}); err == nil {
		t.Fatal("empty role accepted")
	}
	l := &LLM{Name: "a", Kind: KindOllama, Roles: []string{"X", "x"}}
	if err := l.Validate(); err != nil || !reflect.DeepEqual(l.Roles, []string{"x"}) {
		t.Fatalf("validate: %v %v", err, l.Roles)
	}
	l.Roles = []string{""}
	if l.Validate() == nil {
		t.Fatal("Validate accepted an empty role")
	}
}

func TestRoleContention_ConfigWriteConvergence(t *testing.T) {
	m := &RoleContentionMonitor{}
	before := []*LLM{
		{Name: "a", Kind: KindOllama, Roles: []string{"planning"}},
		{Name: "b", Kind: KindOllama, Roles: []string{"verification"}},
	}
	if w := m.Check(before); len(w) != 0 {
		t.Fatalf("clean registry warned: %v", w)
	}
	after := []*LLM{
		{Name: "a", Kind: KindOllama, Roles: []string{"planning", "verification"}},
		{Name: "b", Kind: KindOllama, Roles: []string{"verification"}},
	}
	w := m.Check(after)
	joined := strings.Join(w, "|")
	if !strings.Contains(joined, "converged") || !strings.Contains(joined, `"a"`) {
		t.Fatalf("warnings %v", w)
	}
	if !strings.Contains(joined, "verification") {
		t.Fatalf("missing two-claimant warning: %v", w)
	}
}

func TestRoleContention_PeriodicScanSeesChange(t *testing.T) {
	m := &RoleContentionMonitor{}
	es := []*LLM{
		{Name: "a", Kind: KindOllama, Roles: []string{"planning"}},
		{Name: "b", Kind: KindOllama, Roles: []string{"verification"}},
	}
	m.Check(es)
	if w := m.Check(es); len(w) != 0 {
		t.Fatalf("unchanged scan warned: %v", w)
	}
	es[1].Disabled = true // verification falls through... nobody claims it
	es[0].Roles = append(es[0].Roles, "verification")
	if w := m.Check(es); len(w) != 1 || !strings.Contains(w[0], "converged") {
		t.Fatalf("want convergence warning: %v", w)
	}
	es[1].Disabled = false
	w := m.Check(es)
	if len(w) == 0 || len(m.Warnings()) != len(w) {
		t.Fatalf("scan missed contention: %v / %v", w, m.Warnings())
	}
}

func TestRoleContention_WarningNotFailure(t *testing.T) {
	// nil entries, nil previous and duplicate claimants must neither panic nor
	// return anything but a warning list.
	w := CheckRoleContention([]*LLM{nil, {Name: "a", Roles: []string{"r"}}, {Name: "b", Roles: []string{"r"}}}, nil)
	if len(w) != 1 {
		t.Fatalf("want one ambiguity warning, got %v", w)
	}
	if got := CheckRoleContention(nil, nil); len(got) != 0 {
		t.Fatalf("empty: %v", got)
	}
}
