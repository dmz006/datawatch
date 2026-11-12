// Pinned-model enforcement for pi-family sessions (v9.10.19).
//
// pi and pi-prompt launch the pi.dev harness with an explicit --model
// flag naming the model the session must run on. When a session reaches
// a pi-family backend with an empty model, the harness silently uses
// that host's own default model — on a federated peer that is the
// operator-local node model the session exists to replace. Since
// v9.10.19 the pi backend refuses an empty model at launch time
// (internal/llm/backends/pi), so every datawatch launch path that can
// land on pi/pi-prompt must resolve an explicit, non-empty model first,
// or fail loudly naming the configuration keys an operator should set —
// never fall back to the backend's built-in default.
package autonomous

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoPinnedModel — the resolved backend is pi-family but no
// configuration level produced a model for the session.
var ErrNoPinnedModel = errors.New(
	"no explicit model resolved for a pi-family session — pi and pi-prompt " +
		"refuse to run on their built-in default model (it would be the " +
		"host's own default, not a model this session pinned)")

// ModelTier is one tier of a pinned-model chain, in precedence order.
type ModelTier struct {
	// Key is the configuration key (or tier name) an operator should
	// set, quoted in the error message when the chain resolves nothing.
	Key string
	// Model is the candidate for this tier; empty = unset (skipped).
	Model string
}

// RequirePinnedModel enforces the pi-family invariant for one resolved
// backend/model pair: nil when the model is non-empty, or when the
// backend is not pi-family (every other backend keeps its own fallback
// behavior unchanged). Otherwise it returns an error wrapping
// ErrNoPinnedModel that names every configuration key the caller
// listed, in precedence order, so the operator knows exactly what to
// set — no launch, no retry budget consumed, no silent default.
func RequirePinnedModel(backend, model string, keys ...string) error {
	if model != "" || !servesProviderModels(backend) {
		return nil
	}
	keyList := strings.Join(keys, ", ")
	if keyList == "" {
		keyList = "this session's model settings"
	}
	return fmt.Errorf("%w (backend %q): set one of %s", ErrNoPinnedModel, backend, keyList)
}

// ResolvePinnedModel walks tiers in precedence order and returns the
// first one that is set AND pairs with backend (validateModelForBackend:
// a pi-family backend takes "provider/model" strings but refuses bare
// claude-* aliases; claude-code takes bare aliases but refuses
// provider-qualified names). When no tier resolves it returns ("", an
// error wrapping ErrNoPinnedModel) naming every configured tier key and
// each one it skipped as unservable by the backend.
func ResolvePinnedModel(backend string, tiers ...ModelTier) (string, error) {
	keys := make([]string, 0, len(tiers))
	skipped := make([]string, 0)
	for _, tier := range tiers {
		if tier.Key != "" {
			keys = append(keys, tier.Key)
		}
		model := strings.TrimSpace(tier.Model)
		if model == "" {
			continue
		}
		if pairErr := validateModelForBackend(backend, model); pairErr != nil {
			skipped = append(skipped, fmt.Sprintf("%s %q", tier.Key, model))
			continue
		}
		return model, nil
	}
	keyList := strings.Join(keys, ", ")
	if keyList == "" {
		keyList = "this session's model settings"
	}
	detail := ""
	if len(skipped) > 0 {
		detail = "; skipped, cannot be served by " + backend + ": " + strings.Join(skipped, ", ")
	}
	return "", fmt.Errorf("%w (backend %q): set one of %s%s", ErrNoPinnedModel, backend, keyList, detail)
}
