// Worker-profile launch inputs for the autonomous manager. Each launch reads
// the live worker-profile settings through a provider wired from the daemon's
// config, so a settings change applies to the next launch without a restart.
// See internal/workerprofile for how the resolved profile is chosen.
package autonomous

import (
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// WorkerProfileSettings is one snapshot of the live worker-profile config.
type WorkerProfileSettings struct {
	// GlobalName is autonomous.worker_profile, the daemon-wide profile name.
	GlobalName string
	// RoleDefaults maps a worker role to a profile name (worker_profile_defaults.role).
	RoleDefaults map[string]string
	// KindDefaults maps a task kind to a profile name (worker_profile_defaults.kind).
	KindDefaults map[string]string
	// UserProfiles are the user-defined profiles (autonomous.worker_profiles).
	UserProfiles map[string]workerprofile.WorkerProfile
}

// SetWorkerProfileSettings wires the live reader for the worker-profile
// settings. The reader is called at each launch, so it should return the
// current config values each time.
func (m *Manager) SetWorkerProfileSettings(fn func() WorkerProfileSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workerProfileFn = fn
}

// workerLaunchInput returns a workerprofile.LaunchInput filled from the live
// settings provider. Role, Kind, TaskName, AutomatonName and ProjectName are
// left empty for the caller to fill. Lookup checks the built-in profiles first
// and then the user profiles. Built-in names cannot be shadowed, so the order
// does not change the result for valid config. With no provider, the input has
// no configured defaults and finds only the built-in profiles.
func (m *Manager) workerLaunchInput() workerprofile.LaunchInput {
	m.mu.Lock()
	fn := m.workerProfileFn
	m.mu.Unlock()

	var s WorkerProfileSettings
	if fn != nil {
		s = fn()
	}
	users := s.UserProfiles
	return workerprofile.LaunchInput{
		GlobalName:   s.GlobalName,
		RoleDefaults: s.RoleDefaults,
		KindDefaults: s.KindDefaults,
		Lookup: func(name string) (workerprofile.WorkerProfile, bool) {
			if p, ok := workerprofile.Builtin(name); ok {
				return p, true
			}
			p, ok := users[name]
			return p, ok
		},
	}
}
