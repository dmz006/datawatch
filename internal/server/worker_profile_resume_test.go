package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/claudecode"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// The manager consults the profile-resume hook (SetProfileResumeFn) before
// falling back to the backend's plain LaunchResume. The hook must only hand
// out a resume function for sessions that actually carry an applied
// datawatch MCP profile: a session with no profile, a session whose profile
// fell back at first launch, and a non-claude-code session can all have
// nothing sensible to profile-resume — nil keeps the manager's previous
// profile-less resume behaviour, byte-for-byte.

func TestProfileResumeFnForSession_NilWithoutProfile(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	if fn := s.profileResumeFnForSession(&session.Session{}); fn != nil {
		t.Error("no stored profile must give nil")
	}
	if fn := s.profileResumeFnForSession(nil); fn != nil {
		t.Error("nil session must give nil")
	}
}

func TestProfileResumeFnForSession_NilWhenFellBack(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	sess := &session.Session{
		BackendFamily:         "claude-code",
		WorkerProfile:         builtinDatawatchTools(),
		ProfileFallbackReason: "unknown profile at launch; role default applied",
	}
	if fn := s.profileResumeFnForSession(sess); fn != nil {
		t.Error("a profile that fell back must resume profile-less: got non-nil")
	}
}

func TestProfileResumeFnForSession_NilForNonClaudeBackend(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	sess := &session.Session{
		BackendFamily: "no-such-backend-xyz",
		WorkerProfile: builtinDatawatchTools(),
	}
	if fn := s.profileResumeFnForSession(sess); fn != nil {
		t.Error("an unresolvable backend must give nil")
	}
}

func TestProfileResumeFnForSession_Applied(t *testing.T) {
	// Registry slot save/restore: a sibling server test registers a fake
	// claude-code backend (every real one shares the name "claude-code"),
	// which would fail the *claudecode.Backend assertion. Put the real one in
	// for this test and hand the slot back afterwards.
	var prev llm.Backend
	if b, err := llm.Get("claude-code"); err == nil {
		prev = b
	}
	llm.Register(claudecode.New(""))
	if prev != nil {
		t.Cleanup(func() { llm.Register(prev) })
	}

	s := &Server{cfg: &config.Config{}}
	sess := &session.Session{
		BackendFamily: "claude-code",
		WorkerProfile: builtinDatawatchTools(),
	}
	if fn := s.profileResumeFnForSession(sess); fn == nil {
		t.Fatal("applied datawatch profile on claude-code must give a resume function")
	}
}

func TestProfileLaunchBackend_NotClaudeCode(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	_, _, ok := s.profileLaunchBackend(builtinDatawatchTools(), "no-such-backend-xyz")
	if ok {
		t.Error("unresolvable backend must not apply a profile")
	}
	_, _, ok = s.profileLaunchBackend(nil, "claude-code")
	if ok {
		t.Error("nil profile must not apply")
	}
	_, _, ok = s.profileLaunchBackend(&workerprofile.WorkerProfile{Name: ""}, "claude-code")
	if ok {
		t.Error("empty profile name must not apply")
	}
}

// builtinDatawatchTools is a pointer to a copy of the built-in profile, for
// the session record's WorkerProfile field.
func builtinDatawatchTools() *workerprofile.WorkerProfile {
	p, ok := workerprofile.Builtin("datawatch-tools")
	if !ok {
		panic("datawatch-tools builtin missing")
	}
	return &p
}
