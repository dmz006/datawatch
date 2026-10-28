// Triage-alert chat-sink proof: the triage alert raised by the
// poll pass carries the shared triage record text as its body, and
// Router.SendAlert (the chat sink) must deliver that body to the
// backend verbatim — no truncation, no reformatting.

package router

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
)

// fullTriageRecord builds a triage record with every field set, the
// same shape the poll pass hands to the alert.
func fullTriageRecord() *autonomous.TriageRecord {
	then := time.Date(2026, 11, 19, 9, 12, 0, 0, time.UTC)
	return &autonomous.TriageRecord{
		AutomationID:     "auto-7",
		AutomationName:   "Nightly pipeline",
		PRDID:            "prd-77",
		PRDTitle:         "Add paging to the ingestion path",
		Status:           "failed",
		TaskID:           "task-3",
		TaskTitle:        "Wire the page broker into the dispatcher",
		Error:            "broker.Connect timed out after 30s (dial tcp 10.0.4.17:5672: i/o timeout)",
		SessionEndReason: "worker failed: repeated timeout against the staging broker",
		RetryBudget: autonomous.TriageRetryBudget{
			Infra:        autonomous.TriageRetryCategory{Limit: 2, Used: 1, Left: 1},
			Content:      autonomous.TriageRetryCategory{Limit: 1, Used: 0, Left: 1},
			Scope:        autonomous.TriageRetryCategory{Limit: 0, Used: 0, Left: 0},
			Policy:       autonomous.TriageRetryCategory{Limit: 1, Used: 0, Left: 1},
			CeilingTotal: 4,
			CeilingLeft:  3,
		},
		FirstSeenAt: then,
		UpdatedAt:   then.Add(12 * time.Minute),
		Notes: []autonomous.TriageNote{
			{Source: "alert", Text: "first alert raised after the stale threshold"},
		},
	}
}

// TestSendAlert_TriageRecordBodyIntact proves the chain's chat-sink half:
// a triage alert whose body is the shared record text reaches the
// comm backend with the whole record text intact. The body is longer
// than the legacy 200-char alert cap, so anything truncating it would
// fail this test.
func TestSendAlert_TriageRecordBodyIntact(t *testing.T) {
	rec := fullTriageRecord()
	body := autonomous.RenderTriageRecord(rec)
	if len(body) <= 200 {
		t.Fatalf("test body must exceed the legacy 200-char alert cap (was %d) to prove nothing is cut", len(body))
	}

	r := NewRouter("host", "g1", nil, nil, 0, nil)
	var mu sync.Mutex
	var sent []string
	r.backend = &captureBackend{name: "chat", capture: func(m string) {
		mu.Lock()
		sent = append(sent, m)
		mu.Unlock()
	}}

	a := &alerts.Alert{Level: alerts.LevelWarn, Title: "Triage", Body: body}
	r.SendAlert(a)

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(sent)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	got := append([]string(nil), sent...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected exactly one chat send, got %d: %q", len(got), got)
	}

	// The record text must arrive line for line.
	wantLines := []string{
		`Automaton "Add paging to the ingestion path" (id prd-77) is failed and waiting for operator triage.`,
		`Member of Automation "Nightly pipeline" (id auto-7).`,
		"Failed task: Wire the page broker into the dispatcher (id task-3)",
		"Task error: broker.Connect timed out after 30s",
		"Session end reason: worker failed: repeated timeout against the staging broker",
		"Retry budget: infra 1/2 (1 left); content 0/1 (1 left); scope 0/0 (0 left); policy 0/1 (1 left); ceiling 3 left of 4 total",
		"Stuck since 2026-11-19T09:12:00Z.",
		"Notes:",
		"  alert: first alert raised after the stale threshold",
	}
	for _, line := range wantLines {
		if !strings.Contains(got[0], line) {
			t.Errorf("chat message is missing %q\ngot:\n%s", line, got[0])
		}
	}

	// And the send is exactly the alert with the body verbatim — nothing
	// else rewrites it on the way to the sink.
	want := r.AlertText(a)
	if got[0] != want {
		t.Errorf("chat send is not the untruncated alert text\ngot:\n%s\nwant:\n%s", got[0], want)
	}
}

// TestAlertText_PackageAndRouterAgree pins that the ntfy/email fan-out
// text and the router text are the same format for the same alert.
func TestAlertText_PackageAndRouterAgree(t *testing.T) {
	r := NewRouter("host", "g1", nil, nil, 0, nil)
	a := &alerts.Alert{Level: alerts.LevelWarn, Title: "Triage", Body: "line-one\nline-two"}
	if got, want := AlertText("host", a), r.AlertText(a); got != want {
		t.Errorf("package and router alert text disagree\ngot:\n%s\nwant:\n%s", got, want)
	}
	want := "[host] ALERT [WARN] Triage\nline-one\nline-two"
	if got := r.AlertText(a); got != want {
		t.Errorf("alert text format = %q, want %q", got, want)
	}
	// No body: header only, no dangling separator.
	if got, want := r.AlertText(&alerts.Alert{Level: alerts.LevelInfo, Title: "Ping"}), "[host] ALERT [INFO] Ping"; got != want {
		t.Errorf("bodyless alert text = %q, want %q", got, want)
	}
}
