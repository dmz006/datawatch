// System-alert comm fan-out: the triage alert raised by the poll pass
// is a system alert (no session), and it must reach the operator's
// comm channels — every active router, ntfy, email — with the alert
// body intact. Router.SendAlert does not truncate or reformat it, so
// the shared triage record text the pass put in the body is exactly
// what each chat sink delivers; the ntfy email/ntfy channels honour
// notify_exclude per send.

package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/router"
)

// fakeAlertCapture is a messaging.Backend that records what Send
// delivers, for asserting on the fan-out.
type fakeAlertCapture struct {
	name string
	mu   sync.Mutex
	got  []string
	ch   chan string
}

func newFakeAlertCapture(name string) *fakeAlertCapture {
	return &fakeAlertCapture{name: name, ch: make(chan string, 16)}
}

func (f *fakeAlertCapture) Name() string { return f.name }

func (f *fakeAlertCapture) Send(_, message string) error {
	f.mu.Lock()
	f.got = append(f.got, message)
	f.mu.Unlock()
	select {
	case f.ch <- message:
	default:
	}
	return nil
}

func (f *fakeAlertCapture) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

func (f *fakeAlertCapture) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func (f *fakeAlertCapture) Subscribe(context.Context, func(messaging.Message)) error { return nil }
func (f *fakeAlertCapture) Link(string, func(string)) error                          { return nil }
func (f *fakeAlertCapture) SelfID() string                                           { return "test" }
func (f *fakeAlertCapture) Close() error                                             { return nil }

// triageSystemAlert builds a triage alert the way the poll pass raises
// it: shared record text as the body, no session.
func triageSystemAlert(t *testing.T) *alerts.Alert {
	t.Helper()
	then := time.Date(2026, 11, 19, 9, 12, 0, 0, time.UTC)
	rec := &autonomous.TriageRecord{
		AutomationID: "auto-7", AutomationName: "Nightly pipeline",
		PRDID: "prd-77", PRDTitle: "Add paging to the ingestion path", Status: "failed",
		TaskID: "task-3", TaskTitle: "Wire the page broker into the dispatcher",
		Error: "broker.Connect timed out after 30s (dial tcp 10.0.4.17:5672: i/o timeout)",
		RetryBudget: autonomous.TriageRetryBudget{
			Infra:        autonomous.TriageRetryCategory{Limit: 2, Used: 1, Left: 1},
			Content:      autonomous.TriageRetryCategory{Limit: 1, Used: 0, Left: 1},
			CeilingTotal: 4, CeilingLeft: 3,
		},
		FirstSeenAt: then, UpdatedAt: then.Add(12 * time.Minute),
	}
	body := autonomous.RenderTriageRecord(rec)
	if len(body) <= 200 {
		t.Fatalf("body must exceed the legacy 200-char cap to prove no truncation (was %d)", len(body))
	}
	return &alerts.Alert{Level: alerts.LevelWarn, Title: "Triage", Body: body}
}

// TestSendSystemAlertToComm_FansOutIntact proves the alert-to-comm
// path end to end at the daemon layer: a triage system alert reaches
// every non-excluded comm backend with the body intact (record text
// for record text), skips what notify_exclude names, and drives
// ntfy/email only when present.
func TestSendSystemAlertToComm_FansOutIntact(t *testing.T) {
	a := triageSystemAlert(t)

	sig := newFakeAlertCapture("signal")
	ntfyRt := newFakeAlertCapture("ntfy")

	var ntfyCalls int
	var ntfyText string
	sendNtfy := func(text string) error { ntfyCalls++; ntfyText = text; return nil }

	routers := []*router.Router{
		router.NewRouter("host", "sig-1", sig, nil, 0, nil),
		router.NewRouter("host", "ntfy-1", ntfyRt, nil, 0, nil),
	}

	sendSystemAlertToComm(a, "host", []string{"ntfy"}, routers, sendNtfy, nil)

	deadline := time.Now().Add(2 * time.Second)
	for done := false; !done && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		done = sig.count() >= 1
	}
	time.Sleep(50 * time.Millisecond) // let any (wrong) extra sends land

	if n := sig.count(); n != 1 {
		t.Fatalf("signal router should get exactly one alert send, got %d: %q", n, sig.all())
	}
	want := router.AlertText("host", a)
	if got := sig.all()[0]; got != want {
		t.Errorf("chat sink delivery is not the untruncated alert text\ngot:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(sig.all()[0], a.Body) {
		t.Error("the record text body was lost in transit (truncated or reformatted)")
	}

	// The comm channel named by notify_exclude gets nothing...
	if n := ntfyRt.count(); n != 0 {
		t.Errorf("excluded router should get no alert, got %d", n)
	}
	// ...and the ntfy send channel is excluded by the same name.
	if ntfyCalls != 0 {
		t.Errorf("excluded ntfy channel must not send, sent %q", ntfyText)
	}
}

// TestSendSystemAlertToComm_NtfyAndEmailIncluded proves the opposite
// corner: nothing excluded, ntfy and email present → both fan out with
// the same full text the router receives.
func TestSendSystemAlertToComm_NtfyAndEmailIncluded(t *testing.T) {
	a := triageSystemAlert(t)
	sig := newFakeAlertCapture("signal")

	var ntfyTexts, emailTexts []string
	sendNtfy := func(text string) error { ntfyTexts = append(ntfyTexts, text); return nil }
	sendEmail := func(text string) error { emailTexts = append(emailTexts, text); return nil }

	sendSystemAlertToComm(a, "host", nil,
		[]*router.Router{router.NewRouter("host", "sig-1", sig, nil, 0, nil)},
		sendNtfy, sendEmail)

	deadline := time.Now().Add(2 * time.Second)
	for sig.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	want := router.AlertText("host", a)
	if len(ntfyTexts) != 1 || ntfyTexts[0] != want {
		t.Errorf("ntfy fan-out = %q, want one full alert text %q", ntfyTexts, want)
	}
	if len(emailTexts) != 1 || emailTexts[0] != want {
		t.Errorf("email fan-out = %q, want one full alert text %q", emailTexts, want)
	}
	if !strings.Contains(ntfyTexts[0], a.Body) {
		t.Error("ntfy text lost the body")
	}
}
