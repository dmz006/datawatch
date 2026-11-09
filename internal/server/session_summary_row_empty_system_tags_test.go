// Tests that an empty system_tags list is omitted from every session row
// shape (never sent as null or an empty array), as the docs state, and that
// a non-empty list is still sent in full.

package server

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// assertNoSystemTagsKey checks the row JSON has no system_tags key at all.
func assertNoSystemTagsKey(t *testing.T, label string, raw []byte) {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("%s: unmarshal keys: %v", label, err)
	}
	if v, ok := keys["system_tags"]; ok {
		t.Errorf("%s: system_tags present (%s), want the key omitted", label, v)
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
		assertNoSystemTagsKey(t, name+" sessionSummaryRow", rowRaw)

		hubRaw, err := json.Marshal(h.summaryRow(sess, 80))
		if err != nil {
			t.Fatal(err)
		}
		assertNoSystemTagsKey(t, name+" Hub.summaryRow", hubRaw)
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
		assertNoSystemTagsKey(t, name+" sessionListRecord", listRaw)

		summarized, _ := summarizeSession(sess, 80)
		boundedRaw, err := json.Marshal(sessionListRecord{Session: summarized})
		if err != nil {
			t.Fatal(err)
		}
		assertNoSystemTagsKey(t, name+" bounded record", boundedRaw)
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
