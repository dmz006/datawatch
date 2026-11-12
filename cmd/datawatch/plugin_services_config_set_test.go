// `datawatch config set plugins.services...` must reach the daemon as a
// PUT /api/config patch, accept the values the daemon accepts (scalars raw,
// the per-service mode as the quoted fallback), and report the daemon's
// refusal message when the daemon refuses a value.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// pluginServicesStubDaemon stands in for the daemon's plugins.services patch
// rules (applyConfigPatch / validatePluginServicesPatch in internal/server):
// the same bounds a config load runs, refused with 400 and a message.
func pluginServicesStubDaemon(t *testing.T) {
	t.Helper()
	var mu sync.Mutex
	svc := map[string]float64{}
	perService := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			var patch map[string]any
			if json.Unmarshal(raw, &patch) != nil {
				http.Error(w, "bad JSON", http.StatusBadRequest)
				return
			}
			for k, v := range patch {
				switch {
				case k == "plugins.services.health_interval_seconds":
					f, ok := wholeNumber(v)
					if !ok || f < 5 {
						http.Error(w, "plugins.services.health_interval_seconds: must be a whole number of seconds >= 5", http.StatusBadRequest)
						return
					}
					svc[k] = f
				case k == "plugins.services.backlog_growth_pulls":
					f, ok := wholeNumber(v)
					if !ok || f < 1 {
						http.Error(w, "plugins.services.backlog_growth_pulls: must be a whole number >= 1", http.StatusBadRequest)
						return
					}
					svc[k] = f
				case k == "plugins.services.node_load_ratio", k == "plugins.services.small_model_max_params_billions":
					f, ok := v.(float64)
					if !ok || f <= 0 {
						http.Error(w, k+": must be a number > 0", http.StatusBadRequest)
						return
					}
					svc[k] = f
				case k == "plugins.services.small_model_context_tokens":
					f, ok := wholeNumber(v)
					if !ok || f < 1 {
						http.Error(w, "plugins.services.small_model_context_tokens: must be a whole number >= 1", http.StatusBadRequest)
						return
					}
					svc[k] = f
				case strings.HasPrefix(k, "plugins.services.per_service."):
					name := strings.TrimPrefix(k, "plugins.services.per_service.")
					if name == "" {
						http.Error(w, "plugins.services.per_service: a service name is required", http.StatusBadRequest)
						return
					}
					mode, ok := v.(string)
					if !ok {
						if obj, isObj := v.(map[string]any); isObj {
							mode, _ = obj["lifecycle_mode"].(string)
						} else {
							http.Error(w, "plugins.services.per_service."+name+": attach_only or managed", http.StatusBadRequest)
							return
						}
					}
					mode = strings.ToLower(strings.TrimSpace(mode))
					if mode != "" && mode != "attach_only" && mode != "managed" {
						http.Error(w, "plugins.services.per_service."+name+": invalid lifecycle mode "+mode+" (attach_only|managed)", http.StatusBadRequest)
						return
					}
					perService[name] = mode
				default:
					http.Error(w, "unknown key "+k, http.StatusBadRequest)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"plugins": map[string]any{"services": map[string]any{
					"health_interval_seconds":         svc["plugins.services.health_interval_seconds"],
					"backlog_growth_pulls":            svc["plugins.services.backlog_growth_pulls"],
					"node_load_ratio":                 svc["plugins.services.node_load_ratio"],
					"small_model_max_params_billions": svc["plugins.services.small_model_max_params_billions"],
					"small_model_context_tokens":      svc["plugins.services.small_model_context_tokens"],
					"per_service":                     perService,
				}},
			})
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
}

func wholeNumber(v any) (float64, bool) {
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) {
		return 0, false
	}
	return f, true
}

func TestPluginServicesConfigSet_AcceptsValidValues(t *testing.T) {
	pluginServicesStubDaemon(t)
	cases := []struct{ key, value string }{
		{"plugins.services.health_interval_seconds", "30"},
		{"plugins.services.backlog_growth_pulls", "3"},
		{"plugins.services.node_load_ratio", "1.2"},
		{"plugins.services.small_model_max_params_billions", "4"},
		{"plugins.services.small_model_context_tokens", "4096"},
		{"plugins.services.per_service.my-svc", "managed"},
		{"plugins.services.per_service.other-svc", "attach_only"},
		{"plugins.services.per_service.my-svc", ""},
	}
	for _, c := range cases {
		out, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		})
		if err != nil {
			t.Fatalf("set %s=%s: %v", c.key, c.value, err)
		}
		if !strings.Contains(out, "Set "+c.key+" = "+c.value) {
			t.Errorf("set %s=%s: output %q lacks the Set confirmation", c.key, c.value, out)
		}
	}
}

func TestPluginServicesConfigSet_RefusesBadValues(t *testing.T) {
	pluginServicesStubDaemon(t)
	cases := []struct {
		key, value, want string
	}{
		{"plugins.services.health_interval_seconds", "1", ">= 5"},
		{"plugins.services.backlog_growth_pulls", "0", ">= 1"},
		{"plugins.services.node_load_ratio", "0", "> 0"},
		{"plugins.services.small_model_max_params_billions", "-2", "> 0"},
		{"plugins.services.small_model_context_tokens", "0", ">= 1"},
		{"plugins.services.per_service.my-svc", "reborn", "attach_only"},
	}
	for _, c := range cases {
		_, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s=%s: want refusal containing %q, got %v", c.key, c.value, c.want, err)
		}
	}
}

func TestPluginServicesConfigureBodyPerServiceMap(t *testing.T) {
	// A JSON object goes out as an object (the daemon applies every entry);
	// text that is not a JSON object goes out as a string for the daemon to
	// answer with its own message.
	var m map[string]any
	if err := json.Unmarshal([]byte(configureRequestBody("plugins.services.per_service", `{"my-svc":"managed"}`)), &m); err != nil {
		t.Fatal(err)
	}
	if obj, ok := m["plugins.services.per_service"].(map[string]any); !ok || obj["my-svc"] != "managed" {
		t.Errorf("body = %v, want the per-service object", m)
	}
	m = nil
	if err := json.Unmarshal([]byte(configureRequestBody("plugins.services.per_service", "my-svc=managed")), &m); err != nil {
		t.Fatal(err)
	}
	if s, ok := m["plugins.services.per_service"].(string); !ok || s != "my-svc=managed" {
		t.Errorf("unparseable text must go out as a string, got %v", m)
	}
}
