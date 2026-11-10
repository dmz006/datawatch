// Release tags read: GET /api/autonomous/release-tags lists the release tags
// (vX.Y.Z) of the base repository, oldest first. Read-only. It feeds the
// 30-Day Activity dashboard card, which marks each release on the month.
// The repository is the one the Automata merge into (the manager's
// MergeBaseRepo); with none configured the list is empty, not an error.

package server

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/git"
)

// releaseTagNameRe matches a plain release tag: v, then X.Y.Z. Pre-release
// and other tags are left out of the card.
var releaseTagNameRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// releaseTagView is one release tag: its name, the commit it points at, and
// the tag date (creator date) in UTC, as RFC3339 and as YYYY-MM-DD.
type releaseTagView struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
	At     string `json:"at"`
	Date   string `json:"date"`
}

// releaseTagsResponse is the body of GET /api/autonomous/release-tags.
type releaseTagsResponse struct {
	Tags []releaseTagView `json:"tags"`
}

// shippedDayCounts counts shipped records per UTC merge day (YYYY-MM-DD),
// split by kind ("prds" or "automations"). It is the by_day field of
// GET /api/autonomous/shipped/counts.
type shippedDayCounts map[string]map[string]int

// add counts one shipped record of kind on the UTC day of at. A nil time
// (no merge time recorded) is not counted on any day.
func (c shippedDayCounts) add(at *time.Time, kind string) {
	if at == nil {
		return
	}
	d := at.UTC().Format("2006-01-02")
	if c[d] == nil {
		c[d] = map[string]int{"prds": 0, "automations": 0}
	}
	c[d][kind]++
}

// baseRepoReader is the optional manager view that names the base repository.
type baseRepoReader interface {
	MergeBaseRepo() string
}

// handleAutonomousReleaseTags serves GET /api/autonomous/release-tags.
func (s *Server) handleAutonomousReleaseTags(w http.ResponseWriter, r *http.Request) {
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
	repo := ""
	if rr, ok := s.autonomousMgr.(baseRepoReader); ok {
		repo = strings.TrimSpace(rr.MergeBaseRepo())
	}
	tags, err := listReleaseTags(r.Context(), repo)
	if err != nil {
		http.Error(w, "release tags: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, releaseTagsResponse{Tags: tags})
}

// listReleaseTags reads the vX.Y.Z tags of repo, oldest first. An empty repo
// path yields an empty list.
func listReleaseTags(ctx context.Context, repo string) ([]releaseTagView, error) {
	out := []releaseTagView{}
	if repo == "" {
		return out, nil
	}
	raw, err := git.Run(ctx, repo, "for-each-ref",
		"--format=%(refname:short)%09%(objectname)%09%(creatordate:iso-strict)", "refs/tags/")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if len(f) != 3 || !releaseTagNameRe.MatchString(f[0]) {
			continue
		}
		at, err := time.Parse(time.RFC3339, f[2])
		if err != nil {
			continue
		}
		at = at.UTC()
		out = append(out, releaseTagView{
			Name:   f[0],
			Commit: f[1],
			At:     at.Format(time.RFC3339),
			Date:   at.Format("2006-01-02"),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out, nil
}
