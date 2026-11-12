package config

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/trace"
	"gopkg.in/yaml.v3"
)

func TestTracingYAMLKeys(t *testing.T) {
	in := "tracing:\n  mode: full\n  arg_cap_bytes: 256\n  retention_days: 7\n  backends:\n    pi: false\n"
	var c Config
	if err := yaml.Unmarshal([]byte(in), &c); err != nil {
		t.Fatal(err)
	}
	if c.Tracing.Mode != "full" || c.Tracing.ArgCapBytes != 256 || c.Tracing.RetentionDays != 7 || c.Tracing.Backends["pi"] {
		t.Errorf("parsed wrong: %+v", c.Tracing)
	}
	if c.Tracing.BackendEnabled("pi") || !c.Tracing.BackendEnabled("claude-code") {
		t.Error("backend switches wrong")
	}
}

func TestTracingDefaultsWhenUnset(t *testing.T) {
	var c Config
	got := c.Tracing.WithDefaults()
	if got.Mode != "summary" || got.ArgCapBytes != trace.DefaultArgCapBytes || got.RetentionDays != trace.DefaultRetentionDays {
		t.Errorf("defaults: %+v", got)
	}
}

func TestValidateTracing(t *testing.T) {
	tests := []struct {
		name    string
		in      trace.Settings
		wantErr string
	}{
		{"zero", trace.Settings{}, ""},
		{"full", trace.Settings{Mode: "full", ArgCapBytes: 64, RetentionDays: 30}, ""},
		{"bad mode", trace.Settings{Mode: "loud"}, "tracing.mode"},
		{"neg cap", trace.Settings{ArgCapBytes: -2}, "tracing.arg_cap_bytes"},
		{"neg retention", trace.Settings{RetentionDays: -1}, "tracing.retention_days"},
		{"empty backend", trace.Settings{Backends: map[string]bool{"": true}}, "tracing.backends"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTracing(tc.in)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestTracingInAnnotatedTemplate(t *testing.T) {
	c := &Config{}
	c.Tracing = trace.Settings{Mode: "full", Backends: map[string]bool{"pi": false}}
	out := GenerateAnnotatedConfig(c)
	for _, want := range []string{"tracing:", "mode: full", "arg_cap_bytes: 512", "retention_days: 14", "pi: false"} {
		if !strings.Contains(out, want) {
			t.Errorf("template missing %q", want)
		}
	}
	var back Config
	if err := yaml.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("template does not parse: %v", err)
	}
	if back.Tracing.Mode != "full" || back.Tracing.Backends["pi"] {
		t.Errorf("round trip: %+v", back.Tracing)
	}
}
