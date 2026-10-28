package main

import (
	"errors"
	"time"

	"github.com/dmz006/datawatch/internal/pilog"
	"github.com/dmz006/datawatch/internal/session"
)

// piTailProgress locates and reads the pi session log backing session s.
// It fails for non-pi sessions, sessions without a project dir, and pi
// sessions whose log has not been found or read yet — callers treat that as
// "no status available for now" and keep going.
func piTailProgress(s *session.Session) (pilog.Progress, error) {
	if s == nil {
		return pilog.Progress{}, errors.New("nil session")
	}
	root := piSessionsRoot()
	if root == "" || s.ProjectDir == "" {
		return pilog.Progress{}, errors.New("pi sessions root or project dir unavailable")
	}
	path, err := pilog.LocateLog(root, s.ProjectDir, s.CreatedAt)
	if err != nil {
		return pilog.Progress{}, err
	}
	return pilog.Read(path)
}

// piTailStatusLine returns the formatted one-line status for a pi session as
// of now (e.g. "progress: turns=7 tools=write:2,bash:5 last=bash idle=12s")
// and "" for non-pi sessions or when the log is unavailable. The line is an
// emission for the tail consumer only — it is never written into the
// session's stored output log that completion detection scans.
func piTailStatusLine(s *session.Session, now time.Time) string {
	if !session.IsPiBackendFamily(s.BackendFamily) {
		return ""
	}
	p, err := piTailProgress(s)
	if err != nil {
		return ""
	}
	return session.PiStatusLine(p, now)
}
