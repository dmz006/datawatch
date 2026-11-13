package autonomous

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

func intermittentTestDefect(test, commit string) mergeshadow.IntermittentDefect {
	at := time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)
	return mergeshadow.IntermittentDefectsFromGate(mergeshadow.GateResult{
		Name: "test", Status: mergeshadow.GateIntermittent,
		Output:  `{"Action":"output","Package":"p","Test":"` + test + `","Output":"    x_test.go:3: got 2, want 1\n"}` + "\n" + `{"Action":"fail","Package":"p","Test":"` + test + `"}` + "\n",
		Finding: "x_test.go:3: got 2, want 1",
	}, "a1", commit, at)[0]
}

func TestIntermittentHousekeepingViewRow(t *testing.T) {
	m := &Manager{dataDir: t.TempDir()}
	if got := m.HousekeepingRepairsView(); got.IntermittentDefects == nil || len(got.IntermittentDefects) != 0 {
		t.Fatalf("empty store: rows = %#v, want empty non-nil", got.IntermittentDefects)
	}
	d := intermittentTestDefect("TestFlaky", "abc123")
	if _, err := mergeshadow.RecordIntermittentDefects(m.dataDir, "a1", []mergeshadow.IntermittentDefect{d}); err != nil {
		t.Fatal(err)
	}
	v := m.HousekeepingRepairsView()
	if len(v.Groups) != 4 {
		t.Errorf("groups = %d, want the 4 repair groups unchanged", len(v.Groups))
	}
	if len(v.IntermittentDefects) != 1 {
		t.Fatalf("rows = %d, want 1", len(v.IntermittentDefects))
	}
	row := v.IntermittentDefects[0]
	if row.Test != "TestFlaky" || row.Commit != "abc123" || row.Date != "2026-11-13" || row.Key != d.Key ||
		!strings.Contains(row.Message, "got 2, want 1") || row.FirstLines == "" {
		t.Errorf("row = %+v", row)
	}
	body, _ := json.Marshal(v)
	if !strings.Contains(string(body), `"intermittent_defects":[{`) {
		t.Errorf("json lacks the rows: %s", body)
	}
	var b strings.Builder
	RenderHousekeepingRepairs(&b, &v)
	for _, frag := range []string{"Intermittent test failures (1)", "TestFlaky", "2026-11-13", "commit abc123", "got 2, want 1"} {
		if !strings.Contains(b.String(), frag) {
			t.Errorf("render lacks %q:\n%s", frag, b.String())
		}
	}
	// A view with no rows renders no intermittent block.
	var empty strings.Builder
	none := (*Repairer)(nil).View()
	RenderHousekeepingRepairs(&empty, &none)
	if strings.Contains(empty.String(), "Intermittent") {
		t.Errorf("empty view renders an intermittent block:\n%s", empty.String())
	}
}

func TestIntermittentAlertRaisedWithPlainTitle(t *testing.T) {
	type alert struct{ level, title, body string }
	var got []alert
	m := &Manager{}
	m.SetAlertSink(func(level, title, body string) string {
		got = append(got, alert{level, title, body})
		return ""
	})
	d := intermittentTestDefect("TestFlaky", "abc123")
	m.raiseIntermittentDefectAlerts([]mergeshadow.IntermittentDefect{d})
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want 1", len(got))
	}
	a := got[0]
	if a.title != "TestFlaky" || a.level != "warn" {
		t.Errorf("alert = %+v, want warn titled with the test name", a)
	}
	for _, frag := range []string{"abc123", "2026-11-13", "got 2, want 1", "not blocked"} {
		if !strings.Contains(a.body, frag) {
			t.Errorf("body lacks %q: %s", frag, a.body)
		}
	}
	// No sink and no records raise nothing and do not panic.
	(&Manager{}).raiseIntermittentDefectAlerts([]mergeshadow.IntermittentDefect{d})
	m.raiseIntermittentDefectAlerts(nil)
	if len(got) != 1 {
		t.Errorf("alerts = %d after nil cases, want still 1", len(got))
	}
}

// The finish hook raises one alert per new record: the same test at the same
// commit, run again, files no second record and raises no second alert.
func TestIntermittentFinishHookAlertsOnce(t *testing.T) {
	repo := shadowRepo(t)
	gates := func(context.Context, string, time.Duration) []mergeshadow.GateResult {
		return []mergeshadow.GateResult{
			{Name: "build", Status: mergeshadow.GatePass},
			{Name: "lint", Status: mergeshadow.GatePass},
			{Name: "test", Status: mergeshadow.GateIntermittent, Finding: "x_test.go:3: got 2, want 1",
				Output: `{"Action":"output","Package":"p","Test":"TestFlaky","Output":"    x_test.go:3: got 2, want 1\n"}` + "\n" + `{"Action":"fail","Package":"p","Test":"TestFlaky"}` + "\n"},
		}
	}
	prev := mergeShadowRun
	mergeShadowRun = func(ctx context.Context, in mergeshadow.Input) (*mergeshadow.MergePlan, error) {
		return mergeshadow.Runner{Gates: gates}.Run(ctx, in)
	}
	t.Cleanup(func() { mergeShadowRun = prev })

	m := shadowManager(t)
	var titles []string
	m.SetAlertSink(func(_, title, _ string) string { titles = append(titles, title); return "" })

	for i := 0; i < 2; i++ {
		done := m.startMergeShadow(finishedShadowPRD(repo))
		if done == nil {
			t.Fatal("no run started")
		}
		select {
		case <-done:
		case <-time.After(2 * time.Minute):
			t.Fatal("shadow run did not finish")
		}
	}
	if len(titles) != 1 || titles[0] != "TestFlaky" {
		t.Errorf("alert titles = %v, want exactly [TestFlaky]", titles)
	}
	if rows := m.HousekeepingRepairsView().IntermittentDefects; len(rows) != 1 || rows[0].Test != "TestFlaky" {
		t.Errorf("rows = %+v, want one TestFlaky row", rows)
	}
}
