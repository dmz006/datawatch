package config

import (
	"testing"
	"time"
)

func TestVisionConfig_EffectiveDescribeTimeout(t *testing.T) {
	tests := []struct {
		name string
		sec  int
		want time.Duration
	}{
		{name: "unset defaults to 5s", sec: 0, want: 5 * time.Second},
		{name: "zero defaults to 5s", sec: 0, want: DefaultVisionDescribeTimeout},
		{name: "negative falls back to default", sec: -3, want: 5 * time.Second},
		{name: "large negative falls back to default", sec: -1 << 20, want: 5 * time.Second},
		{name: "one second", sec: 1, want: time.Second},
		{name: "thirty seconds", sec: 30, want: 30 * time.Second},
		{name: "long timeout", sec: 300, want: 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := VisionConfig{DescribeTimeoutSeconds: tt.sec}
			if got := v.EffectiveDescribeTimeout(); got != tt.want {
				t.Fatalf("EffectiveDescribeTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}
