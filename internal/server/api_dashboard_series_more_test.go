package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

var moreNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func moreGet(t *testing.T, h http.HandlerFunc, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestReleaseTrainTagsCIAndDeploy(t *testing.T) {
	findings := []autonomous.StatusFinding{ // newest first
		{PRDID: "a", At: moreNow.Add(-time.Hour), Source: "ship_step", Text: "tag v1.2.0\ngit push"},
		{PRDID: "b", At: moreNow.Add(-48 * time.Hour), Source: "ship_step", Text: "tag v1.1.0\nFAILED: push"},
		{PRDID: "c", At: moreNow.Add(-1000 * time.Hour), Source: "ship_step", Text: "tag v0.1.0"},
	}
	ci := []*autonomous.RepoCIWatchRecord{
		{Repo: "o/r", Status: "green", UpdatedAt: moreNow.Add(-time.Hour)},
		{Repo: "o/r", Status: "failing", UpdatedAt: moreNow.Add(-2 * time.Hour)},
		{Repo: "o/r", Status: "superseded", UpdatedAt: moreNow.Add(-time.Hour)},
	}
	got := buildReleaseTrain(findings, ci, moreNow.AddDate(0, 0, -14), moreNow)
	if len(got.Tags) != 2 || got.Tags[0].Tag != "v1.2.0" || !got.Tags[0].OK || got.Tags[1].OK {
		t.Fatalf("tags: %+v", got.Tags)
	}
	if got.CIState != "failing" || len(got.CI) != 2 {
		t.Fatalf("ci: %s %+v", got.CIState, got.CI)
	}
	if got.Deploy.State != "ok" || got.Deploy.PRDID != "a" || got.Empty {
		t.Fatalf("deploy/empty: %+v", got)
	}
}

func TestReleaseTrainEmptyGivesReason(t *testing.T) {
	got := buildReleaseTrain(nil, nil, moreNow.AddDate(0, 0, -14), moreNow)
	if !got.Empty || got.EmptyReasonKey != seriesReasonReleaseTrain || got.CIState != "none" || got.Deploy.State != "none" {
		t.Fatalf("got %+v", got)
	}
	rec := moreGet(t, (&Server{}).handleDashboardSeriesReleaseTrain, "/api/dashboard/series/release-train")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestReleaseTrainBoundsAndMethod(t *testing.T) {
	s := &Server{}
	for _, q := range []string{"?days=0", "?days=91", "?days=x"} {
		if rec := moreGet(t, s.handleDashboardSeriesReleaseTrain, "/x"+q); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d", q, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	s.handleDashboardSeriesReleaseTrain(rec, httptest.NewRequest(http.MethodPost, "/x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post status %d", rec.Code)
	}
}

func TestReviewBacklogAgesAndOrdersOldestFirst(t *testing.T) {
	plans := []backlogPlan{
		{ID: "new", Since: moreNow.Add(-time.Hour)},
		{ID: "old", Since: moreNow.Add(-5 * time.Hour)},
		{ID: "mid", Since: moreNow.Add(-2 * time.Hour)},
	}
	qs := []backlogQuestion{{QuestionID: "q1", Since: moreNow.Add(-10 * time.Hour)}}
	got := buildReviewBacklog(plans, qs, 2, moreNow)
	if got.PlanCount != 3 || len(got.Plans) != 2 || got.Plans[0].ID != "old" || got.Plans[1].ID != "mid" {
		t.Fatalf("plans: %+v", got.Plans)
	}
	if got.Plans[0].AgeSeconds != 5*3600 || got.OldestAgeSeconds != 10*3600 || got.QuestionCount != 1 || got.Empty {
		t.Fatalf("got %+v", got)
	}
}

func TestReviewBacklogEmptyAndBounds(t *testing.T) {
	got := buildReviewBacklog(nil, nil, 50, moreNow)
	if !got.Empty || got.EmptyReasonKey != seriesReasonReviewBacklog || got.Plans == nil || got.Questions == nil {
		t.Fatalf("got %+v", got)
	}
	s := &Server{}
	if rec := moreGet(t, s.handleDashboardSeriesReviewBacklog, "/x?limit=201"); rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	rec := moreGet(t, s.handleDashboardSeriesReviewBacklog, "/x")
	var body struct {
		Plans []any `json:"plans"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Plans == nil {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestDefectsPerDayFoundAndFixed(t *testing.T) {
	recs := []autonomous.IntakeRecord{
		{At: moreNow, Route: autonomous.IntakeRouteTinyFix},
		{At: moreNow.Add(-time.Hour), Route: autonomous.IntakeRouteNewBatch},
		{At: moreNow, Route: autonomous.IntakeRouteTinyFix, ApplyError: "boom"},
		{At: moreNow.AddDate(0, 0, -2), Route: autonomous.IntakeRouteOpenBatch},
		{At: moreNow.AddDate(0, 0, -30), Route: autonomous.IntakeRouteTinyFix}, // outside the window
	}
	got := buildDefectsPerDay(recs, 3, moreNow)
	if len(got.Points) != 3 || got.Points[2].Date != "2026-11-10" || got.Points[0].Date != "2026-11-08" {
		t.Fatalf("points: %+v", got.Points)
	}
	if got.Points[2].Found != 3 || got.Points[2].Fixed != 1 || got.Points[0].Found != 1 || got.Points[1].Found != 0 {
		t.Fatalf("points: %+v", got.Points)
	}
	if got.TotalFound != 4 || got.TotalFixed != 1 || got.Empty {
		t.Fatalf("totals: %+v", got)
	}
}

func TestDefectsPerDayEmptyAndHandler(t *testing.T) {
	old := seriesDefectRecords
	defer func() { seriesDefectRecords = old }()
	seriesDefectRecords = func(*Server) []autonomous.IntakeRecord { return nil }
	s := &Server{}
	rec := moreGet(t, s.handleDashboardSeriesDefectsPerDay, "/x?days=7")
	var body defectsPerDayResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("status %d", rec.Code)
	}
	if !body.Empty || body.EmptyReasonKey != seriesReasonDefects || len(body.Points) != 7 {
		t.Fatalf("body %+v", body)
	}
	if rec := moreGet(t, s.handleDashboardSeriesDefectsPerDay, "/x?days=91"); rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}
