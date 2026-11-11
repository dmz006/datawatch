package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/pilog"
	"github.com/dmz006/datawatch/internal/session"
)

// A claude-code session's cache tokens reach the record, and a pi session
// with no recorded tokens is counted from its session log.
func TestCostReportSourceCountsCacheAndPiLogTokens(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	workDir := t.TempDir()
	logDir := filepath.Join(root, "sessions", pilog.ProjectDirName(workDir))
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Hour).UTC()
	logName := started.Format("2006-01-02T15-04-05-000Z") + "_picost.jsonl"
	line := `{"type":"message","message":{"role":"assistant","usage":{"input":300,"output":200,"cacheRead":4000,"cacheWrite":100}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logDir, logName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	sm := costReportSessionManager(t)
	now := time.Now()
	costReportSession(t, sm, "cc000001", "claude-code", 10, 20, now)
	cc, _ := sm.GetSession("cc000001")
	cc.CacheReadTokens, cc.CacheWriteTokens = 900, 90
	if err := sm.SaveSession(cc); err != nil {
		t.Fatal(err)
	}
	if err := sm.SaveSession(&session.Session{
		ID: "pi000001", FullID: "test-pi000001", Name: "picost", State: session.StateComplete,
		BackendFamily: "pi-prompt", ProjectDir: workDir, CreatedAt: started, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	// A pi session with no log and no tokens stays out of the report.
	if err := sm.SaveSession(&session.Session{
		ID: "pi000002", FullID: "test-pi000002", Name: "nolog", State: session.StateComplete,
		BackendFamily: "pi", ProjectDir: t.TempDir(), CreatedAt: started, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := managerCostSource{m: sm}.Records(costreport.SourceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]costreport.Record{}
	for _, r := range res.Records {
		got[r.SessionID] = r
	}
	if len(got) != 2 {
		t.Fatalf("want 2 records, got %+v", got)
	}
	if r := got["cc000001"]; r.CacheReadTokens != 900 || r.CacheWriteTokens != 90 || r.TokensIn != 10 {
		t.Errorf("claude record = %+v", r)
	}
	if r := got["pi000001"]; r.TokensIn != 300 || r.TokensOut != 200 || r.CacheReadTokens != 4000 || r.CacheWriteTokens != 100 {
		t.Errorf("pi record = %+v", r)
	}
}

// The REST body carries totals with cache and local tokens, and omits the
// reconciliation when no usage readings are wired.
func TestCostReport_TotalsAndNoReconciliationWithoutReadings(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/cost/report?group_by=backend", nil)
	rr := httptest.NewRecorder()
	s.handleCostReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var tot map[string]int
	if err := json.Unmarshal(body["totals"], &tot); err != nil {
		t.Fatalf("totals: %v", err)
	}
	for _, k := range []string{"sessions", "tokens_in", "tokens_out", "cache_read_tokens", "cache_write_tokens", "local_tokens"} {
		if _, ok := tot[k]; !ok {
			t.Errorf("totals lacks %q", k)
		}
	}
	if _, ok := body["reconciliation"]; ok {
		t.Error("reconciliation must be absent without readings")
	}
}

func TestCostReportUsageInput_NilWithoutCalculator(t *testing.T) {
	s := bl90Server(t)
	if got := s.costReportUsageInput(t.Context(), time.Now()); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}
