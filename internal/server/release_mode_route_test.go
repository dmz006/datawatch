package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
)

// wireReleaseModeRecords gives s the decision record service the daemon has, so
// a release approval mode change can store its record. It returns the rig so a
// test can read the records back.
func wireReleaseModeRecords(t *testing.T, s *Server) *decisionRecordsRig {
	t.Helper()
	rig := newDecisionRecordsRig(t)
	s.decisionRecords = rig.svc
	return rig
}

// releaseModeEvent is one captured release_mode_changed audit event.
type releaseModeEvent struct {
	action  string
	details map[string]any
}

// captureReleaseModeEvents records every audit event the manager emits.
func captureReleaseModeEvents(mgr *autonomous.Manager) *[]releaseModeEvent {
	var events []releaseModeEvent
	mgr.SetAuditFn(func(action string, details map[string]any) {
		events = append(events, releaseModeEvent{action: action, details: details})
	})
	return &events
}

// TestReleaseModeSurfaceParam checks the surface named by ?surface=: the known
// names pass through, anything else falls back to rest.
func TestReleaseModeSurfaceParam(t *testing.T) {
	cases := map[string]string{
		"":        "rest",
		"mcp":     "mcp",
		"COMM":    "comm",
		" cli ":   "cli",
		"config":  "config",
		"pwa":     "pwa",
		"bogus":   "rest",
		"surface": "rest",
	}
	for in, want := range cases {
		target := "/api/config?" + url.Values{"surface": {in}}.Encode()
		if in == "" {
			target = "/api/config"
		}
		r := httptest.NewRequest(http.MethodPut, target, nil)
		if got := releaseModeSurface(r); got != want {
			t.Errorf("surface %q = %q, want %q", in, got, want)
		}
	}
}

// TestReleaseModeKeyTarget checks which config keys are release approval mode
// keys, and the project and size each one names.
func TestReleaseModeKeyTarget(t *testing.T) {
	cases := []struct {
		key, project, size string
		ok                 bool
	}{
		{"autonomous.merge_pipeline.release_approval.patch", "", "patch", true},
		{"autonomous.merge_pipeline.release_approval.major", "", "major", true},
		{"autonomous.merge_pipeline.projects.datawatch.release_approval.minor", "datawatch", "minor", true},
		{"autonomous.merge_pipeline.release_approval.huge", "", "", false},
		{"autonomous.merge_pipeline.release_cadence", "", "", false},
		{"autonomous.merge_pipeline.projects.datawatch.release_cadence", "", "", false},
		{"autonomous.other.release_approval.patch", "", "", false},
	}
	for _, c := range cases {
		project, size, ok := releaseModeKeyTarget(c.key)
		if ok != c.ok || project != c.project || size != c.size {
			t.Errorf("%s = (%q, %q, %v), want (%q, %q, %v)", c.key, project, size, ok, c.project, c.size, c.ok)
		}
	}
}

// TestPutConfigReleaseModeRecordsDecisionPerSurface drives a real mode change
// through PUT /api/config for each surface and checks it stores the mode, emits
// one release_mode_changed event naming that surface, and stores a decision
// record whose ID the event carries.
func TestPutConfigReleaseModeRecordsDecisionPerSurface(t *testing.T) {
	for _, surface := range []string{"pwa", "rest", "mcp", "comm", "cli", "config"} {
		t.Run(surface, func(t *testing.T) {
			s, mgr, _ := newMaxConcurrentTasksServer(t)
			rig := wireReleaseModeRecords(t, s)
			events := captureReleaseModeEvents(mgr)

			body, _ := json.Marshal(map[string]any{"autonomous.merge_pipeline.release_approval.minor": "manual"})
			rec := httptest.NewRecorder()
			s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config?surface="+surface, bytes.NewReader(body)))
			if rec.Code != http.StatusOK {
				t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
			}
			if got := s.cfg.Autonomous.MergePipeline.ReleaseApproval.Minor; got != "manual" {
				t.Errorf("stored minor = %q, want manual", got)
			}

			records := rig.svc.List(autonomous.DecisionRecordFilter{})
			if len(records) != 1 {
				t.Fatalf("decision records = %d, want 1", len(records))
			}
			if want := releaseModeRecordSource(surface); records[0].Source != want {
				t.Errorf("record source = %q, want %q", records[0].Source, want)
			}

			if len(*events) != 1 {
				t.Fatalf("audit events = %d, want 1", len(*events))
			}
			ev := (*events)[0]
			if ev.action != audit.ActionReleaseModeChanged {
				t.Errorf("event action = %q, want %q", ev.action, audit.ActionReleaseModeChanged)
			}
			if got := ev.details["surface"]; got != surface {
				t.Errorf("event surface = %v, want %q", got, surface)
			}
			if got := ev.details["decision_record_id"]; got != records[0].ID {
				t.Errorf("event decision_record_id = %v, want %q", got, records[0].ID)
			}
			if got := ev.details["old_mode"]; got != "auto" {
				t.Errorf("event old_mode = %v, want auto", got)
			}
			if got := ev.details["new_mode"]; got != "manual" {
				t.Errorf("event new_mode = %v, want manual", got)
			}
		})
	}
}

// TestPutConfigReleaseModeNoOpWritesWithoutRecord checks that a mode equal to
// the one in force stores the value but emits no event and no record: nothing
// the operator decides has changed.
func TestPutConfigReleaseModeNoOpWritesWithoutRecord(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	rig := wireReleaseModeRecords(t, s)
	events := captureReleaseModeEvents(mgr)

	body, _ := json.Marshal(map[string]any{"autonomous.merge_pipeline.release_approval.minor": "auto"})
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config?surface=mcp", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := s.cfg.Autonomous.MergePipeline.ReleaseApproval.Minor; got != "auto" {
		t.Errorf("stored minor = %q, want auto", got)
	}
	if n := len(rig.svc.List(autonomous.DecisionRecordFilter{})); n != 0 {
		t.Errorf("decision records = %d, want 0 for a no-op", n)
	}
	if len(*events) != 0 {
		t.Errorf("audit events = %d, want 0 for a no-op", len(*events))
	}
}

// TestPutConfigReleaseModeRefusedWithoutRecords checks that a mode change is
// refused before anything is stored when no decision record service is wired.
func TestPutConfigReleaseModeRefusedWithoutRecords(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	body, _ := json.Marshal(map[string]any{"autonomous.merge_pipeline.release_approval.minor": "manual"})
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config?surface=cli", bytes.NewReader(body)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("PUT: want 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := s.cfg.Autonomous.MergePipeline.ReleaseApproval.Minor; got == "manual" {
		t.Errorf("stored minor = %q after a refused change, want unchanged", got)
	}
}

// TestPutConfigReleaseModeRecordCarriesStoredEvidence checks that the decision
// record of a mode change states the readiness counts of the stored shadow-merge
// comparisons at that moment, the same counts the readiness read returns, not an
// empty set.
func TestPutConfigReleaseModeRecordCarriesStoredEvidence(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	rig := wireReleaseModeRecords(t, s)
	for _, id := range []string{"e1", "e2"} {
		seedReleaseStore(t, s.statePath(), id,
			`{"automaton_id":"`+id+`","release_size":"minor","matched":6,"different":0,"not_compared":0,"differences":[]}`,
			`{"automaton_id":"`+id+`","project":"demo","parity_issue_needed":false,"conflicts":[]}`,
		)
	}

	body, _ := json.Marshal(map[string]any{"autonomous.merge_pipeline.release_approval.minor": "manual"})
	rec := httptest.NewRecorder()
	s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	records := rig.svc.List(autonomous.DecisionRecordFilter{})
	if len(records) != 1 {
		t.Fatalf("decision records = %d, want 1", len(records))
	}
	if got := records[0].Situation; !strings.Contains(got, "2 plan-equal-or-better records") {
		t.Errorf("situation does not carry the stored counts: %s", got)
	}
}
