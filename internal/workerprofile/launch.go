// launch.go decides which worker profile a launch starts with, and whether
// a profile applies at all.
//
// ResolveForLaunch wraps Resolve with two launch rules. First, a configured
// role default beats the built-in task-kind default: when the kind has no
// configured entry but the role does, the kind is dropped before resolving.
// Second, the returned bool says whether a profile is applied. It is false
// when the result comes only from the built-in tables (the role or kind has
// no configured entry). A false result means the launch carries no profile
// and starts with the full harness.
package workerprofile

// noProfileReason is the Decision.Reason recorded when no profile applies.
const noProfileReason = "no profile configured; full harness"

// LaunchInput is what a worker launch asks for. Empty names are unset.
// RoleDefaults and KindDefaults hold only the configured values; a nil map
// means nothing is configured. Lookup finds a named profile; nil means only
// built-in profiles are found.
type LaunchInput struct {
	Role          string
	Kind          string
	TaskName      string
	AutomatonName string
	ProjectName   string
	GlobalName    string
	RoleDefaults  map[string]string
	KindDefaults  map[string]string
	Lookup        func(name string) (WorkerProfile, bool)
}

// ResolveForLaunch chooses the worker profile for a launch and reports
// whether a profile applies. The bool is true only when a profile is applied:
// a named tier that resolves, or a configured kind or role default. When it
// is false, the returned profile is the built-in fallback and must not be
// attached to the launch; the Decision explains why.
func ResolveForLaunch(in LaunchInput) (WorkerProfile, bool, Decision) {
	kind := in.Kind
	if _, kindConfigured := in.KindDefaults[kind]; !kindConfigured {
		if _, roleConfigured := in.RoleDefaults[in.Role]; roleConfigured {
			// A configured role default must beat the built-in kind default.
			kind = ""
		}
	}

	p, d := Resolve(ResolveInput{
		Role:          in.Role,
		Kind:          kind,
		TaskName:      in.TaskName,
		AutomatonName: in.AutomatonName,
		ProjectName:   in.ProjectName,
		GlobalName:    in.GlobalName,
		RoleDefaults:  in.RoleDefaults,
		KindDefaults:  in.KindDefaults,
		Lookup:        in.Lookup,
	})

	applied := profileApplied(in, kind, d)
	if !applied && (Layer(d.Layer) == LayerRole || Layer(d.Layer) == LayerKind) {
		d.Reason = noProfileReason
	}
	return p, applied, d
}

// profileApplied reports whether the resolved decision carries a profile
// that the launch should attach. kind is the kind after the role rule.
func profileApplied(in LaunchInput, kind string, d Decision) bool {
	switch Layer(d.Layer) {
	case LayerTask, LayerAutomaton, LayerProject, LayerGlobal:
		// A named tier that resolves always applies. An unknown name keeps
		// the role-default fallback, and the bool then follows the role rule.
		if d.Reason != unknownProfileReason {
			return true
		}
		return roleConfigured(in)
	case LayerRole:
		return roleConfigured(in) && d.Applied != ""
	case LayerKind:
		_, configured := in.KindDefaults[kind]
		return configured && d.Applied != ""
	}
	return false
}

// roleConfigured reports whether the role default that Resolve uses has a
// configured entry. An unknown role uses the worker role's default, so that
// entry is the one checked.
func roleConfigured(in LaunchInput) bool {
	role := in.Role
	if _, builtin := roleDefaults[role]; !builtin {
		if _, configured := in.RoleDefaults[role]; !configured {
			role = fallbackRole
		}
	}
	_, ok := in.RoleDefaults[role]
	return ok
}
