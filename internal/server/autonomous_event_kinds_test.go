package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func getEventKinds(t *testing.T, method string) (int, eventKindsResponse) {
	t.Helper()
	s := &Server{cfg: config.DefaultConfig()}
	rr := httptest.NewRecorder()
	s.handleAutonomousEventKinds(rr, httptest.NewRequest(method, "/api/autonomous/event-kinds", nil))
	var body eventKindsResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return rr.Code, body
}

func findEventKind(resp eventKindsResponse, kind string) (string, *eventKindEntry) {
	for _, g := range resp.Groups {
		for i := range g.Kinds {
			if g.Kinds[i].Kind == kind {
				return g.Group, &g.Kinds[i]
			}
		}
	}
	return "", nil
}

func TestEventKindsRESTGroupedAndComplete(t *testing.T) {
	code, resp := getEventKinds(t, http.MethodGet)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	total := 0
	for _, g := range resp.Groups {
		if g.Group == "" || len(g.Kinds) == 0 {
			t.Fatalf("empty group %+v", g)
		}
		for _, k := range g.Kinds {
			if k.Kind == "" || k.Description == "" {
				t.Fatalf("incomplete kind %+v", k)
			}
		}
		total += len(g.Kinds)
	}
	if want := len(autonomous.ListEventKinds()); total != want {
		t.Fatalf("listed %d kinds, registry has %d", total, want)
	}
}

func TestEventKindsRESTWakeDefault(t *testing.T) {
	_, resp := getEventKinds(t, http.MethodGet)
	if _, k := findEventKind(resp, "task.failed"); k == nil || !k.WakeDefault {
		t.Fatalf("task.failed should be a wake default: %+v", k)
	}
	if _, k := findEventKind(resp, "task.looping"); k == nil || k.WakeDefault {
		t.Fatalf("task.looping should not be a wake default: %+v", k)
	}
}

func TestEventKindsRESTExtensionKindAppears(t *testing.T) {
	err := autonomous.RegisterEventKind(autonomous.EventKindInfo{
		Kind: "ext.rest_listing_probe", Description: "A probe kind.", Group: autonomous.EventGroupTask,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, resp := getEventKinds(t, http.MethodGet)
	g, k := findEventKind(resp, "ext.rest_listing_probe")
	if k == nil || g != autonomous.EventGroupTask || k.WakeDefault {
		t.Fatalf("extension kind missing or wrong: group %q entry %+v", g, k)
	}
}

func TestEventKindsRESTMethodNotAllowed(t *testing.T) {
	if code, _ := getEventKinds(t, http.MethodPost); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d, want 405", code)
	}
}
