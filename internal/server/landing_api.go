// Landing state over REST: the landing record itself already rides on every
// Automaton read (the PRD's landing field — state, commit, actor, at,
// reason), so the only new endpoints are the operator keeps and drops, as
// sub-actions under /api/autonomous/prds/{id}/landing/keep|drop.

package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// landingDecider is the optional manager surface behind the landing
// keep/drop handlers (Manager.KeepLanding / Manager.DropLanding in
// internal/autonomous/landing.go). Server test doubles that implement only
// AutonomousAPI report 501 for landing decisions instead of breaking at
// compile time — the same pattern as the other per-PRD manager methods
// (planCoverageAckner and friends).
type landingDecider interface {
	KeepLanding(id, actor, reason string) (*autonomous.PRD, error)
	DropLanding(id, actor, reason string) (*autonomous.PRD, error)
}

// LandingRESTOperation is one landing operation on the REST surface.
type LandingRESTOperation struct {
	Name   string // the PRD sub-action: landing/keep or landing/drop
	Path   string // /api/autonomous/prds/{id}/landing/keep
	Method string // POST
}

// landingRESTOperations is the single list of landing REST operations, so
// the parity test enumerates it instead of re-listing the endpoints by hand.
var landingRESTOperations = []LandingRESTOperation{
	{"landing/keep", "/api/autonomous/prds/{id}/landing/keep", http.MethodPost},
	{"landing/drop", "/api/autonomous/prds/{id}/landing/drop", http.MethodPost},
}

// LandingRESTOperations returns the landing REST operations.
func LandingRESTOperations() []LandingRESTOperation {
	return append([]LandingRESTOperation(nil), landingRESTOperations...)
}
