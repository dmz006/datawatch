package config

import "testing"

func TestNormalizeSessionStartConfirmSeconds(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "zero uses default", input: 0, want: 120},
		{name: "negative uses default", input: -5, want: 120},
		{name: "configured 60 passes through", input: 60, want: 60},
		{name: "configured 300 passes through", input: 300, want: 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSessionStartConfirmSeconds(tt.input); got != tt.want {
				t.Fatalf("NormalizeSessionStartConfirmSeconds(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
