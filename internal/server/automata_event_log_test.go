package server

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
	"time"
)

func autoEvt(id string, at time.Time) AutomataEvent {
	return AutomataEvent{
		ID:       id,
		Time:     at,
		Kind:     AutomataEventWatch,
		Summary:  "watch tick",
		Severity: AutomataSeverityInfo,
		Quiet:    true,
	}
}

func TestAutomataEventRecordSortOrder(t *testing.T) {
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	a := []AutomataEvent{autoEvt("a1", base), autoEvt("a2", base.Add(-time.Minute))}
	b := []AutomataEvent{autoEvt("b1", base.Add(2*time.Minute)), autoEvt("b2", base.Add(-time.Hour))}
	got := MergeAutomataEvents(a, b)

	wantIDs := []string{"b1", "a1", "a2", "b2"}
	gotIDs := make([]string, len(got))
	for i, e := range got {
		gotIDs[i] = e.ID
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("order = %v, want %v", gotIDs, wantIDs)
	}
	// Inputs must not be reordered in place.
	if a[0].ID != "a1" || b[0].ID != "b1" {
		t.Fatalf("merge mutated its inputs: a=%v b=%v", a[0].ID, b[0].ID)
	}
}

func TestAutomataEventRecordTieBreak(t *testing.T) {
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	x := []AutomataEvent{autoEvt("ev-c", at), autoEvt("ev-a", at)}
	y := []AutomataEvent{autoEvt("ev-b", at)}
	got := MergeAutomataEvents(x, y)

	var ids []string
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	want := []string{"ev-a", "ev-b", "ev-c"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("tie-break order = %v, want %v", ids, want)
	}
	// Same input in another list order gives the same result.
	again := MergeAutomataEvents(y, x)
	if !reflect.DeepEqual(again, got) {
		t.Fatalf("merge is not deterministic across input order")
	}
}

func TestAutomataEventRecordCursorRoundTrip(t *testing.T) {
	cases := []AutomataEventCursor{
		{Time: time.Date(2026, 11, 9, 12, 30, 15, 123456789, time.UTC), ID: "evt-42"},
		{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ID: "x|y"},
	}
	for _, c := range cases {
		tok := c.Encode()
		if strings.ContainsAny(tok, "+/=") {
			t.Errorf("token %q is not URL-safe", tok)
		}
		back, err := DecodeAutomataEventCursor(tok)
		if err != nil {
			t.Fatalf("decode %q: %v", tok, err)
		}
		if !back.Time.Equal(c.Time) || back.ID != c.ID {
			t.Errorf("round trip = %+v, want %+v", back, c)
		}
	}

	zero, err := DecodeAutomataEventCursor("")
	if err != nil || !zero.IsZero() {
		t.Fatalf("empty token: cursor=%+v err=%v, want zero cursor and nil error", zero, err)
	}

	notATime := base64.RawURLEncoding.EncodeToString([]byte("not-a-time|evt-1"))
	for _, bad := range []string{"!!!", "bm9wZQ", "MjAyNi0xMS0wOVQxMjowMDowMFp8", notATime} {
		if _, err := DecodeAutomataEventCursor(bad); err == nil {
			t.Errorf("decode %q: want error", bad)
		}
	}
}

func TestAutomataEventRecordBeforeCursorPaging(t *testing.T) {
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	all := MergeAutomataEvents([]AutomataEvent{
		autoEvt("e1", base),
		autoEvt("e2", base),
		autoEvt("e3", base.Add(-time.Second)),
		autoEvt("e4", base.Add(-2*time.Second)),
	})

	page1 := AutomataEventsBefore(all, AutomataEventCursor{}, 2)
	if len(page1) != 2 || page1[0].ID != "e1" || page1[1].ID != "e2" {
		t.Fatalf("page 1 = %v", ids(page1))
	}
	last := page1[len(page1)-1]
	c := AutomataEventCursor{Time: last.Time, ID: last.ID}
	page2 := AutomataEventsBefore(all, c, 2)
	if len(page2) != 2 || page2[0].ID != "e3" || page2[1].ID != "e4" {
		t.Fatalf("page 2 = %v", ids(page2))
	}
	page3 := AutomataEventsBefore(all, AutomataEventCursor{Time: base.Add(-2 * time.Second), ID: "e4"}, 2)
	if len(page3) != 0 {
		t.Fatalf("page after last = %v, want empty", ids(page3))
	}
	if all2 := AutomataEventsBefore(all, AutomataEventCursor{}, 0); len(all2) != 4 {
		t.Fatalf("limit 0 returned %d events, want 4", len(all2))
	}
}

func TestAutomataEventRecordEmptyInput(t *testing.T) {
	if got := MergeAutomataEvents(); got == nil || len(got) != 0 {
		t.Fatalf("no lists: got %#v, want empty non-nil slice", got)
	}
	if got := MergeAutomataEvents(nil, []AutomataEvent{}); got == nil || len(got) != 0 {
		t.Fatalf("nil and empty lists: got %#v, want empty non-nil slice", got)
	}
	if got := AutomataEventsBefore(nil, AutomataEventCursor{}, 10); got == nil || len(got) != 0 {
		t.Fatalf("paging empty list: got %#v", got)
	}
	if got := AutomataEventsBefore(nil, AutomataEventCursor{Time: time.Now(), ID: "z"}, 10); len(got) != 0 {
		t.Fatalf("paging empty list with cursor: got %d events", len(got))
	}
}

func TestAutomataEventRecordValidate(t *testing.T) {
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	good := autoEvt("e1", base)
	good.NextAction = "Review the Automaton in the Automation"
	if err := good.Valid(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}

	mutate := []struct {
		name string
		fn   func(*AutomataEvent)
	}{
		{"empty id", func(e *AutomataEvent) { e.ID = " " }},
		{"unknown kind", func(e *AutomataEvent) { e.Kind = "nap" }},
		{"unknown severity", func(e *AutomataEvent) { e.Severity = "fatal" }},
		{"multi-line summary", func(e *AutomataEvent) { e.Summary = "a\nb" }},
		{"PRD in summary", func(e *AutomataEvent) { e.Summary = "PRD stalled" }},
		{"PRD in next_action", func(e *AutomataEvent) { e.NextAction = "Cancel this PRDs entry" }},
	}
	for _, m := range mutate {
		e := autoEvt("e1", base)
		m.fn(&e)
		if err := e.Valid(); err == nil {
			t.Errorf("%s: want error", m.name)
		}
	}

	// "PRD" inside a longer word is not the internal name and is allowed.
	e := autoEvt("e2", base)
	e.Summary = "PRDX queue drained"
	if err := e.Valid(); err != nil {
		t.Errorf("word boundary: %v", err)
	}
}

func ids(evs []AutomataEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.ID
	}
	return out
}
