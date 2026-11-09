package workerprofile

// Resolution decides which worker profile a session launches with. A session
// may name a profile at several scopes; the most specific named profile wins.
// When nothing is named, the task-kind default applies, and when that is not
// known either, the role default applies.

// Layer names the place a resolved profile came from.
type Layer string

// Resolution layers, most specific first.
const (
	LayerTask      Layer = "task"      // the task's own profile name
	LayerAutomaton Layer = "automaton" // the automaton's profile name
	LayerProject   Layer = "project"   // the project's profile name
	LayerGlobal    Layer = "global"    // the global profile name
	LayerKind      Layer = "kind"      // the task-kind default
	LayerRole      Layer = "role"      // the role default
)

// unknownProfileReason is recorded when a set name is not found at launch.
const unknownProfileReason = "unknown profile at launch; role default applied"

// roleDefaults maps each worker role to the profile it starts with when no
// profile is named. The operator may change these recommended defaults.
var roleDefaults = map[string]string{
	"planner":  "full",
	"worker":   "digest",
	"verifier": "digest",
	"judge":    "minimal",
}

// kindDefaults maps each task kind to the profile it starts with when no
// profile is named at a more specific layer.
var kindDefaults = map[string]string{
	"implementation": "digest",
	"docs":           "digest",
	"note":           "minimal",
	"final_gate":     "digest",
}

// fallbackRole is used when the requested role has no default of its own.
const fallbackRole = "worker"

// Decision records how a profile was chosen at launch.
type Decision struct {
	Requested string // the set name that was asked for, or empty when none was set or a default applied
	Applied   string // the name of the profile that was applied
	Layer     string // the layer the applied profile came from
	Reason    string // a short human-readable explanation
}

// ResolveInput carries the names a launch was given. Empty names are unset.
type ResolveInput struct {
	Role          string
	Kind          string
	TaskName      string
	AutomatonName string
	ProjectName   string
	GlobalName    string
	// RoleDefaults overrides the built-in role -> profile name defaults. A role
	// found here uses this value; any other role uses the built-in table. A nil
	// map leaves every role on the built-in default.
	RoleDefaults map[string]string
	// KindDefaults overrides the built-in task kind -> profile name defaults,
	// with the same rules as RoleDefaults. A nil map leaves every kind on the
	// built-in default.
	KindDefaults map[string]string
	// Lookup returns the profile with the given name, covering built-in and
	// user-defined profiles. When nil, only built-in profiles are found.
	Lookup func(name string) (WorkerProfile, bool)
}

// Resolve chooses the worker profile for a launch.
//
// The named layers are checked in order: task, automaton, project, global.
// The first set name is the one used. If that name is not found, Resolve does
// not look at the later layers; it returns the role default and records the
// unknown name in the Decision. With no name set, the task-kind default is
// used, and then the role default.
func Resolve(in ResolveInput) (WorkerProfile, Decision) {
	named := []struct {
		layer Layer
		name  string
	}{
		{LayerTask, in.TaskName},
		{LayerAutomaton, in.AutomatonName},
		{LayerProject, in.ProjectName},
		{LayerGlobal, in.GlobalName},
	}
	for _, n := range named {
		if n.name == "" {
			continue
		}
		if p, ok := lookupProfile(in, n.name); ok {
			return p, Decision{
				Requested: n.name,
				Applied:   p.Name,
				Layer:     string(n.layer),
				Reason:    "profile named at " + string(n.layer) + " layer",
			}
		}
		return roleFallback(in, n.name, n.layer)
	}

	if name, ok := kindLookup(in, in.Kind); ok {
		if p, found := lookupProfile(in, name); found {
			return p, Decision{
				Applied: p.Name,
				Layer:   string(LayerKind),
				Reason:  "task-kind default applied",
			}
		}
	}

	return roleDefault(in)
}

// roleFallback returns the role default for an unknown set name at layer.
func roleFallback(in ResolveInput, requested string, layer Layer) (WorkerProfile, Decision) {
	p, d := roleDefault(in)
	d.Requested = requested
	d.Layer = string(layer)
	d.Reason = unknownProfileReason
	return p, d
}

// roleDefault returns the profile for the role default. An unknown role falls
// back to the worker role's default.
func roleDefault(in ResolveInput) (WorkerProfile, Decision) {
	role := in.Role
	reason := "role default applied"
	name, ok := roleLookup(in, role)
	if !ok {
		name, _ = roleLookup(in, fallbackRole)
		reason = "unknown role; worker role default applied"
	}
	p, found := lookupProfile(in, name)
	if !found {
		// Role defaults are built-in profiles, so this only happens when a
		// caller's Lookup hides a built-in; the built-in is still applied.
		p, _ = Builtin(name)
	}
	return p, Decision{
		Applied: p.Name,
		Layer:   string(LayerRole),
		Reason:  reason,
	}
}

// roleLookup returns the default profile name for role. in.RoleDefaults is
// checked first; a role it does not hold uses the built-in table.
func roleLookup(in ResolveInput, role string) (string, bool) {
	if name, ok := in.RoleDefaults[role]; ok {
		return name, true
	}
	name, ok := roleDefaults[role]
	return name, ok
}

// kindLookup returns the default profile name for kind. in.KindDefaults is
// checked first; a kind it does not hold uses the built-in table.
func kindLookup(in ResolveInput, kind string) (string, bool) {
	if name, ok := in.KindDefaults[kind]; ok {
		return name, true
	}
	name, ok := kindDefaults[kind]
	return name, ok
}

// lookupProfile finds a profile by name through in.Lookup. When Lookup is nil,
// only the built-in profiles are searched.
func lookupProfile(in ResolveInput, name string) (WorkerProfile, bool) {
	if in.Lookup == nil {
		return Builtin(name)
	}
	return in.Lookup(name)
}

// RoleDefaults returns a copy of the built-in role -> profile name defaults.
// Callers may change the returned map freely.
func RoleDefaults() map[string]string { return copyDefaults(roleDefaults) }

// KindDefaults returns a copy of the built-in task kind -> profile name
// defaults. Callers may change the returned map freely.
func KindDefaults() map[string]string { return copyDefaults(kindDefaults) }

func copyDefaults(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
