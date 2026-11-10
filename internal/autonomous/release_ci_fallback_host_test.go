package autonomous

// Release CI fall-back, exercised end to end through the direct push
// release step against the fake git host. The remote is a bare repository
// on local disk (release_fake_host_test.go); only the local git binary
// runs. The fake CI report is recorded on the release record the same way
// the CI watch records it, through setReleaseCIOutcome. The fall-back
// itself is applyReleaseCIFallback, used unchanged.

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// ciFallbackHostRelease is one direct push released through the fake host
// into a release record store, with the release size set on the record.
type ciFallbackHostRelease struct {
	m      *Manager
	fake   *fakeDefectIntake
	alerts []string
	store  *ReleaseRecordStore
	rec    *ReleaseRecord
}

// pushCIFallbackHostRelease runs the direct push release step against the
// fake host for a release of the given size, then returns the pushed
// record. Its CI outcome is pending until the test reports one.
func pushCIFallbackHostRelease(t *testing.T, size string) *ciFallbackHostRelease {
	t.Helper()
	remote, clone, _ := newReleaseClone(t)
	captureReleaseGitAudit(t)

	store, err := NewReleaseRecordStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewReleaseRecordStore: %v", err)
	}
	res, err := runDirectReleasePush(context.Background(), releasePushOptions{
		Dir:              clone,
		Project:          "datawatch",
		Tag:              releaseTestTag,
		Remote:           "origin",
		PushAndTag:       true,
		ReleasePath:      config.ReleasePathDirectPush,
		SecretScanMode:   "enforce",
		CredentialSource: fakeCredentialSource(),
		Store:            store,
	})
	if err != nil {
		t.Fatalf("direct push: %v", err)
	}
	if !res.Pushed {
		t.Fatalf("direct push outcome = %q, want pushed", res.Outcome)
	}
	if got, want := remoteRefValue(t, remote, "refs/heads/main"), res.Commit; got != want {
		t.Fatalf("remote main = %s, want pushed commit %s", got, want)
	}

	rec, ok := store.GetRelease("datawatch", releaseTestTag)
	if !ok {
		t.Fatal("push wrote no release record")
	}
	// The release size and the working tree the push came from are set on
	// the record, as the release path does before the CI watch reports.
	rec.ReleaseSize = size
	rec.ReleasePath = clone
	if err := store.save(rec); err != nil {
		t.Fatalf("save release size: %v", err)
	}

	m := freshDefectIntakeManager(t)
	fake := &fakeDefectIntake{}
	m.SetDefectIntake(fake)
	m.SetDefectIntakeAutomation("bugs")
	fx := &ciFallbackHostRelease{m: m, fake: fake, store: store}
	m.SetAlertSink(func(level, title, body string) string {
		fx.alerts = append(fx.alerts, level+": "+title)
		return ""
	})
	fx.rec = rec
	return fx
}

// reportCI records the fake CI outcome on the release record and returns
// the updated record.
func (fx *ciFallbackHostRelease) reportCI(t *testing.T, outcome string, at time.Time) *ReleaseRecord {
	t.Helper()
	rec, err := fx.store.setReleaseCIOutcome("datawatch", releaseTestTag, outcome, at)
	if err != nil {
		t.Fatalf("record CI outcome %q: %v", outcome, err)
	}
	return rec
}

// Red inside the window after a direct push through the fake host: the
// release size's approval mode becomes ask for the project, one alert is
// raised and one defect is filed.
func TestReleaseCIFallbackHost_RedInsideWindowAfterDirectPush(t *testing.T) {
	fx := pushCIFallbackHostRelease(t, config.ReleaseSizeMinor)
	rec := fx.reportCI(t, ReleaseCIRed, fx.rec.PushedAt.Add(10*time.Minute))

	if rec.CIOutcome != ReleaseCIRed {
		t.Fatalf("CI outcome = %q, want red", rec.CIOutcome)
	}
	if !fx.m.applyReleaseCIFallback(rec, fx.rec.PushedAt.Add(10*time.Minute), 30) {
		t.Fatal("red inside the window: fall-back did not fire")
	}
	mode, ok := fx.m.ReleaseSizeModeOverride("datawatch", config.ReleaseSizeMinor)
	if !ok || mode != config.ReleaseApprovalAsk {
		t.Errorf("minor approval override = %q (set=%v); want ask", mode, ok)
	}
	if _, ok := fx.m.ReleaseSizeModeOverride("datawatch", config.ReleaseSizePatch); ok {
		t.Error("patch approval changed; only the released size may change")
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

// Red after the window after a direct push through the fake host: nothing
// changes. No approval override, no alert and no defect.
func TestReleaseCIFallbackHost_RedAfterWindowChangesNothing(t *testing.T) {
	fx := pushCIFallbackHostRelease(t, config.ReleaseSizePatch)
	rec := fx.reportCI(t, ReleaseCIRed, fx.rec.PushedAt.Add(31*time.Minute))

	if rec.CIOutcome != ReleaseCIRed {
		t.Fatalf("CI outcome = %q, want red", rec.CIOutcome)
	}
	if fx.m.applyReleaseCIFallback(rec, fx.rec.PushedAt.Add(31*time.Minute), 30) {
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
