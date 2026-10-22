// BL428 Phase 4 — /api/usage-backoff REST tests.

package server

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	if got[0].Action != audit.ActionUsageBackoffSet || got[0].Actor != "operator" {
		t.Errorf("entry 0 = %+v", got[0])
	}
	if got[0].Details["backend"] != "claude-code" || got[0].Details["source"] != "operator" {
		t.Errorf("set details = %+v", got[0].Details)
	}
	if _, err := time.Parse(time.RFC3339, got[0].Details["until"].(string)); err != nil {
		t.Errorf("set until not RFC3339: %v", got[0].Details["until"])
	}
	if got[1].Action != audit.ActionUsageBackoffCleared || got[1].Details["backend"] != "claude-code" {
		t.Errorf("entry 1 = %+v", got[1])
	}
	if got[2].Action != audit.ActionUsageBackoffCleared || got[2].Details["backend"] != "all" {
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
