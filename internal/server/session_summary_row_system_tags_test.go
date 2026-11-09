// Tests that system_tags (the machine-derived placement tags) survive every
// session row shape the server emits: the summary row builder, the window
// path (Hub.summaryRow feeding window frames and session_state), and the
// legacy/full list record (writeSessionListView and boundedSessionsFrame).

package server

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// systemTagsOf decodes a JSON-encoded row and returns its system_tags array.
// It fails the test when the key is missing, so an omitted field is caught.
func systemTagsOf(t *testing.T, raw []byte) []string {
	t.Helper()
	var probe struct {
		SystemTags []string `json:"system_tags"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal row: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("unmarshal keys: %v", err)
	}
	if _, ok := keys["system_tags"]; !ok {
		t.Fatalf("row JSON has no system_tags key: %s", raw)
	}
	return probe.SystemTags
}

func TestSessionSummaryRowKeepsSystemTags(t *testing.T) {
	want := []string{"compute:gpu-1", "backend:claude-code"}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	sess := &session.Session{
		ID: "tagged", FullID: "testhost-tagged", Name: "tagged",
		State:      session.StateComplete,
		Task:       "task tagged",
		CreatedAt:  now.Add(-time.Hour),
		UpdatedAt:  now,
		Tags:       []string{"own:mine"},
		SystemTags: append([]string(nil), want...),
	}

	s := bl90Server(t)

	// 1. Server summary row builder.
	rowRaw, err := json.Marshal(s.sessionSummaryRow(sess, 80))
	if err != nil {
		t.Fatal(err)
	}
	if got := systemTagsOf(t, rowRaw); !reflect.DeepEqual(got, want) {
		t.Errorf("sessionSummaryRow system_tags = %v, want %v", got, want)
	}

	// 2. Window path: the Hub wired the same way production wires it, so
	// Hub.summaryRow goes through the real sessionSummaryRow.
	h := NewHub()
	h.setSessionWindowConfig(
		func() time.Duration { return 5 * time.Minute },
		s.sessionSummaryRow,
	)
	hubRaw, err := json.Marshal(h.summaryRow(sess, 80))
	if err != nil {
		t.Fatal(err)
	}
	if got := systemTagsOf(t, hubRaw); !reflect.DeepEqual(got, want) {
		t.Errorf("Hub.summaryRow system_tags = %v, want %v", got, want)
	}

	// 3. Legacy/full list record, as writeSessionListView builds it for the
	// unsummarized list.
	listRec := sessionListRecord{Session: sess}
	listRec.InheritedTags = s.manager.InheritedTags(listRec.Session)
	listRaw, err := json.Marshal(listRec)
	if err != nil {
		t.Fatal(err)
	}
	if got := systemTagsOf(t, listRaw); !reflect.DeepEqual(got, want) {
		t.Errorf("sessionListRecord system_tags = %v, want %v", got, want)
	}

	// 4. Bounded connect-time frame record, as boundedSessionsFrame builds
	// it after summarizeSession.
	summarized, _ := summarizeSession(sess, 80)
	boundedRaw, err := json.Marshal(sessionListRecord{Session: summarized})
	if err != nil {
		t.Fatal(err)
	}
	if got := systemTagsOf(t, boundedRaw); !reflect.DeepEqual(got, want) {
		t.Errorf("bounded record system_tags = %v, want %v", got, want)
	}
}
