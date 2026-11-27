package config

import (
	"testing"
	"time"
)

func TestDeliveryDefaultsAndLimits(t *testing.T) {
	var c DeliveryConfig
	if !c.EnabledOn() || !c.NotifyOn() || c.Interval() != 5*time.Minute {
		t.Fatalf("defaults: %v %v %v", c.EnabledOn(), c.NotifyOn(), c.Interval())
	}
	c.IntervalSeconds = 5
	if c.Interval() != time.Minute {
		t.Fatalf("minimum: %v", c.Interval())
	}
}

// The check reads these two once, at daemon start, so a change must say it
// needs a restart. delivery.notify is read at every check.
func TestDeliveryKeysSayWhenTheyNeedARestart(t *testing.T) {
	for key, want := range map[string]bool{"delivery.enabled": true, "delivery.interval_seconds": true, "delivery.notify": false} {
		if KeyRequiresRestart(key) != want {
			t.Errorf("%s: restart=%v, want %v", key, !want, want)
		}
	}
}
