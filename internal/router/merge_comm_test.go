package router

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
)

const mergeCommSample = `{"mode":"on","alert_minutes":30,"automata":[{
  "automaton_id": "prd-9", "name": "Add paging", "branch": "automaton/prd-9",
  "state": "pending", "commits_ahead": 3, "base": "main",
  "pending_since": "2026-01-02T00:00:00Z", "alerted_at": "2026-01-02T00:31:00Z"
}],"shadow_decisions":[]}`

const safeToDeleteCommSample = `{"safe":[{"branch":"automaton/prd-1","worktree":"/w/prd-1","commits":2,"reason":"merged into main"}],
"held":[{"branch":"automaton/prd-2","worktree":"/w/prd-2","state":"pending","dirty":true,"note":"has local edits"}],
"held_dirty":1}`

func TestMergeComm_VerbMergeRenders(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, mergeCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous merge"), " ")
	if log.path != "/api/autonomous/merge" || log.query != "" {
		t.Errorf("path=%s query=%q; want the merge route with no filters", log.path, log.query)
	}
	var ms autonomous.MergeStatus
	if err := json.Unmarshal([]byte(mergeCommSample), &ms); err != nil {
		t.Fatal(err)
	}
	// The reply is the shared render, line for line.
	for _, line := range strings.Split(strings.TrimRight(autonomous.RenderMergeState(ms), "\n"), "\n") {
		if !strings.Contains(got, strings.TrimSpace(line)) {
			t.Errorf("reply lacks shared render line %q: %q", line, got)
		}
	}
	for _, want := range []string{
		"Automaton merge pending: mode on (alert threshold: 30 min)",
		`Automaton "Add paging" (id prd-9): branch automaton/prd-9 is pending, 3 commit(s) ahead of main`,
		"Nothing has been merged, deleted or pushed.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestMergeComm_VerbSafeToDeleteRenders(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, safeToDeleteCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous safe-to-delete"), " ")
	if log.path != "/api/autonomous/safe-to-delete" || log.query != "" {
		t.Errorf("path=%s query=%q; want the safe-to-delete route with no filters", log.path, log.query)
	}
	for _, want := range []string{
		"Automaton branches safe to delete: 1, held back: 1",
		"automaton/prd-1 (2 commit(s)): merged into main",
		"automaton/prd-2: pending, uncommitted changes — has local edits",
		"This is a report only: nothing has been deleted.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestMergeComm_UnknownSubcommandShowsHelp(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, mergeCommSample)
	r := setupPeerRouter(t, port)

	// Extra arguments are refused with the verb's usage, without a daemon call.
	got := strings.Join(r.HandleTestMessage("autonomous merge now please"), " ")
	if !strings.Contains(got, "usage: autonomous merge") {
		t.Errorf("extra args must be refused with the usage, got: %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous safe-to-delete everything"), " ")
	if !strings.Contains(got, "usage: autonomous safe-to-delete") {
		t.Errorf("extra args must be refused with the usage, got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("the refusals must not reach the daemon, got %d calls", log.calls)
	}

	// An unknown verb gets the autonomous help, which lists both merge verbs.
	got = strings.Join(r.HandleTestMessage("autonomous merg"), " ")
	if !strings.Contains(got, "unknown verb merg") || !strings.Contains(got, "|merge|safe-to-delete|") {
		t.Errorf("an unknown verb must show the help listing the merge verbs, got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("an unknown verb must not reach the daemon, got %d calls", log.calls)
	}
}

// TestMergeComm_AlertBodyIntact proves the merge-pending alert body reaches
// the chat sink verbatim and states the same facts the comm render shows for
// the same record.
func TestMergeComm_AlertBodyIntact(t *testing.T) {
	first := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	now := first.Add(31 * time.Minute)
	rec := &autonomous.TriageRecord{
		AutomationID:   "auto-7",
		AutomationName: "Nightly pipeline",
		MemberID:       "m-2",
		PRDID:          "prd-9",
		PRDTitle:       "Add paging",
		Status:         autonomous.TriageStatusMergePending,
		Branch:         "automaton/prd-9",
		CommitsAhead:   3,
		BaseBranch:     "main",
		FirstSeenAt:    first,
		UpdatedAt:      first,
	}
	body := autonomous.MergePendingAlertBody(rec, now)
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
	a := &alerts.Alert{Level: alerts.LevelWarn, Title: "Automaton Add paging merge pending", Body: body}
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
	if want := r.AlertText(a); got[0] != want {
		t.Errorf("chat send is not the untruncated alert text\ngot:\n%s\nwant:\n%s", got[0], want)
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(got[0], line) {
			t.Errorf("chat message is missing body line %q\ngot:\n%s", line, got[0])
		}
	}

	// The comm render of the same record names the same Automaton, branch,
	// commit count, base and pending-since stamp as the alert body.
	since := first
	render := autonomous.RenderMergeState(autonomous.MergeStatus{
		Mode:         "on",
		AlertMinutes: 30,
		Automata: []autonomous.MergeStatusEntry{{
			AutomatonID: rec.PRDID, Name: rec.PRDTitle, Branch: rec.Branch,
			State: autonomous.MergeStatePending, CommitsAhead: rec.CommitsAhead, Base: rec.BaseBranch, PendingSince: &since,
		}},
	})
	for _, shared := range []string{
		`Automaton "Add paging" (id prd-9)`,
		"automaton/prd-9",
		"3 commit(s)",
		"main",
		"2026-01-02T00:00:00Z",
	} {
		if !strings.Contains(body, shared) {
			t.Errorf("alert body lacks %q: %q", shared, body)
		}
		if !strings.Contains(render, shared) {
			t.Errorf("comm render lacks %q: %q", shared, render)
		}
	}
}

func TestMergeComm_ParityListHasVerbs(t *testing.T) {
	verbs := MergeCommVerbs()
	for _, want := range []string{"merge", "safe-to-delete"} {
		if !slices.Contains(verbs, want) {
			t.Errorf("MergeCommVerbs() = %v, must contain %q", verbs, want)
		}
	}
	for _, verb := range verbs {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}
	r := setupPeerRouter(t, 0)
	got := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(got, "|merge|safe-to-delete|") {
		t.Errorf("the autonomous help must list the merge verbs; got:\n%s", got)
	}
}
