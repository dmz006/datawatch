package config

import (
	"testing"
	"time"
)

func TestReqKeySettingDefaults(t *testing.T) {
	var c DeliveryConfig
	if c.SessionReminderLimit() != 2 || c.HostReminderLimit() != 20 || c.RequestTakeBy() != time.Hour ||
		c.RequestDue() != 24*time.Hour || c.ReminderInterval() != 15*time.Minute || len(c.RequestDueByKind()) != 0 {
		t.Fatal("defaults")
	}
	zero, below, five := 0, -3, 5
	c = DeliveryConfig{RemindersPerSessionHour: &zero, RemindersPerHostHour: &below, RequestTakeBySeconds: -1,
		RequestDueSecondsByKind: map[string]int{"ask": 60, "bug": -1}}
	if c.SessionReminderLimit() != 0 || c.HostReminderLimit() != 0 || c.RequestTakeBy() != time.Hour {
		t.Fatal("0 types no reminders; no time is open-ended")
	}
	if k := c.RequestDueByKind(); len(k) != 1 || k["ask"] != time.Minute {
		t.Fatalf("%v", k)
	}
	c.RemindersPerSessionHour = &five
	if c.SessionReminderLimit() != 5 {
		t.Fatal("set")
	}
}
