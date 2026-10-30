// Repo CI watch read route: GET /api/autonomous/repo-ci-watch.
//
// The body shows the whole repo CI watch state at once: the mode in
// effect (`mode`: off, shadow or on), the alert threshold (`alert_minutes`,
// whole minutes, 0 while the watch is off), the configured repository
// paths (`repos`), the instant the coordinator last polled
// (`last_poll_at`, the zero time when it never has), and the stored
// watch records (`records`, newest first). `repo` narrows `records` to
// one repository path, `status` to one watch status; unknown statuses
// are refused with a 400 that names the allowed values, a read that
// matches nothing is 200 with an empty list — never a 404.
//
// The route is read-only: writes to the records go through the
// coordinator's pass alone, which is why the narrow reader interface
// here exposes a single getter — the read view — and nothing else, and
// why no request method other than GET is accepted. A federated peer
// needs the autonomous read capability (federation.CapAutonomousRead)
// to call it.
package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// repoCIWatchReader is the narrow read surface of the orchestrator's
// autonomous manager this route uses: the repo CI watch's current read
// view (mode, repositories, alert threshold, last poll instant,
// records). The full *autonomous.API satisfies it by its
// RepoCIWatchStatus accessor, so the fake in the tests can stand in
// without the manager.
type repoCIWatchReader interface {
	RepoCIWatchStatus() autonomous.RepoCIWatchStatus
}

// repoCIWatchStatusQuoted renders the record statuses as a
// human-readable comma list — `green, failing, pending, hung, ...` —
// for the 400 that names them.
func repoCIWatchStatusQuoted() string {
	quoted := make([]string, len(autonomous.RepoCIWatchStatuses))
	for i, s := range autonomous.RepoCIWatchStatuses {
		quoted[i] = `"` + s + `"`
	}
	return strings.Join(quoted, ", ")
}

// handleRepoCIWatch serves GET /api/autonomous/repo-ci-watch. The body
// is the read view above, filtered server-side by the optional `repo`
// and `status` query parameters; `records` is newest first (last
// updated first) regardless of the store's order.
func (s *Server) handleRepoCIWatch(w http.ResponseWriter, r *http.Request) {
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
	tr, ok := s.autonomousMgr.(repoCIWatchReader)
	if !ok {
		http.Error(w, "repo ci watch unavailable", http.StatusServiceUnavailable)
		return
	}

	repo := r.URL.Query().Get("repo")
	status := r.URL.Query().Get("status")
	if status != "" && !autonomous.ValidRepoCIWatchStatus(status) {
		http.Error(w, "status must be "+repoCIWatchStatusQuoted(), http.StatusBadRequest)
		return
	}

	snap := tr.RepoCIWatchStatus()
	out := make([]*autonomous.RepoCIWatchRecord, 0, len(snap.Records))
	for _, rec := range snap.Records {
		if repo != "" && rec.Repo != repo {
			continue
		}
		if status != "" && rec.Status != status {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Key() < out[j].Key()
	})

	writeJSONOK(w, struct {
		Mode         string                          `json:"mode"`
		AlertMinutes int                             `json:"alert_minutes"`
		Repos        []string                        `json:"repos"`
		LastPollAt   time.Time                       `json:"last_poll_at"`
		Records      []*autonomous.RepoCIWatchRecord `json:"records"`
	}{
		Mode:         snap.Mode,
		AlertMinutes: snap.AlertMinutes,
		Repos:        snap.Repos,
		LastPollAt:   snap.LastPollAt,
		Records:      out,
	})
}
