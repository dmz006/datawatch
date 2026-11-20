package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/planrecords"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

type setupClock struct{ t time.Time }

func (c *setupClock) Now() time.Time { c.t = c.t.Add(time.Second); return c.t }

type setupSession struct{}

func (setupSession) ProjectDir(p string) (string, error) { return "/work/" + p, nil }
func (setupSession) SupervisorProfile(string) (string, error) {
	return "supervisor-lean", nil
}

type setupRules struct{}

func (setupRules) DefaultPack() (string, error)                  { return "default pack", nil }
func (setupRules) ProjectPack(string) (string, bool, error)      { return "", false, nil }
func (setupRules) ProjectRulesFile(string) (string, bool, error) { return "rules file", true, nil }

type setupPlans struct{}

func (setupPlans) ProjectPlans(string) ([]planrecords.PlanDigest, error) { return nil, nil }
func (setupPlans) BlockedReason(string, string) (string, error)          { return "", nil }

type setupSchedules struct {
	byName map[string]*session.ScheduledCommand
	opts   []session.AddSpawnOptions
}

func (f *setupSchedules) EnsureNamedSpawn(o session.AddSpawnOptions) (*session.ScheduledCommand, bool, error) {
	if e, ok := f.byName[o.ScheduleName]; ok {
		return e, false, nil
	}
	e := &session.ScheduledCommand{ID: "id-" + o.ScheduleName, CronExpr: o.CronExpr, ScheduleName: o.ScheduleName}
	f.byName[o.ScheduleName] = e
	f.opts = append(f.opts, o)
	return e, true, nil
}

type setupMemory struct{ m map[string]string }

func (s *setupMemory) LoadPinned(k string) (string, bool, error) { v, ok := s.m[k]; return v, ok, nil }
func (s *setupMemory) SavePinned(k, t string) error              { s.m[k] = t; return nil }
func (s *setupMemory) Recall(string) ([]string, error)           { return nil, nil }

type setupPack struct{ committed []string }

func (p *setupPack) CommitRule(_, text string) (string, error) {
	p.committed = append(p.committed, text)
	return "commit-1", nil
}

func newSetupServer() (*Server, *setupSchedules, *setupPack) {
	clk := &setupClock{t: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	sch := &setupSchedules{byName: map[string]*session.ScheduledCommand{}}
	pack := &setupPack{}
	recs := supervisorloop.NewStateRecords(&setupMemory{m: map[string]string{}}, clk)
	d := &SupervisorSetupDeps{
		Session: setupSession{}, Rules: setupRules{}, Plans: setupPlans{}, Schedules: sch,
		Records: recs, Corrections: supervisorloop.NewCorrections(recs, pack, nil, clk), Clock: clk,
	}
	return &Server{supervisorSetup: d}, sch, pack
}

func setupCall(h http.HandlerFunc, method, url string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(method, url, &buf))
	return rr
}

func TestSupervisorSetupIdempotent(t *testing.T) {
	s, sch, _ := newSetupServer()
	body := map[string]any{"project": "demo", "model": "claude-haiku-5-5",
		"schedules": []map[string]string{{"name": "watch", "cron": "*/10 * * * *"}, {"name": "full", "cron": "0 * * * *"}}}
	rr := setupCall(s.handleSupervisorSetup, http.MethodPost, "/api/supervisor-setup", body)
	if rr.Code != 200 {
		t.Fatalf("code %d: %s", rr.Code, rr.Body)
	}
	var res SupervisorSetupResult
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res.LaunchRole != "supervisor" || res.ProjectDir != "/work/demo" || res.Model != "claude-haiku-5-5" || len(res.Schedules) != 2 || res.AllExisting {
		t.Fatalf("unexpected result %+v", res)
	}
	o := sch.opts[0]
	if o.LaunchRole != "supervisor" || o.Model != "claude-haiku-5-5" || o.ProjectDir != "/work/demo" || !strings.Contains(o.Task, "Model: claude-haiku-5-5") {
		t.Fatalf("schedule options %+v", o)
	}
	rr = setupCall(s.handleSupervisorSetup, http.MethodPost, "/api/supervisor-setup", body)
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if !res.AllExisting || len(sch.opts) != 2 {
		t.Fatalf("second call added schedules: %+v / %d", res, len(sch.opts))
	}
}

func TestSupervisorSetupRefusals(t *testing.T) {
	s, _, _ := newSetupServer()
	for name, body := range map[string]map[string]any{
		"opus":       {"project": "demo", "model": "claude-opus-5-5"},
		"no model":   {"project": "demo"},
		"no project": {"model": "claude-haiku-5-5"},
		"bad cron":   {"project": "demo", "model": "claude-haiku-5-5", "schedules": []map[string]string{{"name": "a", "cron": "nope"}}},
		"dup name":   {"project": "demo", "model": "claude-haiku-5-5", "schedules": []map[string]string{{"name": "a"}, {"name": "a"}}},
	} {
		if rr := setupCall(s.handleSupervisorSetup, http.MethodPost, "/api/supervisor-setup", body); rr.Code != 400 {
			t.Errorf("%s: code %d", name, rr.Code)
		}
	}
	if rr := setupCall(s.handleSupervisorSetup, http.MethodGet, "/api/supervisor-setup", nil); rr.Code != 405 {
		t.Errorf("GET code %d", rr.Code)
	}
	if rr := setupCall((&Server{}).handleSupervisorSetup, http.MethodPost, "/api/supervisor-setup", map[string]any{}); rr.Code != 503 {
		t.Errorf("unwired code %d", rr.Code)
	}
}

func TestSupervisorSetupStateEdits(t *testing.T) {
	s, _, _ := newSetupServer()
	h := s.handleSupervisorSetupState
	set := map[string]any{"project": "demo", "id": "d1", "section": "decision", "text": "keep lanes", "who": "op", "why": "policy"}
	if rr := setupCall(h, http.MethodPost, "/x", set); rr.Code != 200 {
		t.Fatalf("set %d %s", rr.Code, rr.Body)
	}
	if rr := setupCall(h, http.MethodPatch, "/x", map[string]any{"project": "demo", "id": "d1", "text": "keep two lanes", "who": "op", "why": "tune"}); rr.Code != 200 {
		t.Fatalf("change %d %s", rr.Code, rr.Body)
	}
	rr := setupCall(h, http.MethodGet, "/x?project=demo", nil)
	var rec supervisorloop.StateRecord
	_ = json.Unmarshal(rr.Body.Bytes(), &rec)
	if len(rec.Entries) != 1 || rec.Entries[0].Text != "keep two lanes" || len(rec.Entries[0].Edits) != 2 {
		t.Fatalf("record %+v", rec)
	}
	if got := rec.Entries[0].Edits[1].When; !got.After(time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)) || got.Year() != 2030 {
		t.Fatalf("edit time not from injected clock: %v", got)
	}
	if rr := setupCall(h, http.MethodPatch, "/x", map[string]any{"project": "demo", "id": "nope", "text": "a", "who": "op", "why": "w"}); rr.Code != 404 {
		t.Fatalf("missing change %d", rr.Code)
	}
	if rr := setupCall(h, http.MethodPost, "/x", map[string]any{"project": "demo", "id": "d2", "section": "bogus", "who": "op", "why": "w"}); rr.Code != 400 {
		t.Fatalf("bad section %d", rr.Code)
	}
	if rr := setupCall(h, http.MethodDelete, "/x", map[string]any{"project": "demo", "id": "d1", "who": "op", "why": "done"}); rr.Code != 200 {
		t.Fatalf("remove %d", rr.Code)
	}
	rr = setupCall(h, http.MethodGet, "/x?project=demo", nil)
	if !strings.Contains(rr.Body.String(), `"entries":[]`) {
		t.Fatalf("not empty: %s", rr.Body)
	}
}

func TestSupervisorSetupCorrections(t *testing.T) {
	s, _, pack := newSetupServer()
	rec := map[string]any{"project": "demo", "id": "c1", "text": "never skip tests", "class": "standing_rule", "who": "op", "why": "said so"}
	if rr := setupCall(s.handleSupervisorSetupCorrections, http.MethodPost, "/x", rec); rr.Code != 200 {
		t.Fatalf("record %d %s", rr.Code, rr.Body)
	}
	rr := setupCall(s.handleSupervisorSetupCorrections, http.MethodGet, "/x?project=demo", nil)
	if !strings.Contains(rr.Body.String(), `"c1"`) {
		t.Fatalf("list %s", rr.Body)
	}
	prom := map[string]any{"project": "demo", "id": "c1", "who": "op", "why": "durable"}
	rr = setupCall(s.handleSupervisorSetupPromote, http.MethodPost, "/x", prom)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"promoted"`) || len(pack.committed) != 1 {
		t.Fatalf("promote %d %s", rr.Code, rr.Body)
	}
	rr = setupCall(s.handleSupervisorSetupCorrections, http.MethodGet, "/x?project=demo", nil)
	if !strings.Contains(rr.Body.String(), `"corrections":[]`) {
		t.Fatalf("promoted correction still listed: %s", rr.Body)
	}
	if rr := setupCall(s.handleSupervisorSetupPromote, http.MethodPost, "/x", prom); rr.Code != 404 {
		t.Fatalf("second promote %d", rr.Code)
	}
	st := map[string]any{"project": "demo", "id": "c2", "text": "only now", "class": "state", "who": "op", "why": "w"}
	setupCall(s.handleSupervisorSetupCorrections, http.MethodPost, "/x", st)
	prom["id"] = "c2"
	if rr := setupCall(s.handleSupervisorSetupPromote, http.MethodPost, "/x", prom); rr.Code != 409 {
		t.Fatalf("promote state class %d", rr.Code)
	}
}
