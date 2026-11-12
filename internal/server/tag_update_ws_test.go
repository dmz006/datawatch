package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// recvTagUpdateFrames returns the tag_update frames a client receives within
// a short window. Other frame types, such as session lists the start handler
// sends, are skipped.
func recvTagUpdateFrames(t *testing.T, ch chan []byte) []WSMessage {
	t.Helper()
	var out []WSMessage
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case raw := <-ch:
			var msg WSMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.Fatalf("decode frame: %v (%s)", err, raw)
			}
			if msg.Type == MsgTagUpdate {
				out = append(out, msg)
			}
		case <-deadline:
			return out
		}
	}
}

// fieldNames returns the top-level JSON field names of a frame's data.
func fieldNames(t *testing.T, data json.RawMessage) map[string]bool {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("decode data %s: %v", data, err)
	}
	out := map[string]bool{}
	for k := range fields {
		out[k] = true
	}
	return out
}

func newTagEventHub(t *testing.T) (*Hub, chan []byte) {
	t.Helper()
	hub := NewHub()
	go hub.Run()
	ch := make(chan []byte, 64)
	hub.register <- &client{hub: hub, send: ch, subscribed: make(map[string]bool)}
	return hub, ch
}

func TestTagEventFrameCarriesIDsOnlyForEveryKind(t *testing.T) {
	hub, ch := newTagEventHub(t)
	srv := &HTTPServer{hub: hub}

	cases := []struct {
		ev   autonomous.TagEvent
		keys map[string]bool
	}{
		{autonomous.TagEvent{Kind: autonomous.TagEventCreated, TagID: "t1"}, map[string]bool{"kind": true, "tag_id": true}},
		{autonomous.TagEvent{Kind: autonomous.TagEventRenamed, TagID: "t1"}, map[string]bool{"kind": true, "tag_id": true}},
		{autonomous.TagEvent{Kind: autonomous.TagEventReparented, TagID: "t1"}, map[string]bool{"kind": true, "tag_id": true}},
		{autonomous.TagEvent{Kind: autonomous.TagEventRecolored, TagID: "t1"}, map[string]bool{"kind": true, "tag_id": true}},
		{autonomous.TagEvent{Kind: autonomous.TagEventDeleted, TagID: "t1"}, map[string]bool{"kind": true, "tag_id": true}},
		{autonomous.TagEvent{Kind: autonomous.TagEventAssigned, PRDID: "p1"}, map[string]bool{"kind": true, "prd_id": true}},
		{autonomous.TagEvent{Kind: autonomous.TagEventSessionAssigned, SessionID: "host-s1"}, map[string]bool{"kind": true, "session_id": true}},
		{autonomous.TagEvent{Kind: autonomous.TagEventRegistrySynced}, map[string]bool{"kind": true}},
	}
	for _, tc := range cases {
		srv.BroadcastTagUpdate(tc.ev)
		got := recvTagUpdateFrames(t, ch)
		if len(got) != 1 {
			t.Fatalf("%s: got %d tag_update frames, want 1", tc.ev.Kind, len(got))
		}
		if got[0].Type != MsgTagUpdate {
			t.Errorf("%s: frame type = %q, want %q", tc.ev.Kind, got[0].Type, MsgTagUpdate)
		}
		names := fieldNames(t, got[0].Data)
		if len(names) != len(tc.keys) {
			t.Errorf("%s: data fields = %v, want %v", tc.ev.Kind, names, tc.keys)
		}
		for k := range tc.keys {
			if !names[k] {
				t.Errorf("%s: data fields = %v, missing %q", tc.ev.Kind, names, k)
			}
		}
	}
}

func TestTagEventFrameReachesEveryConnectedClient(t *testing.T) {
	hub, first := newTagEventHub(t)
	second := make(chan []byte, 64)
	hub.register <- &client{hub: hub, send: second, subscribed: make(map[string]bool)}
	srv := &HTTPServer{hub: hub}

	srv.BroadcastTagUpdate(autonomous.TagEvent{Kind: autonomous.TagEventRenamed, TagID: "t1"})

	for name, ch := range map[string]chan []byte{"first": first, "second": second} {
		got := recvTagUpdateFrames(t, ch)
		if len(got) != 1 {
			t.Errorf("%s client: got %d tag_update frames, want 1", name, len(got))
			continue
		}
		if string(got[0].Data) != `{"kind":"renamed","tag_id":"t1"}` {
			t.Errorf("%s client: data = %s", name, got[0].Data)
		}
	}
}

func TestTagEventWithoutHubDropsTheFrame(t *testing.T) {
	srv := &HTTPServer{}
	srv.BroadcastTagUpdate(autonomous.TagEvent{Kind: autonomous.TagEventDeleted, TagID: "t1"})
}

func TestTagEventSessionRouteBroadcastsSessionAssigned(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	useFakeSessionTags(t, s)
	hub, ch := newTagEventHub(t)
	s.hub = hub
	fullID := startTagsSession(t, s, `{"task":"plain","project_dir":"`+t.TempDir()+`"}`)
	recvTagUpdateFrames(t, ch)

	w := sessionTagsCall(t, s, "", fullID, "tag-a")
	if w.Code != http.StatusOK {
		t.Fatalf("set tags: %d %s", w.Code, w.Body.String())
	}
	got := recvTagUpdateFrames(t, ch)
	if len(got) != 1 {
		t.Fatalf("set tags: got %d tag_update frames, want 1", len(got))
	}
	want := `{"kind":"session_assigned","session_id":"` + fullID + `"}`
	if string(got[0].Data) != want {
		t.Errorf("data = %s, want %s", got[0].Data, want)
	}
}

func TestTagEventSessionRouteRefusedTagEmitsNothing(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	useFakeSessionTags(t, s)
	hub, ch := newTagEventHub(t)
	s.hub = hub
	fullID := startTagsSession(t, s, `{"task":"plain","project_dir":"`+t.TempDir()+`"}`)
	recvTagUpdateFrames(t, ch)

	w := sessionTagsCall(t, s, "", fullID, "no-such-tag")
	if w.Code == http.StatusOK {
		t.Fatalf("unknown tag accepted: %d %s", w.Code, w.Body.String())
	}
	if got := recvTagUpdateFrames(t, ch); len(got) != 0 {
		t.Errorf("refused tag set emitted %d tag_update frames, want 0", len(got))
	}
}
