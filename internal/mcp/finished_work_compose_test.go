package mcp

import (
	"context"
	"net/http"
	"testing"
)

func TestPRDCancelToolTakesOutcomeAndDecision(t *testing.T) {
	var query string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Del("view") // the proxy asks for the compact view on every call
		query = q.Encode()
		_, _ = w.Write([]byte(`{"status":"cancelled"}`))
	})
	args := map[string]any{"id": "abcd0001", "outcome": "superseded", "delivered_via": "ff00", "decision": "drop", "reason": "replaced"}
	if _, err := s.handleAutonomousPRDCancel(context.Background(), callReq(args)); err != nil {
		t.Fatal(err)
	}
	if want := "decision=drop&delivered_via=ff00&outcome=superseded&reason=replaced"; query != want {
		t.Errorf("query = %s, want %s", query, want)
	}
	props := s.toolAutonomousPRDCancel().InputSchema.Properties
	for _, k := range []string{"outcome", "delivered_via", "note", "decision", "reason"} {
		if _, ok := props[k]; !ok {
			t.Errorf("autonomous_prd_cancel lacks the %s argument", k)
		}
	}
}
