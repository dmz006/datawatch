package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

func TestHandleLLMRolesTableAndWarnings(t *testing.T) {
	reg := inference.NewRegistry()
	s := &Server{inferenceReg: reg}
	for _, body := range []string{
		`{"name":"a","kind":"ollama","roles":["planning","review"]}`,
		`{"name":"b","kind":"ollama","roles":["planning"]}`,
	} {
		if rec := roleReq(t, s, http.MethodPost, "/api/llms", body); rec.Code != http.StatusOK {
			t.Fatal(rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	s.handleLLMRoles(rec, httptest.NewRequest(http.MethodGet, "/api/llm-roles", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Roles []struct{ Role, Entry string } `json:"roles"`
		Warns []string                       `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range out.Roles {
		got = append(got, r.Role+"="+r.Entry)
	}
	if want := []string{"planning=a", "review=a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("roles = %v, want %v", got, want)
	}
	if len(out.Warns) != 1 {
		t.Errorf("warnings = %v, want one ambiguity warning", out.Warns)
	}

	rec = httptest.NewRecorder()
	s.handleLLMRoles(rec, httptest.NewRequest(http.MethodPost, "/api/llm-roles", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d", rec.Code)
	}
}
