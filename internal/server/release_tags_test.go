// Tests for GET /api/autonomous/release-tags (release_tags.go) and the
// by_day field of GET /api/autonomous/shipped/counts. The tag test builds a
// real throwaway git repository in a temp directory; no daemon is started.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// tagFleet is the fake manager with a base repository, so the handler reads
// that repository's tags.
type tagFleet struct {
	shipFleet
	repo string
}

func (f tagFleet) MergeBaseRepo() string { return f.repo }

// gitIn runs one git command in dir and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// newTagRepo makes a repository with one commit and the given tags. Each tag
// is annotated with the fixed date given, so the creator date is exact.
func newTagRepo(t *testing.T, tags map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	for name, when := range tags {
		cmd := exec.Command("git", "tag", "-a", name, "-m", name)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_COMMITTER_DATE="+when, "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git tag %s: %v: %s", name, err, out)
		}
	}
	return dir
}

func doReleaseTagsGET(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleAutonomousReleaseTags(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/release-tags", nil))
	return rec
}

// TestReleaseTags_ListsVersionTagsOldestFirst: only vX.Y.Z tags are listed,
// oldest first, each with its UTC date and commit.
func TestReleaseTags_ListsVersionTagsOldestFirst(t *testing.T) {
	repo := newTagRepo(t, map[string]string{
		"v9.60.0":     "2026-11-06T09:00:00Z",
		"v9.54.0":     "2026-11-02T18:30:00Z",
		"v9.54.0-rc1": "2026-11-01T10:00:00Z", // pre-release: not a release tag
		"nightly":     "2026-11-03T10:00:00Z", // not vX.Y.Z: skipped
	})
	s := &Server{autonomousMgr: &tagFleet{repo: repo}, cfg: config.DefaultConfig()}
	rec := doReleaseTagsGET(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET release-tags = %d: %s", rec.Code, rec.Body.String())
	}
	var body releaseTagsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tags) != 2 {
		t.Fatalf("tags = %+v; want exactly v9.54.0 and v9.60.0", body.Tags)
	}
	if body.Tags[0].Name != "v9.54.0" || body.Tags[0].Date != "2026-11-02" {
		t.Errorf("first tag = %+v; want v9.54.0 on 2026-11-02", body.Tags[0])
	}
	if body.Tags[1].Name != "v9.60.0" || body.Tags[1].Date != "2026-11-06" {
		t.Errorf("second tag = %+v; want v9.60.0 on 2026-11-06", body.Tags[1])
	}
	if body.Tags[0].Commit == "" || body.Tags[0].At == "" {
		t.Errorf("tag missing commit or at: %+v", body.Tags[0])
	}
}

// TestReleaseTags_NoRepoIsEmpty: with no base repository configured the
// endpoint answers an empty list, not an error.
func TestReleaseTags_NoRepoIsEmpty(t *testing.T) {
	s := &Server{autonomousMgr: &tagFleet{}, cfg: config.DefaultConfig()}
	rec := doReleaseTagsGET(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET release-tags = %d: %s", rec.Code, rec.Body.String())
	}
	var body releaseTagsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Tags == nil || len(body.Tags) != 0 {
		t.Errorf("tags = %#v; want an empty list", body.Tags)
	}
}

// TestReleaseTags_RejectsWritesAndNoManager: GET only, and 503 with no
// autonomous manager.
func TestReleaseTags_RejectsWritesAndNoManager(t *testing.T) {
	s := &Server{autonomousMgr: &tagFleet{}, cfg: config.DefaultConfig()}
	rec := httptest.NewRecorder()
	s.handleAutonomousReleaseTags(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/release-tags", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d; want 405", rec.Code)
	}
	none := &Server{cfg: config.DefaultConfig()}
	rec = doReleaseTagsGET(t, none)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no manager = %d; want 503", rec.Code)
	}
}

// TestListReleaseTags_MissingRepoIsError: a configured path that is not a
// git repository is reported as an error, not silently empty.
func TestListReleaseTags_MissingRepoIsError(t *testing.T) {
	if _, err := listReleaseTags(context.Background(), t.TempDir()+"/nope"); err == nil {
		t.Fatal("listReleaseTags on a missing directory = nil error; want an error")
	}
}

// dayFleet is a fake manager whose PRDs and Automations carry merge times,
// for the shipped by_day field.
type dayFleet struct {
	shipFleet
}

func (dayFleet) ListPRDs() []any {
	late := time.Date(2026, 11, 5, 23, 30, 0, 0, time.UTC)
	early := time.Date(2026, 11, 5, 0, 15, 0, 0, time.UTC)
	other := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	return []any{
		&autonomous.PRD{ID: "p-late", Status: autonomous.PRDCompleted, MergeCommit: "c1", MergedAt: &late},
		&autonomous.PRD{ID: "p-early", Status: autonomous.PRDCompleted, MergeCommit: "c2", MergedAt: &early},
		&autonomous.PRD{ID: "p-other", Status: autonomous.PRDCompleted, MergeCommit: "c3", MergedAt: &other},
		&autonomous.PRD{ID: "p-unshipped", Status: autonomous.PRDCompleted},
		// Shipped but no merge time: counted in shipped state, on no day.
		&autonomous.PRD{ID: "p-untimed", Status: autonomous.PRDCompleted, MergeCommit: "c4"},
	}
}

func (dayFleet) ListAutomations() []any {
	at := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	return []any{
		&autonomous.Automation{ID: "a-1", Status: autonomous.AutomationActive, MergeCommit: "c9", MergedAt: &at},
	}
}

// TestShippedCounts_ByDay: shipped records are grouped under the UTC merge
// day, split by kind; unshipped and untimed records are not on any day.
func TestShippedCounts_ByDay(t *testing.T) {
	s := &Server{autonomousMgr: &dayFleet{}, cfg: config.DefaultConfig()}
	rec := doShipGET(t, s, "/api/autonomous/shipped/counts")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET shipped/counts = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		ByDay map[string]map[string]int `json:"by_day"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]map[string]int{
		"2026-11-05": {"prds": 2, "automations": 1},
		"2026-11-07": {"prds": 1, "automations": 0},
	}
	if !reflect.DeepEqual(body.ByDay, want) {
		t.Errorf("by_day = %v; want %v", body.ByDay, want)
	}
}
