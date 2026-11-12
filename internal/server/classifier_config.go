// Server side of the classifier.* config keys (everything except
// classifier.publisher_allowlist, which has its own confirmed-add endpoint):
// PUT /api/config validation and the applyConfigPatch cases. The reach of each
// key is the same on REST, the CLI `datawatch config set`, the comm
// `configure` verb and the web settings, because all of them end in
// handlePutConfig.

package server

import (
	"fmt"
	"math"
	"strings"

	"github.com/dmz006/datawatch/internal/classifier"
	"github.com/dmz006/datawatch/internal/config"
)

const classifierKeyPrefix = "classifier."

// classifierKeys is every settable classifier.* key except publisher_allowlist.
var classifierKeys = []string{
	"timeout_seconds", "top_logprobs", "abstain_margin", "min_probability",
	"min_option_mass", "rotate_options", "thinking_off_models",
	"probe_ttl_seconds", "probe_timeout_seconds",
	"calibration.test_fraction", "calibration.split_seed", "calibration.min_fit_samples",
	"calibration.false_pass_weight", "calibration.rotation_bias_threshold",
	"calibration.rotation_flip_threshold",
}

func classifierNumber(key string, v interface{}) (float64, error) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case int:
		f = float64(x)
	case string:
		if _, err := fmt.Sscanf(strings.TrimSpace(x), "%g", &f); err != nil {
			return 0, fmt.Errorf("classifier.%s: must be a number", key)
		}
	default:
		return 0, fmt.Errorf("classifier.%s: must be a number", key)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("classifier.%s: must be a finite number", key)
	}
	return f, nil
}

func classifierBool(key string, v interface{}) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "yes", "1", "on":
			return true, nil
		case "false", "no", "0", "off":
			return false, nil
		}
	}
	return false, fmt.Errorf("classifier.%s: must be true or false", key)
}

func classifierList(v interface{}) ([]string, bool) {
	if arr, ok := toStringArray(v); ok {
		return arr, true
	}
	if s, ok := v.(string); ok {
		return splitCSV(s), true
	}
	return nil, false
}

// validateClassifierValue checks one classifier key's patch value. A zero is
// legal for every numeric key and reads as the documented default.
func validateClassifierValue(key string, v interface{}) error {
	switch key {
	case "rotate_options":
		_, err := classifierBool(key, v)
		return err
	case "thinking_off_models":
		if _, ok := classifierList(v); !ok {
			return fmt.Errorf("classifier.thinking_off_models: must be a list of model-name substrings")
		}
		return nil
	case "calibration.split_seed":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("classifier.calibration.split_seed: must be text")
		}
		return nil
	}
	f, err := classifierNumber(key, v)
	if err != nil {
		return err
	}
	switch key {
	case "timeout_seconds", "top_logprobs", "probe_ttl_seconds", "probe_timeout_seconds", "calibration.min_fit_samples":
		if f != math.Trunc(f) || f < 0 {
			return fmt.Errorf("classifier.%s: must be a whole number >= 0 (0 = default)", key)
		}
	case "abstain_margin", "min_probability", "min_option_mass", "calibration.rotation_bias_threshold", "calibration.rotation_flip_threshold":
		if f < 0 || f > 1 {
			return fmt.Errorf("classifier.%s: must be from 0 to 1 (0 = default)", key)
		}
	case "calibration.test_fraction":
		if f < 0 || f >= 1 {
			return fmt.Errorf("classifier.calibration.test_fraction: must be from 0 up to but not including 1 (0 = default)")
		}
	case "calibration.false_pass_weight":
		if f < 0 {
			return fmt.Errorf("classifier.calibration.false_pass_weight: must be >= 0 (0 = default)")
		}
	}
	return nil
}

// validateClassifierPatch checks every classifier.* key in a PUT /api/config
// body before anything is applied. An unknown classifier.* key is rejected, and
// the publisher allowlist is refused here so its confirmation cannot be bypassed.
func validateClassifierPatch(patch map[string]interface{}) error {
	for k, v := range patch {
		if !strings.HasPrefix(k, classifierKeyPrefix) {
			continue
		}
		key := strings.TrimPrefix(k, classifierKeyPrefix)
		if key == "publisher_allowlist" {
			return fmt.Errorf("classifier.publisher_allowlist: add publishers with the confirmed publisher add (REST, MCP, CLI or chat), not here")
		}
		known := false
		for _, kk := range classifierKeys {
			if kk == key {
				known = true
			}
		}
		if !known {
			return fmt.Errorf("%s: unknown key (allowed: %s)", k, strings.Join(classifierKeys, ", "))
		}
		if err := validateClassifierValue(key, v); err != nil {
			return err
		}
	}
	return nil
}

// applyClassifierPatchKey stores one validated classifier.* key; a value that
// fails validation is left unchanged.
func applyClassifierPatchKey(c *config.ClassifierConfig, fullKey string, v interface{}) {
	key := strings.TrimPrefix(fullKey, classifierKeyPrefix)
	if validateClassifierValue(key, v) != nil {
		return
	}
	n := func() float64 { f, _ := classifierNumber(key, v); return f }
	switch key {
	case "timeout_seconds":
		c.TimeoutSeconds = int(n())
	case "top_logprobs":
		c.TopLogprobs = int(n())
	case "probe_ttl_seconds":
		c.ProbeTTLSeconds = int(n())
	case "probe_timeout_seconds":
		c.ProbeTimeoutSeconds = int(n())
	case "abstain_margin":
		c.AbstainMargin = n()
	case "min_probability":
		c.MinProbability = n()
	case "min_option_mass":
		c.MinOptionMass = n()
	case "rotate_options":
		c.RotateOptions, _ = classifierBool(key, v)
	case "thinking_off_models":
		c.ThinkingOffModels, _ = classifierList(v)
	case "calibration.test_fraction":
		c.Calibration.TestFraction = n()
	case "calibration.split_seed":
		c.Calibration.SplitSeed, _ = v.(string)
	case "calibration.min_fit_samples":
		c.Calibration.MinFitSamples = int(n())
	case "calibration.false_pass_weight":
		c.Calibration.FalsePassWeight = n()
	case "calibration.rotation_bias_threshold":
		c.Calibration.RotationBiasThreshold = n()
	case "calibration.rotation_flip_threshold":
		c.Calibration.RotationFlipThreshold = n()
	}
}

// classifierForDisplay is the GET /api/config classifier block: the EFFECTIVE
// values, so an unset config shows its defaults. The allowlist is listed too.
func classifierForDisplay(c config.ClassifierConfig) map[string]interface{} {
	cl := struct {
		client classifier.ClientConfig
		probe  classifier.Config
		cal    classifier.CalibrationConfig
	}{classifier.ClientConfigFrom(c), classifier.ProbeConfigFrom(c), classifier.CalibrationConfigFrom(c)}
	allow := append([]string{}, c.PublisherAllowlist...)
	return map[string]interface{}{
		"publisher_allowlist":   allow,
		"timeout_seconds":       int(cl.client.Timeout.Seconds()),
		"top_logprobs":          cl.client.TopLogprobs,
		"abstain_margin":        cl.client.AbstainMargin,
		"min_probability":       cl.client.MinProbability,
		"min_option_mass":       cl.client.MinOptionMass,
		"rotate_options":        cl.client.RotateOptions,
		"thinking_off_models":   cl.client.ThinkingOffModels,
		"probe_ttl_seconds":     int(cl.probe.ProbeTTL.Seconds()),
		"probe_timeout_seconds": int(cl.probe.ProbeTimeout.Seconds()),
		"calibration": map[string]interface{}{
			"test_fraction":           cl.cal.TestFraction,
			"split_seed":              cl.cal.SplitSeed,
			"min_fit_samples":         cl.cal.MinFitSamples,
			"false_pass_weight":       cl.cal.FalsePassWeight,
			"rotation_bias_threshold": cl.cal.RotationBiasThreshold,
			"rotation_flip_threshold": cl.cal.RotationFlipThreshold,
		},
	}
}
