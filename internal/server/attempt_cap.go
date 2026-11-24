// Attempt cap surfaces: the autonomous.attempt_cap block on GET /api/config and
// PUT /api/config.
//
// The block holds the daemon-owned total attempt cap and its back-off. PUT
// /api/config checks every key before anything is applied, with the rules of
// config.AttemptCapConfig.Validate plus an upper bound on each value, so a bad
// value refuses the whole PUT. backoff_minutes takes a JSON array or a comma
// list, and an explicit empty list is kept as empty (no wait).
package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// attemptCapKeyPrefix is the config path every autonomous.attempt_cap.* key
// shares.
const attemptCapKeyPrefix = "autonomous.attempt_cap."

// PUT /api/config keys of the attempt cap block.
const (
	keyAttemptCapEnabled        = attemptCapKeyPrefix + "enabled"
	keyAttemptCapMaxTotal       = attemptCapKeyPrefix + "max_total"
	keyAttemptCapFreeAttempts   = attemptCapKeyPrefix + "free_attempts"
	keyAttemptCapBackoffMinutes = attemptCapKeyPrefix + "backoff_minutes"
)

// Upper bounds on what PUT /api/config accepts: a delay is multiplied into a
// duration, so an unbounded value could overflow.
const (
	attemptCapMaxTotalLimit    = 1000
	attemptCapMaxDelayItems    = 100
	attemptCapMaxDelayMinutes  = 7 * 24 * 60
	attemptCapFreeAttemptLimit = 1000
)

// attemptCapSettings is the autonomous.attempt_cap block GET /api/config
// serves: the four values in effect, so an unset key shows its default.
type attemptCapSettings struct {
	Enabled        bool  `json:"enabled"`
	MaxTotal       int   `json:"max_total"`
	FreeAttempts   int   `json:"free_attempts"`
	BackoffMinutes []int `json:"backoff_minutes"`
}

// attemptCapConfigSurface returns the values in effect for c.
func attemptCapConfigSurface(c config.AttemptCapConfig) attemptCapSettings {
	return attemptCapSettings{
		Enabled:        c.EffectiveEnabled(),
		MaxTotal:       c.EffectiveMaxTotal(),
		FreeAttempts:   c.EffectiveFreeAttempts(),
		BackoffMinutes: c.EffectiveBackoffMinutes(),
	}
}

// attemptCapKeys lists the keys of the block in a fixed order, so the first
// bad value reported does not depend on map order.
var attemptCapKeys = []string{
	keyAttemptCapEnabled, keyAttemptCapMaxTotal, keyAttemptCapFreeAttempts, keyAttemptCapBackoffMinutes,
}

// validateAttemptCapPatch checks the attempt cap keys of a PUT /api/config body
// before anything is applied. A key that is absent is not checked. The error
// names the key and the valid values.
func validateAttemptCapPatch(patch map[string]any) error {
	var scratch config.AttemptCapConfig
	touched := false
	for _, key := range attemptCapKeys {
		v, ok := patch[key]
		if !ok {
			continue
		}
		touched = true
		if _, err := applyAttemptCapKey(&scratch, key, v); err != nil {
			return err
		}
	}
	if !touched {
		return nil
	}
	return scratch.Validate()
}

// applyAttemptCapPatch writes one attempt cap key into the daemon config. It
// reports whether key belongs to the block. The value was checked by
// validateAttemptCapPatch before the PUT reached this point.
func applyAttemptCapPatch(cfg *config.Config, key string, v any) bool {
	known, _ := applyAttemptCapKey(&cfg.Autonomous.AttemptCap, key, v)
	return known
}

// applyAttemptCapKey parses one key's value and stores it in c. known reports
// whether key is a key of the block; when err is not nil, c is unchanged.
func applyAttemptCapKey(c *config.AttemptCapConfig, key string, v any) (known bool, err error) {
	switch key {
	case keyAttemptCapEnabled:
		b, ok := failureEscalationBool(v)
		if !ok {
			return true, fmt.Errorf("%s: %v is invalid; valid values: true, false", key, v)
		}
		c.Enabled = &b
	case keyAttemptCapMaxTotal:
		n, ok := infraRetryWhole(v)
		if !ok || n < 1 || n > attemptCapMaxTotalLimit {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number from 1 to %d (the default is %d)",
				key, v, attemptCapMaxTotalLimit, config.AttemptCapDefaultMaxTotal)
		}
		c.MaxTotal = n
	case keyAttemptCapFreeAttempts:
		n, ok := infraRetryWhole(v)
		if !ok || n < 1 || n > attemptCapFreeAttemptLimit {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number from 1 to %d (the default is %d)",
				key, v, attemptCapFreeAttemptLimit, config.AttemptCapDefaultFreeAttempts)
		}
		c.FreeAttempts = n
	case keyAttemptCapBackoffMinutes:
		list, err := parseAttemptCapBackoff(v)
		if err != nil {
			return true, err
		}
		c.BackoffMinutes = list
	default:
		return false, nil
	}
	return true, nil
}

// parseAttemptCapBackoff reads the backoff_minutes value: a JSON array of whole
// numbers or a comma list such as 5,15,45. An empty array or an empty string is
// an explicit "no wait" and returns an empty list that is not nil; null clears
// the key back to unset (nil), which reads the default list.
func parseAttemptCapBackoff(v any) ([]int, error) {
	var items []any
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(x) != "" {
			for _, part := range strings.Split(x, ",") {
				items = append(items, strings.TrimSpace(part))
			}
		}
	case []any:
		items = x
	default:
		return nil, fmt.Errorf("%s: must be a JSON array of whole numbers of minutes or a comma list such as 5,15,45",
			keyAttemptCapBackoffMinutes)
	}
	if len(items) > attemptCapMaxDelayItems {
		return nil, fmt.Errorf("%s: %d entries is too many; use at most %d",
			keyAttemptCapBackoffMinutes, len(items), attemptCapMaxDelayItems)
	}
	out := make([]int, 0, len(items))
	for i, it := range items {
		n, ok := infraRetryWhole(it)
		if !ok || n < 0 {
			return nil, fmt.Errorf("%s[%d]: %v is invalid; use a whole number of minutes, 0 or more", keyAttemptCapBackoffMinutes, i, it)
		}
		if n > attemptCapMaxDelayMinutes {
			return nil, fmt.Errorf("%s[%d]: %d is invalid; use at most %d minutes",
				keyAttemptCapBackoffMinutes, i, n, attemptCapMaxDelayMinutes)
		}
		out = append(out, n)
	}
	return out, nil
}

// resetActor returns the actor of a reset_task call. The credential decides
// first: a session's scoped credential names the session and a federation peer
// names itself, whatever the body claims, so a session cannot pass as the
// operator. An admin-token caller is the operator unless its body names another
// actor (a script that names itself is not the operator). Only the operator's
// force reset clears the attempt count.
func (s *Server) resetActor(r *http.Request, claimed string) string {
	if who := s.auditActor(r.Context()); who != "operator" {
		return who
	}
	if claimed = strings.TrimSpace(claimed); claimed != "" {
		return claimed
	}
	return "operator"
}
