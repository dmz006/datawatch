package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

const specCheckDirty = "Fix the crash described in BL123 for the Automaton."
const specCheckClean = "Add a retry to the nightly backup job."

func newSpecCheckServer(t *testing.T) (*Server, *autonomous.Manager) {
	t.Helper()
	mgr, err := autonomous.NewManager(filepath.Join(t.TempDir(), "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return &Server{cfg: config.DefaultConfig(), autonomousMgr: newTestAPI(mgr)}, mgr
}

func specCheckPost(t *testing.T, s *Server, path string, body any) map[string]json.RawMessage {
	t.Helper()
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("POST %s: %d %s", path, rr.Code, rr.Body.String())
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("reply not JSON: %v", err)
	}
	return out
}

func specCheckOf(t *testing.T, reply map[string]json.RawMessage) []autonomous.SpecFinding {
	t.Helper()
	raw, ok := reply["spec_check"]
	if !ok {
		t.Fatalf("reply has no spec_check: %v", reply)
	}
	var f []autonomous.SpecFinding
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("spec_check: %v", err)
	}
	if f == nil {
		t.Fatal("spec_check must be an array, not null")
	}
	return f
}

func TestSpecCheckCreateShowsFindingsAndStillCreates(t *testing.T) {
	s, mgr := newSpecCheckServer(t)
	reply := specCheckPost(t, s, "/api/autonomous/prds", map[string]any{"spec": specCheckDirty, "project_dir": t.TempDir()})
	f := specCheckOf(t, reply)
	if len(f) != 1 || f[0].Phrase != "BL123" || f[0].Rule != autonomous.SpecRuleTrackerID || !f[0].Correctable || f[0].Severity != autonomous.SpecSeverityWarning || f[0].Suggestion == "" {
		t.Fatalf("findings = %#v", f)
	}
	var id string
	_ = json.Unmarshal(reply["id"], &id)
	if _, ok := mgr.Store().GetPRD(id); !ok {
		t.Fatal("the create must not be blocked by a finding")
	}
}

func TestSpecCheckCreateCleanSpecShowsNone(t *testing.T) {
	s, _ := newSpecCheckServer(t)
	reply := specCheckPost(t, s, "/api/autonomous/prds", map[string]any{"spec": specCheckClean, "project_dir": t.TempDir()})
	if f := specCheckOf(t, reply); len(f) != 0 {
		t.Fatalf("clean spec gave %#v", f)
	}
}

func TestSpecCheckReviseShowsFindingsAndCleanShowsNone(t *testing.T) {
	s, mgr := newSpecCheckServer(t)
	for _, c := range []struct {
		spec string
		want int
	}{{specCheckDirty, 1}, {specCheckClean, 0}} {
		prd, err := mgr.CreatePRD(c.spec, t.TempDir(), "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		mgr.Store().WithLock(func() { prd.Status = autonomous.PRDNeedsReview })
		if err := mgr.Store().SavePRD(prd); err != nil {
			t.Fatal(err)
		}
		reply := specCheckPost(t, s, "/api/autonomous/prds/"+prd.ID+"/request_revision", map[string]any{"note": "tighten it"})
		if f := specCheckOf(t, reply); len(f) != c.want {
			t.Fatalf("spec %q: want %d findings, got %#v", c.spec, c.want, f)
		}
	}
}
