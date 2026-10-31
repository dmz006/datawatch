// Request watch configuration surfaces: the autonomous.request_watch block on
// GET /api/config, PUT /api/config and the Automata settings route (GET and
// PUT /api/autonomous/config).
//
// The block holds the policy of the request watch. PUT checks every key
// before anything is applied, so a bad value answers 400 with a message that
// names the key and the allowed values, and nothing is written. The watcher
// reads the live block on every tick, so a write applies with no restart.
// The list key takes a JSON array or a comma list, and an explicit empty list
// is kept as empty (only keep_waiting is offered), which is not the same as an
// unset list (all four options are offered).
package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// requestWatchKeyPrefix is the config path every autonomous.request_watch.*
// key shares.
const requestWatchKeyPrefix = "autonomous.request_watch."

// PUT /api/config keys of the request watch block.
const (
	keyRequestWatchEnabled            = "autonomous.request_watch.enabled"
	keyRequestWatchPollSeconds        = "autonomous.request_watch.poll_seconds"
	keyRequestWatchNoProgressMinutes  = "autonomous.request_watch.no_progress_minutes"
	keyRequestWatchObserveWorkers     = "autonomous.request_watch.observe_workers"
	keyRequestWatchObserveVerifiers   = "autonomous.request_watch.observe_verifiers"
	keyRequestWatchUseGPUSignal       = "autonomous.request_watch.use_gpu_signal"
	keyRequestWatchGPUBusyPercent     = "autonomous.request_watch.gpu_busy_percent"
	keyRequestWatchUnreachableCounts  = "autonomous.request_watch.unreachable_counts"
	keyRequestWatchRaiseAlert         = "autonomous.request_watch.raise_alert"
	keyRequestWatchRaiseQuestion      = "autonomous.request_watch.raise_question"
	keyRequestWatchRemediationOptions = "autonomous.request_watch.remediation_options"
)

// Upper bounds on what a write accepts: both numbers are multiplied into a
// duration, so an unbounded value could overflow.
const (
	requestWatchPollSecondsLimit       = 24 * 60 * 60
	requestWatchNoProgressMinutesLimit = 7 * 24 * 60
	requestWatchListItemsLimit         = 100
)

// requestWatchKeys lists the keys of the block in a fixed order, so the first
// bad value reported does not depend on map order.
var requestWatchKeys = []string{
	keyRequestWatchEnabled, keyRequestWatchPollSeconds, keyRequestWatchNoProgressMinutes,
	keyRequestWatchObserveWorkers, keyRequestWatchObserveVerifiers, keyRequestWatchUseGPUSignal,
	keyRequestWatchGPUBusyPercent, keyRequestWatchUnreachableCounts, keyRequestWatchRaiseAlert,
	keyRequestWatchRaiseQuestion, keyRequestWatchRemediationOptions,
}

// requestWatchSettings is the autonomous.request_watch block GET serves: all
// eleven values in effect, so an unset key shows its default.
type requestWatchSettings struct {
	Enabled            bool     `json:"enabled"`
	PollSeconds        int      `json:"poll_seconds"`
	NoProgressMinutes  int      `json:"no_progress_minutes"`
	ObserveWorkers     bool     `json:"observe_workers"`
	ObserveVerifiers   bool     `json:"observe_verifiers"`
	UseGPUSignal       bool     `json:"use_gpu_signal"`
	GPUBusyPercent     int      `json:"gpu_busy_percent"`
	UnreachableCounts  bool     `json:"unreachable_counts"`
	RaiseAlert         bool     `json:"raise_alert"`
	RaiseQuestion      bool     `json:"raise_question"`
	RemediationOptions []string `json:"remediation_options"`
}

// requestWatchConfigSurface returns the block with every default filled in.
// remediation_options is the list in effect: keep_waiting first, then the
// configured options (all four when the key is unset).
func requestWatchConfigSurface(c config.RequestWatchConfig) requestWatchSettings {
	return requestWatchSettings{
		Enabled:            c.EffectiveEnabled(),
		PollSeconds:        c.EffectivePollSeconds(),
		NoProgressMinutes:  c.EffectiveNoProgressMinutes(),
		ObserveWorkers:     c.EffectiveObserveWorkers(),
		ObserveVerifiers:   c.EffectiveObserveVerifiers(),
		UseGPUSignal:       c.EffectiveUseGPUSignal(),
		GPUBusyPercent:     c.EffectiveGPUBusyPercent(),
		UnreachableCounts:  c.EffectiveUnreachableCounts(),
		RaiseAlert:         c.EffectiveRaiseAlert(),
		RaiseQuestion:      c.EffectiveRaiseQuestion(),
		RemediationOptions: c.EffectiveRemediationOptions(),
	}
}

// validateRequestWatchPatch checks the request watch keys of a PUT /api/config
// body before anything is applied. A key that is absent is not checked. The
// error names the key and the allowed values.
func validateRequestWatchPatch(patch map[string]any) error {
	var scratch config.RequestWatchConfig
	touched := false
	for _, key := range requestWatchKeys {
		v, ok := patch[key]
		if !ok {
			continue
		}
		touched = true
		if _, err := applyRequestWatchKey(&scratch, key, v); err != nil {
			return err
		}
	}
	if !touched {
		return nil
	}
	return scratch.Validate()
}

// applyRequestWatchPatch writes one request watch key into the daemon config.
// It reports whether key belongs to the block. The value was checked by
// validateRequestWatchPatch before the PUT reached this point, so a value that
// does not parse here is left unchanged.
func applyRequestWatchPatch(cfg *config.Config, key string, v any) bool {
	known, _ := applyRequestWatchKey(&cfg.Autonomous.RequestWatch, key, v)
	return known
}

// applyRequestWatchKey parses one key's value and stores it in c. known
// reports whether key is a key of the block; when err is not nil, c is
// unchanged.
func applyRequestWatchKey(c *config.RequestWatchConfig, key string, v any) (known bool, err error) {
	switch key {
	case keyRequestWatchEnabled, keyRequestWatchObserveWorkers, keyRequestWatchObserveVerifiers,
		keyRequestWatchUseGPUSignal, keyRequestWatchUnreachableCounts,
		keyRequestWatchRaiseAlert, keyRequestWatchRaiseQuestion:
		b, ok := failureEscalationBool(v)
		if !ok {
			return true, fmt.Errorf("%s: %v is invalid; valid values: true, false", key, v)
		}
		switch key {
		case keyRequestWatchEnabled:
			c.Enabled = &b
		case keyRequestWatchObserveWorkers:
			c.ObserveWorkers = &b
		case keyRequestWatchObserveVerifiers:
			c.ObserveVerifiers = &b
		case keyRequestWatchUseGPUSignal:
			c.UseGPUSignal = &b
		case keyRequestWatchUnreachableCounts:
			c.UnreachableCounts = &b
		case keyRequestWatchRaiseAlert:
			c.RaiseAlert = &b
		default:
			c.RaiseQuestion = &b
		}
	case keyRequestWatchPollSeconds:
		n, ok := infraRetryWhole(v)
		if !ok || n < 1 || n > requestWatchPollSecondsLimit {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number of seconds from 1 to %d (the default is %d)",
				key, v, requestWatchPollSecondsLimit, config.RequestWatchDefaultPollSeconds)
		}
		c.PollSeconds = n
	case keyRequestWatchNoProgressMinutes:
		n, ok := infraRetryWhole(v)
		if !ok || n < 1 || n > requestWatchNoProgressMinutesLimit {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number of minutes from 1 to %d (the default is %d)",
				key, v, requestWatchNoProgressMinutesLimit, config.RequestWatchDefaultNoProgressMin)
		}
		c.NoProgressMinutes = n
	case keyRequestWatchGPUBusyPercent:
		n, ok := infraRetryWhole(v)
		if !ok || n < 1 || n > 100 {
			return true, fmt.Errorf("%s: %v is invalid; use a whole number from 1 to 100 (the default is %d)",
				key, v, config.RequestWatchDefaultGPUBusyPercent)
		}
		c.GPUBusyPercent = n
	case keyRequestWatchRemediationOptions:
		list, err := parseRequestWatchOptions(v)
		if err != nil {
			return true, err
		}
		c.RemediationOptions = list
	default:
		return false, nil
	}
	return true, nil
}

// parseRequestWatchOptions reads the remediation_options value: a JSON array
// of option ids or a comma list. An empty array or an empty string is an
// explicit "keep_waiting only" and returns an empty list that is not nil; null
// clears the key back to unset (nil), which reads all four options. Whether
// each id is a known option is checked by config.RequestWatchConfig.Validate.
func parseRequestWatchOptions(v any) ([]string, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		out := []string{}
		if strings.TrimSpace(x) == "" {
			return out, nil
		}
		parts := strings.Split(x, ",")
		if len(parts) > requestWatchListItemsLimit {
			return nil, requestWatchOptionsTooMany(len(parts))
		}
		for _, part := range parts {
			out = append(out, strings.TrimSpace(part))
		}
		return out, nil
	case []any:
		if len(x) > requestWatchListItemsLimit {
			return nil, requestWatchOptionsTooMany(len(x))
		}
		out := make([]string, 0, len(x))
		for i, it := range x {
			str, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d]: %v is invalid; use an option id as text; valid options: %s",
					keyRequestWatchRemediationOptions, i, it, strings.Join(config.RequestWatchAllOptions(), ", "))
			}
			out = append(out, strings.TrimSpace(str))
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s: must be a JSON array of option ids or a comma list; valid options: %s",
		keyRequestWatchRemediationOptions, strings.Join(config.RequestWatchAllOptions(), ", "))
}

func requestWatchOptionsTooMany(n int) error {
	return fmt.Errorf("%s: %d entries is too many; use at most %d of: %s",
		keyRequestWatchRemediationOptions, n, requestWatchListItemsLimit, strings.Join(config.RequestWatchAllOptions(), ", "))
}

// validateRequestWatchObject checks the request_watch object of a PUT
// /api/autonomous/config body. The field names are the keys of the block
// without the prefix, and the values are real JSON values: a boolean, a whole
// number, or an array of option ids. A null value is accepted (it clears the
// key back to unset). The error names the key and the allowed values.
func validateRequestWatchObject(raw json.RawMessage) error {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return fmt.Errorf("autonomous.request_watch: must be an object of the request watch keys")
	}
	var scratch config.RequestWatchConfig
	for field := range obj {
		key := requestWatchKeyPrefix + field
		if !isRequestWatchKey(key) {
			return fmt.Errorf("%s: unknown key; valid keys: %s", key, requestWatchFieldNames())
		}
	}
	for _, key := range requestWatchKeys {
		v, ok := obj[strings.TrimPrefix(key, requestWatchKeyPrefix)]
		if !ok || v == nil {
			continue
		}
		if err := requestWatchObjectType(key, v); err != nil {
			return err
		}
		if _, err := applyRequestWatchKey(&scratch, key, v); err != nil {
			return err
		}
	}
	return scratch.Validate()
}

// requestWatchObjectType refuses a value of the wrong JSON type for key, which
// PUT /api/config would coerce from text but the typed settings route does not.
func requestWatchObjectType(key string, v any) error {
	switch key {
	case keyRequestWatchPollSeconds, keyRequestWatchNoProgressMinutes, keyRequestWatchGPUBusyPercent:
		if _, ok := v.(float64); !ok {
			return fmt.Errorf("%s: %v is invalid; use a whole number", key, v)
		}
	case keyRequestWatchRemediationOptions:
		if _, ok := v.([]any); !ok {
			return fmt.Errorf("%s: must be a JSON array of option ids; valid options: %s",
				key, strings.Join(config.RequestWatchAllOptions(), ", "))
		}
	default:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: %v is invalid; valid values: true, false", key, v)
		}
	}
	return nil
}

func isRequestWatchKey(key string) bool {
	for _, k := range requestWatchKeys {
		if k == key {
			return true
		}
	}
	return false
}

func requestWatchFieldNames() string {
	names := make([]string, 0, len(requestWatchKeys))
	for _, k := range requestWatchKeys {
		names = append(names, strings.TrimPrefix(k, requestWatchKeyPrefix))
	}
	return strings.Join(names, ", ")
}

// withRequestWatchSurface replaces the request_watch member of a marshaled
// autonomous manager config with the values in effect, so GET
// /api/autonomous/config shows every default filled in. A body that is not a
// JSON object is returned unchanged.
func withRequestWatchSurface(cfg any) any {
	b, err := json.Marshal(cfg)
	if err != nil {
		return cfg
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil || m == nil {
		return cfg
	}
	var rw config.RequestWatchConfig
	if raw, ok := m["request_watch"]; ok {
		_ = json.Unmarshal(raw, &rw)
	}
	enc, err := json.Marshal(requestWatchConfigSurface(rw))
	if err != nil {
		return cfg
	}
	m["request_watch"] = enc
	return m
}
