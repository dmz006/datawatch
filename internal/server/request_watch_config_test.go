package server

// The autonomous.request_watch block on GET /api/config, PUT /api/config and
// the Automata settings route: the defaults, a PUT then GET round trip of every
// key through the live config and the saved file, the explicit false and empty
// list that must not collapse back to unset, whole-PUT refusal of every bad
// value (the message names the key and nothing is written), and the settings
// route.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// requestWatchDefaultsWant is the block as GET shows it with nothing set.
func requestWatchDefaultsWant() requestWatchSettings {
	return requestWatchSettings{
		Enabled: true, PollSeconds: 30, NoProgressMinutes: 30,
		ObserveWorkers: true, ObserveVerifiers: true, UseGPUSignal: true,
		GPUBusyPercent: 10, UnreachableCounts: true, RaiseAlert: true, RaiseQuestion: true,
		RemediationOptions: []string{"keep_waiting", "end_worker", "retry_other_backend", "unload_reload_model"},
	}
}

// requestWatchBlock reads autonomous.request_watch from GET /api/config.
func requestWatchBlock(t *testing.T, s *Server) requestWatchSettings {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["request_watch"]
	if !ok {
		t.Fatal("GET /api/config autonomous lacks request_watch")
	}
	var got requestWatchSettings
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode request_watch: %v (%s)", err, raw)
	}
	return got
}

func putRequestWatchRaw(s *Server, patch map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(patch)
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rec
}

func putRequestWatchSettings(s *Server, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	s.handleAutonomousConfig(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", bytes.NewReader(b)))
	return rec
}

// requestWatchSettingsBlock reads request_watch from GET /api/autonomous/config.
func requestWatchSettingsBlock(t *testing.T, s *Server) (requestWatchSettings, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleAutonomousConfig(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/autonomous/config: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode settings GET: %v", err)
	}
	raw, ok := top["request_watch"]
	if !ok {
		t.Fatalf("GET /api/autonomous/config lacks request_watch: %s", rec.Body.String())
	}
	var got requestWatchSettings
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode request_watch: %v (%s)", err, raw)
	}
	return got, string(raw)
}

// requestWatchLive is the block the watcher reads on its next tick.
func requestWatchLive(s *Server) requestWatchSettings {
	return requestWatchConfigSurface(s.cfg.Autonomous.RequestWatch)
}

func TestRequestWatchConfigGetDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	raw := getAutonomousConfigMap(t, s)["request_watch"]
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("decode request_watch: %v", err)
	}
	for _, k := range requestWatchKeys {
		short := strings.TrimPrefix(k, requestWatchKeyPrefix)
		if _, ok := keys[short]; !ok {
			t.Errorf("request_watch lacks %q: %s", short, raw)
		}
	}
	if len(keys) != 11 {
		t.Errorf("request_watch has %d keys, want 11: %s", len(keys), raw)
	}
	if got := requestWatchBlock(t, s); !reflect.DeepEqual(got, requestWatchDefaultsWant()) {
		t.Fatalf("defaults = %+v, want %+v", got, requestWatchDefaultsWant())
	}
}

// A PUT of a single key then a GET shows the new value for each of the eleven
// keys, in the live config and in the saved file, and leaves the other ten at
// their defaults.
func TestRequestWatchConfigPutGetRoundTripEveryKey(t *testing.T) {
	cases := []struct {
		key  string
		val  any
		want func(w *requestWatchSettings)
		file func(c config.RequestWatchConfig) bool
	}{
		{"enabled", false, func(w *requestWatchSettings) { w.Enabled = false },
			func(c config.RequestWatchConfig) bool { return c.Enabled != nil && !*c.Enabled }},
		{"poll_seconds", 5, func(w *requestWatchSettings) { w.PollSeconds = 5 },
			func(c config.RequestWatchConfig) bool { return c.PollSeconds == 5 }},
		{"no_progress_minutes", 45, func(w *requestWatchSettings) { w.NoProgressMinutes = 45 },
			func(c config.RequestWatchConfig) bool { return c.NoProgressMinutes == 45 }},
		{"observe_workers", false, func(w *requestWatchSettings) { w.ObserveWorkers = false },
			func(c config.RequestWatchConfig) bool { return c.ObserveWorkers != nil && !*c.ObserveWorkers }},
		{"observe_verifiers", false, func(w *requestWatchSettings) { w.ObserveVerifiers = false },
			func(c config.RequestWatchConfig) bool { return c.ObserveVerifiers != nil && !*c.ObserveVerifiers }},
		{"use_gpu_signal", false, func(w *requestWatchSettings) { w.UseGPUSignal = false },
			func(c config.RequestWatchConfig) bool { return c.UseGPUSignal != nil && !*c.UseGPUSignal }},
		{"gpu_busy_percent", 55, func(w *requestWatchSettings) { w.GPUBusyPercent = 55 },
			func(c config.RequestWatchConfig) bool { return c.GPUBusyPercent == 55 }},
		{"unreachable_counts", false, func(w *requestWatchSettings) { w.UnreachableCounts = false },
			func(c config.RequestWatchConfig) bool { return c.UnreachableCounts != nil && !*c.UnreachableCounts }},
		{"raise_alert", false, func(w *requestWatchSettings) { w.RaiseAlert = false },
			func(c config.RequestWatchConfig) bool { return c.RaiseAlert != nil && !*c.RaiseAlert }},
		{"raise_question", false, func(w *requestWatchSettings) { w.RaiseQuestion = false },
			func(c config.RequestWatchConfig) bool { return c.RaiseQuestion != nil && !*c.RaiseQuestion }},
		{"remediation_options", []any{"end_worker", "unload_reload_model"},
			func(w *requestWatchSettings) {
				w.RemediationOptions = []string{"keep_waiting", "end_worker", "unload_reload_model"}
			},
			func(c config.RequestWatchConfig) bool {
				return reflect.DeepEqual(c.RemediationOptions, []string{"end_worker", "unload_reload_model"})
			}},
	}
	if len(cases) != 11 {
		t.Fatalf("the table covers %d keys, want 11", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			s, _, cfgPath := newMaxConcurrentTasksServer(t)
			rec := putRequestWatchRaw(s, map[string]any{requestWatchKeyPrefix + tc.key: tc.val})
			if rec.Code != http.StatusOK {
				t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
			}
			want := requestWatchDefaultsWant()
			tc.want(&want)
			if got := requestWatchBlock(t, s); !reflect.DeepEqual(got, want) {
				t.Fatalf("GET after PUT = %+v, want %+v", got, want)
			}
			// The live config the watcher reads each tick follows with no restart.
			if live := requestWatchLive(s); !reflect.DeepEqual(live, want) {
				t.Fatalf("live config = %+v, want %+v", live, want)
			}
			saved, err := config.Load(cfgPath)
			if err != nil {
				t.Fatalf("load the saved config: %v", err)
			}
			if !tc.file(saved.Autonomous.RequestWatch) {
				t.Fatalf("saved file lost %s: %+v", tc.key, saved.Autonomous.RequestWatch)
			}
			if got := requestWatchConfigSurface(saved.Autonomous.RequestWatch); !reflect.DeepEqual(got, want) {
				t.Fatalf("saved file = %+v, want %+v", got, want)
			}
		})
	}
}

// All eleven keys in one PUT, then back to the other value of each.
func TestRequestWatchConfigPutAllKeysTwice(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	rec := putRequestWatchRaw(s, map[string]any{
		"autonomous.request_watch.enabled":             false,
		"autonomous.request_watch.poll_seconds":        7,
		"autonomous.request_watch.no_progress_minutes": 12,
		"autonomous.request_watch.observe_workers":     false,
		"autonomous.request_watch.observe_verifiers":   false,
		"autonomous.request_watch.use_gpu_signal":      false,
		"autonomous.request_watch.gpu_busy_percent":    80,
		"autonomous.request_watch.unreachable_counts":  false,
		"autonomous.request_watch.raise_alert":         false,
		"autonomous.request_watch.raise_question":      false,
		"autonomous.request_watch.remediation_options": []any{"retry_other_backend"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want := requestWatchSettings{PollSeconds: 7, NoProgressMinutes: 12, GPUBusyPercent: 80,
		RemediationOptions: []string{"keep_waiting", "retry_other_backend"}}
	if got := requestWatchBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after PUT = %+v, want %+v", got, want)
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load the saved config: %v", err)
	}
	if got := requestWatchConfigSurface(saved.Autonomous.RequestWatch); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved file = %+v, want %+v", got, want)
	}

	rec = putRequestWatchRaw(s, map[string]any{
		"autonomous.request_watch.enabled":             true,
		"autonomous.request_watch.poll_seconds":        "60",
		"autonomous.request_watch.no_progress_minutes": 90,
		"autonomous.request_watch.observe_workers":     "true",
		"autonomous.request_watch.observe_verifiers":   true,
		"autonomous.request_watch.use_gpu_signal":      true,
		"autonomous.request_watch.gpu_busy_percent":    100,
		"autonomous.request_watch.unreachable_counts":  true,
		"autonomous.request_watch.raise_alert":         true,
		"autonomous.request_watch.raise_question":      true,
		"autonomous.request_watch.remediation_options": "end_worker, keep_waiting",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("second PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want = requestWatchSettings{Enabled: true, PollSeconds: 60, NoProgressMinutes: 90,
		ObserveWorkers: true, ObserveVerifiers: true, UseGPUSignal: true, GPUBusyPercent: 100,
		UnreachableCounts: true, RaiseAlert: true, RaiseQuestion: true,
		RemediationOptions: []string{"keep_waiting", "end_worker"}}
	if got := requestWatchBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after the second PUT = %+v, want %+v", got, want)
	}
}

func TestRequestWatchConfigPutOneKeyLeavesTheRest(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if rec := putRequestWatchRaw(s, map[string]any{
		"autonomous.request_watch.poll_seconds":        4,
		"autonomous.request_watch.remediation_options": []any{"end_worker"},
	}); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if rec := putRequestWatchRaw(s, map[string]any{"autonomous.request_watch.enabled": false}); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	got := requestWatchBlock(t, s)
	if got.Enabled || got.PollSeconds != 4 || !reflect.DeepEqual(got.RemediationOptions, []string{"keep_waiting", "end_worker"}) {
		t.Fatalf("a PUT of one key changed another: %+v", got)
	}
	if got.NoProgressMinutes != 30 || got.GPUBusyPercent != 10 || !got.RaiseQuestion {
		t.Fatalf("a key that was never set lost its default: %+v", got)
	}
}

// An unset key reads its default: a PUT that sets one key leaves the others
// unset in the live config and out of the saved file.
func TestRequestWatchConfigUnsetKeyReadsItsDefault(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	if rec := putRequestWatchRaw(s, map[string]any{"autonomous.request_watch.poll_seconds": 9}); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	live := s.cfg.Autonomous.RequestWatch
	if live.Enabled != nil || live.NoProgressMinutes != 0 || live.RemediationOptions != nil || live.RaiseAlert != nil {
		t.Fatalf("a key that was never written is no longer unset: %+v", live)
	}
	if live.EffectiveNoProgressMinutes() != 30 || !live.EffectiveEnabled() || len(live.EffectiveRemediationOptions()) != 4 {
		t.Fatalf("unset keys do not read their defaults: %+v", live)
	}
	file, _ := os.ReadFile(cfgPath)
	for _, absent := range []string{"no_progress_minutes", "remediation_options", "raise_alert"} {
		if strings.Contains(string(file), absent) {
			t.Errorf("an unset key %q was written to the saved file:\n%s", absent, file)
		}
	}
	if !strings.Contains(string(file), "poll_seconds: 9") {
		t.Errorf("the saved file lacks poll_seconds: 9:\n%s", file)
	}
}

// An explicit false and an explicit empty list are not the unset value: they
// must read back as false and keep_waiting only from GET, in the live config
// and from the saved file, and must not fall back to the defaults.
func TestRequestWatchConfigExplicitFalseAndEmptyListSurvive(t *testing.T) {
	for name, empty := range map[string]any{"empty array": []any{}, "empty text": ""} {
		t.Run(name, func(t *testing.T) {
			s, _, cfgPath := newMaxConcurrentTasksServer(t)
			rec := putRequestWatchRaw(s, map[string]any{
				"autonomous.request_watch.enabled":             false,
				"autonomous.request_watch.observe_workers":     false,
				"autonomous.request_watch.observe_verifiers":   false,
				"autonomous.request_watch.use_gpu_signal":      false,
				"autonomous.request_watch.unreachable_counts":  false,
				"autonomous.request_watch.raise_alert":         false,
				"autonomous.request_watch.raise_question":      false,
				"autonomous.request_watch.remediation_options": empty,
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
			}
			got := requestWatchBlock(t, s)
			if got.Enabled || got.ObserveWorkers || got.ObserveVerifiers || got.UseGPUSignal ||
				got.UnreachableCounts || got.RaiseAlert || got.RaiseQuestion {
				t.Errorf("an explicit false read back as true: %+v", got)
			}
			if !reflect.DeepEqual(got.RemediationOptions, []string{"keep_waiting"}) {
				t.Errorf("an explicit empty list read back as %v, want [keep_waiting]", got.RemediationOptions)
			}
			live := s.cfg.Autonomous.RequestWatch
			if live.RemediationOptions == nil || len(live.RemediationOptions) != 0 {
				t.Errorf("live list = %v, want empty and not nil", live.RemediationOptions)
			}
			if live.EffectiveEnabled() || live.EffectiveRaiseAlert() || live.EffectiveRaiseQuestion() {
				t.Errorf("live config fell back to a default: %+v", live)
			}

			saved, err := config.Load(cfgPath)
			if err != nil {
				t.Fatalf("load the saved config: %v", err)
			}
			sv := saved.Autonomous.RequestWatch
			if sv.RemediationOptions == nil || len(sv.RemediationOptions) != 0 {
				t.Errorf("saved list = %v, want empty and not nil", sv.RemediationOptions)
			}
			if sv.EffectiveEnabled() || sv.EffectiveObserveWorkers() || sv.EffectiveRaiseQuestion() {
				t.Errorf("saved file lost an explicit false: %+v", sv)
			}
			file, _ := os.ReadFile(cfgPath)
			if !strings.Contains(string(file), "remediation_options: []") {
				t.Errorf("saved file does not hold the empty list:\n%s", file)
			}
		})
	}
}

func TestRequestWatchConfigNullClearsTheListToUnset(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putRequestWatchRaw(s, map[string]any{"autonomous.request_watch.remediation_options": []any{}})
	rec := putRequestWatchRaw(s, map[string]any{"autonomous.request_watch.remediation_options": nil})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT null: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := requestWatchBlock(t, s); len(got.RemediationOptions) != 4 {
		t.Fatalf("null did not restore all four options: %+v", got)
	}
}

// requestWatchRefused sends a PUT with a good key and a bad one, then checks
// the answer is 400, the message names the bad key and the allowed values, and
// nothing was written: not the bad key, not the good one, and not the file.
func requestWatchRefused(t *testing.T, key string, val any, wantInMessage ...string) {
	t.Helper()
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	rec := putRequestWatchRaw(s, map[string]any{
		requestWatchKeyPrefix + key:                    val,
		"autonomous.request_watch.no_progress_minutes": 99,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT %s=%v: want 400, got %d: %s", key, val, rec.Code, rec.Body.String())
	}
	msg := rec.Body.String()
	if !strings.Contains(msg, requestWatchKeyPrefix+key) {
		t.Errorf("message %q does not name the key %s", msg, key)
	}
	for _, w := range wantInMessage {
		if !strings.Contains(msg, w) {
			t.Errorf("message %q does not name the allowed values (%q)", msg, w)
		}
	}
	if got := requestWatchBlock(t, s); !reflect.DeepEqual(got, requestWatchDefaultsWant()) {
		t.Errorf("a refused PUT changed the block: %+v", got)
	}
	if !s.cfg.Autonomous.RequestWatch.IsZero() {
		t.Errorf("a refused PUT changed the live config: %+v", s.cfg.Autonomous.RequestWatch)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Errorf("a refused PUT wrote the config file")
	}
}

func TestRequestWatchConfigRejectPollSecondsBelowOne(t *testing.T) {
	for _, v := range []any{0, -5, 2.5, "soon", true, 86401} {
		requestWatchRefused(t, "poll_seconds", v, "whole number", "1")
	}
}

func TestRequestWatchConfigRejectNoProgressMinutesBelowOne(t *testing.T) {
	for _, v := range []any{0, -1, 1.5, "long", false, 10081} {
		s, _, cfgPath := newMaxConcurrentTasksServer(t)
		rec := putRequestWatchRaw(s, map[string]any{
			"autonomous.request_watch.no_progress_minutes": v,
			"autonomous.request_watch.poll_seconds":        12,
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT no_progress_minutes=%v: want 400, got %d: %s", v, rec.Code, rec.Body.String())
		}
		if msg := rec.Body.String(); !strings.Contains(msg, "autonomous.request_watch.no_progress_minutes") || !strings.Contains(msg, "whole number") {
			t.Errorf("message %q does not name the key and the allowed values", msg)
		}
		if got := requestWatchBlock(t, s); !reflect.DeepEqual(got, requestWatchDefaultsWant()) {
			t.Errorf("a refused PUT changed the block: %+v", got)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Errorf("a refused PUT wrote the config file")
		}
	}
}

func TestRequestWatchConfigRejectGPUBusyPercentOutsideRange(t *testing.T) {
	for _, v := range []any{0, -1, 101, 1000, 55.5, "busy", true} {
		requestWatchRefused(t, "gpu_busy_percent", v, "1 to 100")
	}
}

func TestRequestWatchConfigRejectUnknownRemediationOption(t *testing.T) {
	for _, v := range []any{
		[]any{"end_worker", "reboot_node"},
		"keep_waiting,bogus",
		[]any{3},
		42,
		true,
	} {
		requestWatchRefused(t, "remediation_options", v, "keep_waiting", "end_worker", "retry_other_backend", "unload_reload_model")
	}
}

func TestRequestWatchConfigRejectNonBooleanFlag(t *testing.T) {
	for _, key := range []string{"enabled", "observe_workers", "observe_verifiers", "use_gpu_signal",
		"unreachable_counts", "raise_alert", "raise_question"} {
		for _, v := range []any{"maybe", 1, 0, []any{true}} {
			requestWatchRefused(t, key, v, "true", "false")
		}
	}
}

func TestRequestWatchConfigBooleanTextFormsAccepted(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	rec := putRequestWatchRaw(s, map[string]any{
		"autonomous.request_watch.enabled":          "false",
		"autonomous.request_watch.poll_seconds":     "6",
		"autonomous.request_watch.raise_alert":      "TRUE",
		"autonomous.request_watch.raise_question":   false,
		"autonomous.request_watch.observe_workers":  true,
		"autonomous.request_watch.use_gpu_signal":   "false",
		"autonomous.request_watch.gpu_busy_percent": "20",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT text forms: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := requestWatchBlock(t, s)
	if got.Enabled || got.PollSeconds != 6 || !got.RaiseAlert || got.RaiseQuestion || got.UseGPUSignal || got.GPUBusyPercent != 20 {
		t.Fatalf("text forms = %+v", got)
	}
}

// The Automata settings route takes a request_watch object, shows it in GET
// with the defaults filled in, applies it live, and persists it.
func TestRequestWatchConfigSettingsRouteRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)

	got, raw := requestWatchSettingsBlock(t, s)
	if !reflect.DeepEqual(got, requestWatchDefaultsWant()) {
		t.Fatalf("settings GET defaults = %+v, want %+v", got, requestWatchDefaultsWant())
	}
	if !strings.Contains(raw, `"remediation_options":["keep_waiting"`) {
		t.Errorf("settings GET does not show the options in effect: %s", raw)
	}

	rec := putRequestWatchSettings(s, map[string]any{"request_watch": map[string]any{
		"enabled":             false,
		"poll_seconds":        8,
		"no_progress_minutes": 20,
		"observe_workers":     true,
		"observe_verifiers":   false,
		"use_gpu_signal":      false,
		"gpu_busy_percent":    40,
		"unreachable_counts":  false,
		"raise_alert":         false,
		"raise_question":      true,
		"remediation_options": []any{"unload_reload_model"},
	}})
	if rec.Code != http.StatusOK {
		t.Fatalf("settings PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want := requestWatchSettings{
		Enabled: false, PollSeconds: 8, NoProgressMinutes: 20,
		ObserveWorkers: true, ObserveVerifiers: false, UseGPUSignal: false,
		GPUBusyPercent: 40, UnreachableCounts: false, RaiseAlert: false, RaiseQuestion: true,
		RemediationOptions: []string{"keep_waiting", "unload_reload_model"},
	}
	if got, _ := requestWatchSettingsBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("settings GET after PUT = %+v, want %+v", got, want)
	}
	// The manager, the daemon config the watcher reads, GET /api/config and the
	// saved file all agree.
	if live := requestWatchConfigSurface(mgr.Config().RequestWatch); !reflect.DeepEqual(live, want) {
		t.Fatalf("manager = %+v, want %+v", live, want)
	}
	if live := requestWatchLive(s); !reflect.DeepEqual(live, want) {
		t.Fatalf("daemon config = %+v, want %+v", live, want)
	}
	if got := requestWatchBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET /api/config = %+v, want %+v", got, want)
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load the saved config: %v", err)
	}
	if got := requestWatchConfigSurface(saved.Autonomous.RequestWatch); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved file = %+v, want %+v", got, want)
	}

	// A partial object changes only the keys it names; an explicit empty list
	// and an explicit false survive.
	rec = putRequestWatchSettings(s, map[string]any{"request_watch": map[string]any{
		"remediation_options": []any{},
		"enabled":             true,
	}})
	if rec.Code != http.StatusOK {
		t.Fatalf("second settings PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want.Enabled = true
	want.RemediationOptions = []string{"keep_waiting"}
	if got, _ := requestWatchSettingsBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("settings GET after the partial PUT = %+v, want %+v", got, want)
	}
	if live := requestWatchLive(s); !reflect.DeepEqual(live, want) {
		t.Fatalf("daemon config after the partial PUT = %+v, want %+v", live, want)
	}
	if live := s.cfg.Autonomous.RequestWatch; live.RemediationOptions == nil || live.EffectiveRaiseAlert() {
		t.Fatalf("explicit empty list or false lost: %+v", live)
	}
	saved, _ = config.Load(cfgPath)
	if sv := saved.Autonomous.RequestWatch; sv.RemediationOptions == nil || len(sv.RemediationOptions) != 0 || sv.EffectiveRaiseAlert() {
		t.Fatalf("saved file lost the explicit empty list or false: %+v", sv)
	}
}

// The settings route refuses a bad value with 400 naming the key and the
// allowed values, and writes nothing.
func TestRequestWatchConfigSettingsRouteRejectInvalid(t *testing.T) {
	cases := []struct {
		name string
		obj  map[string]any
		want []string
	}{
		{"poll_seconds zero", map[string]any{"poll_seconds": 0}, []string{"autonomous.request_watch.poll_seconds", "1"}},
		{"poll_seconds text", map[string]any{"poll_seconds": "fast"}, []string{"autonomous.request_watch.poll_seconds"}},
		{"no_progress_minutes zero", map[string]any{"no_progress_minutes": 0}, []string{"autonomous.request_watch.no_progress_minutes", "1"}},
		{"gpu_busy_percent high", map[string]any{"gpu_busy_percent": 101}, []string{"autonomous.request_watch.gpu_busy_percent", "1 to 100"}},
		{"gpu_busy_percent zero", map[string]any{"gpu_busy_percent": 0}, []string{"autonomous.request_watch.gpu_busy_percent", "1 to 100"}},
		{"unknown option", map[string]any{"remediation_options": []any{"reboot_node"}}, []string{"autonomous.request_watch.remediation_options", "end_worker"}},
		{"options not a list", map[string]any{"remediation_options": "end_worker"}, []string{"autonomous.request_watch.remediation_options"}},
		{"flag text", map[string]any{"raise_alert": "yes"}, []string{"autonomous.request_watch.raise_alert", "true, false"}},
		{"flag number", map[string]any{"enabled": 1}, []string{"autonomous.request_watch.enabled", "true, false"}},
		{"unknown key", map[string]any{"poll_second": 5}, []string{"autonomous.request_watch.poll_second", "poll_seconds"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
			body := map[string]any{"request_watch": mergeObj(tc.obj, map[string]any{"no_progress_minutes": 77})}
			if _, set := tc.obj["no_progress_minutes"]; set {
				body = map[string]any{"request_watch": tc.obj}
			}
			rec := putRequestWatchSettings(s, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("settings PUT: want 400, got %d: %s", rec.Code, rec.Body.String())
			}
			for _, w := range tc.want {
				if !strings.Contains(rec.Body.String(), w) {
					t.Errorf("message %q lacks %q", rec.Body.String(), w)
				}
			}
			if got, _ := requestWatchSettingsBlock(t, s); !reflect.DeepEqual(got, requestWatchDefaultsWant()) {
				t.Errorf("a refused PUT changed the block: %+v", got)
			}
			if !mgr.Config().RequestWatch.IsZero() || !s.cfg.Autonomous.RequestWatch.IsZero() {
				t.Errorf("a refused PUT changed the live config: mgr=%+v cfg=%+v", mgr.Config().RequestWatch, s.cfg.Autonomous.RequestWatch)
			}
			if _, err := os.Stat(cfgPath); err == nil {
				t.Errorf("a refused PUT wrote the config file")
			}
		})
	}
}

func mergeObj(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range b {
		out[k] = v
	}
	for k, v := range a {
		out[k] = v
	}
	return out
}

// A write through the manager itself (the path MCP and the comm channel use)
// is refused with the same rules.
func TestRequestWatchConfigManagerRefusesInvalid(t *testing.T) {
	_, mgr, _ := newMaxConcurrentTasksServer(t)
	api := newTestAPI(mgr)
	err := api.SetConfig(json.RawMessage(`{"request_watch":{"gpu_busy_percent":500}}`))
	if err == nil || !strings.Contains(err.Error(), "autonomous.request_watch.gpu_busy_percent") {
		t.Fatalf("want an error naming gpu_busy_percent, got %v", err)
	}
	if !mgr.Config().RequestWatch.IsZero() {
		t.Fatalf("a refused write changed the manager: %+v", mgr.Config().RequestWatch)
	}
}
