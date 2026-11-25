package server

// The switch autonomous.merge_deploy_run_enabled: off by default, and while
// it is off a merge run or deploy run request changes nothing, names the
// setting and leaves an audit entry. Reads stay available.

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// switchServer is a live-wired server over a real repository with one branch
// waiting to merge, a pipeline that would run, a deploy build command that
// leaves a marker file and an audit log.
func switchServer(t *testing.T) (s *Server, mgr *autonomous.Manager, repo, marker string, ship *postShipSpyShip, log *audit.Log) {
	t.Helper()
	repo = newPostShipRepo(t)
	s, mgr = liveAPIServer(t, repo)
	marker = filepath.Join(t.TempDir(), "deploy-ran")
	s.cfg.Autonomous.Deploy = config.DeployConfig{BuildCommand: "touch " + marker}
	s.cfg.Autonomous.MergePipeline = config.MergePipelineConfig{
		VersionFiles: []string{"cmd/datawatch/main.go", "internal/server/api.go"},
	}
	ship = &postShipSpyShip{}
	s.mergeShip = ship
	var err error
	log, err = audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	s.SetAuditLog(log)
	return s, mgr, repo, marker, ship, log
}

func switchHead(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "for-each-ref", "--format=%(refname) %(objectname)").CombinedOutput()
	if err != nil {
		t.Fatalf("for-each-ref: %v %s", err, out)
	}
	return string(out)
}

const switchMergeBody = `{"title":"t","branches":["feature"]}`

func TestMergeDeployRunSwitch_DefaultIsOff(t *testing.T) {
	if config.DefaultConfig().Autonomous.MergeDeployRunEnabled {
		t.Fatal("autonomous.merge_deploy_run_enabled must default to false")
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("autonomous:\n  push_and_tag: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Autonomous.MergeDeployRunEnabled {
		t.Fatal("a config file without the key must read it as false")
	}
}

func TestMergeDeployRunSwitch_OffRefusesAndChangesNothing(t *testing.T) {
	s, mgr, repo, marker, ship, _ := switchServer(t)
	before := switchHead(t, repo)
	for name, h := range map[string]http.HandlerFunc{"merge/run": s.handleAutonomousMergeRun, "deploy/run": s.handleAutonomousDeployRun} {
		rr := liveDo(h, http.MethodPost, "/api/autonomous/"+name, switchMergeBody)
		if rr.Code != http.StatusForbidden {
			t.Errorf("POST %s: %d, want 403: %s", name, rr.Code, rr.Body.String())
		}
		if got := strings.TrimSpace(rr.Body.String()); got != config.MergeDeployRunRefusal {
			t.Errorf("POST %s: body %q, want the refusal", name, got)
		}
		if !strings.Contains(rr.Body.String(), config.MergeDeployRunKey) {
			t.Errorf("POST %s: the refusal does not name the setting", name)
		}
	}
	if after := switchHead(t, repo); after != before {
		t.Errorf("refs changed while the switch is off:\n%s\n%s", before, after)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("the deploy build command ran while the switch is off (stat: %v)", err)
	}
	if runs := mgr.MergeRuns(0); len(runs) != 0 {
		t.Errorf("%d merge run(s) recorded while the switch is off", len(runs))
	}
	if calls := ship.calls; len(calls) != 0 {
		t.Errorf("ship calls while the switch is off: %v", calls)
	}
}

// The refusal comes before the request is read, so a malformed or empty
// request gets the same answer, and a missing config reads as off.
func TestMergeDeployRunSwitch_OffAnswersAnyBodyAndNoConfig(t *testing.T) {
	s, _, _, _, _, _ := switchServer(t)
	if rr := liveDo(s.handleAutonomousMergeRun, http.MethodPost, "/api/autonomous/merge/run", `{`); rr.Code != http.StatusForbidden {
		t.Errorf("bad body while off: %d %s", rr.Code, rr.Body.String())
	}
	bare := &Server{autonomousMgr: &mergeRunSpy{repo: t.TempDir()}, dataDir: t.TempDir()}
	for name, h := range map[string]http.HandlerFunc{"merge/run": bare.handleAutonomousMergeRun, "deploy/run": bare.handleAutonomousDeployRun} {
		if rr := liveDo(h, http.MethodPost, "/api/autonomous/"+name, switchMergeBody); rr.Code != http.StatusForbidden {
			t.Errorf("no config, POST %s: %d %s", name, rr.Code, rr.Body.String())
		}
	}
}

func TestMergeDeployRunSwitch_OffWritesAnAuditEntry(t *testing.T) {
	s, _, _, _, _, log := switchServer(t)
	liveDo(s.handleAutonomousMergeRun, http.MethodPost, "/api/autonomous/merge/run", switchMergeBody)
	liveDo(s.handleAutonomousDeployRun, http.MethodPost, "/api/autonomous/deploy/run", "")
	entries, err := log.Read(audit.QueryFilter{Action: audit.ActionMergeDeployRunRefused})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]audit.Entry{}
	for _, e := range entries {
		got[e.Subject] = e
	}
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want 2: %+v", len(entries), entries)
	}
	for subject, route := range map[string]string{"merge_run": "/api/autonomous/merge/run", "deploy_run": "/api/autonomous/deploy/run"} {
		e, ok := got[subject]
		if !ok {
			t.Fatalf("no entry with subject %s", subject)
		}
		if e.Actor != "operator" || e.Details["setting"] != config.MergeDeployRunKey ||
			e.Details["route"] != route || e.Details["outcome"] != "refused" {
			t.Errorf("%s entry = %+v", subject, e)
		}
	}
}

func TestMergeDeployRunSwitch_OnRunsBothRoutes(t *testing.T) {
	s, mgr, _, marker, _, log := switchServer(t)
	s.cfg.Autonomous.MergeDeployRunEnabled = true
	if rr := liveDo(s.handleAutonomousMergeRun, http.MethodPost, "/api/autonomous/merge/run", switchMergeBody); rr.Code != http.StatusOK {
		t.Fatalf("merge run with the switch on: %d %s", rr.Code, rr.Body.String())
	}
	if runs := mgr.MergeRuns(0); len(runs) != 1 {
		t.Errorf("merge runs recorded = %d, want 1", len(runs))
	}
	if rr := liveDo(s.handleAutonomousDeployRun, http.MethodPost, "/api/autonomous/deploy/run", ""); rr.Code != http.StatusOK {
		t.Fatalf("deploy run with the switch on: %d %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the deploy build command did not run with the switch on: %v", err)
	}
	entries, err := log.Read(audit.QueryFilter{Action: audit.ActionMergeDeployRunRefused})
	if err != nil || len(entries) != 0 {
		t.Errorf("refusal entries with the switch on: %v %+v", err, entries)
	}
}

func TestMergeDeployRunSwitch_ReadsStayAvailableAndShowTheSwitch(t *testing.T) {
	s, _, _, _, _, _ := switchServer(t)
	for _, on := range []bool{false, true} {
		s.cfg.Autonomous.MergeDeployRunEnabled = on
		rr := liveDo(s.handleAutonomousDeploy, http.MethodGet, "/api/autonomous/deploy", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("GET deploy (on=%v): %d %s", on, rr.Code, rr.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if got, ok := body["run_enabled"].(bool); !ok || got != on {
			t.Errorf("GET deploy (on=%v): run_enabled = %v", on, body["run_enabled"])
		}
		if rr := liveDo(s.handleAutonomousMerge, http.MethodGet, "/api/autonomous/merge", ""); rr.Code != http.StatusOK {
			t.Errorf("GET merge (on=%v): %d %s", on, rr.Code, rr.Body.String())
		}
	}
}

func TestMergeDeployRunSwitch_ConfigRoundTrip(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	read := func() any {
		rr := liveDo(s.handleGetConfig, http.MethodGet, "/api/config", "")
		var out map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("GET config: %v %s", err, rr.Body.String())
		}
		a, _ := out["autonomous"].(map[string]any)
		return a["merge_deploy_run_enabled"]
	}
	if got := read(); got != false {
		t.Fatalf("default in GET /api/config = %v, want false", got)
	}
	// A JSON boolean (REST, web) and a text value (CLI, MCP, chat).
	for _, c := range []struct {
		value any
		want  bool
	}{{true, true}, {false, false}, {"true", true}, {"false", false}, {true, true}} {
		if rr := putMergePipelineConfig(s, map[string]any{config.MergeDeployRunKey: c.value}); rr.Code != http.StatusOK {
			t.Fatalf("PUT %#v: %d %s", c.value, rr.Code, rr.Body.String())
		}
		if got := read(); got != c.want {
			t.Fatalf("after PUT %#v, GET = %v, want %v", c.value, got, c.want)
		}
		saved, err := config.Load(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Autonomous.MergeDeployRunEnabled != c.want {
			t.Fatalf("after PUT %#v, the saved file reads %v", c.value, saved.Autonomous.MergeDeployRunEnabled)
		}
	}
	// A value that is not a boolean refuses the whole write.
	for _, bad := range []any{"yes please", 1.0, nil} {
		rr := putMergePipelineConfig(s, map[string]any{config.MergeDeployRunKey: bad})
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), config.MergeDeployRunKey) {
			t.Errorf("PUT %#v: %d %s, want 400 naming the key", bad, rr.Code, rr.Body.String())
		}
		if got := read(); got != true {
			t.Errorf("PUT %#v changed the value to %v", bad, got)
		}
	}
}
