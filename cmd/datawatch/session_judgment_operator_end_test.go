package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// A one-shot session the operator ended is flagged on the no-output error, so
// the planner is not started again; a session the daemon ended is not.
func TestWaitOneShot_OperatorEndedSessionIsFlagged(t *testing.T) {
	useRunCapFastPoll(t)
	for note, want := range map[string]string{
		"killed by command.kill: from web":                       "command.kill",
		"killed by autonomous.verifier.idle-detector: idle 600s": "",
		"process gone, finalised":                                "",
	} {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/sessions/st-end", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `{"id":"st-end","state":"killed","end_note":%q}`, note)
		})
		mux.HandleFunc("/api/sessions/kill", func(w http.ResponseWriter, _ *http.Request) {})
		srv := httptest.NewServer(mux)
		pointLoopbackAt(t, srv.URL)
		dir := t.TempDir()
		cfg := &config.Config{DataDir: t.TempDir()}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, _, err := waitOneShotJSONSession(ctx, cfg, "p1", decomposeSessionName, "opencode", "m", dir,
			filepath.Join(dir, "out.json"), 20*time.Second, "st-end", time.Now(),
			func() (string, bool) { return "", false })
		cancel()
		srv.Close()
		var noOutput *autonomouspkg.OneShotNoOutputError
		if !errors.As(err, &noOutput) {
			t.Fatalf("note %q: err = %v, want the no-output error", note, err)
		}
		if noOutput.OperatorEndedBy != want {
			t.Errorf("note %q: OperatorEndedBy = %q, want %q", note, noOutput.OperatorEndedBy, want)
		}
	}
}
