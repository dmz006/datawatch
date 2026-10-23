package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

func TestRoleContention_ServerWritePathWarnsNotFails(t *testing.T) {
	reg := inference.NewRegistry()
	for _, n := range []string{"a", "b"} {
		if err := reg.Add(&inference.LLM{Name: n, Kind: inference.KindOllama, Roles: []string{"planning"}}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{inferenceReg: reg}
	if w := s.CheckRoleContention(); len(w) != 1 {
		t.Fatalf("want one warning, got %v", w)
	}
	rec := httptest.NewRecorder()
	s.handleLLMs(rec, httptest.NewRequest(http.MethodGet, "/api/llms", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "role_warnings") ||
		!strings.Contains(rec.Body.String(), "planning") {
		t.Fatalf("listing status missing warnings: %d %s", rec.Code, rec.Body.String())
	}
}
