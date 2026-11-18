package workerprofile

import (
	"reflect"
	"testing"
)

// userLookup returns a Lookup that finds the user-defined profiles in user
// and otherwise the built-in profiles, as the daemon's lookup does.
func userLookup(user map[string]WorkerProfile) func(string) (WorkerProfile, bool) {
	return func(name string) (WorkerProfile, bool) {
		if p, ok := user[name]; ok {
			return p, true
		}
		return Builtin(name)
	}
}

// strictLookup finds only the profiles in user, not the built-ins.
func strictLookup(user map[string]WorkerProfile) func(string) (WorkerProfile, bool) {
	return func(name string) (WorkerProfile, bool) {
		p, ok := user[name]
		return p, ok
	}
}

// customProfile returns a user-defined profile with the given name.
func customProfile(name string) WorkerProfile {
	return WorkerProfile{
		Name:    name,
		MCP:     MCPSelection{Mode: MCPNone},
		Tools:   ToolSelection{Mode: ToolsAll},
		Context: ContextFull,
		Model:   "custom-model",
	}
}

func TestRoleDefaults(t *testing.T) {
	want := map[string]string{
		"planner":  "full",
		"worker":   "digest",
		"verifier": "digest",
		"judge":    "minimal",
	}
	if !reflect.DeepEqual(roleDefaults, want) {
		t.Errorf("roleDefaults = %v, want %v", roleDefaults, want)
	}
}

func TestKindDefaults(t *testing.T) {
	want := map[string]string{
		"implementation": "digest",
		"docs":           "digest",
		"note":           "minimal",
		"final_gate":     "digest",

		"supervisor":           "datawatch-tools",
		"operator_interactive": "digest",
		"scheduled_command":    "minimal",
		"planner":              "full",
		"one_shot":             "minimal",
		"review":               "minimal",
	}
	if !reflect.DeepEqual(kindDefaults, want) {
		t.Errorf("kindDefaults = %v, want %v", kindDefaults, want)
	}
}

func TestResolveEachLayerWins(t *testing.T) {
	user := map[string]WorkerProfile{
		"task-p":      customProfile("task-p"),
		"automaton-p": customProfile("automaton-p"),
		"project-p":   customProfile("project-p"),
		"global-p":    customProfile("global-p"),
	}
	cases := []struct {
		name  string
		in    ResolveInput
		layer Layer
		want  string
	}{
		{"task", ResolveInput{TaskName: "task-p"}, LayerTask, "task-p"},
		{"automaton", ResolveInput{AutomatonName: "automaton-p"}, LayerAutomaton, "automaton-p"},
		{"project", ResolveInput{ProjectName: "project-p"}, LayerProject, "project-p"},
		{"global", ResolveInput{GlobalName: "global-p"}, LayerGlobal, "global-p"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.Lookup = userLookup(user)
			c.in.Role = "worker"
			c.in.Kind = "implementation"
			p, d := Resolve(c.in)
			if p.Name != c.want {
				t.Errorf("profile = %q, want %q", p.Name, c.want)
			}
			wantD := Decision{
				Requested: c.want,
				Applied:   c.want,
				Layer:     string(c.layer),
				Reason:    "profile named at " + string(c.layer) + " layer",
			}
			if d != wantD {
				t.Errorf("decision = %+v, want %+v", d, wantD)
			}
		})
	}
}

func TestResolveMostSpecificNameWins(t *testing.T) {
	user := map[string]WorkerProfile{
		"task-p":      customProfile("task-p"),
		"automaton-p": customProfile("automaton-p"),
		"project-p":   customProfile("project-p"),
		"global-p":    customProfile("global-p"),
	}
	all := ResolveInput{
		Role:          "worker",
		Kind:          "implementation",
		TaskName:      "task-p",
		AutomatonName: "automaton-p",
		ProjectName:   "project-p",
		GlobalName:    "global-p",
		Lookup:        userLookup(user),
	}
	cases := []struct {
		drop  func(*ResolveInput)
		layer Layer
		want  string
	}{
		{func(in *ResolveInput) { in.TaskName = "" }, LayerAutomaton, "automaton-p"},
		{func(in *ResolveInput) { in.TaskName, in.AutomatonName = "", "" }, LayerProject, "project-p"},
		{func(in *ResolveInput) { in.TaskName, in.AutomatonName, in.ProjectName = "", "", "" }, LayerGlobal, "global-p"},
	}
	for _, c := range cases {
		in := all
		c.drop(&in)
		p, d := Resolve(in)
		if p.Name != c.want || d.Layer != string(c.layer) {
			t.Errorf("got profile %q layer %q, want %q layer %q", p.Name, d.Layer, c.want, c.layer)
		}
	}
}

func TestResolveFallsBackToKindDefaultWhenUnset(t *testing.T) {
	// The role default is planner (full); the kind default is note (minimal),
	// so a kind-default result proves the kind layer was used.
	cases := []struct {
		kind string
		want string
	}{
		{"implementation", "digest"},
		{"docs", "digest"},
		{"note", "minimal"},
		{"final_gate", "digest"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			p, d := Resolve(ResolveInput{Role: "planner", Kind: c.kind})
			if p.Name != c.want {
				t.Errorf("profile = %q, want %q", p.Name, c.want)
			}
			wantD := Decision{Applied: c.want, Layer: string(LayerKind), Reason: "task-kind default applied"}
			if d != wantD {
				t.Errorf("decision = %+v, want %+v", d, wantD)
			}
		})
	}
}

func TestResolveKindDefaultBeatsRoleDefault(t *testing.T) {
	// Role verifier defaults to digest; kind note defaults to minimal.
	p, d := Resolve(ResolveInput{Role: "verifier", Kind: "note"})
	if p.Name != "minimal" || d.Layer != string(LayerKind) {
		t.Errorf("got %q layer %q, want minimal from kind", p.Name, d.Layer)
	}
}

func TestResolveFallsBackToRoleDefault(t *testing.T) {
	cases := []struct {
		role string
		kind string
		want string
	}{
		{"planner", "", "full"},
		{"worker", "", "digest"},
		{"verifier", "", "digest"},
		{"judge", "", "minimal"},
		// Unknown kind has no default, so the role default applies.
		{"planner", "unknown-kind", "full"},
	}
	for _, c := range cases {
		t.Run(c.role+"/"+c.kind, func(t *testing.T) {
			p, d := Resolve(ResolveInput{Role: c.role, Kind: c.kind})
			if p.Name != c.want {
				t.Errorf("profile = %q, want %q", p.Name, c.want)
			}
			wantD := Decision{Applied: c.want, Layer: string(LayerRole), Reason: "role default applied"}
			if d != wantD {
				t.Errorf("decision = %+v, want %+v", d, wantD)
			}
		})
	}
}

func TestResolveUnknownRoleUsesWorkerDefault(t *testing.T) {
	p, d := Resolve(ResolveInput{Role: "no-such-role"})
	if p.Name != "digest" {
		t.Errorf("profile = %q, want digest", p.Name)
	}
	if d.Layer != string(LayerRole) || d.Applied != "digest" {
		t.Errorf("decision = %+v", d)
	}
}

func TestResolveUnknownNameAtEachLayerReturnsRoleDefault(t *testing.T) {
	// For each layer, that layer is set to a name that does not exist, and
	// the more general layers are set to a known profile. Resolve must stop at
	// the unknown name and return the role default, not the known profile.
	known := "known"
	lookup := strictLookup(map[string]WorkerProfile{known: customProfile(known)})
	cases := []struct {
		layer Layer
		in    ResolveInput
	}{
		{LayerTask, ResolveInput{TaskName: "ghost", AutomatonName: known, ProjectName: known, GlobalName: known}},
		{LayerAutomaton, ResolveInput{AutomatonName: "ghost", ProjectName: known, GlobalName: known}},
		{LayerProject, ResolveInput{ProjectName: "ghost", GlobalName: known}},
		{LayerGlobal, ResolveInput{GlobalName: "ghost"}},
	}
	for _, c := range cases {
		t.Run(string(c.layer), func(t *testing.T) {
			in := c.in
			in.Role = "planner"
			in.Kind = "note"
			in.Lookup = lookup
			p, d := Resolve(in)
			if p.Name != "full" {
				t.Errorf("profile = %q, want role default full", p.Name)
			}
			wantD := Decision{
				Requested: "ghost",
				Applied:   "full",
				Layer:     string(c.layer),
				Reason:    "unknown profile at launch; role default applied",
			}
			if d != wantD {
				t.Errorf("decision = %+v, want %+v", d, wantD)
			}
		})
	}
}

func TestResolveUnknownNameDoesNotWalkChainWhenStrict(t *testing.T) {
	// With a strict lookup that does not know the built-in "digest", naming
	// it at the task layer is an unknown name even though it is built in.
	in := ResolveInput{
		Role:     "worker",
		Kind:     "note",
		TaskName: "digest",
		Lookup:   strictLookup(map[string]WorkerProfile{}),
	}
	p, d := Resolve(in)
	if p.Name != "digest" || d.Requested != "digest" || d.Layer != string(LayerTask) {
		t.Errorf("got profile %q decision %+v", p.Name, d)
	}
	if d.Reason != "unknown profile at launch; role default applied" {
		t.Errorf("reason = %q", d.Reason)
	}
}

func TestResolveNilLookupUsesBuiltins(t *testing.T) {
	p, d := Resolve(ResolveInput{Role: "judge", ProjectName: "full"})
	if p.Name != "full" || d.Layer != string(LayerProject) || d.Requested != "full" {
		t.Errorf("got %q decision %+v", p.Name, d)
	}

	p, d = Resolve(ResolveInput{Role: "judge", GlobalName: "not-a-profile"})
	if p.Name != "minimal" || d.Requested != "not-a-profile" || d.Layer != string(LayerGlobal) {
		t.Errorf("got %q decision %+v", p.Name, d)
	}
}

func TestResolveEmptyInputUsesRoleDefault(t *testing.T) {
	p, d := Resolve(ResolveInput{})
	if p.Name != "digest" {
		t.Errorf("profile = %q, want digest", p.Name)
	}
	if d.Requested != "" || d.Layer != string(LayerRole) {
		t.Errorf("decision = %+v", d)
	}
}

func TestResolveReturnsIndependentProfile(t *testing.T) {
	p, _ := Resolve(ResolveInput{Role: "worker"})
	p.Name = "changed"
	again, _ := Resolve(ResolveInput{Role: "worker"})
	if again.Name != "digest" {
		t.Errorf("built-in profile changed through a resolved copy: %q", again.Name)
	}
}

func TestRoleOverrideChangesOnlyThatRole(t *testing.T) {
	// Override worker only. Every other role keeps its built-in default, and
	// the kind layer still wins over the role layer when a kind applies.
	overrides := map[string]string{"worker": "full"}
	cases := []struct {
		role  string
		kind  string
		want  string
		layer Layer
	}{
		{"worker", "", "full", LayerRole},
		{"planner", "", "full", LayerRole},
		{"verifier", "", "digest", LayerRole},
		{"judge", "", "minimal", LayerRole},
		{"worker", "note", "minimal", LayerKind},
	}
	for _, c := range cases {
		t.Run(c.role+"/"+c.kind, func(t *testing.T) {
			p, d := Resolve(ResolveInput{Role: c.role, Kind: c.kind, RoleDefaults: overrides})
			if p.Name != c.want || d.Layer != string(c.layer) {
				t.Errorf("got profile %q layer %q, want %q layer %q", p.Name, d.Layer, c.want, c.layer)
			}
		})
	}
}

func TestRoleOverrideAddsUnknownRole(t *testing.T) {
	// A role absent from the built-in table takes the override, not the worker fallback.
	p, d := Resolve(ResolveInput{Role: "reviewer", RoleDefaults: map[string]string{"reviewer": "minimal"}})
	if p.Name != "minimal" || d.Layer != string(LayerRole) || d.Reason != "role default applied" {
		t.Errorf("got %q decision %+v", p.Name, d)
	}
}

func TestKindOverrideChangesOnlyThatKind(t *testing.T) {
	// Override docs only. Built-in kinds keep their defaults, and a new kind
	// can be added through the override.
	overrides := map[string]string{"docs": "minimal", "research": "full"}
	cases := []struct {
		kind string
		want string
	}{
		{"docs", "minimal"},
		{"research", "full"},
		{"implementation", "digest"},
		{"note", "minimal"},
		{"final_gate", "digest"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			p, d := Resolve(ResolveInput{Role: "planner", Kind: c.kind, KindDefaults: overrides})
			if p.Name != c.want || d.Layer != string(LayerKind) {
				t.Errorf("got profile %q layer %q, want %q layer kind", p.Name, d.Layer, c.want)
			}
		})
	}
}

func TestNilOverridesMatchBuiltinResult(t *testing.T) {
	// With nil override maps, Resolve must match the built-in tables for every
	// role and kind, including unknown ones and the empty kind.
	roles := []string{"planner", "worker", "verifier", "judge", "no-such-role"}
	kinds := []string{"implementation", "docs", "note", "final_gate", "unknown-kind", ""}
	for _, role := range roles {
		for _, kind := range kinds {
			in := ResolveInput{Role: role, Kind: kind}
			p, d := Resolve(in)

			// Expected: the kind default when the kind is built in, else the role default.
			wantName, wantLayer := roleDefaults[role], LayerRole
			if !isKnownRole(role) {
				wantName = roleDefaults[fallbackRole]
			}
			if n, ok := kindDefaults[kind]; ok {
				wantName, wantLayer = n, LayerKind
			}
			if p.Name != wantName || d.Layer != string(wantLayer) {
				t.Errorf("role %q kind %q: got %q layer %q, want %q layer %q",
					role, kind, p.Name, d.Layer, wantName, wantLayer)
			}

			// Empty non-nil maps must behave the same as nil ones.
			empty := in
			empty.RoleDefaults = map[string]string{}
			empty.KindDefaults = map[string]string{}
			p2, d2 := Resolve(empty)
			if !reflect.DeepEqual(p2, p) || d2 != d {
				t.Errorf("role %q kind %q: empty maps changed result", role, kind)
			}
		}
	}
}

func isKnownRole(role string) bool {
	_, ok := roleDefaults[role]
	return ok
}

func TestResolveSessionKindDefaults(t *testing.T) {
	cases := map[string]string{
		KindSupervisor: "datawatch-tools",
		KindOperator:   "digest",
		KindScheduled:  "minimal",
		KindPlanner:    "full",
		KindOneShot:    "minimal",
		KindReview:     "minimal",
	}
	for kind, want := range cases {
		p, d := Resolve(ResolveInput{Role: "worker", Kind: kind})
		if p.Name != want || d.Layer != string(LayerKind) {
			t.Errorf("kind %q: got %q at %q, want %q at kind", kind, p.Name, d.Layer, want)
		}
	}
}

func TestResolveSupervisorDefaultIsLean(t *testing.T) {
	p, _ := Resolve(ResolveInput{Role: "worker", Kind: KindSupervisor})
	if p.Skills || p.Context == ContextFull {
		t.Errorf("supervisor default is not lean: %+v", p)
	}
	if p.Name != "datawatch-tools" {
		t.Errorf("supervisor default = %q", p.Name)
	}
}

func TestResolveActivitySelectsKindDefault(t *testing.T) {
	for act, kind := range ActivityKinds() {
		want, _ := Resolve(ResolveInput{Role: "worker", Kind: kind})
		got, d := Resolve(ResolveInput{Role: "worker", Activity: act})
		if got.Name != want.Name || d.Layer != string(LayerKind) {
			t.Errorf("activity %q: got %q at %q, want %q", act, got.Name, d.Layer, want.Name)
		}
	}
	// A kind with its own default wins over the activity.
	p, _ := Resolve(ResolveInput{Role: "worker", Kind: KindScheduled, Activity: ActivitySupervising})
	if p.Name != "minimal" {
		t.Errorf("kind should beat activity, got %q", p.Name)
	}
}

func TestResolveSessionKindNamedScopes(t *testing.T) {
	lookup := userLookup(map[string]WorkerProfile{
		"a": customProfile("a"), "b": customProfile("b"), "c": customProfile("c"), "d": customProfile("d"),
	})
	base := ResolveInput{Role: "worker", Kind: KindSupervisor, Lookup: lookup}
	in := base
	in.GlobalName = "d"
	if p, _ := Resolve(in); p.Name != "d" {
		t.Errorf("global: %q", p.Name)
	}
	in.ProjectName = "c"
	if p, _ := Resolve(in); p.Name != "c" {
		t.Errorf("project: %q", p.Name)
	}
	in.AutomatonName = "b"
	if p, _ := Resolve(in); p.Name != "b" {
		t.Errorf("automaton: %q", p.Name)
	}
	in.TaskName = "a"
	if p, d := Resolve(in); p.Name != "a" || d.Layer != string(LayerTask) {
		t.Errorf("task: %q %q", p.Name, d.Layer)
	}
}

func TestResolveUnknownKindFallsToRoleDefault(t *testing.T) {
	for role, want := range roleDefaults {
		p, d := Resolve(ResolveInput{Role: role, Kind: "no_such_kind", Activity: "no_such_activity"})
		if p.Name != want || d.Layer != string(LayerRole) {
			t.Errorf("role %q: got %q at %q, want %q at role", role, p.Name, d.Layer, want)
		}
	}
}
