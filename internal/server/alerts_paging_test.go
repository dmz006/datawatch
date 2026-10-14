// Cheap alert polling — paged GET /api/alerts, /api/alerts/count,
// alerts_read WS broadcast, /api/info capabilities (BL432 / GH#242).

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// alertsPagingServer returns a server with n alerts, newest last in the store.
func alertsPagingServer(t *testing.T, n int) (*Server, []*alerts.Alert) {
	t.Helper()
	s := gh201Phase5Server(t)
	var made []*alerts.Alert
	for i := 0; i < n; i++ {
		made = append(made, s.alertStore.Add(alerts.LevelInfo, "t", "b", ""))
	}
	return s, made
}

func getAlerts(t *testing.T, s *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/alerts"+query, nil)
	w := httptest.NewRecorder()
	s.handleAlerts(w, req)
	return w
}

func decodeKeys(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return m
}

func TestAlertsGet_NoParamsKeepsLegacyShape(t *testing.T) {
	s, _ := alertsPagingServer(t, 3)
	w := getAlerts(t, s, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	// Byte-identical to the pre-paging encoder output.
	var want bytes.Buffer
	json.NewEncoder(&want).Encode(struct { //nolint:errcheck
		Alerts      []*alerts.Alert `json:"alerts"`
		UnreadCount int             `json:"unread_count"`
	}{s.alertStore.List(), s.alertStore.UnreadCount()})
	if w.Body.String() != want.String() {
		t.Fatalf("legacy body changed:\n got %s\nwant %s", w.Body.String(), want.String())
	}
	m := decodeKeys(t, w.Body.Bytes())
	for _, k := range []string{"total", "has_more", "next_before"} {
		if _, ok := m[k]; ok {
			t.Errorf("legacy response unexpectedly has %q", k)
		}
	}
	// source= alone is still the legacy form.
	m = decodeKeys(t, getAlerts(t, s, "?source=system").Body.Bytes())
	if _, ok := m["total"]; ok {
		t.Error("source-only request should keep the legacy shape")
	}
}

func TestAlertsGet_LimitPagesWithCursor(t *testing.T) {
	s, made := alertsPagingServer(t, 5)
	w := getAlerts(t, s, "?limit=2")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var p1 alertsPageResponse
	json.Unmarshal(w.Body.Bytes(), &p1) //nolint:errcheck
	if len(p1.Alerts) != 2 || p1.Alerts[0].ID != made[4].ID || !p1.HasMore || p1.Total != 5 || p1.UnreadCount != 5 {
		t.Fatalf("page1 = %+v", p1)
	}
	if p1.NextBefore != made[3].ID {
		t.Fatalf("next_before = %q, want %q", p1.NextBefore, made[3].ID)
	}
	w = getAlerts(t, s, "?limit=10&before="+p1.NextBefore)
	var p2 alertsPageResponse
	json.Unmarshal(w.Body.Bytes(), &p2) //nolint:errcheck
	if len(p2.Alerts) != 3 || p2.Alerts[0].ID != made[2].ID || p2.HasMore {
		t.Fatalf("page2 = %+v", p2)
	}
	if _, ok := decodeKeys(t, w.Body.Bytes())["next_before"]; ok {
		t.Error("next_before must be omitted when has_more is false")
	}
}

func TestAlertsGet_LimitZeroCountsOnly(t *testing.T) {
	s, _ := alertsPagingServer(t, 2)
	w := getAlerts(t, s, "?limit=0")
	m := decodeKeys(t, w.Body.Bytes())
	if string(m["alerts"]) != "[]" {
		t.Fatalf("alerts = %s, want []", m["alerts"])
	}
	if string(m["total"]) != "2" || string(m["unread_count"]) != "2" {
		t.Fatalf("counts wrong: %s", w.Body.String())
	}
}

func TestAlertsGet_UnreadOnly(t *testing.T) {
	s, made := alertsPagingServer(t, 3)
	if err := s.alertStore.MarkRead(made[2].ID); err != nil {
		t.Fatal(err)
	}
	var p alertsPageResponse
	json.Unmarshal(getAlerts(t, s, "?unread=1").Body.Bytes(), &p) //nolint:errcheck
	if len(p.Alerts) != 2 || p.Alerts[0].ID != made[1].ID || p.UnreadCount != 2 || p.Total != 3 {
		t.Fatalf("unread page = %+v", p)
	}
}

func TestAlertsGet_SourceCombinedWithPaging(t *testing.T) {
	s, _ := alertsPagingServer(t, 2)
	sys := s.alertStore.AddSystem(alerts.LevelWarn, "sys", "")
	var p alertsPageResponse
	json.Unmarshal(getAlerts(t, s, "?source=system&limit=5").Body.Bytes(), &p) //nolint:errcheck
	if len(p.Alerts) != 1 || p.Alerts[0].ID != sys.ID || p.Total != 1 {
		t.Fatalf("source page = %+v", p)
	}
}

func TestAlertsGet_BadParams(t *testing.T) {
	s, _ := alertsPagingServer(t, 1)
	for _, q := range []string{"?limit=-1", "?limit=abc", "?unread=maybe", "?before=", "?before=doesnotexist"} {
		if w := getAlerts(t, s, q); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, w.Code)
		}
	}
}

func TestAlertsCount(t *testing.T) {
	s, made := alertsPagingServer(t, 3)
	s.alertStore.MarkRead(made[0].ID) //nolint:errcheck
	w := httptest.NewRecorder()
	s.handleAlertsCount(w, httptest.NewRequest(http.MethodGet, "/api/alerts/count", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var got map[string]int
	json.Unmarshal(w.Body.Bytes(), &got) //nolint:errcheck
	if got["unread_count"] != 2 || got["total"] != 3 || len(got) != 2 {
		t.Fatalf("count = %v", got)
	}

	w = httptest.NewRecorder()
	s.handleAlertsCount(w, httptest.NewRequest(http.MethodPost, "/api/alerts/count", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status %d, want 405", w.Code)
	}

	s.alertStore = nil
	w = httptest.NewRecorder()
	s.handleAlertsCount(w, httptest.NewRequest(http.MethodGet, "/api/alerts/count", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("nil store status %d, want 503", w.Code)
	}
}

func TestAlertsCount_FedCapEnforced(t *testing.T) {
	s, store, _ := newFedTestServer(t)
	as, err := alerts.NewStore(t.TempDir() + "/alerts.json")
	if err != nil {
		t.Fatal(err)
	}
	s.alertStore = as
	for _, e := range []*multiserver.Entry{
		{Name: "peer-alerts", Token: "tok-alerts", URL: "http://a:1", Enabled: true, Federated: true, Capabilities: []string{"alerts:list"}},
		{Name: "peer-health", Token: "tok-health", URL: "http://h:1", Enabled: true, Federated: true, Capabilities: []string{"health:read"}},
	} {
		if err := store.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"count", s.handleAlertsCount, "/api/alerts/count"},
		{"paged list", s.handleAlerts, "/api/alerts?limit=5"},
	} {
		wrapped := s.fedAuthMiddleware(h.handler)
		for tok, want := range map[string]int{"admin-token": 200, "tok-alerts": 200, "tok-health": 403} {
			req := httptest.NewRequest(http.MethodGet, h.path, nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)
			if rr.Code != want {
				t.Errorf("%s with %s: status %d, want %d (%s)", h.name, tok, rr.Code, want, rr.Body.String())
			}
		}
	}
}

// wsCapture registers a fake client on a running hub and returns its channel.
func wsCapture(t *testing.T) (*Hub, chan []byte) {
	t.Helper()
	hub := NewHub()
	ch := make(chan []byte, 16)
	go hub.Run()
	hub.register <- &client{hub: hub, send: ch, subscribed: make(map[string]bool)}
	return hub, ch
}

func nextAlertsRead(t *testing.T, ch chan []byte) alertsReadPayload {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case raw := <-ch:
			var msg WSMessage
			if err := json.Unmarshal(raw, &msg); err != nil || msg.Type != MsgAlertsRead {
				continue
			}
			var p alertsReadPayload
			json.Unmarshal(msg.Data, &p) //nolint:errcheck
			return p
		case <-deadline:
			t.Fatal("no alerts_read broadcast within 1s")
		}
	}
}

func postMarkRead(t *testing.T, s *Server, body string) int {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleAlerts(w, httptest.NewRequest(http.MethodPost, "/api/alerts", strings.NewReader(body)))
	return w.Code
}

func TestAlertsMarkRead_BroadcastsAlertsRead(t *testing.T) {
	s, made := alertsPagingServer(t, 3)
	hub, ch := wsCapture(t)
	s.hub = hub

	if code := postMarkRead(t, s, `{"id":"`+made[0].ID+`"}`); code != http.StatusOK {
		t.Fatalf("mark one: %d", code)
	}
	p := nextAlertsRead(t, ch)
	if len(p.IDs) != 1 || p.IDs[0] != made[0].ID || p.All || p.UnreadCount != 2 {
		t.Fatalf("single payload = %+v", p)
	}

	if code := postMarkRead(t, s, `{"all":true}`); code != http.StatusOK {
		t.Fatalf("mark all: %d", code)
	}
	p = nextAlertsRead(t, ch)
	if !p.All || len(p.IDs) != 0 || p.UnreadCount != 0 {
		t.Fatalf("all payload = %+v", p)
	}
}

func TestAlertsMarkRead_NoBroadcastOnFailure(t *testing.T) {
	s, _ := alertsPagingServer(t, 1)
	hub, ch := wsCapture(t)
	s.hub = hub
	if code := postMarkRead(t, s, `{"id":"missing"}`); code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", code)
	}
	select {
	case raw := <-ch:
		if strings.Contains(string(raw), `"alerts_read"`) {
			t.Fatalf("unexpected broadcast after failed mark-read: %s", raw)
		}
	case <-time.After(150 * time.Millisecond):
	}
}

func TestAlertsMarkRead_NilHubSafe(t *testing.T) {
	s, made := alertsPagingServer(t, 1)
	s.hub = nil
	if code := postMarkRead(t, s, `{"id":"`+made[0].ID+`"}`); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
}

func TestHandleInfo_AdvertisesAlertCapabilities(t *testing.T) {
	s := bl90Server(t)
	rr := httptest.NewRecorder()
	s.handleInfo(rr, httptest.NewRequest(http.MethodGet, "/api/info", nil))
	var v struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, c := range v.Capabilities {
		have[c] = true
	}
	for _, want := range []string{"alerts.paging", "alerts.count", "ws.alerts_read"} {
		if !have[want] {
			t.Errorf("capabilities=%v missing %s", v.Capabilities, want)
		}
	}
}
