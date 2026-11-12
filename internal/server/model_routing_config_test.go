package server

// The autonomous.model_routing block on GET /api/config and PUT /api/config:
// the defaults, a round trip of every key, whole-PUT refusal of a bad value,
// and the saved file read back through the config loader.

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// modelRoutingBlock reads autonomous.model_routing from GET /api/config.
func modelRoutingBlock(t *testing.T, s *Server) ModelRoutingSurface {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["model_routing"]
	if !ok {
		t.Fatal("GET /api/config autonomous lacks model_routing")
	}
	var got ModelRoutingSurface
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode model_routing: %v (%s)", err, raw)
	}
	return got
}

func TestModelRoutingConfig_GetReturnsDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	raw := getAutonomousConfigMap(t, s)["model_routing"]
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("decode model_routing: %v", err)
	}
	for _, k := range []string{"refusal_mode", "served_models_cache_seconds", "verifier_follows_worker_node",
		"unknown_node_policy", "probe_timeout_seconds", "alert_on_refusal"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("model_routing lacks %q: %s", k, raw)
		}
	}
	if len(keys) != 6 {
		t.Errorf("model_routing has %d keys, want 6: %s", len(keys), raw)
	}
	want := ModelRoutingSurface{
		RefusalMode: "refuse", ServedModelsCacheSeconds: 300, VerifierFollowsWorkerNode: true,
		UnknownNodePolicy: "assume_serves", ProbeTimeoutSeconds: 8, AlertOnRefusal: true,
	}
	if got := modelRoutingBlock(t, s); got != want {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
}

func TestModelRoutingConfig_PutRoundTripsEveryKey(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	rec := putInfraRetryRaw(s, map[string]any{
		"autonomous.model_routing.refusal_mode":                 "warn",
		"autonomous.model_routing.served_models_cache_seconds":  0,
		"autonomous.model_routing.verifier_follows_worker_node": false,
		"autonomous.model_routing.unknown_node_policy":          "assume_not",
		"autonomous.model_routing.probe_timeout_seconds":        20,
		"autonomous.model_routing.alert_on_refusal":             false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want := ModelRoutingSurface{
		RefusalMode: "warn", ServedModelsCacheSeconds: 0, VerifierFollowsWorkerNode: false,
		UnknownNodePolicy: "assume_not", ProbeTimeoutSeconds: 20, AlertOnRefusal: false,
	}
	if got := modelRoutingBlock(t, s); got != want {
		t.Fatalf("GET after PUT = %+v, want %+v", got, want)
	}
	// The live config follows with no restart: the readers the Automaton
	// manager and the cache use see the new values at once.
	live := s.cfg.Autonomous.ModelRouting
	if live.EffectiveRefusalMode() != "warn" || live.EffectiveServedModelsCacheAge() != 0 ||
		live.EffectiveVerifierFollowsWorkerNode() || live.EffectiveUnknownNodePolicy() != "assume_not" ||
		live.EffectiveProbeTimeout().Seconds() != 20 || live.EffectiveAlertOnRefusal() {
		t.Fatalf("live config = %+v", live)
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load the saved config: %v", err)
	}
	if got := modelRoutingConfigSurface(saved.Autonomous.ModelRouting); got != want {
		t.Fatalf("saved file = %+v, want %+v", got, want)
	}

	// And back to the other value of every key, in text forms, one key at a
	// time so a PUT of one key leaves the rest alone.
	for key, val := range map[string]any{
		"autonomous.model_routing.refusal_mode":                 "refuse",
		"autonomous.model_routing.served_models_cache_seconds":  "45",
		"autonomous.model_routing.verifier_follows_worker_node": "true",
		"autonomous.model_routing.unknown_node_policy":          "assume_serves",
		"autonomous.model_routing.probe_timeout_seconds":        1,
		"autonomous.model_routing.alert_on_refusal":             true,
	} {
		if rec := putInfraRetryRaw(s, map[string]any{key: val}); rec.Code != http.StatusOK {
			t.Fatalf("PUT %s: want 200, got %d: %s", key, rec.Code, rec.Body.String())
		}
	}
	want = ModelRoutingSurface{
		RefusalMode: "refuse", ServedModelsCacheSeconds: 45, VerifierFollowsWorkerNode: true,
		UnknownNodePolicy: "assume_serves", ProbeTimeoutSeconds: 1, AlertOnRefusal: true,
	}
	if got := modelRoutingBlock(t, s); got != want {
		t.Fatalf("GET after the second round = %+v, want %+v", got, want)
	}

	// A whole-block object works too and changes only the keys it names.
	rec = putInfraRetryRaw(s, map[string]any{
		"autonomous.model_routing": map[string]any{"refusal_mode": "warn", "alert_on_refusal": false},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("whole-block PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want.RefusalMode, want.AlertOnRefusal = "warn", false
	if got := modelRoutingBlock(t, s); got != want {
		t.Fatalf("GET after the whole-block PUT = %+v, want %+v", got, want)
	}

	// An explicit false and an explicit 0 stay distinct from unset.
	if rec := putInfraRetryRaw(s, map[string]any{"autonomous.model_routing.served_models_cache_seconds": 0}); rec.Code != http.StatusOK {
		t.Fatalf("PUT 0: %d %s", rec.Code, rec.Body.String())
	}
	if got := modelRoutingBlock(t, s); got.ServedModelsCacheSeconds != 0 || got.AlertOnRefusal {
		t.Fatalf("explicit 0 or false fell back to a default: %+v", got)
	}
}

// Every bad value is refused with 400 naming its key, and a good key sent in
// the same PUT is not applied either.
func TestModelRoutingConfig_PutRefusesBadValueAndAppliesNothing(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  any
	}{
		{"refusal_mode unknown", "refusal_mode", "ignore"},
		{"refusal_mode number", "refusal_mode", 1},
		{"cache negative", "served_models_cache_seconds", -1},
		{"cache fraction", "served_models_cache_seconds", 2.5},
		{"cache text", "served_models_cache_seconds", "soon"},
		{"cache boolean", "served_models_cache_seconds", true},
		{"verifier text", "verifier_follows_worker_node", "maybe"},
		{"verifier number", "verifier_follows_worker_node", 1},
		{"policy unknown", "unknown_node_policy", "guess"},
		{"policy number", "unknown_node_policy", 3},
		{"probe zero", "probe_timeout_seconds", 0},
		{"probe negative", "probe_timeout_seconds", -5},
		{"probe fraction", "probe_timeout_seconds", 1.5},
		{"probe text", "probe_timeout_seconds", "long"},
		{"alert text", "alert_on_refusal", "yes"},
		{"alert number", "alert_on_refusal", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, cfgPath := newMaxConcurrentTasksServer(t)
			before := modelRoutingBlock(t, s)
			liveBefore := s.cfg.Autonomous.ModelRouting.Clone()
			key := modelRoutingKeyPrefix + "." + c.key

			patch := map[string]any{
				key: c.val,
				"autonomous.session_idle_timeout_seconds": 1234,
			}
			for _, other := range modelRoutingKeys {
				if other != key {
					patch[other] = map[string]any{
						keyModelRoutingRefusalMode:               "warn",
						keyModelRoutingServedModelsCacheSeconds:  9,
						keyModelRoutingVerifierFollowsWorkerNode: false,
						keyModelRoutingUnknownNodePolicy:         "assume_not",
						keyModelRoutingProbeTimeoutSeconds:       30,
						keyModelRoutingAlertOnRefusal:            false,
					}[other]
				}
			}
			rec := putInfraRetryRaw(s, patch)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), key) {
				t.Errorf("message %q does not name %s", rec.Body.String(), key)
			}
			if got := modelRoutingBlock(t, s); got != before {
				t.Errorf("a refused PUT changed the block: %+v -> %+v", before, got)
			}
			if live := s.cfg.Autonomous.ModelRouting; !reflect.DeepEqual(live, liveBefore) {
				t.Errorf("a refused PUT reached the live config: %+v -> %+v", liveBefore, live)
			}
			if s.cfg.Autonomous.SessionIdleTimeoutSeconds == 1234 {
				t.Error("a refused PUT applied another key of the same body")
			}
			if _, err := os.Stat(cfgPath); err == nil {
				t.Error("a refused PUT wrote the config file")
			}
		})
	}

	t.Run("whole block with an unknown key", func(t *testing.T) {
		s, _, _ := newMaxConcurrentTasksServer(t)
		rec := putInfraRetryRaw(s, map[string]any{
			"autonomous.model_routing": map[string]any{"refusal_mode": "warn", "colour": "red"},
		})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "autonomous.model_routing.colour") {
			t.Fatalf("want 400 naming the key, got %d: %s", rec.Code, rec.Body.String())
		}
		if got := modelRoutingBlock(t, s); got.RefusalMode != "refuse" {
			t.Errorf("a refused whole-block PUT applied refusal_mode: %+v", got)
		}
	})
	t.Run("whole block that is not an object", func(t *testing.T) {
		s, _, _ := newMaxConcurrentTasksServer(t)
		rec := putInfraRetryRaw(s, map[string]any{"autonomous.model_routing": "warn"})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "autonomous.model_routing") {
			t.Fatalf("want 400 naming the block, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestModelRoutingConfig_SurvivesSaveAndLoad(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	rec := putInfraRetryRaw(s, map[string]any{
		"autonomous.model_routing.refusal_mode":                 "warn",
		"autonomous.model_routing.served_models_cache_seconds":  0,
		"autonomous.model_routing.verifier_follows_worker_node": false,
		"autonomous.model_routing.unknown_node_policy":          "assume_not",
		"autonomous.model_routing.probe_timeout_seconds":        12,
		"autonomous.model_routing.alert_on_refusal":             false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	file, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	for _, want := range []string{"model_routing:", "refusal_mode: warn", "served_models_cache_seconds: 0",
		"verifier_follows_worker_node: false", "unknown_node_policy: assume_not",
		"probe_timeout_seconds: 12", "alert_on_refusal: false"} {
		if !strings.Contains(string(file), want) {
			t.Errorf("config.yaml lacks %q:\n%s", want, file)
		}
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	mr := loaded.Autonomous.ModelRouting
	if mr.EffectiveRefusalMode() != "warn" || mr.ServedModelsCacheSeconds == nil || *mr.ServedModelsCacheSeconds != 0 ||
		mr.EffectiveVerifierFollowsWorkerNode() || mr.EffectiveUnknownNodePolicy() != "assume_not" ||
		mr.EffectiveProbeTimeout().Seconds() != 12 || mr.EffectiveAlertOnRefusal() {
		t.Fatalf("reloaded block = %+v", mr)
	}

	// A fresh server on the reloaded config serves the same values.
	s2, _, _ := newMaxConcurrentTasksServer(t)
	s2.cfg = loaded
	want := ModelRoutingSurface{
		RefusalMode: "warn", ServedModelsCacheSeconds: 0, VerifierFollowsWorkerNode: false,
		UnknownNodePolicy: "assume_not", ProbeTimeoutSeconds: 12, AlertOnRefusal: false,
	}
	if got := modelRoutingBlock(t, s2); got != want {
		t.Fatalf("GET on the reloaded config = %+v, want %+v", got, want)
	}
}
