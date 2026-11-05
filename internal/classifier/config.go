package classifier

import (
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// ClientConfig holds the classifier client tunables. Config keys (documented in
// docs/implementation.md): classifier.timeout, classifier.top_logprobs,
// classifier.abstain_margin, classifier.min_probability,
// classifier.min_option_mass, classifier.rotate_options and
// classifier.thinking_off_models.
type ClientConfig struct {
	// Timeout bounds one classify call. Default 30s. A timeout is a typed
	// infra error.
	Timeout time.Duration
	// TopLogprobs is top_logprobs on the generic path. Default 10.
	TopLogprobs int
	// AbstainMargin: the chosen option must lead the runner-up by at least this
	// probability, otherwise the call abstains. Default 0.10.
	AbstainMargin float64
	// MinProbability: the chosen option must reach this probability, otherwise
	// the call abstains. Default 0.50.
	MinProbability float64
	// MinOptionMass: on the generic path the option tokens must hold at least
	// this share of the first-token probability, otherwise the scores are
	// treated as missing. Default 0.50.
	MinOptionMass float64
	// RotateOptions rotates the option order across calls on the generic path
	// and averages the scores. Off by default; the native path never rotates.
	RotateOptions bool
	// ThinkingOffModels lists case-insensitive model-name substrings whose chat
	// templates get thinking disabled on the generic path. Default ["qwen3"].
	ThinkingOffModels []string
}

// Defaults for ClientConfig.
const (
	DefaultClientTimeout  = 30 * time.Second
	DefaultTopLogprobs    = 10
	DefaultAbstainMargin  = 0.10
	DefaultMinProbability = 0.50
	DefaultMinOptionMass  = 0.50
)

func (c ClientConfig) withDefaults() ClientConfig {
	if c.Timeout <= 0 {
		c.Timeout = DefaultClientTimeout
	}
	if c.TopLogprobs <= 0 {
		c.TopLogprobs = DefaultTopLogprobs
	}
	if c.AbstainMargin <= 0 {
		c.AbstainMargin = DefaultAbstainMargin
	}
	if c.MinProbability <= 0 {
		c.MinProbability = DefaultMinProbability
	}
	if c.MinOptionMass <= 0 {
		c.MinOptionMass = DefaultMinOptionMass
	}
	if c.ThinkingOffModels == nil {
		c.ThinkingOffModels = []string{"qwen3"}
	}
	return c
}

// CalibrationConfig holds the calibration tunables. Config keys:
// classifier.calibration.test_fraction, .split_seed, .min_fit_samples,
// .false_pass_weight, .rotation_bias_threshold and .rotation_flip_threshold.
type CalibrationConfig struct {
	// TestFraction is the share of samples held out for scoring. Default 0.30.
	TestFraction float64
	// SplitSeed salts the deterministic split. Default "classifier-calibration".
	SplitSeed string
	// MinFitSamples is the least number of fit samples a threshold may be
	// fitted on. Default 20.
	MinFitSamples int
	// FalsePassWeight is how many false blocks one false pass costs when the
	// threshold is chosen. Default 5 (a false pass is worse).
	FalsePassWeight float64
	// RotationBiasThreshold: position-mass deviation from uniform at or above
	// which rotation is recommended. Default 0.10.
	RotationBiasThreshold float64
	// RotationFlipThreshold: share of items whose winning option changes across
	// rotations at or above which rotation is recommended. Default 0.10.
	RotationFlipThreshold float64
}

// Defaults for CalibrationConfig.
const (
	DefaultTestFraction          = 0.30
	DefaultSplitSeed             = "classifier-calibration"
	DefaultMinFitSamples         = 20
	DefaultFalsePassWeight       = 5.0
	DefaultRotationBiasThreshold = 0.10
	DefaultRotationFlipThreshold = 0.10
)

func (c CalibrationConfig) withDefaults() CalibrationConfig {
	if c.TestFraction <= 0 || c.TestFraction >= 1 {
		c.TestFraction = DefaultTestFraction
	}
	if c.SplitSeed == "" {
		c.SplitSeed = DefaultSplitSeed
	}
	if c.MinFitSamples <= 0 {
		c.MinFitSamples = DefaultMinFitSamples
	}
	if c.FalsePassWeight <= 0 {
		c.FalsePassWeight = DefaultFalsePassWeight
	}
	if c.RotationBiasThreshold <= 0 {
		c.RotationBiasThreshold = DefaultRotationBiasThreshold
	}
	if c.RotationFlipThreshold <= 0 {
		c.RotationFlipThreshold = DefaultRotationFlipThreshold
	}
	return c
}

// ClientConfigFrom builds the client tunables from the config file; zero
// values fall back to the documented defaults.
func ClientConfigFrom(c config.ClassifierConfig) ClientConfig {
	return ClientConfig{
		Timeout:           time.Duration(c.TimeoutSeconds) * time.Second,
		TopLogprobs:       c.TopLogprobs,
		AbstainMargin:     c.AbstainMargin,
		MinProbability:    c.MinProbability,
		MinOptionMass:     c.MinOptionMass,
		RotateOptions:     c.RotateOptions,
		ThinkingOffModels: c.ThinkingOffModels,
	}.withDefaults()
}

// CalibrationConfigFrom builds the calibration tunables from the config file;
// zero values fall back to the documented defaults.
func CalibrationConfigFrom(c config.ClassifierConfig) CalibrationConfig {
	k := c.Calibration
	return CalibrationConfig{
		TestFraction:          k.TestFraction,
		SplitSeed:             k.SplitSeed,
		MinFitSamples:         k.MinFitSamples,
		FalsePassWeight:       k.FalsePassWeight,
		RotationBiasThreshold: k.RotationBiasThreshold,
		RotationFlipThreshold: k.RotationFlipThreshold,
	}.withDefaults()
}

// ProbeConfigFrom builds the capability probe tunables from the config file;
// zero values fall back to the documented defaults.
func ProbeConfigFrom(c config.ClassifierConfig) Config {
	return Config{
		ProbeTTL:     time.Duration(c.ProbeTTLSeconds) * time.Second,
		ProbeTimeout: time.Duration(c.ProbeTimeoutSeconds) * time.Second,
	}.withDefaults()
}
