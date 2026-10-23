package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestVerifierMetrics_CapacityAPIIncludesVerifierObject(t *testing.T) {
	autonomous.DefaultVerifierStats.Record(20*time.Second, 2*time.Minute, autonomous.VerifierCatInfra)
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	var body struct {
		Verifier *autonomous.VerifierStatsView `json:"verifier"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	v := body.Verifier
	if v == nil || v.Runs < 1 || v.Categories["infra"] < 1 || v.WaitMaxSec < 20 || v.RunMaxSec < 120 {
		t.Fatalf("verifier object missing or wrong: %s", rec.Body.String())
	}
}
