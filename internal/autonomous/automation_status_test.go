package autonomous

import (
	"testing"
	"time"
)

func setIdle(t *testing.T, h *coordHarness, a *Automation, status AutomationStatus, since *time.Time) {
	t.Helper()
	_, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = status
		a.IdleSince = since
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAutomationStatusSummaryFields(t *testing.T) {
	h := newCoordHarness(t, 3)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}, {"m2", "", ""}}, 3))
	st := h.m.Status()
	if len(st.Automations) != 1 {
		t.Fatalf("automations=%d", len(st.Automations))
	}
	s := st.Automations[0]
	if s.ID != a.ID || s.Name != "chain" || s.Status != string(AutomationActive) {
		t.Fatalf("summary=%+v", s)
	}
	if s.CurrentMemberID != "m0" || s.CurrentPRDID != h.prds[0].ID || s.CurrentPRDTitle != "alpha" {
		t.Fatalf("cursor fields=%+v", s)
	}
	if s.PendingMembers != 2 || s.IdleSince != nil || s.IdleSeconds != 0 {
		t.Fatalf("pending/idle=%+v", s)
	}
	if s.SpentUSD != 0 || s.CeilingDisplay != "" {
		t.Fatalf("budget fields set: %+v", s)
	}
}

func TestAutomationStatusIdleSecondsAndBlockedReason(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	since := time.Now().Add(-90 * time.Second)
	_, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = AutomationBlocked
		a.IdleSince = &since
		a.Decisions = append(a.Decisions,
			Decision{At: since, Kind: "blocked", Note: "old reason"},
			Decision{At: since, Kind: "advance", Note: "ignored"},
			Decision{At: since, Kind: "blocked", Note: "newest reason"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := h.m.Status().Automations[0]
	if s.IdleSeconds < 90 || s.IdleSeconds > 95 {
		t.Fatalf("idle seconds=%d", s.IdleSeconds)
	}
	if s.BlockedReason != "newest reason" {
		t.Fatalf("blocked reason=%q", s.BlockedReason)
	}
}

func TestAutomationStatusOmitsTerminal(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto(nil, 1))
	b, err := h.m.CreateAutomation(AutomationInput{Name: "second", Members: []AutomationMember{{ID: "x0", PRDID: h.prds[1].ID}}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	setIdle(t, h, a, AutomationCompleted, nil)
	setIdle(t, h, b, AutomationCancelled, nil)
	if got := h.m.Status().Automations; len(got) != 0 {
		t.Fatalf("terminal automations listed: %+v", got)
	}
}

// idleHarness returns a harness whose coordinator clock is controllable and
// whose threshold is minutes.
func idleHarness(t *testing.T, minutes int) (*coordHarness, *time.Time) {
	t.Helper()
	h := newCoordHarness(t, 1)
	cfg := h.m.Config()
	cfg.Automation.IdleAlertMinutes = minutes
	h.m.SetConfig(cfg)
	now := time.Now()
	h.c.now = func() time.Time { return now }
	return h, &now
}

func TestAutomationIdleOneAlertPerEpisode(t *testing.T) {
	h, now := idleHarness(t, 10)
	a := h.force(h.makeAuto(nil, 1))
	since := *now
	setIdle(t, h, a, AutomationActive, &since)

	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 0 {
		t.Fatalf("alert before threshold: %d", n)
	}
	*now = since.Add(11 * time.Minute)
	h.c.checkIdle()
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 1 {
		t.Fatalf("alerts in one episode=%d", n)
	}

	// Advancing clears the episode; a later idle period alerts again.
	setIdle(t, h, a, AutomationActive, nil)
	h.c.checkIdle()
	since2 := *now
	setIdle(t, h, a, AutomationActive, &since2)
	*now = since2.Add(11 * time.Minute)
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 2 {
		t.Fatalf("alerts after second episode=%d", n)
	}
}

func TestAutomationIdleNoAlertWhenOff(t *testing.T) {
	h, now := idleHarness(t, 0)
	a := h.force(h.makeAuto(nil, 1))
	since := now.Add(-24 * time.Hour)
	setIdle(t, h, a, AutomationActive, &since)
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 0 {
		t.Fatalf("alerts with threshold 0: %d", n)
	}
}

func TestAutomationIdleNoAlertWhenPaused(t *testing.T) {
	h, now := idleHarness(t, 5)
	a := h.force(h.makeAuto(nil, 1))
	since := now.Add(-time.Hour)
	setIdle(t, h, a, AutomationPaused, &since)
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 0 {
		t.Fatalf("alerts while paused: %d", n)
	}
}

func TestAutomationIdleThresholdReadLive(t *testing.T) {
	h, now := idleHarness(t, 0)
	a := h.force(h.makeAuto(nil, 1))
	since := now.Add(-time.Hour)
	setIdle(t, h, a, AutomationActive, &since)
	h.c.checkIdle()
	cfg := h.m.Config()
	cfg.Automation.IdleAlertMinutes = 30
	h.m.SetConfig(cfg)
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 1 {
		t.Fatalf("alerts after live threshold change: %d", n)
	}
}

func TestAutomationIdleStartStop(t *testing.T) {
	h := newCoordHarness(t, 1)
	h.c.idleTick = time.Millisecond
	h.c.Start()
	h.c.Start() // no-op
	time.Sleep(5 * time.Millisecond)
	h.c.Stop()
	h.c.Stop()
	h.c.Start()
	h.c.Stop()
}
