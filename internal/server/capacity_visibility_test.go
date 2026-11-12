package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// visibilityAPI is an AutonomousAPI fake that also supplies the per-task
// wait and hold views.
type visibilityAPI struct {
	automatonStatusAPI
	waiting []autonomous.TaskWaitView
	running []autonomous.TaskHoldView
}

func (v *visibilityAPI) TaskVisibility() ([]autonomous.TaskWaitView, []autonomous.TaskHoldView) {
	return v.waiting, v.running
}

func TestCapacityVisibility_ResponseListsQueuedAndRunningTasks(t *testing.T) {
	since := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	resets := since.Add(time.Hour)
	api := &visibilityAPI{
		waiting: []autonomous.TaskWaitView{
			{TaskID: "t1", PRDID: "p1", PRDTitle: "One", TaskTitle: "Wait", Cause: autonomous.WaitCauseBackoff,
				Reason: "backend claude-code backed off until 13:00 UTC (resets in 60m)", Since: since, ResetsAt: &resets},
			{TaskID: "t3", PRDID: "p2", Cause: autonomous.WaitCauseNextInLine, Reason: "next in line to start", Since: since},
		},
		running: []autonomous.TaskHoldView{
			{TaskID: "t2", PRDID: "p1", TaskTitle: "Run", Pools: []string{"host", "node:n1", "tree:/w"}, Node: "n1", SessionID: "h-aa", Since: since},
			{TaskID: "t4", PRDID: "p2", Pools: []string{"host"}, Since: since},
		},
	}
	bo := autonomous.NewBackoff()
	bo.SetWithReset("claude-code", time.Now().Add(time.Hour), "usage limit", "test", false)
	s := &Server{autonomousMgr: api, usageBackoff: bo}
	s.SetCapacity(capacity.New(capacity.Options{}))

	type body struct {
		Backoff []struct {
			Backend    string `json:"backend"`
			ResetKnown *bool  `json:"reset_known"`
		} `json:"backoff"`
		Tasks struct {
			Waiting []autonomous.TaskWaitView `json:"waiting"`
			Running []autonomous.TaskHoldView `json:"running"`
		} `json:"tasks"`
	}
	get := func(url string) body {
		t.Helper()
		rec := httptest.NewRecorder()
		s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", url, rec.Code, rec.Body.String())
		}
		var b body
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return b
	}

	b := get("/api/capacity")
	if len(b.Tasks.Waiting) != 2 || len(b.Tasks.Running) != 2 {
		t.Fatalf("tasks = %+v; want 2 waiting, 2 running", b.Tasks)
	}
	w := b.Tasks.Waiting[0]
	if w.TaskID != "t1" || w.Cause != autonomous.WaitCauseBackoff || w.ResetsAt == nil || !w.ResetsAt.Equal(resets) || w.Reason == "" {
		t.Errorf("waiting view lost fields: %+v", w)
	}
	r := b.Tasks.Running[0]
	if r.TaskID != "t2" || r.Node != "n1" || r.SessionID != "h-aa" || len(r.Pools) != 3 {
		t.Errorf("running view lost fields: %+v", r)
	}
	if len(b.Backoff) != 1 || b.Backoff[0].ResetKnown == nil || *b.Backoff[0].ResetKnown {
		t.Errorf("backoff entry must carry reset_known=false: %+v", b.Backoff)
	}

	if scoped := get("/api/capacity?prd_id=p2"); len(scoped.Tasks.Waiting) != 1 || scoped.Tasks.Waiting[0].TaskID != "t3" ||
		len(scoped.Tasks.Running) != 1 || scoped.Tasks.Running[0].TaskID != "t4" {
		t.Errorf("prd_id must scope the task views: %+v", scoped.Tasks)
	}

	// Without a provider both lists are still arrays, never null.
	for name, srv := range map[string]*Server{"no manager": {}, "no ledger": {autonomousMgr: &automatonStatusAPI{}}} {
		rec := httptest.NewRecorder()
		srv.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
		var raw struct {
			Tasks map[string]json.RawMessage `json:"tasks"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		for _, k := range []string{"waiting", "running"} {
			if string(raw.Tasks[k]) != "[]" {
				t.Errorf("%s: tasks.%s = %s; want []", name, k, raw.Tasks[k])
			}
		}
	}
}

func TestCapacityVisibility_SessionStatusCarriesCauseAndHoldings(t *testing.T) {
	resets := time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)
	api := &visibilityAPI{
		automatonStatusAPI: automatonStatusAPI{prd: automatonStatusPRD("do the thing")},
		waiting: []autonomous.TaskWaitView{
			{TaskID: "t1", PRDID: "prd1", Cause: autonomous.WaitCauseBackoff, Reason: "backend claude-code backed off", ResetsAt: &resets},
			{TaskID: "other", PRDID: "prd1", Cause: autonomous.WaitCausePoolFull, Reason: "pool host full"},
		},
		running: []autonomous.TaskHoldView{
			{TaskID: "t1", PRDID: "prd1", Pools: []string{"tree:/w/proj"}, Note: "the inference slot is released but the tree lock is kept"},
		},
	}
	s := automatonStatusServer(t, api, &session.Session{ID: "aa", FullID: "h-aa", PRDID: "prd1", TaskID: "t1"})
	a, ok := getSessionStatus(t, s, "h-aa")["automaton"].(map[string]any)
	if !ok {
		t.Fatal("automaton block missing")
	}
	if a["wait_cause"] != "backoff" {
		t.Errorf("wait_cause = %v; want backoff", a["wait_cause"])
	}
	if a["wait_resets_at"] != "2026-10-22T13:00:00Z" {
		t.Errorf("wait_resets_at = %v", a["wait_resets_at"])
	}
	holds, _ := a["holds"].([]any)
	if len(holds) != 1 || holds[0] != "tree:/w/proj" {
		t.Errorf("holds = %v; want [tree:/w/proj]", a["holds"])
	}
	if a["holds_note"] != "the inference slot is released but the tree lock is kept" {
		t.Errorf("holds_note = %v", a["holds_note"])
	}

	// A fake without the visibility capability leaves the fields out.
	plain := automatonStatusServer(t, &automatonStatusAPI{prd: automatonStatusPRD("x")}, &session.Session{ID: "aa", FullID: "h-aa", PRDID: "prd1", TaskID: "t1"})
	pa := getSessionStatus(t, plain, "h-aa")["automaton"].(map[string]any)
	for _, k := range []string{"wait_cause", "wait_resets_at", "holds", "holds_note"} {
		if _, present := pa[k]; present {
			t.Errorf("%s present without a visibility provider", k)
		}
	}
}
