// Worker session profile settings on the /api/config surfaces: the
// autonomous.worker_profiles (user profiles keyed by name),
// autonomous.worker_profile_defaults (role/kind -> profile name) and
// autonomous.worker_profile (the global profile name) keys.

package server

import (
	"encoding/json"
	"fmt"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// workerProfilesForDisplay renders autonomous.worker_profiles for
// GET /api/config: an unset table reads as an empty object rather than
// null, so every surface sees one shape.
func workerProfilesForDisplay(m map[string]workerprofile.WorkerProfile) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for name, p := range m {
		out[name] = p
	}
	return out
}

// workerProfileDefaultsForDisplay renders autonomous.worker_profile_defaults
// for GET /api/config: both maps are always present, empty when unset.
func workerProfileDefaultsForDisplay(d config.WorkerProfileDefaultsConfig) map[string]interface{} {
	role := make(map[string]string, len(d.Role))
	for k, v := range d.Role {
		role[k] = v
	}
	kind := make(map[string]string, len(d.Kind))
	for k, v := range d.Kind {
		kind[k] = v
	}
	return map[string]interface{}{"role": role, "kind": kind}
}

// parseWorkerProfilesValue converts the JSON form of autonomous.worker_profiles
// (an object of profile name -> profile settings) into the typed table.
// ok is false when the value is not such an object or an entry is not a
// profile object.
func parseWorkerProfilesValue(v interface{}) (map[string]workerprofile.WorkerProfile, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var m map[string]workerprofile.WorkerProfile
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// parseWorkerProfileDefaultsValue converts the JSON form of
// autonomous.worker_profile_defaults (an object with a "role" and/or a
// "kind" map of profile names) into the typed config block. ok is false
// when the value is not such an object.
func parseWorkerProfileDefaultsValue(v interface{}) (config.WorkerProfileDefaultsConfig, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return config.WorkerProfileDefaultsConfig{}, false
	}
	var d config.WorkerProfileDefaultsConfig
	if err := json.Unmarshal(b, &d); err != nil {
		return config.WorkerProfileDefaultsConfig{}, false
	}
	return d, true
}

// validateWorkerProfilePatch simulates the worker profile patch onto a copy
// of the current config and runs the load-time validator over the result, so
// a PUT that names a built-in profile, references an unknown profile, or uses
// an unknown role/kind key rejects the whole PUT (HTTP 400 with the
// validation message) before anything is applied. A patch that touches none
// of the three keys passes without validation.
func validateWorkerProfilePatch(cfg *config.Config, patch map[string]interface{}) error {
	candidate := cfg.Autonomous
	touched := false
	if v, ok := patch["autonomous.worker_profiles"]; ok {
		m, ok := parseWorkerProfilesValue(v)
		if !ok {
			return fmt.Errorf("autonomous.worker_profiles: must be an object mapping profile names to profile settings (name, mcp, tools, skills, context, model, effort)")
		}
		candidate.WorkerProfiles = m
		touched = true
	}
	if v, ok := patch["autonomous.worker_profile_defaults"]; ok {
		d, ok := parseWorkerProfileDefaultsValue(v)
		if !ok {
			return fmt.Errorf("autonomous.worker_profile_defaults: must be an object with a \"role\" and/or a \"kind\" map of profile names")
		}
		candidate.WorkerProfileDefaults = d
		touched = true
	}
	if v, ok := patch["autonomous.worker_profile"]; ok {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("autonomous.worker_profile: must be a profile name (or the empty string to clear it)")
		}
		candidate.WorkerProfile = s
		touched = true
	}
	if !touched {
		return nil
	}
	c := *cfg
	c.Autonomous = candidate
	return config.ValidateWorkerProfileConfig(&c)
}

// validateWorkerProfileName checks a worker profile name an Automaton is set
// to: empty clears the override; anything else must be a built-in profile or
// a user-defined autonomous.worker_profiles entry.
func validateWorkerProfileName(cfg *config.Config, name string) error {
	if name == "" {
		return nil
	}
	if _, ok := workerprofile.Builtin(name); ok {
		return nil
	}
	if cfg != nil {
		if _, ok := cfg.Autonomous.WorkerProfiles[name]; ok {
			return nil
		}
	}
	return fmt.Errorf("worker profile %q is neither a built-in nor a user-defined profile", name)
}

// workerProfileInForceForDisplay renders the in_force read for GET /api/config:
// one entry per worker role and task kind, built from workerprofile.InForce
// over the live daemon values (the role and kind defaults, the global profile
// name, and the built-in plus user-defined profile lookup used at launch). Each
// entry carries the scope, key, applied profile (empty when none), tier and
// reason. The list is always present, never null.
func workerProfileInForceForDisplay(a config.AutonomousConfig) []map[string]interface{} {
	users := a.WorkerProfiles
	entries := workerprofile.InForce(workerprofile.InForceInput{
		RoleDefaults:  a.WorkerProfileDefaults.Role,
		KindDefaults:  a.WorkerProfileDefaults.Kind,
		GlobalProfile: a.WorkerProfile,
		Lookup: func(name string) (workerprofile.WorkerProfile, bool) {
			if p, ok := workerprofile.Builtin(name); ok {
				return p, true
			}
			p, ok := users[name]
			return p, ok
		},
	})
	out := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]interface{}{
			"scope":   e.Scope,
			"key":     e.Key,
			"profile": e.Profile,
			"tier":    e.Tier,
			"reason":  e.Reason,
		})
	}
	return out
}
