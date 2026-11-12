// Security failure read route: GET /api/autonomous/security-failures.
//
// The body is a JSON list, newest first, with one entry for each repo CI
// watch record that carries a stored security failure report. An entry
// holds the record's identity — repository, ref kind and name, workflow,
// run ID, run URL and watch status — beside the SecurityFailureReport
// the security failure pass stored for the run, under `report`. `repo`
// narrows the list to one repository path, `status` to one watch status;
// an unknown status is refused with a 400 that names the allowed
// values, and a read that matches nothing is 200 with an empty list —
// never a 404.
//
// The route is read-only: the reports are written by the coordinator's
// security failure pass alone, which is why the narrow reader interface
// here exposes a single getter and why no request method other than GET
// is accepted. A federated peer needs the autonomous read capability
// (federation.CapAutonomousRead) to call it.
package server

import (
	"net/http"
	"sort"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// SecurityFailureReader is the narrow read surface the security failure
// route uses: the repo CI watch records the daemon currently stores,
// from which the route keeps those carrying a security failure report.
// The full *autonomous.API satisfies it, so the fake in the tests can
// stand in without the manager.
type SecurityFailureReader interface {
	ListRepoCIWatchRecords() []*autonomous.RepoCIWatchRecord
}

// SecurityFailureEntry is one entry of the security failure list: the
// run's identity from its repo CI watch record and the stored report.
type SecurityFailureEntry struct {
	Repo     string                           `json:"repo"`
	RefKind  string                           `json:"ref_kind"`
	RefName  string                           `json:"ref_name"`
	Workflow string                           `json:"workflow"`
	RunID    string                           `json:"run_id"`
	RunURL   string                           `json:"run_url"`
	Status   string                           `json:"status"`
	Report   autonomous.SecurityFailureReport `json:"report"`
}

// handleSecurityFailures serves GET /api/autonomous/security-failures.
func (s *Server) handleSecurityFailures(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	rd, ok := s.autonomousMgr.(SecurityFailureReader)
	if !ok {
		http.Error(w, "security failure reports unavailable", http.StatusServiceUnavailable)
		return
	}
	serveSecurityFailures(w, r, rd)
}

// SecurityFailureHandler returns the security failure list handler over
// the given reader (no manager, no federation gate), for surface tests
// outside this package.
func SecurityFailureHandler(rd SecurityFailureReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveSecurityFailures(w, r, rd)
	})
}

// serveSecurityFailures answers the list: GET only, filtered by the
// optional `repo` and `status` query parameters, newest first (last
// updated first, then by record key) whatever order the store yields.
func serveSecurityFailures(w http.ResponseWriter, r *http.Request, rd SecurityFailureReader) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	repo := r.URL.Query().Get("repo")
	status := r.URL.Query().Get("status")
	if status != "" && !autonomous.ValidRepoCIWatchStatus(status) {
		http.Error(w, "status must be "+repoCIWatchStatusQuoted(), http.StatusBadRequest)
		return
	}

	var recs []*autonomous.RepoCIWatchRecord
	for _, rec := range rd.ListRepoCIWatchRecords() {
		if rec == nil || rec.Security == nil {
			continue
		}
		if repo != "" && rec.Repo != repo {
			continue
		}
		if status != "" && rec.Status != status {
			continue
		}
		recs = append(recs, rec)
	}
	sort.Slice(recs, func(i, j int) bool {
		if !recs[i].UpdatedAt.Equal(recs[j].UpdatedAt) {
			return recs[i].UpdatedAt.After(recs[j].UpdatedAt)
		}
		return recs[i].Key() < recs[j].Key()
	})

	out := make([]SecurityFailureEntry, 0, len(recs))
	for _, rec := range recs {
		out = append(out, SecurityFailureEntry{
			Repo:     rec.Repo,
			RefKind:  rec.RefKind,
			RefName:  rec.RefName,
			Workflow: rec.Workflow,
			RunID:    rec.RunID,
			RunURL:   rec.RunURL,
			Status:   rec.Status,
			Report:   *rec.Security,
		})
	}
	writeJSONOK(w, out)
}
