package server

import (
	"strconv"
	"strings"
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
		{"retention zero ok", map[string]interface{}{"tracing.retention_days": 0.0}, false},
		{"retention too big", map[string]interface{}{"tracing.retention_days": 99999.0}, true},
		{"retention negative", map[string]interface{}{"tracing.retention_days": -1.0}, true},
		{"retention 3650 ok", map[string]interface{}{"tracing.retention_days": 3650.0}, false},
		{"retention 3651", map[string]interface{}{"tracing.retention_days": 3651.0}, true},
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

func TestValidateTracingRetentionRefusedMessage(t *testing.T) {
	for _, v := range []int{-1, 3651} {
		err := validateTracingPatch(trace.Settings{}, map[string]interface{}{"tracing.retention_days": float64(v)})
		if err == nil {
			t.Fatalf("retention_days=%d: expected refusal", v)
		}
		if !strings.Contains(err.Error(), "tracing.retention_days must be between 0 and 3650") {
			t.Fatalf("retention_days=%d: message %q", v, err)
		}
		if !strings.Contains(err.Error(), strconv.Itoa(v)) {
			t.Fatalf("retention_days=%d: message names the rejected value? %q", v, err)
		}
		// 0 must stay accepted on the same path.
		if err := validateTracingPatch(trace.Settings{}, map[string]interface{}{"tracing.retention_days": 0.0}); err != nil {
			t.Fatalf("retention_days=0: %v", err)
		}
	}
}

// TestValidateTracingRetentionSharedPath pins the shared validation used by
// every write surface: the REST patch, the config file (config.Load) and the
// MCP/CLI config tools all run trace.Settings.Validate through
// config.ValidateTracing, so the 0 accepted / negative and >3650 refused
// behaviour holds without a per-surface special case.
func TestValidateTracingRetentionSharedPath(t *testing.T) {
	if err := config.ValidateTracing(trace.Settings{RetentionDays: 0}); err != nil {
		t.Fatalf("0 accepted: %v", err)
	}
	if err := config.ValidateTracing(trace.Settings{RetentionDays: 3650}); err != nil {
		t.Fatalf("3650 accepted: %v", err)
	}
	for _, v := range []int{-1, 3651} {
		err := config.ValidateTracing(trace.Settings{RetentionDays: v})
		if err == nil || !strings.Contains(err.Error(), "between 0 and 3650") {
			t.Fatalf("retention_days=%d: err=%v", v, err)
		}
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
