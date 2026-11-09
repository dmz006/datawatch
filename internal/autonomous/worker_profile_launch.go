// Worker-profile launch inputs for the autonomous manager. Each launch reads
// the live worker-profile settings through a provider wired from the daemon's
// config, so a settings change applies to the next launch without a restart.
// See internal/workerprofile for how the resolved profile is chosen.
package autonomous

import (
	"fmt"
	"log"
	"time"

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

// recordWorkerProfileDecision resolves the worker profile this launch
// starts with and records the fallback: when the profile name the launch
// requested was not found, the decision text
// "requested=<name> layer=<layer> applied=<name or none> reason=<reason>"
// lands on the task (overwritten by every attempt) and as a
// worker_profile_fallback entry in the PRD decision log. A launch whose
// requested name resolves carries no fallback and writes nothing.
func (m *Manager) recordWorkerProfileDecision(prd *PRD, t *Task) {
	in := m.workerLaunchInput()
	in.Role = "worker"
	in.Kind = t.Kind
	in.TaskName = t.WorkerProfile
	in.AutomatonName = prd.WorkerProfile
	_, _, d := workerprofile.ResolveForLaunch(in)
	m.recordWorkerProfileDecisionFrom(prd, t, d)
}

// recordWorkerProfileDecisionFrom records the fallback for an already
// resolved decision; see recordWorkerProfileDecision.
func (m *Manager) recordWorkerProfileDecisionFrom(prd *PRD, t *Task, d workerprofile.Decision) {
	// No requested name, or the requested name resolved: not a fallback.
	if d.Requested == "" || d.Requested == d.Applied {
		return
	}
	applied := d.Applied
	if applied == "" {
		applied = "none"
	}
	text := fmt.Sprintf("requested=%s layer=%s applied=%s reason=%s", d.Requested, d.Layer, applied, d.Reason)
	m.store.WithLock(func() {
		t.WorkerProfileDecision = text
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] task=%s save worker_profile_decision: %v", t.ID, err)
	}
	m.recordWorkerProfileFallbackDecision(prd.ID, t.ID, text)
}

// recordWorkerProfileFallbackDecision appends the worker_profile_fallback
// entry naming the task and the decision text to the PRD decision log and
// saves the PRD. It re-fetches the PRD from the store, because the in-flight
// snapshot may predate the task's last save and a stale one would overwrite
// it; this function mutates no task state at all — the caller already
// recorded the decision on the task itself.
func (m *Manager) recordWorkerProfileFallbackDecision(prdID, taskID, note string) {
	p, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	m.store.WithLock(func() {
		p.Decisions = append(p.Decisions, Decision{
			At:    time.Now(),
			Kind:  "worker_profile_fallback",
			Actor: "autonomous",
			Note:  fmt.Sprintf("task %s: %s", taskID, note),
		})
	})
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] prd=%s save worker_profile_fallback decision: %v", prdID, err)
	}
}

// sessionLaunchInput is workerLaunchInput filled for a verifier or judge
// session. Kind comes from the task where one exists; the task and Automaton
// names come from the task and PRD where they exist (empty otherwise).
func (m *Manager) sessionLaunchInput(role string, prd *PRD, t *Task) workerprofile.LaunchInput {
	in := m.workerLaunchInput()
	in.Role = role
	if t != nil {
		in.Kind = t.Kind
		in.TaskName = t.WorkerProfile
	}
	if prd != nil {
		in.AutomatonName = prd.WorkerProfile
	}
	return in
}

// ResolveSessionProfile resolves the profile a verifier or judge session
// starts with. role is "verifier" or "judge"; prd and t may be nil. It returns
// the profile and true only when one is applied; with nothing configured the
// launch carries no profile. The caller passes the profile on the session
// start request, the same path the worker's profile takes.
func (m *Manager) ResolveSessionProfile(role string, prd *PRD, t *Task) (workerprofile.WorkerProfile, bool) {
	p, applied, _ := workerprofile.ResolveForLaunch(m.sessionLaunchInput(role, prd, t))
	if !applied {
		return workerprofile.WorkerProfile{}, false
	}
	return p, true
}
