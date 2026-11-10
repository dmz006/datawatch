package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// releaseCIFallbackFixture is a Manager with an alert sink that records
// every alert and a defect intake that records every offer, plus a red
// release pushed at a fixed time.
type releaseCIFallbackFixture struct {
	m      *Manager
	fake   *fakeDefectIntake
	alerts []string
	pushed time.Time
	dir    string
}

func newReleaseCIFallbackFixture(t *testing.T) *releaseCIFallbackFixture {
	t.Helper()
	m := freshDefectIntakeManager(t)
	fx := &releaseCIFallbackFixture{m: m, fake: &fakeDefectIntake{}, dir: t.TempDir(), pushed: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	m.SetDefectIntake(fx.fake)
	m.SetDefectIntakeAutomation("bugs")
	m.SetAlertSink(func(level, title, body string) string {
		fx.alerts = append(fx.alerts, level+": "+title)
		return ""
	})
	return fx
}

func (fx *releaseCIFallbackFixture) release(ci string, size string) *ReleaseRecord {
	return &ReleaseRecord{
		Project:      "datawatch",
		Tag:          "v9.1.0",
		PushedCommit: "abc1234",
		ReleasePath:  fx.dir,
		PushedAt:     fx.pushed,
		PushOutcome:  "pushed",
		CIOutcome:    ci,
		ReleaseSize:  size,
	}
}

// Red inside the window: the release size's approval mode is set to ask
// for the project, one alert is raised, and one defect is filed through
// the defect intake.
func TestReleaseCIFallback_RedInsideWindowFallsBackAlertsAndFilesDefect(t *testing.T) {
	fx := newReleaseCIFallbackFixture(t)
	rec := fx.release(ReleaseCIRed, config.ReleaseSizeMinor)
	now := fx.pushed.Add(10 * time.Minute)

	if !fx.m.applyReleaseCIFallback(rec, now, 30) {
		t.Fatal("red inside the window: fall-back did not fire")
	}
	mode, ok := fx.m.ReleaseSizeModeOverride("datawatch", config.ReleaseSizeMinor)
	if !ok || mode != config.ReleaseApprovalAsk {
		t.Errorf("minor approval override = %q (set=%v); want ask", mode, ok)
	}
	if _, ok := fx.m.ReleaseSizeModeOverride("datawatch", config.ReleaseSizePatch); ok {
		t.Error("patch approval changed; only the release's own size may change")
	}
	if len(fx.alerts) != 1 {
		t.Fatalf("alerts = %v; want exactly one", fx.alerts)
	}
	if len(fx.fake.calls) != 1 {
		t.Fatalf("defect offers = %d; want exactly one", len(fx.fake.calls))
	}
	if fx.fake.calls[0].Kind != PRDKindBug {
		t.Errorf("defect kind = %q; want %q", fx.fake.calls[0].Kind, PRDKindBug)
	}
}

// Red after the window: nothing changes — no override, no alert, no defect.
func TestReleaseCIFallback_RedAfterWindowDoesNothing(t *testing.T) {
	fx := newReleaseCIFallbackFixture(t)
	rec := fx.release(ReleaseCIRed, config.ReleaseSizePatch)
	now := fx.pushed.Add(31 * time.Minute)

	if fx.m.applyReleaseCIFallback(rec, now, 30) {
		t.Fatal("red after the window: fall-back fired")
	}
	if _, ok := fx.m.ReleaseSizeModeOverride("datawatch", config.ReleaseSizePatch); ok {
		t.Error("red after the window changed the approval mode")
	}
	if len(fx.alerts) != 0 {
		t.Errorf("alerts = %v; want none", fx.alerts)
	}
	if len(fx.fake.calls) != 0 {
		t.Errorf("defect offers = %d; want none", len(fx.fake.calls))
	}
}

// Green does nothing, and so does pending: neither changes the approval
// mode, raises an alert or files a defect, even inside the window.
func TestReleaseCIFallback_GreenDoesNothing(t *testing.T) {
	for _, ci := range []string{ReleaseCIGreen, ReleaseCIPending} {
		t.Run(ci, func(t *testing.T) {
			fx := newReleaseCIFallbackFixture(t)
			rec := fx.release(ci, config.ReleaseSizeMajor)
			now := fx.pushed.Add(5 * time.Minute)

			if fx.m.applyReleaseCIFallback(rec, now, 30) {
				t.Fatalf("%s outcome: fall-back fired", ci)
			}
			if _, ok := fx.m.ReleaseSizeModeOverride("datawatch", config.ReleaseSizeMajor); ok {
				t.Errorf("%s outcome changed the approval mode", ci)
			}
			if len(fx.alerts) != 0 || len(fx.fake.calls) != 0 {
				t.Errorf("%s outcome raised alerts=%v offers=%d; want none", ci, fx.alerts, len(fx.fake.calls))
			}
		})
	}
}
