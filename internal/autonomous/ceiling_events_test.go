package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

func ceilingTestManager(t *testing.T) (*Manager, *fakeAuditLog, *[][3]string) {
	t.Helper()
	m := newManagerForPRDLLM(t)
	fake := &fakeAuditLog{}
	m.SetAuditFn(fake.fn)
	var alerts [][3]string
	m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, [3]string{level, title, body})
		return "a1"
	})
	return m, fake, &alerts
}

func TestRecordCeilingKillEventAndAlert(t *testing.T) {
	m, fake, alerts := ceilingTestManager(t)
	m.RecordCeilingKill("t1", "node", 2048, 1024)
	if fake.count(audit.ActionResourceCeilingKill) != 1 {
		t.Fatalf("actions=%v", fake.actions)
	}
	d := fake.details[0]
	if d["task"] != "t1" || d["process"] != "node" || d["peak_mb"] != 2048 || d["ceiling_mb"] != 1024 {
		t.Fatalf("details=%v", d)
	}
	if len(*alerts) != 1 || (*alerts)[0][0] != "warn" || !strings.Contains((*alerts)[0][2], "node") {
		t.Fatalf("alerts=%v", *alerts)
	}
}

func TestRecordCeilingKillUnknownProcess(t *testing.T) {
	m, fake, _ := ceilingTestManager(t)
	m.RecordCeilingKill("t1", "", 10, 5)
	if fake.details[0]["process"] != "unknown" {
		t.Fatalf("details=%v", fake.details[0])
	}
}

func TestRecordNoCeilingEventOnly(t *testing.T) {
	m, fake, alerts := ceilingTestManager(t)
	m.RecordNoCeiling("t2")
	if fake.count(audit.ActionResourceCeilingKill) != 1 || fake.details[0]["task"] != "t2" {
		t.Fatalf("actions=%v details=%v", fake.actions, fake.details)
	}
	if len(*alerts) != 0 {
		t.Fatalf("unexpected alerts %v", *alerts)
	}
}

func TestCeilingEventsNilSinks(t *testing.T) {
	m := newManagerForPRDLLM(t)
	m.RecordCeilingKill("t", "p", 1, 1)
	m.RecordNoCeiling("t")
}
