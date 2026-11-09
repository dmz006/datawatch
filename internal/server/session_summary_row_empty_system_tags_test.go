// Tests that an empty system_tags list is sent as an empty array in every
// session row shape (never null and never absent), and that a non-empty list
// is still sent in full.

package server

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// assertEmptySystemTags checks the row JSON carries system_tags as an empty
// array: key present, not null, decoding to a non-nil empty slice.
func assertEmptySystemTags(t *testing.T, label string, raw []byte) {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("%s: unmarshal keys: %v", label, err)
	}
	v, ok := keys["system_tags"]
	if !ok {
		t.Fatalf("%s: system_tags absent, want []", label)
	}
	if string(v) != "[]" {
		t.Errorf("%s: system_tags = %s, want []", label, v)
	}
	got := systemTagsOf(t, raw)
	if got == nil || len(got) != 0 {
		t.Errorf("%s: system_tags decoded to %#v, want an empty non-nil slice", label, got)
	}
}

func emptyTagSessions(now time.Time) map[string]*session.Session {
	mk := func(id string, tags []string) *session.Session {
		return &session.Session{
			ID: id, FullID: "testhost-" + id, Name: id,
			State:      session.StateComplete,
			Task:       "task " + id,
			CreatedAt:  now.Add(-time.Hour),
			UpdatedAt:  now,
			SystemTags: tags,
		}
	}
	return map[string]*session.Session{
		"nil":   mk("niltags", nil),
		"empty": mk("emptytags", []string{}),
	}
}

func TestSessionSummaryRowSendsEmptySystemTags(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	s := bl90Server(t)
	h := NewHub()
	h.setSessionWindowConfig(
		func() time.Duration { return 5 * time.Minute },
		s.sessionSummaryRow,
	)
	for name, sess := range emptyTagSessions(now) {
		rowRaw, err := json.Marshal(s.sessionSummaryRow(sess, 80))
		if err != nil {
			t.Fatal(err)
		}
		assertEmptySystemTags(t, name+" sessionSummaryRow", rowRaw)

		hubRaw, err := json.Marshal(h.summaryRow(sess, 80))
		if err != nil {
			t.Fatal(err)
		}
		assertEmptySystemTags(t, name+" Hub.summaryRow", hubRaw)
	}
}

func TestSessionListRecordSendsEmptySystemTags(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	s := bl90Server(t)
	for name, sess := range emptyTagSessions(now) {
		listRec := sessionListRecord{Session: sess}
		listRec.InheritedTags = s.manager.InheritedTags(listRec.Session)
		listRaw, err := json.Marshal(listRec)
		if err != nil {
			t.Fatal(err)
		}
		assertEmptySystemTags(t, name+" sessionListRecord", listRaw)

		summarized, _ := summarizeSession(sess, 80)
		boundedRaw, err := json.Marshal(sessionListRecord{Session: summarized})
		if err != nil {
			t.Fatal(err)
		}
		assertEmptySystemTags(t, name+" bounded record", boundedRaw)
	}

	// A non-empty list is still sent in full.
	want := []string{"compute:gpu-1", "backend:claude-code"}
	full := emptyTagSessions(now)["empty"]
	full.SystemTags = append([]string(nil), want...)
	raw, err := json.Marshal(sessionListRecord{Session: full})
	if err != nil {
		t.Fatal(err)
	}
	if got := systemTagsOf(t, raw); !reflect.DeepEqual(got, want) {
		t.Errorf("non-empty system_tags = %v, want %v", got, want)
	}
}
