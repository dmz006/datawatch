// BL-stale-config — handler-level proof that a runtime autonomous
// config set lands in the very read path the daemon's verify/guardrail
// closures use: PUT /api/autonomous/config mutates the live Manager,
// and the NEXT Manager.Config() snapshot (what main.go's
// liveAutonomousConfig closure takes on every invocation) carries the
// new verification_backend / verification_backends / verification_model
// values. No daemon restart, no Manager re-creation.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestHandleAutonomousConfig_PutThenLiveReadReflectsChange(t *testing.T) {
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.Config{
		VerificationBackend:  "ollama",
		VerificationModel:    "boot-verify-model",
		VerificationBackends: []string{"boot-pool"},
		PlanningBackend:      "ollama",
		ExecutionBackend:     "claude-code",
	}, func(autonomous.DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Same adapter the daemon wires: NewAPI(mgr) satisfies the server
	// package's AutonomousAPI interface.
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: autonomous.NewAPI(mgr),
	}

	set := httptest.NewRequest(http.MethodPut, "/api/autonomous/config", bytes.NewReader([]byte(`{
		"verification_backend": "openwebui",
		"verification_model": "live-verify-model",
		"verification_backends": ["live-pool-a", "live-pool-b"],
		"planning_backend": "openwebui"
	}`)))
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, set)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/autonomous/config = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	// The closure path: the daemon's autonomousVerify/autonomousGuardrail
	// read the Manager's live Config() on every call — exactly this.
	c := mgr.Config()
	if c.VerificationBackend != "openwebui" {
		t.Errorf("VerificationBackend = %q, want openwebui", c.VerificationBackend)
	}
	if c.VerificationModel != "live-verify-model" {
		t.Errorf("VerificationModel = %q, want live-verify-model", c.VerificationModel)
	}
	if !reflect.DeepEqual(c.VerificationBackends, []string{"live-pool-a", "live-pool-b"}) {
		t.Errorf("VerificationBackends = %v, want live pools", c.VerificationBackends)
	}
	if c.PlanningBackend != "openwebui" {
		t.Errorf("PlanningBackend = %q, want openwebui", c.PlanningBackend)
	}
	if c.ExecutionBackend != "claude-code" {
		t.Errorf("ExecutionBackend = %q, want boot value claude-code (untouched by partial PUT)", c.ExecutionBackend)
	}

	// And through the public handler read (GET) — what an operator's
	// config-get or another closure would observe.
	get := httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil)
	grr := httptest.NewRecorder()
	s.handleAutonomousConfig(grr, get)
	if grr.Code != http.StatusOK {
		t.Fatalf("GET /api/autonomous/config = %d, want 200: %s", grr.Code, grr.Body.String())
	}
	var got struct {
		VerificationBackend  string   `json:"verification_backend"`
		VerificationBackends []string `json:"verification_backends"`
		VerificationModel    string   `json:"verification_model"`
	}
	_ = json.NewDecoder(grr.Body).Decode(&got)
	if got.VerificationBackend != "openwebui" || got.VerificationModel != "live-verify-model" ||
		!reflect.DeepEqual(got.VerificationBackends, []string{"live-pool-a", "live-pool-b"}) {
		t.Errorf("GET read = %+v, want the live values set by the PUT", got)
	}
}
