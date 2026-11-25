package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// The offline stop-all records when and by whom each session was ended. tmux is
// a stub on PATH, so no real tmux server is touched.
func TestRunSessionStopAll_StampsTheEndTimeAndNote(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	cfg := &config.Config{DataDir: t.TempDir()}
	path := filepath.Join(cfg.DataDir, "sessions.json")
	store, err := session.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&session.Session{ID: "aa11", FullID: "h-aa11", TmuxSession: "cs-stub-aa11", State: session.StateRunning}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	before := time.Now()
	if err := runSessionStopAll(cfg); err != nil {
		t.Fatal(err)
	}
	after, err := session.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = after.Close() }()
	list := after.List()
	if len(list) != 1 || list[0].State != session.StateKilled {
		t.Fatalf("sessions = %+v", list)
	}
	s := list[0]
	if s.EndedAt == nil || s.EndedAt.Before(before) {
		t.Errorf("ended_at = %v, want a time from this run", s.EndedAt)
	}
	if !strings.HasPrefix(s.EndNote, "killed by cli.session-stop-all: stop-all CLI:") {
		t.Errorf("end_note = %q", s.EndNote)
	}
}

// A council persona session records its end when its answer or failure lands.
func TestFinishCouncilSession_StampsTheEnd(t *testing.T) {
	for _, tc := range []struct {
		response, errMsg string
		want             session.State
	}{
		{"an answer", "", session.StateComplete},
		{"", "model timed out", session.StateFailed},
	} {
		s := &session.Session{State: session.StateRunning}
		finishCouncilSession(s, tc.response, tc.errMsg)
		if s.State != tc.want || s.EndedAt == nil || !strings.HasPrefix(s.EndNote, string(tc.want)+" by council.persona: ") {
			t.Errorf("state %q, ended_at %v, note %q; want %q stamped", s.State, s.EndedAt, s.EndNote, tc.want)
		}
	}
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "finishCouncilSession(sess, response, errMsg)") {
		t.Error("the council wiring does not use finishCouncilSession")
	}
}
