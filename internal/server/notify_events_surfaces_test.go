// notify_events — Configuration Accessibility Rule surfaces for the
// per-Automaton and per-guardrail-profile notification event lists:
//   - POST /api/autonomous/prds/{id}/set_notify_events (set/clear,
//     400 on invalid kinds, 405 on non-POST, federation write guard,
//     notify_events_effective present on the response)
//   - guardrail profile create/update GET/POST/PUT round-trip where a
//     PUT body distinguishes an absent notify_events key (unchanged)
//     from a present empty array (clears)
//   - GET /api/autonomous/status carries the notify block through
//     unchanged

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// notifyEventsSpy implements the pieces of AutonomousAPI the notify_events
// surfaces touch. The embedded interface stands in for everything else and
// would panic if one of those methods was ever called from a surface under
// test, which is exactly what we want to notice.
type notifyEventsSpy struct {
	AutonomousAPI // panics on any other method call

	status any
	// set_notify_events
	setPRDID   string
	setKinds   []string
	setCalled  int
	setReply   any
	setErr     error
	profileGet map[string]any
	// guardrail profiles
	created      *profileCall
	createdReply any
	createdErr   error
	updated      []*profileCall
	updatedReply any
	updatedErr   error
}

type profileCall struct {
	id        string
	notifySet bool
	notify    []string
}

func (s *notifyEventsSpy) Status() any { return s.status }

func (s *notifyEventsSpy) SetPRDNotifyEvents(prdID string, kinds []string) (any, error) {
	s.setPRDID = prdID
	s.setKinds = kinds
	s.setCalled++
	if s.setErr != nil {
		return nil, s.setErr
	}
	return s.setReply, nil
}

func (s *notifyEventsSpy) GetGuardrailProfile(id string) (any, bool) {
	if v, ok := s.profileGet[id]; ok {
		return v, true
	}
	return nil, false
}

func (s *notifyEventsSpy) CreateGuardrailProfile(name, description string, guardrails []string,
	blockOnFileDrift *bool, notifyEvents []string) (any, error) {
	s.created = &profileCall{notifySet: notifyEvents != nil, notify: append([]string(nil), notifyEvents...)}
	if s.createdErr != nil {
		return nil, s.createdErr
	}
	return s.createdReply, nil
}

func (s *notifyEventsSpy) UpdateGuardrailProfile(id, name, description string, guardrails []string,
	blockOnFileDrift *bool, notifyEvents []string) (any, error) {
	s.updated = append(s.updated, &profileCall{id: id, notifySet: notifyEvents != nil, notify: append([]string(nil), notifyEvents...)})
	if s.updatedErr != nil {
		return nil, s.updatedErr
	}
	return s.updatedReply, nil
}

func notifyEventsTestServer(spy *notifyEventsSpy) *Server {
	return &Server{cfg: &config.Config{}, autonomousMgr: spy}
}

func doNotifyRequest(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	var err error
	if body != "" {
		req, err = http.NewRequest(method, path, strings.NewReader(body))
	} else {
		req, err = http.NewRequest(method, path, nil)
	}
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, req)
	return rec
}

// doProfileRequest drives the guardrail-profile route, which is a distinct
// mux handler from the PRD one.
func doProfileRequest(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	s.handleAutonomousGuardrailProfiles(rec, req)
	return rec
}

func TestNotifyEvents_SetAndClear(t *testing.T) {
	spy := &notifyEventsSpy{
		setReply: map[string]any{
			"id":            "prd-1",
			"notify_events": []string{"prd.blocked", "prd.completed"},
			"notify_events_effective": map[string]any{
				"events": []string{"prd.blocked", "prd.completed"},
				"source": "automaton",
			},
		},
	}
	s := notifyEventsTestServer(spy)
	path := "/api/autonomous/prds/prd-1/set_notify_events"

	// Set two valid kinds.
	rec := doNotifyRequest(t, s, http.MethodPost, path, `{"notify_events":["prd.blocked","prd.completed"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST set: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if spy.setPRDID != "prd-1" || len(spy.setKinds) != 2 || spy.setKinds[0] != "prd.blocked" || spy.setKinds[1] != "prd.completed" {
		t.Fatalf("SetPRDNotifyEvents got %q %q", spy.setPRDID, spy.setKinds)
	}
	if !strings.Contains(rec.Body.String(), "notify_events_effective") {
		t.Errorf("set response missing notify_events_effective: %s", rec.Body.String())
	}

	// Clear: nil (key absent), null, and an empty array all mean "inherit".
	for _, body := range []string{`{}`, `{"notify_events":null}`, `{"notify_events":[]}`} {
		rec := doNotifyRequest(t, s, http.MethodPost, path, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: want 200, got %d (%s)", body, rec.Code, rec.Body.String())
		}
		if len(spy.setKinds) != 0 {
			t.Errorf("POST %s: manager got %v, want a clear (nil/empty)", body, spy.setKinds)
		}
	}
	if spy.setCalled != 4 {
		t.Fatalf("manager called %d times, want 4", spy.setCalled)
	}
}

func TestNotifyEvents_InvalidKindRejected(t *testing.T) {
	validList := "prd.blocked, prd.cancelled, prd.completed, prd.failed, prd.needs_review, prd.updated, task.completed"
	for _, tc := range []struct {
		body   string
		err    error
		wantIn string
	}{
		{`{"notify_events":["bogus.kind"]}`, errors.New(`unknown notify event kind "bogus.kind"; valid kinds: ` + validList), "bogus.kind"},
		{`{"notify_events":["prd.blocked",""]}`, errors.New("notify event kind 1 is empty"), "is empty"},
		{`{"notify_events":"not-an-array"}`, errors.New("bad request: json: cannot unquote"), "bad request"},
	} {
		spy := &notifyEventsSpy{setErr: tc.err}
		s := notifyEventsTestServer(spy)
		rec := doNotifyRequest(t, s, http.MethodPost, "/api/autonomous/prds/prd-1/set_notify_events", tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: want 400, got %d (%s)", tc.body, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tc.wantIn) {
			t.Errorf("%q: body %q, want it to mention %q", tc.body, rec.Body.String(), tc.wantIn)
		}
	}
}

func TestNotifyEvents_MethodNotAllowed(t *testing.T) {
	spy := &notifyEventsSpy{}
	s := notifyEventsTestServer(spy)
	rec := doNotifyRequest(t, s, http.MethodGet, "/api/autonomous/prds/prd-1/set_notify_events", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: want 405, got %d", rec.Code)
	}
	if spy.setCalled != 0 {
		t.Fatalf("manager called %d times for a GET it must not touch", spy.setCalled)
	}
}

func TestNotifyEvents_FederationWriteGuard(t *testing.T) {
	spy := &notifyEventsSpy{}
	s := notifyEventsTestServer(spy)

	req, err := http.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-1/set_notify_events", strings.NewReader(`{"notify_events":["prd.blocked"]}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	// A session-scoped credential whose resolved capability list is read-only:
	// fedCap sees sessionCapsKey first and denies the write.
	req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{"autonomous:read"}))
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	if spy.setCalled != 0 {
		t.Fatalf("denied request still reached the manager")
	}
}

func TestNotifyEvents_GuardrailProfileCreateUpdate(t *testing.T) {
	spy := &notifyEventsSpy{
		createdReply: map[string]any{"id": "gp-new", "notify_events": []string{"prd.blocked"}},
		updatedReply: map[string]any{"id": "gp-1", "notify_events": []any{}},
		profileGet:   map[string]any{"gp-1": map[string]any{"id": "gp-1", "name": "strict", "notify_events": []string{"prd.blocked"}}},
	}
	s := notifyEventsTestServer(spy)

	// POST create: present list recorded, absent key stays nil (inherit).
	rec := doProfileRequest(t, s, http.MethodPost, "/api/autonomous/guardrail_profiles", `{"name":"n1","notify_events":["prd.blocked"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with notify_events: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if spy.created == nil || !spy.created.notifySet || len(spy.created.notify) != 1 || spy.created.notify[0] != "prd.blocked" {
		t.Fatalf("create: manager got %v, want notify [prd.blocked]", spy.created)
	}
	spy.created = nil
	rec = doProfileRequest(t, s, http.MethodPost, "/api/autonomous/guardrail_profiles", `{"name":"n2"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create without notify_events: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if spy.created == nil || spy.created.notifySet {
		t.Fatalf("create absent key: manager got %v, want nil notify (inherit)", spy.created)
	}

	// PUT update on an existing profile: absent key → unchanged (nil),
	// present empty array → clears (non-nil empty), present list → replaces.
	rec = doProfileRequest(t, s, http.MethodPut, "/api/autonomous/guardrail_profiles/gp-1", `{"name":"strict2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT absent key: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doProfileRequest(t, s, http.MethodPut, "/api/autonomous/guardrail_profiles/gp-1", `{"notify_events":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT empty array: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doProfileRequest(t, s, http.MethodPut, "/api/autonomous/guardrail_profiles/gp-1", `{"notify_events":["prd.blocked","prd.failed"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT list: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(spy.updated) != 3 {
		t.Fatalf("UpdateGuardrailProfile called %d times, want 3", len(spy.updated))
	}
	if got := spy.updated[0]; got == nil || got.notifySet {
		t.Fatalf("PUT absent key: manager got %v, want nil notify (unchanged)", got)
	}
	if got := spy.updated[1]; got == nil || !got.notifySet || len(got.notify) != 0 {
		t.Fatalf("PUT empty array: manager got %v, want non-nil empty (clear)", got)
	}
	if got := spy.updated[2]; got == nil || !got.notifySet || len(got.notify) != 2 {
		t.Fatalf("PUT list: manager got %v, want [prd.blocked prd.failed]", got)
	}

	// GET an existing profile returns it with notify_events.
	rec = doProfileRequest(t, s, http.MethodGet, "/api/autonomous/guardrail_profiles/gp-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "notify_events") && !strings.Contains(rec.Body.String(), "prd.blocked") {
		t.Errorf("GET body lacks the profile's notify_events: %s", rec.Body.String())
	}
}

func TestNotifyEvents_StatusNotifyBlock(t *testing.T) {
	spy := &notifyEventsSpy{
		status: map[string]any{
			"running": true,
			"notify": map[string]any{
				"default_events": []string{"prd.blocked", "prd.completed"},
				"overrides": []any{
					map[string]any{"id": "prd-1", "source": "automaton", "events": []string{"task.completed"}},
					map[string]any{"id": "prd-2", "source": "profile", "events": []string{}},
				},
			},
		},
	}
	s := notifyEventsTestServer(spy)
	rec := httptest.NewRecorder()
	s.handleAutonomousStatus(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got struct {
		Notify struct {
			DefaultEvents []string `json:"default_events"`
			Overrides     []struct {
				ID     string   `json:"id"`
				Source string   `json:"source"`
				Events []string `json:"events"`
			} `json:"overrides"`
		} `json:"notify"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v (%s)", err, rec.Body.String())
	}
	if len(got.Notify.DefaultEvents) != 2 || got.Notify.DefaultEvents[0] != "prd.blocked" {
		t.Fatalf("default_events = %v", got.Notify.DefaultEvents)
	}
	if len(got.Notify.Overrides) != 2 || got.Notify.Overrides[1].Source != "profile" || len(got.Notify.Overrides[0].Events) != 1 {
		t.Fatalf("overrides = %+v", got.Notify.Overrides)
	}
}

// TestNotifyEvents_WarningKindsOnServerSurfaces pins the new opt-in
// warning kinds on the server REST surface: the Automaton
// set_notify_events endpoint and the guardrail-profile create/update
// paths all accept scope_warning and guardrail_warn (they are warnings —
// accepting them never implies blocking), while a near-miss like
// "scope.warning" is rejected with a message that names the valid
// warning kinds.
func TestNotifyEvents_WarningKindsOnServerSurfaces(t *testing.T) {
	spy := &notifyEventsSpy{
		setReply: map[string]any{
			"id":            "prd-1",
			"notify_events": []string{"prd.blocked", "scope_warning", "guardrail_warn"},
		},
		createdReply: map[string]any{"id": "gp-2", "notify_events": []string{"scope_warning", "guardrail_warn"}},
		updatedReply: map[string]any{"id": "gp-1", "notify_events": []string{"guardrail_warn"}},
		profileGet:   map[string]any{"gp-1": map[string]any{"id": "gp-1", "name": "strict"}},
	}
	s := notifyEventsTestServer(spy)

	// Automaton set: both new warning kinds accepted.
	rec := doNotifyRequest(t, s, http.MethodPost, "/api/autonomous/prds/prd-1/set_notify_events",
		`{"notify_events":["prd.blocked","scope_warning","guardrail_warn"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set with warning kinds: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := spy.setKinds; len(got) != 3 || got[0] != "prd.blocked" || got[1] != "scope_warning" || got[2] != "guardrail_warn" {
		t.Fatalf("manager set got %v, want [prd.blocked scope_warning guardrail_warn]", got)
	}

	// Profile create: warning kinds accepted.
	spy.created = nil
	rec = doProfileRequest(t, s, http.MethodPost, "/api/autonomous/guardrail_profiles",
		`{"name":"warn","notify_events":["scope_warning","guardrail_warn"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with warning kinds: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if spy.created == nil || !spy.created.notifySet || len(spy.created.notify) != 2 || spy.created.notify[0] != "scope_warning" {
		t.Fatalf("create: manager got %v, want notify [scope_warning guardrail_warn]", spy.created)
	}

	// Profile update: warning kind replaces the list.
	rec = doProfileRequest(t, s, http.MethodPut, "/api/autonomous/guardrail_profiles/gp-1",
		`{"notify_events":["guardrail_warn"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update with warning kind: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := spy.updated[len(spy.updated)-1]; got == nil || !got.notifySet || len(got.notify) != 1 || got.notify[0] != "guardrail_warn" {
		t.Fatalf("update: manager got %v, want notify [guardrail_warn]", got)
	}

	// Near-miss: a dotted variant is rejected and the error names the valid
	// warning kinds.
	spy.setErr = errors.New(`unknown notify event kind "scope.warning"; valid kinds: guardrail_warn, scope_warning, ...`)
	rec = doNotifyRequest(t, s, http.MethodPost, "/api/autonomous/prds/prd-1/set_notify_events",
		`{"notify_events":["scope.warning"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("set scope.warning: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "scope_warning") {
		t.Errorf("error body should name the valid warning kinds: %s", rec.Body.String())
	}
}
