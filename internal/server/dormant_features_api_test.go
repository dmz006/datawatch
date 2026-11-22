package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// dormantFeaturesSpy stands in for the autonomous manager on the
// dormant-features route; the route is read-only, so the test also counts
// any config write the handler would attempt.
type dormantFeaturesSpy struct {
	fakeOrchAutonomous
	setCalls int
}

func (d *dormantFeaturesSpy) SetConfig(any) error {
	d.setCalls++
	return nil
}

// dormantReadsFixture is the config the seven off settings list from: a zero
// config is all-off, and the merge-pending row is turned on so exactly the
// seven off settings are listed.
func dormantReadsFixture() *config.Config {
	cfg := &config.Config{}
	cfg.Autonomous.Automation.MergePendingMode = config.MergePendingModeOn
	cfg.Autonomous.SupervisorLoop = config.SupervisorLoopConfig{
		"supervisor_loop.run_merge_until_conflict.enabled": false,
	}
	return cfg
}

var dormantSevenKeys = []string{
	"autonomous.scan.fix_loop_enabled",
	"autonomous.scan.rules_grader_enabled",
	"autonomous.scope_breadth_enabled",
	"autonomous.default_quality_gates.enabled",
	"autonomous.remote_reap_enabled",
	"autonomous.notify.forwarding.enabled",
	"supervisor_loop.run_merge_until_conflict.enabled",
}

func TestDormantFeaturesREST_ListsSevenOffSettings(t *testing.T) {
	s := &Server{autonomousMgr: &dormantFeaturesSpy{}, cfg: dormantReadsFixture()}
	rec := httptest.NewRecorder()
	s.handleDormantFeatures(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/dormant-features", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body %s", rec.Code, rec.Body.String())
	}
	var body dormantFeaturesBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(body.Features) != 7 {
		t.Fatalf("features = %d, want 7: %+v", len(body.Features), body.Features)
	}
	seen := map[string]autonomous.DormantFeature{}
	for _, f := range body.Features {
		seen[f.Key] = f
		if f.State != autonomous.DormantStateOff || f.Default == "" || f.Purpose == "" {
			t.Errorf("%s bad row %+v", f.Key, f)
		}
		if f.EverExercised || f.Evidence == "" {
			t.Errorf("%s should read not exercised: %+v", f.Key, f)
		}
	}
	for _, k := range dormantSevenKeys {
		if _, ok := seen[k]; !ok {
			t.Errorf("%s not listed", k)
		}
	}
}

func TestDormantFeaturesREST_RejectsPOSTWithoutWritingConfig(t *testing.T) {
	spy := &dormantFeaturesSpy{}
	s := &Server{autonomousMgr: spy, cfg: dormantReadsFixture()}
	before, err := json.Marshal(s.cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handleDormantFeatures(rec, httptest.NewRequest(method, "/api/autonomous/dormant-features", strings.NewReader("{}")))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: code = %d, want 405", method, rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
			t.Errorf("%s: Allow = %q, want %q", method, allow, http.MethodGet)
		}
	}
	if spy.setCalls != 0 {
		t.Errorf("handler attempted %d config write(s)", spy.setCalls)
	}
	after, err := json.Marshal(s.cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Error("handler changed the config")
	}
}

func TestDormantFeaturesREST_DisabledAndCapability(t *testing.T) {
	s := &Server{cfg: dormantReadsFixture()}
	rec := httptest.NewRecorder()
	s.handleDormantFeatures(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/dormant-features", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled: code = %d, want 503", rec.Code)
	}

	s = &Server{autonomousMgr: &dormantFeaturesSpy{}, cfg: dormantReadsFixture()}
	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/dormant-features", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec = httptest.NewRecorder()
	s.handleDormantFeatures(rec, deny)
	if rec.Code != http.StatusForbidden {
		t.Errorf("capability: code = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Errorf("capability body %q does not name %s", rec.Body.String(), federation.CapAutonomousRead)
	}

	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/dormant-features", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleDormantFeatures(rec, allow)
	if rec.Code != http.StatusOK {
		t.Errorf("capability: code = %d, want 200", rec.Code)
	}
}

// housekeepingLedgerSpy implements the housekeeping-repairs read surface
// plus the refusal-ledger and finished-work rows.
type housekeepingLedgerSpy struct {
	fakeOrchAutonomous
	ledger autonomous.RefusalLedger
	sweep  autonomous.FinishedWorkSweep
}

func (h *housekeepingLedgerSpy) HousekeepingRepairsView() autonomous.HousekeepingRepairsView {
	return autonomous.HousekeepingRepairsView{GraceSeconds: 60}
}

func (h *housekeepingLedgerSpy) RefusalLedger() *autonomous.RefusalLedger {
	return &h.ledger
}

func (h *housekeepingLedgerSpy) FinishedWorkSweep() *autonomous.FinishedWorkSweep {
	return &h.sweep
}

func TestDormantReadsHousekeepingCarriesRefusalsAndFinishedWork(t *testing.T) {
	spy := &housekeepingLedgerSpy{}
	spy.ledger.Record("alpha", "verify", "tests failing", time.Time{})
	spy.sweep.Record(autonomous.FinishedWorkFinding{
		Automaton: "beta", Title: "add the rest", Status: "completed",
		Cause: "work left unlanded", Action: "review",
	})
	s := &Server{autonomousMgr: spy}
	rec := httptest.NewRecorder()
	s.handleHousekeepingRepairs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/housekeeping-repairs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body %s", rec.Code, rec.Body.String())
	}
	var body housekeepingRepairsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(body.Refusals) != 1 {
		t.Fatalf("refusals = %d, want 1: %+v", len(body.Refusals), body.Refusals)
	}
	if body.Refusals[0].Automaton != "alpha" || body.Refusals[0].Step != "verify" || body.Refusals[0].Repeats != 1 {
		t.Errorf("refusal %+v", body.Refusals[0])
	}
	if len(body.FinishedWork) != 1 {
		t.Fatalf("finished_work = %d, want 1: %+v", len(body.FinishedWork), body.FinishedWork)
	}
	if body.FinishedWork[0].Automaton != "beta" || body.FinishedWork[0].At == "" {
		t.Errorf("finished work %+v", body.FinishedWork[0])
	}
}

func TestDormantReadsHousekeepingRowsAreEmptyLists(t *testing.T) {
	// A manager without the ledger surface still serves the body with the
	// two added rows as empty lists, never null.
	s := &Server{autonomousMgr: &housekeepingRepairsSpy{}}
	rec := httptest.NewRecorder()
	s.handleHousekeepingRepairs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/housekeeping-repairs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body %s", rec.Code, rec.Body.String())
	}
	var body housekeepingRepairsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body.Refusals == nil || len(body.Refusals) != 0 {
		t.Errorf("refusals = %#v, want empty list", body.Refusals)
	}
	if body.FinishedWork == nil || len(body.FinishedWork) != 0 {
		t.Errorf("finished_work = %#v, want empty list", body.FinishedWork)
	}
}
