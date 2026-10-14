// BL427 — the WS prd_update broadcast moved from a single-slot Manager
// callback onto a lifecycle dispatcher subscriber. This pins the frame a
// client receives from BroadcastPRDUpdate(autonomous.PRDUpdatePayload(ev))
// to the frame the legacy main.go closure produced (type + data bytes;
// the ts field is per-broadcast and excluded).

package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func recvPRDUpdateFrame(t *testing.T, ch chan []byte) WSMessage {
	t.Helper()
	select {
	case raw := <-ch:
		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("decode frame: %v (%s)", err, raw)
		}
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for prd_update frame")
	}
	return WSMessage{}
}

func TestBroadcastPRDUpdate_DispatcherFrameMatchesLegacy(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	ch := make(chan []byte, 16)
	hub.register <- &client{hub: hub, send: ch, subscribed: make(map[string]bool)}
	srv := &HTTPServer{hub: hub}

	cases := []struct {
		name   string
		legacy map[string]any // what the pre-dispatcher closure built
		ev     autonomous.LifecycleEvent
	}{
		{
			name:   "existing",
			legacy: map[string]any{"prd_id": "p1", "status": "running"},
			ev:     autonomous.LifecycleEvent{Kind: autonomous.KindPRDUpdated, Level: autonomous.LevelPRD, PRDID: "p1", NewStatus: "running"},
		},
		{
			name:   "deleted",
			legacy: map[string]any{"prd_id": "p2", "deleted": true},
			ev:     autonomous.LifecycleEvent{Kind: autonomous.KindPRDUpdated, Level: autonomous.LevelPRD, PRDID: "p2", Deleted: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv.BroadcastPRDUpdate(tc.legacy)
			want := recvPRDUpdateFrame(t, ch)
			srv.BroadcastPRDUpdate(autonomous.PRDUpdatePayload(tc.ev))
			got := recvPRDUpdateFrame(t, ch)

			if got.Type != MsgPRDUpdate || want.Type != MsgPRDUpdate {
				t.Fatalf("frame type got=%q want=%q", got.Type, want.Type)
			}
			if string(got.Data) != string(want.Data) {
				t.Errorf("frame data mismatch\n got: %s\nwant: %s", got.Data, want.Data)
			}
		})
	}
}
