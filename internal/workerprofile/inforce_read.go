// inforce_read.go turns a GET /api/config body into the profile-in-force
// entries and the lines the CLI and the comm channel print for them. Both
// read surfaces share it so they show the same lines for the same config.
package workerprofile

import (
	"encoding/json"
	"fmt"
)

// inForceConfigBody is the slice of GET /api/config the profile-in-force read
// needs: the autonomous worker profile settings as the daemon reports them.
type inForceConfigBody struct {
	Autonomous struct {
		WorkerProfiles        map[string]WorkerProfile `json:"worker_profiles"`
		WorkerProfileDefaults struct {
			Role map[string]string `json:"role"`
			Kind map[string]string `json:"kind"`
		} `json:"worker_profile_defaults"`
		WorkerProfile string `json:"worker_profile"`
	} `json:"autonomous"`
}

// InForceFromConfig decodes a GET /api/config body and returns InForce over
// the live worker profile values in it. A body that is not JSON is an error;
// missing worker profile settings read as their zero values.
func InForceFromConfig(body []byte) ([]InForceEntry, error) {
	var cfg inForceConfigBody
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	a := cfg.Autonomous
	users := a.WorkerProfiles
	return InForce(InForceInput{
		RoleDefaults:  a.WorkerProfileDefaults.Role,
		KindDefaults:  a.WorkerProfileDefaults.Kind,
		GlobalProfile: a.WorkerProfile,
		Lookup: func(name string) (WorkerProfile, bool) {
			if p, ok := Builtin(name); ok {
				return p, true
			}
			p, ok := users[name]
			return p, ok
		},
	}), nil
}

// FormatInForce renders each entry as one line: the scope, the role or task
// kind, the profile (or 'none') and the tier, for example
// "role worker profile=full tier=built-in". The lines carry no indent or
// prefix; the caller adds those.
func FormatInForce(entries []InForceEntry) []string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		profile := e.Profile
		if profile == "" {
			profile = "none"
		}
		lines = append(lines, fmt.Sprintf("%s %s profile=%s tier=%s", e.Scope, e.Key, profile, e.Tier))
	}
	return lines
}
