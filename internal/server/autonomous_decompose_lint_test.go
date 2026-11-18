// Decompose-lint REST endpoint tests — httptest + fakes only (no daemon,
// no operator config). The spy replaces the three decompose-lint API
// methods on top of fakeOrchAutonomous so each endpoint's HTTP contract
// (status, body shape, read-only semantics) is pinned independently of
// the engine it fronts.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// Sentinel errors the spy returns to exercise the handler's status-code
// mapping. errLintPRDNotFound matches the handler's "not found" sniff; the
// API layer's real message is "lint: prd %q not found",
// so the 404 path is what runs in production too.
var (
	errBadDecomposeLint = errors.New("decompose_lint: mode must be one of off, shadow, enforce")
	errLintPRDNotFound  = errors.New("lint: prd \"missing\" not found")
)

// decomposeLintSpy embeds the shared fake (which supplies the three new
// AutonomousAPI methods with neutral defaults) and overrides them with
// controllable fakes.
type decomposeLintSpy struct {
	fakeOrchAutonomous

	lintConfig   any // DecomposeLintConfig() return
	setLintErr   error
	setLintCalls int

	getPRDVal   any
	getPRDOK    bool
	getPRDCalls int

	dryRun      any
	dryRunErr   error
	dryRunCalls []string
}

func (s *decomposeLintSpy) DecomposeLintConfig() any { return s.lintConfig }

func (s *decomposeLintSpy) SetDecomposeLintConfig(body map[string]any) error {
	s.setLintCalls++
	return s.setLintErr
}

func (s *decomposeLintSpy) GetPRD(id string) (any, bool) {
	s.getPRDCalls++
	return s.getPRDVal, s.getPRDOK
}

func (s *decomposeLintSpy) DecomposeLintDryRun(prdID string) (any, error) {
	s.dryRunCalls = append(s.dryRunCalls, prdID)
	return s.dryRun, s.dryRunErr
}

func newDecomposeLintTestServer(spy *decomposeLintSpy) *Server {
	return &Server{autonomousMgr: spy}
}

func defaultDecomposeLintFixture(t *testing.T) map[string]any {
	t.Helper()
	ro := true
	return map[string]any{
		"decompose_lint": config.DecomposeLintConfig{
			Mode:       "enforce",
			MaxRetries: 2,
			Rules: []config.DecomposeLintRule{{
				ID:       "no-verification-less-tasks",
				Severity: "block",
				Match:    config.DecomposeLintMatch{SpecRegex: ".*"},
				Message:  "every task needs a verification method",
			}},
			DisabledRules: []string{},
		},
		"task_templates": config.TaskTemplatesConfig{
			Enabled:   true,
			Templates: []config.TaskTemplateDef{{ID: "docs", Kind: "docs", Spec: "write the thing"}},
			Projects:  map[string]config.TaskTemplateProjectOverride{"p1": {Enabled: &ro}},
		},
		"decompose_guidance": "keep stories testable",
	}
}

// ── GET /api/autonomous/decompose-lint/config ──────────────────────────

// TestDecomposeLintConfigHandler_GET serves the live config object
// (decompose_lint, task_templates, decompose_guidance) as one JSON body.
func TestDecomposeLintConfigHandler_GET(t *testing.T) {
	spy := &decomposeLintSpy{lintConfig: defaultDecomposeLintFixture(t)}
	s := newDecomposeLintTestServer(spy)

	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/decompose-lint/config", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("GET: Content-Type=%q, want application/json", ct)
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET: body is not JSON: %v (%s)", err, rr.Body.String())
	}
	for _, k := range []string{"decompose_lint", "task_templates", "decompose_guidance"} {
		if _, ok := out[k]; !ok {
			t.Errorf("GET: missing key %q in %v", k, out)
		}
	}
	if out["decompose_guidance"] != "keep stories testable" {
		t.Errorf("GET: decompose_guidance = %v, want %q", out["decompose_guidance"], "keep stories testable")
	}
	lint, _ := out["decompose_lint"].(map[string]any)
	if got := lint["mode"]; got != "enforce" {
		t.Errorf("GET: decompose_lint.mode = %v, want enforce", got)
	}
}

// ── PUT /api/autonomous/decompose-lint/config ──────────────────────────

func putDecomposeLintReq(t *testing.T, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest(http.MethodPut, "/api/autonomous/decompose-lint/config", strings.NewReader(string(b)))
}

// TestDecomposeLintConfigHandler_PUT_Valid forwards the body to
// SetDecomposeLintConfig and echoes the live config on success.
func TestDecomposeLintConfigHandler_PUT_Valid(t *testing.T) {
	spy := &decomposeLintSpy{lintConfig: defaultDecomposeLintFixture(t)}
	s := newDecomposeLintTestServer(spy)

	req := putDecomposeLintReq(t, map[string]any{"decompose_guidance": "be thorough"})
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("PUT valid: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if spy.setLintCalls != 1 {
		t.Errorf("PUT valid: SetDecomposeLintConfig calls = %d, want 1", spy.setLintCalls)
	}
}

// TestDecomposeLintConfigHandler_PUT_Invalid surfaces the API-layer
// validation error as a 400 and applies nothing.
func TestDecomposeLintConfigHandler_PUT_Invalid(t *testing.T) {
	spy := &decomposeLintSpy{setLintErr: errBadDecomposeLint}
	s := newDecomposeLintTestServer(spy)

	req := putDecomposeLintReq(t, map[string]any{"decompose_lint": map[string]any{"mode": "yolo"}})
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintConfig(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT invalid: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if spy.setLintCalls != 1 {
		t.Errorf("PUT invalid: SetDecomposeLintConfig calls = %d, want 1 (validation happens in the API layer)", spy.setLintCalls)
	}
}

// TestDecomposeLintConfigHandler_MethodNotAllowed pins 405 for methods
// the endpoint does not serve.
func TestDecomposeLintConfigHandler_MethodNotAllowed(t *testing.T) {
	spy := &decomposeLintSpy{}
	s := newDecomposeLintTestServer(spy)

	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/decompose-lint/config", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintConfig(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status=%d, want 405", rr.Code)
	}
}

// TestDecomposeLintConfigHandler_Disabled pins 503 when autonomous is
// not running (same guard as every other autonomous endpoint).
func TestDecomposeLintConfigHandler_Disabled(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/decompose-lint/config", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintConfig(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled: status=%d, want 503", rr.Code)
	}
}

// TestDecomposeLintConfigHandler_PUT_RoundTripsToYamlishConfig is the B113
// guard for this endpoint: the PUT must sync the manager's live config
// (the single source of truth, already defaulted) into the YAML-facing
// s.cfg AND persist it, so the write survives a daemon restart instead
// of being re-derived (incompletely) from the raw request body.
func TestDecomposeLintConfigHandler_PUT_RoundTripsToYamlishConfig(t *testing.T) {
	spy := &decomposeLintSpy{lintConfig: defaultDecomposeLintFixture(t)}
	dir := t.TempDir()
	s := &Server{
		autonomousMgr: spy,
		cfg:           &config.Config{Git: config.GitConfig{ReleaseCIWindowMinutes: 60}},
		cfgPath:       dir + "/config.yaml",
	}

	req := putDecomposeLintReq(t, map[string]any{"decompose_lint": map[string]any{"mode": "enforce"}})
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: status=%d body=%s", rr.Code, rr.Body.String())
	}
	// In-memory YAML-facing config synced from the manager's live value.
	if got := s.cfg.Autonomous.DecomposeLint.Mode; got != "enforce" {
		t.Errorf("YAML-facing DecomposeLint.Mode = %q, want enforce", got)
	}
	if got := s.cfg.Autonomous.DecomposeGuidance; got != "keep stories testable" {
		t.Errorf("YAML-facing DecomposeGuidance = %q, want the manager's value", got)
	}
	// And persisted — reload from disk and confirm it survived.
	loaded, err := config.Load(dir + "/config.yaml")
	if err != nil {
		t.Fatalf("config.Load after PUT: %v", err)
	}
	if got := loaded.Autonomous.DecomposeLint.Mode; got != "enforce" {
		t.Errorf("persisted DecomposeLint.Mode = %q, want enforce", got)
	}
	if got := loaded.Autonomous.DecomposeGuidance; got != "keep stories testable" {
		t.Errorf("persisted DecomposeGuidance = %q, want the manager's value", got)
	}
}

// ── GET /api/autonomous/prds/{id}/decompose-lint ───────────────────────

func prdWithLintRecord() map[string]any {
	return map[string]any{
		"id":  "prd-1",
		"prd": "Story X",
		"decompose_lint": map[string]any{
			"mode":                    "enforce",
			"attempts":                2,
			"linted_at":               "2026-01-01T00:00:00Z",
			"decompose_lint_findings": []any{map[string]any{"rule": "no-verification-less-tasks", "task": "T1", "severity": "block", "message": "no verification"}},
		},
	}
}

// TestDecomposeLintPRDHandler_GET_Record returns the stored record plus
// the PRD id.
func TestDecomposeLintPRDHandler_GET_Record(t *testing.T) {
	spy := &decomposeLintSpy{getPRDOK: true, getPRDVal: prdWithLintRecord()}
	s := newDecomposeLintTestServer(spy)

	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/prd-1/decompose-lint", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintPRD(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET record: status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET record: body not JSON: %v (%s)", err, rr.Body.String())
	}
	if out["prd_id"] != "prd-1" {
		t.Errorf("GET record: prd_id = %v, want prd-1", out["prd_id"])
	}
	rec, _ := out["decompose_lint"].(map[string]any)
	if rec == nil {
		t.Fatalf("GET record: decompose_lint not an object: %s", rr.Body.String())
	}
	if rec["mode"] != "enforce" {
		t.Errorf("GET record: record mode = %v, want enforce", rec["mode"])
	}
	if got := rec["attempts"]; got != float64(2) {
		t.Errorf("GET record: attempts = %v, want 2", got)
	}
	if spy.dryRunCalls != nil {
		t.Errorf("GET must not invoke the dry run, calls=%v", spy.dryRunCalls)
	}
}

// TestDecomposeLintPRDHandler_GET_NoneStored pins decompose_lint: null
// when the PRD has no stored record (predates the gate / gate was off)
// — the client needs to distinguish "none stored" from "404".
func TestDecomposeLintPRDHandler_GET_NoneStored(t *testing.T) {
	spy := &decomposeLintSpy{getPRDOK: true, getPRDVal: map[string]any{"id": "prd-9"}}
	s := newDecomposeLintTestServer(spy)

	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/prd-9/decompose-lint", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintPRD(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET none-stored: status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET none-stored: body not JSON: %v (%s)", err, rr.Body.String())
	}
	if v, present := out["decompose_lint"]; !present || v != nil {
		t.Errorf("GET none-stored: decompose_lint = %v (present=%v), want explicit null", v, present)
	}
}

// TestDecomposeLintPRDHandler_GET_NotFound pins 404 for an unknown PRD.
func TestDecomposeLintPRDHandler_GET_NotFound(t *testing.T) {
	spy := &decomposeLintSpy{getPRDOK: false}
	s := newDecomposeLintTestServer(spy)

	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/missing/decompose-lint", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintPRD(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET missing: status=%d, want 404", rr.Code)
	}
}

// TestDecomposeLintPRDHandler_GET_MissingID pins 400 when the id path
// slot could not be resolved to a PRD id.
func TestDecomposeLintPRDHandler_GET_MissingID(t *testing.T) {
	spy := &decomposeLintSpy{}
	s := newDecomposeLintTestServer(spy)

	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/decompose-lint", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintPRD(rr, req)

	// "/decompose-lint" is the endpoint tail — without an id the handler
	// must not guess one.
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("GET without id: status=%d, want 400", rr.Code)
	}
}

// ── POST /api/autonomous/prds/{id}/decompose-lint ──────────────────────

func lintReportFixture() map[string]any {
	return map[string]any{
		"prd":        "prd-1",
		"mode":       "shadow",
		"rule_count": 1,
		"findings":   []any{map[string]any{"rule": "no-verification-less-tasks", "task": "T1", "severity": "block", "message": "no verification"}},
		"summary":    map[string]any{"block": 1, "warn": 0},
	}
}

// TestDecomposeLintPRDHandler_POST_DryRun runs the check (via the API
// layer) and returns the report; the PRD id is forwarded verbatim.
func TestDecomposeLintPRDHandler_POST_DryRun(t *testing.T) {
	spy := &decomposeLintSpy{dryRun: lintReportFixture()}
	s := newDecomposeLintTestServer(spy)

	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/decompose-lint", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintPRD(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST dry-run: status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("POST dry-run: body not JSON: %v (%s)", err, rr.Body.String())
	}
	if got := out["mode"]; got != "shadow" {
		t.Errorf("POST dry-run: mode = %v, want shadow", got)
	}
	if got := spy.dryRunCalls; len(got) != 1 || got[0] != "prd-1" {
		t.Errorf("POST dry-run: calls = %v, want [prd-1]", got)
	}
	if spy.setLintCalls != 0 {
		t.Errorf("POST dry-run must not touch the config, setLintCalls = %d", spy.setLintCalls)
	}
}

// TestDecomposeLintPRDHandler_POST_NotFound maps the API layer's
// "not found" to a 404, distinct from a validation-style 400.
func TestDecomposeLintPRDHandler_POST_NotFound(t *testing.T) {
	spy := &decomposeLintSpy{dryRunErr: errLintPRDNotFound}
	s := newDecomposeLintTestServer(spy)

	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/missing/decompose-lint", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintPRD(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("POST missing: status=%d, want 404", rr.Code)
	}
}

// TestDecomposeLintPRDHandler_POST_ValidationError maps every other API
// error to 400.
func TestDecomposeLintPRDHandler_POST_ValidationError(t *testing.T) {
	spy := &decomposeLintSpy{dryRunErr: errBadDecomposeLint}
	s := newDecomposeLintTestServer(spy)

	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/decompose-lint", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousDecomposeLintPRD(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("POST invalid: status=%d, want 400", rr.Code)
	}
}

// TestDecomposeLintPRDHandler_MethodNotAllowed pins 405 for methods the
// per-PRD endpoint does not serve (GET and POST cover the whole surface).
func TestDecomposeLintPRDHandler_MethodNotAllowed(t *testing.T) {
	spy := &decomposeLintSpy{}
	s := newDecomposeLintTestServer(spy)

	for _, m := range []string{http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(m, "/api/autonomous/prds/prd-1/decompose-lint", nil)
		rr := httptest.NewRecorder()
		s.handleAutonomousDecomposeLintPRD(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status=%d, want 405", m, rr.Code)
		}
	}
}
