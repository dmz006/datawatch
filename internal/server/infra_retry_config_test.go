package server

// The autonomous.infra_retry block on GET /api/config and PUT /api/config:
// the defaults, a round trip of every key through the live manager and the
// saved file, whole-PUT refusal of every bad value, and the explicit false and
// empty list that must not collapse back to unset.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// infraRetryBlock reads autonomous.infra_retry from GET /api/config.
func infraRetryBlock(t *testing.T, s *Server) autonomous.InfraRetrySettings {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["infra_retry"]
	if !ok {
		t.Fatal("GET /api/config autonomous lacks infra_retry")
	}
	var got autonomous.InfraRetrySettings
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode infra_retry: %v (%s)", err, raw)
	}
	return got
}

func putInfraRetryRaw(s *Server, patch map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(patch)
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rec
}

func TestInfraRetryConfigGetDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	raw := getAutonomousConfigMap(t, s)["infra_retry"]
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("decode infra_retry: %v", err)
	}
	for _, k := range []string{"enabled", "max_tries", "backoff_seconds", "categories", "exhausted_action", "retry_dependents", "raise_question"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("infra_retry lacks %q: %s", k, raw)
		}
	}
	if len(keys) != 7 {
		t.Errorf("infra_retry has %d keys, want 7: %s", len(keys), raw)
	}
	want := autonomous.InfraRetrySettings{
		Enabled: true, MaxTries: 3, BackoffSeconds: []int{60, 300, 540},
		Categories:      config.InfraRetryAllClasses(),
		ExhaustedAction: "hold", RetryDependents: true, RaiseQuestion: true,
	}
	if got := infraRetryBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
}

func TestInfraRetryConfigPutRoundTripEveryKey(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	rec := putInfraRetryRaw(s, map[string]any{
		"autonomous.infra_retry.enabled":          false,
		"autonomous.infra_retry.max_tries":        5,
		"autonomous.infra_retry.backoff_seconds":  []any{10, 20, 30},
		"autonomous.infra_retry.categories":       []any{"spawn_error", "daemon_restart"},
		"autonomous.infra_retry.exhausted_action": "count",
		"autonomous.infra_retry.retry_dependents": false,
		"autonomous.infra_retry.raise_question":   false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want := autonomous.InfraRetrySettings{
		Enabled: false, MaxTries: 5, BackoffSeconds: []int{10, 20, 30},
		Categories:      []string{"spawn_error", "daemon_restart"},
		ExhaustedAction: "count", RetryDependents: false, RaiseQuestion: false,
	}
	if got := infraRetryBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after PUT = %+v, want %+v", got, want)
	}
	// The live manager follows with no restart.
	if live := autonomous.InfraRetrySettingsFrom(mgr.Config().InfraRetry); !reflect.DeepEqual(live, want) {
		t.Fatalf("live manager = %+v, want %+v", live, want)
	}
	// The saved file carries the same values.
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load the saved config: %v", err)
	}
	if got := autonomous.InfraRetrySettingsFrom(saved.Autonomous.InfraRetry); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved file = %+v, want %+v", got, want)
	}

	// And back to the other value of every flag and list.
	rec = putInfraRetryRaw(s, map[string]any{
		"autonomous.infra_retry.enabled":          true,
		"autonomous.infra_retry.max_tries":        2,
		"autonomous.infra_retry.backoff_seconds":  "5,15",
		"autonomous.infra_retry.categories":       "storage_write, session_killed_before_delivery",
		"autonomous.infra_retry.exhausted_action": "hold",
		"autonomous.infra_retry.retry_dependents": true,
		"autonomous.infra_retry.raise_question":   true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("second PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want = autonomous.InfraRetrySettings{
		Enabled: true, MaxTries: 2, BackoffSeconds: []int{5, 15},
		Categories:      []string{"storage_write", "session_killed_before_delivery"},
		ExhaustedAction: "hold", RetryDependents: true, RaiseQuestion: true,
	}
	if got := infraRetryBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after the second PUT = %+v, want %+v", got, want)
	}
	if live := autonomous.InfraRetrySettingsFrom(mgr.Config().InfraRetry); !reflect.DeepEqual(live, want) {
		t.Fatalf("live manager after the second PUT = %+v, want %+v", live, want)
	}
}

func TestInfraRetryConfigPutOneKeyLeavesTheRest(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	if rec := putInfraRetryRaw(s, map[string]any{
		"autonomous.infra_retry.max_tries":       4,
		"autonomous.infra_retry.backoff_seconds": []any{1, 2},
	}); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if rec := putInfraRetryRaw(s, map[string]any{"autonomous.infra_retry.enabled": false}); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	got := infraRetryBlock(t, s)
	if got.Enabled || got.MaxTries != 4 || !reflect.DeepEqual(got.BackoffSeconds, []int{1, 2}) {
		t.Fatalf("a PUT of one key changed another: %+v", got)
	}
	if live := mgr.Config().InfraRetry; live.EffectiveEnabled() || live.EffectiveMaxTries() != 4 {
		t.Fatalf("live manager = %+v", live)
	}
}

// An explicit false and an explicit empty list are not the unset value: they
// must read back as false and [] from GET, in the live manager and from the
// saved file, and must not fall back to the defaults.
func TestInfraRetryConfigExplicitFalseAndEmptyListsSurvive(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	for name, empty := range map[string]any{"empty array": []any{}, "empty text": ""} {
		t.Run(name, func(t *testing.T) {
			rec := putInfraRetryRaw(s, map[string]any{
				"autonomous.infra_retry.enabled":          false,
				"autonomous.infra_retry.retry_dependents": false,
				"autonomous.infra_retry.raise_question":   false,
				"autonomous.infra_retry.categories":       empty,
				"autonomous.infra_retry.backoff_seconds":  empty,
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
			}
			got := infraRetryBlock(t, s)
			if got.Enabled || got.RetryDependents || got.RaiseQuestion {
				t.Errorf("an explicit false read back as true: %+v", got)
			}
			if got.Categories == nil || len(got.Categories) != 0 || got.BackoffSeconds == nil || len(got.BackoffSeconds) != 0 {
				t.Errorf("an explicit empty list read back as %v / %v, want [] and []", got.Categories, got.BackoffSeconds)
			}
			raw := string(getAutonomousConfigMap(t, s)["infra_retry"])
			if !strings.Contains(raw, `"categories":[]`) || !strings.Contains(raw, `"backoff_seconds":[]`) {
				t.Errorf("GET JSON = %s, want the empty lists encoded as []", raw)
			}

			live := mgr.Config().InfraRetry
			if live.Categories == nil || len(live.Categories) != 0 || live.BackoffSeconds == nil || len(live.BackoffSeconds) != 0 {
				t.Errorf("live manager lists = %v / %v, want empty and not nil", live.Categories, live.BackoffSeconds)
			}
			if live.EffectiveEnabled() || live.EffectiveRetryDependents() || live.EffectiveRaiseQuestion() || live.HasCategory(config.InfraRetryClassSpawnError) {
				t.Errorf("live manager fell back to a default: %+v", live)
			}

			saved, err := config.Load(cfgPath)
			if err != nil {
				t.Fatalf("load the saved config: %v", err)
			}
			sv := saved.Autonomous.InfraRetry
			if sv.Categories == nil || sv.BackoffSeconds == nil || sv.EffectiveEnabled() || sv.EffectiveRetryDependents() || sv.EffectiveRaiseQuestion() {
				t.Errorf("saved file lost an explicit value: %+v", sv)
			}
			file, _ := os.ReadFile(cfgPath)
			if !strings.Contains(string(file), "categories: []") {
				t.Errorf("saved file does not hold the empty categories list:\n%s", file)
			}
		})
	}
}

func TestInfraRetryConfigNullClearsAListToUnset(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putInfraRetryRaw(s, map[string]any{
		"autonomous.infra_retry.categories":      []any{},
		"autonomous.infra_retry.backoff_seconds": []any{},
	})
	rec := putInfraRetryRaw(s, map[string]any{
		"autonomous.infra_retry.categories":      nil,
		"autonomous.infra_retry.backoff_seconds": nil,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT null: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := infraRetryBlock(t, s)
	if len(got.Categories) != 4 || !reflect.DeepEqual(got.BackoffSeconds, []int{60, 300, 540}) {
		t.Fatalf("null did not restore the defaults: %+v", got)
	}
}

func TestInfraRetryConfigTextFormsOfFlags(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	rec := putInfraRetryRaw(s, map[string]any{
		"autonomous.infra_retry.enabled":   "false",
		"autonomous.infra_retry.max_tries": "6",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT text forms: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := infraRetryBlock(t, s); got.Enabled || got.MaxTries != 6 {
		t.Fatalf("text forms = %+v, want enabled false and max_tries 6", got)
	}
}

// Every bad value is refused with 400 naming its key, and a good key sent in
// the same PUT is not applied either.
func TestInfraRetryConfigInvalidValuesRefuseWholePut(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  any
	}{
		{"enabled text", "enabled", "maybe"},
		{"enabled number", "enabled", 1},
		{"max_tries zero", "max_tries", 0},
		{"max_tries negative", "max_tries", -2},
		{"max_tries fraction", "max_tries", 2.5},
		{"max_tries too large", "max_tries", 1001},
		{"max_tries text", "max_tries", "many"},
		{"max_tries boolean", "max_tries", true},
		{"backoff negative", "backoff_seconds", []any{60, -1}},
		{"backoff fraction", "backoff_seconds", []any{1.5}},
		{"backoff text entry", "backoff_seconds", "60,soon"},
		{"backoff word entry", "backoff_seconds", []any{"soon"}},
		{"backoff too long a delay", "backoff_seconds", []any{604801}},
		{"backoff number", "backoff_seconds", 60},
		{"backoff boolean", "backoff_seconds", true},
		{"categories unknown", "categories", []any{"spawn_error", "disk_on_fire"}},
		{"categories unknown text", "categories", "spawn_error,bogus"},
		{"categories empty entry", "categories", "spawn_error,,daemon_restart"},
		{"categories number entry", "categories", []any{1}},
		{"categories number", "categories", 4},
		{"exhausted_action unknown", "exhausted_action", "drop"},
		{"exhausted_action number", "exhausted_action", 5},
		{"retry_dependents number", "retry_dependents", 1},
		{"raise_question text", "raise_question", "yes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, mgr, _ := newMaxConcurrentTasksServer(t)
			before := infraRetryBlock(t, s)
			liveBefore := mgr.Config().InfraRetry.Clone()
			key := infraRetryKeyPrefix + c.key

			// A good key of the block in the same PUT must not be applied.
			good := map[string]any{
				infraRetryKeyPrefix + "max_tries":        9,
				infraRetryKeyPrefix + "exhausted_action": "count",
			}
			delete(good, key)
			patch := map[string]any{key: c.val, "autonomous.session_idle_timeout_seconds": 1234}
			for k, v := range good {
				patch[k] = v
			}
			rec := putInfraRetryRaw(s, patch)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), key) {
				t.Errorf("message %q does not name %s", rec.Body.String(), key)
			}
			if got := infraRetryBlock(t, s); !reflect.DeepEqual(got, before) {
				t.Errorf("a refused PUT changed the block: %+v -> %+v", before, got)
			}
			if live := mgr.Config().InfraRetry; !reflect.DeepEqual(live, liveBefore) {
				t.Errorf("a refused PUT reached the live manager: %+v -> %+v", liveBefore, live)
			}
			if s.cfg.Autonomous.SessionIdleTimeoutSeconds == 1234 {
				t.Error("a refused PUT applied another key of the same body")
			}
		})
	}
}

func TestInfraRetryConfigMessageListsValidClasses(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	rec := putInfraRetryRaw(s, map[string]any{"autonomous.infra_retry.categories": []any{"nope"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	for _, class := range config.InfraRetryAllClasses() {
		if !strings.Contains(rec.Body.String(), class) {
			t.Errorf("message %q does not list the valid class %s", rec.Body.String(), class)
		}
	}
}
