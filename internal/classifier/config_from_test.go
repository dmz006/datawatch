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
