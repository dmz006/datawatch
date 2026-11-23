package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
)

// The capacity read lists each worker lease over its pool limit with the
// machine-readable over_limit flag, code and reason.
func TestHandleCapacity_OverLimitLeases(t *testing.T) {
	s := &Server{}
	l := capacity.New(capacity.Options{})
	s.SetCapacity(l)
	l.SetLimit("llm:x", 1)
	for _, h := range []string{"a", "b"} {
		l.Adopt(capacity.Request{Holder: h, PRDID: h, Pools: []string{"llm:x"}}, "s-"+h)
	}
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Leases []struct {
			Holder          string `json:"holder"`
			OverLimit       bool   `json:"over_limit"`
			OverLimitCode   string `json:"over_limit_code"`
			OverLimitReason string `json:"over_limit_reason"`
		} `json:"leases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Leases) != 2 {
		t.Fatalf("leases = %+v", body.Leases)
	}
	for _, ls := range body.Leases {
		if !ls.OverLimit || ls.OverLimitCode != "adopted_over_limit" || ls.OverLimitReason == "" {
			t.Fatalf("lease not flagged: %+v", ls)
		}
	}
}
