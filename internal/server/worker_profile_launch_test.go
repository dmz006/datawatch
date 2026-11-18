package server

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

func TestResolveStartWorkerProfile_KindDefaults(t *testing.T) {
	cfg := &config.Config{}
	for kind, want := range map[string]string{
		workerprofile.KindSupervisor: "datawatch-tools",
		workerprofile.KindPlanner:    "full",
		workerprofile.KindReview:     "minimal",
		workerprofile.KindOneShot:    "minimal",
	} {
		p, reason := resolveStartWorkerProfile(cfg, "", kind)
		if p == nil || p.Name != want || reason != "" {
			t.Fatalf("kind %s: got %+v %q want %s", kind, p, reason, want)
		}
	}
	if p, r := resolveStartWorkerProfile(cfg, "", ""); p != nil || r != "" {
		t.Fatalf("no kind, no name must give no profile: %+v %q", p, r)
	}
}

func TestResolveStartWorkerProfile_SessionKindMapping(t *testing.T) {
	if k := sessionKindForLaunch("supervisor", "", false); k != workerprofile.KindSupervisor {
		t.Fatalf("supervisor -> %q", k)
	}
	if k := sessionKindForLaunch("worker", "review", false); k != workerprofile.KindReview {
		t.Fatalf("review -> %q", k)
	}
	if k := sessionKindForLaunch("worker", "task", false); k != "" {
		t.Fatalf("worker task -> %q", k)
	}
}

func TestResolveStartWorkerProfile_NamedAndOverride(t *testing.T) {
	cfg := &config.Config{}
	p, _ := resolveStartWorkerProfile(cfg, "digest", workerprofile.KindSupervisor)
	if p == nil || p.Name != "digest" {
		t.Fatalf("named profile must beat the kind default: %+v", p)
	}
	cfg.Autonomous.WorkerProfileDefaults.Kind = map[string]string{workerprofile.KindSupervisor: "minimal"}
	p, _ = resolveStartWorkerProfile(cfg, "", workerprofile.KindSupervisor)
	if p == nil || p.Name != "minimal" {
		t.Fatalf("configured kind default must win: %+v", p)
	}
}

func TestResolveStartWorkerProfile_UnknownNameFallsBack(t *testing.T) {
	p, reason := resolveStartWorkerProfile(&config.Config{}, "nope", workerprofile.KindSupervisor)
	if p != nil {
		t.Fatalf("unknown name must not apply a profile: %+v", p)
	}
	if !strings.Contains(reason, "nope") || !strings.Contains(reason, "full harness") {
		t.Fatalf("reason = %q", reason)
	}
}
