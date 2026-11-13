// Tests for the shipped-state read surfaces (shipped.go): the per-status
// counts endpoint, the PRD / Automation list shipped filters, and the live
// dashboard card payload. All against an in-memory fake AutonomousAPI and a
// config.DefaultConfig — no home directory, no daemon.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// shipFleet is the whole stored world for these tests: four PRDs (one
// running, one completed, one running whose work has landed, one cancelled
// whose work has landed) and two Automations (one active, one active whose
// work has landed). Shipped state is the merge commit — the stored statuses
// never move.
type shipFleet struct {
	fakeOrchAutonomous
}

func (shipFleet) ListPRDs() []any {
	return []any{
		&autonomous.PRD{ID: "prd-running", Status: autonomous.PRDRunning},
		&autonomous.PRD{ID: "prd-completed", Status: autonomous.PRDCompleted},
		&autonomous.PRD{ID: "prd-running-shipped", Status: autonomous.PRDRunning, MergedInRelease: "v9", MergeCommit: "c-1"},
		&autonomous.PRD{ID: "prd-cancelled-shipped", Status: autonomous.PRDCancelled, MergedInRelease: "v9", MergeCommit: "c-2"},
	}
}

func (shipFleet) ListAutomations() []any {
	return []any{
		&autonomous.Automation{ID: "auto-active", Status: autonomous.AutomationActive},
		&autonomous.Automation{ID: "auto-active-shipped", Status: autonomous.AutomationActive, MergedInRelease: "v9", MergeCommit: "c-3"},
	}
}

func shipBool(v bool) *bool { return &v }

func newShipRESTServer(t *testing.T, mutate func(*config.Config)) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	if mutate != nil {
		mutate(cfg)
	}
	return &Server{autonomousMgr: &shipFleet{}, cfg: cfg}
}

// doShipGET dispatches one GET at the same path the daemon route would use,
// straight to the Server handler.
func doShipGET(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	switch {
	case strings.HasPrefix(path, "/api/autonomous/shipped/counts"):
		s.handleAutonomousShippedCounts(rec, req)
	case strings.HasPrefix(path, "/api/autonomous/prds"):
		s.handleAutonomousPRDs(rec, req)
	case strings.HasPrefix(path, "/api/automations"):
		s.handleAutomations(rec, req)
	default:
		t.Fatalf("doShipGET: no dispatch for %q", path)
	}
	return rec
}

// shippedCountsBody is the JSON shape the counts endpoint (and the shipped
// dashboard card) return.
type shippedCountsBody struct {
	PRDs        map[string]int `json:"prds"`
	Automations map[string]int `json:"automations"`
	Settings    struct {
		Enabled         bool   `json:"enabled"`
		ShowChip        bool   `json:"show_chip"`
		FilterDefault   string `json:"filter_default"`
		CountSeparately bool   `json:"count_separately"`
	} `json:"settings"`
}

// shipListRows is the summary-list row shape the PRD list filter assertions
// need.
type shipListBody struct {
	PRDs  []map[string]any `json:"prds"`
	Total int              `json:"total"`
}

// TestShippedCountsREST_SeparatesShippedFromCancelled: under the default
// autonomous.shipped settings the shipped-state records group under "shipped"
// and nothing hides behind "cancelled".
func TestShippedCountsREST_SeparatesShippedFromCancelled(t *testing.T) {
	s := newShipRESTServer(t, nil)
	rec := doShipGET(t, s, "/api/autonomous/shipped/counts")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET shipped/counts = %d: %s", rec.Code, rec.Body.String())
	}
	var body shippedCountsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	wantPRDs := map[string]int{"running": 1, "completed": 1, "shipped": 2}
	if !reflect.DeepEqual(body.PRDs, wantPRDs) {
		t.Errorf("prds = %v; want %v", body.PRDs, wantPRDs)
	}
	wantAutos := map[string]int{"active": 1, "shipped": 1}
	if !reflect.DeepEqual(body.Automations, wantAutos) {
		t.Errorf("automations = %v; want %v", body.Automations, wantAutos)
	}
	if !body.Settings.Enabled || !body.Settings.CountSeparately || !body.Settings.ShowChip || body.Settings.FilterDefault != "all" {
		t.Errorf("settings = %+v; want enabled + count_separately + show_chip, filter_default all", body.Settings)
	}
}

// TestShippedCountsREST_CountSeparatelyOffFoldsBack: with
// autonomous.shipped.count_separately=false every record counts under its
// stored status again — the shipped bucket disappears and the shipped-yet-
// cancelled PRD folds back into "cancelled".
func TestShippedCountsREST_CountSeparatelyOffFoldsBack(t *testing.T) {
	s := newShipRESTServer(t, func(cfg *config.Config) {
		cfg.Autonomous.Shipped.CountSeparately = shipBool(false)
	})
	rec := doShipGET(t, s, "/api/autonomous/shipped/counts")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET shipped/counts = %d: %s", rec.Code, rec.Body.String())
	}
	var body shippedCountsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := map[string]int{"running": 2, "completed": 1, "cancelled": 1}; !reflect.DeepEqual(body.PRDs, want) {
		t.Errorf("prds = %v; want %v", body.PRDs, want)
	}
	if want := map[string]int{"active": 2}; !reflect.DeepEqual(body.Automations, want) {
		t.Errorf("automations = %v; want %v", body.Automations, want)
	}
	if body.Settings.CountSeparately {
		t.Errorf("settings.count_separately = true; want false")
	}
}

// TestShippedCountsREST_ShippedDisabledCountsStoredStatuses: with
// autonomous.shipped.enabled=false no record carries a display state on
// shipped — everything counts by stored status.
func TestShippedCountsREST_ShippedDisabledCountsStoredStatuses(t *testing.T) {
	s := newShipRESTServer(t, func(cfg *config.Config) {
		cfg.Autonomous.Shipped.Enabled = shipBool(false)
	})
	rec := doShipGET(t, s, "/api/autonomous/shipped/counts")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET shipped/counts = %d: %s", rec.Code, rec.Body.String())
	}
	var body shippedCountsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := map[string]int{"running": 2, "completed": 1, "cancelled": 1}; !reflect.DeepEqual(body.PRDs, want) {
		t.Errorf("prds = %v; want %v", body.PRDs, want)
	}
	if want := map[string]int{"active": 2}; !reflect.DeepEqual(body.Automations, want) {
		t.Errorf("automations = %v; want %v", body.Automations, want)
	}
	if body.Settings.Enabled {
		t.Errorf("settings.enabled = true; want false")
	}
}

func TestShippedCountsREST_MethodNotAllowed(t *testing.T) {
	s := newShipRESTServer(t, nil)
	rec := httptest.NewRecorder()
	s.handleAutonomousShippedCounts(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/shipped/counts", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d; want 405", rec.Code)
	}
}

func TestShippedCountsREST_NoManagerUnavailable(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig()}
	rec := doShipGET(t, s, "/api/autonomous/shipped/counts")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET without an autonomous manager = %d; want 503", rec.Code)
	}
}

func shipListIDSet(t *testing.T, s *Server, rawQuery string) map[string]map[string]any {
	t.Helper()
	rec := doShipGET(t, s, "/api/autonomous/prds"+rawQuery)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET prd list %s = %d: %s", rawQuery, rec.Code, rec.Body.String())
	}
	var body shipListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != len(body.PRDs) {
		t.Fatalf("total = %d; %d rows", body.Total, len(body.PRDs))
	}
	out := map[string]map[string]any{}
	for _, row := range body.PRDs {
		out[row["id"].(string)] = row
	}
	return out
}

// TestShippedPRDListREST_ShippedSelection: shipped=true keeps exactly the PRDs
// that carry shipped state (either is fine — the selection is on the merge
// commit, orthogonal to status); shipped=false the rest. Each row reports
// its stored status unchanged plus the display state.
func TestShippedPRDListREST_ShippedSelection(t *testing.T) {
	s := newShipRESTServer(t, nil)

	got := shipListIDSet(t, s, "?shipped=true")
	if len(got) != 2 {
		t.Fatalf("shipped=true kept %d PRDs (%v); want 2", len(got), got)
	}
	for _, want := range []string{"prd-running-shipped", "prd-cancelled-shipped"} {
		row, ok := got[want]
		if !ok {
			t.Fatalf("shipped=true did not keep %s", want)
		}
		if row["shipped"] != true {
			t.Errorf("%s: shipped flag = %v; want true", want, row["shipped"])
		}
		if row["effective_status"] != "shipped" {
			t.Errorf("%s: effective_status = %v; want shipped", want, row["effective_status"])
		}
	}
	// shipped=true must have kept the cancelled-shipped PRD with its stored
	// status intact: orthogonal, not a mutation.
	if got["prd-cancelled-shipped"]["status"] != "cancelled" {
		t.Errorf("prd-cancelled-shipped stored status = %v; want cancelled (shipped never mutates it)", got["prd-cancelled-shipped"]["status"])
	}

	got = shipListIDSet(t, s, "?shipped=false")
	if len(got) != 2 {
		t.Fatalf("shipped=false kept %d PRDs (%v); want 2", len(got), got)
	}
	for _, want := range []string{"prd-running", "prd-completed"} {
		row, ok := got[want]
		if !ok {
			t.Fatalf("shipped=false did not keep %s", want)
		}
		if row["shipped"] != false {
			t.Errorf("%s: shipped flag = %v; want false", want, row["shipped"])
		}
		if row["effective_status"] != row["status"] {
			t.Errorf("%s: effective_status = %v; want stored status %v", want, row["effective_status"], row["status"])
		}
	}
}

// TestShippedPRDListREST_StatusShippedBucket: the status list selection may
// include shipped, and it selects on the merge commit like shipped=true.
func TestShippedPRDListREST_StatusShippedBucket(t *testing.T) {
	s := newShipRESTServer(t, nil)
	rows := shipListIDSet(t, s, "?status=shipped")
	if len(rows) != 2 {
		t.Fatalf("status=shipped kept %d PRDs (%v); want both shipped-state PRDs", len(rows), rows)
	}
	for id, row := range rows {
		if row["effective_status"] != "shipped" {
			t.Errorf("%s: effective_status = %v; want shipped", id, row["effective_status"])
		}
	}
}

// TestShippedPRDListREST_FilterDefaultView: with no explicit selection the
// in-force autonomous.shipped.filter_default view applies.
func TestShippedPRDListREST_FilterDefaultView(t *testing.T) {
	s := newShipRESTServer(t, func(cfg *config.Config) {
		cfg.Autonomous.Shipped.FilterDefault = "shipped"
	})
	if rows := shipListIDSet(t, s, ""); len(rows) != 2 {
		t.Errorf("filter_default=shipped kept %d PRDs (%v); want the 2 shipped-state ones", len(rows), rows)
	}

	s = newShipRESTServer(t, func(cfg *config.Config) {
		cfg.Autonomous.Shipped.FilterDefault = "unshipped"
	})
	if rows := shipListIDSet(t, s, ""); len(rows) != 2 {
		t.Errorf("filter_default=unshipped kept %d PRDs (%v); want the 2 plain ones", len(rows), rows)
	}
}

// TestShippedPRDListREST_ShippedDisabledSelectionIsEmpty: with autonomous.
// shipped.enabled=false no record carries anything on shipped state, so the
// shipped=true selection matches nothing and shipped=false matches
// everything (nothing has it).
func TestShippedPRDListREST_ShippedDisabledSelectionIsEmpty(t *testing.T) {
	s := newShipRESTServer(t, func(cfg *config.Config) {
		cfg.Autonomous.Shipped.Enabled = shipBool(false)
	})
	if rows := shipListIDSet(t, s, "?shipped=true"); len(rows) != 0 {
		t.Fatalf("shipped surface off: shipped=true kept %d PRDs (%v); want none — nothing displays on shipped", len(rows), rows)
	}
	for id, row := range shipListIDSet(t, s, "?shipped=false") {
		if row["effective_status"] == "shipped" {
			t.Errorf("%s: effective_status = shipped while disabled", id)
		}
	}
}

// TestShippedPRDListREST_ShippedBogus400: a shipped value that is neither
// true nor false is rejected, not silently widened.
func TestShippedPRDListREST_ShippedBogus400(t *testing.T) {
	s := newShipRESTServer(t, nil)
	rec := doShipGET(t, s, "/api/autonomous/prds?shipped=maybe")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("shipped=maybe = %d; want 400 (%s)", rec.Code, rec.Body.String())
	}
}

// shipAutomationIDSet is the automation-list equivalent of shipListIDSet.
func shipAutomationIDSet(t *testing.T, s *Server, rawQuery string) map[string]any {
	t.Helper()
	rec := doShipGET(t, s, "/api/automations"+rawQuery)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET automation list %s = %d: %s", rawQuery, rec.Code, rec.Body.String())
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := map[string]any{}
	for _, a := range list {
		out[a["id"].(string)] = a
	}
	return out
}

// TestShippedAutomationListREST_ShippedSelection: the Automation list
// honours the same shipped true|false selection (and drops it while the
// surface is disabled).
func TestShippedAutomationListREST_ShippedSelection(t *testing.T) {
	s := newShipRESTServer(t, nil)

	got := shipAutomationIDSet(t, s, "?shipped=true")
	if len(got) != 1 {
		t.Fatalf("shipped=true kept %d Automations (%v); want the 1 shipped one", len(got), got)
	}
	if _, ok := got["auto-active-shipped"]; !ok {
		t.Fatalf("shipped=true kept %v; want auto-active-shipped", got)
	}

	got = shipAutomationIDSet(t, s, "?shipped=false")
	if len(got) != 1 {
		t.Fatalf("shipped=false kept %d Automations (%v); want the 1 plain one", len(got), got)
	}
	if _, ok := got["auto-active"]; !ok {
		t.Fatalf("shipped=false kept %v; want auto-active", got)
	}

	s = newShipRESTServer(t, func(cfg *config.Config) {
		cfg.Autonomous.Shipped.Enabled = shipBool(false)
	})
	if got := shipAutomationIDSet(t, s, "?shipped=true"); len(got) != 2 {
		t.Errorf("shipped surface off: shipped=true kept %d Automations (%v); want all 2", len(got), got)
	}
}

// TestShippedCardData_SeparatesShippedFromCancelled: the dashboard card
// payload is the same per-status buckets, fed live on every read.
func TestShippedCardData_SeparatesShippedFromCancelled(t *testing.T) {
	s := newShipRESTServer(t, nil)
	raw, ok := s.shippedCardData().(map[string]any)
	if !ok {
		t.Fatalf("shippedCardData() = %T; want a payload map", s.shippedCardData())
	}
	wantPRDs := map[string]int{"running": 1, "completed": 1, "shipped": 2}
	if !reflect.DeepEqual(raw["prds"], wantPRDs) {
		t.Errorf("card prds = %v; want %v", raw["prds"], wantPRDs)
	}
	wantAutos := map[string]int{"active": 1, "shipped": 1}
	if !reflect.DeepEqual(raw["automations"], wantAutos) {
		t.Errorf("card automations = %v; want %v", raw["automations"], wantAutos)
	}

	s = newShipRESTServer(t, func(cfg *config.Config) {
		cfg.Autonomous.Shipped.CountSeparately = shipBool(false)
	})
	if raw = s.shippedCardData().(map[string]any); !reflect.DeepEqual(raw["prds"], map[string]int{"running": 2, "completed": 1, "cancelled": 1}) {
		t.Errorf("card prds (count_separately off) = %v; want stored-status buckets", raw["prds"])
	}
}

// TestShippedCardData_NilManagerNoPayload: no autonomous manager, no payload.
func TestShippedCardData_NilManagerNoPayload(t *testing.T) {
	if got := (&Server{}).shippedCardData(); got != nil {
		t.Errorf("shippedCardData() without a manager = %v; want nil", got)
	}
}
