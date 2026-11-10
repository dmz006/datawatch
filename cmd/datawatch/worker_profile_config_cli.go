// Worker profile keys on `datawatch config set` and `datawatch config get`.
//
// The command-line keys are:
//
//	worker_profile                  a profile name (built-in or user-defined); empty clears it
//	worker_profiles                 the user profile table, a JSON object of name -> profile settings
//	worker_profile_defaults.role    a JSON object of role (planner, worker, verifier, judge) -> profile name
//	worker_profile_defaults.kind    a JSON object of task kind (implementation, docs, note, final_gate) -> profile name
//
// They map onto the autonomous.worker_profile, autonomous.worker_profiles and
// autonomous.worker_profile_defaults config paths. Every set is checked through
// config.ValidateWorkerProfileConfig against the local config before anything is
// sent, so a refused value never reaches the daemon. Any other key that starts
// with worker_profile is an unknown key and is refused.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

const (
	workerProfileKey     = "worker_profile"
	workerProfilesKey    = "worker_profiles"
	workerProfileRoleKey = "worker_profile_defaults.role"
	workerProfileKindKey = "worker_profile_defaults.kind"
)

// workerProfileKeyPrefix marks the keys this file owns on the config set and
// config get commands.
const workerProfileKeyPrefix = "worker_profile"

// isWorkerProfileConfigKeyFamily reports whether key is in the worker profile
// key family: an accepted key, or any other key that starts with worker_profile
// (which config set refuses as unknown).
func isWorkerProfileConfigKeyFamily(key string) bool {
	return strings.HasPrefix(key, workerProfileKeyPrefix)
}

// workerProfileGetKey maps a worker profile key to the dotted path config get
// reads from the GET /api/config body (under "autonomous"). Every other key,
// including an unknown worker_profile key, is returned unchanged; an unknown
// one then reads as not found.
func workerProfileGetKey(key string) string {
	switch key {
	case workerProfileKey, workerProfilesKey, workerProfileRoleKey, workerProfileKindKey:
		return "autonomous." + key
	}
	return key
}

// isWorkerProfileInForceKey reports whether a config get key is one of the
// worker profile keys whose read also prints the profile in force. Only the
// accepted keys qualify; an unknown worker_profile key reads as not found.
func isWorkerProfileInForceKey(key string) bool {
	switch key {
	case workerProfileKey, workerProfilesKey, workerProfileRoleKey, workerProfileKindKey:
		return true
	}
	return false
}

// workerProfileInForceLines returns the profile-in-force block printed after a
// worker profile key read: a header, then one indented line per role and task
// kind, built by workerprofile.InForce from the live GET /api/config body raw.
func workerProfileInForceLines(raw []byte) ([]string, error) {
	entries, err := workerprofile.InForceFromConfig(raw)
	if err != nil {
		return nil, err
	}
	lines := []string{"profile in force:"}
	for _, l := range workerprofile.FormatInForce(entries) {
		lines = append(lines, "  "+l)
	}
	return lines, nil
}

// workerProfileInForceReader returns the read the comm channel uses for a bare
// worker-profile key: it fetches GET /api/config from the loopback daemon and
// builds the profile in force from the live values.
func workerProfileInForceReader(cfg *config.Config) func() ([]workerprofile.InForceEntry, error) {
	return func() ([]workerprofile.InForceEntry, error) {
		req, err := http.NewRequest(http.MethodGet, loopbackBaseURL(cfg)+"/api/config", nil)
		if err != nil {
			return nil, err
		}
		if cfg.Server.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("daemon not reachable: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("config read: HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return workerprofile.InForceFromConfig(body)
	}
}

// workerProfileSetBody validates one worker profile key and value against a
// copy of cfg and returns the PUT /api/config key and value that carry it. An
// unknown key, a value that is not the right JSON shape, or a value that fails
// config.ValidateWorkerProfileConfig returns the refusal reason and no body.
func workerProfileSetBody(cfg *config.Config, key, value string) (string, any, error) {
	a := cfg.Autonomous
	var putKey string
	var putVal any
	switch key {
	case workerProfileKey:
		a.WorkerProfile = value
		putKey, putVal = "autonomous.worker_profile", value
	case workerProfilesKey:
		var m map[string]workerprofile.WorkerProfile
		if err := json.Unmarshal([]byte(value), &m); err != nil || m == nil {
			return "", nil, fmt.Errorf("%s: must be a JSON object mapping profile names to profile settings (name, mcp, tools, skills, context, model, effort)", key)
		}
		a.WorkerProfiles = m
		putKey, putVal = "autonomous.worker_profiles", m
	case workerProfileRoleKey, workerProfileKindKey:
		var m map[string]string
		if err := json.Unmarshal([]byte(value), &m); err != nil || m == nil {
			return "", nil, fmt.Errorf("%s: must be a JSON object mapping %s names to profile names", key, strings.TrimPrefix(key, "worker_profile_defaults."))
		}
		// The daemon replaces the whole defaults block, so the sibling map is
		// carried over unchanged and the whole block goes out.
		d := a.WorkerProfileDefaults
		if key == workerProfileRoleKey {
			d.Role = m
		} else {
			d.Kind = m
		}
		a.WorkerProfileDefaults = d
		putKey, putVal = "autonomous.worker_profile_defaults", d
	default:
		return "", nil, fmt.Errorf("unknown worker profile key %q (accepted keys: %s, %s, %s, %s)",
			key, workerProfileKey, workerProfilesKey, workerProfileRoleKey, workerProfileKindKey)
	}
	c := *cfg
	c.Autonomous = a
	if err := config.ValidateWorkerProfileConfig(&c); err != nil {
		return "", nil, err
	}
	return putKey, putVal, nil
}

// runWorkerProfileConfigSet validates and sends one worker profile key. It
// returns an error, which makes the command exit non-zero, when the key is
// unknown or the value is refused; nothing is sent in that case.
func runWorkerProfileConfigSet(cfg *config.Config, key, value string) error {
	putKey, putVal, err := workerProfileSetBody(cfg, key, value)
	if err != nil {
		return fmt.Errorf("config set: %w", err)
	}
	body, err := json.Marshal(map[string]any{putKey: putVal})
	if err != nil {
		return fmt.Errorf("config set: %w", err)
	}
	url := loopbackBaseURL(cfg) + "/api/config"
	if err := putConfig(url, cfg.Server.Token, string(body)); err != nil {
		return fmt.Errorf("config set: %w", err)
	}
	fmt.Printf("Set %s = %s\n", key, value)
	return nil
}
