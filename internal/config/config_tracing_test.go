package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadTracingYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestTracingLoadValidation(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"unset", "", ""},
		{"full", "tracing:\n  mode: full\n", ""},
		{"off with backends", "tracing:\n  mode: \"off\"\n  backends:\n    pi: false\n", ""},
		{"all knobs", "tracing:\n  mode: summary\n  arg_cap_bytes: 256\n  retention_days: 30\n", ""},
		{"bad mode", "tracing:\n  mode: loud\n", "tracing.mode"},
		{"negative cap", "tracing:\n  arg_cap_bytes: -5\n", "tracing.arg_cap_bytes"},
		{"oversized retention", "tracing:\n  retention_days: 99999\n", "tracing.retention_days"},
		{"empty backend name", "tracing:\n  backends:\n    \"\": true\n", "tracing.backends"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadTracingYAML(t, c.yaml)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, c.wantErr)
			}
		})
	}
}

func TestTracingLoadErrorNamesAllowedModes(t *testing.T) {
	_, err := loadTracingYAML(t, "tracing:\n  mode: loud\n")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, m := range []string{"off", "summary", "full"} {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("error %q does not name %q", err, m)
		}
	}
}
