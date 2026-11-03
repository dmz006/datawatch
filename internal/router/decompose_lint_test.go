package router

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// lintCommConfig is a live decompose-lint config snapshot, the daemon's GET
// /api/autonomous/decompose-lint/config shape.
const lintCommConfig = `{
  "decompose_lint": {
    "mode": "shadow",
    "max_retries": 4,
    "rules": [
      {"id": "bare_verbs", "severity": "warn", "patterns": ["^add "], "message": "task spec starts with a bare verb"}
    ],
    "disabled_rules": []
  }
}`

// lintCommReport is the daemon's dry-run reply (POST
// /api/autonomous/prds/{id}/decompose-lint).
const lintCommReport = `{
  "prd": "p1",
  "mode": "shadow",
  "rule_count": 6,
  "findings": [
    {"rule_id": "bare_verbs", "severity": "warn", "message": "task spec starts with a bare verb", "story": "s1", "task": "t1", "matched": "Add"}
  ],
  "summary": {"block": 0, "warn": 1}
}`

// lintCommRecord is the daemon's stored-record reply (GET
// /api/autonomous/prds/{id}/decompose-lint).
const lintCommRecord = `{
  "prd_id": "p1",
  "decompose_lint": {
    "decompose_lint_findings": [
      {"rule_id": "bare_verbs", "severity": "warn", "message": "task spec starts with a bare verb", "story": "s1", "task": "t1", "matched": "Add"}
    ],
    "mode": "enforce",
    "attempts": 3,
    "linted_at": "2026-11-01T10:00:00Z"
  }
}`

const lintCommNoRecord = `{"prd_id": "p9", "decompose_lint": null}`

// lintCommLog records every request the fake daemon sees; the last is kept
// by name so a test can assert on the final call of a two-call verb.
type lintCommLog struct {
	calls  int
	last   lintCommCall
	bodies []string
}

type lintCommCall struct {
	method, path, body string
}

func newLintCommDaemon(t *testing.T, status int, body string) (*lintCommLog, int) {
	t.Helper()
	log := &lintCommLog{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		log.calls++
		log.last = lintCommCall{method: r.Method, path: r.URL.Path, body: string(raw)}
		log.bodies = append(log.bodies, string(raw))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	return log, port
}

func TestDecomposeLintComm_ConfigRenders(t *testing.T) {
	log, port := newLintCommDaemon(t, http.StatusOK, lintCommConfig)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lint config"), " ")
	if log.calls != 1 || log.last.method != http.MethodGet ||
		log.last.path != "/api/autonomous/decompose-lint/config" {
		t.Fatalf("calls=%d last=%+v, want one GET on the config endpoint", log.calls, log.last)
	}
	for _, want := range []string{"\"mode\": \"shadow\"", "bare_verbs"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestDecomposeLintComm_ConfigSetSendsMergedBlock(t *testing.T) {
	// Like the real daemon's "here is the new config" reply, the fake
	// echoes the PUT body back; GETs get the pre-set live block.
	log := &lintCommLog{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		log.calls++
		log.last = lintCommCall{method: r.Method, path: r.URL.Path, body: string(raw)}
		log.bodies = append(log.bodies, string(raw))
		if r.Method == http.MethodPut {
			_, _ = w.Write(raw)
			return
		}
		_, _ = w.Write([]byte(lintCommConfig))
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lint config-set mode=enforce max_retries=2"), " ")
	if log.calls != 2 {
		t.Fatalf("calls = %d, want the GET live block then the PUT merged block", log.calls)
	}
	put := log.last
	if put.method != http.MethodPut || put.path != "/api/autonomous/decompose-lint/config" {
		t.Fatalf("last call = %+v, want a PUT on the config endpoint", put)
	}
	for _, want := range []string{
		`"mode":"enforce"`,
		`"max_retries":2`,
		"bare_verbs",
		`"decompose_lint"`,
	} {
		if !strings.Contains(put.body, want) {
			t.Errorf("the PUT body lacks %q:\n%s", want, put.body)
		}
	}
	if !strings.Contains(got, "enforce") {
		t.Errorf("the reply lacks the new mode: %q", got)
	}
}

func TestDecomposeLintComm_ConfigSetRejectsBadPairs(t *testing.T) {
	log, port := newLintCommDaemon(t, http.StatusOK, lintCommConfig)
	r := setupPeerRouter(t, port)
	for _, msg := range []string{
		"autonomous lint config-set mode=bogus",
		"autonomous lint config-set max_retries=-1",
		"autonomous lint config-set nonsense=1",
		"autonomous lint config-set",
	} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "failed") {
			t.Errorf("%s: expected a failure reply, got: %q", msg, got)
		}
	}
	if log.calls != 0 {
		t.Errorf("bad pairs must be refused before any request; daemon saw %d calls", log.calls)
	}
}

func TestDecomposeLintComm_DryRunRenders(t *testing.T) {
	log, port := newLintCommDaemon(t, http.StatusOK, lintCommReport)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lint dry-run p1"), " ")
	if log.calls != 1 || log.last.method != http.MethodPost ||
		log.last.path != "/api/autonomous/prds/p1/decompose-lint" {
		t.Fatalf("calls=%d last=%+v, want one POST on the per-PRD endpoint", log.calls, log.last)
	}
	var rep autonomous.DecomposeLintReport
	if err := json.Unmarshal([]byte(lintCommReport), &rep); err != nil {
		t.Fatal(err)
	}
	want := autonomous.RenderDecomposeLintReport(rep)
	for _, line := range strings.Split(strings.TrimRight(want, "\n"), "\n") {
		if !strings.Contains(got, strings.TrimSpace(line)) {
			t.Errorf("reply lacks shared render line %q: %q", line, got)
		}
	}
}

func TestDecomposeLintComm_FindingsRendersStoredRecord(t *testing.T) {
	log, port := newLintCommDaemon(t, http.StatusOK, lintCommRecord)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lint findings p1"), " ")
	if log.calls != 1 || log.last.method != http.MethodGet ||
		log.last.path != "/api/autonomous/prds/p1/decompose-lint" {
		t.Fatalf("calls=%d last=%+v, want one GET on the per-PRD endpoint", log.calls, log.last)
	}
	var stored struct {
		DecomposeLint *autonomous.DecomposeLintRecord `json:"decompose_lint"`
	}
	if err := json.Unmarshal([]byte(lintCommRecord), &stored); err != nil {
		t.Fatal(err)
	}
	want := autonomous.RenderDecomposeLintRecord("p1", stored.DecomposeLint)
	for _, line := range strings.Split(strings.TrimRight(want, "\n"), "\n") {
		if !strings.Contains(got, strings.TrimSpace(line)) {
			t.Errorf("reply lacks shared render line %q: %q", line, got)
		}
	}
}

func TestDecomposeLintComm_FindingsNoRecordWording(t *testing.T) {
	_, port := newLintCommDaemon(t, http.StatusOK, lintCommNoRecord)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lint findings p9"), " ")
	if !strings.Contains(got, "no decompose-lint record stored for p9") {
		t.Errorf("the no-record reply lacks the no-record wording: %q", got)
	}
}

func TestDecomposeLintComm_UsageOnBadShape(t *testing.T) {
	log, port := newLintCommDaemon(t, http.StatusOK, lintCommConfig)
	r := setupPeerRouter(t, port)
	for _, msg := range []string{
		"autonomous lint",
		"autonomous lint whatever",
		"autonomous lint dry-run",    // missing the prd id
		"autonomous lint findings",   // missing the prd id
		"autonomous lint config-set", // missing key=value pairs
	} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "usage: autonomous lint") {
			t.Errorf("%s: expected the lint usage, got: %q", msg, got)
		}
	}
	if log.calls != 0 {
		t.Errorf("bad shapes must be refused before any request; daemon saw %d calls", log.calls)
	}
}

func TestDecomposeLintComm_ParityListHasVerb(t *testing.T) {
	verbs := DecomposeLintCommVerbs()
	if !slices.Contains(verbs, "lint config") {
		t.Errorf("DecomposeLintCommVerbs() = %v, must contain %q", verbs, "lint config")
	}
}
