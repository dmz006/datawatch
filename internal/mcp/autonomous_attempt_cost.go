package mcp

// The three attempt-cost keys of the autonomous config (first_attempt,
// first_attempt_escalate_after_failures, retry_resume_max_context_tokens) on
// autonomous_config_get and autonomous_config_set. Every write is checked
// with config.ValidateAttemptCostSettings over the stored values layered with
// the candidate before anything is sent, so a refused set writes nothing and
// returns the validator's message, which names the offending key.

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/dmz006/datawatch/internal/config"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// Attempt-cost argument and config key names.
const (
	attemptCostFirstAttemptKey = "first_attempt"
	attemptCostEscalateKey     = "first_attempt_escalate_after_failures"
	attemptCostResumeKey       = "retry_resume_max_context_tokens"
)

// attemptCostKeyNames lists the attempt-cost keys in the autonomous config
// block of GET /api/config, in the order they are read back.
var attemptCostKeyNames = []string{attemptCostFirstAttemptKey, attemptCostEscalateKey, attemptCostResumeKey}

// attemptCostFirstAttemptDesc is the description of the first_attempt argument.
// It names the task kinds and the models a backend needs, so the schema alone
// tells a caller what is accepted.
const attemptCostFirstAttemptDesc = "Attempt-cost choice per task kind: an object mapping a kind (implementation, docs, note or final_gate) to {backend, model}, the backend and model its first attempt runs on. Backends pi, pi-prompt and ollama need a model; an empty entry keeps today's choice. The object replaces the whole map (an empty object clears every choice), so read it with autonomous_config_get first and include the entries you want to keep."

// attemptCostEscalateDesc names the range of first_attempt_escalate_after_failures.
const attemptCostEscalateDesc = "How many failed attempts move the next attempt to the stronger choice: a whole number from 0 to 20 (default 1, so the first failure escalates)."

// attemptCostResumeDesc names the range of retry_resume_max_context_tokens.
const attemptCostResumeDesc = "Cap on the prior context, in tokens, a retry may resume with: a whole number from 0 to 2000000 (default 150000). 0 = every retry starts a fresh session."

// attemptCostArgPresent reports whether the caller gave any attempt-cost argument.
func attemptCostArgPresent(args map[string]any) bool {
	for _, k := range attemptCostKeyNames {
		if _, ok := args[k]; ok {
			return true
		}
	}
	return false
}

// attemptCostSetPatch reads the attempt-cost arguments of autonomous_config_set,
// validates the settings they make together with the stored ones, and returns
// the /api/config patch keys. It returns an empty patch when none was given,
// and no patch with an error when a value is malformed or refused.
func (s *Server) attemptCostSetPatch(req mcpsdk.CallToolRequest) (map[string]any, error) {
	args := req.GetArguments()
	if !attemptCostArgPresent(args) {
		return map[string]any{}, nil
	}
	stored, err := s.judgePromptSettings()
	if err != nil {
		return nil, err
	}
	return attemptCostPatch(args, stored)
}

// attemptCostPatch is the pure half of attemptCostSetPatch: stored is the
// autonomous block of GET /api/config (nil means nothing is stored yet).
func attemptCostPatch(args map[string]any, stored map[string]json.RawMessage) (map[string]any, error) {
	candidate, err := storedAttemptCost(stored)
	if err != nil {
		return nil, err
	}
	patch := map[string]any{}
	if !attemptCostArgPresent(args) {
		return patch, nil
	}
	if raw, ok := args[attemptCostFirstAttemptKey]; ok {
		choices, err := decodeFirstAttemptArg(raw)
		if err != nil {
			return nil, err
		}
		candidate.FirstAttempt = choices
		patch["autonomous."+attemptCostFirstAttemptKey] = choices
	}
	if n, ok, err := attemptCostWholeNumber(args, attemptCostEscalateKey); err != nil {
		return nil, err
	} else if ok {
		candidate.FirstAttemptEscalateAfterFailures = n
		patch["autonomous."+attemptCostEscalateKey] = n
	}
	if n, ok, err := attemptCostWholeNumber(args, attemptCostResumeKey); err != nil {
		return nil, err
	} else if ok {
		candidate.RetryResumeMaxContextTokens = n
		patch["autonomous."+attemptCostResumeKey] = n
	}
	// The shared validator names the key and the range; nothing is sent when
	// it refuses.
	if err := config.ValidateAttemptCostSettings(candidate); err != nil {
		return nil, err
	}
	return patch, nil
}

// storedAttemptCost decodes the stored attempt-cost keys of the autonomous
// config block into the validator's shape. A missing key stays zero.
func storedAttemptCost(stored map[string]json.RawMessage) (config.AutonomousConfig, error) {
	cfg := config.AutonomousConfig{FirstAttempt: map[string]config.FirstAttemptChoice{}}
	if raw, ok := stored[attemptCostFirstAttemptKey]; ok && len(raw) > 0 {
		var choices map[string]config.FirstAttemptChoice
		if err := json.Unmarshal(raw, &choices); err != nil {
			return cfg, fmt.Errorf("decode stored %s: %w", attemptCostFirstAttemptKey, err)
		}
		for kind, c := range choices {
			cfg.FirstAttempt[kind] = c
		}
	}
	if raw, ok := stored[attemptCostEscalateKey]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg.FirstAttemptEscalateAfterFailures); err != nil {
			return cfg, fmt.Errorf("decode stored %s: %w", attemptCostEscalateKey, err)
		}
	}
	if raw, ok := stored[attemptCostResumeKey]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg.RetryResumeMaxContextTokens); err != nil {
			return cfg, fmt.Errorf("decode stored %s: %w", attemptCostResumeKey, err)
		}
	}
	return cfg, nil
}

// decodeFirstAttemptArg reads the first_attempt argument: an object (or null,
// which clears every choice), or a string holding that object as JSON.
func decodeFirstAttemptArg(raw any) (map[string]config.FirstAttemptChoice, error) {
	choices := map[string]config.FirstAttemptChoice{}
	if raw == nil {
		return choices, nil
	}
	var b []byte
	if str, ok := raw.(string); ok {
		b = []byte(str)
	} else {
		enc, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %v is invalid: %w", attemptCostFirstAttemptKey, raw, err)
		}
		b = enc
	}
	if err := json.Unmarshal(b, &choices); err != nil {
		return nil, fmt.Errorf("%s: %v is invalid; use an object of task kind to {backend, model} (kinds implementation, docs, note, final_gate)", attemptCostFirstAttemptKey, raw)
	}
	if choices == nil {
		choices = map[string]config.FirstAttemptChoice{}
	}
	return choices, nil
}

// attemptCostWholeNumber reads one whole-number argument. The range is left to
// the shared validator, which names the key and the bounds.
func attemptCostWholeNumber(args map[string]any, key string) (int, bool, error) {
	raw, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	f, isNum := raw.(float64)
	if !isNum || f != math.Trunc(f) || math.Abs(f) > 1e9 {
		return 0, false, fmt.Errorf("%s: %v is invalid; use a whole number", key, raw)
	}
	return int(f), true, nil
}
