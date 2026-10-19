package config

import "testing"

func TestNormalizeSessionIdleTimeoutSeconds(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "zero uses default", input: 0, want: 900},
		{name: "negative uses default", input: -5, want: 900},
		{name: "configured 300 passes through", input: 300, want: 300},
		{name: "configured 3600 passes through", input: 3600, want: 3600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSessionIdleTimeoutSeconds(tt.input); got != tt.want {
				t.Fatalf("NormalizeSessionIdleTimeoutSeconds(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
