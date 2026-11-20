package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// verifyingAPI adds the verifying views to the visibility fake.
type verifyingAPI struct {
	visibilityAPI
	verifying []autonomous.TaskVerifyView
}

func (v *verifyingAPI) TaskVerifyingViews() []autonomous.TaskVerifyView { return v.verifying }

func getVerifying(t *testing.T, s *Server, url string) []autonomous.TaskVerifyView {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, rec.Code, rec.Body.String())
	}
	var b struct {
		Tasks struct {
			Verifying []autonomous.TaskVerifyView `json:"verifying"`
			Running   []autonomous.TaskHoldView   `json:"running"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return b.Tasks.Verifying
}

func TestCapacityVerifying_ListsVerifyingAndWaitingForVerifier(t *testing.T) {
	since := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	api := &verifyingAPI{
		visibilityAPI: visibilityAPI{
			running: []autonomous.TaskHoldView{{TaskID: "run1", PRDID: "p1", Pools: []string{"host"}, Since: since}},
		},
		verifying: []autonomous.TaskVerifyView{
			{TaskID: "v1", PRDID: "p1", TaskTitle: "Check", Node: "n1", Since: since, State: autonomous.VerifyStateVerifying},
			{TaskID: "v2", PRDID: "p2", TaskTitle: "Queued", Since: since, State: autonomous.VerifyStateWaitingForVerifier,
				WaitReason: "pool llm:x full"},
		},
	}
	s := &Server{autonomousMgr: api}
	s.SetCapacity(capacity.New(capacity.Options{}))

	got := getVerifying(t, s, "/api/capacity")
	if len(got) != 2 {
		t.Fatalf("verifying = %+v; want 2", got)
	}
	if got[0].TaskID != "v1" || got[0].State != "verifying" || got[0].Node != "n1" {
		t.Errorf("verifying view wrong: %+v", got[0])
	}
	if got[1].TaskID != "v2" || got[1].State != "waiting_for_verifier" || got[1].WaitReason == "" {
		t.Errorf("waiting view wrong: %+v", got[1])
	}
	for _, v := range got {
		if v.TaskID == "run1" {
			t.Errorf("a running task must not appear in verifying: %+v", v)
		}
	}
	if scoped := getVerifying(t, s, "/api/capacity?prd_id=p2"); len(scoped) != 1 || scoped[0].TaskID != "v2" {
		t.Errorf("prd_id must scope verifying: %+v", scoped)
	}
}

func TestCapacityVerifying_AlwaysArray(t *testing.T) {
	for name, srv := range map[string]*Server{"no manager": {}, "no provider": {autonomousMgr: &automatonStatusAPI{}}} {
		rec := httptest.NewRecorder()
		srv.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
		var raw struct {
			Tasks map[string]json.RawMessage `json:"tasks"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if string(raw.Tasks["verifying"]) != "[]" {
			t.Errorf("%s: tasks.verifying = %s; want []", name, raw.Tasks["verifying"])
		}
	}
}
