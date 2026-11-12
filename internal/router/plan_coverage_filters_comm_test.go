// 2026-11-01 — comm-channel parity for the plan-coverage read surface:
// `autonomous plan-coverage <id>` (aliases `plan_coverage` / `coverage`)
// must GET /api/autonomous/prds/{id}/plan_coverage and reply with a
// compact text summary — mode and minimum ratio, covered / missing /
// weakened, the findings count, the suppressed created-file and
// requirement counts, and the acknowledgement state — instead of the
// raw JSON envelope.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDaemonPlanCoverage stands in for the local daemon loopback and
// answers with the body the test supplies (plus every request is
// recorded via the shared capturedDaemonRequest list).
func fakeDaemonPlanCoverage(t *testing.T, reqs *[]capturedDaemonRequest, body string) (int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*reqs = append(*reqs, capturedDaemonRequest{method: r.Method, path: r.URL.Path, body: string(b)})
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(body, "STATUS:") {
			code, rest, _ := strings.Cut(body, ":")
			var n int
			for _, c := range code {
				if c < '0' || c > '9' {
					break
				}
				n = n*10 + int(c-'0')
			}
			if n == 0 {
				n = 500
			}
			w.WriteHeader(n)
			_, _ = w.Write([]byte(rest))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

// pcCoverageEnvelope is the stored-record reply the read route sends
// when a plan coverage record exists on the Automaton.
const pcWithRecord = `{
	"id": "pc-ver-1",
	"status": "needs_review",
	"plan_coverage": {
		"mode": "block",
		"min_ratio": 0.9,
		"report": {"covered": 14, "missing": 4, "weakened": 2, "suppressed": 3, "unmatched": []},
		"ack": {"by": "operator", "fingerprint": "pc-fp-1"},
		"findings": [{"kind": "unresolved_file", "detail": "plan creates docs/x.md"}],
		"suppressed": {"created_files": 2, "requirements": 3}
	},
	"filters": {"suppress_created_files": true, "scoped_requirements": true, "requirement_sections": ["scope", "constraints"], "min_requirement_chars": 30},
	"suppressed": {"created_files": 2, "requirements": 3, "total": 5}
}`

// pcCoverageNoRecord is the reply when the Automaton has never had a
// coverage report stored.
const pcNoRecord = `{
	"id": "pc-ver-bare",
	"status": "draft",
	"plan_coverage": null,
	"filters": {"suppress_created_files": true, "scoped_requirements": true, "requirement_sections": ["scope", "constraints"], "min_requirement_chars": 30},
	"suppressed": {"created_files": 0, "requirements": 0, "total": 0}
}`

func TestPlanCoverageFilters_CommHitsReadPath(t *testing.T) {
	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanCoverage(t, &reqs, pcWithRecord)
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	r.HandleTestMessage("autonomous plan-coverage pc-ver-1")

	if len(reqs) != 1 {
		t.Fatalf("daemon requests = %d want 1 (%v)", len(reqs), reqs)
	}
	if reqs[0].method != http.MethodGet {
		t.Errorf("method = %q want GET", reqs[0].method)
	}
	if reqs[0].path != "/api/autonomous/prds/pc-ver-1/plan_coverage" {
		t.Errorf("path = %q want /api/autonomous/prds/pc-ver-1/plan_coverage", reqs[0].path)
	}
}

func TestPlanCoverageFilters_CommAliasReachesPath(t *testing.T) {
	for _, verb := range []string{"plan_coverage", "coverage"} {
		var reqs []capturedDaemonRequest
		port, closeFn := fakeDaemonPlanCoverage(t, &reqs, pcWithRecord)

		r := &Router{
			hostname: "h",
			backend:  &captureBackend{name: "test", capture: func(string) {}},
		}
		r.SetWebPort(port)

		r.HandleTestMessage("autonomous " + verb + " pc-ver-1")
		closeFn()

		if len(reqs) != 1 || reqs[0].path != "/api/autonomous/prds/pc-ver-1/plan_coverage" {
			t.Errorf("verb %q: requests = %v, want one GET on the plan_coverage path", verb, reqs)
		}
	}
}

func TestPlanCoverageFilters_CommSummaryHoldsCountsAndAck(t *testing.T) {
	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanCoverage(t, &reqs, pcWithRecord)
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	out := strings.Join(r.HandleTestMessage("autonomous plan-coverage pc-ver-1"), "\n")
	for _, want := range []string{
		"mode block",
		"0.9",
		"covered 14",
		"missing 4",
		"weakened 2",
		"findings 1",
		"created files 2",
		"requirements 3",
		"acknowledged by operator",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reply missing %q: %q", want, out)
		}
	}
}

func TestPlanCoverageFilters_CommNoAckLine(t *testing.T) {
	body := strings.Replace(pcWithRecord, `"ack": {"by": "operator", "fingerprint": "pc-fp-1"},`, "", 1)
	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanCoverage(t, &reqs, body)
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	out := strings.Join(r.HandleTestMessage("autonomous plan-coverage pc-ver-1"), "\n")
	if !strings.Contains(out, "no acknowledgement yet") {
		t.Errorf("reply does not say the report is unacknowledged: %q", out)
	}
	if strings.Contains(out, "acknowledged by") {
		t.Errorf("reply must not claim an acknowledgement: %q", out)
	}
}

func TestPlanCoverageFilters_CommUsageWithoutID(t *testing.T) {
	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	out := strings.Join(r.HandleTestMessage("autonomous plan-coverage"), "\n")
	if !strings.Contains(out, "usage: autonomous plan-coverage <automaton-id>") {
		t.Errorf("expected usage hint, got: %q", out)
	}
}

func TestPlanCoverageFilters_CommNullRecordLine(t *testing.T) {
	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanCoverage(t, &reqs, pcNoRecord)
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	out := strings.Join(r.HandleTestMessage("autonomous plan-coverage pc-ver-bare"), "\n")
	if !strings.Contains(out, "no plan coverage record is stored yet") {
		t.Errorf("expected the no-record line, got: %q", out)
	}
	if strings.Contains(out, "mode ") {
		t.Errorf("no-record reply must not carry summary fields: %q", out)
	}
}

func TestPlanCoverageFilters_CommRelaysDaemonError(t *testing.T) {
	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanCoverage(t, &reqs, "STATUS:500:plan coverage read: exploded")
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	out := strings.Join(r.HandleTestMessage("autonomous plan-coverage pc-ver-1"), "\n")
	if !strings.Contains(out, "autonomous plan-coverage failed") {
		t.Errorf("expected the failure title, got: %q", out)
	}
	if !strings.Contains(out, "HTTP 500") || !strings.Contains(out, "exploded") {
		t.Errorf("daemon error not relayed: %q", out)
	}
}

func TestPlanCoverageFilters_CommHelpListsVerb(t *testing.T) {
	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	out := strings.Join(r.HandleTestMessage("autonomous"), "\n")
	if !strings.Contains(out, "plan-coverage <id>") {
		t.Errorf("help string missing the plan-coverage verb: %q", out)
	}
}

// Sanity that the summary path tolerates the envelope's top-level
// suppressed block being the only suppressed source (record present,
// but the JSON field order differs from the constant above).
func TestPlanCoverageFilters_CommToleratesRecordWithoutFindingsKey(t *testing.T) {
	var v map[string]any
	if err := json.Unmarshal([]byte(pcWithRecord), &v); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	delete(v["plan_coverage"].(map[string]any), "findings")
	fix, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("remarshal fixture: %v", err)
	}

	var reqs []capturedDaemonRequest
	port, closeFn := fakeDaemonPlanCoverage(t, &reqs, string(fix))
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	out := strings.Join(r.HandleTestMessage("autonomous plan-coverage pc-ver-1"), "\n")
	if !strings.Contains(out, "findings 0") {
		t.Errorf("missing findings count without the key: %q", out)
	}
}
