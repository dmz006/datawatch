package server

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// verifierPolicyKeyPrefix is the config key prefix of the verifier policy
// fields in PUT /api/config.
const verifierPolicyKeyPrefix = "autonomous.verifier_policy."

// verifierPolicyFields lists the fields of autonomous.verifier_policy, in the
// order the docs name them.
var verifierPolicyFields = []string{
	"mode", "backends", "claude_session_cap", "claude_cap_window_minutes",
	"force_stronger_on_retry", "final_gate_stronger", "stronger_backend", "stronger_model",
}

// validateVerifierPolicyField checks one autonomous.verifier_policy field
// value as decoded from JSON. It rejects an unknown field, a mode outside
// config.ValidVerifierPolicyModes, a backend list with an empty entry, and a
// session cap or window that is negative or not a whole number. A nil value is
// treated as absent and passes.
func validateVerifierPolicyField(field string, v any) error {
	if v == nil {
		return nil
	}
	switch field {
	case "mode":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("verifier_policy.mode: must be a string (%s)", strings.Join(config.ValidVerifierPolicyModes, ", "))
		}
		_, err := config.NormalizeVerifierPolicyMode(str)
		return err
	case "backends":
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("verifier_policy.backends: must be an array of backend names")
		}
		for i, item := range arr {
			str, ok := item.(string)
			if !ok {
				return fmt.Errorf("verifier_policy.backends[%d]: must be a string", i)
			}
			if strings.TrimSpace(str) == "" {
				return fmt.Errorf("verifier_policy.backends[%d]: entries must not be empty", i)
			}
		}
		return nil
	case "claude_session_cap", "claude_cap_window_minutes":
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
			return fmt.Errorf("verifier_policy.%s: must be a whole number >= 0", field)
		}
		if n < 0 {
			return fmt.Errorf("verifier_policy.%s: %d must be >= 0", field, int(n))
		}
		return nil
	case "force_stronger_on_retry", "final_gate_stronger":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("verifier_policy.%s: must be true or false", field)
		}
		return nil
	case "stronger_backend", "stronger_model":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("verifier_policy.%s: must be a string", field)
		}
		return nil
	}
	return fmt.Errorf("verifier_policy: unknown field %q (valid: %s)", field, strings.Join(verifierPolicyFields, ", "))
}

// validateVerifierPolicyObject checks the verifier_policy object of a PUT
// /api/autonomous/config body.
func validateVerifierPolicyObject(raw json.RawMessage) error {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("verifier_policy: must be an object")
	}
	for field, v := range obj {
		if err := validateVerifierPolicyField(field, v); err != nil {
			return err
		}
	}
	return nil
}

// verifierPolicyPatchValue converts one PUT /api/config value into the
// decoded-JSON shape validateVerifierPolicyField expects. The CLI and comm
// channel send every value as a string, so numbers and booleans are parsed
// and a backend list is split on commas. An empty entry in a comma list is
// kept so the validator can reject it.
func verifierPolicyPatchValue(field string, v any) any {
	str, isStr := v.(string)
	if !isStr {
		return v
	}
	switch field {
	case "backends":
		if strings.TrimSpace(str) == "" {
			return []any{}
		}
		parts := strings.Split(str, ",")
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = strings.TrimSpace(p)
		}
		return out
	case "claude_session_cap", "claude_cap_window_minutes":
		if n, err := strconv.ParseFloat(strings.TrimSpace(str), 64); err == nil {
			return n
		}
	case "force_stronger_on_retry", "final_gate_stronger":
		if b, err := strconv.ParseBool(strings.TrimSpace(str)); err == nil {
			return b
		}
	}
	return v
}

// validateVerifierPolicyPatch checks every autonomous.verifier_policy.* key of
// a PUT /api/config patch before anything is applied, so one bad value
// rejects the whole PUT.
func validateVerifierPolicyPatch(patch map[string]any) error {
	for key, v := range patch {
		field, ok := strings.CutPrefix(key, verifierPolicyKeyPrefix)
		if !ok {
			continue
		}
		if err := validateVerifierPolicyField(field, verifierPolicyPatchValue(field, v)); err != nil {
			return err
		}
	}
	return nil
}

// applyVerifierPolicyPatchKey stores one validated autonomous.verifier_policy.*
// value on the daemon config. It reports whether the key was one of the policy
// fields. A value that fails validation is left unchanged.
func applyVerifierPolicyPatchKey(p *config.VerifierPolicyConfig, key string, v any) bool {
	field, ok := strings.CutPrefix(key, verifierPolicyKeyPrefix)
	if !ok {
		return false
	}
	v = verifierPolicyPatchValue(field, v)
	if validateVerifierPolicyField(field, v) != nil || v == nil {
		return true
	}
	switch field {
	case "mode":
		p.Mode, _ = config.NormalizeVerifierPolicyMode(v.(string))
		if strings.TrimSpace(v.(string)) == "" {
			p.Mode = ""
		}
	case "backends":
		arr, _ := toStringArray(v)
		p.Backends = config.NormalizeVerifierPolicyBackends(arr)
	case "claude_session_cap":
		p.ClaudeSessionCap = int(v.(float64))
	case "claude_cap_window_minutes":
		p.ClaudeCapWindowMinutes = int(v.(float64))
	case "force_stronger_on_retry":
		p.ForceStrongerOnRetry = v.(bool)
	case "final_gate_stronger":
		p.FinalGateStronger = v.(bool)
	case "stronger_backend":
		p.StrongerBackend = strings.TrimSpace(v.(string))
	case "stronger_model":
		p.StrongerModel = strings.TrimSpace(v.(string))
	}
	return true
}

// handleVerifierPolicyAction serves set_verifier_policy (Automaton level) and
// set_task_verifier_policy (task level). The body is presence-aware: an absent
// mode or backends is left unchanged, an empty mode or an empty list clears
// it. Locking follows the verification LLM overrides, which the manager
// enforces.
func (s *Server) handleVerifierPolicyAction(w http.ResponseWriter, r *http.Request, id, action string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	var req struct {
		TaskID   string    `json:"task_id"`
		Mode     *string   `json:"mode"`
		Backends *[]string `json:"backends"`
		Actor    string    `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if action == "set_task_verifier_policy" && req.TaskID == "" {
		http.Error(w, "task_id required", http.StatusBadRequest)
		return
	}
	if req.Actor == "" {
		req.Actor = "operator"
	}
	if req.Mode != nil {
		if err := validateVerifierPolicyField("mode", *req.Mode); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if req.Backends != nil {
		list := make([]any, len(*req.Backends))
		for i, b := range *req.Backends {
			list[i] = b
		}
		if err := validateVerifierPolicyField("backends", list); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if s.inferenceReg != nil {
			for _, b := range *req.Backends {
				if _, err := s.inferenceReg.Get(strings.TrimSpace(b)); err != nil {
					http.Error(w, "unknown LLM "+strconv.Quote(b)+" — check /api/llms for valid names", http.StatusBadRequest)
					return
				}
			}
		}
	}

	var updated any
	var err error
	detail := map[string]any{}
	if action == "set_task_verifier_policy" {
		updated, err = s.autonomousMgr.SetTaskVerifierPolicy(id, req.TaskID, req.Mode, req.Backends, req.Actor)
		detail["task_id"] = req.TaskID
	} else {
		updated, err = s.autonomousMgr.SetPRDVerifierPolicy(id, req.Mode, req.Backends, req.Actor)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Mode != nil {
		detail["mode"] = *req.Mode
	}
	if req.Backends != nil {
		detail["backends"] = *req.Backends
	}
	s.audit(r.Context(), action, "automaton", id, detail)
	prd, _ := updated.(*autonomous.PRD)
	if action == "set_task_verifier_policy" {
		s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
		return
	}
	s.writeAutonomousResult(w, r, updated, "", "", "")
}

// verifierPolicyForDisplay renders the global policy for GET /api/config. The
// backend list is never nil so clients see an empty array, not null.
func verifierPolicyForDisplay(p config.VerifierPolicyConfig) config.VerifierPolicyConfig {
	p.Backends = append([]string{}, p.Backends...)
	return p
}
