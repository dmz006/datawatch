package main

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

func TestPriorityReachesInteractiveRequest(t *testing.T) {
	r := interactiveAdmitRequest("h", []string{"host"}, "", "m", 4)
	if r.Priority != 4 || !r.Interactive {
		t.Fatalf("interactive request = %+v", r)
	}
}

func TestPriorityReachesVerifierRequest(t *testing.T) {
	r := verifierAdmitRequest("verify:t", "prd", []string{"host"}, "", "m", 5)
	if r.Priority != 5 || r.Role != capacity.RoleVerifier {
		t.Fatalf("verifier request = %+v", r)
	}
}

func TestPriorityReachesCouncilRequest(t *testing.T) {
	r := councilAdmitRequest("run-p", "", []string{"host"}, "", "m", 6)
	if r.Priority != 6 {
		t.Fatalf("council request = %+v", r)
	}
}

func TestPriorityFromOwningSession(t *testing.T) {
	mgr, err := session.NewManager("h", t.TempDir(), "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := mgr.SaveSession(&session.Session{ID: "p002", FullID: "h-p002", Priority: 9, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if got := sessionPriority(mgr, "p002"); got != 9 {
		t.Fatalf("sessionPriority = %d, want 9", got)
	}
	// No owning session: priority stays 0.
	for _, id := range []string{"", "missing"} {
		if got := sessionPriority(mgr, id); got != 0 {
			t.Fatalf("sessionPriority(%q) = %d, want 0", id, got)
		}
	}
	if got := sessionPriority(nil, "p002"); got != 0 {
		t.Fatalf("nil manager priority = %d", got)
	}
}
