package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// pcFiltersReadAPI serves one stored Automaton (or none) from the
// interface-satisfying stub; the read route only touches GetPRD.
type pcFiltersReadAPI struct {
	fakeOrchAutonomous
	prd *autonomous.PRD
}

func (f *pcFiltersReadAPI) GetPRD(id string) (any, bool) {
	if f.prd == nil || f.prd.ID != id {
		return nil, false
	}
	return f.prd, true
}

func newPCFiltersReadServer(t *testing.T, prd *autonomous.PRD, live config.PlanCoverageFilters) *Server {
	t.Helper()
	boolPtr := func(b bool) *bool { return &b }
	return &Server{
		autonomousMgr: &pcFiltersReadAPI{prd: prd},
		cfg: &config.Config{Autonomous: config.AutonomousConfig{
			PlanCoverageSuppressCreatedFiles: boolPtr(live.SuppressCreatedFiles),
			PlanCoverageScopedRequirements:   boolPtr(live.ScopedRequirements),
			PlanCoverageRequirementSections:  strings.Join(live.RequirementSections, ","),
			PlanCoverageMinRequirementChars:  live.MinRequirementChars,
		}},
	}
}

// doPCFiltersRead hits the route and decodes the JSON reply (nil body when none).
func doPCFiltersRead(t *testing.T, s *Server, method, id string) (status int, raw []byte, body map[string]any) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(method, "/api/autonomous/prds/"+id+"/plan_coverage", nil))
	raw = rr.Body.Bytes()
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	return rr.Code, raw, body
}

func jsonNorm(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	// Round-trip through any so maps sort their keys; struct field order
	// would otherwise make identical values compare differently.
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b, err = json.Marshal(out)
	if err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	return string(b)
}

func pcStoredRecord() *autonomous.PRD {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	ack := &autonomous.PlanCoverageAck{
		By:          "operator",
		At:          &at,
		Note:        "accepted, follow-up planned",
		Fingerprint: "pc-fp-1",
	}
	return &autonomous.PRD{
		ID:     "pc-read-prd",
		Status: autonomous.PRDNeedsReview,
		PlanCoverage: &autonomous.PlanCoverage{
			Mode:     "block",
			MinRatio: 0.9,
			Report: autonomous.PlanCoverageReport{
				Covered:    14,
				Missing:    4,
				Weakened:   2,
				Suppressed: 3,
				Unmatched: []autonomous.RequirementVerdict{
					{Requirement: "rotate daemon logs under a size cap", Verdict: "missing"},
					{Requirement: "keep the PWA bundle under a size budget", Verdict: "weakened"},
				},
			},
			Ack:        ack,
			Findings:   []autonomous.StructuralFinding{{Kind: "unresolved_file", Detail: "plan creates docs/x.md"}},
			Suppressed: autonomous.PlanCoverageSuppressed{CreatedFiles: 2, Requirements: 3},
			Filters: config.PlanCoverageFilters{
				SuppressCreatedFiles: false,
				ScopedRequirements:   true,
				RequirementSections:  []string{"scope"},
				MinRequirementChars:  20,
			},
		},
	}
}

// pcLiveFilters is the config the server reads live; it deliberately differs
// from the record's pinned filters so the two can be told apart.
func pcLiveFilters() config.PlanCoverageFilters {
	return config.PlanCoverageFilters{
		SuppressCreatedFiles: true,
		ScopedRequirements:   true,
		RequirementSections:  []string{"scope", "constraints"},
		MinRequirementChars:  40,
	}
}

func TestPlanCoverageFilters_ReadWithRecord(t *testing.T) {
	prd := pcStoredRecord()
	s := newPCFiltersReadServer(t, prd, pcLiveFilters())

	status, _, body := doPCFiltersRead(t, s, http.MethodGet, prd.ID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["id"] != prd.ID {
		t.Errorf("id = %v, want %q", body["id"], prd.ID)
	}
	if body["status"] != "needs_review" {
		t.Errorf("status = %v, want needs_review", body["status"])
	}
	rec, ok := body["plan_coverage"].(map[string]any)
	if !ok {
		t.Fatalf("plan_coverage missing or not an object: %v", body)
	}
	// The stored ack is surfaced on the record.
	ack, ok := rec["ack"].(map[string]any)
	if !ok || ack["note"] != "accepted, follow-up planned" || ack["by"] != "operator" {
		t.Errorf("ack = %v, want the stored ack", rec["ack"])
	}
	// The record pins its own (different) filters.
	if jsonNorm(t, rec["filters"]) != jsonNorm(t, prd.PlanCoverage.Filters) {
		t.Errorf("record filters = %v, want the pinned %v", rec["filters"], prd.PlanCoverage.Filters)
	}
	if rec["mode"] != "block" {
		t.Errorf("record mode = %v, want block", rec["mode"])
	}
	if rec["min_ratio"] != float64(0.9) {
		t.Errorf("record min_ratio = %v, want 0.9", rec["min_ratio"])
	}
}

func TestPlanCoverageFilters_ReadEnvelope(t *testing.T) {
	live := pcLiveFilters()
	s := newPCFiltersReadServer(t, pcStoredRecord(), live)

	status, _, body := doPCFiltersRead(t, s, http.MethodGet, "pc-read-prd")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	// Envelope filters are the live config, not the record's pinned ones.
	if body["filters"] == nil {
		t.Fatal("filters missing from reply")
	}
	filt := body["filters"].(map[string]any)
	if filt["suppress_created_files"] != true || filt["min_requirement_chars"] != float64(40) {
		t.Errorf("filters = %v, want the live config %v", filt, live)
	}
	if jsonNorm(t, body["filters"]) != jsonNorm(t, live) {
		t.Errorf("filters = %v, want the live config %v", body["filters"], live)
	}
	// Envelope suppressed repeats the record's counts plus the total.
	sup := body["suppressed"].(map[string]any)
	if sup["created_files"] != float64(2) || sup["requirements"] != float64(3) || sup["total"] != float64(5) {
		t.Errorf("suppressed = %v, want {created_files 2, requirements 3, total 5}", sup)
	}
}

func TestPlanCoverageFilters_ReadNoRecord(t *testing.T) {
	live := pcLiveFilters()
	s := newPCFiltersReadServer(t, &autonomous.PRD{ID: "pc-bare", Status: autonomous.PRDDraft}, live)

	status, _, body := doPCFiltersRead(t, s, http.MethodGet, "pc-bare")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["plan_coverage"] != nil {
		t.Errorf("plan_coverage = %v, want null when no report is stored", body["plan_coverage"])
	}
	sup := body["suppressed"].(map[string]any)
	if sup["created_files"] != float64(0) || sup["requirements"] != float64(0) || sup["total"] != float64(0) {
		t.Errorf("suppressed = %v, want all zeroes without a record", sup)
	}
	if jsonNorm(t, body["filters"]) != jsonNorm(t, live) {
		t.Errorf("filters = %v, want the live config %v", body["filters"], live)
	}
}

func TestPlanCoverageFilters_ReadNotFound(t *testing.T) {
	s := newPCFiltersReadServer(t, nil, pcLiveFilters())
	status, _, _ := doPCFiltersRead(t, s, http.MethodGet, "pc-unknown")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown id", status)
	}
}

func TestPlanCoverageFilters_ReadMethodNotAllowed(t *testing.T) {
	s := newPCFiltersReadServer(t, pcStoredRecord(), pcLiveFilters())
	status, _, _ := doPCFiltersRead(t, s, http.MethodPost, "pc-read-prd")
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 for POST on the read route", status)
	}
}

func TestPlanCoverageFilters_ReadLiveNotPinned(t *testing.T) {
	// Live config and the stored record pin different filters; the reply must
	// carry the live ones at top level and the pinned ones on the record.
	s := newPCFiltersReadServer(t, pcStoredRecord(), pcLiveFilters())
	status, _, body := doPCFiltersRead(t, s, http.MethodGet, "pc-read-prd")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	liveFilt := body["filters"].(map[string]any)
	if liveFilt["suppress_created_files"] != true || liveFilt["min_requirement_chars"] != float64(40) {
		t.Errorf("top-level filters = %v, want the live config (suppress on, min 40 chars)", liveFilt)
	}
	recFilt := body["plan_coverage"].(map[string]any)["filters"].(map[string]any)
	if recFilt["suppress_created_files"] != false || recFilt["min_requirement_chars"] != float64(20) {
		t.Errorf("record filters = %v, want the pinned values (off, min 20 chars)", recFilt)
	}
	if jsonNorm(t, liveFilt) == jsonNorm(t, recFilt) {
		t.Fatal("live and pinned filters must occupy both places in the reply")
	}
}

var pcTrackerID = regexp.MustCompile(`"([A-Z]{1,5}[0-9]{2,4})"`)

func TestPlanCoverageFilters_ReadNoTrackerID(t *testing.T) {
	s := newPCFiltersReadServer(t, pcStoredRecord(), pcLiveFilters())
	_, raw, body := doPCFiltersRead(t, s, http.MethodGet, "pc-read-prd")
	if body == nil {
		t.Fatal("no JSON reply decoded")
	}
	if m := pcTrackerID.FindAll(raw, -1); len(m) > 0 {
		t.Fatalf("reply carries tracker-looking identifiers %q; body: %s", m, raw)
	}
}
