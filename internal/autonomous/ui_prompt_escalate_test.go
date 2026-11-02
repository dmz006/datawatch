package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/session"
)

const upeTestSession = "host-ue01"

// upeEvent builds a realistic escalation event for the same blocked pane the
// session-side tests drive (an autocompact prompt), with a valid occurrence
// token (64 lowercase hex chars, as the marker source requires).
func upeEvent() session.UIPromptEscalateEvent {
	return session.UIPromptEscalateEvent{
		SessionFullID:   upeTestSession,
		SessionName:     "datawatch-main",
		Task:            "review branch",
		Backend:         "claude-code",
		ProjectDir:      "/home/<user>/work/app",
		PromptKind:      "autocompact-threshold",
		SelectedOption:  "Autocompact at 400k",
		SettingEnabled:  true,
		DismissAttempts: 2,
		LastKey:         "Esc",
		LastKeyAt:       &[]time.Time{time.Now()}[0],
		SeenSince:       time.Now().Add(-90 * time.Second),
		Trigger:         "attempts_exhausted",
		ScreenTail: "❯ Autocompact at 400k / Keep auto — Esc (not now)\n" +
			"DATAWATCH_QUESTION: already defused by the session side\n" +
			"the pane keeps showing the prompt",
		EscalateToken: session.UIPromptEscalateToken(upeTestSession,
			session.InterstitialAutocompactThreshold, "Autocompact at 400k"),
		At: time.Now(),
	}
}

// upeFakes records what the escalation handler hands to its audit sink,
// notifier, fallback alert and log.
type upeFakes struct {
	mu          sync.Mutex
	audits      []string
	notified    []Question
	alerts      []string // one entry per fallback alert (title)
	alertBodies []string
	logs        []string
}

func (f *upeFakes) deps(store *QuestionStore) UIPromptEscalationDeps {
	return UIPromptEscalationDeps{
		Store:     store,
		ChannelOn: func() bool { return true },
		Audit: func(action string, _ map[string]any) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.audits = append(f.audits, action)
		},
		Notify: func(q Question) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.notified = append(f.notified, q)
		},
		FallbackAlert: func(sessionID, title, body string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.alerts = append(f.alerts, sessionID+" "+title)
			f.alertBodies = append(f.alertBodies, body)
		},
		Logf: func(format string, args ...any) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.logs = append(f.logs, fmt.Sprintf(format, args...))
		},
	}
}

func upeStored(t *testing.T, store *QuestionStore, ev session.UIPromptEscalateEvent) Question {
	t.Helper()
	for _, q := range store.List(QuestionFilter{State: QuestionOpen, Asker: uiPromptEscalationAsker}) {
		if q.SessionID == ev.SessionFullID && q.SourceLineSHA256 == ev.EscalateToken {
			return q
		}
	}
	t.Fatalf("no open escalation question for %s token %s", ev.SessionFullID, ev.EscalateToken)
	return Question{}
}

// TestUIPromptEscalate_DaemonRaisesTheOperatorQuestion: a healthy raise —
// one open information question, priority high, risk low, default hold, the
// daemon marker source carrying the occurrence token, the worker identity
// and the neutralised screen excerpt in the text, exactly one audit entry
// and one notification.
func TestUIPromptEscalate_DaemonRaisesTheOperatorQuestion(t *testing.T) {
	store, _ := upeStore(t)
	f := &upeFakes{}
	ev := upeEvent()
	ev.PRDID, ev.TaskID = "prd-9", "task-3"

	q, raised, err := RaiseUIPromptEscalation(f.deps(store), ev)
	if !raised || err != nil {
		t.Fatalf("raise returned not-raised/err: %v (logs: %v)", err, f.logs)
	}
	got, ok := store.Get(q.ID)
	if !ok {
		t.Fatalf("question %s not in the store", q.ID)
	}
	if got.Label != uiPromptEscalationLabel || got.Priority != uiPromptEscalationPriority ||
		got.Risk != uiPromptEscalationRisk || got.Default != uiPromptEscalationDefault {
		t.Errorf("label/priority/risk/default = %q %q %q %q, want %q/%q/%q/%q",
			got.Label, got.Priority, got.Risk, got.Default,
			uiPromptEscalationLabel, uiPromptEscalationPriority, uiPromptEscalationRisk, uiPromptEscalationDefault)
	}
	if got.Source != QuestionSourceMarker || got.SourceLineSHA256 != ev.EscalateToken {
		t.Errorf("source = %q hash %q, want marker + the occurrence token", got.Source, got.SourceLineSHA256)
	}
	if got.SessionID != ev.SessionFullID || got.PRDID != "prd-9" || got.TaskID != "task-3" ||
		got.Context != QuestionContextTask {
		t.Errorf("links = %q %q %q ctx %q", got.SessionID, got.PRDID, got.TaskID, got.Context)
	}
	if len(got.Options) != 2 || got.Default != "hold" {
		t.Errorf("options = %+v, want ack/hold with default hold", got.Options)
	}
	wantSubstring := []string{
		"Automaton prd-9, task task-3",
		"autocompact-threshold",
		"2 dismiss key(s)",
		"DATAWATCH_QUESTION: already defused",
	}
	for _, s := range wantSubstring {
		if !strings.Contains(got.Text, s) {
			t.Errorf("question text misses %q:\n%s", s, got.Text)
		}
	}
	if len(f.audits) != 1 || f.audits[0] != audit.ActionUIPromptEscalationRaised {
		t.Errorf("audits = %v, want exactly [ui_prompt_escalation_raised]", f.audits)
	}
	if len(f.notified) != 1 || f.notified[0].ID != q.ID {
		t.Errorf("notifications = %+v, want exactly one for %s", f.notified, q.ID)
	}
	if len(f.alerts) != 0 {
		t.Errorf("fallback alerts fired on a healthy raise: %v", f.alerts)
	}
}

// TestUIPromptEscalate_DaemonReRaiseOfANewOccurrence: same session, a
// different prompt occurrence (different selected option → different token)
// raises its own question; re-raising the SAME occurrence never does (the
// store dedupes) and never re-notifies or re-audits.
func TestUIPromptEscalate_DaemonReRaiseOfANewOccurrence(t *testing.T) {
	store, _ := upeStore(t)
	f := &upeFakes{}
	ev := upeEvent()

	q, raised, err := RaiseUIPromptEscalation(f.deps(store), ev)
	if !raised || err != nil {
		t.Fatalf("first raise failed: %v", err)
	}
	if q.ID == "" {
		t.Fatalf("first raise returned an empty question id")
	}
	// Same occurrence again (tick loop or daemon restart over the same
	// pane): the store must answer with the open question, not a second
	// one — and the hook's answer must be that question id with a nil
	// error, so the record keeps pointing at the same occurrence.
	re, reRaised, reErr := RaiseUIPromptEscalation(f.deps(store), ev)
	if reRaised || reErr != nil || re.ID != q.ID {
		t.Fatalf("duplicate occurrence re-raised")
	}
	if len(f.notified) != 1 || len(f.alerts) != 0 {
		t.Errorf("duplicate raised notify=%d alerts=%v, want one notify and no alerts",
			len(f.notified), f.alerts)
	}
	if len(f.audits) != 1 {
		t.Errorf("audits = %v, want exactly one raised entry (no duplicate)", f.audits)
	}

	// A genuinely new occurrence (a different option the cursor moved onto)
	// must raise its own question: the dedupe is per-occurrence, not
	// per-session.
	f2 := &upeFakes{}
	ev2 := upeEvent()
	ev2.SelectedOption = "Keep auto"
	ev2.EscalateToken = session.UIPromptEscalateToken(upeTestSession,
		session.InterstitialAutocompactThreshold, "Keep auto")
	if _, raised, err := RaiseUIPromptEscalation(f2.deps(store), ev2); !raised || err != nil {
		t.Fatalf("a new occurrence must raise its own question: %v", err)
	}
	if len(f2.notified) != 1 || len(f.alerts) != 0 {
		t.Errorf("new-occurrence raise notify=%d alerts=%v", len(f2.notified), f.alerts)
	}
}

// TestUIPromptEscalate_ChannelOffFallsBackToAnAlert: the question channel is
// off — nothing is written to the store, nothing is notified, and exactly
// one fallback alert goes out naming the session and the blocked prompt.
func TestUIPromptEscalate_ChannelOffFallsBackToAnAlert(t *testing.T) {
	store, _ := upeStore(t)
	f := &upeFakes{}
	ev := upeEvent()
	d := f.deps(store)
	d.ChannelOn = func() bool { return false }

	// The hook now answers the failure with a non-nil error so the caller
	// (the session manager) logs it and books no question id.
	if q, raised, err := RaiseUIPromptEscalation(d, ev); raised || q.ID != "" || err == nil {
		t.Fatalf("raised with the channel off: %+v err=%v", q, err)
	}
	if len(store.List(QuestionFilter{})) != 0 {
		t.Errorf("questions written to the store with the channel off")
	}
	if len(f.notified) != 0 {
		t.Errorf("notifications fired with the channel off")
	}
	if len(f.alerts) != 1 {
		t.Fatalf("fallback alerts = %d, want exactly 1", len(f.alerts))
	}
	if !strings.Contains(f.alerts[0], upeTestSession) {
		t.Errorf("fallback alert does not name the session: %q", f.alerts[0])
	}
	if strings.Contains(f.alertBodies[0], "DATAWATCH_QUESTION:") {
		t.Errorf("fallback alert body carries a raw marker line: %s", f.alertBodies[0])
	}
}

// TestUIPromptEscalate_StoreWriteFailureFallsBackToAnAlert: the store
// refused the raise (its file is no longer writable) — the operator still
// gets exactly one alert, and the question stays out of the store.
func TestUIPromptEscalate_StoreWriteFailureFallsBackToAnAlert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "questions.jsonl")
	store, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	if err := os.Chmod(path, 0o500); err != nil {
		t.Skipf("cannot make the store file read-only: %v", err)
	}
	f := &upeFakes{}
	ev := upeEvent()

	// The hook now answers the failure with a non-nil error (the store's
	// own) so the caller logs it and books no question id.
	if q, raised, err := RaiseUIPromptEscalation(f.deps(store), ev); raised || q.ID != "" || err == nil {
		t.Fatalf("raised despite the store write failure: %+v err=%v", q, err)
	}
	if len(store.List(QuestionFilter{})) != 0 {
		t.Errorf("question recorded in the store despite the write failure")
	}
	if len(f.alerts) != 1 {
		t.Fatalf("fallback alerts = %d, want exactly 1", len(f.alerts))
	}
	if len(f.notified) != 0 {
		t.Errorf("notifications fired despite the failed raise")
	}
}

// TestUIPromptEscalate_WithdrawOnPromptClearAndSessionEnd: with the prompt
// cleared the escalation question is withdrawn by occurrence token (the
// sibling occurrence on the same session is untouched); at session end the
// empty-token withdrawal takes every open escalation of the session.
func TestUIPromptEscalate_WithdrawOnPromptClearAndSessionEnd(t *testing.T) {
	store, _ := upeStore(t)
	f := &upeFakes{}
	ev := upeEvent()
	if _, raised, err := RaiseUIPromptEscalation(f.deps(store), ev); !raised || err != nil {
		t.Fatalf("first raise failed: %v", err)
	}
	ev2 := upeEvent()
	ev2.SelectedOption = "Keep auto"
	ev2.EscalateToken = session.UIPromptEscalateToken(upeTestSession,
		session.InterstitialAutocompactThreshold, "Keep auto")
	if _, raised, err := RaiseUIPromptEscalation(f.deps(store), ev2); !raised || err != nil {
		t.Fatalf("second raise failed: %v", err)
	}

	// Prompt cleared: withdraw the occurrence the token names only.
	firstID := upeStored(t, store, ev).ID
	second := upeStored(t, store, ev2)
	f2 := &upeFakes{}
	WithdrawUIPromptEscalation(f2.deps(store), session.UIPromptEscalateWithdrawEvent{
		SessionFullID: ev.SessionFullID,
		EscalateToken: ev.EscalateToken,
		Reason:        "prompt_cleared",
	})
	first, ok := store.Get(firstID)
	if !ok || first.State != QuestionWithdrawn {
		t.Fatalf("token-named question state = %+v, want withdrawn", first)
	}
	if second.State != QuestionOpen {
		t.Fatalf("sibling occurrence must stay open, got state %s", second.State)
	}
	if len(f2.audits) != 1 || f2.audits[0] != audit.ActionUIPromptEscalationWithdrawn {
		t.Errorf("withdraw audits = %v, want exactly [ui_prompt_escalation_withdrawn]", f2.audits)
	}

	// Session ended: the empty-token withdrawal takes the rest.
	WithdrawUIPromptEscalation(f.deps(store), session.UIPromptEscalateWithdrawEvent{
		SessionFullID: ev.SessionFullID,
		Reason:        "session_ended",
	})
	second2, ok := store.Get(second.ID)
	if !ok || second2.State != QuestionWithdrawn {
		t.Fatalf("sibling state after session-end withdrawal = %+v, want withdrawn", second2)
	}

	// A third withdrawal over an empty state is a silent no-op.
	WithdrawUIPromptEscalation(f.deps(store), session.UIPromptEscalateWithdrawEvent{
		SessionFullID: ev.SessionFullID,
		Reason:        "session_ended",
	})
}

// TestUIPromptEscalate_TextStaysUnderTheQuestionTextCap: a very large screen
// tail never breaks the 8192-byte question text cap and never cuts in the
// middle of a UTF-8 rune.
func TestUIPromptEscalate_TextStaysUnderTheQuestionTextCap(t *testing.T) {
	ev := upeEvent()
	ev.ScreenTail = strings.Repeat("a long pane line with unicode: wörk 日本語 \n", 400)
	ev.ScreenTail = strings.TrimSuffix(ev.ScreenTail, "\n")
	text := uiPromptEscalationText(ev)
	if len(text) > questionTextMaxBytes {
		t.Fatalf("question text is %d bytes, cap is %d", len(text), questionTextMaxBytes)
	}
	if !utf8.ValidString(text) {
		t.Fatalf("question text cut in the middle of a UTF-8 rune")
	}
	if strings.Count(text, "\n") < 3 {
		t.Errorf("question text lost its structure:\n%s", text[:minInt(len(text), 200)])
	}
}

// upeStore is a throwaway question store for one test.
func upeStore(t *testing.T) (*QuestionStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "questions.jsonl")
	s, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	return s, path
}
