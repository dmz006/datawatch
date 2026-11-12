package classifier

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestConfigFromDefaultsAndOverrides(t *testing.T) {
	d := ClientConfigFrom(config.ClassifierConfig{})
	if d.Timeout != DefaultClientTimeout || d.TopLogprobs != DefaultTopLogprobs || d.MinProbability != DefaultMinProbability {
		t.Fatalf("defaults not applied: %+v", d)
	}
	c := ClientConfigFrom(config.ClassifierConfig{TimeoutSeconds: 5, AbstainMargin: 0.2, RotateOptions: true})
	if c.Timeout != 5*time.Second || c.AbstainMargin != 0.2 || !c.RotateOptions {
		t.Fatalf("overrides lost: %+v", c)
	}
	k := CalibrationConfigFrom(config.ClassifierConfig{Calibration: config.ClassifierCalibrationConfig{MinFitSamples: 7}})
	if k.MinFitSamples != 7 || k.TestFraction != DefaultTestFraction {
		t.Fatalf("calibration: %+v", k)
	}
}

func TestProbeConfigFromDefaultsAndOverrides(t *testing.T) {
	d := ProbeConfigFrom(config.ClassifierConfig{})
	if d.ProbeTTL != DefaultProbeTTL || d.ProbeTimeout != DefaultProbeTimeout {
		t.Fatalf("defaults not applied: %+v", d)
	}
	c := ProbeConfigFrom(config.ClassifierConfig{ProbeTTLSeconds: 90, ProbeTimeoutSeconds: 2})
	if c.ProbeTTL != 90*time.Second || c.ProbeTimeout != 2*time.Second {
		t.Fatalf("overrides lost: %+v", c)
	}
}

func TestConfigFromAllFieldsReachTunables(t *testing.T) {
	in := config.ClassifierConfig{
		TimeoutSeconds: 7, TopLogprobs: 4, AbstainMargin: 0.2, MinProbability: 0.6,
		MinOptionMass: 0.7, RotateOptions: true, ThinkingOffModels: []string{"x"},
		Calibration: config.ClassifierCalibrationConfig{TestFraction: 0.4, SplitSeed: "s",
			MinFitSamples: 9, FalsePassWeight: 3, RotationBiasThreshold: 0.2, RotationFlipThreshold: 0.3},
	}
	c := ClientConfigFrom(in)
	if c.Timeout != 7*time.Second || c.TopLogprobs != 4 || c.AbstainMargin != 0.2 || c.MinProbability != 0.6 ||
		c.MinOptionMass != 0.7 || !c.RotateOptions || len(c.ThinkingOffModels) != 1 {
		t.Fatalf("client: %+v", c)
	}
	k := CalibrationConfigFrom(in)
	if k.TestFraction != 0.4 || k.SplitSeed != "s" || k.MinFitSamples != 9 || k.FalsePassWeight != 3 ||
		k.RotationBiasThreshold != 0.2 || k.RotationFlipThreshold != 0.3 {
		t.Fatalf("calibration: %+v", k)
	}
}
