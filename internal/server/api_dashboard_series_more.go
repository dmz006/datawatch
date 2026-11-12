// api_dashboard_series_more.go — three more series endpoints for dashboard cards:
//
//	GET /api/dashboard/series/release-train?days=14     autonomous:read
//	GET /api/dashboard/series/review-backlog?limit=50   autonomous:read
//	GET /api/dashboard/series/defects-per-day?days=14   autonomous:read
//
// Every window is bounded: release-train and defects-per-day take days from 1
// to 90 (default 14); review-backlog takes a limit from 1 to 200 (default 50)
// on each of its two lists.
//
// Release train reads the ship status (the push and tag outcomes recorded on
// the Automata's triage records) and the repository CI watch. It reports the
// tags cut in the window, the CI state per watched run and the deploy state,
// which is the newest recorded push outcome.
//
// Review backlog lists the plans waiting for a decision (the attention list:
// Automata in an attention status or holding a task for review) and the open
// held questions, each with its age.
//
// Defects per day buckets the defect intake decision log by UTC day: a defect
// is found on the day its intake record was written, and fixed on that day
// when the intake applied a tiny fix without error.
//
// Each response carries empty and, when empty, empty_reason_key, the locale key
// of the one line a card shows to say why it is hidden. A card that is off,
// empty or never is not shown.
package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// Bounds for the series endpoints in this file.
const (
	seriesMoreDefaultDays  = 14
	seriesMoreMaxDays      = 90
	seriesMoreDefaultLimit = 50
	seriesMoreMaxLimit     = 200
)

// Locale keys of the one-line reasons a card is hidden.
const (
	seriesReasonReleaseTrain  = "dash_series_release_train_none"
	seriesReasonReviewBacklog = "dash_series_review_backlog_none"
	seriesReasonDefects       = "dash_series_defects_none"
)

// seriesDefectRecords reads the defect intake decision records. Tests swap it.
var seriesDefectRecords = func(s *Server) []autonomous.IntakeRecord {
	l, ok := s.defectLog.(*autonomous.FileIntakeLog)
	if !ok || l == nil || l.Path == "" {
		return nil
	}
	f, err := os.Open(l.Path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []autonomous.IntakeRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var r autonomous.IntakeRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil && !r.At.IsZero() {
			out = append(out, r)
		}
	}
	return out
}

// registerDashboardSeriesMoreRoutes mounts the three routes.
func (s *Server) registerDashboardSeriesMoreRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/dashboard/series/release-train", s.handleDashboardSeriesReleaseTrain)
	mux.HandleFunc("/api/dashboard/series/review-backlog", s.handleDashboardSeriesReviewBacklog)
	mux.HandleFunc("/api/dashboard/series/defects-per-day", s.handleDashboardSeriesDefectsPerDay)
}

// seriesMoreGate checks the method and the capability.
func (s *Server) seriesMoreGate(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return s.fedCap(w, r, federation.CapAutonomousRead)
}

// seriesIntParam reads a whole-number query value in [1, hi].
func seriesIntParam(r *http.Request, name string, def, hi int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > hi {
		return 0, fmt.Errorf("bad request: %s: must be a whole number from 1 to %d", name, hi)
	}
	return n, nil
}

// ---- release train ----

type releaseTag struct {
	At    time.Time `json:"at"`
	PRDID string    `json:"prd_id"`
	Tag   string    `json:"tag"`
	OK    bool      `json:"ok"`
}

type releaseCIRun struct {
	Repo      string    `json:"repo"`
	RefKind   string    `json:"ref_kind"`
	RefName   string    `json:"ref_name"`
	Workflow  string    `json:"workflow"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type releaseDeploy struct {
	State string    `json:"state"` // ok | failed | none
	At    time.Time `json:"at,omitempty"`
	PRDID string    `json:"prd_id,omitempty"`
}

type releaseTrainResponse struct {
	GeneratedAt    time.Time      `json:"generated_at"`
	Days           int            `json:"days"`
	WindowFrom     time.Time      `json:"window_from"`
	Tags           []releaseTag   `json:"tags"`
	CIState        string         `json:"ci_state"` // failing | pending | green | none
	CI             []releaseCIRun `json:"ci"`
	Deploy         releaseDeploy  `json:"deploy"`
	Empty          bool           `json:"empty"`
	EmptyReasonKey string         `json:"empty_reason_key,omitempty"`
}

var releaseTagRe = regexp.MustCompile(`\btag (\S+)`)

// buildReleaseTrain assembles the response from ship findings and CI watch records.
func buildReleaseTrain(findings []autonomous.StatusFinding, ci []*autonomous.RepoCIWatchRecord, from, now time.Time) releaseTrainResponse {
	out := releaseTrainResponse{
		GeneratedAt: now, WindowFrom: from,
		Tags: []releaseTag{}, CI: []releaseCIRun{}, CIState: "none", Deploy: releaseDeploy{State: "none"},
	}
	for _, f := range findings { // newest first
		if f.Source != "ship_step" {
			continue
		}
		failed := strings.Contains(f.Text, "FAILED")
		if out.Deploy.State == "none" {
			out.Deploy = releaseDeploy{State: "ok", At: f.At, PRDID: f.PRDID}
			if failed {
				out.Deploy.State = "failed"
			}
		}
		if f.At.Before(from) {
			continue
		}
		if m := releaseTagRe.FindStringSubmatch(f.Text); m != nil {
			out.Tags = append(out.Tags, releaseTag{At: f.At, PRDID: f.PRDID, Tag: strings.TrimRight(m[1], ",.;:"), OK: !failed})
		}
	}
	rank := map[string]int{"none": 0, "green": 1, "pending": 2, "failing": 3}
	for _, rec := range ci {
		if rec == nil || rec.UpdatedAt.Before(from) || rec.Status == "superseded" {
			continue
		}
		out.CI = append(out.CI, releaseCIRun{Repo: rec.Repo, RefKind: rec.RefKind, RefName: rec.RefName,
			Workflow: rec.Workflow, Status: rec.Status, UpdatedAt: rec.UpdatedAt})
		state := "pending"
		switch rec.Status {
		case "green":
			state = "green"
		case "failing", "hung":
			state = "failing"
		}
		if rank[state] > rank[out.CIState] {
			out.CIState = state
		}
	}
	sort.SliceStable(out.CI, func(i, j int) bool { return out.CI[i].UpdatedAt.After(out.CI[j].UpdatedAt) })
	out.Empty = len(out.Tags) == 0 && len(out.CI) == 0 && out.Deploy.State == "none"
	if out.Empty {
		out.EmptyReasonKey = seriesReasonReleaseTrain
	}
	return out
}

func (s *Server) handleDashboardSeriesReleaseTrain(w http.ResponseWriter, r *http.Request) {
	if !s.seriesMoreGate(w, r) {
		return
	}
	days, err := seriesIntParam(r, "days", seriesMoreDefaultDays, seriesMoreMaxDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	var findings []autonomous.StatusFinding
	var ci []*autonomous.RepoCIWatchRecord
	if s.autonomousMgr != nil {
		if tr, ok := s.autonomousMgr.(triageReader); ok {
			findings = autonomous.BuildShipStatus(tr.ListTriageRecords(), "", "").Findings
		}
		if cr, ok := s.autonomousMgr.(repoCIWatchReader); ok {
			ci = cr.RepoCIWatchStatus().Records
		}
	}
	writeJSONOK(w, buildReleaseTrain(findings, ci, now.AddDate(0, 0, -days), now))
}

// ---- review backlog ----

type backlogPlan struct {
	ID         string    `json:"id"`
	Title      string    `json:"title,omitempty"`
	Status     string    `json:"status"`
	Since      time.Time `json:"since"`
	AgeSeconds int64     `json:"age_seconds"`
}

type backlogQuestion struct {
	QuestionID string    `json:"question_id"`
	PRDID      string    `json:"prd_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	State      string    `json:"state,omitempty"`
	Since      time.Time `json:"since"`
	AgeSeconds int64     `json:"age_seconds"`
}

type reviewBacklogResponse struct {
	GeneratedAt      time.Time         `json:"generated_at"`
	Limit            int               `json:"limit"`
	PlanCount        int               `json:"plan_count"`
	QuestionCount    int               `json:"question_count"`
	OldestAgeSeconds int64             `json:"oldest_age_seconds"`
	Plans            []backlogPlan     `json:"plans"`
	Questions        []backlogQuestion `json:"questions"`
	Empty            bool              `json:"empty"`
	EmptyReasonKey   string            `json:"empty_reason_key,omitempty"`
}

// buildReviewBacklog ages and orders (oldest first) the plans and questions,
// keeping at most limit of each; the counts are the totals before the cut.
func buildReviewBacklog(plans []backlogPlan, qs []backlogQuestion, limit int, now time.Time) reviewBacklogResponse {
	out := reviewBacklogResponse{GeneratedAt: now, Limit: limit, PlanCount: len(plans), QuestionCount: len(qs)}
	age := func(t time.Time) int64 {
		if a := int64(now.Sub(t) / time.Second); a > 0 {
			return a
		}
		return 0
	}
	for i := range plans {
		plans[i].AgeSeconds = age(plans[i].Since)
	}
	for i := range qs {
		qs[i].AgeSeconds = age(qs[i].Since)
	}
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].AgeSeconds > plans[j].AgeSeconds })
	sort.SliceStable(qs, func(i, j int) bool { return qs[i].AgeSeconds > qs[j].AgeSeconds })
	if len(plans) > limit {
		plans = plans[:limit]
	}
	if len(qs) > limit {
		qs = qs[:limit]
	}
	if plans == nil {
		plans = []backlogPlan{}
	}
	if qs == nil {
		qs = []backlogQuestion{}
	}
	out.Plans, out.Questions = plans, qs
	for _, p := range plans {
		if p.AgeSeconds > out.OldestAgeSeconds {
			out.OldestAgeSeconds = p.AgeSeconds
		}
	}
	for _, q := range qs {
		if q.AgeSeconds > out.OldestAgeSeconds {
			out.OldestAgeSeconds = q.AgeSeconds
		}
	}
	out.Empty = out.PlanCount == 0 && out.QuestionCount == 0
	if out.Empty {
		out.EmptyReasonKey = seriesReasonReviewBacklog
	}
	return out
}

func (s *Server) handleDashboardSeriesReviewBacklog(w http.ResponseWriter, r *http.Request) {
	if !s.seriesMoreGate(w, r) {
		return
	}
	limit, err := seriesIntParam(r, "limit", seriesMoreDefaultLimit, seriesMoreMaxLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	var plans []backlogPlan
	titles := map[string]string{}
	if s.autonomousMgr != nil {
		if api, ok := s.tagAPI(); ok {
			for _, it := range api.AttentionAutomata(autonomous.ResolveAttentionStatuses(s.autonomousSettings)) {
				var v autonomous.SummaryView
				switch x := it.(type) {
				case autonomous.SummaryView:
					v = x
				case *autonomous.SummaryView:
					if x == nil {
						continue
					}
					v = *x
				default:
					continue
				}
				titles[v.ID] = v.Title
				plans = append(plans, backlogPlan{ID: v.ID, Title: v.Title, Status: string(v.Status), Since: v.UpdatedAt})
			}
		}
	}
	var qs []backlogQuestion
	if s.questionStore != nil && (s.cfg == nil || s.cfg.Autonomous.Questions.Enabled) {
		for _, q := range s.questionStore.List(autonomous.QuestionFilter{State: autonomous.QuestionOpen}) {
			if q.Source != autonomous.QuestionSourceHeld || q.CreatedAt == nil {
				continue
			}
			qq := q
			qs = append(qs, backlogQuestion{QuestionID: q.ID, PRDID: q.PRDID, Title: titles[q.PRDID],
				State: heldStateOfOpenQuestion(&qq), Since: *q.CreatedAt})
		}
	}
	writeJSONOK(w, buildReviewBacklog(plans, qs, limit, now))
}

// ---- defects per day ----

type defectDay struct {
	Date  string `json:"date"` // UTC day, YYYY-MM-DD
	Found int    `json:"found"`
	Fixed int    `json:"fixed"`
}

type defectsPerDayResponse struct {
	GeneratedAt    time.Time   `json:"generated_at"`
	Days           int         `json:"days"`
	Points         []defectDay `json:"points"`
	TotalFound     int         `json:"total_found"`
	TotalFixed     int         `json:"total_fixed"`
	Empty          bool        `json:"empty"`
	EmptyReasonKey string      `json:"empty_reason_key,omitempty"`
}

// buildDefectsPerDay buckets the records into one point per UTC day, oldest first.
func buildDefectsPerDay(recs []autonomous.IntakeRecord, days int, now time.Time) defectsPerDayResponse {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	first := today.AddDate(0, 0, -(days - 1))
	out := defectsPerDayResponse{GeneratedAt: now, Days: days, Points: make([]defectDay, days)}
	for i := range out.Points {
		out.Points[i].Date = first.AddDate(0, 0, i).Format("2006-01-02")
	}
	for _, r := range recs {
		d := r.At.UTC()
		d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		if d.Before(first) || d.After(today) {
			continue
		}
		i := int(d.Sub(first) / (24 * time.Hour))
		out.Points[i].Found++
		out.TotalFound++
		if r.Route == autonomous.IntakeRouteTinyFix && r.ApplyError == "" && r.TinyFixRefusal == "" {
			out.Points[i].Fixed++
			out.TotalFixed++
		}
	}
	out.Empty = out.TotalFound == 0
	if out.Empty {
		out.EmptyReasonKey = seriesReasonDefects
	}
	return out
}

func (s *Server) handleDashboardSeriesDefectsPerDay(w http.ResponseWriter, r *http.Request) {
	if !s.seriesMoreGate(w, r) {
		return
	}
	days, err := seriesIntParam(r, "days", seriesMoreDefaultDays, seriesMoreMaxDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSONOK(w, buildDefectsPerDay(seriesDefectRecords(s), days, time.Now()))
}
