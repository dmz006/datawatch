package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func TestFiltersAPI_InputTypingRules(t *testing.T) {
	s := bl90Server(t)
	fs, err := session.NewFilterStore(filepath.Join(t.TempDir(), "filters.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.filterStore = fs
	call := func(method, body string) (int, string) {
		rr := httptest.NewRecorder()
		s.handleFilters(rr, httptest.NewRequest(method, "/api/filters", strings.NewReader(body)))
		return rr.Code, rr.Body.String()
	}

	// Not on purpose: refused.
	if code, _ := call(http.MethodPost, `{"pattern":"batch done","action":"send_input","value":"continue"}`); code != http.StatusBadRequest {
		t.Fatalf("create without allow_input: %d", code)
	}
	// A prompt pattern: refused even on purpose.
	if code, body := call(http.MethodPost, `{"pattern":"Proceed\\?","action":"schedule","value":"yes","allow_input":true}`); code != http.StatusBadRequest {
		t.Fatalf("prompt pattern: %d %s", code, body)
	}
	if len(fs.List()) != 0 {
		t.Fatalf("stored %+v", fs.List())
	}
	if code, body := call(http.MethodPost, `{"pattern":"batch done","action":"send_input","value":"continue","allow_input":true}`); code != http.StatusCreated || !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("create: %d %s", code, body)
	}
	id := fs.List()[0].ID
	if code, body := call(http.MethodPatch, `{"id":"`+id+`","enabled":true}`); code != http.StatusOK || !strings.Contains(body, `"enabled":true`) {
		t.Fatalf("enable: %d %s", code, body)
	}
	// An edit that asks to stay enabled is turned off and says so.
	if code, body := call(http.MethodPatch, `{"id":"`+id+`","pattern":"batch over","action":"send_input","value":"continue","enabled":true}`); code != http.StatusOK || !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("edit: %d %s", code, body)
	}
	if code, _ := call(http.MethodPatch, `{"id":"`+id+`","pattern":"\\[y/N\\]","action":"send_input","value":"y"}`); code != http.StatusBadRequest {
		t.Fatalf("edit to prompt pattern: %d", code)
	}
	// Converting an alert filter needs the same consent.
	al, _ := fs.Add("boom", session.FilterActionAlert, "x")
	if code, _ := call(http.MethodPatch, `{"id":"`+al.ID+`","action":"send_input","value":"go"}`); code != http.StatusBadRequest {
		t.Fatalf("convert without consent: %d", code)
	}
}

func TestInputFiltersSwitchDefaultsOffAndPatches(t *testing.T) {
	s := bl90Server(t)
	if s.cfg.Session.InputFiltersEnabled {
		t.Fatal("must default off")
	}
	applyConfigPatch(s.cfg, map[string]interface{}{"session.input_filters_enabled": true})
	if !s.cfg.Session.InputFiltersEnabled {
		t.Fatal("patch did not turn it on")
	}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if !strings.Contains(rr.Body.String(), `"input_filters_enabled":true`) {
		t.Fatalf("GET /api/config lacks the switch: %d", rr.Code)
	}
	applyConfigPatch(s.cfg, map[string]interface{}{"session.input_filters_enabled": false})
	if s.cfg.Session.InputFiltersEnabled {
		t.Fatal("patch did not turn it off")
	}
}
