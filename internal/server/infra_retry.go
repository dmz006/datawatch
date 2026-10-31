// Infrastructure retry surfaces: GET /api/autonomous/infra-retries and the
// autonomous.infra_retry block on GET /api/config and PUT /api/config.
//
// The route reads the retry policy in effect and every task that was retried,
// or is held, because the machinery around it failed. The config block holds
// the policy. PUT /api/config checks every key before anything is applied, with
// the rules of config.InfraRetryConfig.Validate plus an upper bound on each
// value, so a bad value refuses the whole PUT. The two list keys take a JSON
// array or a comma list, and an explicit empty list is kept as empty.
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// infraRetryKeyPrefix is the config path every autonomous.infra_retry.* key
// shares.
const infraRetryKeyPrefix = "autonomous.infra_retry."

// PUT /api/config keys of the infrastructure retry block.
const (
	keyInfraRetryEnabled         = infraRetryKeyPrefix + "enabled"
	keyInfraRetryMaxTries        = infraRetryKeyPrefix + "max_tries"
	keyInfraRetryBackoffSeconds  = infraRetryKeyPrefix + "backoff_seconds"
	keyInfraRetryCategories      = infraRetryKeyPrefix + "categories"
	keyInfraRetryExhaustedAction = infraRetryKeyPrefix + "exhausted_action"
	keyInfraRetryRetryDependents = infraRetryKeyPrefix + "retry_dependents"
	keyInfraRetryRaiseQuestion   = infraRetryKeyPrefix + "raise_question"
)

// Upper bounds on what PUT /api/config accepts: the try limit sizes the delay
// list, and a delay is multiplied into a duration, so an unbounded value could
// exhaust memory or overflow.
const (
	infraRetryMaxTriesLimit = 1000
	infraRetryMaxDelayItems = 1000
	infraRetryMaxDelaySecs  = 7 * 24 * 60 * 60
)

// infraRetryAPI is what the Automaton manager offers for the infrastructure
// retry read. A manager that does not (a test double, for example) answers 501.
type infraRetryAPI interface {
	InfraRetryStatus(automatonID string, limit int) autonomous.InfraRetryStatus
}

// handleAutonomousInfraRetries serves GET /api/autonomous/infra-retries: the
// retry policy in effect and up to limit tasks (default 50, at most 1000), the
// most recently retried first. The optional automaton value limits the list to
// one Automaton.
func (s *Server) handleAutonomousInfraRetries(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	q := r.URL.Query()
	limit, err := laneFillLimit(q)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	api, ok := s.autonomousMgr.(infraRetryAPI)
	if !ok {
		http.Error(w, "infrastructure retry is not available on this daemon", http.StatusNotImplemented)
		return
	}
	writeJSONOK(w, api.InfraRetryStatus(strings.TrimSpace(q.Get("automaton")), limit))
}

// infraRetryConfigSurface is the autonomous.infra_retry block GET /api/config
// serves: the seven values in effect, so an unset key shows its default.
func infraRetryConfigSurface(c config.InfraRetryConfig) autonomous.InfraRetrySettings {
	return autonomous.InfraRetrySettingsFrom(c)
}

// validateInfraRetryPatch checks the infrastructure retry keys of a PUT
// /api/config body before anything is applied. A key that is absent is not
// checked. The error names the key and the valid values.
func validateInfraRetryPatch(patch map[string]any) error {
	var scratch config.InfraRetryConfig
	touched := false
	for _, key := range infraRetryKeys {
		v, ok := patch[key]
		if !ok {
			continue
		}
		touched = true
		if _, err := applyInfraRetryKey(&scratch, key, v); err != nil {
			return err
		}
	}
	if !touched {
		return nil
	}
	// An unset try limit reads as the default, which Validate would refuse as 0.
	if scratch.MaxTries == 0 {
		scratch.MaxTries = config.InfraRetryDefaultMaxTries
	}
	return scratch.Validate()
}

// infraRetryKeys lists the keys of the block in a fixed order, so the first
// bad value reported does not depend on map order.
var infraRetryKeys = []string{
	keyInfraRetryEnabled, keyInfraRetryMaxTries, keyInfraRetryBackoffSeconds,
	keyInfraRetryCategories, keyInfraRetryExhaustedAction,
	keyInfraRetryRetryDependents, keyInfraRetryRaiseQuestion,
}

// applyInfraRetryPatch writes one infrastructure retry key into the daemon
// config. It reports whether key belongs to the block. The value was checked by
// validateInfraRetryPatch before the PUT reached this point, so a value that
// does not parse here is left unchanged.
func applyInfraRetryPatch(cfg *config.Config, key string, v any) bool {
	known, _ := applyInfraRetryKey(&cfg.Autonomous.InfraRetry, key, v)
	return known
}

// applyInfraRetryKey parses one key's value and stores it in c. known reports
// whether key is a key of the block; when err is not nil, c is unchanged.
func applyInfraRetryKey(c *config.InfraRetryConfig, key string, v any) (known bool, err error) {
	switch key {
	case keyInfraRetryEnabled, keyInfraRetryRetryDependents, keyInfraRetryRaiseQuestion:
		b, ok := failureEscalationBool(v)
		if !ok {
			return true, fmt.Errorf("%s: %v is invalid; valid values: true, false", key, v)
		}
		switch key {
		case keyInfraRetryEnabled:
			c.Enabled = &b
		case keyInfraRetryRetryDependents:
			c.RetryDependents = &b
		default:
			c.RaiseQuestion = &b
		}
	case keyInfraRetryMaxTries:
		n, ok := infraRetryWhole(v)
		if !ok || n < 1 || n > infraRetryMaxTriesLimit {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number from 1 to %d (the default is %d)",
				key, v, infraRetryMaxTriesLimit, config.InfraRetryDefaultMaxTries)
		}
		c.MaxTries = n
	case keyInfraRetryBackoffSeconds:
		list, err := parseInfraRetryBackoff(v)
		if err != nil {
			return true, err
		}
		c.BackoffSeconds = list
	case keyInfraRetryCategories:
		list, err := parseInfraRetryCategories(v)
		if err != nil {
			return true, err
		}
		c.Categories = list
	case keyInfraRetryExhaustedAction:
		str, ok := v.(string)
		if !ok {
			return true, fmt.Errorf("%s: %v is invalid; use %q or %q", key, v,
				config.ExhaustedActionHold, config.ExhaustedActionCount)
		}
		c.ExhaustedAction = strings.TrimSpace(str)
	default:
		return false, nil
	}
	return true, nil
}

// parseInfraRetryBackoff reads the backoff_seconds value: a JSON array of whole
// numbers or a comma list such as 60,300,540. An empty array or an empty string
// is an explicit "no delay" and returns an empty list that is not nil; null
// clears the key back to unset (nil), which reads the default list.
func parseInfraRetryBackoff(v any) ([]int, error) {
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
		return nil, fmt.Errorf("%s: must be a JSON array of whole numbers of seconds or a comma list such as 60,300,540",
			keyInfraRetryBackoffSeconds)
	}
	if len(items) > infraRetryMaxDelayItems {
		return nil, fmt.Errorf("%s: %d entries is too many; use at most %d",
			keyInfraRetryBackoffSeconds, len(items), infraRetryMaxDelayItems)
	}
	out := make([]int, 0, len(items))
	for i, it := range items {
		n, ok := infraRetryWhole(it)
		if !ok {
			return nil, fmt.Errorf("%s[%d]: %v is invalid; use a whole number of seconds", keyInfraRetryBackoffSeconds, i, it)
		}
		if n > infraRetryMaxDelaySecs {
			return nil, fmt.Errorf("%s[%d]: %d is invalid; use at most %d seconds",
				keyInfraRetryBackoffSeconds, i, n, infraRetryMaxDelaySecs)
		}
		out = append(out, n)
	}
	return out, nil
}

// parseInfraRetryCategories reads the categories value: a JSON array of failure
// class names or a comma list. An empty array or an empty string is an
// explicit "retry no class" and returns an empty list that is not nil; null
// clears the key back to unset (nil), which reads all four classes. Whether
// each name is a known class is checked by config.InfraRetryConfig.Validate.
func parseInfraRetryCategories(v any) ([]string, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		out := []string{}
		if strings.TrimSpace(x) == "" {
			return out, nil
		}
		for _, part := range strings.Split(x, ",") {
			out = append(out, strings.TrimSpace(part))
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(x))
		for i, it := range x {
			str, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d]: %v is invalid; use a failure class name as text",
					keyInfraRetryCategories, i, it)
			}
			out = append(out, strings.TrimSpace(str))
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s: must be a JSON array of failure class names or a comma list", keyInfraRetryCategories)
}

// infraRetryWhole reads a whole number: a JSON number with no fraction, or a
// string of digits. A fraction such as 2.5 is refused, not cut.
func infraRetryWhole(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		if x != float64(int64(x)) || x > 1<<40 || x < -(1<<40) {
			return 0, false
		}
		return int(x), true
	case int:
		return x, true
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return n, true
		}
	}
	return 0, false
}
