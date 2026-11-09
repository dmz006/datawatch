package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

// The provider's values must reach the helper's output, and a later change to
// the provider's values must show up on the next call (no restart needed).
func TestWorkerLaunchInputCarriesProviderValues(t *testing.T) {
	m := &Manager{}
	user := workerprofile.WorkerProfile{Name: "mine"}
	settings := WorkerProfileSettings{
		GlobalName:   "mine",
		RoleDefaults: map[string]string{"worker": "mine"},
		KindDefaults: map[string]string{"docs": "mine"},
		UserProfiles: map[string]workerprofile.WorkerProfile{"mine": user},
	}
	m.SetWorkerProfileSettings(func() WorkerProfileSettings { return settings })

	in := m.workerLaunchInput()
	if in.GlobalName != "mine" {
		t.Errorf("GlobalName = %q, want mine", in.GlobalName)
	}
	if in.RoleDefaults["worker"] != "mine" || in.KindDefaults["docs"] != "mine" {
		t.Errorf("defaults not carried: role=%v kind=%v", in.RoleDefaults, in.KindDefaults)
	}
	if in.Role != "" || in.Kind != "" || in.TaskName != "" || in.AutomatonName != "" || in.ProjectName != "" {
		t.Errorf("caller-filled names must be empty: %+v", in)
	}
	if p, ok := in.Lookup("mine"); !ok || p.Name != "mine" {
		t.Errorf("Lookup(user) = %+v, %v; want user profile", p, ok)
	}
	if _, ok := in.Lookup("no-such-profile"); ok {
		t.Errorf("Lookup(unknown) found a profile")
	}

	// A settings change applies to the next call without re-wiring.
	settings = WorkerProfileSettings{GlobalName: "other"}
	if got := m.workerLaunchInput().GlobalName; got != "other" {
		t.Errorf("after change GlobalName = %q, want other", got)
	}
}

// Built-in profiles are found even when no user profiles are configured.
func TestWorkerLaunchInputFindsBuiltinWithoutProvider(t *testing.T) {
	m := &Manager{}
	in := m.workerLaunchInput()
	if in.GlobalName != "" || in.RoleDefaults != nil || in.KindDefaults != nil {
		t.Errorf("no provider should leave settings empty: %+v", in)
	}
	names := workerprofile.Names()
	if len(names) == 0 {
		t.Skip("no built-in profiles")
	}
	if _, ok := in.Lookup(names[0]); !ok {
		t.Errorf("Lookup(builtin %q) not found", names[0])
	}
}
