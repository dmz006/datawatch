// v6.0.4 (BL210-remaining) — tests for the round-2 MCP gap closure tools.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// ── tool name assertions ──────────────────────────────────────────────────────

func TestBL210Remaining_ToolNames(t *testing.T) {
	s := &Server{}
	cases := []struct {
		want string
		got  string
	}{
		{"filter_list", s.toolFilterList().Name},
		{"filter_add", s.toolFilterAdd().Name},
		{"filter_delete", s.toolFilterDelete().Name},
		{"filter_toggle", s.toolFilterToggle().Name},
		{"backends_list", s.toolBackendsList().Name},
		{"backends_active", s.toolBackendsActive().Name},
		{"session_set_state", s.toolSessionSetState().Name},
		{"federation_sessions", s.toolFederationSessions().Name},
		{"device_register", s.toolDeviceRegister().Name},
		{"device_list", s.toolDeviceList().Name},
		{"device_delete", s.toolDeviceDelete().Name},
		{"files_list", s.toolFilesList().Name},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("tool name = %q, want %q", c.got, c.want)
		}
	}
}

// ── filter_list ───────────────────────────────────────────────────────────────

func TestBL210Remaining_FilterList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/filters" {
			http.Error(w, "unexpected", 400)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{ //nolint:errcheck
			{"id": "f1", "pattern": "secret", "action": "alert"},
		})
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out := invoke(t, s.handleFilterList, nil)
	if !strings.Contains(out, "secret") {
		t.Errorf("expected filter payload, got: %s", out)
	}
}

// ── filter_add ────────────────────────────────────────────────────────────────

func TestBL210Remaining_FilterAdd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/filters" {
			http.Error(w, "unexpected", 400)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if body["pattern"] != "passwd" || body["action"] != "kill" {
			http.Error(w, "bad body", 400)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "f2"}) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out := invoke(t, s.handleFilterAdd, map[string]any{"pattern": "passwd", "action": "kill"})
	if !strings.Contains(out, "f2") {
		t.Errorf("expected created ID, got: %s", out)
	}
}

// ── backends_list ─────────────────────────────────────────────────────────────

func TestBL210Remaining_BackendsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/backends" {
			http.Error(w, "unexpected", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"llm":    []map[string]any{{"name": "claude-code", "available": true}},
			"active": "claude-code",
		})
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out := invoke(t, s.handleBackendsList, nil)
	if !strings.Contains(out, "claude-code") {
		t.Errorf("expected backend name, got: %s", out)
	}
}

// ── session_set_state ─────────────────────────────────────────────────────────

func TestBL210Remaining_SessionSetState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sessions/state" {
			http.Error(w, "unexpected", 400)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if body["id"] != "s1" || body["state"] != "complete" {
			http.Error(w, "bad body", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out := invoke(t, s.handleSessionSetState, map[string]any{"id": "s1", "state": "complete"})
	if !strings.Contains(out, "ok") {
		t.Errorf("expected ok status, got: %s", out)
	}
}

// ── files_list ────────────────────────────────────────────────────────────────

func TestBL210Remaining_FilesList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/files" {
			http.Error(w, "unexpected", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"entries": []map[string]any{{"name": "main.go", "type": "file"}},
		})
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out := invoke(t, s.handleFilesList, nil)
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected file entry, got: %s", out)
	}
}

// ── federation_sessions ───────────────────────────────────────────────────────

func TestBL210Remaining_FederationSessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/federation/sessions" {
			http.Error(w, "unexpected", 400)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{ //nolint:errcheck
			{"id": "remote-1", "peer": "peer-a", "state": "running"},
		})
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out := invoke(t, s.handleFederationSessions, nil)
	if !strings.Contains(out, "remote-1") {
		t.Errorf("expected federation session, got: %s", out)
	}
}

// ── device_register ───────────────────────────────────────────────────────────

func TestBL210Remaining_DeviceRegister(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/devices/register" {
			http.Error(w, "unexpected", 400)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if body["alias"] != "my-phone" {
			http.Error(w, "bad alias", 400)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "d1"}) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out := invoke(t, s.handleDeviceRegister, map[string]any{"alias": "my-phone", "token": "fcm-token-abc"})
	if !strings.Contains(out, "d1") {
		t.Errorf("expected device ID, got: %s", out)
	}
}

// ── filter_delete ─────────────────────────────────────────────────────────────

func TestBL210Remaining_FilterDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/filters" {
			http.Error(w, "unexpected", 400)
			return
		}
		if r.URL.Query().Get("id") != "f1" {
			http.Error(w, "bad id", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"}) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out := invoke(t, s.handleFilterDelete, map[string]any{"id": "f1"})
	if !strings.Contains(out, "deleted") {
		t.Errorf("expected deleted status, got: %s", out)
	}
}

// ── device_list ───────────────────────────────────────────────────────────────

func TestBL210Remaining_DeviceList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/devices" {
			http.Error(w, "unexpected", 400)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{ //nolint:errcheck
			{"id": "d1", "alias": "my-phone"},
		})
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out := invoke(t, s.handleDeviceList, nil)
	if !strings.Contains(out, "my-phone") {
		t.Errorf("expected device entry, got: %s", out)
	}
}

// ── no-webport error guard ────────────────────────────────────────────────────

func TestBL210Remaining_NoWebPort(t *testing.T) {
	s := &Server{webPort: 0}
	res, err := s.handleFilterList(context.Background(), call(nil))
	if err != nil {
		t.Fatal(err)
	}
	out := resultText(t, res, nil)
	if !strings.Contains(out, "Error") {
		t.Errorf("expected error when webPort=0, got: %s", out)
	}
}
