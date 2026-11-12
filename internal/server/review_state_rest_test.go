// review_state on the PRD REST views: GET list rows and GET detail both
// carry the computed review_state (state, cause, action, attempts). The
// list row always serializes the field (a pending row is explicit, not
// absent); the detail view serializes what CapPRDText computed from the
// recorded verdicts. A judge-timeout hold reads timed_out with a cause that
// names the backends that timed out, and a stronger configured backend that
// none of the attempts tried yields the reroute action. The two strong-first
// routing config keys ride the /api/config GET/PUT surface the same way
// judge_size_threshold_lines does.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type reviewStateFake struct {
	fakeOrchAutonomous
	prds []*autonomous.PRD
}

func (f *reviewStateFake) ListPRDs() []any {
	out := make([]any, len(f.prds))
	for i, p := range f.prds {
		out[i] = p
	}
	return out
}

func (f *reviewStateFake) GetPRD(id string) (any, bool) {
	for _, p := range f.prds {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

var reviewStateBase = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)

// reviewStateFleet covers the four review states plus the reroute variant:
// a judge-timeout hold, a block whose stronger configured backend was never
// tried, a real judgment block, a pass, and no verdicts at all.
func reviewStateFleet(t *testing.T) []*autonomous.PRD {
	t.Helper()
	tmo := autonomous.JudgeOutcomeTimedOut
	return []*autonomous.PRD{
		{ID: "aa000001", Title: "timed out", Status: autonomous.PRDNeedsReview,
			CreatedAt: reviewStateBase, UpdatedAt: reviewStateBase,
			Verdicts: []autonomous.GuardrailVerdict{{
				Guardrail: "rules-review", Outcome: "block", Category: autonomous.JudgeTimeoutCategory,
				Summary:   "judge timed out on 2 backends (limit 30m0s)",
				VerdictAt: reviewStateBase,
				JudgeAttempts: []autonomous.JudgeAttemptRecord{
					{Backend: "pi", Outcome: tmo}, {Backend: "qwen", Outcome: tmo},
				},
			}}},
		{ID: "aa000002", Title: "rerouted", Status: autonomous.PRDNeedsReview,
			GuardrailBackend: "claude-code-strong",
			CreatedAt:        reviewStateBase, UpdatedAt: reviewStateBase,
			Verdicts: []autonomous.GuardrailVerdict{{
				Guardrail: "rules-review", Outcome: "block", Category: autonomous.JudgeTimeoutCategory,
				Summary:       "judge timed out on 1 backend (limit 30m0s)",
				VerdictAt:     reviewStateBase,
				JudgeAttempts: []autonomous.JudgeAttemptRecord{{Backend: "pi", Outcome: tmo}},
			}}},
		{ID: "aa000003", Title: "blocked", Status: autonomous.PRDBlocked,
			CreatedAt: reviewStateBase, UpdatedAt: reviewStateBase,
			Verdicts: []autonomous.GuardrailVerdict{{
				Guardrail: "rules-review", Outcome: "block",
				Summary:   "task t1 violates the retry budget",
				VerdictAt: reviewStateBase,
			}}},
		{ID: "aa000004", Title: "passed", Status: autonomous.PRDCompleted,
			CreatedAt: reviewStateBase, UpdatedAt: reviewStateBase,
			Verdicts: []autonomous.GuardrailVerdict{{
				Guardrail: "rules-review", Outcome: "pass",
				Summary: "clean", VerdictAt: reviewStateBase,
			}}},
		{ID: "aa000005", Title: "pending", Status: autonomous.PRDRunning,
			CreatedAt: reviewStateBase, UpdatedAt: reviewStateBase},
	}
}

func reviewStateServer(t *testing.T) *Server {
	t.Helper()
	return &Server{cfg: config.DefaultConfig(), autonomousMgr: &reviewStateFake{prds: reviewStateFleet(t)}}
}

// reviewStateKeys pulls the judge strong-first routing keys off the
// autonomous block of GET /api/config.
func reviewStateJudgeKeys(t *testing.T, s *Server) (string, bool) {
	t.Helper()
	aut := getAutonomousConfigMap(t, s)
	sb, ok := aut["judge_strong_backend"]
	if !ok {
		t.Fatalf("GET /api/config lacks judge_strong_backend")
	}
	var sbStr string
	if err := json.Unmarshal(sb, &sbStr); err != nil {
		t.Fatalf("judge_strong_backend is not a string: %s", sb)
	}
	cv, ok := aut["judge_verdict_cache_enabled"]
	if !ok {
		t.Fatalf("GET /api/config lacks judge_verdict_cache_enabled")
	}
	var enabled bool
	if err := json.Unmarshal(cv, &enabled); err != nil {
		t.Fatalf("judge_verdict_cache_enabled is not a boolean: %s", cv)
	}
	return sbStr, enabled
}

// Every list row carries an explicit review_state field: the pending row
// serializes "pending" rather than dropping the key, and the decided rows
// carry their state and action in the summary.
func TestReviewStateListRowsCarryState(t *testing.T) {
	s := reviewStateServer(t)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET list: status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Prds []struct {
			ID          string         `json:"id"`
			ReviewState map[string]any `json:"review_state"`
			ReviewKey   bool
		} `json:"prds"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Prds) != 5 {
		t.Fatalf("rows = %d, want 5", len(resp.Prds))
	}
	rows := map[string]map[string]any{}
	for _, p := range resp.Prds {
		rows[p.ID] = p.ReviewState
	}
	want := map[string]string{
		"aa000001": "timed_out", "aa000002": "timed_out", "aa000003": "blocked",
		"aa000004": "passed", "aa000005": "pending",
	}
	for id, st := range want {
		row, ok := rows[id]
		if !ok {
			t.Errorf("no row for %s", id)
			continue
		}
		if row["state"] != st {
			t.Errorf("%s: state = %v, want %s", id, row["state"], st)
		}
	}
	// The pending row must still carry the key (no omitempty on the row):
	// it is explicit, not absent.
	pending, ok := rows["aa000005"]
	if !ok || len(pending) == 0 {
		t.Error("pending row dropped review_state")
	}
	if a, _ := rows["aa000003"]["action"].(string); a != "fix" {
		t.Errorf("blocked row action = %v, want fix", a)
	}
}

// The detail view carries cause and action, computed from the recorded
// verdicts.
func TestReviewStateDetailCarriesCauseAndAction(t *testing.T) {
	s := reviewStateServer(t)
	detail := func(id string) map[string]any {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d: %s", id, rr.Code, rr.Body.String())
		}
		var prd struct {
			ReviewState map[string]any `json:"review_state"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &prd); err != nil {
			t.Fatal(err)
		}
		return prd.ReviewState
	}

	blocked := detail("aa000003")
	if blocked["state"] != "blocked" || blocked["action"] != "fix" {
		t.Errorf("blocked: %v", blocked)
	}
	if c, _ := blocked["cause"].(string); !strings.Contains(c, "rules-review") || !strings.Contains(c, "retry budget") {
		t.Errorf("blocked cause %q does not name the judgment", c)
	}

	passed := detail("aa000004")
	if passed["state"] != "passed" {
		t.Errorf("passed: %v", passed)
	}
}

// A judge-timeout hold reads timed_out, names the backends that timed
// out, and points at retry unless a configured stronger backend was never
// tried.
func TestReviewStateTimedOutReadsRetryOrReroute(t *testing.T) {
	s := reviewStateServer(t)
	detail := func(id string) map[string]any {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d: %s", id, rr.Code, rr.Body.String())
		}
		var prd struct {
			ReviewState map[string]any `json:"review_state"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &prd); err != nil {
			t.Fatal(err)
		}
		return prd.ReviewState
	}

	held := detail("aa000001")
	if held["state"] != "timed_out" || held["action"] != "retry" {
		t.Fatalf("held: state=%v action=%v, want timed_out/retry", held["state"], held["action"])
	}
	c, _ := held["cause"].(string)
	if !strings.Contains(c, "pi") || !strings.Contains(c, "qwen") || !strings.Contains(c, "30m0s") {
		t.Errorf("hold cause %q must name both backends and the limit", c)
	}
	if held["attempts"] != float64(2) {
		t.Errorf("hold attempts = %v, want 2", held["attempts"])
	}

	rerouted := detail("aa000002")
	if rerouted["state"] != "timed_out" || rerouted["action"] != "reroute" {
		t.Fatalf("rerouted: state=%v action=%v, want timed_out/reroute", rerouted["state"], rerouted["action"])
	}
}

// /api/config shows judge_strong_backend (empty by default) and
// judge_verdict_cache_enabled (unset reads true), accepts a name and a
// boolean, and refuses the wrong types with a 400 that names the key.
func TestReviewStateJudgeStrongRoutingConfigSurface(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig(), cfgPath: t.TempDir() + "/config.yaml"}

	if sb, en := reviewStateJudgeKeys(t, s); sb != "" || en != true {
		t.Fatalf("defaults: strong=%q cache=%v, want \"\"/true", sb, en)
	}

	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.judge_strong_backend":        "claude-code-strong",
		"autonomous.judge_verdict_cache_enabled": false,
	})
	if sb, en := reviewStateJudgeKeys(t, s); sb != "claude-code-strong" || en != false {
		t.Fatalf("after PUT: strong=%q cache=%v", sb, en)
	}
	if s.cfg.Autonomous.JudgeStrongBackend != "claude-code-strong" ||
		s.cfg.Autonomous.JudgeVerdictCacheEnabled == nil || *s.cfg.Autonomous.JudgeVerdictCacheEnabled != false {
		t.Fatalf("stored: %q / %v", s.cfg.Autonomous.JudgeStrongBackend, s.cfg.Autonomous.JudgeVerdictCacheEnabled)
	}

	// Clearing works too: the empty string and an explicit true round-trip.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.judge_strong_backend":        "   ",
		"autonomous.judge_verdict_cache_enabled": true,
	})
	if sb, en := reviewStateJudgeKeys(t, s); sb != "" || en != true {
		t.Fatalf("after clear: strong=%q cache=%v", sb, en)
	}
	if s.cfg.Autonomous.JudgeVerdictCacheEnabled == nil || *s.cfg.Autonomous.JudgeVerdictCacheEnabled != true {
		t.Fatalf("stored after clear: %v", s.cfg.Autonomous.JudgeVerdictCacheEnabled)
	}

	bad := map[string]any{
		"autonomous.judge_strong_backend":        true,
		"autonomous.judge_verdict_cache_enabled": "true",
	}
	for badKey, badVal := range bad {
		s2 := &Server{cfg: config.DefaultConfig(), cfgPath: t.TempDir() + "/config.yaml"}
		b, _ := json.Marshal(map[string]any{badKey: badVal})
		rr := httptest.NewRecorder()
		s2.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %s=%v: want 400, got %d body=%s", badKey, badVal, rr.Code, rr.Body.String())
			continue
		}
		if !strings.Contains(rr.Body.String(), badKey) {
			t.Errorf("PUT %s: error %q does not name the key", badKey, rr.Body.String())
		}
	}
}
