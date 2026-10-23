// BL428 Phase 4 — /api/usage-backoff REST tests.

package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func usageBackoffServer(t *testing.T) *Server {
	t.Helper()
	return &Server{cfg: &config.Config{}, usageBackoff: autonomous.NewBackoff()}
}

func ubDo(s *Server, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/usage-backoff", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleUsageBackoff(rr, req)
	return rr
}

func ubStatus(t *testing.T, rr *httptest.ResponseRecorder) autonomous.UsageBackoffStatus {
	t.Helper()
	var st autonomous.UsageBackoffStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return st
}

func TestUsageBackoff_GetEmptyShowsDefaults(t *testing.T) {
	s := usageBackoffServer(t)
	rr := ubDo(s, http.MethodGet, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"active":[]`) {
		t.Errorf("active must marshal as an empty array: %s", rr.Body.String())
	}
	st := ubStatus(t, rr)
	if st.Config.Mode != "pause_backend" || st.Config.MaxWaitMinutes != 60 {
		t.Errorf("config = %+v, want pause_backend/60", st.Config)
	}
}

func TestUsageBackoff_GetReflectsConfig(t *testing.T) {
	s := usageBackoffServer(t)
	s.cfg.Autonomous.UsageBackoff = "off"
	s.cfg.Autonomous.BackoffMaxWaitMinutes = 15
	st := ubStatus(t, ubDo(s, http.MethodGet, ""))
	if st.Config.Mode != "off" || st.Config.MaxWaitMinutes != 15 {
		t.Errorf("config = %+v, want off/15", st.Config)
	}
}

func TestUsageBackoff_PostMinutesThenClearOne(t *testing.T) {
	s := usageBackoffServer(t)
	before := time.Now()
	rr := ubDo(s, http.MethodPost, `{"backend":"claude-code","minutes":45,"reason":"weekly cap"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("post: %d %s", rr.Code, rr.Body.String())
	}
	st := ubStatus(t, rr)
	if len(st.Active) != 1 {
		t.Fatalf("active = %+v", st.Active)
	}
	e := st.Active[0]
	if e.Backend != "claude-code" || e.Source != autonomous.BackoffSourceOperator || e.Reason != "weekly cap" {
		t.Errorf("entry = %+v", e)
	}
	if d := e.Until.Sub(before); d < 44*time.Minute || d > 46*time.Minute {
		t.Errorf("until is %v from now, want ~45m", d)
	}
	if blocked, _ := s.usageBackoff.Blocked("claude-code"); !blocked {
		t.Error("registry should report claude-code blocked")
	}

	rr = ubDo(s, http.MethodDelete, `{"backend":"claude-code"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rr.Code, rr.Body.String())
	}
	var del struct {
		Cleared int `json:"cleared"`
		autonomous.UsageBackoffStatus
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &del); err != nil {
		t.Fatal(err)
	}
	if del.Cleared != 1 || len(del.Active) != 0 {
		t.Errorf("delete result = %+v", del)
	}
}

func TestUsageBackoff_PostUntil(t *testing.T) {
	s := usageBackoffServer(t)
	until := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	rr := ubDo(s, http.MethodPost, `{"backend":"pi","until":"`+until.Format(time.RFC3339)+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("post: %d %s", rr.Code, rr.Body.String())
	}
	st := ubStatus(t, rr)
	if len(st.Active) != 1 || !st.Active[0].Until.Equal(until) {
		t.Errorf("active = %+v, want until %v", st.Active, until)
	}
}

func TestUsageBackoff_PostDefaultsTo30Minutes(t *testing.T) {
	s := usageBackoffServer(t)
	before := time.Now()
	rr := ubDo(s, http.MethodPost, `{"backend":"pi"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("post: %d %s", rr.Code, rr.Body.String())
	}
	st := ubStatus(t, rr)
	if d := st.Active[0].Until.Sub(before); d < 29*time.Minute || d > 31*time.Minute {
		t.Errorf("default until is %v from now, want ~30m", d)
	}
}

func TestUsageBackoff_ManualSetWorksWhenModeOff(t *testing.T) {
	s := usageBackoffServer(t)
	s.cfg.Autonomous.UsageBackoff = "off"
	rr := ubDo(s, http.MethodPost, `{"backend":"pi","minutes":5}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("manual set with mode=off must succeed: %d %s", rr.Code, rr.Body.String())
	}
	if blocked, _ := s.usageBackoff.Blocked("pi"); !blocked {
		t.Error("manual flag should be active even with usage_backoff=off")
	}
}

func TestUsageBackoff_DeleteAll(t *testing.T) {
	for _, body := range []string{"", `{}`, `{"backend":""}`, `{"backend":"all"}`} {
		s := usageBackoffServer(t)
		_ = ubDo(s, http.MethodPost, `{"backend":"a","minutes":5}`)
		_ = ubDo(s, http.MethodPost, `{"backend":"b","minutes":5}`)
		rr := ubDo(s, http.MethodDelete, body)
		if rr.Code != http.StatusOK {
			t.Fatalf("delete %q: %d %s", body, rr.Code, rr.Body.String())
		}
		if got := len(s.usageBackoff.Active()); got != 0 {
			t.Errorf("delete %q left %d flags", body, got)
		}
	}
}

func TestUsageBackoff_BadRequests(t *testing.T) {
	cases := []struct {
		name, method, body, want string
	}{
		{"missing backend", http.MethodPost, `{"minutes":5}`, "backend: required"},
		{"blank backend", http.MethodPost, `{"backend":"  ","minutes":5}`, "backend: required"},
		{"minutes zero", http.MethodPost, `{"backend":"a","minutes":0}`, "minutes: must be >= 1"},
		{"minutes negative", http.MethodPost, `{"backend":"a","minutes":-5}`, "minutes: must be >= 1"},
		{"minutes too large", http.MethodPost, `{"backend":"a","minutes":99999999}`, "minutes: must be <="},
		{"both minutes and until", http.MethodPost, `{"backend":"a","minutes":5,"until":"2099-01-01T00:00:00Z"}`, "specify exactly one"},
		{"until not rfc3339", http.MethodPost, `{"backend":"a","until":"tomorrow"}`, "until: must be an RFC3339"},
		{"until in the past", http.MethodPost, `{"backend":"a","until":"2000-01-01T00:00:00Z"}`, "until: must be in the future"},
		{"malformed post", http.MethodPost, `{`, "bad request"},
		{"empty post body", http.MethodPost, ``, "bad request"},
		{"malformed delete", http.MethodDelete, `{`, "bad request"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := usageBackoffServer(t)
			rr := ubDo(s, c.method, c.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d %s, want 400", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), c.want) {
				t.Errorf("body %q, want it to contain %q", rr.Body.String(), c.want)
			}
			if got := len(s.usageBackoff.Active()); got != 0 {
				t.Errorf("a rejected request must not set a flag, have %d", got)
			}
		})
	}
}

func TestUsageBackoff_MethodNotAllowedAndUnwired(t *testing.T) {
	s := usageBackoffServer(t)
	if rr := ubDo(s, http.MethodPut, `{}`); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT = %d, want 405", rr.Code)
	}
	unwired := &Server{cfg: &config.Config{}}
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		if rr := ubDo(unwired, m, `{"backend":"a"}`); rr.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with no registry = %d, want 503", m, rr.Code)
		}
	}
}

func TestUsageBackoff_CapabilityGate(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	s.cfg = &config.Config{}
	s.usageBackoff = autonomous.NewBackoff()
	// admin / full-control / read-only
	capTest(t, s, http.MethodGet, "/api/usage-backoff", nil, s.handleUsageBackoff,
		http.StatusOK, http.StatusOK, http.StatusOK)
	capTest(t, s, http.MethodPost, "/api/usage-backoff", []byte(`{"backend":"a","minutes":5}`), s.handleUsageBackoff,
		http.StatusOK, http.StatusOK, http.StatusForbidden)
	capTest(t, s, http.MethodDelete, "/api/usage-backoff", []byte(`{}`), s.handleUsageBackoff,
		http.StatusOK, http.StatusOK, http.StatusForbidden)
}

func readAuditLines(t *testing.T, dir string) []audit.Entry {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []audit.Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e audit.Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("bad audit line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}

func TestUsageBackoff_AuditEvents(t *testing.T) {
	dir := t.TempDir()
	log, err := audit.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := usageBackoffServer(t)
	s.auditLog = log

	_ = ubDo(s, http.MethodPost, `{"backend":"claude-code","minutes":10}`)
	_ = ubDo(s, http.MethodDelete, `{"backend":"claude-code"}`)
	_ = ubDo(s, http.MethodDelete, ``)
	_ = log.Close()

	got := readAuditLines(t, dir)
	if len(got) != 3 {
		t.Fatalf("want 3 audit entries, got %d: %+v", len(got), got)
	}
	if got[0].Action != audit.ActionUsageBackoffSet || got[0].Actor != "operator" || got[0].Subject != got[0].Details["backend"] {
		t.Errorf("entry 0 = %+v", got[0])
	}
	if got[0].Details["backend"] != "claude-code" || got[0].Details["source"] != "operator" {
		t.Errorf("set details = %+v", got[0].Details)
	}
	if _, err := time.Parse(time.RFC3339, got[0].Details["until"].(string)); err != nil {
		t.Errorf("set until not RFC3339: %v", got[0].Details["until"])
	}
	if got[1].Action != audit.ActionUsageBackoffCleared || got[1].Details["backend"] != "claude-code" || got[1].Subject != "claude-code" {
		t.Errorf("entry 1 = %+v", got[1])
	}
	if got[2].Action != audit.ActionUsageBackoffCleared || got[2].Details["backend"] != "all" || got[2].Subject != "all" {
		t.Errorf("entry 2 = %+v", got[2])
	}
}

func TestUsageBackoff_RejectedRequestsNotAudited(t *testing.T) {
	dir := t.TempDir()
	log, err := audit.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := usageBackoffServer(t)
	s.auditLog = log
	_ = ubDo(s, http.MethodPost, `{"backend":"a","minutes":0}`)
	_ = log.Close()
	if info, err := os.Stat(filepath.Join(dir, "audit.log")); err == nil && info.Size() > 0 {
		t.Errorf("rejected set must not write an audit entry, log has %d bytes", info.Size())
	}
}

func TestCapacity_ReportsBackoff(t *testing.T) {
	s := usageBackoffServer(t)
	s.usageBackoff.Set("pi", time.Now().Add(time.Hour), "rate-limit — reset", autonomous.BackoffSourceRateLimit)
	s.usageBackoff.Set("old", time.Now().Add(-time.Minute), "expired", autonomous.BackoffSourceRateLimit)

	req := httptest.NewRequest(http.MethodGet, "/api/capacity", nil)
	rr := httptest.NewRecorder()
	s.handleCapacity(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Backoff []struct {
			Backend string    `json:"backend"`
			Until   time.Time `json:"until"`
			Reason  string    `json:"reason"`
		} `json:"backoff"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Backoff) != 1 || resp.Backoff[0].Backend != "pi" || resp.Backoff[0].Reason == "" || resp.Backoff[0].Until.IsZero() {
		t.Errorf("backoff = %+v, want only the live pi flag", resp.Backoff)
	}

	// No flags: still an array, never null.
	empty := &Server{cfg: &config.Config{}}
	rr = httptest.NewRecorder()
	empty.handleCapacity(rr, httptest.NewRequest(http.MethodGet, "/api/capacity", bytes.NewReader(nil)))
	if !strings.Contains(rr.Body.String(), `"backoff":[]`) {
		t.Errorf("empty capacity must carry backoff:[] — %s", rr.Body.String())
	}
}

func patternsServer(t *testing.T) *Server {
	t.Helper()
	s, _ := patternsServerWithCLI(t, "9.9.9-test")
	return s
}

// fakeCLIRunner stands in for `claude --version` so tests never depend on a
// real claude binary (or PATH) being present.
type fakeCLIRunner struct {
	out string
	err error
}

func (r *fakeCLIRunner) run(context.Context, string, ...string) ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	return []byte(r.out), nil
}

// patternsServerWithCLI builds a /api/usage-backoff server whose session
// manager answers the claude version command with out (or err, simulating
// an absent tool). The fake is returned so tests can mutate out mid-test
// and observe the staleness guard.
func patternsServerWithCLI(t *testing.T, out string) (*Server, *fakeCLIRunner) {
	t.Helper()
	sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// In-memory tmux fake: these tests never need a live multiplexer.
	sm.WithFakeTmux()
	t.Cleanup(sm.WaitBackground)
	fr := &fakeCLIRunner{out: out}
	sm.SetClaudeCLIVersionRunner(fr.run)
	s := usageBackoffServer(t)
	s.manager = sm
	return s, fr
}

func ubRecheck(s *Server, method string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/usage-backoff/recheck", strings.NewReader(""))
	rr := httptest.NewRecorder()
	s.handleUsageBackoffRecheck(rr, req)
	return rr
}

func TestUsageBackoff_StatusCarriesPatternsFields(t *testing.T) {
	s := patternsServer(t)
	rr := ubDo(s, http.MethodGet, "")
	for _, key := range []string{`"patterns_version":"`, `"patterns_stale":true`, `"claude_cli_version":"9.9.9-test"`} {
		if !strings.Contains(rr.Body.String(), key) {
			t.Errorf("GET /api/usage-backoff lacks %s: %s", key, rr.Body.String())
		}
	}
	st := ubStatus(t, rr)
	if want := s.manager.RateLimitPatternsVersion(); st.PatternsVersion != want {
		t.Errorf("patterns_version = %q, want %q", st.PatternsVersion, want)
	}

	// Unwired manager: keys still present.
	bare := usageBackoffServer(t)
	body := ubDo(bare, http.MethodGet, "").Body.String()
	for _, key := range []string{`"patterns_version":""`, `"patterns_stale":false`, `"claude_cli_version":""`} {
		if !strings.Contains(body, key) {
			t.Errorf("no-manager status lacks %s: %s", key, body)
		}
	}
}

func TestUsageBackoff_RecheckClearsStaleAndFlipsBackOnChange(t *testing.T) {
	s := patternsServer(t)
	rr := ubRecheck(s, http.MethodPost)
	if rr.Code != http.StatusOK {
		t.Fatalf("recheck: %d %s", rr.Code, rr.Body.String())
	}
	st := ubStatus(t, rr)
	if st.PatternsStale || st.PatternsVersion == "" {
		t.Errorf("recheck response = %+v, want version set and not stale", st)
	}
	if got := ubStatus(t, ubDo(s, http.MethodGet, "")); got.PatternsStale || got.PatternsVersion != st.PatternsVersion {
		t.Errorf("GET after recheck = %+v", got)
	}

	s.manager.SetDetection(config.DetectionConfig{RateLimitPatterns: []string{"reworded limit"}})
	got := ubStatus(t, ubDo(s, http.MethodGet, ""))
	if !got.PatternsStale || got.PatternsVersion == st.PatternsVersion {
		t.Errorf("after pattern change = %+v, want stale with a new version", got)
	}
}

func TestUsageBackoff_RecheckMethodAndManager(t *testing.T) {
	s := patternsServer(t)
	if rr := ubRecheck(s, http.MethodGet); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET recheck = %d, want 405", rr.Code)
	}
	if rr := ubRecheck(usageBackoffServer(t), http.MethodPost); rr.Code != http.StatusServiceUnavailable {
		t.Errorf("recheck without manager = %d, want 503", rr.Code)
	}
}

func TestUsageBackoff_RecheckAudited(t *testing.T) {
	dir := t.TempDir()
	log, err := audit.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := patternsServer(t)
	s.auditLog = log
	_ = ubRecheck(s, http.MethodGet) // rejected: not audited
	rr := ubRecheck(s, http.MethodPost)
	_ = log.Close()

	got := readAuditLines(t, dir)
	if len(got) != 1 {
		t.Fatalf("want 1 audit entry, got %d: %+v", len(got), got)
	}
	if got[0].Action != "autonomous_usage_backoff_patterns_rechecked" || got[0].Actor != "operator" {
		t.Errorf("entry = %+v", got[0])
	}
	if want := ubStatus(t, rr).PatternsVersion; got[0].Details["patterns_version"] != want {
		t.Errorf("details = %+v, want patterns_version %q", got[0].Details, want)
	}
}

func TestUsageBackoff_RecheckCapabilityGate(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	s.cfg = &config.Config{}
	sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// In-memory tmux fake: these tests never need a live multiplexer.
	sm.WithFakeTmux()
	t.Cleanup(sm.WaitBackground)
	sm.SetClaudeCLIVersionRunner(func(context.Context, string, ...string) ([]byte, error) { return []byte("9.9.9-test"), nil })
	s.manager = sm
	capTest(t, s, http.MethodPost, "/api/usage-backoff/recheck", []byte(`{}`), s.handleUsageBackoffRecheck,
		http.StatusOK, http.StatusOK, http.StatusForbidden)
}

func TestCapacity_ReportsPatternsFields(t *testing.T) {
	s := patternsServer(t)
	for name, srv := range map[string]*Server{"with manager": s, "without manager": usageBackoffServer(t)} {
		rr := httptest.NewRecorder()
		srv.handleCapacity(rr, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
		for _, key := range []string{`"patterns_version":`, `"patterns_stale":`, `"claude_cli_version":`} {
			if !strings.Contains(rr.Body.String(), key) {
				t.Errorf("%s: capacity lacks %s: %s", name, key, rr.Body.String())
			}
		}
	}
	rr := httptest.NewRecorder()
	s.handleCapacity(rr, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	var resp struct {
		Version string `json:"patterns_version"`
		Stale   bool   `json:"patterns_stale"`
		CLI     string `json:"claude_cli_version"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version != s.manager.RateLimitPatternsVersion() || !resp.Stale || resp.CLI != "9.9.9-test" {
		t.Errorf("capacity patterns = %+v", resp)
	}

	// No manager: the key is still present and empty.
	empty := usageBackoffServer(t)
	rr = httptest.NewRecorder()
	empty.handleCapacity(rr, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if !strings.Contains(rr.Body.String(), `"claude_cli_version":""`) {
		t.Errorf("no-manager capacity lacks claude_cli_version: %s", rr.Body.String())
	}
}

func TestUsageBackoff_StatusCarriesClaudeCLIVersion(t *testing.T) {
	s, _ := patternsServerWithCLI(t, "9.9.9-test")
	st := ubStatus(t, ubDo(s, http.MethodGet, ""))
	if st.ClaudeCLIVersion != "9.9.9-test" {
		t.Errorf("claude_cli_version = %q, want 9.9.9-test", st.ClaudeCLIVersion)
	}
}

func TestUsageBackoff_RecheckStaysFreshUntilVersionChanges(t *testing.T) {
	s, fr := patternsServerWithCLI(t, "9.9.9-test")
	rr := ubRecheck(s, http.MethodPost)
	if rr.Code != http.StatusOK {
		t.Fatalf("recheck: %d %s", rr.Code, rr.Body.String())
	}
	st := ubStatus(t, rr)
	if st.PatternsStale || st.PatternsVersion == "" || st.ClaudeCLIVersion != "9.9.9-test" {
		t.Fatalf("recheck response = %+v, want not stale, version set, cli 9.9.9-test", st)
	}
	// Unchanged version: a plain re-check and GET stay not stale.
	if got := ubStatus(t, ubRecheck(s, http.MethodPost)); got.PatternsStale {
		t.Errorf("second recheck = %+v, want not stale", got)
	}
	if got := ubStatus(t, ubDo(s, http.MethodGet, "")); got.PatternsStale || got.ClaudeCLIVersion != "9.9.9-test" {
		t.Fatalf("GET after recheck = %+v, want not stale", got)
	}

	// A claude version change (tool upgrade rewords output) flips the guard
	// stale once the version is re-read, without any pattern change.
	fr.out = "8.8.8-upgraded"
	s.manager.RefreshClaudeCLIVersion()
	got := ubStatus(t, ubDo(s, http.MethodGet, ""))
	if !got.PatternsStale || got.ClaudeCLIVersion != "8.8.8-upgraded" || got.PatternsVersion == st.PatternsVersion {
		t.Errorf("after version change = %+v, want stale with new cli text and version", got)
	}

	// Recheck clears the flag (recording the new tool text as approved).
	got = ubStatus(t, ubRecheck(s, http.MethodPost))
	if got.PatternsStale || got.ClaudeCLIVersion != "8.8.8-upgraded" {
		t.Errorf("recheck after change = %+v, want cleared and cli 8.8.8-upgraded", got)
	}
}

func TestUsageBackoff_AbsentToolReportsUnknownWithoutStale(t *testing.T) {
	s, _ := patternsServerWithCLI(t, "")
	s.manager.SetClaudeCLIVersionRunner(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("claude: not found")
	})
	rr := ubRecheck(s, http.MethodPost)
	if rr.Code != http.StatusOK {
		t.Fatalf("recheck: %d %s", rr.Code, rr.Body.String())
	}
	st := ubStatus(t, rr)
	if st.ClaudeCLIVersion != "unknown" || st.PatternsStale {
		t.Errorf("recheck with absent tool = %+v, want unknown and not stale", st)
	}
	if st.PatternsVersion == "" {
		t.Error("patterns_version = \"\", want the pattern-only fingerprint")
	}
	// A bare absence of the tool never raises patterns_stale on its own.
	got := ubStatus(t, ubDo(s, http.MethodGet, ""))
	if got.ClaudeCLIVersion != "unknown" || got.PatternsStale {
		t.Errorf("GET with absent tool = %+v, want unknown and not stale", got)
	}
}

func TestUsageBackoff_RecheckAuditIncludesClaudeCLIVersion(t *testing.T) {
	dir := t.TempDir()
	log, err := audit.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := patternsServerWithCLI(t, "9.9.9-test")
	s.auditLog = log
	rr := ubRecheck(s, http.MethodPost)
	_ = log.Close()

	got := readAuditLines(t, dir)
	if len(got) != 1 {
		t.Fatalf("want 1 audit entry, got %d: %+v", len(got), got)
	}
	st := ubStatus(t, rr)
	if got[0].Details["patterns_version"] != st.PatternsVersion {
		t.Errorf("details = %+v, want patterns_version %q", got[0].Details, st.PatternsVersion)
	}
	if got[0].Details["claude_cli_version"] != "9.9.9-test" {
		t.Errorf("details = %+v, want claude_cli_version 9.9.9-test", got[0].Details)
	}
}
