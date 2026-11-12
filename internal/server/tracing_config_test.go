package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/trace"
)

func TestValidateTracingPatch(t *testing.T) {
	cases := []struct {
		name    string
		patch   map[string]interface{}
		wantErr bool
	}{
		{"no tracing keys", map[string]interface{}{"git.push_remote": "x"}, false},
		{"mode full", map[string]interface{}{"tracing.mode": "full"}, false},
		{"mode loud", map[string]interface{}{"tracing.mode": "loud"}, true},
		{"mode not string", map[string]interface{}{"tracing.mode": 3.0}, true},
		{"cap ok", map[string]interface{}{"tracing.arg_cap_bytes": 256.0}, false},
		{"cap negative", map[string]interface{}{"tracing.arg_cap_bytes": -1.0}, true},
		{"cap fractional", map[string]interface{}{"tracing.arg_cap_bytes": 1.5}, true},
		{"retention too big", map[string]interface{}{"tracing.retention_days": 99999.0}, true},
		{"backends ok", map[string]interface{}{"tracing.backends": map[string]interface{}{"claude-code": false}}, false},
		{"backends non-bool", map[string]interface{}{"tracing.backends": map[string]interface{}{"claude-code": "no"}}, true},
		{"backend dotted ok", map[string]interface{}{"tracing.backends.pi-prompt": true}, false},
		{"backend dotted non-bool", map[string]interface{}{"tracing.backends.pi-prompt": "x"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateTracingPatch(trace.Settings{}, c.patch)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestApplyConfigPatchTracing(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"tracing.mode":           "full",
		"tracing.arg_cap_bytes":  128.0,
		"tracing.retention_days": 7.0,
		"tracing.backends.pi":    false,
	})
	if cfg.Tracing.Mode != "full" || cfg.Tracing.ArgCapBytes != 128 || cfg.Tracing.RetentionDays != 7 {
		t.Fatalf("not applied: %+v", cfg.Tracing)
	}
	if cfg.Tracing.BackendEnabled("pi") || !cfg.Tracing.BackendEnabled("claude-code") {
		t.Fatalf("backends wrong: %+v", cfg.Tracing.Backends)
	}
}
