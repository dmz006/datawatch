package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

func roleReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleLLMs(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestRoleParityRESTStoresRolesAndUpdateSemantics(t *testing.T) {
	reg := inference.NewRegistry()
	s := &Server{inferenceReg: reg}

	// The body MCP, CLI and comm all send for the same input.
	if rec := roleReq(t, s, http.MethodPost, "/api/llms", `{"name":"a","kind":"ollama","roles":["planning","Review"," planning "]}`); rec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := reg.Get("a")
	if want := []string{"planning", "review"}; !reflect.DeepEqual(got.Roles, want) {
		t.Fatalf("stored roles = %v, want %v", got.Roles, want)
	}

	// omitting roles keeps the stored list
	if rec := roleReq(t, s, http.MethodPut, "/api/llms/a", `{"name":"a","kind":"ollama","model":"m"}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	got, _ = reg.Get("a")
	if !reflect.DeepEqual(got.Roles, []string{"planning", "review"}) {
		t.Errorf("update without roles changed them: %v", got.Roles)
	}

	// get carries roles and the warnings naming this entry
	if rec := roleReq(t, s, http.MethodPost, "/api/llms", `{"name":"b","kind":"ollama","roles":["planning"]}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	rec := roleReq(t, s, http.MethodGet, "/api/llms/a", "")
	var view struct {
		Roles        []string `json:"roles"`
		RoleWarnings []string `json:"role_warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.Roles, []string{"planning", "review"}) || len(view.RoleWarnings) != 1 {
		t.Errorf("get view = %+v (%s)", view, rec.Body.String())
	}

	// an empty list clears
	if rec := roleReq(t, s, http.MethodPut, "/api/llms/a", `{"name":"a","kind":"ollama","roles":[]}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	got, _ = reg.Get("a")
	if len(got.Roles) != 0 {
		t.Errorf("empty list should clear roles: %v", got.Roles)
	}

	// an empty role is a validation error, same shape as other llm errors
	rec = roleReq(t, s, http.MethodPut, "/api/llms/a", `{"name":"a","kind":"ollama","roles":["x",""]}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "llm: roles must not contain an empty role") {
		t.Errorf("invalid role: %d %q", rec.Code, rec.Body.String())
	}
}
