package workerprofile

import "testing"

// launchRoles are the roles the launch rules are checked against.
var launchRoles = []string{"worker", "verifier", "judge"}

func TestResolveForLaunchNothingConfiguredIsNoProfile(t *testing.T) {
	for _, role := range launchRoles {
		t.Run(role, func(t *testing.T) {
			_, applied, d := ResolveForLaunch(LaunchInput{Role: role})
			if applied {
				t.Fatalf("role %s with nothing configured: applied = true, want false (decision %+v)", role, d)
			}
			if d.Layer != string(LayerRole) {
				t.Errorf("layer = %q, want %q", d.Layer, LayerRole)
			}
			if d.Reason != noProfileReason {
				t.Errorf("reason = %q, want %q", d.Reason, noProfileReason)
			}
		})
	}
}

func TestResolveForLaunchBuiltinKindIsNoProfile(t *testing.T) {
	// The built-in kind default comes from the built-in table only, so the
	// launch carries no profile.
	_, applied, d := ResolveForLaunch(LaunchInput{Role: "worker", Kind: "implementation"})
	if applied {
		t.Fatalf("built-in kind with nothing configured: applied = true, want false")
	}
	if d.Layer != string(LayerKind) {
		t.Errorf("layer = %q, want %q", d.Layer, LayerKind)
	}
	if d.Reason != noProfileReason {
		t.Errorf("reason = %q, want %q", d.Reason, noProfileReason)
	}
}

func TestResolveForLaunchConfiguredRoleApplies(t *testing.T) {
	for _, role := range launchRoles {
		t.Run(role, func(t *testing.T) {
			in := LaunchInput{
				Role:         role,
				RoleDefaults: map[string]string{role: "full"},
			}
			p, applied, d := ResolveForLaunch(in)
			if !applied {
				t.Fatalf("configured role %s: applied = false, want true (decision %+v)", role, d)
			}
			if p.Name != "full" {
				t.Errorf("profile = %q, want full", p.Name)
			}
			if d.Layer != string(LayerRole) || d.Applied != "full" {
				t.Errorf("decision = %+v, want layer role applied full", d)
			}
		})
	}
}

func TestResolveForLaunchConfiguredRoleBeatsBuiltinKind(t *testing.T) {
	// Kind "implementation" is only a built-in default (digest). The configured
	// role default must win, so the kind is cleared and the role profile applies.
	in := LaunchInput{
		Role:         "worker",
		Kind:         "implementation",
		RoleDefaults: map[string]string{"worker": "minimal"},
	}
	p, applied, d := ResolveForLaunch(in)
	if !applied {
		t.Fatalf("applied = false, want true (decision %+v)", d)
	}
	if p.Name != "minimal" || d.Layer != string(LayerRole) {
		t.Errorf("got profile %q layer %q, want minimal at role", p.Name, d.Layer)
	}
}

func TestResolveForLaunchConfiguredKindApplies(t *testing.T) {
	in := LaunchInput{
		Role:         "worker",
		Kind:         "docs",
		KindDefaults: map[string]string{"docs": "full"},
	}
	p, applied, d := ResolveForLaunch(in)
	if !applied {
		t.Fatalf("configured kind: applied = false, want true (decision %+v)", d)
	}
	if p.Name != "full" || d.Layer != string(LayerKind) {
		t.Errorf("got profile %q layer %q, want full at kind", p.Name, d.Layer)
	}
	if d.Reason != "task-kind default applied" {
		t.Errorf("reason = %q, want task-kind default applied", d.Reason)
	}
}

func TestResolveForLaunchConfiguredKindBeatsConfiguredRole(t *testing.T) {
	in := LaunchInput{
		Role:         "worker",
		Kind:         "docs",
		RoleDefaults: map[string]string{"worker": "minimal"},
		KindDefaults: map[string]string{"docs": "full"},
	}
	p, applied, d := ResolveForLaunch(in)
	if !applied || p.Name != "full" || d.Layer != string(LayerKind) {
		t.Fatalf("got profile %q applied %v layer %q, want full applied at kind", p.Name, applied, d.Layer)
	}
}

func TestResolveForLaunchNamedTaskBeatsConfiguredKind(t *testing.T) {
	in := LaunchInput{
		Role:         "worker",
		Kind:         "docs",
		TaskName:     "minimal",
		KindDefaults: map[string]string{"docs": "full"},
	}
	p, applied, d := ResolveForLaunch(in)
	if !applied {
		t.Fatalf("named task profile: applied = false, want true")
	}
	if p.Name != "minimal" || d.Layer != string(LayerTask) || d.Requested != "minimal" {
		t.Errorf("got profile %q decision %+v, want minimal at task requested minimal", p.Name, d)
	}
}

func TestResolveForLaunchNamedTierAlwaysApplies(t *testing.T) {
	// A named tier that resolves applies even with nothing configured.
	for _, in := range []LaunchInput{
		{Role: "judge", TaskName: "full"},
		{Role: "judge", AutomatonName: "full"},
		{Role: "judge", ProjectName: "full"},
		{Role: "judge", GlobalName: "full"},
	} {
		_, applied, d := ResolveForLaunch(in)
		if !applied {
			t.Errorf("named %+v: applied = false, want true (decision %+v)", in, d)
		}
	}
}

func TestResolveForLaunchNilLookupBuiltinName(t *testing.T) {
	p, applied, d := ResolveForLaunch(LaunchInput{Role: "judge", TaskName: "digest", Lookup: nil})
	if !applied || p.Name != "digest" || d.Layer != string(LayerTask) {
		t.Fatalf("nil lookup builtin: got %q applied %v decision %+v", p.Name, applied, d)
	}
}

func TestResolveForLaunchUnknownTaskFallsBackToRole(t *testing.T) {
	// Unknown name, role not configured: role fallback, bool false, and the
	// Decision keeps Requested, Layer and Reason.
	_, applied, d := ResolveForLaunch(LaunchInput{Role: "verifier", TaskName: "nope"})
	if applied {
		t.Fatalf("unknown task, unconfigured role: applied = true, want false")
	}
	if d.Requested != "nope" || d.Layer != string(LayerTask) || d.Reason != unknownProfileReason {
		t.Errorf("decision = %+v, want requested nope, layer task, unknown reason", d)
	}
	if d.Applied != "digest" {
		t.Errorf("applied profile = %q, want the verifier role fallback digest", d.Applied)
	}
}

func TestResolveForLaunchUnknownTaskWithConfiguredRoleApplies(t *testing.T) {
	p, applied, d := ResolveForLaunch(LaunchInput{
		Role:         "verifier",
		TaskName:     "nope",
		RoleDefaults: map[string]string{"verifier": "full"},
	})
	if !applied || p.Name != "full" {
		t.Fatalf("unknown task, configured role: got %q applied %v decision %+v, want full applied", p.Name, applied, d)
	}
	if d.Requested != "nope" || d.Reason != unknownProfileReason {
		t.Errorf("decision = %+v, want requested nope and unknown reason kept", d)
	}
}

func TestResolveForLaunchUnknownRoleNotConfigured(t *testing.T) {
	_, applied, d := ResolveForLaunch(LaunchInput{Role: "mystery"})
	if applied {
		t.Fatalf("unknown role with nothing configured: applied = true, want false")
	}
	if d.Reason != noProfileReason {
		t.Errorf("reason = %q, want %q", d.Reason, noProfileReason)
	}
}

func TestResolveForLaunchConfiguredProfileMissingIsNotApplied(t *testing.T) {
	// The configured role names a profile that does not exist, so nothing is
	// applied and the launch carries no profile.
	_, applied, d := ResolveForLaunch(LaunchInput{
		Role:         "worker",
		RoleDefaults: map[string]string{"worker": "no-such-profile"},
	})
	if applied {
		t.Fatalf("missing configured profile: applied = true, want false (decision %+v)", d)
	}
}
