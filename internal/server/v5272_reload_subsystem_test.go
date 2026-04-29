// v5.27.2 — tests for subsystem-aware hot reload.
//
// Covers Server.RegisterReloader + Server.ReloadSubsystem and the
// `?subsystem=` HTTP query-param path. Pairs with the existing
// bl17_reload_test.go which covers the full-config-reload path.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReloadSubsystem_RegisteredFires(t *testing.T) {
	s := bl90Server(t)
	called := 0
	s.RegisterReloader("filters", func() error {
		called++
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/reload?subsystem=filters", nil)
	rr := httptest.NewRecorder()
	s.handleReload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if called != 1 {
		t.Errorf("reloader fired %d times, want 1", called)
	}
	var got ReloadResult
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if !got.OK {
		t.Errorf("expected ok, got %+v", got)
	}
	if got.Subsystem != "filters" {
		t.Errorf("subsystem=%q want filters", got.Subsystem)
	}
	if len(got.Applied) != 1 || got.Applied[0] != "filters" {
		t.Errorf("applied=%v want [filters]", got.Applied)
	}
}

func TestReloadSubsystem_UnknownReturns500(t *testing.T) {
	s := bl90Server(t)
	s.RegisterReloader("filters", func() error { return nil })

	req := httptest.NewRequest(http.MethodPost, "/api/reload?subsystem=bogus", nil)
	rr := httptest.NewRecorder()
	s.handleReload(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500; body=%s", rr.Code, rr.Body.String())
	}
	var got ReloadResult
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.OK {
		t.Errorf("expected !ok, got %+v", got)
	}
	if !strings.Contains(got.Error, "unknown subsystem") || !strings.Contains(got.Error, "filters") {
		t.Errorf("error message %q should name unknown subsystem + list registered ones", got.Error)
	}
}

func TestReloadSubsystem_PropagatesReloaderError(t *testing.T) {
	s := bl90Server(t)
	s.RegisterReloader("memory", func() error {
		return errors.New("backend not initialised")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/reload?subsystem=memory", nil)
	rr := httptest.NewRecorder()
	s.handleReload(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", rr.Code)
	}
	var got ReloadResult
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.OK {
		t.Errorf("expected !ok")
	}
	if !strings.Contains(got.Error, "backend not initialised") {
		t.Errorf("reloader error not surfaced; got %q", got.Error)
	}
}

func TestReloadSubsystem_EmptyNameFallsToFullReload(t *testing.T) {
	// Empty / "all" / "config" subsystem should run the full
	// config reload — same as bare POST /api/reload.
	s := bl90Server(t)
	for _, name := range []string{"", "all", "config"} {
		path := "/api/reload"
		if name != "" {
			path += "?subsystem=" + name
		}
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rr := httptest.NewRecorder()
		s.handleReload(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("subsystem=%q: status=%d body=%s", name, rr.Code, rr.Body.String())
		}
		var got ReloadResult
		_ = json.NewDecoder(rr.Body).Decode(&got)
		if !got.OK {
			t.Errorf("subsystem=%q: expected ok", name)
		}
		// Full reload populates RequiresRestart with the always-restart
		// list — that's how we know the full path ran (vs the named
		// per-subsystem reloader).
		if len(got.RequiresRestart) == 0 {
			t.Errorf("subsystem=%q: expected requires_restart list, got empty", name)
		}
	}
}

func TestReloadSubsystem_RegisterReloaderOverrides(t *testing.T) {
	// Re-registering against the same name overrides the previous
	// fn (last-write-wins so main.go can rebind on hot re-init paths).
	s := bl90Server(t)
	first, second := 0, 0
	s.RegisterReloader("memory", func() error { first++; return nil })
	s.RegisterReloader("memory", func() error { second++; return nil })

	res := s.ReloadSubsystem("memory")
	if !res.OK {
		t.Fatalf("expected ok, got %+v", res)
	}
	if first != 0 {
		t.Errorf("first reloader fired %d times, want 0 (overridden)", first)
	}
	if second != 1 {
		t.Errorf("second reloader fired %d times, want 1", second)
	}
}
