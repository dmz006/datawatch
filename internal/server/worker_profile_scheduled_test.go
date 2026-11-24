package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// A scheduled supervisor spawn resolves a profile for kind supervisor and
// reports an unresolvable one as a fallback reason, without touching settings.
func TestScheduledSpawnProfile_SupervisorKindResolved(t *testing.T) {
	cfg := &config.Config{}
	cfg.Autonomous.WorkerProfileDefaults.Kind = map[string]string{"supervisor": "datawatch-tools"}
	s := &Server{cfg: cfg}
	opts := &session.StartOptions{}
	reason := s.scheduledSpawnProfile(session.DeferredSession{LaunchRole: "supervisor", OneShot: true}, opts)
	if reason == nil {
		t.Fatal("nil reason pointer")
	}
	if opts.WorkerProfile.Name == "" && *reason == "" {
		t.Fatal("neither profile nor fallback reason recorded")
	}
	if opts.WorkerProfile.Name != "" && opts.WorkerProfile.Name != "datawatch-tools" {
		t.Fatalf("profile = %q", opts.WorkerProfile.Name)
	}
}
