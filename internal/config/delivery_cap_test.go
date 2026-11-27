package config

import (
	"testing"
	"time"
)

func TestDeliveryIntervalNeverPassesADay(t *testing.T) {
	for in, want := range map[int]time.Duration{86400: 24 * time.Hour, 86401: 24 * time.Hour, 90000: 24 * time.Hour} {
		if got := (DeliveryConfig{IntervalSeconds: in}).Interval(); got != want {
			t.Errorf("%d: %v, want %v", in, got, want)
		}
	}
}
