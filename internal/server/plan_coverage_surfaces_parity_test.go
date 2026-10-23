// plan_coverage_surfaces_parity_test.go — the one parity test proving
// that the plan-requirement coverage gate settings and the operator
// acknowledgement flow are reachable AND consistent on every operator
// surface: REST config, MCP tool schema, CLI, and the comm channel.
//
// Strategy: the shared contract literals (both config keys, the bare
// setting names, the acknowledgement action) are defined ONCE below.
// The REST + Manager path is exercised SEMANTICALLY here (the same
// wiring main.go uses: the real autonomous.Manager behind
// autonomous.API, real handlers, real config persistence). The other
// three surfaces are thin pass-throughs/proxies over REST, so for them
// the test verifies the shared literals mechanically against each
// surface's source — a rename or path drift on ANY surface fails it.
//
// The gate's own semantics (block stops approve, ack unblocks it,
// fingerprint scoping, warn/off non-gating) are pinned in
// internal/autonomous/plan_coverage_gate_test.go — this test pins the
// SURFACE WIRING around them, not the gate itself.
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/router"
)

// ── Shared contract (single source of truth for this test) ──────────
//
// Every surface must agree on these literals; a mismatch on any
// surface must fail this test.
const (
	// Full config keys — the REST /api/config form, and the form the
	// CLI (`config set`) and the comm channel send as-is.
	pcCheckKey = "autonomous.plan_coverage_check"
	pcRatioKey = "autonomous.plan_coverage_min_ratio"

	// Bare setting names — the /api/autonomous/config form, and the
	// MCP tool argument names.
	pcCheckArg = "plan_coverage_check"
	pcRatioArg = "plan_coverage_min_ratio"

	// REST acknowledgement action (subpath of /api/autonomous/prds/{id}/
	//), the ack tool name, and the CLI verbs.
	pcAckAction = "plan_coverage/acknowledge"
	pcAckTool   = "autonomous_prd_plan_coverage_acknowledge"
	pcCLIVerb   = "prd-coverage"
	pcAckCLI    = "prd-ack-coverage"
)

// gateSpec / gateFailingPlan mirror the internal/autonomous gate
// fixtures: three extractable numbered requirements and a plan that
// silently drops requirement 2. Defined here (not imported — this test
// lives outside the autonomous package) for ONE failing surface to
// prove the report+ack wiring through the real handlers.
const pcGateSpec = `1. implement login form
2. enforce rate limiting
3. emit audit trail entries
`

const pcGateFailingPlan = `{"title":"feature","stories":[
	{"title":"login form work","tasks":[{"title":"implement login form","spec":"build the login form","verification_script":"go test ./..."}]},
	{"title":"audit trail work","tasks":[{"title":"emit audit trail entries","spec":"emit audit trail entries","verification_script":"go test ./..."}]}
]}`

// newPlanCoverageServer — the same wiring main.go uses: a real
// autonomous.Manager behind autonomous.API (the adapter the ack handler
// type-switches on), a real config and a writable config path.
func newPlanCoverageServer(t *testing.T, mode string, minRatio float64, planJSON string) (*Server, *autonomous.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := autonomous.DefaultConfig()
	cfg.PlanCoverageCheck = mode
	cfg.PlanCoverageMinRatio = minRatio
	mgr, err := autonomous.NewManager(filepath.Join(dir, "autonomous"), cfg, func(req autonomous.DecomposeRequest) (string, error) {
		return planJSON, nil
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	s := &Server{
		cfg:           config.DefaultConfig(),
		cfgPath:       cfgPath,
		autonomousMgr: autonomous.NewAPI(mgr),
	}
	return s, mgr, cfgPath
}

// srcMustContain — the non-REST surfaces are pass-throughs to the
// shared REST contract, so each one's source must contain exactly the
// shared literals it promises to use (built from the constants above).
func srcMustContain(t *testing.T, relPath string, literals ...string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean("../../" + relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	for _, lit := range literals {
		if !strings.Contains(string(raw), lit) {
			t.Fatalf("%s: missing shared contract literal %q", relPath, lit)
		}
	}
}

func TestPlanCoverage_SurfacesParity(t *testing.T) {
	t.Run("REST config surface", func(t *testing.T) {
		s, mgr, cfgPath := newPlanCoverageServer(t, "block", 1.0, pcGateFailingPlan)

		// Defaults always visible on GET /api/config's autonomous
		// section: the safe pair (block / 1).
		aut := getAutonomousConfigMap(t, s)
		if g := string(aut[pcCheckArg]); g != `"block"` {
			t.Fatalf("default %s = %s, want \"block\"", pcCheckKey, g)
		}
		if g := string(aut[pcRatioArg]); !strings.Contains(g, "1") {
			t.Fatalf("default %s = %s, want 1", pcRatioKey, g)
		}

		// Round-trip through PUT /api/config in the exact CLI/comm
		// shape (string values, full dash-prefixed keys).
		pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{pcCheckKey: "warn", pcRatioKey: "0.9"})
		aut = getAutonomousConfigMap(t, s)
		if g := string(aut[pcCheckArg]); g != `"warn"` {
			t.Fatalf("after PUT %s: %s = %s, want \"warn\"", pcCheckKey, pcCheckKey, g)
		}
		if g := string(aut[pcRatioArg]); !strings.Contains(g, "0.9") {
			t.Fatalf("after PUT %s: %s = %s, want 0.9", pcRatioKey, pcRatioKey, g)
		}
		if m := mgr.Config(); m.PlanCoverageCheck != "warn" || m.PlanCoverageMinRatio != 0.9 {
			t.Fatalf("live manager after PUT = %q / %v, want warn / 0.9", m.PlanCoverageCheck, m.PlanCoverageMinRatio)
		}
		raw, _ := os.ReadFile(cfgPath)
		if !strings.Contains(string(raw), "plan_coverage_check: warn") || !strings.Contains(string(raw), "plan_coverage_min_ratio: 0.9") {
			t.Fatalf("config.yaml not persisted:\n%s", raw)
		}

		// Bare-key form on the dedicated autonomous config surface —
		// the exact shape the MCP autonomous_config_set tool sends.
		pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{pcCheckArg: "block", pcRatioArg: 1.0})
		aut = getAutonomousConfigMap(t, s)
		if g := string(aut[pcCheckArg]); g != `"block"` {
			t.Fatalf("after autonomous-config PUT: %s = %s, want \"block\"", pcCheckKey, g)
		}
		if m := mgr.Config(); m.PlanCoverageCheck != "block" || m.PlanCoverageMinRatio != 1.0 {
			t.Fatalf("live manager after autonomous-config PUT = %q / %v, want block / 1.0", m.PlanCoverageCheck, m.PlanCoverageMinRatio)
		}

		// Out-of-range-but-numeric ratio: accepted by REST — the EFFECTIVE
		// contract (the gate and the config display both resolve through
		// config.NormalizePlanCoverageRatio) clamps it to the 1.0 safe
		// default, the "clamp to 1.0" contract every surface documents.
		pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{pcRatioArg: 0})
		eff, err := mgr.EffectivePlanCoverage()
		if err != nil {
			t.Fatalf("EffectivePlanCoverage: %v", err)
		}
		if eff.MinRatio != 1.0 {
			t.Fatalf("ratio 0 must be effective 1.0, got %v", eff.MinRatio)
		}
		// And the GET /api/config surface displays the clamped value.
		aut = getAutonomousConfigMap(t, s)
		if g := string(aut[pcRatioArg]); strings.Contains(g, "0") {
			t.Fatalf("displayed %s after PUT 0 = %s, want the 1.0 clamp", pcRatioKey, g)
		}

		// Invalid values are rejected on BOTH config surfaces.
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{pcCheckKey: "bogus"})
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{pcRatioKey: "not-a-number"})
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{pcRatioKey: true})
		pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{pcCheckArg: "bogus"})
		pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{pcRatioArg: "oops"})
	})

	t.Run("REST report + ack surface", func(t *testing.T) {
		s, mgr, _ := newPlanCoverageServer(t, "block", 1.0, pcGateFailingPlan)

		draft, err := mgr.CreatePRD(pcGateSpec, t.TempDir(), "", "", "")
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		prd, err := mgr.Decompose(draft.ID)
		if err != nil {
			t.Fatalf("Decompose: %v", err)
		}
		if prd.PlanCoverage == nil || !prd.PlanCoverage.IsFailing() {
			t.Fatalf("stored report should be failing: %+v", prd.PlanCoverage)
		}

		// The report is visible on the PRD GET (detail=full) — the same
		// call the CLI prd-coverage command proxies.
		rr := httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+prd.ID+"?detail=full", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET PRD: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var rawPRD map[string]json.RawMessage
		if err := json.Unmarshal(rr.Body.Bytes(), &rawPRD); err != nil {
			t.Fatalf("decode PRD: %v", err)
		}
		pcBody, ok := rawPRD["plan_coverage"]
		if !ok {
			t.Fatalf("PRD GET (detail=full) lacks plan_coverage: %s", rr.Body.String())
		}
		var pc map[string]any
		if err := json.Unmarshal(pcBody, &pc); err != nil {
			t.Fatalf("decode plan_coverage: %v", err)
		}
		if got, _ := pc["mode"].(string); got != "block" {
			t.Fatalf("plan_coverage.mode = %v, want block", pc["mode"])
		}

		// Approve via REST is blocked in block mode with a failing
		// report; the gate error names the dropped requirement.
		rr = httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/approve", strings.NewReader(`{"actor":"operator"}`)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("approve with failing report: want 400, got %d body=%s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "enforce rate limiting") {
			t.Fatalf("gate error must name the unmatched requirement: %s", rr.Body.String())
		}

		// Ack without a non-empty note is rejected.
		rr = httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/"+pcAckAction, strings.NewReader(`{"actor":"operator","note":"  "}`)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("ack with blank note: want 400, got %d body=%s", rr.Code, rr.Body.String())
		}

		// Ack with a note lands (the same actor+note the CLI and MCP
		// surfaces send).
		rr = httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/"+pcAckAction, strings.NewReader(`{"actor":"parity-op","note":"rate limiting deferred — tracked on a separate task"}`)))
		if rr.Code != http.StatusOK {
			t.Fatalf("ack: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}

		// Approve after the matching ack proceeds and the PRD is approved.
		rr = httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/approve", strings.NewReader(`{"actor":"parity-op","note":"approved with ack"}`)))
		if rr.Code != http.StatusOK {
			t.Fatalf("approve after ack: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		final, _ := mgr.Store().GetPRD(prd.ID)
		if final == nil || final.Status != autonomous.PRDApproved {
			t.Fatalf("final status = %+v, want approved", final)
		}
	})

	t.Run("MCP surface", func(t *testing.T) {
		// Tool schema: both settings present as typed arguments.
		srcMustContain(t, "internal/mcp/autonomous.go",
			`WithString("`+pcCheckArg+`"`,
			`WithNumber("`+pcRatioArg+`"`,
			// Both settings reach the REST config surface MCP proxies.
			"/api/autonomous/config",
			// The ack tool: exact tool name + the exact REST ack path.
			`"`+pcAckTool+`"`,
			`"+id+"/`+pcAckAction+`"`,
			// Report visibility: the PRD tool advertises plan_coverage
			// on the detail=full path.
			"plan_coverage",
		)
		// The ack tool is registered on the MCP server.
		srcMustContain(t, "internal/mcp/server.go",
			"PlanCoverageAcknowledge",
		)
	})

	t.Run("CLI surface", func(t *testing.T) {
		// Autonomous verbs: report view + ack, both proxying the exact
		// REST paths this test proved.
		srcMustContain(t, "cmd/datawatch/cli_autonomous.go",
			"prd-coverage <id>",
			"prd-ack-coverage <id>",
			"detail=full",
			`"/api/autonomous/prds/"+args[0]+`+`"/`+pcAckAction+`"`,
		)
		// Generic config set (the `config set <key> <value>` pass-through
		// that carries both dash-prefixed keys to /api/config).
		srcMustContain(t, "cmd/datawatch/main.go",
			"set <key> <value>",
			"/api/config",
		)
	})

	t.Run("comm surface", func(t *testing.T) {
		// The comm parser routes both full keys into the configure
		// command unchanged (free pass-through to the configure func).
		for _, tc := range []struct{ text, wantText string }{
			{"configure " + pcCheckKey + "=warn", pcCheckKey + "=warn"},
			{"set " + pcRatioKey + "=0.9", pcRatioKey + "=0.9"},
		} {
			cmd := router.Parse(tc.text)
			if cmd.Type != router.CmdConfigure {
				t.Fatalf("Parse(%q) type = %v, want configure", tc.text, cmd.Type)
			}
			if cmd.Text != tc.wantText {
				t.Fatalf("Parse(%q) text = %q, want %q", tc.text, cmd.Text, tc.wantText)
			}
		}
		// The comm help documents both keys in both its help blocks.
		help := readRel(t, "internal/router/router.go", pcCheckKey, pcRatioKey)
		if c := strings.Count(help, pcCheckKey); c < 2 {
			t.Fatalf("comm help should document %s in both Common keys and Configurable keys blocks (found %d)", pcCheckKey, c)
		}
		// The daemon wires comm configure → PUT /api/config (the REST
		// surface already proved above).
		readRel(t, "cmd/datawatch/main.go", "SetConfigureFunc", "/api/config")
	})
}

// ── helpers ─────────────────────────────────────────────────────────

// readRel reads a repo file (relative to the parent of this package
// dir) and asserts it contains every literal.
func readRel(t *testing.T, relPath string, literals ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean("../../" + relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	for _, lit := range literals {
		if !strings.Contains(string(raw), lit) {
			t.Fatalf("%s: missing shared contract literal %q", relPath, lit)
		}
	}
	return string(raw)
}

func pcPutOK(t *testing.T, h http.HandlerFunc, path string, body any) {
	t.Helper()
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT %s %s: want 200, got %d body=%s", path, b, rr.Code, rr.Body.String())
	}
}

func pcPutBad(t *testing.T, h http.HandlerFunc, path string, body any) {
	t.Helper()
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT %s %s: want 400 for an invalid value, got %d body=%s", path, b, rr.Code, rr.Body.String())
	}
}
