package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func classifierGet(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := doBurnGuard(s, http.MethodGet, "/api/config", "", s.handleGetConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rr.Code, rr.Body.String())
	}
	var all map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	c, ok := all["classifier"].(map[string]interface{})
	if !ok {
		t.Fatalf("no classifier block: %s", rr.Body.String())
	}
	return c
}

func TestClassifierConfigGetShowsEffectiveDefaults(t *testing.T) {
	c := classifierGet(t, burnGuardConfigServer(t))
	want := map[string]float64{"timeout_seconds": 30, "top_logprobs": 10, "abstain_margin": 0.10,
		"min_probability": 0.50, "min_option_mass": 0.50, "probe_ttl_seconds": 600, "probe_timeout_seconds": 5}
	for k, w := range want {
		if got, _ := c[k].(float64); got != w {
			t.Errorf("%s = %v, want %v", k, c[k], w)
		}
	}
	if c["rotate_options"] != false {
		t.Errorf("rotate_options = %v", c["rotate_options"])
	}
	cal := c["calibration"].(map[string]interface{})
	if cal["split_seed"] != "classifier-calibration" || cal["min_fit_samples"].(float64) != 20 ||
		cal["false_pass_weight"].(float64) != 5 || cal["test_fraction"].(float64) != 0.30 {
		t.Errorf("calibration = %v", cal)
	}
}

func TestClassifierConfigPutRoundTrip(t *testing.T) {
	s := burnGuardConfigServer(t)
	body := `{"classifier.timeout_seconds":12,"classifier.top_logprobs":"6","classifier.abstain_margin":0.2,
"classifier.min_probability":0.6,"classifier.min_option_mass":0.7,"classifier.rotate_options":"true",
"classifier.thinking_off_models":"qwen3, glm","classifier.probe_ttl_seconds":90,"classifier.probe_timeout_seconds":2,
"classifier.calibration.test_fraction":0.4,"classifier.calibration.split_seed":"seed",
"classifier.calibration.min_fit_samples":9,"classifier.calibration.false_pass_weight":3,
"classifier.calibration.rotation_bias_threshold":0.2,"classifier.calibration.rotation_flip_threshold":0.3}`
	rr := doBurnGuard(s, http.MethodPut, "/api/config", body, s.handlePutConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, rr.Body.String())
	}
	c := s.cfg.Classifier
	if c.TimeoutSeconds != 12 || c.TopLogprobs != 6 || c.AbstainMargin != 0.2 || c.MinProbability != 0.6 ||
		c.MinOptionMass != 0.7 || !c.RotateOptions || len(c.ThinkingOffModels) != 2 ||
		c.ProbeTTLSeconds != 90 || c.ProbeTimeoutSeconds != 2 {
		t.Fatalf("stored = %+v", c)
	}
	k := c.Calibration
	if k.TestFraction != 0.4 || k.SplitSeed != "seed" || k.MinFitSamples != 9 || k.FalsePassWeight != 3 ||
		k.RotationBiasThreshold != 0.2 || k.RotationFlipThreshold != 0.3 {
		t.Fatalf("calibration = %+v", k)
	}
	got := classifierGet(t, s)
	if got["timeout_seconds"].(float64) != 12 || got["rotate_options"] != true {
		t.Fatalf("GET after PUT = %v", got)
	}
	// Zero is legal: it clears the override back to the default, and false is stored.
	rr = doBurnGuard(s, http.MethodPut, "/api/config",
		`{"classifier.timeout_seconds":0,"classifier.rotate_options":false,"classifier.calibration.min_fit_samples":0}`, s.handlePutConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT zero = %d: %s", rr.Code, rr.Body.String())
	}
	if s.cfg.Classifier.TimeoutSeconds != 0 || s.cfg.Classifier.RotateOptions || s.cfg.Classifier.Calibration.MinFitSamples != 0 {
		t.Fatalf("zero not stored: %+v", s.cfg.Classifier)
	}
	if classifierGet(t, s)["timeout_seconds"].(float64) != 30 {
		t.Fatal("zero should read back as the default")
	}
}

func TestClassifierConfigPutRejectsBadValues(t *testing.T) {
	bad := []string{
		`{"classifier.timeout_seconds":-1}`,
		`{"classifier.timeout_seconds":1.5}`,
		`{"classifier.top_logprobs":"x"}`,
		`{"classifier.abstain_margin":1.5}`,
		`{"classifier.min_probability":-0.1}`,
		`{"classifier.rotate_options":"maybe"}`,
		`{"classifier.thinking_off_models":5}`,
		`{"classifier.calibration.split_seed":5}`,
		`{"classifier.calibration.test_fraction":1}`,
		`{"classifier.calibration.false_pass_weight":-2}`,
		`{"classifier.calibration.min_fit_samples":2.5}`,
		`{"classifier.probe_ttl_seconds":true}`,
		`{"classifier.nonsense":1}`,
		`{"classifier.publisher_allowlist":["x"]}`,
	}
	for _, b := range bad {
		s := burnGuardConfigServer(t)
		rr := doBurnGuard(s, http.MethodPut, "/api/config", b, s.handlePutConfig)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", b, rr.Code)
		}
		if strings.Contains(b, "publisher") && !strings.Contains(rr.Body.String(), "confirmed publisher add") {
			t.Errorf("allowlist refusal text: %s", rr.Body.String())
		}
	}
}

func TestApplyClassifierPatchKeySkipsInvalid(t *testing.T) {
	s := burnGuardConfigServer(t)
	applyClassifierPatchKey(&s.cfg.Classifier, "classifier.top_logprobs", "bad")
	if s.cfg.Classifier.TopLogprobs != 0 {
		t.Fatal("invalid value applied")
	}
}
