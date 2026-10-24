package autonomous

// Live, opt-in smoke for the failure escalation REST surface of a SANDBOX
// daemon: the global autonomous.failure_escalation block on /api/config, the
// Automaton and task overrides on set_failure_escalation, the 409 that
// resolve_task_review answers for a task that is not held, the held-task fields
// of the status and summary views, and the 400 for an invalid policy. It drives
// the daemon over its REST API, in the same way as TestAutomationControlLiveSmoke
// in automation_control_live_smoke_test.go, and shares that file's client helpers.
//
// It is skipped unless DW_LIVE_FAILURE_ESCALATION_SMOKE is set, so `go test ./...`
// in CI skips it. The variable has no default: its value is the base URL of the
// sandbox daemon the operator stood up, so the test can never reach production by
// accident. Environment:
//
//	DW_LIVE_FAILURE_ESCALATION_SMOKE=https://host:port  base URL of the sandbox daemon
//	DW_BASE_TOKEN=...                                    bearer token, if the daemon has one
//	DW_BASE_INSECURE=0                                   verify TLS (default: skip verify)
//	DW_LIVE_FAILURE_ESCALATION_PROJECT_DIR=...           project_dir for the smoke Automaton
//	                                                     (needed unless the sandbox has worktree mode)
//
// Run it against a sandbox daemon the operator started:
//
//	DW_LIVE_FAILURE_ESCALATION_SMOKE=https://127.0.0.1:18443 \
//	  go test -count=1 -timeout 10m -run TestFailureEscalationLiveSmoke -v ./internal/autonomous/
//
// The test only calls the REST API of that daemon. It never touches the terminal
// multiplexer or any process on the host.
//
// What it changes, and how it puts that back:
//   - The global block. max_failures and the steps ladder are written, once as a
//     JSON array and once as compact text, and each write is read back and
//     compared. Cleanup writes the original max_failures and steps again and reads
//     them back. The enabled flag is never written.
//   - One Automaton, created as a draft and then cancelled once. A draft refuses
//     the structural edits (add_story, add_task) and the override guard, and a
//     cancelled Automaton accepts both. Nothing in it ever runs: it has no task
//     session, so the cancel ends none. Cleanup leaves it on the sandbox and logs
//     its ID, as the other live smokes leave their draft PRDs.
//
// The held-task path is not forced here. A task reaches TaskReviewNeeded only
// after its attempts fail on a worker, and a sandbox smoke must not run workers.
// The retry and cancel decisions on a held task, and the held-task rows of the
// status and summary views, are covered by the fake-based tests in
// failure_escalation_review_test.go, failure_escalation_rest_test.go and
// failure_escalation_views_test.go. This smoke checks the not-held side of those
// routes and that the held-task fields stay absent while nothing is held.

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

const failureEscalationSmokeEnv = "DW_LIVE_FAILURE_ESCALATION_SMOKE"

// escalationSmokeLadderText is the steps ladder in compact text form.
const escalationSmokeLadderText = "claude-code:claude-opus-5-5@2,pi-prompt:ollama/qwen3.8:27b@1"

// escalationSmokeLadderJSON is the same ladder as the JSON array form of steps.
var escalationSmokeLadderJSON = []map[string]any{
	{"backend": "claude-code", "model": "claude-opus-5-5", "attempts": 2},
	{"backend": "pi-prompt", "model": "ollama/qwen3.8:27b", "attempts": 1},
}

// escalationSmokeLadder is the stored ladder both forms must produce. Effort is
// empty in both, since the compact text has no effort form.
var escalationSmokeLadder = []FailureEscalationStep{
	{Backend: "claude-code", Model: "claude-opus-5-5", Attempts: 2},
	{Backend: "pi-prompt", Model: "ollama/qwen3.8:27b", Attempts: 1},
}

// escalationSmokeBlock is the autonomous.failure_escalation block GET /api/config returns.
type escalationSmokeBlock struct {
	Enabled     bool                    `json:"enabled"`
	MaxFailures int                     `json:"max_failures"`
	Steps       []FailureEscalationStep `json:"steps"`
}

// escalationSmokeAutomaton is the subset of an Automaton record this smoke reads.
type escalationSmokeAutomaton struct {
	ID                string                   `json:"id"`
	Status            string                   `json:"status"`
	FailureEscalation *FailureEscalationPolicy `json:"failure_escalation"`
	Stories           []escalationSmokeStory   `json:"stories"`
}

type escalationSmokeStory struct {
	ID    string                `json:"id"`
	Tasks []escalationSmokeTask `json:"tasks"`
}

type escalationSmokeTask struct {
	ID                string                   `json:"id"`
	Status            string                   `json:"status"`
	FailureEscalation *FailureEscalationPolicy `json:"failure_escalation"`
}

// escalationSmokeConfig reads the autonomous.failure_escalation block.
func escalationSmokeConfig(t *testing.T, c *smokeClient) escalationSmokeBlock {
	t.Helper()
	var cfg struct {
		Autonomous struct {
			FailureEscalation escalationSmokeBlock `json:"failure_escalation"`
		} `json:"autonomous"`
	}
	out := c.must(t, http.MethodGet, "/api/config", nil, http.StatusOK)
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("decode config: %v body=%s", err, out)
	}
	return cfg.Autonomous.FailureEscalation
}

// escalationSmokePut sends a PUT /api/config patch and returns the status and body.
func escalationSmokePut(t *testing.T, c *smokeClient, patch map[string]any) (int, []byte) {
	t.Helper()
	return c.do(t, http.MethodPut, "/api/config", patch)
}

// escalationSmokePost sends one per-Automaton POST and returns the status and body.
func escalationSmokePost(t *testing.T, c *smokeClient, id, op string, body any) (int, []byte) {
	t.Helper()
	return c.do(t, http.MethodPost, "/api/autonomous/prds/"+id+"/"+op, body)
}

// escalationSmokeMustPost is escalationSmokePost for a setup step: anything but
// 200 stops the test.
func escalationSmokeMustPost(t *testing.T, c *smokeClient, id, op string, body any) {
	t.Helper()
	status, out := escalationSmokePost(t, c, id, op, body)
	if status != http.StatusOK {
		t.Fatalf("%s %s: want 200, got %d body=%s", id, op, status, out)
	}
}

// escalationSmokeCreate creates one draft Automaton with a placeholder spec and
// returns its ID.
func escalationSmokeCreate(t *testing.T, c *smokeClient) string {
	t.Helper()
	body := map[string]any{"spec": "failure escalation live smoke placeholder: never run"}
	if dir := os.Getenv("DW_LIVE_FAILURE_ESCALATION_PROJECT_DIR"); dir != "" {
		body["project_dir"] = dir
	}
	status, out := c.do(t, http.MethodPost, "/api/autonomous/prds", body)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create automaton: status %d body=%s (set DW_LIVE_FAILURE_ESCALATION_PROJECT_DIR unless the sandbox has worktree mode)", status, out)
	}
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &p); err != nil || p.ID == "" {
		t.Fatalf("decode created automaton: %v body=%s", err, out)
	}
	return p.ID
}

// escalationSmokeGet reads one Automaton back through its detail route.
func escalationSmokeGet(t *testing.T, c *smokeClient, id string) escalationSmokeAutomaton {
	t.Helper()
	var a escalationSmokeAutomaton
	out := c.must(t, http.MethodGet, "/api/autonomous/prds/"+id, nil, http.StatusOK)
	if err := json.Unmarshal(out, &a); err != nil {
		t.Fatalf("decode automaton %s: %v body=%s", id, err, out)
	}
	return a
}

// escalationSmokeTaskOf returns task taskID of a, stopping the test when a has none.
func escalationSmokeTaskOf(t *testing.T, a escalationSmokeAutomaton, taskID string) escalationSmokeTask {
	t.Helper()
	for _, s := range a.Stories {
		for _, task := range s.Tasks {
			if task.ID == taskID {
				return task
			}
		}
	}
	t.Fatalf("task %s not in automaton %s", taskID, a.ID)
	return escalationSmokeTask{}
}

func TestFailureEscalationLiveSmoke(t *testing.T) {
	base := strings.TrimRight(os.Getenv(failureEscalationSmokeEnv), "/")
	if base == "" {
		t.Skip("set DW_LIVE_FAILURE_ESCALATION_SMOKE=https://host:port of a sandbox daemon to run the failure escalation live smoke")
	}
	c := newSmokeClient(base, os.Getenv("DW_BASE_TOKEN"))

	// (1) The global block. The original values go back in cleanup, whether the
	// test passes or fails, and the restore is read back.
	orig := escalationSmokeConfig(t, c)
	t.Logf("autonomous.failure_escalation before: %+v", orig)
	t.Cleanup(func() {
		status, out := escalationSmokePut(t, c, map[string]any{
			"autonomous.failure_escalation.max_failures": orig.MaxFailures,
			"autonomous.failure_escalation.steps":        orig.Steps,
		})
		if status != http.StatusOK {
			t.Errorf("restore failure escalation: status %d body=%s", status, out)
			return
		}
		if got := escalationSmokeConfig(t, c); got.MaxFailures != orig.MaxFailures || !reflect.DeepEqual(got.Steps, orig.Steps) {
			t.Errorf("restore failure escalation: GET max_failures=%d steps=%+v, want %d / %+v",
				got.MaxFailures, got.Steps, orig.MaxFailures, orig.Steps)
		}
	})

	status, out := escalationSmokePut(t, c, map[string]any{
		"autonomous.failure_escalation.max_failures": 4,
		"autonomous.failure_escalation.steps":        escalationSmokeLadderJSON,
	})
	if status != http.StatusOK {
		t.Fatalf("PUT steps as a JSON array: status %d body=%s", status, out)
	}
	if got := escalationSmokeConfig(t, c); got.MaxFailures != 4 || !reflect.DeepEqual(got.Steps, escalationSmokeLadder) {
		t.Errorf("GET after the JSON array PUT: max_failures=%d steps=%+v, want 4 / %+v", got.MaxFailures, got.Steps, escalationSmokeLadder)
	}

	status, out = escalationSmokePut(t, c, map[string]any{
		"autonomous.failure_escalation.steps": escalationSmokeLadderText,
	})
	if status != http.StatusOK {
		t.Fatalf("PUT steps as compact text: status %d body=%s", status, out)
	}
	if got := escalationSmokeConfig(t, c); got.MaxFailures != 4 || !reflect.DeepEqual(got.Steps, escalationSmokeLadder) {
		t.Errorf("GET after the compact text PUT: max_failures=%d steps=%+v, want 4 / %+v", got.MaxFailures, got.Steps, escalationSmokeLadder)
	}

	// (2) A draft Automaton, one placeholder task, and the overrides on both levels.
	// It is cancelled once because a draft refuses the structural edits and the
	// override guard; a cancelled one accepts both, and nothing in it ever ran.
	id := escalationSmokeCreate(t, c)
	t.Cleanup(func() { t.Logf("draft automaton left on the sandbox for operator review: %s", id) })
	t.Logf("draft automaton %s created", id)

	escalationSmokeMustPost(t, c, id, "cancel", nil)
	escalationSmokeMustPost(t, c, id, "add_story", map[string]any{
		"title": "placeholder story", "description": "never run", "actor": "operator",
	})
	stories := escalationSmokeGet(t, c, id).Stories
	if len(stories) != 1 {
		t.Fatalf("automaton %s has %d stories after add_story, want 1", id, len(stories))
	}
	escalationSmokeMustPost(t, c, id, "add_task", map[string]any{
		"story_id": stories[0].ID, "title": "placeholder task", "spec": "placeholder spec: never run", "actor": "operator",
	})
	a := escalationSmokeGet(t, c, id)
	if len(a.Stories) != 1 || len(a.Stories[0].Tasks) != 1 {
		t.Fatalf("automaton %s after add_task: %+v, want one story with one task", id, a.Stories)
	}
	taskID := a.Stories[0].Tasks[0].ID

	escalationSmokeMustPost(t, c, id, "set_failure_escalation", map[string]any{
		"policy": map[string]any{"enabled": false, "max_failures": 5},
	})
	escalationSmokeMustPost(t, c, id, "set_failure_escalation", map[string]any{
		"task_id": taskID, "policy": map[string]any{"max_failures": 2},
	})
	a = escalationSmokeGet(t, c, id)
	if fe := a.FailureEscalation; fe == nil || fe.MaxFailures != 5 || fe.Enabled == nil || *fe.Enabled {
		t.Errorf("Automaton override after set = %+v, want enabled=false max_failures=5", fe)
	}
	if fe := escalationSmokeTaskOf(t, a, taskID).FailureEscalation; fe == nil || fe.MaxFailures != 2 {
		t.Errorf("task override after set = %+v, want max_failures=2", fe)
	}

	// A null policy clears one level. Clearing the Automaton level must leave the
	// task level alone, and the reverse.
	escalationSmokeMustPost(t, c, id, "set_failure_escalation", map[string]any{"policy": nil})
	a = escalationSmokeGet(t, c, id)
	if a.FailureEscalation != nil {
		t.Errorf("Automaton override after a null policy = %+v, want none", a.FailureEscalation)
	}
	if fe := escalationSmokeTaskOf(t, a, taskID).FailureEscalation; fe == nil || fe.MaxFailures != 2 {
		t.Errorf("clearing the Automaton override changed the task override: %+v", fe)
	}
	escalationSmokeMustPost(t, c, id, "set_failure_escalation", map[string]any{"task_id": taskID, "policy": nil})
	if fe := escalationSmokeTaskOf(t, escalationSmokeGet(t, c, id), taskID).FailureEscalation; fe != nil {
		t.Errorf("task override after a null policy = %+v, want none", fe)
	}

	// (3) The task is not held, so resolve_task_review is a 409 and the task
	// keeps its status.
	status, out = escalationSmokePost(t, c, id, "resolve_task_review", map[string]any{"task_id": taskID, "action": "retry"})
	t.Logf("resolve_task_review on a task not held -> %d %s", status, strings.TrimSpace(string(out)))
	if status != http.StatusConflict {
		t.Errorf("resolve_task_review on a task not held: status %d, want 409 body=%s", status, out)
	}
	if s := escalationSmokeTaskOf(t, escalationSmokeGet(t, c, id), taskID).Status; s == string(TaskReviewNeeded) {
		t.Errorf("task %s is %q after a refused resolve", taskID, s)
	}

	// (4) Nothing is held, so the held-task fields are absent. The status view also
	// lists tasks held on other Automata of the sandbox, so there only the entries
	// that name this Automaton are checked. The summary view has no held-task field
	// of its own, so it must have none at all.
	statusView := map[string]json.RawMessage{}
	if err := json.Unmarshal(c.must(t, http.MethodGet, "/api/autonomous/status", nil, http.StatusOK), &statusView); err != nil {
		t.Fatalf("decode autonomous status: %v", err)
	}
	if raw, ok := statusView["review_needed"]; ok {
		t.Logf("status lists tasks held on other Automata: %s", raw)
		var held []struct {
			AutomatonID string `json:"automaton_id"`
		}
		if err := json.Unmarshal(raw, &held); err != nil {
			t.Fatalf("decode status review_needed: %v body=%s", err, raw)
		}
		for _, h := range held {
			if h.AutomatonID == id {
				t.Errorf("status review_needed names automaton %s, which holds nothing: %s", id, raw)
			}
		}
	}
	summary := map[string]json.RawMessage{}
	if err := json.Unmarshal(c.must(t, http.MethodGet, "/api/autonomous/prds/"+id+"?detail=summary", nil, http.StatusOK), &summary); err != nil {
		t.Fatalf("decode automaton summary: %v", err)
	}
	if raw, ok := summary["review_needed"]; ok {
		t.Errorf("summary review_needed = %s, want absent when nothing is held", raw)
	}

	// (5) An invalid policy is a 400 and writes nothing, on the Automaton and on
	// the global block.
	status, out = escalationSmokePost(t, c, id, "set_failure_escalation", map[string]any{
		"policy": map[string]any{"max_failures": -1},
	})
	t.Logf("set_failure_escalation with max_failures -1 -> %d %s", status, strings.TrimSpace(string(out)))
	if status != http.StatusBadRequest {
		t.Errorf("set_failure_escalation with max_failures -1: status %d, want 400 body=%s", status, out)
	}
	if fe := escalationSmokeGet(t, c, id).FailureEscalation; fe != nil {
		t.Errorf("refused policy changed the Automaton override: %+v", fe)
	}

	before := escalationSmokeConfig(t, c)
	status, out = escalationSmokePut(t, c, map[string]any{"autonomous.failure_escalation.max_failures": -1})
	t.Logf("PUT max_failures -1 -> %d %s", status, strings.TrimSpace(string(out)))
	if status != http.StatusBadRequest {
		t.Errorf("PUT max_failures -1: status %d, want 400 body=%s", status, out)
	}
	if got := escalationSmokeConfig(t, c); !reflect.DeepEqual(got, before) {
		t.Errorf("refused PUT changed the block: %+v, want %+v", got, before)
	}
}
