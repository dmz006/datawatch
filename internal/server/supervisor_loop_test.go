package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

type fakeLoopControl struct {
	recs   []supervisorloop.RunRecord
	ran    []string
	runErr error
}

func (f *fakeLoopControl) Runs() ([]supervisorloop.RunRecord, error) { return f.recs, nil }
func (f *fakeLoopControl) RunJob(_ context.Context, name string, _ SupervisorLoopRunInput) (string, error) {
	f.ran = append(f.ran, name)
	return "done " + name, f.runErr
}

func newLoopServer(t *testing.T, recs ...supervisorloop.RunRecord) (*Server, *fakeLoopControl) {
	t.Helper()
	ctl := &fakeLoopControl{recs: recs}
	return &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml"), supervisorLoop: ctl}, ctl
}

func loopRec(job string, out supervisorloop.Outcome, attempts int, started time.Time) supervisorloop.RunRecord {
	return supervisorloop.RunRecord{Job: job, Outcome: out, Attempts: attempts, Detail: "r-" + job,
		ValidatorErrors: []string{"bad"}, Started: started, Duration: 1500 * time.Millisecond}
}

func loopGet(t *testing.T, h http.HandlerFunc, url string) (int, map[string]json.RawMessage) {
	t.Helper()
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, url, nil))
	var m map[string]json.RawMessage
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	return rr.Code, m
}

func TestSupervisorLoopListJobs(t *testing.T) {
	now := time.Now()
	s, _ := newLoopServer(t, loopRec("cleanup_sweep", supervisorloop.OutcomeOK, 1, now))
	code, m := loopGet(t, s.handleSupervisorLoopJobs, "/api/supervisor-loop/jobs")
	if code != 200 {
		t.Fatalf("code %d", code)
	}
	var jobs []supervisorLoopJobView
	if err := json.Unmarshal(m["jobs"], &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 11 {
		t.Fatalf("want 11 jobs, got %d", len(jobs))
	}
	by := map[string]supervisorLoopJobView{}
	for _, j := range jobs {
		by[j.Name] = j
	}
	if by["cleanup_sweep"].LastOutcome != "ok" || by["cleanup_sweep"].Kind != "mechanical" || !by["cleanup_sweep"].Enabled {
		t.Errorf("cleanup_sweep: %+v", by["cleanup_sweep"])
	}
	if d := by["draft_spec"]; d.Style != "oneshot" || d.Kind != "model" || d.LastOutcome != "" {
		t.Errorf("draft_spec: %+v", d)
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.supervisor_loop.cleanup_sweep.enabled": false,
		"autonomous.supervisor_loop.pick_backend.model":    "m1",
	})
	_, m = loopGet(t, s.handleSupervisorLoopJobs, "/api/supervisor-loop/jobs")
	_ = json.Unmarshal(m["jobs"], &jobs)
	for _, j := range jobs {
		if j.Name == "cleanup_sweep" && j.Enabled {
			t.Error("cleanup_sweep should read disabled")
		}
		if j.Name == "pick_backend" && j.Model != "m1" {
			t.Errorf("pick_backend model %q", j.Model)
		}
	}
}

func TestSupervisorLoopRunsFilter(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	s, _ := newLoopServer(t,
		loopRec("draft_spec", supervisorloop.OutcomeEscalated, 3, t0),
		loopRec("pick_backend", supervisorloop.OutcomeOK, 1, t0.Add(time.Minute)),
		loopRec("draft_spec", supervisorloop.OutcomeOK, 2, t0.Add(2*time.Minute)),
	)
	runs := func(q string) []supervisorLoopRunView {
		code, m := loopGet(t, s.handleSupervisorLoopRuns, "/api/supervisor-loop/runs"+q)
		if code != 200 {
			t.Fatalf("%s: code %d", q, code)
		}
		var r []supervisorLoopRunView
		_ = json.Unmarshal(m["runs"], &r)
		return r
	}
	all := runs("")
	if len(all) != 3 || all[0].Outcome != "ok" || all[0].Job != "draft_spec" || all[2].Outcome != "escalated" {
		t.Fatalf("newest first: %+v", all)
	}
	if all[0].Retries != 1 || all[2].Escalations != 1 || all[0].DurationMS != 1500 || len(all[0].ValidatorFailures) != 1 || all[0].Result != "r-draft_spec" {
		t.Errorf("fields: %+v", all)
	}
	if r := runs("?job=draft_spec"); len(r) != 2 {
		t.Errorf("job filter: %d", len(r))
	}
	if r := runs("?outcome=escalated"); len(r) != 1 || r[0].Job != "draft_spec" {
		t.Errorf("outcome filter: %+v", r)
	}
	if r := runs("?limit=1"); len(r) != 1 || r[0].Outcome != "ok" {
		t.Errorf("limit: %+v", r)
	}
	for _, bad := range []string{"?limit=0", "?limit=x", "?limit=501", "?outcome=nope"} {
		if code, _ := loopGet(t, s.handleSupervisorLoopRuns, "/api/supervisor-loop/runs"+bad); code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", bad, code)
		}
	}
}

func TestSupervisorLoopRunUnknownJob404(t *testing.T) {
	s, ctl := newLoopServer(t)
	post := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		s.handleSupervisorLoopJobRun(rr, httptest.NewRequest(http.MethodPost, path, strings.NewReader("")))
		return rr
	}
	if rr := post("/api/supervisor-loop/jobs/nope/run"); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown job: want 404, got %d", rr.Code)
	}
	if len(ctl.ran) != 0 {
		t.Fatal("an unknown job must not run")
	}
	rr := post("/api/supervisor-loop/jobs/cleanup_sweep/run")
	if rr.Code != 200 || len(ctl.ran) != 1 || ctl.ran[0] != "cleanup_sweep" {
		t.Fatalf("known job: %d %v", rr.Code, ctl.ran)
	}
	rr = httptest.NewRecorder()
	s.handleSupervisorLoopJobRun(rr, httptest.NewRequest(http.MethodGet, "/api/supervisor-loop/jobs/cleanup_sweep/run", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: want 405, got %d", rr.Code)
	}
}

func TestSupervisorLoopPutThenGetRoundTrip(t *testing.T) {
	// A non-default value for every key.
	want := map[string]any{"enabled": false, "validator_retries": 4, "escalate_after_failures": 3,
		"run_retention": 100, "archive_cancelled_after_minutes": 15, "lane_count.min": 1, "lane_count.max": 6,
		"wake.enabled": true, "wake.kinds": []any{"task.failed", "prd.blocked"}, "wake.coalesce_seconds": 5, "wake.fallback_interval_minutes": 7}
	for _, n := range supervisorloop.StepNames {
		want[n+".enabled"] = false
	}
	for _, n := range supervisorloop.ModelJobNames {
		want[n+".style"], want[n+".backend"], want[n+".model"] = "session", "be-"+n, "mo-"+n
	}
	if len(want) != len(supervisorloop.Keys()) {
		t.Fatalf("test covers %d keys, the block has %d", len(want), len(supervisorloop.Keys()))
	}
	for key, val := range want {
		s, _ := newLoopServer(t)
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.supervisor_loop." + key: val})
		got := getAutonomousConfigMap(t, s)
		var surf map[string]any
		if err := json.Unmarshal(got["supervisor_loop"], &surf); err != nil {
			t.Fatal(err)
		}
		if g, w := loopSurfaceValue(surf[key]), loopSurfaceValue(val); g != w {
			t.Errorf("%s: GET after PUT = %v, want %v", key, g, w)
		}
		// Every other key still reads its default.
		def := supervisorloop.DefaultSettings().Flat()
		for k, v := range surf {
			if k == key {
				continue
			}
			if dv, gv := loopSurfaceValue(def["supervisor_loop."+k]), loopSurfaceValue(v); dv != gv {
				t.Errorf("PUT %s moved %s: %v, want default %v", key, k, gv, dv)
			}
		}
	}
	// The plan-style key works too.
	s, _ := newLoopServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"supervisor_loop.enabled": false})
	if s.supervisorLoopSettings().Enabled {
		t.Error("supervisor_loop.enabled PUT did not stick")
	}
}

func TestSupervisorLoopPutBadValueRefusesWholePut(t *testing.T) {
	bad := map[string]any{
		"enabled": "yes", "validator_retries": -1, "escalate_after_failures": 0, "run_retention": 0,
		"lane_count.min": -2, "lane_count.max": "x", "draft_spec.style": "weekly", "pick_backend.model": 7,
		"cleanup_sweep.enabled": 1, "nope": true, "validator_retries.x": 1,
	}
	for key, val := range bad {
		s, _ := newLoopServer(t)
		b, _ := json.Marshal(map[string]any{
			"autonomous.supervisor_loop.run_retention": 77, // good sibling must not apply
			"autonomous.supervisor_loop." + key:        val,
		})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s=%v: want 400, got %d", key, val, rr.Code)
			continue
		}
		if !strings.Contains(rr.Body.String(), key) {
			t.Errorf("%s: error does not name the key: %q", key, rr.Body.String())
		}
		if s.supervisorLoopSettings().RunRetention == 77 {
			t.Errorf("%s: the good sibling key was applied", key)
		}
	}
}

// TestSupervisorLoopArchiveGraceRoundTrip: the archive grace is served and
// stored by the generic supervisor_loop path (it derives from
// supervisorloop.Settings, no key list of its own). A PUT is read back by GET
// in both key spellings, 0 is accepted, and a negative value refuses the whole
// PUT, naming the key, without applying its good sibling.
func TestSupervisorLoopArchiveGraceRoundTrip(t *testing.T) {
	const key = "archive_cancelled_after_minutes"
	grace := func(s *Server) any {
		var surf map[string]any
		if err := json.Unmarshal(getAutonomousConfigMap(t, s)["supervisor_loop"], &surf); err != nil {
			t.Fatal(err)
		}
		return surf[key]
	}

	s, _ := newLoopServer(t)
	if g := grace(s); g != float64(60) {
		t.Fatalf("GET default = %v, want 60", g)
	}
	for _, prefix := range []string{"autonomous.supervisor_loop.", "supervisor_loop."} {
		for _, v := range []int{15, 0, 240} {
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{prefix + key: v})
			if g := grace(s); g != float64(v) {
				t.Errorf("%s%s: PUT %d, GET %v", prefix, key, v, g)
			}
			if got := s.supervisorLoopSettings().ArchiveCancelledAfterMinutes; got != v {
				t.Errorf("%s%s: live settings = %d, want %d", prefix, key, got, v)
			}
		}
	}

	// A negative value refuses the whole PUT.
	s, _ = newLoopServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.supervisor_loop." + key: 15})
	b, _ := json.Marshal(map[string]any{
		"autonomous.supervisor_loop.run_retention": 77, // good sibling must not apply
		"autonomous.supervisor_loop." + key:        -1,
	})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("negative: want 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), key) {
		t.Errorf("error does not name the key: %q", rr.Body.String())
	}
	if g := grace(s); g != float64(15) {
		t.Errorf("refused PUT changed the grace to %v, want 15", g)
	}
	if s.supervisorLoopSettings().RunRetention == 77 {
		t.Error("the good sibling key was applied")
	}

	// A non-number is refused too.
	b, _ = json.Marshal(map[string]any{"autonomous.supervisor_loop." + key: "soon"})
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("string value: want 400, got %d", rr.Code)
	}
}

func TestSupervisorLoopStatsEndpoint(t *testing.T) {
	now := time.Now()
	s, _ := newLoopServer(t,
		loopRec("draft_spec", supervisorloop.OutcomeOK, 1, now.Add(-time.Hour)),
		loopRec("pick_backend", supervisorloop.OutcomeEscalated, 3, now.Add(-time.Minute)))
	rr := httptest.NewRecorder()
	s.handleSupervisorLoopStats(rr, httptest.NewRequest(http.MethodGet, "/api/supervisor-loop/stats", nil))
	if rr.Code != 200 {
		t.Fatalf("code %d", rr.Code)
	}
	var st supervisorloop.Stats
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || st.Jobs != 11 || st.Runs != 2 || st.RunsOK != 1 || st.RunsEscalated != 1 || st.Retries != 2 {
		t.Errorf("stats: %+v", st)
	}
	if st.LastRunAgeSeconds < 55 || st.LastRunAgeSeconds > 120 {
		t.Errorf("age = %d", st.LastRunAgeSeconds)
	}
	rr = httptest.NewRecorder()
	s.handleSupervisorLoopStats(rr, httptest.NewRequest(http.MethodPost, "/api/supervisor-loop/stats", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST code %d", rr.Code)
	}
}

// loopSurfaceValue folds a decoded config value into a comparable form: a
// JSON number to int, a list (the wake kinds) to its joined text.
func loopSurfaceValue(v any) any {
	switch x := v.(type) {
	case float64:
		return int(x)
	case []string:
		return strings.Join(x, ",")
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = fmt.Sprint(e)
		}
		return strings.Join(parts, ",")
	}
	return v
}
