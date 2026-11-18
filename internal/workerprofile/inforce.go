// inforce.go reports which worker profile each role, task kind and session
// kind starts with, as a read-only view of the launch rules in launch.go.
//
// InForce asks ResolveForLaunch once per role (worker, verifier, judge),
// once per task kind (implementation, docs, note, final_gate) and once per
// session kind (supervisor, operator_interactive, scheduled_command,
// planner, one_shot, review), so the output shows the same profile a real
// launch would attach. Task kinds are resolved as worker launches, which is
// the role that runs them; session kinds are resolved the way the daemon
// resolves a started session (kind only, no role). Nothing here changes
// configuration or starts a session.
package workerprofile

// Scopes an InForceEntry can report.
const (
	ScopeRole        = "role"         // the entry is for a worker role
	ScopeKind        = "kind"         // the entry is for a task kind
	ScopeSessionKind = "session_kind" // the entry is for a session kind
)

// Tiers an InForceEntry can report. Layer names from resolve.go are also
// used when a configured layer applies (role, kind, daemon is named here).
const (
	TierNone    = "none"     // no profile is applied; the launch uses the full harness
	TierBuiltIn = "built-in" // the value comes from the built-in tables
	TierDaemon  = "daemon"   // the global profile name is set and used
)

// inForceRoles lists the roles InForce reports, in output order.
var inForceRoles = []string{"worker", "verifier", "judge"}

// inForceKinds lists the task kinds InForce reports, in output order.
var inForceKinds = []string{"implementation", "docs", "note", "final_gate"}

// inForceSessionKinds lists the session kinds InForce reports, in output
// order.
var inForceSessionKinds = []string{KindSupervisor, KindOperator, KindScheduled, KindPlanner, KindOneShot, KindReview}

// InForceInput carries the configured values to report on. Empty names are
// unset. RoleDefaults and KindDefaults hold only configured values. Lookup
// finds a named profile; nil means only built-in profiles are found.
type InForceInput struct {
	RoleDefaults  map[string]string
	KindDefaults  map[string]string
	GlobalProfile string
	Lookup        func(name string) (WorkerProfile, bool)
}

// InForceEntry says which profile one role, task kind or session kind starts
// with.
type InForceEntry struct {
	Scope   string // ScopeRole, ScopeKind or ScopeSessionKind
	Key     string // the role, task kind or session kind name
	Profile string // the applied profile name, empty when none
	Tier    string // a layer name, TierBuiltIn, TierDaemon or TierNone
	Reason  string // a short human-readable explanation
}

// InForce returns one entry per role, task kind and session kind, roles
// first.
func InForce(in InForceInput) []InForceEntry {
	entries := make([]InForceEntry, 0, len(inForceRoles)+len(inForceKinds)+len(inForceSessionKinds))
	for _, role := range inForceRoles {
		li := LaunchInput{
			Role:         role,
			GlobalName:   in.GlobalProfile,
			RoleDefaults: in.RoleDefaults,
			KindDefaults: in.KindDefaults,
			Lookup:       in.Lookup,
		}
		entries = append(entries, inForceEntry(ScopeRole, role, li))
	}
	for _, kind := range inForceKinds {
		li := LaunchInput{
			Role:         "worker",
			Kind:         kind,
			GlobalName:   in.GlobalProfile,
			RoleDefaults: in.RoleDefaults,
			KindDefaults: in.KindDefaults,
			Lookup:       in.Lookup,
		}
		entries = append(entries, inForceEntry(ScopeKind, kind, li))
	}
	for _, kind := range inForceSessionKinds {
		li := LaunchInput{
			Kind:         kind,
			GlobalName:   in.GlobalProfile,
			KindDefaults: in.KindDefaults,
			Lookup:       in.Lookup,
		}
		entries = append(entries, inForceEntry(ScopeSessionKind, kind, li))
	}
	return entries
}

// inForceEntry resolves one launch and reports the result as an entry.
func inForceEntry(scope, key string, li LaunchInput) InForceEntry {
	p, applied, d := ResolveForLaunch(li)
	profile := ""
	if applied {
		profile = p.Name
	}
	return InForceEntry{
		Scope:   scope,
		Key:     key,
		Profile: profile,
		Tier:    inForceTier(li, d, applied),
		Reason:  d.Reason,
	}
}

// inForceTier maps a launch decision to the tier a reader should see.
// A global name that did not resolve is not "used", so the role default it
// fell back to is reported as a role tier.
func inForceTier(li LaunchInput, d Decision, applied bool) string {
	if !applied {
		return TierNone
	}
	layer := Layer(d.Layer)
	if layer == LayerGlobal && d.Reason == unknownProfileReason {
		layer = LayerRole
	}
	switch layer {
	case LayerGlobal:
		return TierDaemon
	case LayerRole:
		if roleConfigured(li) {
			return string(LayerRole)
		}
		return TierBuiltIn
	case LayerKind:
		if _, ok := li.KindDefaults[li.Kind]; ok {
			return string(LayerKind)
		}
		return TierBuiltIn
	}
	return string(layer)
}
