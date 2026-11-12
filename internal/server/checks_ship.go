// Checks and ship status reads: GET /api/autonomous/checks and
// GET /api/autonomous/ship, plus the autonomous.checks.* and
// autonomous.ship.* settings on the config surface. Both reads are
// read-only views over the findings the checks and the ship step already
// record on the Automaton's triage record.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/federation"
)

// checksShipStatusRead is the shared preamble: availability, method and
// capability gates, then the records and the project directory.
func (s *Server) checksShipStatusRead(w http.ResponseWriter, r *http.Request) (recs []*autonomous.TriageRecord, prdID, dir string, ok bool) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	tr, isReader := s.autonomousMgr.(triageReader)
	if !isReader {
		http.Error(w, "triage records unavailable", http.StatusServiceUnavailable)
		return
	}
	prdID = strings.TrimSpace(r.URL.Query().Get("prd_id"))
	dir = strings.TrimSpace(r.URL.Query().Get("project_dir"))
	return tr.ListTriageRecords(), prdID, dir, true
}

// handleAutonomousChecks serves GET /api/autonomous/checks: the resolved
// checks policy and the recorded check findings, newest first. prd_id narrows
// the findings to one Automaton; project_dir picks the per-project policy.
func (s *Server) handleAutonomousChecks(w http.ResponseWriter, r *http.Request) {
	recs, prdID, dir, ok := s.checksShipStatusRead(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	st := autonomous.BuildChecksStatus(recs, prdID, dir)
	st.Attribution = s.attributionStatus()
	_ = json.NewEncoder(w).Encode(st)
}

// handleAutonomousShip serves GET /api/autonomous/ship: the resolved ship
// policy and the recorded push, tag and CI-watch outcomes, newest first.
func (s *Server) handleAutonomousShip(w http.ResponseWriter, r *http.Request) {
	recs, prdID, dir, ok := s.checksShipStatusRead(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(autonomous.BuildShipStatus(recs, prdID, dir))
}

// ChecksShipRESTOperations lists the two status reads, for the parity test.
func ChecksShipRESTOperations() []TriageRESTOperation {
	return []TriageRESTOperation{
		{"checks", http.MethodGet, "/api/autonomous/checks"},
		{"ship", http.MethodGet, "/api/autonomous/ship"},
	}
}

var checksModeKeys = []string{
	"autonomous.checks.pre_start_verifier", "autonomous.checks.plan_rules",
	"autonomous.checks.after_done", "autonomous.checks.registry_readme",
	"autonomous.ship.push", "autonomous.ship.tag", "autonomous.ship.ci_watch",
}

// validateChecksShipPatch refuses the whole PUT when a checks or ship value
// is invalid: modes must be off, shadow or on; block_when_missing a boolean;
// tag_format must contain the version placeholder; ci_workflows a list.
func validateChecksShipPatch(patch map[string]interface{}) error {
	for _, k := range checksModeKeys {
		v, ok := patch[k]
		if !ok {
			continue
		}
		str, isStr := v.(string)
		if !isStr {
			return fmt.Errorf("%s: must be \"off\", \"shadow\" or \"on\"", k)
		}
		if err := config.ValidateChecksMode(strings.TrimPrefix(k, "autonomous."), str); err != nil {
			return fmt.Errorf("%s: must be \"off\", \"shadow\" or \"on\" (got %q)", k, str)
		}
	}
	if v, ok := patch["autonomous.checks.block_when_missing"]; ok {
		if _, isBool := v.(bool); !isBool {
			return fmt.Errorf("autonomous.checks.block_when_missing: must be a boolean (true or false)")
		}
	}
	if v, ok := patch["autonomous.ship.tag_format"]; ok {
		str, isStr := v.(string)
		if !isStr {
			return fmt.Errorf("autonomous.ship.tag_format: must be a string")
		}
		if err := config.ValidateShipTagFormat("tag_format", str); err != nil {
			return err
		}
	}
	if v, ok := patch["autonomous.ship.remote"]; ok {
		if _, isStr := v.(string); !isStr {
			return fmt.Errorf("autonomous.ship.remote: must be a string")
		}
	}
	if v, ok := patch["autonomous.ship.ci_workflows"]; ok {
		if _, isList := toStringArray(v); !isList {
			if _, isStr := v.(string); !isStr {
				return fmt.Errorf("autonomous.ship.ci_workflows: must be a list of workflow names or a comma-separated string")
			}
		}
	}
	return nil
}

// applyChecksShipKey stores one already-validated checks or ship setting.
func applyChecksShipKey(cfg *config.Config, k string, v interface{}) {
	c, sh := &cfg.Autonomous.Checks, &cfg.Autonomous.Ship
	str, _ := v.(string)
	str = strings.TrimSpace(str)
	switch k {
	case "autonomous.checks.pre_start_verifier":
		c.PreStartVerifier = config.NormalizeChecksMode(str)
	case "autonomous.checks.plan_rules":
		c.PlanRules = config.NormalizeChecksMode(str)
	case "autonomous.checks.after_done":
		c.AfterDone = config.NormalizeChecksMode(str)
	case "autonomous.checks.registry_readme":
		c.RegistryReadme = config.NormalizeChecksMode(str)
	case "autonomous.checks.block_when_missing":
		if b, ok := v.(bool); ok {
			c.BlockWhenMissing = &b
		}
	case "autonomous.ship.push":
		sh.Push = config.NormalizeChecksMode(str)
	case "autonomous.ship.tag":
		sh.Tag = config.NormalizeChecksMode(str)
	case "autonomous.ship.ci_watch":
		sh.CIWatch = config.NormalizeChecksMode(str)
	case "autonomous.ship.tag_format":
		sh.TagFormat = str
	case "autonomous.ship.remote":
		sh.Remote = str
	case "autonomous.ship.ci_workflows":
		if arr, ok := toStringArray(v); ok {
			sh.CIWorkflows = arr
		} else {
			sh.CIWorkflows = splitCSV(str)
		}
	}
}

// checksShipConfigDisplay is the effective value of every checks and ship
// key for GET /api/config: an empty mode shows its shipped default (on).
func checksShipConfigDisplay(cfg *config.Config) map[string]interface{} {
	mode := func(m string) string {
		if m = config.NormalizeChecksMode(m); m != "" {
			return m
		}
		return config.ChecksModeOn
	}
	c, sh := cfg.Autonomous.Checks, cfg.Autonomous.Ship
	block := c.BlockWhenMissing == nil || *c.BlockWhenMissing
	tagFmt, remote := sh.TagFormat, sh.Remote
	if strings.TrimSpace(tagFmt) == "" {
		tagFmt = config.ShipDefaultTagFormat
	}
	if strings.TrimSpace(remote) == "" {
		remote = config.ShipDefaultRemote
	}
	wf := sh.CIWorkflows
	if wf == nil {
		wf = []string{}
	}
	return map[string]interface{}{
		"checks": map[string]interface{}{
			"pre_start_verifier": mode(c.PreStartVerifier),
			"block_when_missing": block,
			"plan_rules":         mode(c.PlanRules),
			"after_done":         mode(c.AfterDone),
			"registry_readme":    mode(c.RegistryReadme),
		},
		"ship": map[string]interface{}{
			"push": mode(sh.Push), "tag": mode(sh.Tag), "ci_watch": mode(sh.CIWatch),
			"tag_format": tagFmt, "remote": remote, "ci_workflows": wf,
		},
	}
}

// attributionStatus runs the cost-report attribution check over the whole
// spend ledger; nil when no ledger is wired.
func (s *Server) attributionStatus() *autonomous.AttributionStatus {
	if s.costReportLedger == nil {
		return nil
	}
	return attributionStatusFrom(s.costReportLedger)
}

func attributionStatusFrom(l spendLedgerReader) *autonomous.AttributionStatus {
	var recs []costreport.Record
	for _, e := range l.Entries() {
		recs = append(recs, costreport.Record{SessionID: e.SessionID, TaskID: e.TaskID,
			Role: e.Role, FromLedger: true, LedgerAutomatonID: e.AutomatonID})
	}
	c := costreport.CheckAttribution(recs, time.Time{}, time.Time{}, true)
	return &autonomous.AttributionStatus{AutomatonLaunched: c.AutomatonLaunched,
		Unattributed: c.Unattributed, SessionIDs: c.SessionIDs, OK: c.OK, Note: c.Note}
}
