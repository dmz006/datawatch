package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func requestSettingsView(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	sec, ok := getConfigView(t, s)["delivery_requests"].(map[string]interface{})
	if !ok {
		t.Fatal("GET /api/config has no delivery_requests section")
	}
	return sec
}

// With nothing set all six read their values in force.
func TestReqSurfaceSettingsDefaults(t *testing.T) {
	want := map[string]interface{}{
		"request_take_by_seconds": float64(3600), "request_due_seconds": float64(86400),
		"request_due_seconds_by_kind": map[string]interface{}{}, "reminder_interval_seconds": float64(900),
		"reminders_per_session_hour": float64(2), "reminders_per_host_hour": float64(20),
	}
	if got := requestSettingsView(t, newRoundTripServer(t)); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %v", got)
	}
}

func TestReqSurfaceSettingsRoundTrip(t *testing.T) {
	s := newRoundTripServer(t)
	// Numbers are what the web sends; strings what chat, the command line and
	// the assistant tool send.
	for i, patch := range []map[string]interface{}{
		{"delivery.request_take_by_seconds": 60, "delivery.request_due_seconds": 120,
			"delivery.request_due_seconds_by_kind": map[string]interface{}{"ask": 300, "bug": 600},
			"delivery.reminder_interval_seconds":   30, "delivery.reminders_per_session_hour": 0, "delivery.reminders_per_host_hour": 7},
		{"delivery.request_take_by_seconds": "60", "delivery.request_due_seconds": "120",
			"delivery.request_due_seconds_by_kind": "ask=300, bug=600",
			"delivery.reminder_interval_seconds":   "30", "delivery.reminders_per_session_hour": "0", "delivery.reminders_per_host_hour": "7"},
		{"delivery.request_due_seconds_by_kind": `{"ask":300,"bug":"600","wake":0}`},
	} {
		if rr := putPatch(t, s, patch); rr.Code != http.StatusOK {
			t.Fatalf("PUT %d = %d: %s", i, rr.Code, rr.Body.String())
		}
		byKind := map[string]interface{}{"ask": float64(300), "bug": float64(600)}
		want := map[string]interface{}{"request_take_by_seconds": float64(60), "request_due_seconds": float64(120),
			"request_due_seconds_by_kind": byKind, "reminder_interval_seconds": float64(30),
			"reminders_per_session_hour": float64(0), "reminders_per_host_hour": float64(7)}
		if got := requestSettingsView(t, s); !reflect.DeepEqual(got, want) {
			t.Fatalf("patch %d readback = %v", i, got)
		}
		// A set key reads back under its own name too.
		sec := deliveryConfigView(t, s)
		for k, v := range want {
			if !reflect.DeepEqual(sec[k], v) {
				t.Errorf("delivery.%s = %v, want %v", k, sec[k], v)
			}
		}
	}
	loaded, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	d := loaded.Delivery
	if d.RequestTakeBySeconds != 60 || d.RequestDueSeconds != 120 || d.RequestDueSecondsByKind["bug"] != 600 || len(d.RequestDueSecondsByKind) != 2 ||
		d.ReminderIntervalSeconds != 30 || d.RemindersPerSessionHour == nil || *d.RemindersPerSessionHour != 0 ||
		d.RemindersPerHostHour == nil || *d.RemindersPerHostHour != 7 {
		t.Fatalf("saved = %+v", d)
	}
	// 0 gives a time its default back; 0 reminders stays none. An empty map
	// clears the kinds.
	if rr := putPatch(t, s, map[string]interface{}{"delivery.request_take_by_seconds": 0, "delivery.request_due_seconds_by_kind": ""}); rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	got := requestSettingsView(t, s)
	if got["request_take_by_seconds"] != float64(3600) || len(got["request_due_seconds_by_kind"].(map[string]interface{})) != 0 || got["reminders_per_session_hour"] != float64(0) {
		t.Fatalf("after zero = %v", got)
	}
}

// A negative or unreadable value is refused, and nothing in the same change is
// applied.
func TestReqSurfaceSettingsRefused(t *testing.T) {
	keys := []string{"request_take_by_seconds", "request_due_seconds", "reminder_interval_seconds", "reminders_per_session_hour", "reminders_per_host_hour"}
	var bad []map[string]interface{}
	for _, k := range keys {
		for _, v := range []interface{}{-1, "-5", 1.5, "soon", true, 1e30} {
			bad = append(bad, map[string]interface{}{"delivery." + k: v})
		}
	}
	for _, v := range []interface{}{-1, "ask=-1", "nokind=5", "ask", `{"ask":1.5}`, `{"ask":-2}`, []interface{}{1}, map[string]interface{}{"ask": -1}, map[string]interface{}{"x y": 1}} {
		bad = append(bad, map[string]interface{}{"delivery.request_due_seconds_by_kind": v})
	}
	for _, patch := range bad {
		s := newRoundTripServer(t)
		var key string
		for k := range patch {
			key = k
		}
		patch["delivery.notify"] = false
		rr := putPatch(t, s, patch)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), key) {
			b, _ := json.Marshal(patch)
			t.Errorf("PUT %s = %d: %s", b, rr.Code, rr.Body.String())
		}
		if deliveryConfigView(t, s)["notify"] != true {
			t.Errorf("a refused change applied part of itself: %v", patch)
		}
	}
}
