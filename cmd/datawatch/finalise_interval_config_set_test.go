package main

// `datawatch config set session.finalise_interval_minutes <n>` sends the dotted
// key and the number to the daemon's config endpoint. Zero means "no scheduled
// finalise runs", so it must reach the daemon as a real 0 and not be dropped as
// an empty value.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const finaliseIntervalKey = "session.finalise_interval_minutes"

// finaliseIntervalRecorder is a stand-in for PUT /api/config that records every
// request body it receives.
type finaliseIntervalRecorder struct {
	bodies []string
}

func newFinaliseIntervalRecorder(t *testing.T) *finaliseIntervalRecorder {
	t.Helper()
	rec := &finaliseIntervalRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		rec.bodies = append(rec.bodies, string(raw))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return rec
}

// sentValue runs config set and returns the decoded value sent for the key.
func (rec *finaliseIntervalRecorder) sentValue(t *testing.T, value string) any {
	t.Helper()
	if err := runConfigSet(&config.Config{}, finaliseIntervalKey, value); err != nil {
		t.Fatalf("runConfigSet(%s, %s): %v", finaliseIntervalKey, value, err)
	}
	if len(rec.bodies) != 1 {
		t.Fatalf("PUT count = %d, want 1 (bodies: %v)", len(rec.bodies), rec.bodies)
	}
	var patch map[string]any
	if err := json.Unmarshal([]byte(rec.bodies[0]), &patch); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.bodies[0], err)
	}
	if len(patch) != 1 {
		t.Fatalf("body %q carries %d keys, want exactly the dotted key", rec.bodies[0], len(patch))
	}
	got, ok := patch[finaliseIntervalKey]
	if !ok {
		t.Fatalf("body %q lacks the dotted key %q", rec.bodies[0], finaliseIntervalKey)
	}
	return got
}

func TestConfigSetFinaliseIntervalSendsDottedKey(t *testing.T) {
	rec := newFinaliseIntervalRecorder(t)
	got := rec.sentValue(t, "15")
	if n, ok := got.(float64); !ok || n != 15 {
		t.Errorf("sent value = %#v, want the number 15", got)
	}
}

func TestConfigSetFinaliseIntervalZeroIsSent(t *testing.T) {
	rec := newFinaliseIntervalRecorder(t)
	got := rec.sentValue(t, "0")
	if n, ok := got.(float64); !ok || n != 0 {
		t.Errorf("sent value = %#v, want the number 0 (not dropped or sent as an empty string)", got)
	}
}
