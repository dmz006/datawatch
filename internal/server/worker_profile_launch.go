package server

import (
	"context"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/claudecode"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// workerProfileLaunchFn returns a per-session launch function that applies
// the worker profile to this one claude-code launch, or nil when no profile
// was sent or the effective backend is not claude-code. The shared backend
// instance is never mutated, so concurrent sessions each carry their own
// profile. When the profile cannot be applied the launch starts with no
// profile (see claudecode.LaunchWithProfileFallback) and the reason is written
// to fallbackReason, which the caller owns; it may be nil.
func (s *Server) workerProfileLaunchFn(p *workerprofile.WorkerProfile, backendName string, fallbackReason *string) session.LaunchFunc {
	cc, profile, ok := s.profileLaunchBackend(p, backendName)
	if !ok {
		return nil
	}
	return func(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
		w := claudecode.WorkerProfileLaunch{
			Profile:    profile,
			MCPServers: channel.LaunchMCPServers(tmuxSession, projectDir),
			Starting:   autonomous.StartingContextInput{Role: "worker", TaskSpec: task},
		}
		reason, err := cc.LaunchWithProfileFallback(ctx, w, task, tmuxSession, projectDir, logFile)
		if reason != "" && fallbackReason != nil {
			*fallbackReason = reason
		}
		return err
	}
}

// scheduledSpawnProfile resolves the worker profile for a scheduled spawn
// (same resolution and launch function as the interactive start) and sets
// opts.WorkerProfile and opts.LaunchFn. It returns the fallback-reason
// pointer the launch fills in.
func (s *Server) scheduledSpawnProfile(ds session.DeferredSession, opts *session.StartOptions) *string {
	kind := sessionKindForLaunch(ds.LaunchRole, "", ds.OneShot)
	p, reason := resolveStartWorkerProfile(s.cfg, "", kind)
	fallback := reason
	if p != nil {
		opts.WorkerProfile = *p
	}
	opts.LaunchFn = s.workerProfileLaunchFn(p, ds.Backend, &fallback)
	return &fallback
}

// workerProfileResumeLaunchFn is the resume counterpart of
// workerProfileLaunchFn: a per-session resume launch function that applies
// the worker profile to this one claude-code resume (a start with a resume
// id, or a restart), or nil when no profile applies or the effective
// backend is not claude-code. Same session-mutates-shared-state guarantee as
// workerProfileLaunchFn: the shared backend is never mutated. When the
// profile cannot be applied the resume starts with no profile (see
// claudecode.LaunchResumeWithProfileFallback) and the reason is written to
// fallbackReason, which the caller owns; it may be nil.
func (s *Server) workerProfileResumeLaunchFn(p *workerprofile.WorkerProfile, backendName string, fallbackReason *string) session.ResumeLaunchFunc {
	cc, profile, ok := s.profileLaunchBackend(p, backendName)
	if !ok {
		return nil
	}
	return func(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
		w := claudecode.WorkerProfileLaunch{
			Profile:    profile,
			MCPServers: channel.LaunchMCPServers(tmuxSession, projectDir),
			Starting:   autonomous.StartingContextInput{Role: "worker", TaskSpec: task},
		}
		reason, err := cc.LaunchResumeWithProfileFallback(ctx, w, task, tmuxSession, projectDir, logFile, resumeID)
		if reason != "" && fallbackReason != nil {
			*fallbackReason = reason
		}
		return err
	}
}

// profileResumeFnForSession implements the manager's profile-resume hook
// (session.Manager.SetProfileResumeFn): it returns the resume launch
// function that re-applies the session's stored worker profile on resume,
// or nil when the session has no stored profile (or its profile already
// fell back at first launch, so whatever is actually running is
// profile-less and the resume must keep it that way). Nil preserves the
// manager's previous profile-less resume behaviour, so non-profile sessions
// are byte-for-byte unchanged.
func (s *Server) profileResumeFnForSession(sess *session.Session) session.ResumeLaunchFunc {
	if sess == nil || sess.WorkerProfile == nil || sess.ProfileFallbackReason != "" {
		return nil
	}
	return s.workerProfileResumeLaunchFn(sess.WorkerProfile, sess.BackendFamily, &sess.ProfileFallbackReason)
}

// profileLaunchBackend resolves the claude-code backend for a profiled
// launch plus the captured profile, or !ok when the profile does not apply
// (no profile, missing backend, or a backend that is not claude-code).
func (s *Server) profileLaunchBackend(p *workerprofile.WorkerProfile, backendName string) (*claudecode.Backend, workerprofile.WorkerProfile, bool) {
	if p == nil || p.Name == "" {
		return nil, workerprofile.WorkerProfile{}, false
	}
	if backendName == "" && s.cfg != nil {
		backendName = s.cfg.Session.LLMBackend
	}
	b, err := llm.Get(backendName)
	if err != nil {
		return nil, workerprofile.WorkerProfile{}, false
	}
	cc, ok := b.(*claudecode.Backend)
	if !ok {
		return nil, workerprofile.WorkerProfile{}, false
	}
	return cc, *p, true
}

// startWorkerProfileValue returns the profile for StartOptions.WorkerProfile
// (the zero value when none applies).
func startWorkerProfileValue(p *workerprofile.WorkerProfile) workerprofile.WorkerProfile {
	if p == nil {
		return workerprofile.WorkerProfile{}
	}
	return *p
}
