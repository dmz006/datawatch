package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNormalizeDescendantStallSeconds(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "zero uses default", input: 0, want: 2700},
		{name: "negative uses default", input: -5, want: 2700},
		{name: "configured 600 passes through", input: 600, want: 600},
		{name: "configured 10800 passes through", input: 10800, want: 10800},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeDescendantStallSeconds(tt.input); got != tt.want {
				t.Fatalf("NormalizeDescendantStallSeconds(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestDescendantStallSecondsYAMLRoundTrip(t *testing.T) {
	in := Config{Autonomous: AutonomousConfig{DescendantStallSeconds: 1800}}
	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	var out Config
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if out.Autonomous.DescendantStallSeconds != 1800 {
		t.Fatalf("round trip: DescendantStallSeconds = %d, want 1800", out.Autonomous.DescendantStallSeconds)
	}
}
